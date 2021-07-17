package updates

import (
	"encoding/json"
	"fmt"
	"net/http"

	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/redhatinsights/edge-api/config"
	"github.com/redhatinsights/edge-api/pkg/commits"
	"github.com/redhatinsights/edge-api/pkg/common"
	"github.com/redhatinsights/edge-api/pkg/db"
	"github.com/redhatinsights/edge-api/pkg/models"

	"github.com/cavaliercoder/grab"
	apierrors "github.com/redhatinsights/edge-api/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// MakeRouter adds support for operations on update
func MakeRouter(sub chi.Router) {
	sub.Use(UpdateCtx)
	sub.With(common.Paginate).Get("/", GetUpdates)
	sub.Post("/", AddUpdate)
	sub.Route("/{updateID}", func(r chi.Router) {
		r.Use(UpdateCtx)
		r.Get("/", GetByID)
		r.Put("/", UpdatesUpdate)
	})
}

func GetUpdates(w http.ResponseWriter, r *http.Request) {
	var updates []models.UpdateTransaction
	account, err := common.GetAccount(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// FIXME - need to sort out how to get this query to be against commit.account
	result := db.DB.Where("account = ?", account).Find(&updates)
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(&updates)
}

func isUUID(param string) bool {
	_, err := uuid.Parse(param)
	return err == nil

}

var RepoBuilderInstance RepoBuilderInterface

// InitRepoBuilder initializes the repository builder in this package
func InitRepoBuilder() {
	RepoBuilderInstance = &RepoBuilder{}
}

func getCommitFromDB(commitID uint) (*models.Commit, error) {
	var commit models.Commit
	result := db.DB.First(&commit, commitID)
	if result.Error != nil {
		return nil, result.Error
	}
	return &commit, nil
}

type UpdatePostJSON struct {
	CommitID   uint   `json:"CommitID"`
	Tag        string `json:"Tag"`
	DeviceUUID string `json:"DeviceUUID"`
}

func updateFromHTTP(w http.ResponseWriter, r *http.Request) (*models.UpdateTransaction, error) {
	var updateJSON UpdatePostJSON
	err := json.NewDecoder(r.Body).Decode(&updateJSON)
	log.Debugf("updateFromHTTP::updateJSON: %#v", updateJSON)

	if !(updateJSON.CommitID == 0) {
		err := apierrors.NewInternalServerError()
		err.Title = fmt.Sprint("Must provide a CommitID")
		w.WriteHeader(err.Status)
		return nil, err
	}
	if (updateJSON.Tag == "") && (updateJSON.DeviceUUID == "") {
		err := apierrors.NewInternalServerError()
		err.Title = fmt.Sprint("At least one of Tag or DeviceUUID required.")
		w.WriteHeader(err.Status)
		return nil, err
	}

	var inventory Inventory
	if updateJSON.Tag != "" {
		inventory, err = ReturnDevicesByTag(w, r)
		if err != nil {
			err := apierrors.NewInternalServerError()
			err.Title = fmt.Sprintf("No devices in this tag %s", updateJSON.Tag)
			w.WriteHeader(err.Status)
			return &models.UpdateTransaction{}, err
		}
	}
	if updateJSON.DeviceUUID != "" {
		inventory, err = ReturnDevicesByID(w, r)
		if err != nil {
			err := apierrors.NewInternalServerError()
			err.Title = fmt.Sprintf("No devices found for UUID %s", updateJSON.DeviceUUID)
			w.WriteHeader(err.Status)
			return &models.UpdateTransaction{}, err
		}
	}

	update := models.UpdateTransaction{}
	update.Commit, err = getCommitFromDB(updateJSON.CommitID)
	inventoryHosts := update.InventoryHosts
	oldCommits := update.OldCommits
	// - populate the update.InventoryHosts []Device data
	fmt.Printf("Devices in this tag %v", inventory.Result)
	for _, device := range inventory.Result {
		updateDevice := new(models.Device)
		updateDevice.UUID = device.ID
		updateDevice.DesiredHash = update.Commit.OSTreeCommit
		inventoryHosts = append(inventoryHosts, *updateDevice)
		update.InventoryHosts = inventoryHosts
		for _, ostreeDeployment := range device.Ostree.RpmOstreeDeployments {
			if ostreeDeployment.Booted {
				var oldCommit models.Commit
				result := db.DB.Where("ostreecommit = ?", ostreeDeployment.Checksum).Take(&oldCommit)
				if result.Error != nil {
					http.Error(w, result.Error.Error(), http.StatusBadRequest)
					return &models.UpdateTransaction{}, err
				}
				oldCommits = append(oldCommits, oldCommit)
				update.OldCommits = oldCommits
			}
		}

	}

	log.Debugf("updateFromHTTP::update: %#v", update)
	return &update, err
}

type key int

const UpdateContextKey key = 0

// Implement Context interface so we can shuttle around multiple values
type UpdateContext struct {
	DeviceUUID string
	Tag        string
}

// UpdateCtx is a handler for Update requests
func UpdateCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var uCtx UpdateContext
		uCtx.DeviceUUID = chi.URLParam(r, "DeviceUUID")

		uCtx.Tag = chi.URLParam(r, "Tag")
		log.Debugf("UpdateCtx::uCtx: %#v", uCtx)
		ctx := context.WithValue(r.Context(), UpdateContextKey, &uCtx)
		log.Debugf("UpdateCtx::ctx: %#v", ctx)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AddUpdate adds an object to the database for an account
func AddUpdate(w http.ResponseWriter, r *http.Request) {

	update, err := updateFromHTTP(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	update.Account, err = common.GetAccount(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check to make sure we're not duplicating the job
	// FIXME - this didn't work and I don't have time to debug right now
	// FIXME - handle UpdateTransaction Commit vs UpdateCommitID
	/*
		var dupeRecord models.UpdateTransaction
		queryDuplicate := map[string]interface{}{
			"Account":        update.Account,
			"InventoryHosts": update.InventoryHosts,
			"OldCommitIDs":   update.OldCommitIDs,
		}
		result := db.DB.Where(queryDuplicate).Find(&dupeRecord)
		if result.Error == nil {
			if dupeRecord.UpdateCommitID != 0 {
				http.Error(w, "Can not submit duplicate update job", http.StatusInternalServerError)
				return
			}
		}
	*/

	// FIXME - need to remove duplicate OldCommit values from UpdateTransaction

	json.NewEncoder(w).Encode(&update)
	db.DB.Create(&update)

	go RepoBuilderInstance.BuildRepo(update)
}

// GetByID obtains an update from the database for an account
func GetByID(w http.ResponseWriter, r *http.Request) {
	if update := getUpdate(w, r); update != nil {
		json.NewEncoder(w).Encode(update)
	}
}

// UpdatesUpdate a update object in the database for an an account
func UpdatesUpdate(w http.ResponseWriter, r *http.Request) {
	update := getUpdate(w, r)
	if update == nil {
		return
	}

	incoming, err := updateFromHTTP(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	incoming.ID = update.ID
	incoming.CreatedAt = now
	incoming.UpdatedAt = now
	db.DB.Save(&incoming)

	json.NewEncoder(w).Encode(incoming)
}

func getUpdate(w http.ResponseWriter, r *http.Request) *models.UpdateTransaction {
	ctx := r.Context()
	update, ok := ctx.Value(UpdateContextKey).(*models.UpdateTransaction)
	if !ok {
		http.Error(w, "must pass id", http.StatusBadRequest)
		return nil
	}
	return update
}

// RepoBuilderInterface defines the interface of a repository builder
type RepoBuilderInterface interface {
	BuildRepo(ur *models.UpdateTransaction) (*models.UpdateTransaction, error)
}

// RepoBuilder is the implementation of a RepoBuilderInterface
type RepoBuilder struct{}

// BuildRepo build an update repo with the set of commits all merged into a single repo
// with static deltas generated between them all
func (rb *RepoBuilder) BuildRepo(ur *models.UpdateTransaction) (*models.UpdateTransaction, error) {
	cfg := config.Get()

	var update models.UpdateTransaction
	db.DB.First(&update, ur.ID)
	update.Status = models.UpdateStatusCreated
	db.DB.Save(&update)

	log.Debugf("RepoBuilder::updateCommit: %#v", ur.Commit)

	path := filepath.Join(cfg.UpdateTempPath, strconv.FormatUint(uint64(ur.ID), 10))
	log.Debugf("RepoBuilder::path: %#v", path)
	err := os.MkdirAll(path, os.FileMode(int(0755)))
	if err != nil {
		return nil, err
	}
	err = os.Chdir(path)
	if err != nil {
		return nil, err
	}
	DownloadExtractVersionRepo(ur.Commit, path)

	if len(ur.OldCommits) > 0 {
		stagePath := filepath.Join(path, "staging")
		err = os.MkdirAll(stagePath, os.FileMode(int(0755)))
		if err != nil {
			return nil, err
		}
		err = os.Chdir(stagePath)
		if err != nil {
			return nil, err
		}

		// If there are any old commits, we need to download them all to be merged
		// into the update commit repo
		//
		// FIXME: hardcoding "repo" in here because that's how it comes from osbuild
		for _, commit := range ur.OldCommits {
			DownloadExtractVersionRepo(&commit, filepath.Join(stagePath, commit.OSTreeCommit))
			RepoPullLocalStaticDeltas(ur.Commit, &commit, filepath.Join(path, "repo"), filepath.Join(stagePath, commit.OSTreeCommit, "repo"))
		}

		// Once all the old commits have been pulled into the update commit's repo
		// and has static deltas generated, then we don't need the old commits
		// anymore.
		err = os.RemoveAll(stagePath)
		if err != nil {
			return nil, err
		}

	}

	var uploader commits.Uploader
	uploader = &commits.FileUploader{
		BaseDir: path,
	}
	if cfg.BucketName != "" {
		uploader = commits.NewS3Uploader()
	}
	// FIXME: Need to actually do something with the return string for Server

	// NOTE: This relies on the file path being cfg.UpdateTempPath/models.UpdateTransaction.ID
	repoURL, err := uploader.UploadRepo(filepath.Join(path, "repo"), ur.Account)
	if err != nil {
		return nil, err
	}

	var updateDone models.UpdateTransaction
	db.DB.First(&updateDone, ur.ID)
	updateDone.Status = models.UpdateStatusSuccess
	updateDone.UpdateRepoURL = repoURL
	db.DB.Save(&updateDone)

	return &updateDone, nil
}

// DownloadExtractVersionRepo Download and Extract the repo tarball to dest dir
func DownloadExtractVersionRepo(c *models.Commit, dest string) error {
	// ensure the destination directory exists and then chdir there
	log.Debugf("DownloadExtractVersionRepo::dest: %#v", dest)
	err := os.MkdirAll(dest, os.FileMode(int(0755)))
	if err != nil {
		return err
	}
	err = os.Chdir(dest)
	if err != nil {
		return err
	}

	// Save the tarball to the OSBuild Hash ID and then extract it
	tarFileName := strings.Join([]string{c.ImageBuildHash, "tar"}, ".")
	log.Debugf("DownloadExtractVersionRepo::tarFileName: %#v", tarFileName)
	_, err = grab.Get(filepath.Join(dest, tarFileName), c.ImageBuildTarURL)
	if err != nil {
		return err
	}
	log.Debugf("Download finished::tarFileName: %#v", tarFileName)

	tarFile, err := os.Open(filepath.Join(dest, tarFileName))
	if err != nil {
		return err
	}
	err = common.Untar(tarFile, filepath.Join(dest))
	if err != nil {
		return err
	}
	tarFile.Close()

	err = os.Remove(filepath.Join(dest, tarFileName))
	if err != nil {
		return err
	}

	// FIXME: The repo path is hard coded because this is how it comes from
	//		  osbuild composer but we might want to revisit this later
	//
	// commit the version metadata to the current ref
	cmd := exec.Command("ostree", "--repo", "./repo", "commit", c.OSTreeRef, "--add-metadata-string", fmt.Sprintf("version=%s.%d", c.BuildDate, c.BuildNumber))
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

// RepoPullLocalStaticDeltas pull local repo into the new update repo and compute static deltas
//  uprepo should be where the update commit lives, u is the update commit
//  oldrepo should be where the old commit lives, o is the commit to be merged
func RepoPullLocalStaticDeltas(u *models.Commit, o *models.Commit, uprepo string, oldrepo string) error {
	err := os.Chdir(uprepo)
	if err != nil {
		return err
	}

	updateRevParse, err := RepoRevParse(uprepo, u.OSTreeRef)
	if err != nil {
		return err
	}
	oldRevParse, err := RepoRevParse(oldrepo, o.OSTreeRef)
	if err != nil {
		return err
	}

	// pull the local repo at the exact rev (which was HEAD of o.OSTreeRef)
	cmd := exec.Command("ostree", "--repo", uprepo, "pull-local", oldrepo, oldRevParse)
	err = cmd.Run()
	if err != nil {
		return err
	}

	// generate static delta
	cmd = exec.Command("ostree", "--repo", uprepo, "static-delta", "generate", "--from", oldRevParse, "--to", updateRevParse)
	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil

}

// RepoRevParse Handle the RevParse separate since we need the stdout parsed
func RepoRevParse(path string, ref string) (string, error) {
	cmd := exec.Command("ostree", "rev-parse", "--repo", path, ref)

	var res bytes.Buffer
	cmd.Stdout = &res

	err := cmd.Run()

	if err != nil {
		return "", err
	}

	return strings.TrimSpace(res.String()), nil
}

package commits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"

	"github.com/redhatinsights/edge-api/config"
	"github.com/redhatinsights/edge-api/pkg/common"
	"github.com/redhatinsights/edge-api/pkg/db"
	"github.com/redhatinsights/edge-api/pkg/models"

	"github.com/cavaliercoder/grab"
	log "github.com/sirupsen/logrus"
)

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

func updateFromReadCloser(rc io.ReadCloser) (*models.UpdateTransaction, error) {
	defer rc.Close()
	var update models.UpdateTransaction
	err := json.NewDecoder(rc).Decode(&update)

	log.Debugf("updateFromReadCloser::update: %#v", update)
	log.Debugf("updateFromReadCloser::update.Commit: %#v", update.Commit)

	if !(update.Commit.OSTreeCommit == "") {
		return nil, errors.New("Invalid Commit OSTree Hash provided")
	}
	if len(update.InventoryHosts) == 0 {
		return nil, errors.New("Inventory Hosts to update required")
	}

	return &update, err
}

// UpdatesMakeRouter adds support for operations on commits
func UpdatesMakeRouter(sub chi.Router) {
	sub.Post("/", UpdatesAdd)
	sub.Get("/", UpdatesGetAll)
	sub.Route("/{updateID}", func(r chi.Router) {
		r.Use(UpdateCtx)
		r.Get("/", UpdatesGetByID)
		r.Put("/", UpdatesUpdate)
	})
}

const updateKey key = 0

// UpdateCtx is a handler for Update requests
func UpdateCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var update models.UpdateTransaction
		account, err := common.GetAccount(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Debugf("UpdateCtx::update: %#v", update)
		if updateID := chi.URLParam(r, "updateID"); updateID != "" {
			id, err := strconv.Atoi(updateID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			result := db.DB.Where("account = ?", account).First(&update, id)
			if result.Error != nil {
				http.Error(w, result.Error.Error(), http.StatusNotFound)
				return
			}
			ctx := context.WithValue(r.Context(), updateKey, &update)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// UpdatesAdd add an object to the database for an account
func UpdatesAdd(w http.ResponseWriter, r *http.Request) {

	update, err := updateFromReadCloser(r.Body)
	log.Debugf("UpdatesAdd::update: %#v", update)
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

	db.DB.Create(&update)

	go RepoBuilderInstance.BuildRepo(update)
}

// UpdatesGetAll update objects from the database for an account
func UpdatesGetAll(w http.ResponseWriter, r *http.Request) {
}

// UpdatesGetByID obtains an update from the database for an account
func UpdatesGetByID(w http.ResponseWriter, r *http.Request) {
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

	incoming, err := updateFromReadCloser(r.Body)
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
	update, ok := ctx.Value(updateKey).(*models.UpdateTransaction)
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

	var updateTransaction models.UpdateTransaction
	db.DB.First(&updateTransaction, ur.ID)
	updateTransaction.Status = models.UpdateStatusCreated
	db.DB.Save(&updateTransaction)

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

	var uploader Uploader
	uploader = &FileUploader{
		BaseDir: path,
	}
	if cfg.BucketName != "" {
		uploader = NewS3Uploader()
	}
	// FIXME: Need to actually do something with the return string for Server

	// NOTE: This relies on the file path being cfg.UpdateTempPath/models.UpdateTransaction.ID
	repoURL, err := uploader.UploadRepo(filepath.Join(path, "repo"), ur.Account)
	if err != nil {
		return nil, err
	}

	var updateTransactionDone models.UpdateTransaction
	db.DB.First(&updateTransactionDone, ur.ID)
	updateTransactionDone.Status = models.UpdateStatusSuccess
	updateTransactionDone.UpdateRepoURL = repoURL
	db.DB.Save(&updateTransactionDone)

	return &updateTransactionDone, nil
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

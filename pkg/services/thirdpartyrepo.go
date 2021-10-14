package services

import (
	"context"

	"github.com/redhatinsights/edge-api/pkg/db"
	"github.com/redhatinsights/edge-api/pkg/models"
	log "github.com/sirupsen/logrus"
)

// TPRepoServiceInterface defines the interface that helps handles
// the business logic of creating Third Party Repository
type TPRepoServiceInterface interface {
	CreateThirdyPartyRepo(tprepo *models.ThirdyPartyRepo, account string) error
}

// NewTPRepoService gives a instance of the main implementation of a TPRepoServiceInterface
func NewTPRepoService(ctx context.Context) TPRepoServiceInterface {
	return &TPRepoService{}
}

// TPRepoService is the main implementation of a TPRepoServiceInterface
type TPRepoService struct {
	ctx context.Context
}

// CreateThirdyPartyRepo creates the ThirdyPartyRepo for an Account on our database
func (s *TPRepoService) CreateThirdyPartyRepo(tprepo *models.ThirdyPartyRepo, account string) error {
	if tprepo.URL != "" && tprepo.Name != "" {
		tprepo = &models.ThirdyPartyRepo{
			Name:        tprepo.Name,
			URL:         tprepo.URL,
			Description: tprepo.Description,
			Account:     account,
		}
		result := db.DB.Create(&tprepo)
		if result.Error != nil {
			return result.Error
		}
		log.Infof("Getting ThirdyPartyRepo info: repo %s, %s", tprepo.URL, tprepo.Name)

	}
	return nil
}

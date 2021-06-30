package common

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

// FilterFunc is a function that takes http request and GORM DB adds a query according to the request
type FilterFunc func(r *http.Request, tx *gorm.DB) *gorm.DB

// ContainFilterHandler handles sub string values
func ContainFilterHandler(name string) FilterFunc {
	sqlQuery := fmt.Sprintf("%s LIKE ?", name)
	return FilterFunc(func(r *http.Request, tx *gorm.DB) *gorm.DB {
		if val := r.URL.Query().Get(name); val != "" {
			tx = tx.Where(sqlQuery, "%"+val+"%")
		}
		return tx
	})
}

// OneOfFilterHandler handles multiple values filters
func OneOfFilterHandler(name string) FilterFunc {
	sqlQuery := fmt.Sprintf("%s IN ?", name)
	return FilterFunc(func(r *http.Request, tx *gorm.DB) *gorm.DB {
		if vals, ok := r.URL.Query()[name]; ok {
			tx = tx.Where(sqlQuery, vals)
		}
		return tx
	})
}

const layoutISO = "2006-01-02"

// CreatedAtFilterHandler handles the "created_at" filter
func CreatedAtFilterHandler() FilterFunc {
	return FilterFunc(func(r *http.Request, tx *gorm.DB) *gorm.DB {
		if val := r.URL.Query().Get("created_at"); val != "" {
			currentDay, err := time.Parse(layoutISO, val)
			if err != nil {
				return tx
			}
			nextDay := currentDay.Add(time.Hour * 24)
			tx = tx.Where("created_at BETWEEN ? AND ?", currentDay.Format(layoutISO), nextDay.Format(layoutISO))
		}
		return tx
	})
}

// SortFilterHandler handles sorting
func SortFilterHandler(defaultSortKey string, defaultOrder string) FilterFunc {
	return FilterFunc(func(r *http.Request, tx *gorm.DB) *gorm.DB {
		sortBy := defaultSortKey
		sortOrder := defaultOrder
		if val := r.URL.Query().Get("sort_by"); val != "" {
			if strings.HasPrefix(val, "-") {
				sortOrder = "DESC"
				sortBy = val[1:]
			} else {
				sortBy = val
			}
		}
		return tx.Order(fmt.Sprintf("%s %s", sortBy, sortOrder))
	})
}

// ComposeFilters composes all the filters into one function
func ComposeFilters(fs ...FilterFunc) FilterFunc {
	return func(r *http.Request, tx *gorm.DB) *gorm.DB {
		for _, f := range fs {
			tx = f(r, tx)
		}
		return tx
	}
}
package core

import (
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func InitializeSqliteDatabase(databasePath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Migrate the schema
	err = db.AutoMigrate(model.GetAllModels()...)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func WriteStaticGtfsFeedToDatabase(feed *model.GtfsStaticFeed, db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, agency := range feed.Agency {
			result := tx.Create(&agency)
			if result.Error != nil {
				return result.Error
			}
		}
		for _, stop := range feed.Stop {
			result := tx.Create(&stop)
			if result.Error != nil {
				return result.Error
			}
		}
		for _, route := range feed.Route {
			result := tx.Create(&route)
			if result.Error != nil {
				return result.Error
			}
		}
		for _, trip := range feed.Trip {
			result := tx.Create(&trip)
			if result.Error != nil {
				return result.Error
			}
		}
		for _, stopTime := range feed.StopTime {
			result := tx.Create(&stopTime)
			if result.Error != nil {
				return result.Error
			}
		}
		for _, calendar := range feed.Calendar {
			result := tx.Create(&calendar)
			if result.Error != nil {
				return result.Error
			}
		}

		return nil
	})
}

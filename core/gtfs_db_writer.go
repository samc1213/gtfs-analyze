package core

import (
	"errors"
	stdlog "log"
	"os"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitializeSqliteDatabase(databasePath string, logLevel log.Level) (*gorm.DB, error) {
	loggerConfig := logger.Config{LogLevel: getGormLogLevel(logLevel)}
	innerLogger := logger.New(stdlog.New(os.Stderr, "", stdlog.LstdFlags), loggerConfig)
	db, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{Logger: innerLogger})
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

func getGormLogLevel(logLevel log.Level) logger.LogLevel {
	switch logLevel {
	case log.Debug:
		return logger.Info
	case log.Info:
		// This isn't a bug - the Info level logs from GORM are very verbose,
		// what we would consider a debug log for gtfs-analyze
		return logger.Warn
	case log.Warning:
		return logger.Warn
	case log.Error:
		return logger.Error
	case log.Silent:
		return logger.Silent
	default:
		return logger.Info
	}
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
		result := tx.Create(&feed.FeedInfo)
		if result.Error != nil {
			return result.Error
		}

		return nil
	})
}

func WriteRealTimePositionUpdateToDatabase(positionUpdates []model.VehiclePosition, db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, positionUpdate := range positionUpdates {
			// Save won't fail if there is a Primary Key conflict
			result := tx.Save(&positionUpdate)
			if result.Error != nil {
				return result.Error
			}
		}

		return nil
	})
}

type LatestRtUpdateTracker struct {
	latestVehiclePositionTimestamp uint64
}

func (tracker *LatestRtUpdateTracker) ShouldProcessMessage(vehiclePositionTimestamp uint64) bool {
	if vehiclePositionTimestamp > tracker.latestVehiclePositionTimestamp {
		tracker.latestVehiclePositionTimestamp = vehiclePositionTimestamp
		return true
	}

	return false
}

func NewUpdateTracker(db *gorm.DB) (*LatestRtUpdateTracker, error) {
	var vehiclePosition model.VehiclePosition
	tx := db.Order("message_timestamp DESC").First(&vehiclePosition)
	if tx.Error != nil && !errors.Is(tx.Error, gorm.ErrRecordNotFound) {
		return nil, tx.Error
	}
	return &LatestRtUpdateTracker{latestVehiclePositionTimestamp: vehiclePosition.MessageTimestamp}, nil
}

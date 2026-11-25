package core

import (
	"context"
	stdlog "log"
	"os"

	"github.com/glebarez/sqlite"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitializeSqliteDatabase(databasePath string, logLevel log.Level) (*gorm.DB, error) {
	loggerConfig := logger.Config{LogLevel: getGormLogLevel(logLevel)}
	innerLogger := logger.New(stdlog.New(os.Stderr, "", stdlog.LstdFlags), loggerConfig)
	// The static and realtime feeds are written from separate goroutines. busy_timeout makes
	// a writer wait for another's lock instead of failing with "database is locked".
	db, err := gorm.Open(sqlite.Open(databasePath+"?_pragma=busy_timeout(30000)"), &gorm.Config{Logger: innerLogger})
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

// insertAll inserts rows one at a time. SQLite is a stopgap store, so this favours
// simplicity over speed (multi-row inserts would need a gorm dialector workaround).
func insertAll[T any](tx *gorm.DB, rows []T) error {
	for i := range rows {
		if err := tx.Create(&rows[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func WriteStaticGtfsFeedToDatabase(ctx context.Context, feed *model.GtfsStaticFeed, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := insertAll(tx, feed.Agency); err != nil {
			return err
		}
		if err := insertAll(tx, feed.Stop); err != nil {
			return err
		}
		if err := insertAll(tx, feed.Route); err != nil {
			return err
		}
		if err := insertAll(tx, feed.Trip); err != nil {
			return err
		}
		if err := insertAll(tx, feed.StopTime); err != nil {
			return err
		}
		if err := insertAll(tx, feed.Calendar); err != nil {
			return err
		}
		if err := insertAll(tx, feed.Shape); err != nil {
			return err
		}
		// FeedInfo is written last: its presence marks the feed as fully stored
		return tx.Create(&feed.FeedInfo).Error
	})
}

// WriteRtChangesToDatabase stores the rows from one GTFS-RT snapshot in a single transaction.
func WriteRtChangesToDatabase(ctx context.Context, changes *RtChanges, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := insertAll(tx, changes.VehiclePositions); err != nil {
			return err
		}
		if err := insertAll(tx, changes.TripUpdates); err != nil {
			return err
		}
		if err := insertAll(tx, changes.StopTimeUpdates); err != nil {
			return err
		}
		// gorm also inserts each alert's ActivePeriods and InformedEntities
		return insertAll(tx, changes.Alerts)
	})
}

package core

import (
	"context"
	stdlog "log"
	"os"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Rows per INSERT statement. Bigger isn't better here: the SQLite driver binds
// parameters in time quadratic in the parameter count, and on RTD's static feed
// 200 rows was faster than both 50 and 500.
const insertBatchSize = 200

func InitializeSqliteDatabase(databasePath string, logLevel log.Level) (*gorm.DB, error) {
	loggerConfig := logger.Config{LogLevel: getGormLogLevel(logLevel), IgnoreRecordNotFoundError: true}
	innerLogger := logger.New(stdlog.New(os.Stderr, "", stdlog.LstdFlags), loggerConfig)
	// WAL lets other processes (e.g. `calculate otp`) read while the store daemon writes.
	// busy_timeout makes a connection wait for a lock held by another process rather than fail.
	// Set via the DSN so they apply to every connection the pool opens.
	dsn := databasePath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)&_pragma=synchronous(NORMAL)"
	db, err := gorm.Open(newSqliteDialector(dsn), &gorm.Config{Logger: innerLogger, CreateBatchSize: insertBatchSize})
	if err != nil {
		return nil, err
	}

	// SQLite allows only one writer at a time. Use a single connection so the static
	// and realtime pollers queue up in-process instead of failing with "database is locked".
	sqlDb, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDb.SetMaxOpenConns(1)

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

// insertAll inserts rows in batches. gorm returns an error for an empty slice, so skip those.
func insertAll[T any](tx *gorm.DB, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(rows, insertBatchSize).Error
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

package core

import (
	"fmt"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
)

func Store(sqliteDbPath string, staticGtfsUrl string, logLevel log.Level) error {
	logger := log.New(logLevel)
	logger.Info("Parsing static GTFS from url: %s", staticGtfsUrl)
	feed, err := ParseStaticGtfsFromUrl(staticGtfsUrl)
	if err != nil {
		return err
	}
	logger.Info("Done parsing static GTFS from url: %s", staticGtfsUrl)

	logger.Info("Initializing SQLite database at path: %s", sqliteDbPath)
	db, err := InitializeSqliteDatabase(sqliteDbPath, logLevel)
	if err != nil {
		return err
	}
	logger.Info("Done initializing SQLite database at path: %s", sqliteDbPath)

	// TODO: wrap the gorm database objects in some interface for better testing
	// and ability to change library?
	feedExists, err := doesFeedAlreadyExist(feed, db)
	if err != nil {
		return err
	}

	if feedExists {
		logger.Warning("Feed already exists in database at %s", sqliteDbPath)
	} else {
		logger.Info("Writing GTFS static feed to database")
		err = WriteStaticGtfsFeedToDatabase(feed, db)
		logger.Info("Done writing GTFS static feed to database")

		if err != nil {
			return err
		}
	}

	return nil
}

func doesFeedAlreadyExist(feed *model.GtfsStaticFeed, db *gorm.DB) (bool, error) {
	if len(feed.FeedInfo) != 1 {
		return true, fmt.Errorf("expected only one feedInfo. Got %d", len(feed.FeedInfo))
	}
	var feedInfo model.FeedInfo

	result := db.First(&feedInfo, "version = ?", feed.FeedInfo[0].Version)
	return result.RowsAffected > 0, result.Error
}

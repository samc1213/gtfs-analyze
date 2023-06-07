package core

import (
	"time"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
)

func Store(sqliteDbPath string, staticGtfsUrl string, vehiclePositionUrl string,
	staticPollIntervalMins uint, rtPollIntervalSecs uint, logLevel log.Level) (*chan struct{}, error) {
	logger := log.New(logLevel)

	db, err := initializeSqliteDb(logger, sqliteDbPath, logLevel)
	if err != nil {
		return nil, err
	}

	quitPoll := make(chan struct{})
	polling := false

	if staticGtfsUrl != "" {
		if staticPollIntervalMins != 0 {
			polling = true
			err = storeStaticGtfs(logger, staticGtfsUrl, db, sqliteDbPath)
			if err != nil {
				return &quitPoll, err
			}
			staticTicker := time.NewTicker(time.Duration(staticPollIntervalMins) * time.Minute)
			go func() {
				for {
					select {
					case <-staticTicker.C:
						err = storeStaticGtfs(logger, staticGtfsUrl, db, sqliteDbPath)
						if err != nil {
							staticTicker.Stop()
							return
						}
					case <-quitPoll:
						staticTicker.Stop()
						return
					}
				}
			}()
		} else {
			err = storeStaticGtfs(logger, staticGtfsUrl, db, sqliteDbPath)
			if err != nil {
				return &quitPoll, err
			}
		}
	}

	if vehiclePositionUrl != "" {
		if rtPollIntervalSecs != 0 {
			polling = true
			err = storeRtGtfs(logger, vehiclePositionUrl, db)
			if err != nil {
				return &quitPoll, err
			}
			rtTicker := time.NewTicker(time.Duration(rtPollIntervalSecs) * time.Second)
			go func() {
				for {
					select {
					case <-rtTicker.C:
						err = storeRtGtfs(logger, vehiclePositionUrl, db)
						if err != nil {
							rtTicker.Stop()
							return
						}
					case <-quitPoll:
						rtTicker.Stop()
						return
					}
				}
			}()
		} else {
			err = storeRtGtfs(logger, vehiclePositionUrl, db)
			if err != nil {
				return &quitPoll, err
			}
		}
	}

	if polling {
		for {
			time.Sleep(50 * time.Millisecond)
		}
	}

	return &quitPoll, err
}

func storeStaticGtfs(logger log.Interface, staticGtfsUrl string, db *gorm.DB, sqliteDbPath string) error {
	feed, err := parseStaticGtfsFromUrl(logger, staticGtfsUrl)
	if err != nil {
		return err
	}

	err = writeStaticGtfsToDbIfNeeded(feed, db, logger, sqliteDbPath)
	if err != nil {
		return err
	}
	return nil
}

func storeRtGtfs(logger log.Interface, vehiclePositionUrl string, db *gorm.DB) error {
	updateTracker, err := NewUpdateTracker(db)
	if err != nil {
		return err
	}
	logger.Info("Fetching GTFS-RT data")
	vehiclePositions, err := ParseRtGtfsFromUrl(vehiclePositionUrl)
	if err != nil {
		return err
	}
	logger.Info("Done fetching GTFS-RT data")
	if len(vehiclePositions) > 0 && updateTracker.ShouldProcessMessage(vehiclePositions[0].MessageTimestamp) {
		logger.Info("Writing GTFS-RT data to database")
		WriteRealTimePositionUpdateToDatabase(vehiclePositions, db)
		if err != nil {
			return err
		}
		logger.Info("Done writing GTFS-RT data to database")
	} else {
		logger.Info("Duplicate RT message detected, will not process")
	}
	return nil
}

// TODO: wrap the gorm database objects in some interface for better testing
// and ability to change library?
func writeStaticGtfsToDbIfNeeded(feed *model.GtfsStaticFeed, db *gorm.DB, logger log.Interface, sqliteDbPath string) error {
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

func parseStaticGtfsFromUrl(logger log.Interface, staticGtfsUrl string) (*model.GtfsStaticFeed, error) {
	logger.Info("Parsing static GTFS from url: %s", staticGtfsUrl)
	feed, err := ParseStaticGtfsFromUrl(staticGtfsUrl)
	if err != nil {
		return nil, err
	}
	logger.Info("Done parsing static GTFS from url: %s", staticGtfsUrl)
	return feed, nil
}

func initializeSqliteDb(logger log.Interface, sqliteDbPath string, logLevel log.Level) (*gorm.DB, error) {
	logger.Info("Initializing SQLite database at path: %s", sqliteDbPath)
	db, err := InitializeSqliteDatabase(sqliteDbPath, logLevel)
	if err != nil {
		return nil, err
	}
	logger.Info("Done initializing SQLite database at path: %s", sqliteDbPath)
	return db, nil
}

func doesFeedAlreadyExist(feed *model.GtfsStaticFeed, db *gorm.DB) (bool, error) {
	var count int64
	result := db.Model(&model.FeedInfo{}).Where("version = ?", feed.FeedInfo.Version).Count(&count)
	return count > 0, result.Error
}

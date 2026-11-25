package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
)

type StoreConfig struct {
	DbPath             string
	StaticUrl          string
	VehiclePositionUrl string
	TripUpdatesUrl     string
	AlertsUrl          string
	// Zero means fetch once and exit instead of polling
	StaticPollInterval time.Duration
	// Zero means fetch once and exit instead of polling
	RtPollInterval time.Duration
	LogLevel       log.Level
}

// A feed to fetch and store, either once or on an interval
type storeJob struct {
	name     string
	interval time.Duration
	run      func(ctx context.Context) error
}

// Store fetches the configured feeds and writes them to the SQLite database.
//
// Feeds with a non-zero poll interval are polled until ctx is cancelled. A failed
// poll is logged and retried on the next interval; it never stops the daemon.
// Feeds with a zero poll interval are fetched once, and their errors are returned.
// Store returns once ctx is cancelled and in-flight work has stopped, or, if nothing
// polls, once every feed has been fetched.
func Store(ctx context.Context, config StoreConfig) error {
	logger := log.New(config.LogLevel)

	if config.StaticUrl == "" && config.VehiclePositionUrl == "" && config.TripUpdatesUrl == "" && config.AlertsUrl == "" {
		return errors.New("nothing to store: provide at least one of --static-url, --vehicle-pos-url, --trip-updates-url or --alerts-url")
	}

	logger.Info("Initializing SQLite database at path: %s", config.DbPath)
	db, err := InitializeSqliteDatabase(config.DbPath, config.LogLevel)
	if err != nil {
		return err
	}
	defer closeDb(db, logger)

	jobs := buildStoreJobs(config, logger, db)

	var wg sync.WaitGroup
	var oneShotErrsLock sync.Mutex
	var oneShotErrs []error
	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			if job.interval == 0 {
				if err := job.run(ctx); err != nil {
					oneShotErrsLock.Lock()
					oneShotErrs = append(oneShotErrs, fmt.Errorf("%s: %w", job.name, err))
					oneShotErrsLock.Unlock()
				}
				return
			}
			poll(ctx, job, logger)
		}()
	}
	wg.Wait()

	if len(oneShotErrs) > 0 {
		for _, err := range oneShotErrs[1:] {
			logger.Error("%s", err)
		}
		return oneShotErrs[0]
	}
	logger.Info("Stopped storing feeds")
	return nil
}

func buildStoreJobs(config StoreConfig, logger log.Interface, db *gorm.DB) []storeJob {
	var jobs []storeJob

	if config.StaticUrl != "" {
		// The static feed is tens of MB, so allow it far longer than a realtime fetch
		client := &http.Client{Timeout: 10 * time.Minute}
		jobs = append(jobs, storeJob{
			name:     "static GTFS",
			interval: config.StaticPollInterval,
			run: func(ctx context.Context) error {
				return storeStaticGtfs(ctx, logger, client, config.StaticUrl, db)
			},
		})
	}

	rtClient := &http.Client{Timeout: 30 * time.Second}
	rtFeeds := []struct{ name, url string }{
		{"vehicle positions", config.VehiclePositionUrl},
		{"trip updates", config.TripUpdatesUrl},
		{"alerts", config.AlertsUrl},
	}
	for _, feed := range rtFeeds {
		if feed.url == "" {
			continue
		}
		name, url := feed.name, feed.url
		tracker := NewRtFeedTracker()
		jobs = append(jobs, storeJob{
			name:     name,
			interval: config.RtPollInterval,
			run: func(ctx context.Context) error {
				return storeRtGtfs(ctx, logger, rtClient, name, url, tracker, db)
			},
		})
	}

	return jobs
}

// poll runs job immediately and then every job.interval until ctx is cancelled.
// Runs never overlap: if a run takes longer than the interval, the missed ticks are skipped.
func poll(ctx context.Context, job storeJob, logger log.Interface) {
	logger.Info("Polling %s every %s", job.name, job.interval)
	ticker := time.NewTicker(job.interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		err := job.run(ctx)
		if ctx.Err() != nil {
			// Shutting down; an error here is just the cancelled request or transaction
			return
		}
		if err != nil {
			consecutiveFailures++
			logger.Error("Storing %s failed (%d in a row), will retry in %s: %s", job.name, consecutiveFailures, job.interval, err)
		} else if consecutiveFailures > 0 {
			logger.Warning("Storing %s recovered after %d failed attempts", job.name, consecutiveFailures)
			consecutiveFailures = 0
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func closeDb(db *gorm.DB, logger log.Interface) {
	sqlDb, err := db.DB()
	if err == nil {
		err = sqlDb.Close()
	}
	if err != nil {
		logger.Error("Closing database: %s", err)
	}
}

func storeStaticGtfs(ctx context.Context, logger log.Interface, client *http.Client, staticGtfsUrl string, db *gorm.DB) error {
	logger.Info("Downloading and parsing static GTFS from %s", staticGtfsUrl)
	feed, err := ParseStaticGtfsFromUrl(ctx, client, staticGtfsUrl)
	if err != nil {
		return err
	}

	feedExists, err := doesFeedAlreadyExist(ctx, feed, db)
	if err != nil {
		return err
	}
	if feedExists {
		logger.Info("Static GTFS version %s is already stored", feed.FeedInfo.Version)
		return nil
	}

	logger.Info("Writing static GTFS version %s to database", feed.FeedInfo.Version)
	start := time.Now()
	if err := WriteStaticGtfsFeedToDatabase(ctx, feed, db); err != nil {
		return err
	}
	logger.Info("Stored static GTFS version %s (%d trips, %d stop times) in %s",
		feed.FeedInfo.Version, len(feed.Trip), len(feed.StopTime), time.Since(start).Round(time.Millisecond))
	return nil
}

func storeRtGtfs(ctx context.Context, logger log.Interface, client *http.Client, name string, url string, tracker *RtFeedTracker, db *gorm.DB) error {
	message, err := FetchRtFeed(ctx, client, url)
	if err != nil {
		return err
	}

	changes := tracker.Diff(message)
	if changes == nil {
		logger.Debug("%s: snapshot %d already stored", name, message.GetHeader().GetTimestamp())
		return nil
	}

	const maxEntityErrorsToLog = 5
	for i, entityErr := range changes.EntityErrors {
		if i == maxEntityErrorsToLog {
			logger.Warning("%s: skipped %d more invalid entities", name, len(changes.EntityErrors)-maxEntityErrorsToLog)
			break
		}
		logger.Warning("%s: skipped invalid entity: %s", name, entityErr)
	}

	if changes.RowCount() > 0 {
		if err := WriteRtChangesToDatabase(ctx, changes, db); err != nil {
			return err
		}
	}
	tracker.Commit(changes)

	logger.Info("%s: snapshot %d of %d entities stored %d vehicle positions, %d trip updates, %d stop time updates, %d alerts",
		name, changes.MessageTimestamp, len(message.Entity), len(changes.VehiclePositions), len(changes.TripUpdates), len(changes.StopTimeUpdates), len(changes.Alerts))
	return nil
}

func doesFeedAlreadyExist(ctx context.Context, feed *model.GtfsStaticFeed, db *gorm.DB) (bool, error) {
	var count int64
	result := db.WithContext(ctx).Model(&model.FeedInfo{}).Where("version = ?", feed.FeedInfo.Version).Count(&count)
	return count > 0, result.Error
}

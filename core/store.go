package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	configpkg "github.com/samc1213/gtfs-analyze/config"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
)

type StoreConfig struct {
	DbPath   string
	Agencies []configpkg.Agency
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

// Store fetches every feed of the configured agencies and writes them to the SQLite database.
//
// Feeds with a non-zero poll interval are polled until ctx is cancelled. A failed
// poll is logged and retried on the next interval; it never stops the daemon.
// Feeds with a zero poll interval are fetched once, and their errors are returned.
// Store returns once ctx is cancelled and in-flight work has stopped, or, if nothing
// polls, once every feed has been fetched.
func Store(ctx context.Context, config StoreConfig) error {
	logger := log.New(config.LogLevel)

	if len(config.Agencies) == 0 {
		return errors.New("nothing to store: no agencies selected")
	}
	// The SQLite schema has no agency column, so one database holds one agency
	if len(config.Agencies) > 1 {
		return fmt.Errorf("a SQLite database stores one agency, but %d are selected; choose one with --agency", len(config.Agencies))
	}

	target := &storeTarget{}
	jobs, err := buildStoreJobs(config, logger, target)
	if err != nil {
		return err
	}

	logger.Info("Initializing SQLite database at path: %s", config.DbPath)
	db, err := InitializeSqliteDatabase(config.DbPath, config.LogLevel)
	if err != nil {
		return err
	}
	defer closeDb(db, logger)
	target.db = db

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

// storeTarget is where fetched feeds are written; it is set before any job runs
type storeTarget struct {
	db *gorm.DB
}

func buildStoreJobs(config StoreConfig, logger log.Interface, target *storeTarget) ([]storeJob, error) {
	var jobs []storeJob
	for _, agency := range config.Agencies {
		for _, feed := range agency.Feeds {
			url, headers, err := feed.Request()
			if err != nil {
				return nil, fmt.Errorf("agency %q: %w", agency.Id, err)
			}
			name := agency.Id + "/" + feed.Id
			switch feed.Kind {
			case configpkg.Static:
				// The static feed is tens of MB, so allow it far longer than a realtime fetch
				client := newFeedClient(10*time.Minute, headers)
				jobs = append(jobs, storeJob{
					name:     name,
					interval: config.StaticPollInterval,
					run: func(ctx context.Context) error {
						return storeStaticGtfs(ctx, logger, client, name, url, target.db)
					},
				})
			case configpkg.Realtime:
				client := newFeedClient(30*time.Second, headers)
				tracker := NewRtFeedTracker()
				jobs = append(jobs, storeJob{
					name:     name,
					interval: config.RtPollInterval,
					run: func(ctx context.Context) error {
						return storeRtGtfs(ctx, logger, client, name, url, tracker, target.db)
					},
				})
			default:
				return nil, fmt.Errorf("%s: unknown feed kind %q", name, feed.Kind)
			}
		}
	}
	return jobs, nil
}

// newFeedClient returns an HTTP client that sends the feed's configured headers (e.g. API keys) with every request
func newFeedClient(timeout time.Duration, headers http.Header) *http.Client {
	client := &http.Client{Timeout: timeout}
	if len(headers) > 0 {
		client.Transport = headerTransport{headers: headers, base: http.DefaultTransport}
	}
	return client
}

type headerTransport struct {
	headers http.Header
	base    http.RoundTripper
}

func (transport headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for name, values := range transport.headers {
		request.Header[name] = values
	}
	return transport.base.RoundTrip(request)
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

func storeStaticGtfs(ctx context.Context, logger log.Interface, client *http.Client, name string, staticGtfsUrl string, db *gorm.DB) error {
	logger.Info("%s: downloading and parsing static GTFS from %s", name, staticGtfsUrl)
	feed, err := ParseStaticGtfsFromUrl(ctx, client, staticGtfsUrl)
	if err != nil {
		return err
	}

	feedExists, err := doesFeedAlreadyExist(ctx, feed, db)
	if err != nil {
		return err
	}
	if feedExists {
		logger.Info("%s: static GTFS version %s is already stored", name, feed.FeedInfo.Version)
		return nil
	}

	logger.Info("%s: writing static GTFS version %s to database", name, feed.FeedInfo.Version)
	start := time.Now()
	if err := WriteStaticGtfsFeedToDatabase(ctx, feed, db); err != nil {
		return err
	}
	logger.Info("%s: stored static GTFS version %s (%d trips, %d stop times) in %s",
		name, feed.FeedInfo.Version, len(feed.Trip), len(feed.StopTime), time.Since(start).Round(time.Millisecond))
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

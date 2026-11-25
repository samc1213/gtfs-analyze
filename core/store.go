package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	configpkg "github.com/samc1213/gtfs-analyze/config"
	"github.com/samc1213/gtfs-analyze/gtfs_realtime"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
)

type StoreConfig struct {
	// SQLite database for parsed data. Optional if ArchiveDir is set.
	DbPath string
	// Directory for the raw archive of every downloaded feed. Optional if DbPath is set.
	ArchiveDir string
	Agencies   []configpkg.Agency
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

// Store fetches every feed of the configured agencies, archives the raw downloads and
// writes the parsed data to the SQLite database.
//
// The raw archive is written first: it is the source of truth the database can be
// rebuilt from, so a snapshot is never in the database without being archived.
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
	if config.DbPath == "" && config.ArchiveDir == "" {
		return errors.New("nowhere to store: provide --db-path, --archive-dir, or both")
	}
	// The SQLite schema has no agency column, so one database holds one agency.
	// The archive is laid out by agency, so it can hold any number.
	if config.DbPath != "" && len(config.Agencies) > 1 {
		return fmt.Errorf("a SQLite database stores one agency, but %d are selected; choose one with --agency", len(config.Agencies))
	}

	targets := &storeTargets{}
	jobs, err := buildStoreJobs(config, logger, targets)
	if err != nil {
		return err
	}

	if config.ArchiveDir != "" {
		logger.Info("Archiving raw feeds to directory: %s", config.ArchiveDir)
		targets.archive = &DirArchive{Root: config.ArchiveDir}
	}
	if config.DbPath != "" {
		logger.Info("Initializing SQLite database at path: %s", config.DbPath)
		db, err := InitializeSqliteDatabase(config.DbPath, config.LogLevel)
		if err != nil {
			return err
		}
		defer closeDb(db, logger)
		targets.db = db
	}

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

// Where fetched feeds are written. Either may be nil, but not both. Set before any job runs.
type storeTargets struct {
	db      *gorm.DB
	archive Archive
}

func buildStoreJobs(config StoreConfig, logger log.Interface, targets *storeTargets) ([]storeJob, error) {
	var jobs []storeJob
	for _, agency := range config.Agencies {
		for _, feed := range agency.Feeds {
			url, headers, err := feed.Request()
			if err != nil {
				return nil, fmt.Errorf("agency %q: %w", agency.Id, err)
			}
			// <agency>/<feed>: used in logs and as the feed's folder in the archive
			name := agency.Id + "/" + feed.Id
			switch feed.Kind {
			case configpkg.Static:
				// The static feed is tens of MB, so allow it far longer than a realtime fetch
				client := newFeedClient(10*time.Minute, headers)
				jobs = append(jobs, storeJob{
					name:     name,
					interval: config.StaticPollInterval,
					run: func(ctx context.Context) error {
						return storeStaticGtfs(ctx, logger, client, name, url, targets)
					},
				})
			case configpkg.Realtime:
				client := newFeedClient(30*time.Second, headers)
				rt := &rtFeed{name: name, url: url, tracker: NewRtFeedTracker()}
				jobs = append(jobs, storeJob{
					name:     name,
					interval: config.RtPollInterval,
					run: func(ctx context.Context) error {
						return storeRtGtfs(ctx, logger, client, rt, targets)
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

func storeStaticGtfs(ctx context.Context, logger log.Interface, client *http.Client, name string, staticGtfsUrl string, targets *storeTargets) error {
	logger.Info("%s: downloading static GTFS from %s", name, staticGtfsUrl)
	download, err := DownloadStaticGtfs(ctx, client, staticGtfsUrl)
	if err != nil {
		return err
	}
	defer download.Remove()

	feed, err := ParseStaticGtfsFromPath(download.Path)
	if err != nil {
		if targets.archive != nil {
			// Keep the unparseable zip for debugging, like invalid realtime snapshots
			key := fmt.Sprintf("%s/invalid_%s.zip", name, download.Sha256[:16])
			if archiveErr := archiveFile(ctx, targets.archive, key, download.Path); archiveErr != nil {
				logger.Error("%s: archiving invalid static GTFS: %s", name, archiveErr)
			}
		}
		return fmt.Errorf("parsing static GTFS: %w", err)
	}
	version := feed.FeedInfo.Version

	if targets.archive != nil {
		key := staticArchiveKey(name, version, download.Sha256)
		exists, err := targets.archive.Exists(ctx, key)
		if err != nil {
			return err
		}
		if !exists {
			if err := archiveFile(ctx, targets.archive, key, download.Path); err != nil {
				return err
			}
			logger.Info("%s: archived static GTFS version %s as %s", name, version, key)
		}
	}

	if targets.db == nil {
		return nil
	}
	feedExists, err := doesFeedAlreadyExist(ctx, feed, targets.db)
	if err != nil {
		return err
	}
	if feedExists {
		logger.Info("%s: static GTFS version %s is already stored", name, version)
		return nil
	}

	logger.Info("%s: writing static GTFS version %s to database", name, version)
	start := time.Now()
	if err := WriteStaticGtfsFeedToDatabase(ctx, feed, targets.db); err != nil {
		return err
	}
	logger.Info("%s: stored static GTFS version %s (%d trips, %d stop times) in %s",
		name, version, len(feed.Trip), len(feed.StopTime), time.Since(start).Round(time.Millisecond))
	return nil
}

func archiveFile(ctx context.Context, archive Archive, key string, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return archive.Put(ctx, key, file)
}

// A realtime feed and what has already been stored from it
type rtFeed struct {
	name    string // <agency>/<feed>, also its folder in the archive
	url     string
	tracker *RtFeedTracker

	// The last archived snapshot, so an unchanged feed isn't archived again on every poll
	lastArchivedTimestamp uint64
	lastArchivedHash      [sha256.Size]byte
}

func storeRtGtfs(ctx context.Context, logger log.Interface, client *http.Client, feed *rtFeed, targets *storeTargets) error {
	fetchedAt := time.Now()
	body, err := FetchRtFeedBytes(ctx, client, feed.url)
	if err != nil {
		return err
	}
	message, parseErr := ParseRtFeed(body)

	if targets.archive != nil {
		if err := archiveRtSnapshot(ctx, logger, targets.archive, feed, body, message, fetchedAt); err != nil {
			return err
		}
	}
	if parseErr != nil {
		return parseErr
	}
	if targets.db == nil {
		return nil
	}

	changes := feed.tracker.Diff(message)
	if changes == nil {
		logger.Debug("%s: snapshot %d already stored", feed.name, message.GetHeader().GetTimestamp())
		return nil
	}

	const maxEntityErrorsToLog = 5
	for i, entityErr := range changes.EntityErrors {
		if i == maxEntityErrorsToLog {
			logger.Warning("%s: skipped %d more invalid entities", feed.name, len(changes.EntityErrors)-maxEntityErrorsToLog)
			break
		}
		logger.Warning("%s: skipped invalid entity: %s", feed.name, entityErr)
	}

	if changes.RowCount() > 0 {
		if err := WriteRtChangesToDatabase(ctx, changes, targets.db); err != nil {
			return err
		}
	}
	feed.tracker.Commit(changes)

	logger.Info("%s: snapshot %d of %d entities stored %d vehicle positions, %d trip updates, %d stop time updates, %d alerts",
		feed.name, changes.MessageTimestamp, len(message.Entity), len(changes.VehiclePositions), len(changes.TripUpdates), len(changes.StopTimeUpdates), len(changes.Alerts))
	return nil
}

// archiveRtSnapshot gzips and archives a raw realtime download, unless it is the same
// snapshot as the last one archived. message is nil if body couldn't be parsed.
func archiveRtSnapshot(ctx context.Context, logger log.Interface, archive Archive, feed *rtFeed, body []byte, message *gtfs_realtime.FeedMessage, fetchedAt time.Time) error {
	hash := sha256.Sum256(body)
	messageTimestamp := message.GetHeader().GetTimestamp()
	if hash == feed.lastArchivedHash || (messageTimestamp != 0 && messageTimestamp <= feed.lastArchivedTimestamp) {
		return nil
	}

	// Name the file by the feed's own timestamp when there is one, else by when we fetched it
	snapshotTime := fetchedAt
	if messageTimestamp != 0 {
		snapshotTime = time.Unix(int64(messageTimestamp), 0)
	}
	key := rtArchiveKey(feed.name, snapshotTime, message != nil)

	compressed, err := gzipBytes(body)
	if err != nil {
		return err
	}
	if err := archive.Put(ctx, key, compressed); err != nil {
		return err
	}
	logger.Debug("%s: archived %s (%d bytes)", feed.name, key, compressed.Len())

	feed.lastArchivedHash = hash
	if messageTimestamp > feed.lastArchivedTimestamp {
		feed.lastArchivedTimestamp = messageTimestamp
	}
	return nil
}

func doesFeedAlreadyExist(ctx context.Context, feed *model.GtfsStaticFeed, db *gorm.DB) (bool, error) {
	var count int64
	result := db.WithContext(ctx).Model(&model.FeedInfo{}).Where("version = ?", feed.FeedInfo.Version).Count(&count)
	return count > 0, result.Error
}

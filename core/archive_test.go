package core

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/samc1213/gtfs-analyze/config"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/samc1213/gtfs-analyze/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// archivedFiles lists every file under root as slash-separated relative paths
func archivedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(root, func(p string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return err
	}))
	sort.Strings(files)
	return files
}

func gunzipFile(t *testing.T, p string) []byte {
	t.Helper()
	f, err := os.Open(p)
	require.NoError(t, err)
	defer f.Close()
	reader, err := gzip.NewReader(f)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return data
}

func TestDirArchivePutAndExists(t *testing.T) {
	archive := &DirArchive{Root: t.TempDir()}
	ctx := context.Background()

	exists, err := archive.Exists(ctx, "a/b/c.txt")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, archive.Put(ctx, "a/b/c.txt", strings.NewReader("hello")))
	exists, err = archive.Exists(ctx, "a/b/c.txt")
	require.NoError(t, err)
	assert.True(t, exists)

	// Overwrites, and leaves no temporary files behind
	require.NoError(t, archive.Put(ctx, "a/b/c.txt", strings.NewReader("hello again")))
	assert.Equal(t, []string{"a/b/c.txt"}, archivedFiles(t, archive.Root))
	contents, err := os.ReadFile(filepath.Join(archive.Root, "a", "b", "c.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello again", string(contents))
}

func TestDirArchivePutCancelledLeavesNothing(t *testing.T) {
	archive := &DirArchive{Root: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Error(t, archive.Put(ctx, "x/y.bin", strings.NewReader("data")))
	assert.Empty(t, archivedFiles(t, archive.Root))
}

func TestArchiveRtSnapshotSkipsDuplicates(t *testing.T) {
	archive := &DirArchive{Root: t.TempDir()}
	ctx := context.Background()
	logger := log.New(log.Silent)
	feed := &rtFeed{name: "rtd/vehicle_positions"}
	fetchedAt := time.Date(2026, 11, 24, 23, 0, 0, 0, time.UTC)

	body, err := os.ReadFile(path.Join(getTestFilesPath(), "VehiclePosition_RTD_2023_05_23.pb"))
	require.NoError(t, err)
	message, err := ParseRtFeed(body)
	require.NoError(t, err)

	// First snapshot is archived under the feed's own timestamp (1684864397 = 2023-05-23T17:53:17Z)
	require.NoError(t, archiveRtSnapshot(ctx, logger, archive, feed, body, message, fetchedAt))
	// The same snapshot fetched again is skipped
	require.NoError(t, archiveRtSnapshot(ctx, logger, archive, feed, body, message, fetchedAt.Add(30*time.Second)))
	files := archivedFiles(t, archive.Root)
	require.Equal(t, []string{"rtd/vehicle_positions/2023/05/23/20230523T175317Z.pb.gz"}, files)
	assert.Equal(t, body, gunzipFile(t, filepath.Join(archive.Root, files[0])))

	// A newer snapshot is archived
	next := nextSnapshot(message, 30)
	nextBody, err := proto.Marshal(next)
	require.NoError(t, err)
	require.NoError(t, archiveRtSnapshot(ctx, logger, archive, feed, nextBody, next, fetchedAt))
	assert.Len(t, archivedFiles(t, archive.Root), 2)

	// An unparseable body is kept for debugging, named by fetch time, and not repeated
	garbage := []byte("<html>503 Service Unavailable</html>")
	require.NoError(t, archiveRtSnapshot(ctx, logger, archive, feed, garbage, nil, fetchedAt))
	require.NoError(t, archiveRtSnapshot(ctx, logger, archive, feed, garbage, nil, fetchedAt.Add(30*time.Second)))
	files = archivedFiles(t, archive.Root)
	assert.Len(t, files, 3)
	assert.Contains(t, files, "rtd/vehicle_positions/2026/11/24/20261124T230000Z.invalid.gz")
}

func testAgency(id string, feeds ...config.Feed) config.Agency {
	return config.Agency{Id: id, Name: id, Feeds: feeds}
}

// An archive-only store (no database) can collect several agencies, each in its own folder
func TestStoreArchiveOnly(t *testing.T) {
	body, err := os.ReadFile(path.Join(getTestFilesPath(), "Alerts_RTD_2026_11_24.pb"))
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer server.Close()

	archiveDir := t.TempDir()
	err = Store(context.Background(), StoreConfig{
		ArchiveDir: archiveDir,
		Agencies: []config.Agency{
			testAgency("rtd", config.Feed{Id: "alerts", Kind: config.Realtime, Url: server.URL}),
			testAgency("other", config.Feed{Id: "alerts", Kind: config.Realtime, Url: server.URL}),
		},
		LogLevel: log.Silent,
	})
	require.NoError(t, err)

	files := archivedFiles(t, archiveDir)
	require.Len(t, files, 2)
	sort.Strings(files)
	assert.True(t, strings.HasPrefix(files[0], "other/alerts/"), files[0])
	assert.True(t, strings.HasPrefix(files[1], "rtd/alerts/"), files[1])
	assert.Equal(t, body, gunzipFile(t, filepath.Join(archiveDir, files[1])))
}

func TestStoreRequiresATarget(t *testing.T) {
	agencies := []config.Agency{testAgency("rtd", config.Feed{Id: "alerts", Kind: config.Realtime, Url: "http://example.invalid"})}
	err := Store(context.Background(), StoreConfig{Agencies: agencies, LogLevel: log.Silent})
	assert.ErrorContains(t, err, "nowhere to store")
}

func makeStaticZip(t *testing.T, feedVersion string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{
		"agency.txt":    "agency_id,agency_name,agency_url,agency_timezone\nRTD,Regional Transportation District,https://www.rtd-denver.com,America/Denver\n",
		"feed_info.txt": "feed_publisher_name,feed_publisher_url,feed_lang,feed_start_date,feed_end_date,feed_version\nRTD,https://www.rtd-denver.com,en,20260101,20261231," + feedVersion + "\n",
	}
	for name, contents := range files {
		f, err := writer.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(contents))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestStoreStaticArchivesEachVersionOnce(t *testing.T) {
	zipBytes := makeStaticZip(t, "v1/2026")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(zipBytes) }))
	defer server.Close()

	archiveDir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	storeConfig := StoreConfig{
		ArchiveDir: archiveDir,
		DbPath:     dbPath,
		Agencies:   []config.Agency{testAgency("rtd", config.Feed{Id: "gtfs", Kind: config.Static, Url: server.URL})},
		LogLevel:   log.Silent,
	}
	require.NoError(t, Store(context.Background(), storeConfig))
	require.NoError(t, Store(context.Background(), storeConfig))

	files := archivedFiles(t, archiveDir)
	require.Len(t, files, 1)
	// The version is made safe for use in a path
	assert.Regexp(t, `^rtd/gtfs/v1_2026_[0-9a-f]{16}\.zip$`, files[0])
	archived, err := os.ReadFile(filepath.Join(archiveDir, files[0]))
	require.NoError(t, err)
	assert.Equal(t, zipBytes, archived)

	// The database got the feed too
	db, err := InitializeSqliteDatabase(dbPath, log.Silent)
	require.NoError(t, err)
	defer closeDb(db, log.New(log.Silent))
	var count int64
	require.NoError(t, db.Model(&model.FeedInfo{}).Where("version = ?", "v1/2026").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

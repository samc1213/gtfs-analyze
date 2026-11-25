package core

import (
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func getTestFilesPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return path.Join(path.Dir(thisFile), "test_files")
}

// This test takes nearly 30 seconds to run - TODO: figure out what's slow in parsing and improve parsing performance if possible
func TestParseRtdStatic(t *testing.T) {
	staticFeed, err := ParseStaticGtfsFromPath(path.Join(getTestFilesPath(), "google_transit_rtd_2023_05_12.zip"))
	assert.NoError(t, err)
	assert.Equal(t, 1, len(staticFeed.Agency))
	assert.Equal(t, "Regional Transportation District", staticFeed.Agency[0].Name)
	assert.Equal(t, 19, len(staticFeed.Calendar))
	// No feed_info.txt in this feed, so the version is a hash of its files (including shapes.txt)
	assert.Equal(t, "b1e1d2cc3b5fe6b84fb20d75d7f4beb9", staticFeed.FeedInfo.Version)
	assert.NotEmpty(t, staticFeed.Shape)
	assert.Equal(t, staticFeed.FeedInfo.Version, staticFeed.Shape[0].Version)
}

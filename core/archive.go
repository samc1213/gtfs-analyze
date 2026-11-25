package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Archive stores raw feed downloads, the source of truth that the database can be
// rebuilt from. Keys are slash-separated paths such as
// "rtd/trip_updates/2026/11/24/20261124T233005Z.pb.gz". Implementations must make Put
// atomic: a reader never sees a partially written object.
//
// DirArchive stores objects on the local filesystem; an object-storage (S3, R2, GCS)
// implementation only needs these two methods.
type Archive interface {
	Put(ctx context.Context, key string, contents io.Reader) error
	Exists(ctx context.Context, key string) (bool, error)
}

// DirArchive is an Archive backed by a local directory
type DirArchive struct {
	Root string
}

func (archive *DirArchive) path(key string) string {
	return filepath.Join(archive.Root, filepath.FromSlash(key))
}

func (archive *DirArchive) Put(ctx context.Context, key string, contents io.Reader) error {
	finalPath := archive.path(key)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	// Write to a temporary file in the same directory, then rename into place, so a
	// crash or shutdown mid-write never leaves a truncated file at the final path
	tempFile, err := os.CreateTemp(filepath.Dir(finalPath), ".partial-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // no-op once renamed

	if _, err := io.Copy(tempFile, readerWithContext{ctx, contents}); err != nil {
		tempFile.Close()
		return fmt.Errorf("archiving %s: %w", key, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("archiving %s: %w", key, err)
	}
	return os.Rename(tempPath, finalPath)
}

func (archive *DirArchive) Exists(ctx context.Context, key string) (bool, error) {
	_, err := os.Stat(archive.path(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// readerWithContext stops a long copy (e.g. a large static zip) when ctx is cancelled
type readerWithContext struct {
	ctx    context.Context
	reader io.Reader
}

func (r readerWithContext) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// rtArchiveKey names a realtime snapshot by feed (<agency>/<feed>) and UTC time.
// Snapshots that couldn't be parsed are kept (to debug feed problems) under a different extension.
func rtArchiveKey(feedKey string, snapshotTime time.Time, valid bool) string {
	snapshotTime = snapshotTime.UTC()
	extension := ".pb.gz"
	if !valid {
		extension = ".invalid.gz"
	}
	return fmt.Sprintf("%s/%s/%s%s", feedKey, snapshotTime.Format("2006/01/02"), snapshotTime.Format("20060102T150405Z"), extension)
}

var unsafeKeyCharacters = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// staticArchiveKey names a static feed zip by feed (<agency>/<feed>), version and content hash.
// Agencies don't always bump feed_version when the contents change, so the hash keeps those apart.
func staticArchiveKey(feedKey string, version string, sha256Hex string) string {
	safeVersion := unsafeKeyCharacters.ReplaceAllString(version, "_")
	if len(safeVersion) > 100 {
		safeVersion = safeVersion[:100]
	}
	return fmt.Sprintf("%s/%s_%s.zip", feedKey, safeVersion, sha256Hex[:16])
}

func gzipBytes(data []byte) (*bytes.Buffer, error) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return &buffer, nil
}

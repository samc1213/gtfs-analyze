package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samc1213/gtfs-analyze/csv_parse"
	"github.com/samc1213/gtfs-analyze/model"
	"gorm.io/gorm"
)

// DownloadedStaticGtfs is a static GTFS zip saved to a temporary file. Call Remove when done.
type DownloadedStaticGtfs struct {
	Path string
	// Hex SHA-256 of the zip file
	Sha256 string
}

func (download *DownloadedStaticGtfs) Remove() {
	os.Remove(download.Path)
}

// DownloadStaticGtfs saves the static GTFS zip at url to a temporary file.
func DownloadStaticGtfs(ctx context.Context, client *http.Client, url string) (*DownloadedStaticGtfs, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	tempFile, err := os.CreateTemp(os.TempDir(), "google_transit*.zip")
	if err != nil {
		return nil, err
	}
	download := &DownloadedStaticGtfs{Path: tempFile.Name()}

	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(tempFile, hash), resp.Body)
	closeErr := tempFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		download.Remove()
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	download.Sha256 = hex.EncodeToString(hash.Sum(nil))
	return download, nil
}

// Parses a static GTFS feed into a struct. Handles a local folder, or local zipped file
func ParseStaticGtfsFromPath(path string) (*model.GtfsStaticFeed, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var gtfsFiles *GtfsFileCollection

	if fileInfo.IsDir() {
		files, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		var gtfsFilesList []GtfsFile
		for _, f := range files {

			fileObj, err := os.Open(filepath.Join(path, f.Name()))
			if err != nil {
				return nil, err
			}
			defer fileObj.Close()

			gtfsFilesList = append(gtfsFilesList, GtfsFile{Name: f.Name(), FileObj: fileObj})
		}

		gtfsFiles = &GtfsFileCollection{GtfsFiles: gtfsFilesList}
	} else {
		gtfsFiles, err = getGtfsFilesFromZip(path)
		if err != nil {
			return nil, err
		}
	}

	result, err := parseStaticGtfsFromFiles(gtfsFiles)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Opens a zip file and gets file names and objects for each file in the zip file. Leaves files open
func getGtfsFilesFromZip(path string) (*GtfsFileCollection, error) {
	var gtfsFiles []GtfsFile
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, errors.New("Unable to read zip file at " + path + ". Must provide an unzipped directory with GTFS txt files, or a zipped google_transit.zip file")
	}
	// Every file is read fully into memory below, so the zip can be closed on return
	defer archive.Close()

	for _, f := range archive.File {
		readerCloser, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer readerCloser.Close()

		// Memory-inefficient to read all these files into memory, but simplifies
		// downstream hashing code, which reads from the same file again (implements ReaderSeeker interface)
		fileBytes, err := io.ReadAll(readerCloser)
		if err != nil {
			return nil, err
		}

		gtfsFiles = append(gtfsFiles, GtfsFile{Name: f.Name, FileObj: bytes.NewReader(fileBytes)})
	}
	return &GtfsFileCollection{GtfsFiles: gtfsFiles}, nil
}

type GtfsFileCollection struct {
	GtfsFiles []GtfsFile
}

type GtfsFile struct {
	Name    string
	FileObj io.ReadSeeker
}

func parseStaticGtfsFromFiles(files *GtfsFileCollection) (*model.GtfsStaticFeed, error) {
	var result model.GtfsStaticFeed

	hash := md5.New()
	// Sort to ensure consistent hashing for version creation
	sort.Slice(files.GtfsFiles, func(i, j int) bool { return files.GtfsFiles[i].Name < files.GtfsFiles[j].Name })

	for _, f := range files.GtfsFiles {
		if strings.ToLower(f.Name) == "agency.txt" {
			agencies, err := parseSingleStaticFile[model.Agency](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.Agency = agencies
		}
		if strings.ToLower(f.Name) == "stops.txt" {
			stops, err := parseSingleStaticFile[model.Stop](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.Stop = stops
		}
		if strings.ToLower(f.Name) == "routes.txt" {
			routes, err := parseSingleStaticFile[model.Route](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.Route = routes
		}
		if strings.ToLower(f.Name) == "trips.txt" {
			trips, err := parseSingleStaticFile[model.Trip](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.Trip = trips
		}
		if strings.ToLower(f.Name) == "stop_times.txt" {
			stopTimes, err := parseSingleStaticFile[model.StopTime](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.StopTime = stopTimes
		}
		if strings.ToLower(f.Name) == "calendar.txt" {
			calendars, err := parseSingleStaticFile[model.Calendar](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.Calendar = calendars
		}
		if strings.ToLower(f.Name) == "shapes.txt" {
			shapes, err := parseSingleStaticFile[model.Shape](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}
			result.Shape = shapes
		}
		if strings.ToLower(f.Name) == "feed_info.txt" {
			feedInfos, err := parseSingleStaticFile[model.FeedInfo](f.FileObj)
			if err != nil {
				return &result, err
			}
			err = updateHash(hash, f.FileObj)
			if err != nil {
				return &result, err
			}

			if len(feedInfos) > 1 {
				return &result, errors.New("multiple feed info rows detected. Expected 1 or 0")
			} else if len(feedInfos) == 1 {
				result.FeedInfo = feedInfos[0]
			}
		}
	}

	updateVersion(&result, hex.EncodeToString(hash.Sum(nil)))

	return &result, nil
}

// If a feed_info file with a version is not provided, use the md5 hash of the included
// GTFS files to generate a fake FeedInfo object
func updateVersion(feed *model.GtfsStaticFeed, hash string) error {
	var defaultFeedInfo model.FeedInfo
	if feed.FeedInfo == defaultFeedInfo {
		feed.FeedInfo = model.FeedInfo{Version: hash}
		addVersionToAllObjects(feed, hash)
	} else {
		addVersionToAllObjects(feed, feed.FeedInfo.Version)
	}

	feed.FeedInfo.DownloadTime = time.Now()

	return nil
}

func addVersionToAllObjects(feed *model.GtfsStaticFeed, version string) {
	for i := range feed.Agency {
		feed.Agency[i].Version = version
	}
	for i := range feed.Stop {
		feed.Stop[i].Version = version
	}
	for i := range feed.Route {
		feed.Route[i].Version = version
	}
	for i := range feed.Trip {
		feed.Trip[i].Version = version
	}
	for i := range feed.StopTime {
		feed.StopTime[i].Version = version
	}
	for i := range feed.Calendar {
		feed.Calendar[i].Version = version
	}
	for i := range feed.Shape {
		feed.Shape[i].Version = version
	}
}

func updateHash(hash hash.Hash, fileObj io.ReadSeeker) error {
	fileObj.Seek(0, io.SeekStart)
	if _, err := io.Copy(hash, fileObj); err != nil {
		return err
	}
	return nil
}

func parseSingleStaticFile[T any](path io.ReadSeeker) ([]T, error) {
	elements := make([]T, 0)
	recordProvider, err := csv_parse.BeginParseCsv[T](path)
	if err != nil {
		return elements, err
	}

	for {
		element, err := recordProvider.FetchNext()
		if err != nil {
			if err == csv_parse.EOF {
				break
			}
			return elements, err
		}
		elements = append(elements, element)
	}

	return elements, nil
}

func GetFeedOnDate(year int, month time.Month, day int, db *gorm.DB) (*model.GtfsStaticFeed, error) {
	var feedInfo model.FeedInfo
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	tx := db.Where("start_date <= ? and end_date >= ? and cast(download_time as date) <= ?", date, date, date).Order("download_time DESC").First(&feedInfo)
	if tx.Error != nil {
		return nil, tx.Error
	}
	var feed model.GtfsStaticFeed
	feed.FeedInfo = feedInfo
	// TODO: Put this all in the same transaction?
	tx = db.Where("version = ?", feedInfo.Version).Find(&feed.Agency)
	if tx.Error != nil {
		return nil, tx.Error
	}

	tx = db.Where("version = ?", feedInfo.Version).Find(&feed.Stop)
	if tx.Error != nil {
		return nil, tx.Error
	}

	tx = db.Where("version = ?", feedInfo.Version).Find(&feed.Route)
	if tx.Error != nil {
		return nil, tx.Error
	}

	tx = db.Where("version = ?", feedInfo.Version).Find(&feed.Trip)
	if tx.Error != nil {
		return nil, tx.Error
	}

	tx = db.Where("version = ?", feedInfo.Version).Find(&feed.StopTime)
	if tx.Error != nil {
		return nil, tx.Error
	}

	tx = db.Where("version = ?", feedInfo.Version).Find(&feed.Calendar)
	if tx.Error != nil {
		return nil, tx.Error
	}

	return &feed, nil
}

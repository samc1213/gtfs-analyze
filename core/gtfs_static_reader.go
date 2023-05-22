package core

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/samc1213/gtfs-analyze/csv_parse"
	"github.com/samc1213/gtfs-analyze/model"
)

func ParseStaticGtfsFromUrl(url string) (*model.GtfsStaticFeed, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("Non-successful response from uri " + url)
	}

	tempfile, err := os.CreateTemp(os.TempDir(), "google_transit*.zip")
	if err != nil {
		return nil, err
	}
	defer tempfile.Close()
	defer os.Remove(tempfile.Name())

	_, err = io.Copy(tempfile, resp.Body)
	if err != nil {
		return nil, err
	}

	gtfsFiles, err := getGtfsFilesFromZip(tempfile.Name())
	if err != nil {
		return nil, err
	}
	defer gtfsFiles.CloseAll()

	result, err := parseStaticGtfsFromFiles(gtfsFiles)

	if err != nil {
		return nil, err
	}
	return result, nil
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
		defer gtfsFiles.CloseAll()
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

	for _, f := range archive.File {
		readerCloser, err := f.Open()
		if err != nil {
			return nil, err
		}

		gtfsFiles = append(gtfsFiles, GtfsFile{Name: f.Name, FileObj: readerCloser})
	}
	return &GtfsFileCollection{GtfsFiles: gtfsFiles}, nil
}

func (collection *GtfsFileCollection) CloseAll() error {
	for _, gtfsFile := range collection.GtfsFiles {
		err := gtfsFile.FileObj.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type GtfsFileCollection struct {
	GtfsFiles []GtfsFile
}

type GtfsFile struct {
	Name    string
	FileObj io.ReadCloser
}

func parseStaticGtfsFromFiles(files *GtfsFileCollection) (*model.GtfsStaticFeed, error) {
	var result model.GtfsStaticFeed

	for _, f := range files.GtfsFiles {
		if strings.ToLower(f.Name) == "agency.txt" {
			agencies, err := parseSingleStaticFile[model.Agency](f.FileObj)
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
			result.Stop = stops
		}
		if strings.ToLower(f.Name) == "routes.txt" {
			routes, err := parseSingleStaticFile[model.Route](f.FileObj)
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
			result.Trip = trips
		}
		if strings.ToLower(f.Name) == "stop_times.txt" {
			stopTimes, err := parseSingleStaticFile[model.StopTime](f.FileObj)
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
			result.Calendar = calendars
		}
		if strings.ToLower(f.Name) == "feed_info.txt" {
			feedInfos, err := parseSingleStaticFile[model.FeedInfo](f.FileObj)
			if err != nil {
				return &result, err
			}
			result.FeedInfo = feedInfos
		}
	}

	return &result, nil
}

func parseSingleStaticFile[T any](path io.ReadCloser) ([]T, error) {
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

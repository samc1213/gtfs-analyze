// Package config loads the list of agencies and feeds to collect (agencies.yaml).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type FeedKind string

const (
	Static   FeedKind = "static"
	Realtime FeedKind = "realtime"
)

type Config struct {
	Agencies []Agency `yaml:"agencies"`
}

// Agency is an organization that publishes feeds. It is usually one transit agency,
// but can be a regional publisher whose feeds cover several agencies.
type Agency struct {
	// Our identifier, e.g. "rtd". It names the agency in archive paths and on the
	// command line, so it must never change once data has been collected.
	Id    string `yaml:"id"`
	Name  string `yaml:"name"`
	Feeds []Feed `yaml:"feeds"`
}

type Feed struct {
	// Unique within the agency, e.g. "gtfs" or "vehicle_positions"
	Id   string   `yaml:"id"`
	Kind FeedKind `yaml:"kind"`
	// May reference environment variables as ${NAME}, for API keys passed in the query string
	Url string `yaml:"url"`
	// HTTP headers sent with each request. Values may reference environment variables as ${NAME}.
	Headers map[string]string `yaml:"headers,omitempty"`
	// The agency's terms of use for this feed
	LicenseUrl string `yaml:"license_url"`
	// The feed's ID in the Mobility Database catalog (https://mobilitydatabase.org), e.g. "mdb-178"
	MobilityDatabaseId string `yaml:"mobility_database_id,omitempty"`
}

// Lowercase letters, digits, hyphens and underscores, starting with a letter: safe in
// file paths, object storage keys and URLs.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
var mobilityDatabaseIdPattern = regexp.MustCompile(`^mdb-[0-9]+$`)

// Load reads and validates a config file. Unknown fields are an error, so a typo
// doesn't silently drop a setting.
func Load(path string) (*Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config, err := Parse(contents)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

func Parse(contents []byte) (*Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (config *Config) validate() error {
	if len(config.Agencies) == 0 {
		return errors.New("no agencies configured")
	}
	var errs []error
	agencyIds := make(map[string]bool)
	for i, agency := range config.Agencies {
		where := fmt.Sprintf("agency %q", agency.Id)
		if agency.Id == "" {
			where = fmt.Sprintf("agency #%d", i+1)
		}
		if !idPattern.MatchString(agency.Id) {
			errs = append(errs, fmt.Errorf("%s: id must be lowercase letters, digits, '-' or '_', starting with a letter", where))
		}
		if agencyIds[agency.Id] {
			errs = append(errs, fmt.Errorf("%s: duplicate id", where))
		}
		agencyIds[agency.Id] = true
		if agency.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required", where))
		}
		if len(agency.Feeds) == 0 {
			errs = append(errs, fmt.Errorf("%s: no feeds", where))
		}
		feedIds := make(map[string]bool)
		for j, feed := range agency.Feeds {
			feedWhere := fmt.Sprintf("%s feed %q", where, feed.Id)
			if feed.Id == "" {
				feedWhere = fmt.Sprintf("%s feed #%d", where, j+1)
			}
			if !idPattern.MatchString(feed.Id) {
				errs = append(errs, fmt.Errorf("%s: id must be lowercase letters, digits, '-' or '_', starting with a letter", feedWhere))
			}
			if feedIds[feed.Id] {
				errs = append(errs, fmt.Errorf("%s: duplicate id", feedWhere))
			}
			feedIds[feed.Id] = true
			if feed.Kind != Static && feed.Kind != Realtime {
				errs = append(errs, fmt.Errorf("%s: kind must be %q or %q", feedWhere, Static, Realtime))
			}
			if parsed, err := url.Parse(feed.Url); err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				errs = append(errs, fmt.Errorf("%s: url must be an http(s) URL", feedWhere))
			}
			if feed.LicenseUrl == "" {
				errs = append(errs, fmt.Errorf("%s: license_url is required; link the agency's terms of use", feedWhere))
			}
			if feed.MobilityDatabaseId != "" && !mobilityDatabaseIdPattern.MatchString(feed.MobilityDatabaseId) {
				errs = append(errs, fmt.Errorf("%s: mobility_database_id must look like mdb-123", feedWhere))
			}
		}
	}
	return joinErrors(errs)
}

// joinErrors reports every problem at once, one per line (errors.Join needs Go 1.20)
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	messages := make([]string, len(errs))
	for i, err := range errs {
		messages[i] = err.Error()
	}
	return errors.New(strings.Join(messages, "\n"))
}

// Select returns the agencies with the given ids, in the order given, or every agency if ids is empty.
func (config *Config) Select(ids []string) ([]Agency, error) {
	if len(ids) == 0 {
		return config.Agencies, nil
	}
	byId := make(map[string]Agency)
	var known []string
	for _, agency := range config.Agencies {
		byId[agency.Id] = agency
		known = append(known, agency.Id)
	}
	var selected []Agency
	for _, id := range ids {
		agency, ok := byId[id]
		if !ok {
			return nil, fmt.Errorf("unknown agency %q (configured: %s)", id, strings.Join(known, ", "))
		}
		selected = append(selected, agency)
	}
	return selected, nil
}

// Request returns the feed's URL and headers with environment variables substituted.
// A referenced variable that isn't set is an error, rather than a silently broken request.
func (feed Feed) Request() (string, http.Header, error) {
	var missing []string
	expand := func(value string) string {
		return os.Expand(value, func(name string) string {
			resolved, ok := os.LookupEnv(name)
			if !ok {
				missing = append(missing, name)
			}
			return resolved
		})
	}
	resolvedUrl := expand(feed.Url)
	headers := make(http.Header)
	for name, value := range feed.Headers {
		headers.Set(name, expand(value))
	}
	if len(missing) > 0 {
		return "", nil, fmt.Errorf("feed %q: environment variables not set: %s", feed.Id, strings.Join(missing, ", "))
	}
	return resolvedUrl, headers, nil
}

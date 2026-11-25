package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validConfig = `
agencies:
  - id: rtd
    name: Regional Transportation District
    feeds:
      - id: gtfs
        kind: static
        url: https://example.com/gtfs.zip
        license_url: https://example.com/license
        mobility_database_id: mdb-178
      - id: vehicle_positions
        kind: realtime
        url: https://example.com/vp.pb?key=${TEST_FEED_KEY}
        headers:
          x-api-key: ${TEST_FEED_KEY}
        license_url: https://example.com/license
  - id: other
    name: Other Agency
    feeds:
      - id: gtfs
        kind: static
        url: https://example.com/other.zip
        license_url: https://example.com/license
`

// The repository's own registry must always load
func TestRepositoryAgenciesFileIsValid(t *testing.T) {
	config, err := Load("../agencies.yaml")
	require.NoError(t, err)
	agencies, err := config.Select([]string{"rtd"})
	require.NoError(t, err)
	assert.NotEmpty(t, agencies[0].Feeds)
}

func TestParse(t *testing.T) {
	config, err := Parse([]byte(validConfig))
	require.NoError(t, err)
	require.Len(t, config.Agencies, 2)
	rtd := config.Agencies[0]
	assert.Equal(t, "rtd", rtd.Id)
	assert.Equal(t, Static, rtd.Feeds[0].Kind)
	assert.Equal(t, "mdb-178", rtd.Feeds[0].MobilityDatabaseId)
	assert.Equal(t, Realtime, rtd.Feeds[1].Kind)
	// The agency's only static feed is implied
	assert.Equal(t, []string{"gtfs"}, rtd.Feeds[1].StaticFeeds)
	assert.Empty(t, rtd.Feeds[0].StaticFeeds)
}

func TestStaticFeedsLinkRealtimeFeedsToStaticFeeds(t *testing.T) {
	config, err := Parse([]byte(`
agencies:
  - id: mta-bus
    name: MTA buses
    feeds:
      - id: bronx
        kind: static
        url: https://example.com/bronx.zip
        license_url: https://example.com/license
      - id: brooklyn
        kind: static
        url: https://example.com/brooklyn.zip
        license_url: https://example.com/license
      - id: trip_updates
        kind: realtime
        url: https://example.com/tu.pb
        license_url: https://example.com/license
        static_feeds: [bronx, brooklyn]
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"bronx", "brooklyn"}, config.Agencies[0].Feeds[2].StaticFeeds)
}

func TestStaticFeedsValidation(t *testing.T) {
	_, err := Parse([]byte(`
agencies:
  - id: two-static
    name: Two static feeds
    feeds:
      - id: a
        kind: static
        url: https://example.com/a.zip
        license_url: https://example.com/license
        static_feeds: [b]
      - id: b
        kind: static
        url: https://example.com/b.zip
        license_url: https://example.com/license
      - id: unlinked
        kind: realtime
        url: https://example.com/rt.pb
        license_url: https://example.com/license
      - id: wrong_link
        kind: realtime
        url: https://example.com/rt.pb
        license_url: https://example.com/license
        static_feeds: [unlinked, missing]
  - id: realtime-only
    name: No static feed
    feeds:
      - id: rt
        kind: realtime
        url: https://example.com/rt.pb
        license_url: https://example.com/license
`))
	require.Error(t, err)
	for _, problem := range []string{
		`feed "a": static_feeds is only for realtime feeds`,
		`feed "unlinked": static_feeds is required because the agency has several static feeds (a, b)`,
		`feed "wrong_link": static_feeds: "unlinked" is not a static feed of this agency`,
		`feed "wrong_link": static_feeds: "missing" is not a static feed of this agency`,
		`agency "realtime-only" feed "rt": the agency has no static feed`,
	} {
		assert.Contains(t, err.Error(), problem)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte("agencies:\n  - id: rtd\n    nmae: typo\n"))
	assert.ErrorContains(t, err, "nmae")
}

func TestValidationReportsEveryProblem(t *testing.T) {
	_, err := Parse([]byte(`
agencies:
  - id: RTD
    name: Bad id
    feeds:
      - id: gtfs
        kind: zip
        url: ftp://example.com/gtfs.zip
        mobility_database_id: "178"
      - id: gtfs
        kind: static
        url: https://example.com/gtfs.zip
        license_url: https://example.com/license
  - id: empty
    name: ""
`))
	require.Error(t, err)
	for _, problem := range []string{
		`agency "RTD": id must be lowercase`,
		`feed "gtfs": kind must be`,
		`feed "gtfs": url must be an http(s) URL`,
		`feed "gtfs": license_url is required`,
		`feed "gtfs": mobility_database_id must look like`,
		`feed "gtfs": duplicate id`,
		`agency "empty": name is required`,
		`agency "empty": no feeds`,
	} {
		assert.Contains(t, err.Error(), problem)
	}
}

func TestParseRejectsDuplicateAgencies(t *testing.T) {
	_, err := Parse([]byte(validConfig + `
  - id: rtd
    name: Duplicate
    feeds:
      - id: gtfs
        kind: static
        url: https://example.com/gtfs.zip
        license_url: https://example.com/license
`))
	assert.ErrorContains(t, err, `agency "rtd": duplicate id`)
}

func TestSelect(t *testing.T) {
	config, err := Parse([]byte(validConfig))
	require.NoError(t, err)

	all, err := config.Select(nil)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	selected, err := config.Select([]string{"other"})
	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "other", selected[0].Id)

	_, err = config.Select([]string{"nope"})
	assert.ErrorContains(t, err, `unknown agency "nope" (configured: rtd, other)`)
}

func TestRequestSubstitutesEnvironmentVariables(t *testing.T) {
	config, err := Parse([]byte(validConfig))
	require.NoError(t, err)
	feed := config.Agencies[0].Feeds[1]

	_, _, err = feed.Request()
	assert.ErrorContains(t, err, "environment variables not set: TEST_FEED_KEY")

	t.Setenv("TEST_FEED_KEY", "secret")
	url, headers, err := feed.Request()
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/vp.pb?key=secret", url)
	assert.Equal(t, "secret", headers.Get("X-Api-Key"))
}

package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samc1213/gtfs-analyze/config"
	"github.com/samc1213/gtfs-analyze/core"
	"github.com/spf13/cobra"
)

var DbPath string
var ArchiveDir string
var ConfigPath string
var AgencyIds []string
var RtPollIntervalSecs uint
var StaticPollIntervalMins uint

// storeCmd represents the store command
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Stores GTFS and GTFS-RT feeds to a database",
	Long: `The store command collects agencies' static GTFS and GTFS-RT feeds. Agencies and
their feed URLs are listed in a config file (see agencies.yaml); --agency picks
which to collect. At least one of --archive-dir and --db-path is required.

--archive-dir keeps each distinct raw download exactly as received, for any
number of agencies: realtime snapshots gzipped under <agency>/<feed>/YYYY/MM/DD/,
static zips under <agency>/<feed>/. It is written before the database, so the
database can always be rebuilt from it.

--db-path parses the feeds into a SQLite database for analysis. A database holds
one agency.

Each new static feed version is stored in full. Realtime feeds are stored as a
history of changes: a row is written only when a vehicle position, stop prediction,
trip or alert changes.

With non-zero poll intervals the command runs until interrupted (Ctrl+C or SIGTERM).
A failed fetch is logged and retried on the next interval. Set a poll interval to 0
to fetch that feed once and exit.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Flags parsed fine; don't print usage for runtime errors like a failed download
		cmd.SilenceUsage = true

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		agencyConfig, err := config.Load(ConfigPath)
		if err != nil {
			return err
		}
		agencies, err := agencyConfig.Select(AgencyIds)
		if err != nil {
			return err
		}

		return core.Store(ctx, core.StoreConfig{
			DbPath:             DbPath,
			ArchiveDir:         ArchiveDir,
			Agencies:           agencies,
			StaticPollInterval: time.Duration(StaticPollIntervalMins) * time.Minute,
			RtPollInterval:     time.Duration(RtPollIntervalSecs) * time.Second,
			LogLevel:           LogLevel,
		})
	},
}

func init() {
	rootCmd.AddCommand(storeCmd)

	storeCmd.Flags().StringVar(&DbPath, "db-path", "", "The path to a local SQLite database for parsed feed data")
	storeCmd.Flags().StringVar(&ArchiveDir, "archive-dir", "", "A directory to archive every raw feed download in (gzipped GTFS-RT snapshots and static zips)")
	storeCmd.Flags().StringVar(&ConfigPath, "config", "agencies.yaml", "The config file listing agencies and their feeds")
	storeCmd.Flags().StringSliceVar(&AgencyIds, "agency", nil, "Ids of the agencies to store, e.g. rtd (default: every agency in the config)")
	storeCmd.Flags().UintVar(&RtPollIntervalSecs, "rt-poll-interval", 30, "How often to poll the GTFS-RT feeds, in seconds (0 = fetch once)")
	storeCmd.Flags().UintVar(&StaticPollIntervalMins, "static-poll-interval", 60, "How often to poll for static GTFS data, in minutes (0 = fetch once)")
}

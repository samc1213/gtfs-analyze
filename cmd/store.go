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
var ConfigPath string
var AgencyIds []string
var RtPollIntervalSecs uint
var StaticPollIntervalMins uint

// storeCmd represents the store command
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Stores GTFS and GTFS-RT feeds to a database",
	Long: `The store command saves an agency's static GTFS and GTFS-RT feeds into a SQLite
database for further analysis. Agencies and their feed URLs are listed in a config
file (see agencies.yaml); a database holds one agency, chosen with --agency.

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
			Agencies:           agencies,
			StaticPollInterval: time.Duration(StaticPollIntervalMins) * time.Minute,
			RtPollInterval:     time.Duration(RtPollIntervalSecs) * time.Second,
			LogLevel:           LogLevel,
		})
	},
}

func init() {
	rootCmd.AddCommand(storeCmd)

	storeCmd.Flags().StringVar(&DbPath, "db-path", "", "The path to a local SQLite database for logging")
	storeCmd.MarkFlagRequired("db-path")
	storeCmd.Flags().StringVar(&ConfigPath, "config", "agencies.yaml", "The config file listing agencies and their feeds")
	storeCmd.Flags().StringSliceVar(&AgencyIds, "agency", nil, "Ids of the agencies to store, e.g. rtd (default: every agency in the config)")
	storeCmd.Flags().UintVar(&RtPollIntervalSecs, "rt-poll-interval", 30, "How often to poll the GTFS-RT feeds, in seconds (0 = fetch once)")
	storeCmd.Flags().UintVar(&StaticPollIntervalMins, "static-poll-interval", 60, "How often to poll for static GTFS data, in minutes (0 = fetch once)")
}

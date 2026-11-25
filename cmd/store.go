package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samc1213/gtfs-analyze/core"
	"github.com/spf13/cobra"
)

var DbPath string
var StaticUrl string
var VehiclePositionUrl string
var TripUpdatesUrl string
var AlertsUrl string
var RtPollIntervalSecs uint
var StaticPollIntervalMins uint

// storeCmd represents the store command
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Stores GTFS and GTFS-RT feeds to a database",
	Long: `The store command saves a static GTFS feed and GTFS-RT feeds (vehicle positions,
trip updates and alerts) into a SQLite database for further analysis.

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

		return core.Store(ctx, core.StoreConfig{
			DbPath:             DbPath,
			StaticUrl:          StaticUrl,
			VehiclePositionUrl: VehiclePositionUrl,
			TripUpdatesUrl:     TripUpdatesUrl,
			AlertsUrl:          AlertsUrl,
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
	storeCmd.Flags().StringVar(&StaticUrl, "static-url", "", "The web url for a static GTFS feed")
	storeCmd.Flags().StringVar(&VehiclePositionUrl, "vehicle-pos-url", "", "The web url for a GTFS-RT VehiclePosition protobuf feed")
	storeCmd.Flags().StringVar(&TripUpdatesUrl, "trip-updates-url", "", "The web url for a GTFS-RT TripUpdate protobuf feed")
	storeCmd.Flags().StringVar(&AlertsUrl, "alerts-url", "", "The web url for a GTFS-RT Alert protobuf feed")
	storeCmd.Flags().UintVar(&RtPollIntervalSecs, "rt-poll-interval", 30, "How often to poll the GTFS-RT feeds, in seconds (0 = fetch once)")
	storeCmd.Flags().UintVar(&StaticPollIntervalMins, "static-poll-interval", 60, "How often to poll for static GTFS data, in minutes (0 = fetch once)")
}

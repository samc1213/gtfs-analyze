package cmd

import (
	"github.com/samc1213/gtfs-analyze/core"
	"github.com/spf13/cobra"
)

var DbPath string
var StaticUrl string
var RtUrl string
var VehiclePositionUrl string
var RtPollIntervalSecs uint
var StaticPollIntervalMins uint

// storeCmd represents the log command
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Stores a GTFS feed to a database",
	Long: `The store command saves all data from a GTFS feed into a database
for further analysis. It currently supports SQLite databases and only saves
static GTFS feeds`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := core.Store(DbPath, StaticUrl, VehiclePositionUrl, StaticPollIntervalMins, RtPollIntervalSecs, LogLevel)
		return err
	},
}

func init() {
	rootCmd.AddCommand(storeCmd)

	storeCmd.Flags().StringVar(&DbPath, "db-path", "", "The path to a local SQLite database for logging")
	storeCmd.MarkFlagRequired("db-path")
	storeCmd.Flags().StringVar(&StaticUrl, "static-url", "", "The web url for a static GTFS feed")
	storeCmd.Flags().StringVar(&VehiclePositionUrl, "vehicle-pos-url", "", "The web url for a GTFS-RT VehiclePosition protobuf update")
	storeCmd.Flags().UintVar(&RtPollIntervalSecs, "rt-poll-interval", 30, "How often to poll for GTFS-RT data, in seconds")
	storeCmd.Flags().UintVar(&StaticPollIntervalMins, "static-poll-interval", 60, "How often to poll for static GTFS data, in minutes")

}

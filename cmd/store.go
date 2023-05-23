package cmd

import (
	"github.com/samc1213/gtfs-analyze/core"
	"github.com/spf13/cobra"
)

var DbPath string
var StaticUrl string

// storeCmd represents the log command
var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Stores a GTFS feed to a database",
	Long: `The store command saves all data from a GTFS feed into a database
for further analysis. It currently supports SQLite databases and only saves
static GTFS feeds`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return core.Store(DbPath, StaticUrl, LogLevel)
	},
}

func init() {
	rootCmd.AddCommand(storeCmd)

	storeCmd.Flags().StringVar(&DbPath, "db-path", "", "The path to a local SQLite database for logging")
	storeCmd.MarkFlagRequired("db-path")
	storeCmd.Flags().StringVar(&StaticUrl, "static-url", "", "The web url for a static GTFS feed")
	storeCmd.MarkFlagRequired("static-url")
}

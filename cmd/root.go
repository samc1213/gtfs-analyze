package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gtfs-analyze",
	Short: "A utility for analyzing GTFS data",
	Long: `gtfs is a utility written in Go to help log, track changes in, and analyze 
General Transit Feed Specificiation (GTFS) data. GTFS is commonly used by US-based 
transit agencies to specify their schedules, routes, and provide real-time updates to users.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
}

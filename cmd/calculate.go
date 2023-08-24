package cmd

import (
	"github.com/spf13/cobra"
)

var calculateCmd = &cobra.Command{
	Use:   "calculate",
	Short: "Calculate some metric from the GTFS data",
}

func init() {
	rootCmd.AddCommand(calculateCmd)
}

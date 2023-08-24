package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/samc1213/gtfs-analyze/core"
	"github.com/spf13/cobra"
)

var StartTime string
var EndTime string
var OnTimeThreshold time.Duration

func parseTime(timeString string) (time.Time, error) {
	result, err := time.Parse(time.RFC3339, timeString)
	if err != nil {
		result, err = time.Parse(time.RFC822Z, timeString)
	}
	return result, err
}

var otpCmd = &cobra.Command{
	Use:   "otp",
	Short: "Calculate on-time performance",
	RunE: func(cmd *cobra.Command, args []string) error {
		startTime, err := parseTime(StartTime)
		if err != nil {
			return errors.New("start-time must be in format " + time.RFC3339 + " or " + time.RFC822Z)
		}
		endTime, err := parseTime(EndTime)
		if err != nil {
			return errors.New("end-time must be in format " + time.RFC3339 + " or " + time.RFC822Z)
		}
		summary, err := core.CalculateOtpForTimeRange(DbPath, startTime, endTime, OnTimeThreshold, LogLevel)
		if err != nil {
			return err
		}
		fmt.Println(summary.PrettyPrint())
		return nil
	},
}

func init() {
	calculateCmd.AddCommand(otpCmd)

	otpCmd.Flags().StringVar(&DbPath, "db-path", "", "The path to a local SQLite database for logging")
	otpCmd.MarkFlagRequired("db-path")

	otpCmd.Flags().StringVar(&StartTime, "start-time", "", "When to start the OTP calculation")
	otpCmd.MarkFlagRequired("start-time")

	otpCmd.Flags().StringVar(&EndTime, "end-time", "", "When to end the OTP calculation")
	otpCmd.MarkFlagRequired("end-time")

	otpCmd.Flags().DurationVar(&OnTimeThreshold, "threshold", 7*time.Minute, "How close to expected arrival a vehicle must be to count as on-time")
}

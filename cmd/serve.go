package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samc1213/gtfs-analyze/api"
	"github.com/samc1213/gtfs-analyze/core"
	"github.com/samc1213/gtfs-analyze/log"
	"github.com/spf13/cobra"
)

var ServeAddr string
var ServeAllowOrigin string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve a read-only JSON API over the database, for the web UI",
	Long: `The serve command exposes the data collected by "store" as a JSON API under /api,
used by the web UI in web/. It only reads, so it can run alongside "store" on the
same database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		logger := log.New(LogLevel)

		db, err := core.InitializeSqliteDatabase(DbPath, LogLevel)
		if err != nil {
			return err
		}

		server := &http.Server{
			Addr:              ServeAddr,
			Handler:           api.NewServer(db, logger, ServeAllowOrigin),
			ReadHeaderTimeout: 10 * time.Second,
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			server.Shutdown(shutdownCtx)
		}()

		// Printed regardless of log level, so it's clear where to point the web UI
		cmd.Printf("Serving API on http://%s/api\n", ServeAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVar(&DbPath, "db-path", "", "The path to the SQLite database written by the store command")
	serveCmd.MarkFlagRequired("db-path")
	serveCmd.Flags().StringVar(&ServeAddr, "addr", "localhost:8080", "Address to listen on")
	serveCmd.Flags().StringVar(&ServeAllowOrigin, "allow-origin", "*", "Access-Control-Allow-Origin header for the web UI (empty to disable CORS)")
}

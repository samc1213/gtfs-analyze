# gtfs-analyze
gtfs-analyze is a command line tool to analyze General Transit Feed Specification ([GTFS](https://gtfs.org/)) data. I'm learning [go](https://go.dev/) to implement the tool.

## Usage
Right now, `gtfs-analyze` has two commands: 

* `store` - watch a GTFS static feed and GTFS-RT feeds (vehicle positions, trip updates, alerts), and log the changes to a SQLite database
* `calculate otp` - calculate the on-time performance of an agency based on the data logged in the `store` command


To start storing data for Denver's RTD system, we would run a command like this:

```bash
$ gtfs-analyze --log-level info store \
    --db-path ~/Downloads/rtd.db \
    --static-url https://www.rtd-denver.com/files/gtfs/google_transit.zip \
    --vehicle-pos-url https://www.rtd-denver.com/files/gtfs-rt/VehiclePosition.pb \
    --trip-updates-url https://www.rtd-denver.com/files/gtfs-rt/TripUpdate.pb \
    --alerts-url https://www.rtd-denver.com/files/gtfs-rt/Alerts.pb
```

This runs until interrupted (Ctrl+C or SIGTERM), polling the static feed every `--static-poll-interval` minutes (default 60) and the realtime feeds every `--rt-poll-interval` seconds (default 30). A failed fetch is logged and retried on the next interval. Set an interval to `0` to fetch that feed once and exit.

What gets stored:
* **Static GTFS**: each new feed version is stored in full, keyed by `feed_info.txt`'s `feed_version` (or a hash of the feed files if there is none). Versions already in the database are skipped.
* **GTFS-RT**: stored as a history of changes rather than a copy of every snapshot. A row is written to `vehicle_positions`, `trip_updates`, `stop_time_updates` or `alerts` only when that vehicle, trip, stop prediction or alert changed since the previous snapshot. `message_timestamp` on each row is the feed snapshot in which the change was seen. Change detection is kept in memory, so the first snapshot after a restart is stored in full.

The database uses SQLite's WAL mode, so you can query it (or run `calculate otp`) while `store` is running.

Then, in another process, we can analyze the on-time performance in the system for a given timerange:

```bash
$ gtfs-analyze --log-level info calculate otp --db-path ~/Downloads/rtd.db --start-time 2023-08-22T15:00:00-07:00 --end-time 2023-08-22T16:00:00-07:00
```

This will print a table with the on-time performance per trip. You can configure what is considered to be on-time with the `--threshold` flag.

For help, try `gtfs-analyze --help`.

## Building
Building requires Go and [`protoc`](https://github.com/protocolbuffers/protobuf/releases), which generates the GTFS-RT bindings:

```bash
$ ./gtfs_realtime/generate.sh
$ go build -o build/ .
$ go test ./...
```

No C compiler is needed: SQLite access uses a pure-Go driver.

## Packages
* **[core](core/)**: All the core logic and database interaction of the tool
* **[model](model/)**: The GTFS data model
* **[csv_parse](csv_parse/)**: CSV parser. An excuse to learn about reflection in go
* **[log](log/)**: A wrapper around go's standard `log` package
* **[cmd](cmd/)**: All the entry commands for the CLI tool. Created by [cobra](https://github.com/spf13/cobra)

# gtfs-analyze
gtfs-analyze is a command line tool to analyze General Transit Feed Specification ([GTFS](https://gtfs.org/)) data. I'm learning [go](https://go.dev/) to implement the tool.

## Usage
Right now, `gtfs-analyze` has two commands: 

* `store` - watch an agency's GTFS static and GTFS-RT feeds (vehicle positions, trip updates, alerts), and log the changes to a SQLite database
* `calculate otp` - calculate the on-time performance of an agency based on the data logged in the `store` command


Agencies and their feeds are listed in [`agencies.yaml`](agencies.yaml). Each agency has a short id we assign (e.g. `rtd`), and each feed has its URL, a link to the agency's terms of use, and optionally its [Mobility Database](https://mobilitydatabase.org) id. Feeds that need an API key can reference environment variables (`${NAME}`) in the URL or in `headers`. The file's header comment documents every field.

To start storing data for Denver's RTD system:

```bash
$ gtfs-analyze --log-level info store --agency rtd --db-path ~/Downloads/rtd.db --archive-dir ~/Downloads/gtfs-archive
```

`--config` points at a different config file (default `agencies.yaml`). A SQLite database holds one agency.

This runs until interrupted (Ctrl+C or SIGTERM), polling the static feed every `--static-poll-interval` minutes (default 60) and the realtime feeds every `--rt-poll-interval` seconds (default 30). A failed fetch is logged and retried on the next interval. Set an interval to `0` to fetch that feed once and exit.

What gets stored:
* **Static GTFS**: each new feed version is stored in full, keyed by `feed_info.txt`'s `feed_version` (or a hash of the feed files if there is none). Versions already in the database are skipped.
* **Raw archive** (`--archive-dir`): every distinct download, exactly as received, laid out by agency and feed id from `agencies.yaml`. Realtime snapshots are gzipped under `<agency>/<feed>/YYYY/MM/DD/<UTC time>.pb.gz` (`.invalid.gz` if the body wasn't a valid GTFS-RT message), static zips under `<agency>/<feed>/<version>_<hash>.zip`. The archive is written before the database, so the database can always be rebuilt from it. `--archive-dir` works without `--db-path` for an archive-only fetcher, and then can collect any number of agencies at once (omit `--agency` to collect every agency in the config).
* **GTFS-RT**: stored as a history of changes rather than a copy of every snapshot. A row is written to `vehicle_positions`, `trip_updates`, `stop_time_updates` or `alerts` only when that vehicle, trip, stop prediction or alert changed since the previous snapshot. `message_timestamp` on each row is the feed snapshot in which the change was seen. Change detection is kept in memory, so the first snapshot after a restart is stored in full.

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
* **[config](config/)**: Loads and validates `agencies.yaml`
* **[model](model/)**: The GTFS data model
* **[csv_parse](csv_parse/)**: CSV parser. An excuse to learn about reflection in go
* **[log](log/)**: A wrapper around go's standard `log` package
* **[cmd](cmd/)**: All the entry commands for the CLI tool. Created by [cobra](https://github.com/spf13/cobra)

# gtfs-analyze
gtfs-analyze is a command line tool to analyze General Transit Feed Specification ([GTFS](https://gtfs.org/)) data. I'm learning [go](https://go.dev/) to implement the tool.

## Usage
Right now, `gtfs-analyze` has two commands: 

* `store` - watch a GTFS static feed and a GTFS-RT live feed, and log the changes to a SQLite database
* `calculate otp` - calculate the on-time performance of an agency based on the data logged in the `store` command


To start storing data for Denver's RTD system, we would run a command like this:

```bash
$ gtfs-analyze --log-level info store --db-path ~/Downloads/rtd.db --static-url https://www.rtd-denver.com/files/gtfs/google_transit.zip --vehicle-pos-url https://www.rtd-denver.com/files/gtfs-rt/VehiclePosition.pb
```

This will download the static GTFS dataset at the provided `static-url` and parse it into a SQLite database at the provided `db-path`. It will also download the GTFS-RT dataset at the provided `vehicle-pos-url`. If the dataset is already found in the database, nothing will happen. Hence, you can run this command to "watch" a GTFS feed, and keep all the historical data downloaded in a database. The poll intervals are configured by the `--rt-poll-interval` and `--static-poll-interval` options.

Then, in another process, we can analyze the on-time performance in the system for a given timerange:

```bash
$ gtfs-analyze --log-level info calculate otp --db-path ~/Downloads/rtd.db --start-time 2023-08-22T15:00:00-07:00 --end-time 2023-08-22T16:00:00-07:00
```

This will print a table with the on-time performance per trip. You can configure what is considered to be on-time with the `--threshold` flag.

For help, try `gtfs-analyze --help`.

## Packages
* **[core](core/)**: All the core logic and database interaction of the tool
* **[model](model/)**: The GTFS data model
* **[csv_parse](csv_parse/)**: CSV parser. An excuse to learn about reflection in go
* **[log](log/)**: A wrapper around go's standard `log` package
* **[cmd](cmd/)**: All the entry commands for the CLI tool. Created by [cobra](https://github.com/spf13/cobra)

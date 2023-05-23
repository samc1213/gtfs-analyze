# gtfs-analyze
gtfs-analyze is a command line tool to analyze General Transit Feed Specification ([GTFS](https://gtfs.org/)) data. I'm learning [go](https://go.dev/) to implement the tool.

## Usage
Right now, `gtfs-analyze` only has one command: `store`. Use it like this:

```bash
$ gtfs-analyze --log-level info store --db-path ~/Downloads/rtd.db --static-url https://www.rtd-denver.com/files/gtfs/google_transit.zip
```

This will download the static GTFS dataset at the provided `static-url`, and parse it into a SQLite database at the provided `db-path`. If the dataset is already found in the database, nothing will happen. Hence, you can run this command periodically to "watch" a GTFS feed, and keep all the historical data downloaded in a database.

For help, try `gtfs-analyze --help`.

## Packages
* **[core](core/)**: All the core logic and database interaction of the tool
* **[model](model/)**: The GTFS data model
* **[csv_parse](csv_parse/)**: CSV parser. An excuse to learn about reflection in go
* **[log](log/)**: A wrapper around go's standard `log` package
* **[cmd](cmd/)**: All the entry commands for the CLI tool. Created by [cobra](https://github.com/spf13/cobra)

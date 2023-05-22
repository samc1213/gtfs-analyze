# csv_parse Package
Inspired by [gocsv](https://github.com/gocarina/gocsv), this package helps simplify parsing CSVs into go structs.

## Usage
First, provide a CSV. For example, assume the file `invoice.csv` looks like this:

```csv
item_name,price_in_usd,qty
cookies,12.34,20
brownies,10.22,4
```

To use, first declare a struct that defines your CSV data model (see `InvoiceRow` below). Then, you can use the csv_parse library like so:

```go
package main

import (
	"fmt"
	"os"

	"github.com/samc1213/gtfs-analyze/csv_parse"
)

type InvoiceRow struct {
	Item     string  `csv_parse:"item_name"`
	Price    float32 `csv_parse:"price_in_usd"`
	Quantity int32   `csv_parse:"qty"`
}

func main() {
	file, err := os.Open("invoice.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	recordProvider, err := csv_parse.BeginParseCsv[InvoiceRow](file)
	if err != nil {
		panic(err)
	}

	newRecord, err := recordProvider.FetchNext()
	if err != nil {
		panic(err)
	}

	// Prints "cookies"
	fmt.Println(newRecord.Item)
}
```

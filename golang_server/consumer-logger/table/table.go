package table

import (
	"fmt"
	"os"
	"text/tabwriter"
)

type TableData struct {
	tw *tabwriter.Writer
}

func NewTableData() *TableData {
	return &TableData{
		tw: tabwriter.NewWriter(os.Stdout, 4, 4, 3, ' ', 0),
	}
}

func (td *TableData) GetTableOptimalPriceSummary(side, book string, amount, rate, value, taker_fee, maker_fee, total, price_percentage_change float64) {
	if side == "sell" {
		fmt.Fprintf(td.tw, TabHeaderOptimalAskPriceSummary.String())
	} else {
		fmt.Fprintf(td.tw, TabHeaderOptimalBidPriceSummary.String())
	}
	fmt.Fprintf(td.tw, "%s\t%f\t%f\t%f\t%f\t%f\t%f\t%f\n",
		book,
		amount,
		rate,
		value,
		taker_fee,
		maker_fee,
		total,
		price_percentage_change,
	)
	td.tw.Flush()
}

func (td *TableData) GetTableOrderPlacement(book, side, order_type, major, minor, price string) {
	fmt.Fprintf(td.tw, TabHeaderOrderPlacement.String())
	fmt.Fprintf(td.tw, "%f\t%f\t%f\t%f\t%f\t%f\t%f\n",
		book,
		side,
		order_type,
		major,
		minor,
		price,
	)
	td.tw.Flush()
}

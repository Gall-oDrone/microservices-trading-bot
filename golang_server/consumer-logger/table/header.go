package table

type TabHeader uint8

// List of table headers.
const (
	TabHeaderNone TabHeader = iota
	TabHeaderOptimalAskPriceSummary
	TabHeaderOptimalBidPriceSummary
	TabHeaderOrderPlacement
)

var headerFormats = map[TabHeader]string{
	TabHeaderOptimalAskPriceSummary: "\BOOK\tAMOUNT\tBID_RATE\tASK_VALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tBID_RATE_CHANGE(%%)\n",
	TabHeaderOptimalBidPriceSummary: "\BOOK\tAMOUNT\tASK_RATE\tBID_VALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tASK_RATE_CHANGE(%%)\n",
	TabHeaderOrderPlacement:         "BOOK\tSIDE\tTYPE\tMAJOR\tMINOR\tPRICE\n",
}

func (s TabHeader) String() string {
	if z, ok := headerFormats[s]; ok {
		return z
	}
	panic("unsupported header format")
}

package domain

const (
	GSCPageSize    = 1000
	GSCDetailLimit = 50000
)

type GSCCursor struct {
	Day           string
	TotalsChecked bool
	NextRow       int
}

type GSCProgress struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Pending   int    `json:"pending_days"`
	Complete  int    `json:"complete_days"`
	Truncated int    `json:"truncated_days"`
	Failed    int    `json:"failed_days"`
	Rows      int    `json:"rows"`
}

package domain

const (
	GSCPageSize    = 1000
	GSCDetailLimit = 50000
)

type GSCCursor struct {
	SearchType, DataState string
	Day                   string
	TotalsChecked         bool
	NextRow               int
}

type GSCReportPage struct {
	Metrics             []ExecutionMetric
	FirstIncompleteDate string
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

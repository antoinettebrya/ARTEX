package domain

// ProviderReceipt is allowlisted metadata. Its fields must never contain a
// credential, URL with query parameters, request body or raw error message.
type ProviderReceipt struct {
	Provider       string `json:"provider"`
	Operation      string `json:"operation"`
	HTTPStatus     int    `json:"http_status"`
	RequestID      string `json:"request_id,omitempty"`
	Code           string `json:"code,omitempty"`
	UpstreamCode   string `json:"upstream_code,omitempty"`
	Parameter      string `json:"parameter,omitempty"`
	Category       string `json:"category"`
	Retryable      bool   `json:"retryable"`
	MayBeCharged   bool   `json:"may_be_charged"`
	DurationMS     int64  `json:"duration_ms"`
	InputTokens    *int64 `json:"input_tokens,omitempty"`
	OutputTokens   *int64 `json:"output_tokens,omitempty"`
	CachedTokens   *int64 `json:"cached_tokens,omitempty"`
	ReservedTokens int64  `json:"reserved_tokens,omitempty"`
	ReservedMicros int64  `json:"reserved_micros,omitempty"`
	CostMicros     *int64 `json:"cost_micros,omitempty"`
	CostSource     string `json:"cost_source"`
}

type ProviderReconciliation struct {
	ProviderID       string `json:"provider_id"`
	DayUTC           string `json:"day_utc"`
	ObservedCalls    int64  `json:"observed_calls"`
	UnknownCalls     int64  `json:"unknown_calls"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CachedTokens     int64  `json:"cached_tokens"`
	ConfirmedMicros  int64  `json:"confirmed_micros"`
	EstimatedMicros  int64  `json:"estimated_micros"`
	ReservedMicros   int64  `json:"reserved_micros"`
	VendorMicros     int64  `json:"vendor_micros"`
	DifferenceMicros *int64 `json:"difference_micros"`
}

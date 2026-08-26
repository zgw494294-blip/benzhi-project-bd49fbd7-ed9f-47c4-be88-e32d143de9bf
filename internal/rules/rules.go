package rules

type RoundResult struct {
	ExchangeRatio     float64  `json:"exchangeRatio"`
	DurationMin       float64  `json:"durationMin"`
	ChlorineDeviation float64  `json:"chlorineDeviation"`
	Pass              bool     `json:"pass"`
	Message           string   `json:"message"`
	FailureReasons    []string `json:"failureReasons"`
}

type ReleaseAssessment struct {
	Eligible bool     `json:"eligible"`
	Message  string   `json:"message"`
	Blockers []string `json:"blockers"`
}

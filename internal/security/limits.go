package security

import "time"

const (
	DefaultMinAmountMSat int64 = 1_000
	DefaultMaxAmountMSat int64 = 10_000_000
	DefaultPriceMSat     int64 = 100_000

	ChallengeRatePerIPPerSecond      float64 = 5
	ChallengeBurstPerIP              float64 = 10
	ChallengeRatePerSubjectPerSecond float64 = 2
	ChallengeBurstPerSubject         float64 = 5

	HandlerTimeout = 15 * time.Second
)

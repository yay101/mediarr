package tracker

type TrackerError string

func (e TrackerError) Error() string { return string(e) }

const (
	ErrNotAuthenticated     TrackerError = "not authenticated"
	ErrAuthenticationFailed TrackerError = "authentication failed"
	ErrInvalidConfig        TrackerError = "invalid tracker configuration"
	ErrUnknownTrackerType   TrackerError = "unknown tracker type"
	ErrRateLimited          TrackerError = "rate limited by tracker"
	ErrBanned               TrackerError = "banned by tracker"
	ErrTestFailed           TrackerError = "tracker test failed"
	ErrRequestFailed        TrackerError = "tracker request failed"
	ErrNotSupported         TrackerError = "operation not supported"
)

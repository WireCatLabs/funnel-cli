package main

const (
	stateStopped       = "stopped"
	stateStarting      = "starting"
	stateNeedsLogin    = "needs-login"
	stateNeedsApproval = "needs-approval"
	stateNeedsFunnel   = "needs-funnel"
	stateRunning       = "running"
	stateError         = "error"
)

// Status keeps every key present, null when empty: Desktop parses it with one schema and no optional fields.
type Status struct {
	State string  `json:"state"`
	PID   *int    `json:"pid"`
	URL   *string `json:"url"`
	// ActionURL is the page the owner opens next; state says which: sign in, approve the device, or allow Funnel.
	ActionURL *string `json:"action_url"`
	Error     *string `json:"error"`
	Routes    []Route `json:"routes"`
	Dir       string  `json:"dir"`
	Version   string  `json:"version"`
}

func ptr[T any](v T) *T { return &v }

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func val[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

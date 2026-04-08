package dispatch

// DispatchResult holds the response from dispatching a ticket to agent-mux.
type DispatchResult struct {
	DispatchID string `json:"dispatch_id"`
	SessionID  string `json:"session_id"`
}

// StatusResult holds the response from querying agent-mux status.
type StatusResult struct {
	Status string     `json:"status"` // running, completed, failed, timeout
	Error  string     `json:"error"`
	Tokens *TokenData `json:"tokens"`
}

type TokenData struct {
	In          int `json:"in"`
	Out         int `json:"out"`
	Cache       int `json:"cache"`
	PeakContext int `json:"peak_context"`
}

// DispatchOptions holds the parameters for a dispatch call.
type DispatchOptions struct {
	Profile    string
	Engine     string
	Model      string
	Effort     string
	WorkDir    string
	TicketPath string   // absolute path to the ticket card file
	Preamble   string   // optional preamble for retry context
	Skills     []string // skill names to pass to agent-mux
}

// Dispatcher is the interface for dispatching tickets to an execution backend.
type Dispatcher interface {
	// Dispatch sends a ticket to the execution backend and returns dispatch info.
	Dispatch(opts DispatchOptions) (*DispatchResult, error)

	// Status queries the current state of a dispatch by ID.
	Status(dispatchID string) (*StatusResult, error)
}

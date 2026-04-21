package dispatch

// DispatchResult holds the response from dispatching a ticket to agent-mux.
type DispatchResult struct {
	Kind       string `json:"kind"`
	DispatchID string `json:"dispatch_id"`
	SessionID  string `json:"session_id"`
}

// StatusResult holds the response from querying agent-mux status.
type StatusResult struct {
	Status    string `json:"status"` // running, completed, failed, timeout
	State     string `json:"state"`  // agent-mux minimal schema uses "state" instead of "status"
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// EffectiveStatus returns the resolved status, preferring the full-schema
// "status" field but falling back to the minimal-schema "state" field when
// "status" is empty.
func (s *StatusResult) EffectiveStatus() string {
	if s.Status != "" {
		return s.Status
	}
	return s.State
}

// OptionSource indicates where a resolved option value came from.
type OptionSource string

const (
	SourceCLI        OptionSource = "cli"        // explicit --flag on the command line
	SourceCard       OptionSource = "card"       // ticket card frontmatter
	SourceInitiative OptionSource = "initiative" // initiative default_profile
	SourceConfig     OptionSource = "config"     // .tickets.toml global defaults
	SourceNone       OptionSource = "none"       // not set at all
)

// DispatchOptions holds the parameters for a dispatch call.
type DispatchOptions struct {
	Profile    string
	Engine     string
	Model      string
	Effort     string
	WorkDir    string
	Skills     []string // skill names passed via --skill flags
	TicketPath string   // absolute path to the ticket card file
	Preamble   string   // optional preamble for retry context

	// Sources track where each resolved value came from, so the dispatch
	// layer can decide whether to pass engine/model/effort flags or let
	// the profile define them.
	ProfileSource OptionSource
	EngineSource  OptionSource
	ModelSource   OptionSource
	EffortSource  OptionSource
}

// Dispatcher is the interface for dispatching tickets to an execution backend.
type Dispatcher interface {
	// Dispatch sends a ticket to the execution backend and returns dispatch info.
	Dispatch(opts DispatchOptions) (*DispatchResult, error)

	// Status queries the current state of a dispatch by ID.
	Status(dispatchID string) (*StatusResult, error)
}

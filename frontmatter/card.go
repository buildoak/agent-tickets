package frontmatter

type Status string

const (
	StatusOpen       Status = "open"
	StatusDispatched Status = "dispatched"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
	StatusBlocked    Status = "blocked"
	StatusClosed     Status = "closed"
)

// IsTerminal returns true if the status is a terminal state (done, failed, blocked, closed).
// Terminal states are used by the awaits dependency semantic — a ticket awaiting
// another only needs the awaited ticket to reach any terminal state, not necessarily done.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusDone, StatusFailed, StatusBlocked, StatusClosed:
		return true
	default:
		return false
	}
}

type Tier string

const (
	TierWorker Tier = "worker"
	TierDeep   Tier = "deep"
	TierHeavy  Tier = "heavy"
)

type Card struct {
	ID         string   `yaml:"id"`
	Initiative string   `yaml:"initiative"`
	Title      string   `yaml:"title"`
	Status     Status   `yaml:"status"`
	Tier       Tier     `yaml:"tier"`
	Tags       []string `yaml:"tags"`
	Created    string   `yaml:"created"`
	Manual     bool     `yaml:"manual"`

	PlanRef *string `yaml:"plan_ref"`

	DependsOn []string `yaml:"depends_on"`
	Awaits    []string `yaml:"awaits"`

	Skills []string `yaml:"skills"`

	DispatchID   *string `yaml:"dispatch_id"`
	SessionID    *string `yaml:"session_id"`
	DispatchedAt *string `yaml:"dispatched_at"`
	Profile      *string `yaml:"profile"`
	Engine       *string `yaml:"engine"`
	Model        *string `yaml:"model"`
	Effort       *string `yaml:"effort"`

	Attempts           int     `yaml:"attempts"`
	LastAttemptOutcome *string `yaml:"last_attempt_outcome"`
	BlockReason        *string `yaml:"block_reason"`

	DefaultProfile *string  `yaml:"default_profile"`
	DefaultSkills  []string `yaml:"default_skills"`
}

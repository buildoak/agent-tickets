package frontmatter

type Status string

const (
	StatusOpen       Status = "open"
	StatusDispatched Status = "dispatched"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
	StatusBlocked    Status = "blocked"
)

type Tier string

const (
	TierWorker Tier = "worker"
	TierDeep   Tier = "deep"
	TierHeavy  Tier = "heavy"
)

type TokenUsage struct {
	In          int `yaml:"in"`
	Out         int `yaml:"out"`
	Cache       int `yaml:"cache"`
	PeakContext int `yaml:"peak_context"`
}

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

	DispatchID *string `yaml:"dispatch_id"`
	SessionID  *string `yaml:"session_id"`
	Profile    *string `yaml:"profile"`
	Engine     *string `yaml:"engine"`
	Model      *string `yaml:"model"`
	Effort     *string `yaml:"effort"`
	WorkDir    *string  `yaml:"work_dir"`
	Skills     []string `yaml:"skills"`

	Attempts           int     `yaml:"attempts"`
	LastAttemptOutcome *string `yaml:"last_attempt_outcome"`
	BlockReason        *string `yaml:"block_reason"`

	Tokens *TokenUsage `yaml:"tokens"`
}

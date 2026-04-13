package fsm

import (
	"testing"

	"github.com/buildoak/agent-tickets/frontmatter"
)

func TestApplyValidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from frontmatter.Status
		tr   Transition
		to   frontmatter.Status
	}{
		{
			name: "open dispatch dispatched",
			from: frontmatter.StatusOpen,
			tr:   TransitionDispatch,
			to:   frontmatter.StatusDispatched,
		},
		{
			name: "open block blocked",
			from: frontmatter.StatusOpen,
			tr:   TransitionBlock,
			to:   frontmatter.StatusBlocked,
		},
		{
			name: "dispatched complete done",
			from: frontmatter.StatusDispatched,
			tr:   TransitionComplete,
			to:   frontmatter.StatusDone,
		},
		{
			name: "dispatched fail failed",
			from: frontmatter.StatusDispatched,
			tr:   TransitionFail,
			to:   frontmatter.StatusFailed,
		},
		{
			name: "dispatched cancel open",
			from: frontmatter.StatusDispatched,
			tr:   TransitionCancel,
			to:   frontmatter.StatusOpen,
		},
		{
			name: "failed reopen open",
			from: frontmatter.StatusFailed,
			tr:   TransitionReopen,
			to:   frontmatter.StatusOpen,
		},
		{
			name: "failed block blocked",
			from: frontmatter.StatusFailed,
			tr:   TransitionBlock,
			to:   frontmatter.StatusBlocked,
		},
		{
			name: "blocked reopen open",
			from: frontmatter.StatusBlocked,
			tr:   TransitionReopen,
			to:   frontmatter.StatusOpen,
		},
		{
			name: "done reopen open",
			from: frontmatter.StatusDone,
			tr:   TransitionReopen,
			to:   frontmatter.StatusOpen,
		},
		{
			name: "open close closed",
			from: frontmatter.StatusOpen,
			tr:   TransitionClose,
			to:   frontmatter.StatusClosed,
		},
		{
			name: "failed close closed",
			from: frontmatter.StatusFailed,
			tr:   TransitionClose,
			to:   frontmatter.StatusClosed,
		},
		{
			name: "done close closed",
			from: frontmatter.StatusDone,
			tr:   TransitionClose,
			to:   frontmatter.StatusClosed,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Apply(tt.from, tt.tr)
			if err != nil {
				t.Fatalf("Apply(%q, %q) returned error: %v", tt.from, tt.tr, err)
			}

			if got.From != tt.from {
				t.Fatalf("From = %q, want %q", got.From, tt.from)
			}
			if got.To != tt.to {
				t.Fatalf("To = %q, want %q", got.To, tt.to)
			}
			if got.Transition != tt.tr {
				t.Fatalf("Transition = %q, want %q", got.Transition, tt.tr)
			}
		})
	}
}

func TestApplyInvalidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from frontmatter.Status
		tr   Transition
	}{
		{
			name: "open complete",
			from: frontmatter.StatusOpen,
			tr:   TransitionComplete,
		},
		{
			name: "open fail",
			from: frontmatter.StatusOpen,
			tr:   TransitionFail,
		},
		{
			name: "dispatched block",
			from: frontmatter.StatusDispatched,
			tr:   TransitionBlock,
		},
		{
			name: "done dispatch",
			from: frontmatter.StatusDone,
			tr:   TransitionDispatch,
		},
		{
			name: "done fail",
			from: frontmatter.StatusDone,
			tr:   TransitionFail,
		},
		{
			name: "done block",
			from: frontmatter.StatusDone,
			tr:   TransitionBlock,
		},
		{
			name: "blocked dispatch",
			from: frontmatter.StatusBlocked,
			tr:   TransitionDispatch,
		},
		{
			name: "blocked complete",
			from: frontmatter.StatusBlocked,
			tr:   TransitionComplete,
		},
		{
			name: "blocked fail",
			from: frontmatter.StatusBlocked,
			tr:   TransitionFail,
		},
		{
			name: "failed dispatch",
			from: frontmatter.StatusFailed,
			tr:   TransitionDispatch,
		},
		{
			name: "failed complete",
			from: frontmatter.StatusFailed,
			tr:   TransitionComplete,
		},
		{
			name: "dispatched close",
			from: frontmatter.StatusDispatched,
			tr:   TransitionClose,
		},
		{
			name: "blocked close",
			from: frontmatter.StatusBlocked,
			tr:   TransitionClose,
		},
		{
			name: "closed dispatch",
			from: frontmatter.StatusClosed,
			tr:   TransitionDispatch,
		},
		{
			name: "closed reopen",
			from: frontmatter.StatusClosed,
			tr:   TransitionReopen,
		},
		{
			name: "closed close",
			from: frontmatter.StatusClosed,
			tr:   TransitionClose,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Apply(tt.from, tt.tr); err == nil {
				t.Fatalf("Apply(%q, %q) expected error, got nil", tt.from, tt.tr)
			}
		})
	}
}

func TestApplyReopenFromFailedSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusFailed, TransitionReopen)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if !got.IncrementAttempts {
		t.Fatalf("IncrementAttempts = false, want true")
	}
	if !got.ClearDispatchFields {
		t.Fatalf("ClearDispatchFields = false, want true")
	}
}

func TestApplyReopenFromDoneSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusDone, TransitionReopen)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if !got.ArchiveResult {
		t.Fatalf("ArchiveResult = false, want true")
	}
	if !got.ClearDispatchFields {
		t.Fatalf("ClearDispatchFields = false, want true")
	}
	if got.IncrementAttempts {
		t.Fatalf("IncrementAttempts = true, want false")
	}
}

func TestApplyReopenFromBlockedSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusBlocked, TransitionReopen)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if !got.ClearBlockReason {
		t.Fatalf("ClearBlockReason = false, want true")
	}
	if !got.ClearDispatchFields {
		t.Fatalf("ClearDispatchFields = false, want true")
	}
}

func TestApplyCancelSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusDispatched, TransitionCancel)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got.SetLastOutcome != "cancelled" {
		t.Fatalf("SetLastOutcome = %q, want %q", got.SetLastOutcome, "cancelled")
	}
	if !got.ClearDispatchFields {
		t.Fatalf("ClearDispatchFields = false, want true")
	}
	if got.IncrementAttempts {
		t.Fatalf("IncrementAttempts = true, want false")
	}
}

func TestApplyFailSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusDispatched, TransitionFail)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if got.SetLastOutcome != "failed" {
		t.Fatalf("SetLastOutcome = %q, want %q", got.SetLastOutcome, "failed")
	}
}

func TestApplyBlockFromOpenSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusOpen, TransitionBlock)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if !got.SetBlockReason {
		t.Fatalf("SetBlockReason = false, want true")
	}
}

func TestApplyBlockFromFailedSideEffects(t *testing.T) {
	t.Parallel()

	got, err := Apply(frontmatter.StatusFailed, TransitionBlock)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	if !got.SetBlockReason {
		t.Fatalf("SetBlockReason = false, want true")
	}
}

func TestValidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from frontmatter.Status
		want []Transition
	}{
		{
			name: "open",
			from: frontmatter.StatusOpen,
			want: []Transition{TransitionDispatch, TransitionBlock, TransitionClose},
		},
		{
			name: "dispatched",
			from: frontmatter.StatusDispatched,
			want: []Transition{TransitionComplete, TransitionFail, TransitionCancel},
		},
		{
			name: "done",
			from: frontmatter.StatusDone,
			want: []Transition{TransitionReopen, TransitionClose},
		},
		{
			name: "failed",
			from: frontmatter.StatusFailed,
			want: []Transition{TransitionReopen, TransitionBlock, TransitionClose},
		},
		{
			name: "blocked",
			from: frontmatter.StatusBlocked,
			want: []Transition{TransitionReopen},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ValidTransitions(tt.from)
			if len(got) != len(tt.want) {
				t.Fatalf("len(ValidTransitions(%q)) = %d, want %d", tt.from, len(got), len(tt.want))
			}

			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("ValidTransitions(%q)[%d] = %q, want %q", tt.from, i, got[i], tt.want[i])
				}
			}
		})
	}
}

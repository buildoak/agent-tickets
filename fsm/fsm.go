// Package fsm implements the ticket state machine with enforced transitions.
package fsm

import (
	"fmt"

	"github.com/buildoak/agent-tickets/frontmatter"
)

type Transition string

const (
	TransitionDispatch Transition = "dispatch"
	TransitionComplete Transition = "complete"
	TransitionFail     Transition = "fail"
	TransitionCancel   Transition = "cancel"
	TransitionReopen   Transition = "reopen"
	TransitionBlock    Transition = "block"
	TransitionClose    Transition = "close"
)

type TransitionResult struct {
	From                frontmatter.Status
	To                  frontmatter.Status
	Transition          Transition
	IncrementAttempts   bool
	ClearDispatchFields bool
	ClearBlockReason    bool
	SetBlockReason      bool
	SetLastOutcome      string
	ClearLastOutcome    bool
	ArchiveResult       bool
}

type transitionDefinition struct {
	to                  frontmatter.Status
	incrementAttempts   bool
	clearDispatchFields bool
	clearBlockReason    bool
	setBlockReason      bool
	setLastOutcome      string
	clearLastOutcome    bool
	archiveResult       bool
}

var transitions = map[frontmatter.Status]map[Transition]transitionDefinition{
	frontmatter.StatusOpen: {
		TransitionDispatch: {
			to:               frontmatter.StatusDispatched,
			clearLastOutcome: true,
		},
		TransitionBlock: {
			to:             frontmatter.StatusBlocked,
			setBlockReason: true,
		},
		TransitionClose: {
			to:                  frontmatter.StatusClosed,
			clearDispatchFields: true,
		},
	},
	frontmatter.StatusDispatched: {
		TransitionComplete: {
			to: frontmatter.StatusDone,
		},
		TransitionFail: {
			to:             frontmatter.StatusFailed,
			setLastOutcome: "failed",
		},
		TransitionCancel: {
			to:                  frontmatter.StatusOpen,
			clearDispatchFields: true,
			setLastOutcome:      "cancelled",
		},
	},
	frontmatter.StatusFailed: {
		TransitionReopen: {
			to:                  frontmatter.StatusOpen,
			incrementAttempts:   true,
			clearDispatchFields: true,
			archiveResult:       true,
		},
		TransitionBlock: {
			to:             frontmatter.StatusBlocked,
			setBlockReason: true,
		},
		TransitionClose: {
			to:                  frontmatter.StatusClosed,
			clearDispatchFields: true,
		},
	},
	frontmatter.StatusBlocked: {
		TransitionReopen: {
			to:                  frontmatter.StatusOpen,
			clearDispatchFields: true,
			clearBlockReason:    true,
		},
	},
	frontmatter.StatusDone: {
		TransitionReopen: {
			to:                  frontmatter.StatusOpen,
			clearDispatchFields: true,
			archiveResult:       true,
		},
		TransitionClose: {
			to:                  frontmatter.StatusClosed,
			clearDispatchFields: true,
			archiveResult:       true,
		},
	},
}

var validTransitions = map[frontmatter.Status][]Transition{
	frontmatter.StatusOpen:       {TransitionDispatch, TransitionBlock, TransitionClose},
	frontmatter.StatusDispatched: {TransitionComplete, TransitionFail, TransitionCancel},
	frontmatter.StatusDone:       {TransitionReopen, TransitionClose},
	frontmatter.StatusFailed:     {TransitionReopen, TransitionBlock, TransitionClose},
	frontmatter.StatusBlocked:    {TransitionReopen},
}

func Apply(current frontmatter.Status, t Transition) (TransitionResult, error) {
	definitions, ok := transitions[current]
	if !ok {
		return TransitionResult{}, fmt.Errorf("invalid transition: cannot %s from %s", t, current)
	}

	definition, ok := definitions[t]
	if !ok {
		return TransitionResult{}, fmt.Errorf("invalid transition: cannot %s from %s", t, current)
	}

	return TransitionResult{
		From:                current,
		To:                  definition.to,
		Transition:          t,
		IncrementAttempts:   definition.incrementAttempts,
		ClearDispatchFields: definition.clearDispatchFields,
		ClearBlockReason:    definition.clearBlockReason,
		SetBlockReason:      definition.setBlockReason,
		SetLastOutcome:      definition.setLastOutcome,
		ClearLastOutcome:    definition.clearLastOutcome,
		ArchiveResult:       definition.archiveResult,
	}, nil
}

func ValidTransitions(current frontmatter.Status) []Transition {
	transitions, ok := validTransitions[current]
	if !ok {
		return nil
	}

	return append([]Transition(nil), transitions...)
}

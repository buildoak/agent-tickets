package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
)

func TestCreateAndShow(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha initiative")
	out := mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "First ticket", "--tier", "worker")
	id := strings.TrimSpace(out)

	if id != "ALPHA-001" {
		t.Fatalf("unexpected ticket id: %s", id)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Title != "First ticket" {
		t.Fatalf("unexpected title: %s", doc.Card.Title)
	}
	if doc.Card.Status != frontmatter.StatusOpen {
		t.Fatalf("unexpected status: %s", doc.Card.Status)
	}

	showOut := mustRunStdout(t, "show", id, "--json")
	var entry BoardEntry
	if err := json.Unmarshal([]byte(showOut), &entry); err != nil {
		t.Fatalf("unmarshal show json: %v", err)
	}
	if entry.ID != id {
		t.Fatalf("unexpected card id from show: %s", entry.ID)
	}
	if entry.Tier != frontmatter.TierWorker {
		t.Fatalf("unexpected card tier: %s", entry.Tier)
	}
	if entry.Annotation != "queued" {
		t.Fatalf("unexpected annotation from show: %s", entry.Annotation)
	}
}

func TestListFiltering(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	mustRun(t, "init", "BETA", "--title", "Beta")

	alpha1 := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "One", "--tier", "worker"))
	alpha2 := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Two", "--tier", "deep"))
	beta1 := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BETA", "--title", "Three", "--tier", "heavy"))

	addTag(t, baseDir, alpha1, "blue")
	addTag(t, baseDir, alpha2, "red")
	addTag(t, baseDir, beta1, "blue")

	mustRun(t, "dispatch", alpha2, "--engine", "codex", "--model", "gpt-5.4-mini", "--effort", "high")

	initOut := mustRunStdout(t, "list", "--initiative", "ALPHA")
	if !strings.Contains(initOut, alpha1) || !strings.Contains(initOut, alpha2) || strings.Contains(initOut, beta1) {
		t.Fatalf("initiative filter output mismatch:\n%s", initOut)
	}

	statusOut := mustRunStdout(t, "list", "--status", "dispatched")
	if !strings.Contains(statusOut, alpha2) || strings.Contains(statusOut, alpha1) {
		t.Fatalf("status filter output mismatch:\n%s", statusOut)
	}

	tagJSON := mustRunStdout(t, "list", "--tag", "blue", "--json")
	var entries []BoardEntry
	if err := json.Unmarshal([]byte(tagJSON), &entries); err != nil {
		t.Fatalf("unmarshal list json: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 blue-tag cards, got %d", len(entries))
	}
	for _, entry := range entries {
		if entry.Annotation == "" {
			t.Fatalf("expected annotation on list entry %s", entry.ID)
		}
	}
}

func TestBoard(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Open ticket", "--tier", "worker"))
	strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Depends on A", "--tier", "deep", "--depends-on", a))
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Manual ticket", "--tier", "worker", "--manual")

	out := mustRunStdout(t, "board")
	if !strings.Contains(out, "queued") {
		t.Fatalf("board should show queued for open ticket: %s", out)
	}
	if !strings.Contains(out, "waiting") {
		t.Fatalf("board should show waiting for ticket with unresolved deps: %s", out)
	}
	if !strings.Contains(out, "manual") {
		t.Fatalf("board should show manual annotation: %s", out)
	}

	jsonOut := mustRunStdout(t, "board", "--json")
	var entries []BoardEntry
	if err := json.Unmarshal([]byte(jsonOut), &entries); err != nil {
		t.Fatalf("unmarshal board json: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	mustRun(t, "init", "BETA", "--title", "Beta")
	mustRunStdout(t, "create", "--initiative", "BETA", "--title", "Beta ticket", "--tier", "worker")
	filteredOut := mustRunStdout(t, "board", "--initiative", "ALPHA")
	if strings.Contains(filteredOut, "BETA") {
		t.Fatalf("initiative filter should exclude BETA: %s", filteredOut)
	}
}

func TestInitiatives(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha initiative")
	mustRun(t, "init", "BETA", "--title", "Beta initiative")

	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A1", "--tier", "worker")
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A2", "--tier", "worker")
	mustRunStdout(t, "create", "--initiative", "BETA", "--title", "B1", "--tier", "worker")

	out := mustRunStdout(t, "initiatives")
	if !strings.Contains(out, "ALPHA") || !strings.Contains(out, "BETA") {
		t.Fatalf("initiatives should list both: %s", out)
	}

	jsonOut := mustRunStdout(t, "initiatives", "--json")
	var infos []InitiativeInfo
	if err := json.Unmarshal([]byte(jsonOut), &infos); err != nil {
		t.Fatalf("unmarshal initiatives json: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 initiatives, got %d", len(infos))
	}

	for _, info := range infos {
		if info.Initiative == "ALPHA" && info.Tickets["open"] != 2 {
			t.Fatalf("expected 2 open tickets for ALPHA, got %d", info.Tickets["open"])
		}
	}

	filtered := mustRunStdout(t, "initiatives", "--status", "active")
	if !strings.Contains(filtered, "ALPHA") || !strings.Contains(filtered, "BETA") {
		t.Fatalf("status filter should keep active initiatives: %s", filtered)
	}
}

func TestFullLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Lifecycle", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4", "--effort", "high")
	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected dispatched, got %s", doc.Card.Status)
	}

	doc.SetSection("Result", "Completed work.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "complete", id)
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDone {
		t.Fatalf("expected done, got %s", doc.Card.Status)
	}
}

func TestFailAndReopen(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Retry me", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "agent timed out")
	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusFailed {
		t.Fatalf("expected failed, got %s", doc.Card.Status)
	}

	mustRun(t, "reopen", id)
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected open, got %s", doc.Card.Status)
	}
	if doc.Card.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", doc.Card.Attempts)
	}

	// Card-spec dispatch fields are cleared on reopen so the ticket
	// can be re-dispatched with fresh parameters.
	if doc.Card.Engine != nil {
		t.Fatalf("expected engine cleared, got %#v", doc.Card.Engine)
	}
	if doc.Card.Model != nil {
		t.Fatalf("expected model cleared, got %#v", doc.Card.Model)
	}
}

func TestBlockAndReopen(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Blocked", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "bad output")
	mustRun(t, "block", id, "--reason", "needs human input")
	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusBlocked {
		t.Fatalf("expected blocked, got %s", doc.Card.Status)
	}
	if doc.Card.BlockReason == nil || *doc.Card.BlockReason != "needs human input" {
		t.Fatalf("unexpected block reason: %#v", doc.Card.BlockReason)
	}

	mustRun(t, "reopen", id)
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected open after reopen, got %s", doc.Card.Status)
	}
	if doc.Card.BlockReason != nil {
		t.Fatalf("expected cleared block reason, got %#v", doc.Card.BlockReason)
	}
}

func TestCancelPreservesPartialWork(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Cancel me", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	doc := mustParseTicket(t, baseDir, id)
	doc.SetSection("Result", "Partial draft.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "cancel", id, "--reason", "manual intervention")
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected open after cancel, got %s", doc.Card.Status)
	}
	if !strings.Contains(doc.GetSection("Result"), "Partial draft.") {
		t.Fatalf("expected result content preserved")
	}
}

func TestCompleteRequiresResult(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Need result", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	err := run([]string{"complete", id})
	if err == nil || !strings.Contains(err.Error(), "## Result section is empty") {
		t.Fatalf("expected result-section error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Write your result") {
		t.Fatalf("expected instructional message, got %v", err)
	}
}

func TestCompleteRejectsPlaceholderResult(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Placeholder result", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	placeholders := []string{
		"[Filled by the executing agent...]",
		"[To be filled after execution]",
		"TODO",
		"[Placeholder]",
	}
	for _, ph := range placeholders {
		doc := mustParseTicket(t, baseDir, id)
		doc.SetSection("Result", ph+"\n")
		writeTicket(t, baseDir, id, doc)

		err := run([]string{"complete", id})
		if err == nil {
			t.Fatalf("expected rejection for placeholder %q, got nil", ph)
		}
		if !strings.Contains(err.Error(), "placeholder") {
			t.Fatalf("expected placeholder error for %q, got: %v", ph, err)
		}
	}
}

func TestBuildcileIgnoresPlaceholderResult(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "completed"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Placeholder reconcile", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	doc := mustParseTicket(t, baseDir, id)
	doc.SetSection("Result", "[Filled by the executing agent...]\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "reconcile")

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusFailed {
		t.Fatalf("expected failed for placeholder result, got %s", doc.Card.Status)
	}
}

func TestMaxDependencyDepthNoPanicOnCycle(t *testing.T) {
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {"A"},
	}

	depth, chain := maxDependencyDepth(graph, "A", nil)
	if depth < 1 {
		t.Fatalf("expected positive depth, got %d", depth)
	}
	if len(chain) == 0 {
		t.Fatalf("expected non-empty chain, got empty")
	}
	// Must not panic -- that's the main assertion. If we got here, no stack overflow.
	_ = depth
	_ = chain
}

func TestMaxDependencyDepthDisjointCycle(t *testing.T) {
	// A depends on B, B depends on C (no cycle through A).
	// But D->E->D forms a disjoint cycle.
	// Walking from A should terminate without panic.
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {},
		"D": {"E"},
		"E": {"D"},
	}

	depth, _ := maxDependencyDepth(graph, "A", nil)
	if depth != 3 {
		t.Fatalf("expected depth 3 for A->B->C, got %d", depth)
	}

	// Walking from D should also terminate
	depth2, _ := maxDependencyDepth(graph, "D", nil)
	if depth2 < 1 {
		t.Fatalf("expected positive depth for cycle D->E->D, got %d", depth2)
	}
}

func TestCompleteRejectsNonDispatched(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Open", "--tier", "worker"))

	err := run([]string{"complete", id})
	if err == nil || !strings.Contains(err.Error(), "must be dispatched") {
		t.Fatalf("expected state error, got %v", err)
	}
}

func TestDispatchRequiresScope(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Missing scope", "--tier", "worker"))

	err := run([]string{"dispatch", id, "--engine", "codex", "--model", "gpt-5.4"})
	if err == nil || !strings.Contains(err.Error(), "## Scope") {
		t.Fatalf("expected scope gate error, got %v", err)
	}
}

func TestFailRejectsNonDispatched(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Open", "--tier", "worker"))

	err := run([]string{"fail", id, "--reason", "test"})
	if err == nil || !strings.Contains(err.Error(), "must be dispatched") {
		t.Fatalf("expected state error, got %v", err)
	}
}

func TestCancelRejectsNonDispatched(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Open", "--tier", "worker"))

	err := run([]string{"cancel", id, "--reason", "test"})
	if err == nil || !strings.Contains(err.Error(), "must be dispatched") {
		t.Fatalf("expected state error, got %v", err)
	}
}

func TestBlockRejectsDispatched(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Dispatched", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	err := run([]string{"block", id, "--reason", "test"})
	if err == nil || !strings.Contains(err.Error(), "must be open or failed") {
		t.Fatalf("expected state error, got %v", err)
	}
}

func TestReopenRejectsOpen(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Open", "--tier", "worker"))

	err := run([]string{"reopen", id})
	if err == nil || !strings.Contains(err.Error(), "invalid transition") {
		t.Fatalf("expected transition error, got %v", err)
	}
}

func TestDispatchRejectsNonOpen(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Test", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	err := run([]string{"dispatch", id, "--engine", "codex", "--model", "gpt-5.4"})
	if err == nil || !strings.Contains(err.Error(), "must be open") {
		t.Fatalf("expected state error, got %v", err)
	}
}

func TestShowMissingTicket(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")

	err := run([]string{"show", "ALPHA-999"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestShowJSONIncludesAnnotation(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Test", "--tier", "worker"))

	out := mustRunStdout(t, "show", id, "--json")
	var entry BoardEntry
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		t.Fatalf("unmarshal show json: %v", err)
	}
	if entry.Annotation != "queued" {
		t.Fatalf("expected annotation 'queued', got %q", entry.Annotation)
	}
}

func TestListJSONIncludesAnnotations(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker")
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker")

	out := mustRunStdout(t, "list", "--json")
	var entries []BoardEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal list json: %v", err)
	}
	for _, entry := range entries {
		if entry.Annotation == "" {
			t.Fatalf("expected annotation on list entry %s", entry.ID)
		}
	}
}

func TestDispatchUsesRealAdapter(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if !filepath.IsAbs(opts.TicketPath) {
				t.Fatalf("expected absolute ticket path, got %q", opts.TicketPath)
			}
			return &dispatch.DispatchResult{
				DispatchID: "real-dispatch-123",
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Dispatch me", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4", "--effort", "high")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.DispatchID == nil || *doc.Card.DispatchID != "real-dispatch-123" {
		t.Fatalf("unexpected dispatch id: %#v", doc.Card.DispatchID)
	}
	// session_id is not in the async_started response; should be nil.
	if doc.Card.SessionID != nil {
		t.Fatalf("expected nil session_id from async dispatch, got %#v", doc.Card.SessionID)
	}
}

func TestDispatchWritesDispatchIDAtomically(t *testing.T) {
	// Verifies the new dispatch flow: dispatch_id is on the card in the same
	// write as status=dispatched (before the worker would start reading).
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	var cardAtDispatchTime *frontmatter.Document
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			// Capture the card state at dispatch time (before the CLI writes).
			// In the new flow, the card is still open here because dispatch
			// happens BEFORE the card write.
			doc, err := frontmatter.ParseFile(opts.TicketPath)
			if err != nil {
				t.Fatalf("read card at dispatch time: %v", err)
			}
			cardAtDispatchTime = doc
			return &dispatch.DispatchResult{DispatchID: "atomic-dispatch-123"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Atomic dispatch", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4", "--effort", "high")

	// The card at dispatch time should still be open (dispatch happens first).
	if cardAtDispatchTime == nil {
		t.Fatal("dispatch mock was not called")
	}
	if cardAtDispatchTime.Card.Status != frontmatter.StatusOpen {
		t.Fatalf("card at dispatch time should be open, got %s", cardAtDispatchTime.Card.Status)
	}

	// After dispatch returns, the card should have dispatch_id + status in one write.
	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected dispatched, got %s", doc.Card.Status)
	}
	if doc.Card.DispatchID == nil || *doc.Card.DispatchID != "atomic-dispatch-123" {
		t.Fatalf("expected dispatch_id atomic-dispatch-123, got %#v", doc.Card.DispatchID)
	}
	// session_id should be nil (not in async response).
	if doc.Card.SessionID != nil {
		t.Fatalf("expected nil session_id, got %#v", doc.Card.SessionID)
	}
	// Log should contain dispatch entry.
	logSection := doc.GetSection("Log")
	if !strings.Contains(logSection, "dispatched --") || !strings.Contains(logSection, "dispatch_id=atomic-dispatch-123") {
		t.Fatalf("dispatch log entry missing or malformed: %q", logSection)
	}
}

func TestDispatchClearsLastAttemptOutcome(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Outcome cleared", "--tier", "worker"))

	// Dispatch, fail (sets last_attempt_outcome), reopen.
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "first failure")
	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.LastAttemptOutcome == nil || *doc.Card.LastAttemptOutcome != "failed" {
		t.Fatalf("expected last_attempt_outcome=failed after fail, got %#v", doc.Card.LastAttemptOutcome)
	}

	mustRun(t, "reopen", id)
	// After reopen, last_attempt_outcome should still be set (reopen doesn't clear it).
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.LastAttemptOutcome == nil || *doc.Card.LastAttemptOutcome != "failed" {
		t.Fatalf("expected last_attempt_outcome=failed after reopen, got %#v", doc.Card.LastAttemptOutcome)
	}

	// Re-dispatch should clear the stale outcome.
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.LastAttemptOutcome != nil {
		t.Fatalf("expected last_attempt_outcome cleared on dispatch, got %#v", doc.Card.LastAttemptOutcome)
	}
}

func TestBatchDispatch(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	first := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "First", "--tier", "worker"))
	second := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Second", "--tier", "worker"))

	mustRun(t, "dispatch", first+","+second, "--engine", "codex", "--model", "gpt-5.4")

	for _, id := range []string{first, second} {
		doc := mustParseTicket(t, baseDir, id)
		if doc.Card.Status != frontmatter.StatusDispatched {
			t.Fatalf("%s status = %s, want dispatched", id, doc.Card.Status)
		}
	}
}

func TestDispatchMissingEngineModel(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withWorkingDir(t, baseDir) // isolate from project .tickets.toml
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Missing defaults", "--tier", "worker"))

	err := run([]string{"dispatch", id})
	if err == nil || !strings.Contains(err.Error(), "dispatch requires engine and model") {
		t.Fatalf("expected missing engine/model error, got %v", err)
	}
}

func TestDispatchConfigDefaults(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"retry\"\n")
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if opts.Engine != "codex" || opts.Model != "gpt-5.4-mini" || opts.Effort != "medium" || opts.Profile != "retry" {
				t.Fatalf("unexpected dispatch options: %#v", opts)
			}
			return &dispatch.DispatchResult{DispatchID: "cfg-dispatch"}, nil
		},
	})

	withWorkingDir(t, baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Use config", "--tier", "worker"))

	mustRun(t, "dispatch", id)
}

func TestDispatchTicketFrontmatterOverridesConfig(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"cfg-engine\"\nmodel = \"cfg-model\"\neffort = \"cfg-effort\"\nprofile = \"cfg-profile\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if opts.Profile != "card-profile" || opts.Engine != "card-engine" || opts.Model != "card-model" || opts.Effort != "card-effort" {
				t.Fatalf("unexpected dispatch options: %#v", opts)
			}
			return &dispatch.DispatchResult{DispatchID: "card-dispatch"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Use card values", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Profile = stringPtr("card-profile")
	doc.Card.Engine = stringPtr("card-engine")
	doc.Card.Model = stringPtr("card-model")
	doc.Card.Effort = stringPtr("card-effort")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "dispatch", id)
}

func TestDispatchCLIOverridesTicketFrontmatter(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"cfg-engine\"\nmodel = \"cfg-model\"\nprofile = \"cfg-profile\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if opts.Profile != "cli-profile" || opts.Engine != "cli-engine" || opts.Model != "cli-model" || opts.Effort != "cli-effort" {
				t.Fatalf("unexpected dispatch options: %#v", opts)
			}
			return &dispatch.DispatchResult{DispatchID: "cli-dispatch"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "CLI wins", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Profile = stringPtr("card-profile")
	doc.Card.Engine = stringPtr("card-engine")
	doc.Card.Model = stringPtr("card-model")
	doc.Card.Effort = stringPtr("card-effort")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "dispatch", id, "--profile", "cli-profile", "--engine", "cli-engine", "--model", "cli-model", "--effort", "cli-effort")
}

func TestDispatchInitiativeProfileOmitsConfigEngine(t *testing.T) {
	// When profile comes from initiative default_profile and engine/model/effort
	// come from config defaults, the dispatch should NOT pass engine flags to
	// agent-mux — let the profile define them.
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"xhigh\"\nprofile = \"ticket-worker\"\n")
	withWorkingDir(t, baseDir)

	var gotOpts dispatch.DispatchOptions
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			gotOpts = opts
			return &dispatch.DispatchResult{DispatchID: "init-dispatch"}, nil
		},
	})

	mustRun(t, "init", "RESEARCH", "--title", "Paper operations")

	// Patch the initiative file to include default_profile.
	initPath := filepath.Join(baseDir, "INITIATIVES", "RESEARCH.md")
	initContent := fmt.Sprintf("---\ninitiative: RESEARCH\ntitle: \"Paper operations\"\nstatus: active\ncreated: %s\ndefault_profile: paper-ops-worker\n---\n\n## Objective\n\n## Context\n\n## Conventions\n", time.Now().Format("2006-01-02"))
	if err := os.WriteFile(initPath, []byte(initContent), 0o644); err != nil {
		t.Fatalf("write initiative: %v", err)
	}

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RESEARCH", "--title", "Extract knowledge", "--tier", "worker"))
	mustRun(t, "dispatch", id)

	// Profile should come from initiative.
	if gotOpts.Profile != "paper-ops-worker" {
		t.Fatalf("expected profile paper-ops-worker, got %q", gotOpts.Profile)
	}
	if gotOpts.ProfileSource != dispatch.SourceInitiative {
		t.Fatalf("expected profile source %q, got %q", dispatch.SourceInitiative, gotOpts.ProfileSource)
	}

	// Engine/model/effort should be from config.
	if gotOpts.EngineSource != dispatch.SourceConfig {
		t.Fatalf("expected engine source %q, got %q", dispatch.SourceConfig, gotOpts.EngineSource)
	}

	// Card should record profile-defined for engine/model since they were omitted
	// from the actual dispatch.
	doc := mustParseTicket(t, baseDir, id)
	if got := valueOrBlank(doc.Card.Engine); got != "profile-defined" {
		t.Fatalf("expected card engine 'profile-defined', got %q", got)
	}
	if got := valueOrBlank(doc.Card.Model); got != "profile-defined" {
		t.Fatalf("expected card model 'profile-defined', got %q", got)
	}
}

func TestDispatchInitiativeProfileWithCLIEngineOverride(t *testing.T) {
	// When profile comes from initiative but engine is explicitly set via CLI,
	// the engine flag should still be passed through.
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\nprofile = \"ticket-worker\"\n")
	withWorkingDir(t, baseDir)

	var gotOpts dispatch.DispatchOptions
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			gotOpts = opts
			return &dispatch.DispatchResult{DispatchID: "cli-override"}, nil
		},
	})

	mustRun(t, "init", "RESEARCH", "--title", "Paper operations")

	// Patch the initiative file to include default_profile.
	initPath := filepath.Join(baseDir, "INITIATIVES", "RESEARCH.md")
	initContent := fmt.Sprintf("---\ninitiative: RESEARCH\ntitle: \"Paper operations\"\nstatus: active\ncreated: %s\ndefault_profile: paper-ops-worker\n---\n\n## Objective\n\n## Context\n\n## Conventions\n", time.Now().Format("2006-01-02"))
	if err := os.WriteFile(initPath, []byte(initContent), 0o644); err != nil {
		t.Fatalf("write initiative: %v", err)
	}

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RESEARCH", "--title", "With CLI engine", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "claude", "--model", "opus")

	if gotOpts.Profile != "paper-ops-worker" {
		t.Fatalf("expected profile paper-ops-worker, got %q", gotOpts.Profile)
	}
	if gotOpts.EngineSource != dispatch.SourceCLI {
		t.Fatalf("expected engine source %q, got %q", dispatch.SourceCLI, gotOpts.EngineSource)
	}

	// Card should record the explicit engine since it was passed.
	doc := mustParseTicket(t, baseDir, id)
	if got := valueOrBlank(doc.Card.Engine); got != "claude" {
		t.Fatalf("expected card engine 'claude', got %q", got)
	}
	if got := valueOrBlank(doc.Card.Model); got != "opus" {
		t.Fatalf("expected card model 'opus', got %q", got)
	}
}

func TestDispatchRetryPreamble(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	var calls []dispatch.DispatchOptions
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			calls = append(calls, opts)
			return &dispatch.DispatchResult{DispatchID: fmt.Sprintf("dispatch-%d", len(calls))}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Retry me", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "timed out")
	mustRun(t, "reopen", id)
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	if len(calls) != 2 {
		t.Fatalf("expected 2 dispatch calls, got %d", len(calls))
	}
	if calls[0].Preamble != "" {
		t.Fatalf("first dispatch preamble = %q, want empty", calls[0].Preamble)
	}
	if !strings.Contains(calls[1].Preamble, "This ticket has had 1 previous attempt(s).") {
		t.Fatalf("retry preamble missing attempts text: %q", calls[1].Preamble)
	}
	if !strings.Contains(calls[1].Preamble, "Last outcome: failed") {
		t.Fatalf("retry preamble missing outcome text: %q", calls[1].Preamble)
	}
}

func TestCompleteNilDispatchID(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Nil dispatch_id", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	// Clear dispatch_id to simulate the race window
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.DispatchID = nil
	doc.SetSection("Result", "Completed work.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "complete", id)

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDone {
		t.Fatalf("expected done, got %s", doc.Card.Status)
	}
}

func TestBuildcileCompletedWithResult(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "completed"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Buildcile me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	doc := mustParseTicket(t, baseDir, id)
	doc.SetSection("Result", "Agent output: completed the full analysis of the target system with detailed findings and recommendations.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "reconcile")

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDone {
		t.Fatalf("expected done, got %s", doc.Card.Status)
	}
}

func TestBuildcileCompletedNoResult(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "completed"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Empty result", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	mustRun(t, "reconcile")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusFailed {
		t.Fatalf("expected failed (no Result), got %s", doc.Card.Status)
	}
	if !strings.Contains(doc.GetSection("Log"), "worker completed without writing Result") {
		t.Fatalf("expected reason in log, got %q", doc.GetSection("Log"))
	}
}

func TestBuildcileFailed(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "failed"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Fail me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	mustRun(t, "reconcile")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusFailed {
		t.Fatalf("expected failed, got %s", doc.Card.Status)
	}
}

func TestBuildcileFailedIncludesBackendError(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "failed", Error: "backend exploded"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Fail me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "reconcile")

	doc := mustParseTicket(t, baseDir, id)
	if !strings.Contains(doc.GetSection("Log"), "backend exploded") {
		t.Fatalf("expected backend error in log: %q", doc.GetSection("Log"))
	}

	out := mustRunStdout(t, "board")
	if !strings.Contains(out, "backend exploded") {
		t.Fatalf("expected board to show backend error, got %q", out)
	}
}

func TestBuildcileStatusQueryErrorRetriesBeforeFailing(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "max_retry = 2\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return nil, fmt.Errorf("mux unavailable")
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Orphan me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	out := mustRunStdout(t, "reconcile")
	if !strings.Contains(out, "warning: agent-mux status query failed (1/2): mux unavailable") {
		t.Fatalf("expected retry warning, got %q", out)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected dispatched after first query error, got %s", doc.Card.Status)
	}

	mustRun(t, "reconcile")

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusFailed {
		t.Fatalf("expected failed, got %s", doc.Card.Status)
	}
	if !strings.Contains(doc.GetSection("Log"), "agent-mux status query failed (2/2): mux unavailable") {
		t.Fatalf("expected status-query error in log")
	}
}

func TestBuildcileRunning(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "running"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Still running", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	out := mustRunStdout(t, "reconcile")
	if !strings.Contains(out, "nothing to reconcile") {
		t.Fatalf("expected no-op output, got %q", out)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected dispatched, got %s", doc.Card.Status)
	}
}

func TestBuildcileDryRun(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "failed"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Preview failure", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	out := mustRunStdout(t, "reconcile", "--dry-run")
	if !strings.Contains(out, "Would fail "+id) {
		t.Fatalf("unexpected dry-run output: %q", out)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("dry-run should not mutate status, got %s", doc.Card.Status)
	}
}

func TestDispatchReady(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	root := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Root", "--tier", "worker"))
	blocked := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Blocked by root", "--tier", "worker", "--depends-on", root))
	manual := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Manual", "--tier", "worker", "--manual"))
	older := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Older", "--tier", "worker"))
	newer := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Newer", "--tier", "worker"))

	rootDoc := mustParseTicket(t, baseDir, root)
	rootDoc.Card.Created = "2026-04-03"
	writeTicket(t, baseDir, root, rootDoc)

	olderDoc := mustParseTicket(t, baseDir, older)
	olderDoc.Card.Created = "2026-04-01"
	writeTicket(t, baseDir, older, olderDoc)

	newerDoc := mustParseTicket(t, baseDir, newer)
	newerDoc.Card.Created = "2026-04-02"
	writeTicket(t, baseDir, newer, newerDoc)

	mustRun(t, "dispatch-ready", "--max", "2")

	if len(dispatched) != 2 {
		t.Fatalf("expected 2 dispatched tickets, got %d", len(dispatched))
	}
	if dispatched[0] != older || dispatched[1] != newer {
		t.Fatalf("unexpected dispatch order: %#v", dispatched)
	}

	if mustParseTicket(t, baseDir, older).Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected %s dispatched", older)
	}
	if mustParseTicket(t, baseDir, newer).Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected %s dispatched", newer)
	}
	if mustParseTicket(t, baseDir, root).Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected %s to remain open", root)
	}
	if mustParseTicket(t, baseDir, blocked).Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected dependent ticket to remain open")
	}
	if mustParseTicket(t, baseDir, manual).Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected manual ticket to remain open")
	}
}

func TestCreateWithSkills(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Skilled", "--tier", "worker", "--skills", "web-search,summarize"))

	doc := mustParseTicket(t, baseDir, id)
	if got := doc.Card.Skills; len(got) != 2 || got[0] != "web-search" || got[1] != "summarize" {
		t.Fatalf("unexpected skills: %#v", got)
	}
}

func TestCreateInheritsDefaultSkillsFromInitiative(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "RESEARCH", "--title", "Paper operations")

	// Patch the initiative file to include default_skills.
	initPath := filepath.Join(baseDir, "INITIATIVES", "RESEARCH.md")
	initContent := fmt.Sprintf("---\ninitiative: RESEARCH\ntitle: \"Paper operations\"\nstatus: active\ncreated: %s\ndefault_profile: paper-ops-worker\ndefault_skills:\n- web-search\n---\n\n## Objective\n\n## Context\n\n## Conventions\n", time.Now().Format("2006-01-02"))
	if err := os.WriteFile(initPath, []byte(initContent), 0o644); err != nil {
		t.Fatalf("write initiative: %v", err)
	}

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RESEARCH", "--title", "Inherit skills", "--tier", "worker"))

	doc := mustParseTicket(t, baseDir, id)
	if got := doc.Card.Skills; len(got) != 1 || got[0] != "web-search" {
		t.Fatalf("expected skills [web-search], got %#v", got)
	}
}

func TestCreateExplicitSkillsOverrideInitiativeDefaults(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "RESEARCH", "--title", "Paper operations")

	// Patch the initiative file to include default_skills.
	initPath := filepath.Join(baseDir, "INITIATIVES", "RESEARCH.md")
	initContent := fmt.Sprintf("---\ninitiative: RESEARCH\ntitle: \"Paper operations\"\nstatus: active\ncreated: %s\ndefault_skills:\n- web-search\n---\n\n## Objective\n\n## Context\n\n## Conventions\n", time.Now().Format("2006-01-02"))
	if err := os.WriteFile(initPath, []byte(initContent), 0o644); err != nil {
		t.Fatalf("write initiative: %v", err)
	}

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RESEARCH", "--title", "Override skills", "--tier", "worker", "--skills", "web-search"))

	doc := mustParseTicket(t, baseDir, id)
	if got := doc.Card.Skills; len(got) != 1 || got[0] != "web-search" {
		t.Fatalf("expected skills [web-search], got %#v", got)
	}
}

func TestCreateNoDefaultSkillsStaysEmpty(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")

	// Initiative has no default_skills — created ticket should have no skills.
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "No skills", "--tier", "worker"))

	doc := mustParseTicket(t, baseDir, id)
	if got := doc.Card.Skills; len(got) != 0 {
		t.Fatalf("expected empty skills, got %#v", got)
	}
}

func TestDispatchPassesSkillsWithoutAutoInjection(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var got dispatch.DispatchOptions
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			got = opts
			return &dispatch.DispatchResult{DispatchID: "dispatch-1"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Skills", "--tier", "worker", "--skills", "web-search,summarize"))
	mustRun(t, "dispatch", id)

	wantSkills := []string{"web-search", "summarize"}
	if !equalStrings(got.Skills, wantSkills) {
		t.Fatalf("dispatch skills = %#v, want %#v", got.Skills, wantSkills)
	}
	resolvedBaseDir, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		t.Fatalf("eval base dir symlinks: %v", err)
	}
	resolvedWorkDir, err := filepath.EvalSymlinks(got.WorkDir)
	if err != nil {
		t.Fatalf("eval workdir symlinks: %v", err)
	}
	if resolvedWorkDir != resolvedBaseDir {
		t.Fatalf("dispatch workdir = %q, want %q", got.WorkDir, baseDir)
	}
}

func TestDispatchReadySkipsEmptyScopeBeforeLimit(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{DispatchID: "dispatch-ok"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	emptyScope := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Empty scope", "--tier", "worker"))
	readyOne := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Ready one", "--tier", "worker"))
	readyTwo := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Ready two", "--tier", "worker"))

	ensureScope(t, baseDir, readyOne)
	ensureScope(t, baseDir, readyTwo)

	if _, err := dispatchReadyTickets(baseDir, 2, false); err != nil {
		t.Fatalf("dispatchReadyTickets() error = %v", err)
	}
	if len(dispatched) != 2 || dispatched[0] != readyOne || dispatched[1] != readyTwo {
		t.Fatalf("unexpected dispatched tickets: %#v", dispatched)
	}
	if mustParseTicket(t, baseDir, emptyScope).Card.Status != frontmatter.StatusOpen {
		t.Fatalf("empty-scope ticket should remain open")
	}
}

func TestDispatchReadyDryRun(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Preview dispatch", "--tier", "worker"))

	out := mustRunStdout(t, "dispatch-ready", "--dry-run")
	if !strings.Contains(out, "Would dispatch "+id) {
		t.Fatalf("unexpected dry-run output: %q", out)
	}
	if mustParseTicket(t, baseDir, id).Card.Status != frontmatter.StatusOpen {
		t.Fatalf("dry-run should not change status")
	}
}

func TestDispatchReadyContinuesAfterIndividualFailure(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			id := strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md")
			if strings.Contains(id, "001") {
				return nil, fmt.Errorf("boom")
			}
			dispatched = append(dispatched, id)
			return &dispatch.DispatchResult{DispatchID: "dispatch-ok"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	first := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "First", "--tier", "worker"))
	second := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Second", "--tier", "worker"))

	out := mustRunStdout(t, "dispatch-ready", "--max", "2")
	if !strings.Contains(out, "boom") {
		t.Fatalf("expected failure output, got %q", out)
	}
	if len(dispatched) != 1 || dispatched[0] != second {
		t.Fatalf("expected second ticket to dispatch, got %#v", dispatched)
	}
	if mustParseTicket(t, baseDir, first).Card.Status != frontmatter.StatusOpen {
		t.Fatalf("expected failed dispatch ticket to remain open")
	}
	if mustParseTicket(t, baseDir, second).Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected second ticket dispatched")
	}
}

func TestWeightAwareDispatchRespectsEngineCap(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"

[concurrency]
codex = 2

[model_weight]
"gpt-5.4-mini" = 1
"gpt-5.4" = 2
`)
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{DispatchID: fmt.Sprintf("d-%d", len(dispatched))}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")

	aID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A already in flight", "--tier", "worker"))
	mustRun(t, "dispatch", aID, "--engine", "codex", "--model", "gpt-5.4-mini")
	dispatched = nil

	bID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B heavy model", "--tier", "worker"))
	bDoc := mustParseTicket(t, baseDir, bID)
	bDoc.Card.Engine = stringPtr("codex")
	bDoc.Card.Model = stringPtr("gpt-5.4")
	writeTicket(t, baseDir, bID, bDoc)

	cID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "C light model", "--tier", "worker"))

	out := mustRunStdout(t, "dispatch-ready", "--max", "5")

	if len(dispatched) != 1 {
		t.Fatalf("expected 1 dispatch, got %d: %v", len(dispatched), dispatched)
	}
	if dispatched[0] != cID {
		t.Fatalf("expected %s dispatched, got %s", cID, dispatched[0])
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, bID) {
		t.Fatalf("expected skip message for %s, got: %s", bID, out)
	}
}

func TestWeightAwareDispatchNoConcurrencyConfigIsUnlimited(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"
`)
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{DispatchID: fmt.Sprintf("d-%d", len(dispatched))}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	for i := 0; i < 5; i++ {
		mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", fmt.Sprintf("T%d", i), "--tier", "worker")
	}

	mustRun(t, "dispatch-ready", "--max", "10")
	if len(dispatched) != 5 {
		t.Fatalf("expected 5 dispatched, got %d", len(dispatched))
	}
}

func TestMaxDispatchPerTickFromConfig(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
max_dispatch_per_tick = 2
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"
`)
	withWorkingDir(t, baseDir)

	dispatched := 0
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched++
			return &dispatch.DispatchResult{DispatchID: fmt.Sprintf("d-%d", dispatched)}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	for i := 0; i < 5; i++ {
		mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", fmt.Sprintf("T%d", i), "--tier", "worker")
	}

	out := mustRunStdout(t, "tick")
	if dispatched != 2 {
		t.Fatalf("expected 2 dispatched via max_dispatch_per_tick, got %d", dispatched)
	}
	if !strings.Contains(out, "dispatched 2") {
		t.Fatalf("expected tick output to show dispatched 2, got: %s", out)
	}
}

func TestTick(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{DispatchID: "tick-dispatch"}, nil
		},
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "completed"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	root := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Root", "--tier", "worker"))
	child := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Child", "--tier", "worker", "--depends-on", root))

	mustRun(t, "dispatch", root, "--engine", "codex", "--model", "gpt-5.4")
	rootDoc := mustParseTicket(t, baseDir, root)
	rootDoc.SetSection("Result", "Finished root task: completed the full analysis with detailed findings and actionable recommendations.\n")
	writeTicket(t, baseDir, root, rootDoc)
	dispatched = nil

	out := mustRunStdout(t, "tick", "--max-dispatch", "1")
	if !strings.Contains(out, "tick: reconciled 1, stalled 0, dispatched 1") {
		t.Fatalf("unexpected tick output: %q", out)
	}

	if mustParseTicket(t, baseDir, root).Card.Status != frontmatter.StatusDone {
		t.Fatalf("expected %s done after reconcile", root)
	}
	if mustParseTicket(t, baseDir, child).Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected %s dispatched after tick", child)
	}
	if len(dispatched) != 1 || dispatched[0] != child {
		t.Fatalf("unexpected dispatched tickets: %#v", dispatched)
	}
}

func TestTickLockPreventsConcurrentRun(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	lockPath := filepath.Join(baseDir, ".tick.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer func() {
		_ = lockFile.Close()
		_ = os.Remove(lockPath)
	}()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock lock: %v", err)
	}
	if _, err := fmt.Fprintf(lockFile, "%d\n", os.Getpid()); err != nil {
		t.Fatalf("write lock pid: %v", err)
	}

	out := mustRunStdout(t, "tick")
	if out != "" {
		t.Fatalf("expected silent exit when lock held, got %q", out)
	}
}

func TestTickReclaimsStaleLock(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	lockPath := filepath.Join(baseDir, ".tick.lock")
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	out := mustRunStdout(t, "tick")
	if !strings.Contains(out, "tick: reconciled 0, stalled 0, dispatched 0") {
		t.Fatalf("expected stale lock to be reclaimed, got %q", out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed after tick, got %v", err)
	}
}

func TestStallDetectionWarnsOnStall(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"

[stall_timeout_minutes]
worker = 1
`)
	withWorkingDir(t, baseDir)

	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	ticketID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Stall me", "--tier", "worker"))
	mustRun(t, "dispatch", ticketID, "--engine", "codex", "--model", "gpt-5.4-mini")

	doc := mustParseTicket(t, baseDir, ticketID)
	fiveMinAgo := time.Now().Add(-5 * time.Minute).Format(time.RFC3339)
	doc.Card.DispatchedAt = &fiveMinAgo
	writeTicket(t, baseDir, ticketID, doc)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	out := captureStdout(t, func() {
		count, err := runStallDetection(baseDir, cfg)
		if err != nil {
			t.Fatalf("runStallDetection error: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 stalled, got %d", count)
		}
	})

	if !strings.Contains(out, "[STALL_WARNING]") {
		t.Fatalf("expected [STALL_WARNING] in output, got %q", out)
	}
	if !strings.Contains(out, ticketID) {
		t.Fatalf("expected stalled ticket ID in output, got %q", out)
	}
	// No AUDIT ticket should be created.
	if _, err := findTicketFile(baseDir, "AUDIT-001"); err == nil {
		t.Fatalf("did not expect guardian ticket")
	}
}

func TestStallDetectionNoFalsePositives(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"

[stall_timeout_minutes]
worker = 30
`)
	withWorkingDir(t, baseDir)

	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	ticketID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Fresh dispatch", "--tier", "worker"))
	mustRun(t, "dispatch", ticketID, "--engine", "codex", "--model", "gpt-5.4-mini")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	count, err := runStallDetection(baseDir, cfg)
	if err != nil {
		t.Fatalf("runStallDetection error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 stalled for fresh dispatch, got %d", count)
	}
}

func TestDispatchedAtTimestampSetOnDispatch(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Dispatch me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4-mini")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.DispatchedAt == nil {
		t.Fatalf("expected dispatched_at set")
	}
	if _, err := time.Parse(time.RFC3339, *doc.Card.DispatchedAt); err != nil {
		t.Fatalf("expected RFC3339 dispatched_at, got %q: %v", *doc.Card.DispatchedAt, err)
	}
}

func TestDispatchedAtClearedOnCancel(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Cancel me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4-mini")
	mustRun(t, "cancel", id, "--reason", "test")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.DispatchedAt != nil {
		t.Fatalf("expected dispatched_at cleared, got %v", doc.Card.DispatchedAt)
	}
}

func TestBuildEngineWeightMap(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"

[model_weight]
"gpt-5.4-mini" = 1
"gpt-5.4" = 2
"gemini-3-flash-preview" = 1
`)
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	aID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))
	bID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker"))
	cID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "C", "--tier", "worker"))

	mustRun(t, "dispatch", aID, "--engine", "codex", "--model", "gpt-5.4-mini")
	mustRun(t, "dispatch", bID, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "dispatch", cID, "--engine", "gemini", "--model", "gemini-3-flash-preview")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	files, err := allTicketFiles(baseDir)
	if err != nil {
		t.Fatalf("allTicketFiles: %v", err)
	}
	weights := buildEngineWeightMap(files, cfg)
	if weights["codex"] != 3 || weights["gemini"] != 1 {
		t.Fatalf("unexpected weights: %#v", weights)
	}
}

func TestBuildEngineWeightMapResolvesProfileDefined(t *testing.T) {
	// When a ticket is dispatched via an initiative's default_profile,
	// the card stores engine: "profile-defined". buildEngineWeightMap
	// must resolve through profile_engine config to count the weight
	// under the real engine (e.g. "gemini"), not "profile-defined".
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"

[model_weight]
"gpt-5.4-mini" = 1
"gpt-5.4" = 2
"gemini-3-flash-preview" = 1

[profile_engine]
"paper-ops-worker" = "gemini"
"ticket-worker" = "codex"

[profile_model]
"paper-ops-worker" = "gemini-3-flash-preview"
"ticket-worker" = "gpt-5.4-mini"
`)
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "RESEARCH", "--title", "Paper operations")

	// Patch the initiative file to include default_profile.
	initPath := filepath.Join(baseDir, "INITIATIVES", "RESEARCH.md")
	initContent := fmt.Sprintf(
		"---\ninitiative: RESEARCH\ntitle: \"Paper operations\"\nstatus: active\ncreated: %s\ndefault_profile: paper-ops-worker\n---\n\n## Objective\n\n## Context\n\n## Conventions\n",
		time.Now().Format("2006-01-02"),
	)
	if err := os.WriteFile(initPath, []byte(initContent), 0o644); err != nil {
		t.Fatalf("write initiative: %v", err)
	}

	// Create and dispatch two tickets via the initiative (profile-defined path).
	aID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RESEARCH", "--title", "Paper A", "--tier", "worker"))
	bID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RESEARCH", "--title", "Paper B", "--tier", "worker"))

	mustRun(t, "dispatch", aID)
	mustRun(t, "dispatch", bID)

	// Also dispatch one with explicit engine to verify mixed counting.
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	cID := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Direct codex", "--tier", "worker"))
	mustRun(t, "dispatch", cID, "--engine", "codex", "--model", "gpt-5.4-mini")

	// Verify the dispatched cards are actually profile-defined.
	docA := mustParseTicket(t, baseDir, aID)
	if got := valueOrBlank(docA.Card.Engine); got != "profile-defined" {
		t.Fatalf("expected card engine 'profile-defined', got %q", got)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	files, err := allTicketFiles(baseDir)
	if err != nil {
		t.Fatalf("allTicketFiles: %v", err)
	}
	weights := buildEngineWeightMap(files, cfg)

	// Two profile-defined tickets should resolve to gemini with weight 1 each.
	// One explicit codex ticket with weight 1.
	if weights["gemini"] != 2 {
		t.Fatalf("expected gemini weight 2, got %d (weights: %#v)", weights["gemini"], weights)
	}
	if weights["codex"] != 1 {
		t.Fatalf("expected codex weight 1, got %d (weights: %#v)", weights["codex"], weights)
	}
	// profile-defined should not appear as its own engine bucket.
	if weights["profile-defined"] != 0 {
		t.Fatalf("expected no weight under 'profile-defined', got %d (weights: %#v)", weights["profile-defined"], weights)
	}
}

func TestTickOutputIncludesStallCount(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, `
[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "batch"

[stall_timeout_minutes]
worker = 1

[guardian]
engine = "codex"
model = "gpt-5.4-mini"
effort = "xhigh"
profile = "ticket-worker"
initiative = "AUDIT"
`)
	withWorkingDir(t, baseDir)

	dispatchCount := 0
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatchCount++
			return &dispatch.DispatchResult{DispatchID: fmt.Sprintf("d-%d", dispatchCount)}, nil
		},
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "running"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Stall me", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Status = frontmatter.StatusDispatched
	doc.Card.DispatchID = stringPtr("dispatch-1")
	doc.Card.Engine = stringPtr("codex")
	doc.Card.Model = stringPtr("gpt-5.4-mini")
	past := time.Now().Add(-5 * time.Minute).Format(time.RFC3339)
	doc.Card.DispatchedAt = &past
	writeTicket(t, baseDir, id, doc)

	out := mustRunStdout(t, "tick", "--max-dispatch", "0")
	if !strings.Contains(out, "tick: reconciled 0, stalled 1, dispatched 0") {
		t.Fatalf("unexpected tick output: %q", out)
	}
}

func TestFailAutoBlockFromConfig(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "max_retry = 1\n")
	withMockDispatcher(t, &dispatch.MockDispatcher{})
	withWorkingDir(t, baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Auto block", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "first failure")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusBlocked {
		t.Fatalf("status = %s, want blocked", doc.Card.Status)
	}
	if doc.Card.BlockReason == nil || !strings.Contains(*doc.Card.BlockReason, "1 failed attempts") {
		t.Fatalf("unexpected block reason: %#v", doc.Card.BlockReason)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := stdout
	stdout = &buf
	defer func() {
		stdout = prev
	}()
	fn()
	return buf.String()
}

func TestFailBlocksOnThirdFailureWhenMaxRetryIsThree(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "max_retry = 3\n")
	withMockDispatcher(t, &dispatch.MockDispatcher{})
	withWorkingDir(t, baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Three strikes", "--tier", "worker"))

	for i := 0; i < 2; i++ {
		mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
		mustRun(t, "fail", id, "--reason", fmt.Sprintf("failure %d", i+1))
		doc := mustParseTicket(t, baseDir, id)
		if doc.Card.Status != frontmatter.StatusFailed {
			t.Fatalf("failure %d should leave ticket failed, got %s", i+1, doc.Card.Status)
		}
		mustRun(t, "reopen", id)
	}

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "failure 3")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusBlocked {
		t.Fatalf("third failure should block, got %s", doc.Card.Status)
	}
}

func TestDeleteRefusesDispatched(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Delete me", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	err := run([]string{"delete", id})
	if err == nil || !strings.Contains(err.Error(), "cannot delete dispatched ticket") {
		t.Fatalf("expected dispatched delete error, got %v", err)
	}
}

func TestDeleteShowsDependencyBranch(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Root", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Child", "--tier", "worker", "--depends-on", a))

	err := run([]string{"delete", a})
	if err == nil {
		t.Fatal("expected error")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, b) {
		t.Fatalf("error should mention dependent %s: %s", b, errMsg)
	}
	if !strings.Contains(errMsg, "--cascade") {
		t.Fatalf("error should mention --cascade: %s", errMsg)
	}
}

func TestDeleteCascade(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Root", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Child", "--tier", "worker", "--depends-on", a))

	mustRun(t, "delete", a, "--cascade")

	if _, err := findTicketFile(baseDir, a); err == nil {
		t.Fatalf("expected %s deleted", a)
	}
	if _, err := findTicketFile(baseDir, b); err == nil {
		t.Fatalf("expected %s deleted", b)
	}
}

func TestDeleteCascadeRefusesDispatchedDescendant(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	root := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Root", "--tier", "worker"))
	child := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Child", "--tier", "worker", "--depends-on", root))

	rootDoc := mustParseTicket(t, baseDir, root)
	rootDoc.Card.Status = frontmatter.StatusDone
	writeTicket(t, baseDir, root, rootDoc)

	mustRun(t, "dispatch", child, "--engine", "codex", "--model", "gpt-5.4")

	err := run([]string{"delete", root, "--cascade"})
	if err == nil || !strings.Contains(err.Error(), child) {
		t.Fatalf("expected dispatched descendant error, got %v", err)
	}
	if _, err := findTicketFile(baseDir, root); err != nil {
		t.Fatalf("root should still exist: %v", err)
	}
}

func TestMigrate(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "BUILD", "--title", "Build")
	mustRun(t, "init", "STARK", "--title", "Stark")

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "Explore", "--tier", "worker"))

	mustRun(t, "migrate", id, "STARK")

	if _, err := findTicketFile(baseDir, id); err == nil {
		t.Fatalf("old ticket should not exist: %s", id)
	}

	newDoc := mustParseTicket(t, baseDir, "STARK-001")
	if newDoc.Card.Initiative != "STARK" {
		t.Fatalf("initiative should be STARK, got %s", newDoc.Card.Initiative)
	}
	if newDoc.Card.ID != "STARK-001" {
		t.Fatalf("id should be STARK-001, got %s", newDoc.Card.ID)
	}
}

func TestMigrateUpdatesDependsOn(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "BUILD", "--title", "Build")
	mustRun(t, "init", "STARK", "--title", "Stark")

	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "B", "--tier", "worker", "--depends-on", a))

	mustRun(t, "migrate", a, "STARK")

	bDoc := mustParseTicket(t, baseDir, b)
	if len(bDoc.Card.DependsOn) != 1 || bDoc.Card.DependsOn[0] != "STARK-001" {
		t.Fatalf("depends_on should reference STARK-001, got %#v", bDoc.Card.DependsOn)
	}
}

func TestMigrateRefusesDispatched(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "BUILD", "--title", "Build")
	mustRun(t, "init", "STARK", "--title", "Stark")

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "In flight", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	err := run([]string{"migrate", id, "STARK"})
	if err == nil || !strings.Contains(err.Error(), "dispatched") {
		t.Fatalf("expected dispatched error, got %v", err)
	}
}

func TestMigrateDryRun(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "BUILD", "--title", "Build")
	mustRun(t, "init", "STARK", "--title", "Stark")

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "Preview", "--tier", "worker"))

	out := mustRunStdout(t, "migrate", id, "STARK", "--dry-run")
	if !strings.Contains(out, "Would migrate") {
		t.Fatalf("dry-run should preview: %s", out)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Initiative != "BUILD" {
		t.Fatalf("dry-run should not change initiative: %s", doc.Card.Initiative)
	}
}

func TestCreatedDateOnly(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Date test", "--tier", "worker"))

	doc := mustParseTicket(t, baseDir, id)
	assertDateOnly(t, doc.Card.Created)

	initData, err := os.ReadFile(filepath.Join(baseDir, "INITIATIVES", "ALPHA.md"))
	if err != nil {
		t.Fatalf("read initiative: %v", err)
	}
	matches := regexp.MustCompile(`(?m)^created:\s+(.+)$`).FindSubmatch(initData)
	if len(matches) != 2 {
		t.Fatalf("created field not found in initiative file:\n%s", string(initData))
	}
	assertDateOnly(t, string(matches[1]))
}

func TestCreateRejectsMissingDependency(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")

	err := run([]string{"create", "--initiative", "ALPHA", "--title", "Missing dep", "--tier", "worker", "--depends-on", "ALPHA-999"})
	if err == nil || !strings.Contains(err.Error(), "dependency not found: ALPHA-999") {
		t.Fatalf("expected missing dependency error, got %v", err)
	}
}

func TestCreateRejectsCircularDependency(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker", "--depends-on", a))

	doc := mustParseTicket(t, baseDir, a)
	doc.Card.DependsOn = []string{b}
	writeTicket(t, baseDir, a, doc)

	err := run([]string{"create", "--initiative", "ALPHA", "--title", "Cycle", "--tier", "worker", "--depends-on", b})
	if err == nil || !strings.Contains(err.Error(), "circular dependency detected") {
		t.Fatalf("expected circular dependency error, got %v", err)
	}
}

func TestCreateRejectsDeepDependencyChain(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker", "--depends-on", a))
	c := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "C", "--tier", "worker", "--depends-on", b))

	err := run([]string{"create", "--initiative", "ALPHA", "--title", "Too deep", "--tier", "worker", "--depends-on", c})
	if err == nil || !strings.Contains(err.Error(), "dependency chain too deep (max 3)") {
		t.Fatalf("expected deep dependency error, got %v", err)
	}
}

func TestCreateAcceptsDependencyChainDepthThree(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker", "--depends-on", a))
	c := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "C", "--tier", "worker", "--depends-on", b))

	doc := mustParseTicket(t, baseDir, c)
	if len(doc.Card.DependsOn) != 1 || doc.Card.DependsOn[0] != b {
		t.Fatalf("unexpected dependencies: %#v", doc.Card.DependsOn)
	}
}

func mustRun(t *testing.T, args ...string) {
	t.Helper()
	makeDispatchCommandsReady(t, args...)
	if err := run(args); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
}

func mustRunStdout(t *testing.T, args ...string) string {
	t.Helper()
	makeDispatchCommandsReady(t, args...)

	var buf bytes.Buffer
	prev := stdout
	stdout = &buf
	defer func() {
		stdout = prev
	}()

	if err := run(args); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return buf.String()
}

func makeDispatchCommandsReady(t *testing.T, args ...string) {
	t.Helper()
	if len(args) == 0 {
		return
	}

	baseDir := os.Getenv("TICKETS_BASE_DIR")
	if baseDir == "" {
		return
	}

	switch args[0] {
	case "dispatch":
		if len(args) < 2 {
			return
		}
		for _, id := range strings.Split(args[1], ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ensureScope(t, baseDir, id)
			}
		}
	case "dispatch-ready", "tick":
		files, err := allTicketFiles(baseDir)
		if err != nil {
			t.Fatalf("list tickets: %v", err)
		}
		for _, file := range files {
			doc, err := frontmatter.ParseFile(file)
			if err != nil {
				t.Fatalf("parse ticket %s: %v", file, err)
			}
			ensureScopeDoc(t, file, doc)
		}
	}
}

func ensureScope(t *testing.T, baseDir, id string) {
	t.Helper()
	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		t.Fatalf("load ticket %s: %v", id, err)
	}
	ensureScopeDoc(t, path, doc)
}

func ensureScopeDoc(t *testing.T, path string, doc *frontmatter.Document) {
	t.Helper()
	if strings.TrimSpace(doc.GetSection("Scope")) != "" {
		return
	}
	doc.SetSection("Scope", "Test scope.\n")
	if err := doc.WriteFile(path); err != nil {
		t.Fatalf("write scope to %s: %v", path, err)
	}
}

func mustParseTicket(t *testing.T, baseDir, id string) *frontmatter.Document {
	t.Helper()
	path, err := findTicketFile(baseDir, id)
	if err != nil {
		t.Fatalf("find ticket %s: %v", id, err)
	}
	doc, err := frontmatter.ParseFile(path)
	if err != nil {
		t.Fatalf("parse ticket %s: %v", id, err)
	}
	return doc
}

func writeTicket(t *testing.T, baseDir, id string, doc *frontmatter.Document) {
	t.Helper()
	path, err := findTicketFile(baseDir, id)
	if err != nil {
		t.Fatalf("find ticket %s: %v", id, err)
	}
	if err := doc.WriteFile(path); err != nil {
		t.Fatalf("write ticket %s: %v", id, err)
	}
}

func addTag(t *testing.T, baseDir, id, tag string) {
	t.Helper()
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Tags = append(doc.Card.Tags, tag)
	writeTicket(t, baseDir, id, doc)
}

func TestMain(m *testing.M) {
	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	code := m.Run()
	stdout = os.Stdout
	stderr = os.Stderr
	os.Exit(code)
}

func withMockDispatcher(t *testing.T, mock *dispatch.MockDispatcher) {
	t.Helper()
	dispatcher = mock
	t.Cleanup(func() {
		dispatcher = nil
	})
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			panic(err)
		}
	})
}

func writeConfigFile(t *testing.T, dir, body string) {
	t.Helper()

	path := filepath.Join(dir, ".tickets.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestBuildcileBackfillsSessionIDWhileRunning(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "running", SessionID: "ses-abc123"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Session backfill running", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	// Clear session_id to simulate async dispatch that didn't return one
	doc := mustParseTicket(t, baseDir, id)
	empty := ""
	doc.Card.SessionID = &empty
	writeTicket(t, baseDir, id, doc)

	out := mustRunStdout(t, "reconcile")
	if !strings.Contains(out, "backfilled session_id") {
		t.Fatalf("expected session_id backfill action, got %q", out)
	}

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.SessionID == nil || *doc.Card.SessionID != "ses-abc123" {
		t.Fatalf("expected session_id ses-abc123, got %v", doc.Card.SessionID)
	}
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("expected still dispatched, got %s", doc.Card.Status)
	}
}

func TestBuildcileBackfillsSessionIDOnComplete(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{
				Status:    "completed",
				SessionID: "ses-xyz789",
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Session backfill complete", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	// Set a substantial result and clear session_id so reconcile must backfill it.
	doc := mustParseTicket(t, baseDir, id)
	empty := ""
	doc.Card.SessionID = &empty
	doc.SetSection("Result", "Session backfill test: completed the full analysis with detailed findings and actionable recommendations.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "reconcile")

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDone {
		t.Fatalf("expected done, got %s", doc.Card.Status)
	}
	if doc.Card.SessionID == nil || *doc.Card.SessionID != "ses-xyz789" {
		t.Fatalf("expected session_id ses-xyz789, got %v", doc.Card.SessionID)
	}
}

// Note: terminal-state cards (done/failed/blocked/closed) are NOT queried
// by reconcile anymore — the tokens-on-card feature was carved out along
// with the backfill loop that would have filled session_id on already-
// terminal cards. Session backfill is still exercised for running
// (dispatched) and just-transitioning cards via the other reconcile tests.

func TestBuildcileAutoBlocksWhenRetryBudgetExhausted(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "max_retry = 1\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{Status: "failed", Error: "backend exploded"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Block on reconcile", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "reconcile")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusBlocked {
		t.Fatalf("expected blocked, got %s", doc.Card.Status)
	}
	if doc.Card.BlockReason == nil || !strings.Contains(*doc.Card.BlockReason, "1 failed attempts") {
		t.Fatalf("unexpected block reason: %#v", doc.Card.BlockReason)
	}
}

func TestNextSequenceCaseInsensitive(t *testing.T) {
	dir := t.TempDir()

	// Write a file with a lowercase name to simulate macOS case-collision scenario.
	if err := os.WriteFile(filepath.Join(dir, "build-005.md"), []byte(""), 0o644); err != nil {
		t.Fatalf("write lowercase file: %v", err)
	}

	seq, err := nextSequence(dir, "BUILD")
	if err != nil {
		t.Fatalf("nextSequence error: %v", err)
	}
	if seq != 6 {
		t.Fatalf("expected seq 6 (after seeing build-005.md case-insensitively), got %d", seq)
	}
}

func TestCreateInitiativeUppercasedInID(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha initiative")
	out := mustRunStdout(t, "create", "--initiative", "alpha", "--title", "Lowercase initiative", "--tier", "worker")
	id := strings.TrimSpace(out)

	if id != "ALPHA-001" {
		t.Fatalf("expected ID ALPHA-001 (uppercased initiative), got %q", id)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.ID != "ALPHA-001" {
		t.Fatalf("card ID should be ALPHA-001, got %q", doc.Card.ID)
	}
}

func ticketPath(baseDir, id string) string {
	initiative, _, _ := parseTicketID(id)
	return filepath.Join(baseDir, "cards", initiative, id+".md")
}

func assertDateOnly(t *testing.T, value string) {
	t.Helper()
	if ok, err := regexp.MatchString(`^\d{4}-\d{2}-\d{2}$`, value); err != nil {
		t.Fatalf("compile date regex: %v", err)
	} else if !ok {
		t.Fatalf("expected date-only value, got %q", value)
	}
	if strings.ContainsAny(value, "TZ") {
		t.Fatalf("expected no RFC3339 markers in %q", value)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCreateWithAwaits(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	target := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Target", "--tier", "worker"))

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter", "--tier", "worker", "--awaits", target))

	doc := mustParseTicket(t, baseDir, id)
	if len(doc.Card.Awaits) != 1 || doc.Card.Awaits[0] != target {
		t.Fatalf("expected awaits [%s], got %#v", target, doc.Card.Awaits)
	}
}

func TestCreateRejectsCircularAwaits(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))

	// Make A await itself (via B's awaits pointing back)
	err := run([]string{"create", "--initiative", "ALPHA", "--title", "Self-await", "--tier", "worker", "--awaits", "ALPHA-999"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected dependency not found error, got %v", err)
	}

	// Create circular: B awaits A, then try to make A depend on B
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker", "--awaits", a))

	doc := mustParseTicket(t, baseDir, a)
	doc.Card.DependsOn = []string{b}
	writeTicket(t, baseDir, a, doc)

	err = run([]string{"create", "--initiative", "ALPHA", "--title", "Cycle", "--tier", "worker", "--depends-on", b})
	if err == nil || !strings.Contains(err.Error(), "circular dependency detected") {
		t.Fatalf("expected circular dependency error, got %v", err)
	}
}

func TestDispatchRejectsNonTerminalAwaits(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	target := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Target", "--tier", "worker"))
	awaiter := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter", "--tier", "worker", "--awaits", target))

	err := run([]string{"dispatch", awaiter, "--engine", "codex", "--model", "gpt-5.4"})
	if err == nil || !strings.Contains(err.Error(), "awaited ticket "+target+" is not terminal") {
		t.Fatalf("expected non-terminal awaits error, got %v", err)
	}
}

func TestDispatchReadyAwaitsBlocksOnNonTerminal(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	target := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Target (open)", "--tier", "worker"))
	strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter", "--tier", "worker", "--awaits", target))

	mustRun(t, "dispatch-ready", "--max", "10")

	// Only the target (which has no awaits/deps) should be dispatched.
	// The awaiter should be skipped because its awaited ticket is still open.
	if len(dispatched) != 1 {
		t.Fatalf("expected 1 dispatched ticket (target only), got %d: %v", len(dispatched), dispatched)
	}
	if dispatched[0] != target {
		t.Fatalf("expected target dispatched, got %s", dispatched[0])
	}
}

func TestDispatchReadyAwaitsTerminal(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	t1 := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Done target", "--tier", "worker"))
	t2 := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Failed target", "--tier", "worker"))
	awaiter := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter", "--tier", "worker", "--awaits", t1+","+t2))

	// Mark t1 as done, t2 as failed — both terminal.
	doc1 := mustParseTicket(t, baseDir, t1)
	doc1.Card.Status = frontmatter.StatusDone
	writeTicket(t, baseDir, t1, doc1)

	doc2 := mustParseTicket(t, baseDir, t2)
	doc2.Card.Status = frontmatter.StatusFailed
	writeTicket(t, baseDir, t2, doc2)

	mustRun(t, "dispatch-ready", "--max", "10")

	found := false
	for _, d := range dispatched {
		if d == awaiter {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected awaiter to be dispatched (both awaits terminal), dispatched: %v", dispatched)
	}
}

func TestDispatchReadyBothDependsOnAndAwaits(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	dep := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Hard dep (done)", "--tier", "worker"))
	aw := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Soft dep (failed)", "--tier", "worker"))
	ticket := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Both deps", "--tier", "worker", "--depends-on", dep, "--awaits", aw))

	// dep is done (hard dep satisfied), aw is failed (terminal, soft dep satisfied)
	depDoc := mustParseTicket(t, baseDir, dep)
	depDoc.Card.Status = frontmatter.StatusDone
	writeTicket(t, baseDir, dep, depDoc)

	awDoc := mustParseTicket(t, baseDir, aw)
	awDoc.Card.Status = frontmatter.StatusFailed
	writeTicket(t, baseDir, aw, awDoc)

	mustRun(t, "dispatch-ready", "--max", "10")

	found := false
	for _, d := range dispatched {
		if d == ticket {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ticket with both deps satisfied to be dispatched, dispatched: %v", dispatched)
	}
}

func TestDispatchReadyDependsOnBlocksEvenIfAwaitsReady(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	dep := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Hard dep (failed)", "--tier", "worker"))
	aw := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Soft dep (done)", "--tier", "worker"))
	ticket := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Mixed deps", "--tier", "worker", "--depends-on", dep, "--awaits", aw))

	// dep is failed (hard dep NOT satisfied — must be done), aw is done (terminal, soft dep satisfied)
	depDoc := mustParseTicket(t, baseDir, dep)
	depDoc.Card.Status = frontmatter.StatusFailed
	writeTicket(t, baseDir, dep, depDoc)

	awDoc := mustParseTicket(t, baseDir, aw)
	awDoc.Card.Status = frontmatter.StatusDone
	writeTicket(t, baseDir, aw, awDoc)

	mustRun(t, "dispatch-ready", "--max", "10")

	for _, d := range dispatched {
		if d == ticket {
			t.Fatalf("ticket with unsatisfied hard dep should NOT be dispatched, but was")
		}
	}
	_ = ticket // suppress unused
}

func TestBoardAwaitsAnnotation(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	target := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Target", "--tier", "worker"))
	strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter", "--tier", "worker", "--awaits", target))

	out := mustRunStdout(t, "board")
	if !strings.Contains(out, "(awaits)") {
		t.Fatalf("board should show (awaits) suffix for unresolved soft dep: %s", out)
	}
	if !strings.Contains(out, "waiting") {
		t.Fatalf("board should show waiting for ticket with unresolved awaits: %s", out)
	}
}

func TestMigrateUpdatesAwaits(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "BUILD", "--title", "Build")
	mustRun(t, "init", "STARK", "--title", "Stark")

	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "B", "--tier", "worker", "--awaits", a))

	mustRun(t, "migrate", a, "STARK")

	bDoc := mustParseTicket(t, baseDir, b)
	if len(bDoc.Card.Awaits) != 1 || bDoc.Card.Awaits[0] != "STARK-001" {
		t.Fatalf("awaits should reference STARK-001, got %#v", bDoc.Card.Awaits)
	}
}

func TestDispatchReadyClosedTerminal(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Closed target", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter of closed", "--tier", "worker", "--awaits", a))

	// Close ticket A — closed is terminal.
	mustRun(t, "close", a, "--reason", "no longer needed")

	mustRun(t, "dispatch-ready", "--max", "10")

	found := false
	for _, d := range dispatched {
		if d == b {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected awaiter to be dispatch-ready after awaited ticket closed, dispatched: %v", dispatched)
	}
}

func TestDispatchReadyBlockedTerminal(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\neffort = \"medium\"\nprofile = \"batch\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{
				DispatchID: fmt.Sprintf("dispatch-%d", len(dispatched)),
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Blocked target", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Awaiter of blocked", "--tier", "worker", "--awaits", a))

	// Dispatch, fail, then block ticket A — blocked is terminal for awaits.
	mustRun(t, "dispatch", a, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", a, "--reason", "bad output")
	mustRun(t, "block", a, "--reason", "needs human input")

	mustRun(t, "dispatch-ready", "--max", "10")

	found := false
	for _, d := range dispatched {
		if d == b {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected awaiter to be dispatch-ready after awaited ticket blocked, dispatched: %v", dispatched)
	}
}

func TestEditInvalidAwaitsValidation(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Editable", "--tier", "worker"))

	// Manually add an invalid awaits reference to the card file.
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Awaits = []string{"NONEXISTENT-999"}
	writeTicket(t, baseDir, id, doc)

	// Validate the card — validateDependencies should catch the invalid reference.
	doc = mustParseTicket(t, baseDir, id)
	err := validateDependencies(baseDir, doc.Card.ID, doc.Card.Awaits)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected dependency not found error for invalid awaits, got %v", err)
	}
}

func TestMigrateUpdatesBothDependsOnAndAwaits(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "BUILD", "--title", "Build")
	mustRun(t, "init", "STARK", "--title", "Stark")

	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "A", "--tier", "worker"))
	// Create B with both depends_on and awaits pointing to A.
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "BUILD", "--title", "B", "--tier", "worker", "--depends-on", a, "--awaits", a))

	mustRun(t, "migrate", a, "STARK")

	bDoc := mustParseTicket(t, baseDir, b)
	if len(bDoc.Card.DependsOn) != 1 || bDoc.Card.DependsOn[0] != "STARK-001" {
		t.Fatalf("depends_on should reference STARK-001, got %#v", bDoc.Card.DependsOn)
	}
	if len(bDoc.Card.Awaits) != 1 || bDoc.Card.Awaits[0] != "STARK-001" {
		t.Fatalf("awaits should reference STARK-001, got %#v", bDoc.Card.Awaits)
	}
}

func TestCreateRejectsSelfAwait(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	// Pre-create a ticket so we know the next ID will be ALPHA-002.
	strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "First", "--tier", "worker"))

	// Attempt to create ALPHA-002 with --awaits ALPHA-002 (self-reference).
	err := run([]string{"create", "--initiative", "ALPHA", "--title", "Self-await", "--tier", "worker", "--awaits", "ALPHA-002"})
	if err == nil {
		t.Fatalf("expected error for self-await, got nil")
	}
	if !strings.Contains(err.Error(), "circular dependency detected") && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected cycle or not-found error for self-await, got: %v", err)
	}
}

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

func TestReconcileRejectsPlaceholderResult(t *testing.T) {
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
				SessionID:  "real-session-456",
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
	if doc.Card.SessionID == nil || *doc.Card.SessionID != "real-session-456" {
		t.Fatalf("unexpected session id: %#v", doc.Card.SessionID)
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
			return &dispatch.DispatchResult{DispatchID: "cfg-dispatch", SessionID: "cfg-session"}, nil
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
			return &dispatch.DispatchResult{DispatchID: "card-dispatch", SessionID: "card-session"}, nil
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
			return &dispatch.DispatchResult{DispatchID: "cli-dispatch", SessionID: "cli-session"}, nil
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

func TestDispatchRetryPreamble(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	var calls []dispatch.DispatchOptions
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			calls = append(calls, opts)
			return &dispatch.DispatchResult{DispatchID: fmt.Sprintf("dispatch-%d", len(calls)), SessionID: fmt.Sprintf("session-%d", len(calls))}, nil
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

func TestDispatchSkillsFromCardFrontmatter(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4\"\nprofile = \"default\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if len(opts.Skills) != 2 || opts.Skills[0] != "web-search" || opts.Skills[1] != "code-review" {
				t.Fatalf("expected card skills [web-search code-review], got %#v", opts.Skills)
			}
			return &dispatch.DispatchResult{DispatchID: "skill-dispatch", SessionID: "skill-session"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Card skills", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Skills = []string{"web-search", "code-review"}
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "dispatch", id)
}

func TestDispatchSkillsFromCLIFlag(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4\"\nprofile = \"default\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if len(opts.Skills) != 1 || opts.Skills[0] != "cli-skill" {
				t.Fatalf("expected CLI skill [cli-skill], got %#v", opts.Skills)
			}
			return &dispatch.DispatchResult{DispatchID: "cli-skill-dispatch", SessionID: "cli-skill-session"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "CLI skill", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--skill", "cli-skill")
}

func TestDispatchSkillsAdditive(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4\"\nprofile = \"default\"\ndefault_skills = [\"config-skill\"]\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			want := []string{"config-skill", "card-skill", "cli-skill"}
			if len(opts.Skills) != 3 || opts.Skills[0] != want[0] || opts.Skills[1] != want[1] || opts.Skills[2] != want[2] {
				t.Fatalf("expected additive skills %v, got %#v", want, opts.Skills)
			}
			return &dispatch.DispatchResult{DispatchID: "add-dispatch", SessionID: "add-session"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Additive skills", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.Skills = []string{"card-skill"}
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "dispatch", id, "--skill", "cli-skill")
}

func TestDispatchWorkDirEmptyByDefault(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4\"\nprofile = \"default\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if opts.WorkDir != "" {
				t.Fatalf("expected empty WorkDir by default, got %q", opts.WorkDir)
			}
			return &dispatch.DispatchResult{DispatchID: "cwd-dispatch", SessionID: "cwd-session"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "No cwd", "--tier", "worker"))

	mustRun(t, "dispatch", id)
}

func TestDispatchWorkDirFromCardFrontmatter(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4\"\nprofile = \"default\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if opts.WorkDir != "/home/user/project" {
				t.Fatalf("expected WorkDir /home/user/project, got %q", opts.WorkDir)
			}
			return &dispatch.DispatchResult{DispatchID: "card-cwd-dispatch", SessionID: "card-cwd-session"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Card cwd", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.WorkDir = stringPtr("/home/user/project")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "dispatch", id)
}

func TestDispatchWorkDirCLIOverridesCard(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4\"\nprofile = \"default\"\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			if opts.WorkDir != "/cli/override" {
				t.Fatalf("expected WorkDir /cli/override, got %q", opts.WorkDir)
			}
			return &dispatch.DispatchResult{DispatchID: "cli-cwd-dispatch", SessionID: "cli-cwd-session"}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "CLI cwd wins", "--tier", "worker"))
	doc := mustParseTicket(t, baseDir, id)
	doc.Card.WorkDir = stringPtr("/card/workdir")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "dispatch", id, "--cwd", "/cli/override")
}

func TestCompletePopulatesTokens(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(dispatchID string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{
				Status: "completed",
				Tokens: &dispatch.TokenData{In: 11, Out: 7, Cache: 3, PeakContext: 101},
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Token test", "--tier", "worker"))

	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	doc := mustParseTicket(t, baseDir, id)
	doc.SetSection("Result", "Completed work.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "complete", id)
	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Tokens == nil {
		t.Fatal("expected tokens to be populated")
	}
	if doc.Card.Tokens.In != 11 || doc.Card.Tokens.Out != 7 || doc.Card.Tokens.Cache != 3 || doc.Card.Tokens.PeakContext != 101 {
		t.Fatalf("unexpected tokens: %#v", doc.Card.Tokens)
	}
}

func TestReconcileCompletedWithResult(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{
				Status: "completed",
				Tokens: &dispatch.TokenData{In: 100, Out: 50, Cache: 30, PeakContext: 200},
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Reconcile me", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	doc := mustParseTicket(t, baseDir, id)
	doc.SetSection("Result", "Agent output here.\n")
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "reconcile")

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDone {
		t.Fatalf("expected done, got %s", doc.Card.Status)
	}
	if doc.Card.Tokens == nil || doc.Card.Tokens.In != 100 {
		t.Fatalf("expected tokens backfilled, got %#v", doc.Card.Tokens)
	}
}

func TestReconcileCompletedNoResult(t *testing.T) {
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
		t.Fatalf("expected failed, got %s", doc.Card.Status)
	}
	if doc.Card.LastAttemptOutcome == nil || *doc.Card.LastAttemptOutcome != "failed" {
		t.Fatalf("unexpected last attempt outcome: %#v", doc.Card.LastAttemptOutcome)
	}
	if !strings.Contains(doc.GetSection("Log"), "worker completed but no result found in ticket") {
		t.Fatalf("expected specific missing-result log, got %q", doc.GetSection("Log"))
	}
}

func TestReconcileFailed(t *testing.T) {
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

func TestReconcileFailedIncludesBackendError(t *testing.T) {
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

func TestReconcileStatusQueryErrorRetriesBeforeFailing(t *testing.T) {
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

func TestReconcileRunning(t *testing.T) {
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

func TestReconcileDryRun(t *testing.T) {
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

func TestReconcileBackfillsDoneTokens(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{
				Status: "completed",
				Tokens: &dispatch.TokenData{In: 9, Out: 4, Cache: 1, PeakContext: 77},
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Done token fill", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	doc := mustParseTicket(t, baseDir, id)
	doc.SetSection("Result", "Completed work.\n")
	writeTicket(t, baseDir, id, doc)
	mustRun(t, "complete", id)

	doc = mustParseTicket(t, baseDir, id)
	doc.Card.Tokens = nil
	writeTicket(t, baseDir, id, doc)

	mustRun(t, "reconcile")

	doc = mustParseTicket(t, baseDir, id)
	if doc.Card.Tokens == nil || doc.Card.Tokens.In != 9 {
		t.Fatalf("expected done tokens backfilled, got %#v", doc.Card.Tokens)
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
				SessionID:  fmt.Sprintf("session-%d", len(dispatched)),
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
			return &dispatch.DispatchResult{DispatchID: "dispatch-ok", SessionID: "session-ok"}, nil
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

func TestTick(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n")
	withWorkingDir(t, baseDir)

	var dispatched []string
	withMockDispatcher(t, &dispatch.MockDispatcher{
		DispatchFunc: func(opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
			dispatched = append(dispatched, strings.TrimSuffix(filepath.Base(opts.TicketPath), ".md"))
			return &dispatch.DispatchResult{DispatchID: "tick-dispatch", SessionID: "tick-session"}, nil
		},
		StatusFunc: func(id string) (*dispatch.StatusResult, error) {
			return &dispatch.StatusResult{
				Status: "completed",
				Tokens: &dispatch.TokenData{In: 5, Out: 2, Cache: 1, PeakContext: 42},
			}, nil
		},
	})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	root := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Root", "--tier", "worker"))
	child := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Child", "--tier", "worker", "--depends-on", root))

	mustRun(t, "dispatch", root, "--engine", "codex", "--model", "gpt-5.4")
	rootDoc := mustParseTicket(t, baseDir, root)
	rootDoc.SetSection("Result", "Finished root.\n")
	writeTicket(t, baseDir, root, rootDoc)
	dispatched = nil

	out := mustRunStdout(t, "tick", "--max-dispatch", "1")
	if !strings.Contains(out, "tick: reconciled 1, dispatched 1") {
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
	if !strings.Contains(out, "tick: reconciled 0, dispatched 0") {
		t.Fatalf("expected stale lock to be reclaimed, got %q", out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock removed after tick, got %v", err)
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
	mustRun(t, "reopen", id)
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	mustRun(t, "fail", id, "--reason", "second failure")

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusBlocked {
		t.Fatalf("status = %s, want blocked", doc.Card.Status)
	}
	if doc.Card.BlockReason == nil || !strings.Contains(*doc.Card.BlockReason, "1 failed attempts") {
		t.Fatalf("unexpected block reason: %#v", doc.Card.BlockReason)
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

	mustRun(t, "init", "RECON", "--title", "Recon")
	mustRun(t, "init", "STARK", "--title", "Stark")

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RECON", "--title", "Explore", "--tier", "worker"))

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

	mustRun(t, "init", "RECON", "--title", "Recon")
	mustRun(t, "init", "STARK", "--title", "Stark")

	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RECON", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RECON", "--title", "B", "--tier", "worker", "--depends-on", a))

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

	mustRun(t, "init", "RECON", "--title", "Recon")
	mustRun(t, "init", "STARK", "--title", "Stark")

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RECON", "--title", "In flight", "--tier", "worker"))
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")

	err := run([]string{"migrate", id, "STARK"})
	if err == nil || !strings.Contains(err.Error(), "dispatched") {
		t.Fatalf("expected dispatched error, got %v", err)
	}
}

func TestMigrateDryRun(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	mustRun(t, "init", "RECON", "--title", "Recon")
	mustRun(t, "init", "STARK", "--title", "Stark")

	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "RECON", "--title", "Preview", "--tier", "worker"))

	out := mustRunStdout(t, "migrate", id, "STARK", "--dry-run")
	if !strings.Contains(out, "Would migrate") {
		t.Fatalf("dry-run should preview: %s", out)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Initiative != "RECON" {
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

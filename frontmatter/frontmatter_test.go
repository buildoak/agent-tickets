package frontmatter

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return data
}

func TestRoundTripFidelity(t *testing.T) {
	fixtures := []string{"open.md", "dispatched.md", "done.md", "failed.md", "blocked.md"}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			doc, err := Parse(loadFixture(t, fixture))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			serialized, err := doc.Serialize()
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}

			reparsed, err := Parse(serialized)
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}

			if !bytes.Equal(doc.Body, reparsed.Body) {
				t.Fatalf("body mismatch after round trip\noriginal:\n%s\nreparsed:\n%s", doc.Body, reparsed.Body)
			}
		})
	}
}

func TestFieldMutationRoundTrip(t *testing.T) {
	doc, err := Parse(loadFixture(t, "open.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	originalCard := doc.Card
	originalBody := append([]byte(nil), doc.Body...)

	doc.Card.Status = StatusDone

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}

	if reparsed.Card.Status != StatusDone {
		t.Fatalf("expected status %q, got %q", StatusDone, reparsed.Card.Status)
	}

	originalCard.Status = StatusDone
	if !reflect.DeepEqual(originalCard, reparsed.Card) {
		t.Fatalf("unexpected card after round trip\nwant: %#v\ngot: %#v", originalCard, reparsed.Card)
	}

	if !bytes.Equal(originalBody, reparsed.Body) {
		t.Fatalf("body changed after field mutation")
	}
}

func TestHeaderMutationPreservesUnchangedBytes(t *testing.T) {
	data := []byte("---\nid: TEST-010\ninitiative: TEST\ntitle: Keep formatting\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-06\nmanual: false\nplan_ref: null\ndepends_on: []\nskills: []\nwork_dir: null\ndispatch_id: null\nsession_id: null\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ntokens: null\n---\n\n## Scope\nKeep\n")
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	doc.Card.Status = StatusDone

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	text := string(serialized)
	if !strings.Contains(text, "status: done\n") {
		t.Fatalf("expected updated status line:\n%s", text)
	}
	for _, line := range []string{
		"id: TEST-010\n",
		"title: Keep formatting\n",
	} {
		if !strings.Contains(text, line) {
			t.Fatalf("expected unchanged line %q in:\n%s", line, text)
		}
	}
	// Deprecated fields (work_dir, tokens) are silently dropped on rewrite.
	for _, deprecated := range []string{"work_dir:", "tokens:"} {
		if strings.Contains(text, deprecated) {
			t.Fatalf("expected deprecated field %q dropped after rewrite:\n%s", deprecated, text)
		}
	}
}

func TestPointerFieldHandling(t *testing.T) {
	doc, err := Parse(loadFixture(t, "open.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if doc.Card.DispatchID != nil || doc.Card.SessionID != nil || doc.Card.Profile != nil || doc.Card.Engine != nil || doc.Card.Model != nil || doc.Card.Effort != nil {
		t.Fatalf("expected nil dispatch/session/profile/engine/model/effort pointers, got %#v", doc.Card)
	}

	dispatchID := "dispatch-1"
	sessionID := "session-1"
	profile := "worker"
	engine := "codex"
	model := "gpt-5.4-mini"
	effort := "high"
	planRef := "plan-1"
	outcome := "done"
	blockReason := "none"

	doc.Card.DispatchID = &dispatchID
	doc.Card.SessionID = &sessionID
	doc.Card.Profile = &profile
	doc.Card.Engine = &engine
	doc.Card.Model = &model
	doc.Card.Effort = &effort
	doc.Card.PlanRef = &planRef
	doc.Card.LastAttemptOutcome = &outcome
	doc.Card.BlockReason = &blockReason

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize with pointers set: %v", err)
	}

	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("reparse with pointers set: %v", err)
	}

	if reparsed.Card.DispatchID == nil || *reparsed.Card.DispatchID != dispatchID {
		t.Fatalf("dispatch_id round trip failed: %#v", reparsed.Card.DispatchID)
	}
	if reparsed.Card.PlanRef == nil || *reparsed.Card.PlanRef != planRef {
		t.Fatalf("plan_ref round trip failed: %#v", reparsed.Card.PlanRef)
	}

	reparsed.Card.DispatchID = nil
	reparsed.Card.SessionID = nil
	reparsed.Card.Profile = nil
	reparsed.Card.Engine = nil
	reparsed.Card.Model = nil
	reparsed.Card.Effort = nil
	reparsed.Card.PlanRef = nil
	reparsed.Card.LastAttemptOutcome = nil
	reparsed.Card.BlockReason = nil

	serialized, err = reparsed.Serialize()
	if err != nil {
		t.Fatalf("serialize with pointers nil: %v", err)
	}

	for _, field := range []string{
		"dispatch_id: null",
		"session_id: null",
		"profile: null",
		"engine: null",
		"model: null",
		"effort: null",
		"plan_ref: null",
		"last_attempt_outcome: null",
		"block_reason: null",
	} {
		if !strings.Contains(string(serialized), field) {
			t.Fatalf("expected serialized output to contain %q", field)
		}
	}
}

func TestTagsAndDependsOnSerialization(t *testing.T) {
	doc := &Document{
		Card: Card{
			ID:         "TEST-006",
			Initiative: "TEST",
			Title:      "Slice serialization",
			Status:     StatusOpen,
			Tier:       TierWorker,
			Tags:       []string{},
			Created:    "2026-04-06",
			Manual:     false,
			DependsOn:  []string{"TEST-001", "TEST-002"},
		},
		Body: []byte("## Result\n\nbody\n"),
	}

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	text := string(serialized)
	if !strings.Contains(text, "tags: []") {
		t.Fatalf("expected empty tags slice to serialize as []\n%s", text)
	}
	if !strings.Contains(text, "depends_on:\n    - TEST-001\n    - TEST-002") && !strings.Contains(text, "depends_on:\n  - TEST-001\n  - TEST-002") {
		t.Fatalf("expected depends_on entries in serialized output\n%s", text)
	}

	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}

	if len(reparsed.Card.Tags) != 0 {
		t.Fatalf("expected empty tags slice, got %#v", reparsed.Card.Tags)
	}
	if !reflect.DeepEqual(doc.Card.DependsOn, reparsed.Card.DependsOn) {
		t.Fatalf("depends_on mismatch\nwant: %#v\ngot: %#v", doc.Card.DependsOn, reparsed.Card.DependsOn)
	}
}

func TestProfileRoundTrip(t *testing.T) {
	data := []byte("---\nid: TEST-011\ninitiative: TEST\ntitle: Profile round trip\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-06\nmanual: false\nplan_ref: null\ndepends_on: []\ndispatch_id: null\nsession_id: null\nprofile: my-profile\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ntokens: null\n---\n\n## Scope\nKeep\n")
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Card.Profile == nil || *doc.Card.Profile != "my-profile" {
		t.Fatalf("profile parse mismatch: %#v", doc.Card.Profile)
	}

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if !strings.Contains(string(serialized), "profile: my-profile\n") {
		t.Fatalf("expected profile in serialized output:\n%s", serialized)
	}

	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed.Card.Profile == nil || *reparsed.Card.Profile != "my-profile" {
		t.Fatalf("profile reparse mismatch: %#v", reparsed.Card.Profile)
	}
}

func TestSectionExtraction(t *testing.T) {
	doc, err := Parse(loadFixture(t, "done.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := "The task was completed successfully. Found 8 relevant papers with summaries.\nOutput written to `research/papers-survey.md`.\n\n"
	if got := doc.GetSection("Result"); got != want {
		t.Fatalf("unexpected Result section\nwant: %q\ngot: %q", want, got)
	}

	if got := doc.GetSection("Nonexistent"); got != "" {
		t.Fatalf("expected empty string for missing section, got %q", got)
	}
}

func TestSectionMutation(t *testing.T) {
	doc, err := Parse(loadFixture(t, "open.md"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	doc.SetSection("Result", "new content\n")
	if got := doc.GetSection("Result"); got != "new content\n" {
		t.Fatalf("unexpected Result section after set: %q", got)
	}

	doc.AppendToSection("Log", "- 2026-04-06T10:30:00Z done -- appended\n")
	logSection := doc.GetSection("Log")
	if !strings.Contains(logSection, "- 2026-04-06T10:30:00Z done -- appended\n") {
		t.Fatalf("log section missing appended entry: %q", logSection)
	}

	doc.AppendToSection("New Section", "created\n")
	if got := doc.GetSection("New Section"); got != "created\n" {
		t.Fatalf("unexpected new section content: %q", got)
	}
}

func TestSectionLookupDoesNotSubstringMatch(t *testing.T) {
	doc, err := Parse([]byte("---\nid: TEST-011\ninitiative: TEST\ntitle: Sections\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-06\nmanual: false\nplan_ref: null\ndepends_on: []\ndispatch_id: null\nsession_id: null\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ntokens: null\n---\r\n\r\n## Result Archive\r\nold\r\n\r\n## Result\r\nexact\r\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := doc.GetSection("Result"); got != "exact\r\n" {
		t.Fatalf("expected exact Result match, got %q", got)
	}
}

func TestParseEdgeCases(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		data := []byte("---\nid: TEST-007\ninitiative: TEST\ntitle: Empty body\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-06\nmanual: false\nplan_ref: null\ndepends_on: []\ndispatch_id: null\nsession_id: null\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ntokens: null\n---")
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("parse empty body: %v", err)
		}
		if len(doc.Body) != 0 {
			t.Fatalf("expected empty body, got %q", doc.Body)
		}
	})

	t.Run("no frontmatter", func(t *testing.T) {
		if _, err := Parse([]byte("# heading\nno frontmatter\n")); err == nil {
			t.Fatal("expected error for document without frontmatter")
		}
	})

	t.Run("utf8 body", func(t *testing.T) {
		data := []byte("---\nid: TEST-008\ninitiative: TEST\ntitle: UTF-8\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-06\nmanual: false\nplan_ref: null\ndepends_on: []\ndispatch_id: null\nsession_id: null\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ntokens: null\n---\n\n## Context\ncafe, na\xcc\x88ive, \xe6\x9d\xb1\xe4\xba\xac, \xf0\x9f\x9a\x80\n")
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("parse utf8: %v", err)
		}
		serialized, err := doc.Serialize()
		if err != nil {
			t.Fatalf("serialize utf8: %v", err)
		}
		reparsed, err := Parse(serialized)
		if err != nil {
			t.Fatalf("reparse utf8: %v", err)
		}
		if !bytes.Equal(doc.Body, reparsed.Body) {
			t.Fatalf("utf8 body changed")
		}
	})

	t.Run("multiline yaml", func(t *testing.T) {
		data := []byte("---\nid: TEST-009\ninitiative: TEST\ntitle: |\n  line one\n  line two\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-06\nmanual: false\nplan_ref: null\ndepends_on: []\ndispatch_id: null\nsession_id: null\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ntokens: null\n---\n")
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("parse multiline yaml: %v", err)
		}
		if doc.Card.Title != "line one\nline two\n" {
			t.Fatalf("unexpected multiline title: %q", doc.Card.Title)
		}
	})
}

func TestParseFileAndWriteFile(t *testing.T) {
	doc, err := Parse(loadFixture(t, "dispatched.md"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "ticket.md")
	if err := doc.WriteFile(path); err != nil {
		t.Fatalf("write file: %v", err)
	}

	reparsed, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}

	if !reflect.DeepEqual(doc.Card, reparsed.Card) {
		t.Fatalf("card mismatch after write/read\nwant: %#v\ngot: %#v", doc.Card, reparsed.Card)
	}
	if !bytes.Equal(doc.Body, reparsed.Body) {
		t.Fatalf("body mismatch after write/read")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	doc, err := Parse(loadFixture(t, "dispatched.md"))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "ticket.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := doc.WriteFile(path); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	want, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("atomic write mismatch")
	}

	matches, err := filepath.Glob(filepath.Join(dir, ".ticket.md.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected temp files cleaned up, got %#v", matches)
	}
}

func TestAwaitsRoundTrip(t *testing.T) {
	doc := &Document{
		Card: Card{
			ID:         "TEST-020",
			Initiative: "TEST",
			Title:      "Awaits round trip",
			Status:     StatusOpen,
			Tier:       TierWorker,
			Tags:       []string{},
			Created:    "2026-04-13",
			Manual:     false,
			DependsOn:  []string{},
			Awaits:     []string{"FOO-001", "FOO-002"},
			Skills:     []string{},
		},
		Body: []byte("## Result\n\nbody\n"),
	}

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	text := string(serialized)
	if !strings.Contains(text, "FOO-001") || !strings.Contains(text, "FOO-002") {
		t.Fatalf("expected awaits entries in serialized output\n%s", text)
	}

	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}

	if !reflect.DeepEqual(doc.Card.Awaits, reparsed.Card.Awaits) {
		t.Fatalf("awaits mismatch\nwant: %#v\ngot: %#v", doc.Card.Awaits, reparsed.Card.Awaits)
	}

	// Empty awaits normalizes to [].
	emptyDoc := &Document{
		Card: Card{
			ID:         "TEST-021",
			Initiative: "TEST",
			Title:      "Empty awaits",
			Status:     StatusOpen,
			Tier:       TierWorker,
			Tags:       []string{},
			Created:    "2026-04-13",
			DependsOn:  []string{},
			Skills:     []string{},
		},
		Body: []byte("## Result\n"),
	}

	serialized, err = emptyDoc.Serialize()
	if err != nil {
		t.Fatalf("serialize empty awaits: %v", err)
	}

	reparsed, err = Parse(serialized)
	if err != nil {
		t.Fatalf("reparse empty awaits: %v", err)
	}
	if reparsed.Card.Awaits == nil || len(reparsed.Card.Awaits) != 0 {
		t.Fatalf("expected empty awaits slice, got %#v", reparsed.Card.Awaits)
	}
}

func TestDefaultProfileAndSkillsSurviveMutation(t *testing.T) {
	data := []byte("---\nid: INIT-001\ninitiative: INIT\ntitle: Initiative card\nstatus: open\ntier: worker\ntags: []\ncreated: 2026-04-13\nmanual: false\nplan_ref: null\ndepends_on: []\nawaits: []\nskills: []\ndispatch_id: null\nsession_id: null\ndispatched_at: null\nprofile: null\nengine: null\nmodel: null\neffort: null\nattempts: 0\nlast_attempt_outcome: null\nblock_reason: null\ndefault_profile: scout\ndefault_skills:\n    - web-search\n    - paper-ops\ntokens: null\n---\n\n## Scope\nTest\n")
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if doc.Card.DefaultProfile == nil || *doc.Card.DefaultProfile != "scout" {
		t.Fatalf("expected default_profile 'scout', got %#v", doc.Card.DefaultProfile)
	}
	if len(doc.Card.DefaultSkills) != 2 || doc.Card.DefaultSkills[0] != "web-search" || doc.Card.DefaultSkills[1] != "paper-ops" {
		t.Fatalf("expected default_skills [web-search, paper-ops], got %#v", doc.Card.DefaultSkills)
	}

	// Mutate an unrelated field.
	doc.Card.Title = "Mutated title"

	serialized, err := doc.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	text := string(serialized)
	if !strings.Contains(text, "default_profile: scout\n") {
		t.Fatalf("default_profile lost after mutation:\n%s", text)
	}
	if !strings.Contains(text, "web-search") || !strings.Contains(text, "paper-ops") {
		t.Fatalf("default_skills lost after mutation:\n%s", text)
	}

	reparsed, err := Parse(serialized)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed.Card.DefaultProfile == nil || *reparsed.Card.DefaultProfile != "scout" {
		t.Fatalf("default_profile lost after round trip: %#v", reparsed.Card.DefaultProfile)
	}
	if len(reparsed.Card.DefaultSkills) != 2 || reparsed.Card.DefaultSkills[0] != "web-search" || reparsed.Card.DefaultSkills[1] != "paper-ops" {
		t.Fatalf("default_skills lost after round trip: %#v", reparsed.Card.DefaultSkills)
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []Status{StatusDone, StatusFailed, StatusBlocked, StatusClosed}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Fatalf("expected %q to be terminal", s)
		}
	}

	nonTerminal := []Status{StatusOpen, StatusDispatched}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Fatalf("expected %q to be non-terminal", s)
		}
	}
}

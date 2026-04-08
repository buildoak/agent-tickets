package frontmatter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"gopkg.in/yaml.v3"
)

func (d *Document) Serialize() ([]byte, error) {
	card := d.Card
	normalizeCardSlices(&card)

	header, err := d.serializeHeader(card)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Grow(len(header) + len(d.Body) + 8)
	out.WriteString("---\n")
	out.Write(header)
	out.WriteString("---")
	if len(d.Body) == 0 {
		return out.Bytes(), nil
	}
	out.WriteByte('\n')
	out.Write(d.Body)

	return out.Bytes(), nil
}

func (d *Document) WriteFile(path string) error {
	data, err := d.Serialize()
	if err != nil {
		return err
	}

	return writeFileAtomic(path, data, 0o644)
}

var cardFieldOrder = []string{
	"id",
	"initiative",
	"title",
	"status",
	"tier",
	"tags",
	"created",
	"manual",
	"plan_ref",
	"depends_on",
	"dispatch_id",
	"session_id",
	"profile",
	"engine",
	"model",
	"effort",
	"work_dir",
	"skills",
	"attempts",
	"last_attempt_outcome",
	"block_reason",
	"tokens",
}

func (d *Document) serializeHeader(card Card) ([]byte, error) {
	if len(d.rawHeader) == 0 {
		return yaml.Marshal(&card)
	}

	original := d.originalCard
	normalizeCardSlices(&original)
	if reflect.DeepEqual(original, card) {
		return append([]byte(nil), d.rawHeader...), nil
	}

	var out bytes.Buffer
	seen := make(map[string]struct{}, len(cardFieldOrder))
	for _, key := range d.headerOrder() {
		seen[key] = struct{}{}
		fieldBytes, err := d.serializeField(key, original, card)
		if err != nil {
			return nil, err
		}
		out.Write(fieldBytes)
	}
	for _, key := range cardFieldOrder {
		if _, ok := seen[key]; ok {
			continue
		}
		fieldBytes, err := marshalCardField(key, card)
		if err != nil {
			return nil, err
		}
		out.Write(fieldBytes)
	}

	return out.Bytes(), nil
}

func (d *Document) headerOrder() []string {
	if len(d.fieldOrder) > 0 {
		return append([]string(nil), d.fieldOrder...)
	}
	return append([]string(nil), cardFieldOrder...)
}

func (d *Document) serializeField(key string, original, current Card) ([]byte, error) {
	if cardFieldEqual(key, original, current) {
		if raw := d.rawFieldBytes[key]; len(raw) > 0 {
			return append([]byte(nil), raw...), nil
		}
	}
	return marshalCardField(key, current)
}

func marshalCardField(key string, card Card) ([]byte, error) {
	var value any
	switch key {
	case "id":
		value = card.ID
	case "initiative":
		value = card.Initiative
	case "title":
		value = card.Title
	case "status":
		value = card.Status
	case "tier":
		value = card.Tier
	case "tags":
		value = card.Tags
	case "created":
		value = card.Created
	case "manual":
		value = card.Manual
	case "plan_ref":
		value = card.PlanRef
	case "depends_on":
		value = card.DependsOn
	case "dispatch_id":
		value = card.DispatchID
	case "session_id":
		value = card.SessionID
	case "profile":
		value = card.Profile
	case "engine":
		value = card.Engine
	case "model":
		value = card.Model
	case "effort":
		value = card.Effort
	case "work_dir":
		value = card.WorkDir
	case "skills":
		value = card.Skills
	case "attempts":
		value = card.Attempts
	case "last_attempt_outcome":
		value = card.LastAttemptOutcome
	case "block_reason":
		value = card.BlockReason
	case "tokens":
		value = card.Tokens
	default:
		return nil, fmt.Errorf("unknown frontmatter field: %s", key)
	}

	return marshalYAMLField(key, value)
}

func marshalYAMLField(key string, value any) ([]byte, error) {
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{}
	if err := valueNode.Encode(value); err != nil {
		return nil, fmt.Errorf("encode frontmatter field %s: %w", key, err)
	}

	doc := &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind:    yaml.MappingNode,
			Content: []*yaml.Node{keyNode, valueNode},
		}},
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal frontmatter field %s: %w", key, err)
	}
	return out, nil
}

func cardFieldEqual(key string, a, b Card) bool {
	switch key {
	case "id":
		return a.ID == b.ID
	case "initiative":
		return a.Initiative == b.Initiative
	case "title":
		return a.Title == b.Title
	case "status":
		return a.Status == b.Status
	case "tier":
		return a.Tier == b.Tier
	case "tags":
		return reflect.DeepEqual(a.Tags, b.Tags)
	case "created":
		return a.Created == b.Created
	case "manual":
		return a.Manual == b.Manual
	case "plan_ref":
		return reflect.DeepEqual(a.PlanRef, b.PlanRef)
	case "depends_on":
		return reflect.DeepEqual(a.DependsOn, b.DependsOn)
	case "dispatch_id":
		return reflect.DeepEqual(a.DispatchID, b.DispatchID)
	case "session_id":
		return reflect.DeepEqual(a.SessionID, b.SessionID)
	case "profile":
		return reflect.DeepEqual(a.Profile, b.Profile)
	case "engine":
		return reflect.DeepEqual(a.Engine, b.Engine)
	case "model":
		return reflect.DeepEqual(a.Model, b.Model)
	case "effort":
		return reflect.DeepEqual(a.Effort, b.Effort)
	case "work_dir":
		return reflect.DeepEqual(a.WorkDir, b.WorkDir)
	case "skills":
		return reflect.DeepEqual(a.Skills, b.Skills)
	case "attempts":
		return a.Attempts == b.Attempts
	case "last_attempt_outcome":
		return reflect.DeepEqual(a.LastAttemptOutcome, b.LastAttemptOutcome)
	case "block_reason":
		return reflect.DeepEqual(a.BlockReason, b.BlockReason)
	case "tokens":
		return reflect.DeepEqual(a.Tokens, b.Tokens)
	default:
		return false
	}
}

func normalizeCardSlices(card *Card) {
	if card.Tags == nil {
		card.Tags = []string{}
	}
	if card.DependsOn == nil {
		card.DependsOn = []string{}
	}
	if card.Skills == nil {
		card.Skills = []string{}
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}

	cleanup = false
	return nil
}

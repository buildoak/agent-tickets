package frontmatter

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var (
	errMissingFrontmatter = fmt.Errorf("frontmatter delimiters not found")
	delimiterLine         = []byte("---")
)

func Parse(data []byte) (*Document, error) {
	headerStart, headerEnd, bodyStart, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}

	var card Card
	header := append([]byte(nil), data[headerStart:headerEnd]...)
	if err := yaml.Unmarshal(header, &card); err != nil {
		return nil, fmt.Errorf("parse yaml header: %w", err)
	}

	fieldOrder, rawFieldBytes, err := extractRawFields(header)
	if err != nil {
		return nil, err
	}
	normalizeCardSlices(&card)

	return &Document{
		Card:          card,
		Body:          append([]byte(nil), data[bodyStart:]...),
		rawHeader:     header,
		originalCard:  cloneCard(card),
		fieldOrder:    fieldOrder,
		rawFieldBytes: rawFieldBytes,
	}, nil
}

func ParseFile(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return Parse(data)
}

func splitFrontmatter(data []byte) (headerStart int, headerEnd int, bodyStart int, err error) {
	if len(data) < len(delimiterLine) || !bytes.Equal(data[:len(delimiterLine)], delimiterLine) {
		return 0, 0, 0, errMissingFrontmatter
	}

	openEnd, ok := findLineEnd(data, 0)
	if !ok {
		return 0, 0, 0, errMissingFrontmatter
	}
	if !bytes.Equal(trimLineEnding(data[:openEnd]), delimiterLine) {
		return 0, 0, 0, errMissingFrontmatter
	}

	headerStart = openEnd
	lineStart := headerStart
	for lineStart <= len(data) {
		lineEnd, found := findLineEnd(data, lineStart)
		if !found {
			lineEnd = len(data)
		}

		if bytes.Equal(trimLineEnding(data[lineStart:lineEnd]), delimiterLine) {
			return headerStart, lineStart, lineEnd, nil
		}

		if !found {
			break
		}
		lineStart = lineEnd
	}

	return 0, 0, 0, errMissingFrontmatter
}

func findLineEnd(data []byte, start int) (int, bool) {
	for i := start; i < len(data); i++ {
		if data[i] == '\n' {
			return i + 1, true
		}
	}

	return len(data), false
}

func trimLineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	return line
}

func cloneCard(card Card) Card {
	cloned := card
	if card.Tags != nil {
		cloned.Tags = append([]string(nil), card.Tags...)
	}
	if card.DependsOn != nil {
		cloned.DependsOn = append([]string(nil), card.DependsOn...)
	}
	cloned.PlanRef = cloneStringPtr(card.PlanRef)
	cloned.DispatchID = cloneStringPtr(card.DispatchID)
	cloned.SessionID = cloneStringPtr(card.SessionID)
	cloned.Profile = cloneStringPtr(card.Profile)
	cloned.Engine = cloneStringPtr(card.Engine)
	cloned.Model = cloneStringPtr(card.Model)
	cloned.Effort = cloneStringPtr(card.Effort)
	cloned.WorkDir = cloneStringPtr(card.WorkDir)
	if card.Skills != nil {
		cloned.Skills = append([]string(nil), card.Skills...)
	}
	cloned.LastAttemptOutcome = cloneStringPtr(card.LastAttemptOutcome)
	cloned.BlockReason = cloneStringPtr(card.BlockReason)
	if card.Tokens != nil {
		tokens := *card.Tokens
		cloned.Tokens = &tokens
	}
	return cloned
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func extractRawFields(header []byte) ([]string, map[string][]byte, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(header, &node); err != nil {
		return nil, nil, fmt.Errorf("parse yaml header structure: %w", err)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse yaml header structure: expected top-level mapping")
	}

	mapping := node.Content[0]
	lineOffsets := headerLineOffsets(header)
	order := make([]string, 0, len(mapping.Content)/2)
	fields := make(map[string][]byte, len(mapping.Content)/2)
	for i := 0; i < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		valueNode := mapping.Content[i+1]
		key := keyNode.Value
		start := lineOffset(lineOffsets, keyNode.Line)
		endLine := valueNode.Line + countNodeLines(valueNode)
		end := lineOffset(lineOffsets, endLine)
		if end < start {
			end = start
		}
		order = append(order, key)
		fields[key] = append([]byte(nil), header[start:end]...)
	}

	return order, fields, nil
}

func headerLineOffsets(header []byte) []int {
	offsets := []int{0}
	for i, b := range header {
		if b == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	offsets = append(offsets, len(header))
	return offsets
}

func lineOffset(offsets []int, line int) int {
	if line <= 1 {
		return 0
	}
	index := line - 1
	if index >= len(offsets) {
		return offsets[len(offsets)-1]
	}
	return offsets[index]
}

func countNodeLines(node *yaml.Node) int {
	maxLine := node.Line
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Line > maxLine {
			maxLine = n.Line
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(node)
	return max(1, maxLine-node.Line+1)
}

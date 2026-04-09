package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/frontmatter"
)

var ticketIDPattern = regexp.MustCompile(`^(?P<initiative>.+)-(?P<seq>\d{3})$`)

func resolveBaseDir(args []string) (string, []string, error) {
	filtered := make([]string, 0, len(args))
	var base string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--base":
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("missing value for --base")
			}
			base = args[i+1]
			i++
		case strings.HasPrefix(arg, "--base="):
			base = strings.TrimPrefix(arg, "--base=")
		default:
			filtered = append(filtered, arg)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return "", nil, err
	}
	if base == "" {
		base = cfg.BaseDir
	}
	if base == "" {
		return "", nil, fmt.Errorf("tickets base directory not set")
	}

	return base, filtered, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	return fs
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func findTicketFile(baseDir, id string) (string, error) {
	initiative, _, err := parseTicketID(id)
	if err != nil {
		return "", err
	}

	path := filepath.Join(baseDir, "cards", initiative, id+".md")
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("ticket path is a directory: %s", path)
		}
		return path, nil
	}

	if !os.IsNotExist(err) {
		return "", err
	}

	files, err := allTicketFiles(baseDir)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(file), ".md"), id) {
			return file, nil
		}
	}

	return "", fmt.Errorf("ticket not found: %s", id)
}

func nextSequence(initiativeDir, initiative string) (int, error) {
	pattern := filepath.Join(initiativeDir, initiative+"-*.md")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, err
	}

	maxSeq := 0
	for _, match := range matches {
		base := strings.TrimSuffix(filepath.Base(match), ".md")
		_, seq, err := parseTicketID(base)
		if err != nil {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}

	return maxSeq + 1, nil
}

func parseTicketID(id string) (initiative string, seq int, err error) {
	matches := ticketIDPattern.FindStringSubmatch(id)
	if matches == nil {
		return "", 0, fmt.Errorf("invalid ticket id: %s", id)
	}

	seq, err = strconv.Atoi(matches[2])
	if err != nil {
		return "", 0, fmt.Errorf("invalid ticket id: %s", id)
	}

	return matches[1], seq, nil
}

func allTicketFiles(baseDir string) ([]string, error) {
	root := filepath.Join(baseDir, "cards")
	if filepath.Base(baseDir) == "cards" || filepath.Base(filepath.Dir(baseDir)) == "cards" {
		root = baseDir
	}

	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func timestamp() string {
	return time.Now().Format(time.RFC3339)
}

func dateOnly() string {
	return time.Now().Format("2006-01-02")
}

func initiativeExists(baseDir, initiative string) (string, error) {
	initFile := filepath.Join(baseDir, "INITIATIVES", initiative+".md")
	info, err := os.Stat(initFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("initiative not found: %s", initiative)
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("initiative path is a directory: %s", initFile)
	}

	return filepath.Join(baseDir, "cards", initiative), nil
}

func loadTicket(baseDir, id string) (string, *frontmatter.Document, error) {
	path, err := findTicketFile(baseDir, id)
	if err != nil {
		return "", nil, err
	}

	doc, err := frontmatter.ParseFile(path)
	if err != nil {
		return "", nil, err
	}

	return path, doc, nil
}

func ticketAbsPath(baseDir, path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Abs(path)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func appendLog(doc *frontmatter.Document, line string) {
	doc.AppendToSection("Log", fmt.Sprintf("- %s %s\n", timestamp(), line))
}

func clearDispatchFields(card *frontmatter.Card) {
	// Only clear ephemeral dispatch-runtime fields.
	// Card-spec fields (engine, model, effort, profile, work_dir, skills)
	// are intentionally preserved — they define HOW the ticket should be
	// dispatched on retry and are set by the user or dispatch-ready logic.
	card.DispatchID = nil
	card.SessionID = nil
}

func ticketTemplate(card frontmatter.Card) *frontmatter.Document {
	body := strings.Join([]string{
		"## Context",
		"",
		"## Scope",
		"",
		"## Result",
		"",
		"## Log",
		"",
	}, "\n")

	return &frontmatter.Document{
		Card: card,
		Body: []byte(body),
	}
}

func isPlaceholder(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	placeholders := []string{
		"[filled by",
		"[to be filled",
		"todo",
		"[placeholder",
	}
	for _, placeholder := range placeholders {
		if strings.HasPrefix(lower, placeholder) {
			return true
		}
	}
	return false
}

func dependentMap(baseDir string) (map[string][]string, error) {
	files, err := allTicketFiles(baseDir)
	if err != nil {
		return nil, err
	}

	dependents := make(map[string][]string)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			return nil, err
		}
		for _, dep := range doc.Card.DependsOn {
			dependents[dep] = append(dependents[dep], doc.Card.ID)
		}
	}

	for key := range dependents {
		sort.Strings(dependents[key])
	}
	return dependents, nil
}

func collectCascadeIDs(root string, dependents map[string][]string, seen map[string]struct{}) []string {
	if _, ok := seen[root]; ok {
		return nil
	}
	seen[root] = struct{}{}

	ids := []string{root}
	for _, dep := range dependents[root] {
		ids = append(ids, collectCascadeIDs(dep, dependents, seen)...)
	}
	return ids
}

func validateDependencies(baseDir string, ticketID string, deps []string) error {
	graph, err := buildDepGraph(baseDir)
	if err != nil {
		return err
	}
	graph[ticketID] = append([]string(nil), deps...)
	return validateDependencyGraph(graph, ticketID, deps)
}

func buildDepGraph(baseDir string) (map[string][]string, error) {
	files, err := allTicketFiles(baseDir)
	if err != nil {
		return nil, err
	}

	graph := make(map[string][]string, len(files))
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}
		graph[doc.Card.ID] = append([]string(nil), doc.Card.DependsOn...)
	}

	return graph, nil
}

func validateDependencyGraph(graph map[string][]string, ticketID string, deps []string) error {
	for _, dep := range deps {
		if _, ok := graph[dep]; !ok {
			return fmt.Errorf("dependency not found: %s", dep)
		}
	}

	if cycle := detectCycle(graph, ticketID); len(cycle) > 0 {
		return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
	}

	if depth, chain := maxDependencyDepth(graph, ticketID, nil); depth > 3 {
		return fmt.Errorf("dependency chain too deep (max 3): %s", strings.Join(chain, " -> "))
	}

	return nil
}

func detectCycle(graph map[string][]string, start string) []string {
	visited := make(map[string]bool, len(graph))
	stack := make(map[string]int, len(graph))
	var path []string

	var dfs func(string) []string
	dfs = func(node string) []string {
		visited[node] = true
		stack[node] = len(path)
		path = append(path, node)

		for _, dep := range graph[node] {
			if index, ok := stack[dep]; ok {
				cycle := append([]string(nil), path[index:]...)
				cycle = append(cycle, dep)
				return cycle
			}
			if visited[dep] {
				continue
			}
			if cycle := dfs(dep); len(cycle) > 0 {
				return cycle
			}
		}

		delete(stack, node)
		path = path[:len(path)-1]
		return nil
	}

	return dfs(start)
}

func maxDependencyDepth(graph map[string][]string, node string, path []string) (int, []string) {
	return maxDependencyDepthVisited(graph, node, path, make(map[string]bool))
}

func maxDependencyDepthVisited(graph map[string][]string, node string, path []string, visited map[string]bool) (int, []string) {
	if visited[node] {
		return len(path), append([]string(nil), path...)
	}
	visited[node] = true

	path = append(path, node)
	deps := graph[node]
	if len(deps) == 0 {
		return len(path), append([]string(nil), path...)
	}

	maxDepth := len(path)
	longest := append([]string(nil), path...)
	for _, dep := range deps {
		depth, chain := maxDependencyDepthVisited(graph, dep, path, visited)
		if depth > maxDepth {
			maxDepth = depth
			longest = chain
		}
	}

	return maxDepth, longest
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
)

func cmdEdit(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("edit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets edit TICKET-ID")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets edit TICKET-ID")
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		return fmt.Errorf("EDITOR is not set")
	}

	path, err := findTicketFile(baseDir, id)
	if err != nil {
		return err
	}

	cmd := exec.Command(editor, path)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	doc, err := frontmatter.ParseFile(path)
	if err != nil {
		return err
	}
	if err := validateDependencies(baseDir, doc.Card.ID, doc.Card.DependsOn); err != nil {
		return err
	}
	return validateDependencies(baseDir, doc.Card.ID, doc.Card.Awaits)
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdInit(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	initiative := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		initiative = args[0]
		args = args[1:]
	}

	fs := newFlagSet("init")
	title := fs.String("title", "", "initiative title")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if initiative == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets init INITIATIVE --title \"...\"")
		}
		initiative = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets init INITIATIVE --title \"...\"")
	}
	if strings.TrimSpace(*title) == "" {
		return fmt.Errorf("--title is required")
	}

	initiativesDir := filepath.Join(baseDir, "INITIATIVES")
	if err := os.MkdirAll(initiativesDir, 0o755); err != nil {
		return err
	}

	initiativePath := filepath.Join(initiativesDir, initiative+".md")
	if _, err := os.Stat(initiativePath); err == nil {
		return fmt.Errorf("initiative already exists")
	} else if !os.IsNotExist(err) {
		return err
	}

	content := fmt.Sprintf(`---
initiative: %s
title: %q
status: active
created: %s
---

## Objective

## Context

## Conventions
`, initiative, *title, dateOnly())

	if err := os.WriteFile(initiativePath, []byte(content), 0o644); err != nil {
		return err
	}

	return os.MkdirAll(filepath.Join(baseDir, "cards", initiative), 0o755)
}

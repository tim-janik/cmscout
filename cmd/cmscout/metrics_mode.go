package main

import (
	"flag"
	"fmt"
	"os"
)

func (mf metrics_flags) input_mode(paths []string, cf compareFlags) (string, error) {
	mode := ""
	for _, option := range []struct {
		mode     string
		selected bool
	}{
		{"scan", mf.scan}, {"staged", mf.staged}, {"worktree", mf.worktree}, {"revision", mf.revision != ""},
	} {
		if option.selected {
			if mode != "" {
				return "", fmt.Errorf("choose one of --scan, --staged, --worktree, or --revision")
			}
			mode = option.mode
		}
	}
	if mode != "" {
		return mode, nil
	}
	if len(paths) == 2 || cf.oldName != "" || cf.newName != "" || cf.beforeFile != "" || cf.afterFile != "" {
		return "pair", nil
	}
	if len(paths) == 1 && mf.name == "" && mf.stdin_name == "" {
		if info, err := os.Stat(paths[0]); err == nil && info.IsDir() {
			return "scan", nil
		}
	}
	return "file", nil
}

func (mf metrics_flags) validate_flags(fs *flag.FlagSet, mode string) error {
	var invalid string
	fs.Visit(func(option *flag.Flag) {
		if invalid == "" && !metric_flag_allowed(option.Name, mode) {
			invalid = option.Name
		}
		if option.Name == "parent" && (mf.parent < 1 || mf.base != "") {
			invalid = option.Name
		}
	})
	if invalid != "" {
		return fmt.Errorf("--%s cannot be used with this metrics input mode", invalid)
	}
	if mf.format != "text" && mf.format != "json" {
		return fmt.Errorf("unknown metrics format %q: expected text or json", mf.format)
	}
	return nil
}

func metric_flag_allowed(name, mode string) bool {
	git_mode := mode == "staged" || mode == "worktree" || mode == "revision"
	switch name {
	case "metrics", "format", "root", "explain", "no-color":
		return true
	case "name", "stdin-name":
		return mode == "file"
	case "scan", "staged", "worktree", "revision":
		return mode == name
	case "include", "exclude":
		return mode == "scan" || git_mode
	case "base", "parent":
		return mode == "revision"
	case "old", "new", "B", "A", "before-contents", "after-contents":
		return mode == "pair"
	case "word-diff", "word-diff-span-threshold", "ignore-all-space":
		return mode == "pair" || git_mode
	}
	return false
}

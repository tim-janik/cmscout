package source

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"cmscout/pkg/lang"
)

type GitOptions struct {
	Directory string
	Mode      string
	Revision  string
	Base      string
	Parent    int
	Filter    Filter
}

type Selection struct {
	Mode   string `json:"mode"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type FilePair struct {
	Before *File
	After  *File
	Change string
}

type GitSet struct {
	Root        string
	Input       Selection
	Files       []FilePair
	Skipped     []Notice
	Diagnostics []Notice
}

type git_change struct {
	before_path, after_path string
	before_mode, after_mode string
	before_oid, after_oid   string
	status                  string
}

func git_command(directory string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", arguments[0], err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

func git_revision(root, revision string) (string, error) {
	output, err := git_command(root, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	return strings.TrimSuffix(string(output), "\n"), err
}

func git_selection(root string, options GitOptions) (Selection, []string, error) {
	selection := Selection{Mode: options.Mode}
	arguments := []string{"diff", "--raw", "-z", "--no-abbrev", "--no-ext-diff", "--no-textconv",
		"--find-renames", "--ignore-submodules=none", "--no-relative"}
	switch options.Mode {
	case "staged":
		selection.After = "index"
		head, err := git_revision(root, "HEAD")
		if err != nil {
			if _, symbolic_error := git_command(root, "symbolic-ref", "-q", "HEAD"); symbolic_error != nil {
				return selection, nil, err
			}
			selection.Before = "empty"
			arguments = append(arguments, "--cached")
		} else {
			selection.Before = head
			arguments = append(arguments, "--cached", head)
		}
	case "worktree":
		selection.Before, selection.After = "index", "worktree"
	case "revision":
		revision, err := git_revision(root, options.Revision)
		if err != nil {
			return selection, nil, err
		}
		selection.After = revision
		if options.Base != "" {
			selection.Before, err = git_revision(root, options.Base)
		} else {
			selection.Before, err = git_parent(root, revision, options.Parent)
		}
		if err != nil {
			return selection, nil, err
		}
		arguments[0] = "diff-tree"
		arguments = append(arguments, "-r", "--no-commit-id")
		if selection.Before == "empty" {
			arguments = append(arguments, "--root", revision)
		} else {
			arguments = append(arguments, selection.Before, revision)
		}
	default:
		return selection, nil, fmt.Errorf("unknown Git metrics mode %q", options.Mode)
	}
	return selection, append(arguments, "--"), nil
}

func git_parent(root, revision string, parent int) (string, error) {
	output, err := git_command(root, "cat-file", "commit", revision)
	if err != nil {
		return "", err
	}
	header, _, _ := strings.Cut(string(output), "\n\n")
	parents := []string{}
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "parent ") {
			parents = append(parents, strings.TrimPrefix(line, "parent "))
		}
	}
	count := len(parents)
	if parent == 0 {
		if count > 1 {
			return "", fmt.Errorf("merge revision has %d parents; choose --parent <number> or --base <revision>", count)
		}
		if count == 0 {
			return "empty", nil
		}
		parent = 1
	}
	if parent < 1 || parent > count {
		return "", fmt.Errorf("parent %d is outside this revision's %d parents", parent, count)
	}
	return parents[parent-1], nil
}

func Git(options GitOptions) (*GitSet, error) {
	if options.Mode != "revision" && (options.Revision != "" || options.Base != "" || options.Parent != 0) ||
		options.Mode == "revision" && (options.Revision == "" || options.Parent < 0 || options.Base != "" && options.Parent != 0) {
		return nil, fmt.Errorf("revision and parent options require one revision input mode")
	}
	if err := options.Filter.Validate(); err != nil {
		return nil, err
	}
	directory := options.Directory
	if directory == "" {
		directory = "."
	}
	output, err := git_command(directory, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root := strings.TrimSuffix(string(output), "\n")
	if options.Directory != "" {
		absolute, err := filepath.Abs(options.Directory)
		if err != nil {
			return nil, err
		}
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		if absolute != root {
			return nil, fmt.Errorf("Git metrics --root must name the repository root %q", root)
		}
	}
	selection, arguments, err := git_selection(root, options)
	if err != nil {
		return nil, err
	}
	output, err = git_command(root, arguments...)
	if err != nil {
		return nil, err
	}
	changes, err := parse_git_changes(output)
	if err != nil {
		return nil, err
	}
	result := &GitSet{Root: root, Input: selection, Files: []FilePair{}, Skipped: []Notice{}, Diagnostics: []Notice{}}
	conflicted := map[string]bool{}
	for _, change := range changes {
		if change.status == "U" {
			conflicted[change.after_path] = true
		}
	}
	for _, change := range changes {
		if conflicted[change.after_path] && change.status != "U" {
			continue
		}
		if !options.Filter.Allows(change.before_path) && !options.Filter.Allows(change.after_path) {
			result.Skipped = append(result.Skipped, Notice{change.after_path, "filtered", "excluded by scan patterns"})
			continue
		}
		if change.status == "U" {
			result.Diagnostics = append(result.Diagnostics, Notice{change.after_path, "unmerged", "resolve unmerged index entries first"})
			continue
		}
		if notice := git_unsupported(change); notice != nil {
			result.Skipped = append(result.Skipped, *notice)
			_, before_known := lang.Detect(change.before_path)
			_, after_known := lang.Detect(change.after_path)
			if before_known || after_known {
				result.Diagnostics = append(result.Diagnostics, *notice)
			}
			continue
		}
		before, err := read_git_file(root, change.before_path, change.before_mode, change.before_oid, false)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Notice{change.before_path, "read_error", err.Error()})
			continue
		}
		after, err := read_git_file(root, change.after_path, change.after_mode, change.after_oid, options.Mode == "worktree")
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Notice{change.after_path, "read_error", err.Error()})
			continue
		}
		result.Files = append(result.Files, FilePair{Before: before, After: after, Change: change.status})
	}
	sort.Slice(result.Files, func(i, j int) bool { return pair_path(result.Files[i]) < pair_path(result.Files[j]) })
	sort.Slice(result.Skipped, func(i, j int) bool { return result.Skipped[i].Path < result.Skipped[j].Path })
	sort.Slice(result.Diagnostics, func(i, j int) bool { return result.Diagnostics[i].Path < result.Diagnostics[j].Path })
	return result, nil
}

func pair_path(pair FilePair) string {
	if pair.After != nil {
		return pair.After.Path
	}
	return pair.Before.Path
}

func git_unsupported(change git_change) *Notice {
	for _, side := range []struct{ path, mode string }{
		{change.before_path, change.before_mode}, {change.after_path, change.after_mode},
	} {
		if side.mode == "000000" {
			continue
		}
		if side.mode != "100644" && side.mode != "100755" {
			return &Notice{side.path, "nonregular", "Git symlinks and submodules are not source files"}
		}
		if _, known := lang.Detect(side.path); !known {
			return &Notice{side.path, "unsupported_language", "unrecognized source extension"}
		}
	}
	return nil
}

func read_git_file(root, name, mode, oid string, worktree bool) (*File, error) {
	if mode == "000000" {
		return nil, nil
	}
	var content []byte
	var err error
	if worktree {
		filename := filepath.Join(root, filepath.FromSlash(name))
		info, stat_error := os.Lstat(filename)
		if stat_error != nil {
			return nil, stat_error
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("working-tree source is not a regular file")
		}
		content, err = os.ReadFile(filename)
	} else {
		content, err = git_command(root, "cat-file", "blob", oid)
	}
	if err != nil {
		return nil, err
	}
	if err := CheckText(content); err != nil {
		return nil, err
	}
	return &File{Path: name, Content: content}, nil
}

func parse_git_changes(data []byte) ([]git_change, error) {
	changes := []git_change{}
	for len(data) > 0 {
		header, rest, found := bytes.Cut(data, []byte{0})
		if !found {
			return nil, fmt.Errorf("unterminated Git change header")
		}
		fields := strings.Fields(string(header))
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") || len(fields[4]) == 0 {
			return nil, fmt.Errorf("invalid Git change header %q", header)
		}
		change := git_change{before_mode: strings.TrimPrefix(fields[0], ":"), after_mode: fields[1],
			before_oid: fields[2], after_oid: fields[3], status: fields[4][:1]}
		name, remaining, found := bytes.Cut(rest, []byte{0})
		if !found {
			return nil, fmt.Errorf("unterminated Git filename")
		}
		change.before_path, change.after_path = string(name), string(name)
		data = remaining
		if change.status == "R" || change.status == "C" {
			name, data, found = bytes.Cut(data, []byte{0})
			if !found {
				return nil, fmt.Errorf("unterminated Git rename filename")
			}
			change.after_path = string(name)
		}
		for _, name := range []string{change.before_path, change.after_path} {
			if !utf8.ValidString(name) || name == "" || name == "." || filepath.IsAbs(name) ||
				name != filepath.ToSlash(filepath.Clean(name)) || strings.HasPrefix(name, "../") {
				return nil, fmt.Errorf("Git path cannot be represented as a relative UTF-8 source name: %q", name)
			}
		}
		if !strings.Contains("AMDRCTU", change.status) {
			return nil, fmt.Errorf("unsupported Git change status %q", change.status)
		}
		changes = append(changes, change)
	}
	return changes, nil
}

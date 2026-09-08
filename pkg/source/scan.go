package source

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"cmscout/pkg/lang"
)

type File struct {
	Path    string
	Content []byte
}

type Notice struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type FileSet struct {
	Root        string
	Files       []File
	Skipped     []Notice
	Diagnostics []Notice
}

type Filter struct {
	Include []string
	Exclude []string
}

func (filter Filter) Validate() error {
	for _, patterns := range [][]string{filter.Include, filter.Exclude} {
		for _, pattern := range patterns {
			if pattern == "" || strings.HasPrefix(pattern, "/") {
				return fmt.Errorf("scan patterns must be nonempty relative paths: %q", pattern)
			}
			for _, part := range strings.Split(pattern, "/") {
				if _, err := path.Match(part, ""); err != nil {
					return fmt.Errorf("invalid scan pattern %q: %w", pattern, err)
				}
			}
		}
	}
	return nil
}

func match_path(pattern, name string) bool {
	parts, names := strings.Split(pattern, "/"), strings.Split(name, "/")
	reachable := make([]bool, len(names)+1)
	reachable[0] = true
	for _, part := range parts {
		next := make([]bool, len(names)+1)
		for i := 0; i <= len(names); i++ {
			if part == "**" {
				next[i] = reachable[i] || i > 0 && next[i-1]
			} else if i > 0 && reachable[i-1] {
				next[i], _ = path.Match(part, names[i-1])
			}
		}
		reachable = next
	}
	return reachable[len(names)]
}

func (filter Filter) Allows(name string) bool {
	if filter.excludes(name) {
		return false
	}
	if len(filter.Include) == 0 {
		return true
	}
	for _, pattern := range filter.Include {
		if match_path(pattern, name) {
			return true
		}
	}
	return false
}

func (filter Filter) excludes(name string) bool {
	for current := name; ; current = path.Dir(current) {
		for _, pattern := range filter.Exclude {
			if match_path(pattern, current) {
				return true
			}
		}
		if path.Dir(current) == current {
			return false
		}
	}
}

func InferRoot(directory string) string {
	for current := directory; ; current = filepath.Dir(current) {
		for _, marker := range []string{"go.mod", "go.work", "compile_commands.json", ".git"} {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current
			}
		}
		if filepath.Dir(current) == current {
			return directory
		}
	}
}

func Scan(paths []string, root string, filter Filter) (*FileSet, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	inputs := make([]string, 0, len(paths))
	common := ""
	for _, input := range paths {
		if input == "-" {
			return nil, fmt.Errorf("scans require filesystem paths, not stdin")
		}
		absolute, err := filepath.Abs(input)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, absolute)
		directory := absolute
		if !info.IsDir() {
			directory = filepath.Dir(directory)
		}
		if common == "" {
			common = directory
		}
		for directory != common && !strings.HasPrefix(directory, common+string(filepath.Separator)) && filepath.Dir(common) != common {
			common = filepath.Dir(common)
		}
	}
	if root == "" {
		root = InferRoot(common)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("metrics root must be a directory: %s", root)
	}
	for _, input := range inputs {
		if _, err := relative_path(root, input); err != nil {
			return nil, err
		}
	}
	result := &FileSet{Root: root, Files: []File{}, Skipped: []Notice{}, Diagnostics: []Notice{}}
	seen := map[string]bool{}
	sort.Strings(inputs)
	for _, input := range inputs {
		err = filepath.WalkDir(input, func(filename string, entry fs.DirEntry, walk_error error) error {
			name, err := relative_path(root, filename)
			if err != nil {
				return err
			}
			if walk_error != nil {
				result.Diagnostics = append(result.Diagnostics, Notice{name, "read_error", walk_error.Error()})
				return nil
			}
			if seen[name] {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			seen[name] = true
			if strings.Contains("/"+filepath.ToSlash(filename)+"/", "/.git/") {
				result.Skipped = append(result.Skipped, Notice{name, "metadata", "Git metadata"})
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if filter.excludes(name) {
					result.Skipped = append(result.Skipped, Notice{name, "filtered", "directory excluded by scan patterns"})
					return filepath.SkipDir
				}
				return nil
			}
			if !filter.Allows(name) {
				result.Skipped = append(result.Skipped, Notice{name, "filtered", "excluded by scan patterns"})
				return nil
			}
			if !entry.Type().IsRegular() {
				result.Skipped = append(result.Skipped, Notice{name, "nonregular", "symlinks and special files are not followed"})
				return nil
			}
			if _, known := lang.Detect(name); !known {
				result.Skipped = append(result.Skipped, Notice{name, "unsupported_language", "unrecognized source extension"})
				return nil
			}
			content, err := os.ReadFile(filename)
			if err == nil {
				err = CheckText(content)
			}
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, Notice{name, "read_error", err.Error()})
			} else {
				result.Files = append(result.Files, File{name, content})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	sort.Slice(result.Skipped, func(i, j int) bool { return result.Skipped[i].Path < result.Skipped[j].Path })
	sort.Slice(result.Diagnostics, func(i, j int) bool { return result.Diagnostics[i].Path < result.Diagnostics[j].Path })
	return result, nil
}

func relative_path(root, filename string) (string, error) {
	name, err := filepath.Rel(root, filename)
	if err != nil {
		return "", err
	}
	if name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("metrics input %s is outside source root %s", filename, root)
	}
	return filepath.ToSlash(name), nil
}

func CheckText(content []byte) error {
	if bytes.IndexByte(content, 0) >= 0 {
		return fmt.Errorf("binary source contains a NUL byte")
	}
	return nil
}

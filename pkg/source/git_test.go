package source

import (
	"strings"
	"testing"
)

func TestGit_raw_paths(t *testing.T) {
	data := ":100644 100644 old new R100\x00old name\n.js\x00-new\tname.js\x00" +
		":000000 100755 zero new A\x00script.sh\x00"
	changes, err := parse_git_changes([]byte(data))
	if err != nil || len(changes) != 2 || changes[0].before_path != "old name\n.js" ||
		changes[0].after_path != "-new\tname.js" || changes[1].after_mode != "100755" {
		t.Fatalf("raw path parsing failed: %+v %v", changes, err)
	}
	for _, data := range []string{
		"header", ":100644 100644 old new M\x00missing terminator",
		":100644 100644 old new R100\x00a.js\x00",
		":100644 100644 old new M\x00../escape.js\x00",
		":100644 100644 old new M\x00/absolute.js\x00",
		":100644 100644 old new M\x00bad\xff.js\x00",
		strings.Replace(data, "R100", "X", 1),
	} {
		if _, err := parse_git_changes([]byte(data)); err == nil {
			t.Errorf("accepted malformed Git input %q", data)
		}
	}
}

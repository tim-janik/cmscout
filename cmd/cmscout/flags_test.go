// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package main

import "testing"

func TestCompareFlagsResolvePreservesExplicitNames(t *testing.T) {
	cf := compareFlags{oldName: "display-old.ts", newName: "display-new.ts"}
	oldPath, newPath, err := cf.resolve([]string{"old-content.ts", "new-content.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if oldPath != "old-content.ts" || newPath != "new-content.ts" {
		t.Fatalf("content paths = %q, %q; want positional paths", oldPath, newPath)
	}
	if cf.oldName != "display-old.ts" || cf.newName != "display-new.ts" {
		t.Fatalf("display names = %q, %q; positional names overwrote flags", cf.oldName, cf.newName)
	}

	cf.beforeFile = "old-redirect"
	cf.afterFile = "new-redirect"
	oldPath, newPath, err = cf.resolve([]string{"old-content.ts", "new-content.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if oldPath != "old-redirect" || newPath != "new-redirect" {
		t.Fatalf("content paths = %q, %q; want explicit redirections", oldPath, newPath)
	}
}

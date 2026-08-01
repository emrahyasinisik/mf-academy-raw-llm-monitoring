package migrations

import (
	"io/fs"
	"strings"
	"testing"
)

// Migrations apply in filename order and every one of them runs on every boot.
// Two files sharing a number is not a sort error — it is two authors believing
// they own the same slot, which is exactly what happens when a feature branch
// adds 009 while another branch does the same. The collision is invisible until
// the merge, and then the ordering between them is alphabetical accident.
func TestMigrationNumbersAreUnique(t *testing.T) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		num, _, ok := strings.Cut(name, "_")
		if !ok {
			t.Errorf("migration %q has no NNN_ prefix", name)
			continue
		}
		if prev, dup := seen[num]; dup {
			t.Errorf("migrations %q and %q share the number %s", prev, name, num)
		}
		seen[num] = name
	}
	if len(seen) == 0 {
		t.Fatal("no migrations found — the embed pattern is wrong")
	}
}

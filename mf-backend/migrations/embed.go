// Package migrations embeds the SQL schema files into the binary so the server
// can run migrations regardless of its working directory (important on Render).
package migrations

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed *.sql
var files embed.FS

// SQL returns every embedded migration's contents, ordered by filename so
// 001_, 002_ … apply in sequence.
func SQL() ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, n := range names {
		b, err := files.ReadFile(n)
		if err != nil {
			return nil, err
		}
		out = append(out, string(b))
	}
	return out, nil
}

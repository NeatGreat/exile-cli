package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"exileagent/internal/container"
	"exileagent/internal/project"
)

// projectDir returns the absolute project directory: --workdir or the
// current directory.
func projectDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if workdir != "" {
		if dir, err = container.HostPath(workdir, dir); err != nil {
			return "", err
		}
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, nil
}

// describe formats a mount as "PATH" or "PATH -> TARGET".
func describe(m container.Mount) string {
	if m.Target == "" || m.Target == filepath.ToSlash(m.Source) {
		return m.Source
	}
	return m.Source + " -> " + m.Target
}

// row is a line in a table of mounts.
type row struct {
	mode, path, scope string
}

func mountRows(entries []project.Entry) []row {
	rows := make([]row, len(entries))
	for i, e := range entries {
		rows[i] = row{string(e.Mode), describe(e.Mount), e.Scope}
	}
	return rows
}

func printRows(w io.Writer, header row, rows []row) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range append([]row{header}, rows...) {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.mode, r.path, r.scope)
	}
	return tw.Flush()
}

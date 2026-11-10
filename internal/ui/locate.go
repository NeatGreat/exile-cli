package ui

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"exileagent/internal/project"
)

// A dropped item as the browser describes it. Browsers do not reveal where
// a file dropped from Finder lives, so exile looks for a file or directory
// that matches this description.
type dropped struct {
	Name     string   `json:"name"`
	Dir      bool     `json:"dir"`
	Size     int64    `json:"size"`
	Modified int64    `json:"modified"` // milliseconds since the epoch
	Children []string `json:"children"` // names of a directory's entries
}

type located struct {
	Name       string        `json:"name"`
	Dir        bool          `json:"dir"`
	Candidates []browseEntry `json:"candidates"`
}

// maxCandidates limits how many matching paths are offered per item.
const maxCandidates = 5

// locate finds the paths of dropped files and directories below the home
// directory. The user picks the right one before anything is mounted.
func (s *Server) locate(r *http.Request) (any, error) {
	req, err := decode[struct{ Items []dropped }](r)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	results := []located{}
	for _, item := range req.Items {
		res := located{Name: item.Name, Dir: item.Dir, Candidates: []browseEntry{}}
		if item.Name == "" || strings.ContainsRune(item.Name, '/') {
			results = append(results, res)
			continue
		}
		for _, p := range findByName(ctx, home, item.Name) {
			if len(res.Candidates) == maxCandidates {
				break
			}
			if matches(item, p) {
				res.Candidates = append(res.Candidates, browseEntry{Name: item.Name, Path: p, Dir: item.Dir, Sensitive: project.Sensitive(p)})
			}
		}
		results = append(results, res)
	}
	return map[string]any{"items": results}, nil
}

// findByName returns paths below root whose base name is name: from the
// Spotlight index on macOS, otherwise or if that finds nothing by walking
// the directory tree.
func findByName(ctx context.Context, root, name string) []string {
	var paths []string
	if runtime.GOOS == "darwin" {
		out, _ := exec.CommandContext(ctx, "mdfind", "-onlyin", root, "-name", name).Output()
		for p := range strings.Lines(string(out)) {
			if p = strings.TrimSuffix(p, "\n"); filepath.Base(p) == name {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		paths = walkForName(ctx, root, name)
	}
	return paths
}

// skipDirs are not searched when walking: they are huge, or exile never
// mounts them anyway.
var skipDirs = map[string]bool{"node_modules": true, "Library": true, ".git": true, ".cache": true}

// walkForName searches root for name, up to a few levels deep and for a
// limited time.
func walkForName(ctx context.Context, root, name string) []string {
	const maxDepth = 6
	var paths []string
	errStop := errors.New("stop")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return errStop
		}
		if err != nil {
			return nil
		}
		if d.Name() == name && p != root {
			paths = append(paths, p)
		}
		if d.IsDir() && p != root {
			depth := strings.Count(strings.TrimPrefix(p, root), string(filepath.Separator))
			if depth >= maxDepth || skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
		}
		return nil
	})
	return paths
}

// matches reports whether the file or directory at p is the dropped item:
// files must have the same size and modification time, directories must
// contain the same entries.
func matches(item dropped, p string) bool {
	fi, err := os.Lstat(p)
	if err != nil || fi.IsDir() != item.Dir {
		return false
	}
	if !item.Dir {
		diff := fi.ModTime().UnixMilli() - item.Modified
		return fi.Size() == item.Size && (item.Modified == 0 || diff > -2000 && diff < 2000)
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, c := range item.Children {
		if !names[c] {
			return false
		}
	}
	return len(item.Children) > 0 || len(entries) == 0
}

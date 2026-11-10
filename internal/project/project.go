// Package project computes what an agent started in a project directory can
// access: the configured mounts and hosts plus the defaults and safety rules
// exile applies on top.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"exileagent/internal/config"
	"exileagent/internal/container"
)

// Where an entry comes from.
const (
	ScopeDefault   = "default"   // the project directory itself
	ScopeProtected = "protected" // read-only because the host executes it
	ScopeGlobal    = "global"
	ScopeProject   = "project"
)

// ProtectedNames are files and directories in a project that tools on the
// host execute or trust: git hooks and config, direnv, IDE run
// configurations, agent settings with hooks, and git hook managers. Inside
// writable mounts they are mounted read-only unless configured explicitly.
var ProtectedNames = []string{
	".git", ".envrc", ".idea", ".vscode", ".claude", ".mcp.json", ".codex",
	".cline", ".clinerules", ".husky", ".pre-commit-config.yaml", "lefthook.yml",
}

// SensitivePaths hold credentials or control the host. Paths are relative
// to the home directory unless absolute. exile refuses to make them visible
// to the agent, even read-only.
var SensitivePaths = []string{
	".ssh", ".gnupg", ".aws", ".azure", ".config/gcloud", ".kube", ".docker",
	".colima", ".lima", ".orbstack", ".netrc", ".npmrc", ".pypirc",
	".git-credentials", ".config/gh", ".password-store", ".claude", ".claude.json",
	".codex", ".cline", "Library", "/var/run/docker.sock",
}

// Entry is an effective mount and where it comes from.
type Entry struct {
	container.Mount
	Scope string
}

// Mounts returns the effective mounts for the project in dir: the directory
// itself (read-write), then the enabled global and project mounts, then the
// protected paths. A later mount with the same source replaces an earlier
// one, which is how "exile add . --ro" makes the project read-only.
func Mounts(cfg *config.Config, dir string) []Entry {
	var entries []Entry
	add := func(scope string, m container.Mount) {
		for i := range entries {
			if entries[i].Source == m.Source {
				entries[i] = Entry{m, scope}
				return
			}
		}
		entries = append(entries, Entry{m, scope})
	}
	add(ScopeDefault, container.Mount{Source: dir, Mode: container.ReadWrite})
	for _, e := range cfg.Global.Mounts {
		if !e.Disabled {
			add(ScopeGlobal, e.Mount)
		}
	}
	for _, e := range cfg.Project(dir).Mounts {
		if !e.Disabled {
			add(ScopeProject, e.Mount)
		}
	}

	configured := sources(entries)
	for _, e := range entries[:len(entries):len(entries)] {
		for _, p := range protectedIn(e) {
			if fi, err := os.Lstat(p.Source); err == nil && fi.Mode()&fs.ModeSymlink == 0 && !configured[p.Source] {
				entries = append(entries, Entry{p, ScopeProtected})
			}
		}
	}
	return entries
}

// Unprotected returns the protected paths that do not exist yet inside
// writable mounts. The agent could create them; exile warns if it does.
func Unprotected(entries []Entry) []string {
	configured := sources(entries)
	var paths []string
	for _, e := range entries {
		for _, p := range protectedIn(e) {
			if _, err := os.Lstat(p.Source); errors.Is(err, fs.ErrNotExist) && !configured[p.Source] {
				paths = append(paths, p.Source)
			}
		}
	}
	return paths
}

// protectedIn returns read-only mounts for the protected paths inside a
// writable directory mount.
func protectedIn(e Entry) []container.Mount {
	if e.Mode != container.ReadWrite {
		return nil
	}
	if fi, err := os.Stat(e.Source); err != nil || !fi.IsDir() {
		return nil
	}
	var target string
	if e.Target != "" {
		target = e.Target
	} else {
		target = filepath.ToSlash(e.Source)
	}
	mounts := make([]container.Mount, len(ProtectedNames))
	for i, name := range ProtectedNames {
		mounts[i] = container.Mount{Source: filepath.Join(e.Source, name), Target: path.Join(target, name), Mode: container.ReadOnly}
	}
	return mounts
}

func sources(entries []Entry) map[string]bool {
	m := map[string]bool{}
	for _, e := range entries {
		m[e.Source] = true
	}
	return m
}

// Resolve fills in the container paths of entries and enforces the safety
// rules: no credentials, no sockets and no exile configuration.
func Resolve(entries []Entry) ([]Entry, error) {
	mounts := make([]container.Mount, len(entries))
	for i, e := range entries {
		mounts[i] = e.Mount
	}
	resolved, err := container.Resolve(mounts)
	if err != nil {
		return nil, err
	}
	if err := checkSensitive(resolved); err != nil {
		return nil, err
	}
	out := make([]Entry, len(entries))
	for i, m := range resolved {
		out[i] = Entry{m, entries[i].Scope}
	}
	return out, nil
}

// sensitivePaths returns SensitivePaths as absolute paths, plus the exile
// configuration directory: it holds the agents' credentials, and an agent
// that could write to it could widen its own access.
func sensitivePaths() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfgPath, err := config.Path()
	if err != nil {
		return nil, err
	}
	paths := []string{filepath.Dir(cfgPath)}
	for _, p := range SensitivePaths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(home, p)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func checkSensitive(mounts []container.Mount) error {
	sensitive, err := sensitivePaths()
	if err != nil {
		return err
	}
	hidden := map[string]bool{}
	for _, m := range mounts {
		if m.Mode == container.Hidden {
			hidden[m.Source] = true
		}
	}
	for _, m := range mounts {
		if m.Mode == container.Hidden {
			continue
		}
		if fi, err := os.Stat(m.Source); err == nil && fi.Mode()&fs.ModeSocket != 0 {
			return fmt.Errorf("%s is a socket; exile never passes sockets to the agent", m.Source)
		}
		for _, p := range sensitive {
			if container.Contains(p, m.Source) {
				return fmt.Errorf("%s holds credentials or controls the host; exile never mounts it", m.Source)
			}
			if _, err := os.Lstat(p); err == nil && container.Contains(m.Source, p) && !hidden[p] {
				return fmt.Errorf("mounting %s would expose %s; hide it with \"exile add --hide %s\" or mount a narrower directory", m.Source, p, p)
			}
		}
	}
	return nil
}

// Sensitive reports whether p is or lies inside a path exile never mounts.
func Sensitive(p string) bool {
	sensitive, err := sensitivePaths()
	if err != nil {
		return false
	}
	for _, s := range sensitive {
		if container.Contains(s, p) {
			return true
		}
	}
	return false
}

// Validate checks that the project in dir can start with cfg.
func Validate(cfg *config.Config, dir string) error {
	for _, e := range cfg.Global.Mounts {
		if e.Mode == container.Hidden {
			return fmt.Errorf("%s: hidden paths can only be set per project", e.Source)
		}
	}
	_, err := Resolve(Mounts(cfg, dir))
	return err
}

// Host is an allowed host and where it comes from: an agent name, global or
// project.
type Host struct {
	Name  string
	Scope string
}

// Hosts returns the hosts the given agents may reach when started in dir.
func Hosts(cfg *config.Config, dir string, agents ...string) []Host {
	var hosts []Host
	seen := map[string]bool{}
	add := func(scope string, names []string) {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				hosts = append(hosts, Host{n, scope})
			}
		}
	}
	for _, a := range agents {
		add(a, container.Agents[a].Hosts)
	}
	add(ScopeGlobal, cfg.Global.Allow)
	add(ScopeProject, cfg.Project(dir).Allow)
	return hosts
}

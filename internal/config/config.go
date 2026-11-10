// Package config stores which host paths are mounted into the agent
// container and which hosts it may reach. The file lives outside every
// project, so an agent cannot widen its own access by editing it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"exileagent/internal/container"
)

// Entry is a configured mount. Disabled entries are kept but not mounted.
type Entry struct {
	container.Mount
	Disabled bool `json:"disabled,omitempty"`
}

// Scope is the access granted to one project, or to all of them.
type Scope struct {
	Mounts []Entry  `json:"mounts,omitempty"`
	Allow  []string `json:"allow,omitempty"`
}

// Add adds e, replacing an entry with the same source. It reports whether an
// existing entry was replaced.
func (s *Scope) Add(e Entry) bool {
	for i := range s.Mounts {
		if s.Mounts[i].Source == e.Source {
			s.Mounts[i] = e
			return true
		}
	}
	s.Mounts = append(s.Mounts, e)
	return false
}

// Find returns the entry for source, or nil.
func (s *Scope) Find(source string) *Entry {
	for i := range s.Mounts {
		if s.Mounts[i].Source == source {
			return &s.Mounts[i]
		}
	}
	return nil
}

// Remove removes the entry for source and reports whether there was one.
func (s *Scope) Remove(source string) bool {
	n := len(s.Mounts)
	s.Mounts = slices.DeleteFunc(s.Mounts, func(e Entry) bool { return e.Source == source })
	return len(s.Mounts) != n
}

// AllowHost adds host to the allowlist and reports whether it was new.
func (s *Scope) AllowHost(host string) bool {
	if slices.Contains(s.Allow, host) {
		return false
	}
	s.Allow = append(s.Allow, host)
	return true
}

// DisallowHost removes host from the allowlist and reports whether it was
// there.
func (s *Scope) DisallowHost(host string) bool {
	n := len(s.Allow)
	s.Allow = slices.DeleteFunc(s.Allow, func(h string) bool { return h == host })
	return len(s.Allow) != n
}

// Config holds the global scope and those of each project, keyed by the
// project's absolute path.
type Config struct {
	Global   Scope             `json:"global"`
	Projects map[string]*Scope `json:"projects,omitempty"`
}

// Project returns the scope of the project in dir, creating it if needed.
func (c *Config) Project(dir string) *Scope {
	if c.Projects == nil {
		c.Projects = map[string]*Scope{}
	}
	if c.Projects[dir] == nil {
		c.Projects[dir] = &Scope{}
	}
	return c.Projects[dir]
}

// Path returns the config file location; EXILE_CONFIG overrides it.
func Path() (string, error) {
	if p := os.Getenv("EXILE_CONFIG"); p != "" {
		return filepath.Abs(p)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "exile", "config.json"), nil
}

// Load reads the config file. A missing file yields an empty config.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &c, nil
}

// Save writes the config file, dropping empty projects.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	for dir, s := range c.Projects {
		if len(s.Mounts) == 0 && len(s.Allow) == 0 {
			delete(c.Projects, dir)
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

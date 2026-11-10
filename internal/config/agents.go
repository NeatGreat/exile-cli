package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"exileagent/internal/container"
)

// SectionAll holds the variables set for every agent.
const SectionAll = "all"

// Var is an environment variable for an agent.
type Var struct {
	Name, Value string
}

// AgentConfig maps sections, SectionAll or an agent name, to the variables
// configured in them.
type AgentConfig map[string][]Var

// Env returns the variables for agent: those of SectionAll, then those of
// the agent's own section, which win on conflicts.
func (c AgentConfig) Env(agent string) []Var {
	var vars []Var
	for _, v := range append(slices.Clone(c[SectionAll]), c[agent]...) {
		vars = slices.DeleteFunc(vars, func(o Var) bool { return o.Name == v.Name })
		vars = append(vars, v)
	}
	return vars
}

// AgentsPath returns the location of the agent configuration, next to the
// config file.
func AgentsPath() (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "agents.conf"), nil
}

// LoadAgents reads the agent configuration. A missing file yields an empty
// configuration.
func LoadAgents() (AgentConfig, error) {
	p, err := AgentsPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return AgentConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c, err := ParseAgents(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// ParseAgents parses an agent configuration: KEY=VALUE lines below [section]
// headers, with # comments. Lines before the first header belong to
// SectionAll.
func ParseAgents(r io.Reader) (AgentConfig, error) {
	c := AgentConfig{}
	section := SectionAll
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
			continue
		case line[0] == '[':
			name, ok := strings.CutSuffix(line[1:], "]")
			name = strings.ToLower(strings.TrimSpace(name))
			if !ok || name != SectionAll && container.Agents[name].Command == nil {
				return nil, fmt.Errorf("line %d: unknown section %s; want [%s] or one of the agents %s",
					n, line, SectionAll, strings.Join(container.AgentNames(), ", "))
			}
			section = name
			continue
		}
		name, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		name = strings.TrimSpace(name)
		if !ok || !validName(name) {
			return nil, fmt.Errorf("line %d: want NAME=VALUE", n)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		c[section] = append(c[section], Var{name, value})
	}
	return c, sc.Err()
}

func validName(s string) bool {
	for i, r := range s {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return s != ""
}

// InitAgents creates the agent configuration from a template unless it
// exists. It returns the path and whether the file was created.
func InitAgents() (string, bool, error) {
	p, err := AgentsPath()
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", false, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return p, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if _, err := io.WriteString(f, agentsTemplate); err != nil {
		f.Close()
		return "", false, err
	}
	return p, true, f.Close()
}

const agentsTemplate = `# Exile agent configuration
#
# Environment variables for the coding agents. Variables in [all] are set for
# every agent, those in [claude], [codex], [cline] or [shell] for that agent
# only.
# At start, exile copies the agent's variables into its container; other
# agents never see them, and they do not show up in "docker inspect".
#
# Keep this file private: it holds your credentials.

[all]
# GIT_AUTHOR_NAME=Your Name
# GIT_AUTHOR_EMAIL=you@example.com
# GIT_COMMITTER_NAME=Your Name
# GIT_COMMITTER_EMAIL=you@example.com

[claude]
# Claude subscription: create a long-lived token on your machine with
#   claude setup-token
# CLAUDE_CODE_OAUTH_TOKEN=
#
# Or an API key from console.anthropic.com:
# ANTHROPIC_API_KEY=

[codex]
# OpenAI API key; exile logs Codex in with it at start.
# OPENAI_API_KEY=

[cline]
# A provider's API key, model and provider ID; exile sets Cline up with them
# at start. Without a key, sign in with your Cline account inside Cline.
# For a provider other than cline, allow its API host, for example with
# "exile allow api.anthropic.com".
# CLINE_API_KEY=
# CLINE_MODEL=claude-sonnet-5-5
# CLINE_PROVIDER=anthropic
`

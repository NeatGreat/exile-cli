package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseAgents(t *testing.T) {
	c, err := ParseAgents(strings.NewReader(`
TOP=1
# comment
[all]
GIT_AUTHOR_NAME = "Jane Doe"
SHARED=all
[Claude]
export CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-abc=
SHARED='claude'
[codex]
; comment
OPENAI_API_KEY=sk-1
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Var{{"TOP", "1"}, {"GIT_AUTHOR_NAME", "Jane Doe"}, {"CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-abc="}, {"SHARED", "claude"}}
	if got := c.Env("claude"); !slices.Equal(got, want) {
		t.Errorf("Env(claude) = %q, want %q", got, want)
	}
	want = []Var{{"TOP", "1"}, {"GIT_AUTHOR_NAME", "Jane Doe"}, {"SHARED", "all"}, {"OPENAI_API_KEY", "sk-1"}}
	if got := c.Env("codex"); !slices.Equal(got, want) {
		t.Errorf("Env(codex) = %q, want %q", got, want)
	}

	for _, bad := range []string{"[cluade]\nA=1", "[all\nA=1", "NO_EQUALS", "1A=x", "A B=x", "=x"} {
		if _, err := ParseAgents(strings.NewReader(bad)); err == nil {
			t.Errorf("ParseAgents(%q) succeeded", bad)
		}
	}
}

func TestInitAgents(t *testing.T) {
	t.Setenv("EXILE_CONFIG", filepath.Join(t.TempDir(), "exile", "config.json"))
	p, created, err := InitAgents()
	if err != nil || !created {
		t.Fatalf("InitAgents = %q, %v, %v", p, created, err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("agents.conf: %v, %v", fi, err)
	}
	if _, created, err := InitAgents(); err != nil || created {
		t.Errorf("second InitAgents created = %v, %v", created, err)
	}
	c, err := LoadAgents()
	if err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if len(c.Env("claude")) != 0 {
		t.Errorf("template sets variables: %q", c.Env("claude"))
	}
}

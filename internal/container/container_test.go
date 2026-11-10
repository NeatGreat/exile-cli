package container

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testSpec(t *testing.T) Spec {
	project, _ := tree(t)
	return Spec{
		Name:       "exile-test",
		Image:      DefaultImage,
		Agent:      "claude",
		Command:    []string{"claude", "--continue"},
		Workdir:    project,
		Mounts:     []Mount{{Source: project, Mode: ReadWrite}},
		HomeVolume: "exile-home-test",
		Env:        []string{"FOO=bar"},
		Network:    NetworkFiltered,
		Allow:      []string{"api.anthropic.com", "*.github.com"},
		Memory:     "1536m",
		CPUs:       "1.50",
		PidsLimit:  1024,
		UID:        501,
		GID:        20,
	}
}

// find returns the command starting with prefix, joined by spaces.
func find(t *testing.T, cmds []Step, prefix ...string) string {
	t.Helper()
	for _, c := range cmds {
		if len(c.Args) >= len(prefix) && slices.Equal(c.Args[:len(prefix)], prefix) {
			return strings.Join(c.Args, " ")
		}
	}
	t.Fatalf("no command %q in %q", prefix, cmds)
	return ""
}

func TestCommandsFiltered(t *testing.T) {
	spec := testSpec(t)
	cmds, err := spec.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if last := strings.Join(cmds[len(cmds)-1].Args, " "); last != "start --attach --interactive exile-test" {
		t.Errorf("last command = %q", last)
	}

	agent := find(t, cmds, "create", "--interactive")
	for _, want := range []string{
		"--name exile-test ",
		"--workdir " + spec.Workdir,
		"--mount type=volume,source=exile-home-test,target=/home/agent",
		"--mount type=bind,source=" + spec.Workdir + ",target=" + spec.Workdir,
		"--env EXILE_UID=501 --env EXILE_GID=20",
		"--env FOO=bar",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--memory 1536m --memory-swap 1536m --cpus 1.50 --pids-limit 1024",
		"--network exile-test-net",
		"--env HTTPS_PROXY=http://proxy:3128",
		DefaultImage + " claude --continue",
	} {
		if !strings.Contains(agent, want) {
			t.Errorf("agent command missing %q:\n%s", want, agent)
		}
	}
	if strings.Contains(agent, "--tty") {
		t.Error("--tty without a terminal")
	}

	find(t, cmds, "network", "create", "--internal")
	find(t, cmds, "network", "connect", "--alias", "proxy", "exile-test-net", "exile-test-proxy")
	proxy := find(t, cmds, "create", "--name", "exile-test-proxy")
	for _, want := range []string{"--read-only", "--cap-drop ALL", "EXILE_ALLOW=api.anthropic.com,*.github.com"} {
		if !strings.Contains(proxy, want) {
			t.Errorf("proxy command missing %q:\n%s", want, proxy)
		}
	}
}

func TestCommandsSessionFiles(t *testing.T) {
	spec := testSpec(t)
	spec.AgentEnv = []string{"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-x", "QUOTED=it's"}
	root := filepath.Dir(spec.Workdir)
	notes := filepath.Join(root, "notes.md")
	if err := os.WriteFile(notes, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	spec.Mounts = append(spec.Mounts,
		Mount{Source: filepath.Join(root, "lib"), Mode: ReadOnly},
		Mount{Source: notes, Mode: ReadWrite},
		Mount{Source: filepath.Join(spec.Workdir, "secrets"), Mode: ReadOnly},
	)
	cmds, err := spec.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if agent := find(t, cmds, "create", "--interactive"); strings.Contains(agent, "sk-ant") {
		t.Errorf("secret on the command line:\n%s", agent)
	}
	var cp Step
	for _, c := range cmds {
		if c.Args[0] == "cp" {
			cp = c
		}
	}
	if strings.Join(cp.Args, " ") != "cp - exile-test:/run/exile" {
		t.Fatalf("copy step = %q", cp.Args)
	}
	files := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(cp.Input))
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		if h.Mode != 0o400 {
			t.Errorf("%s has mode %o", h.Name, h.Mode)
		}
		data, _ := io.ReadAll(tr)
		files[h.Name] = string(data)
	}
	want := "export CLAUDE_CODE_OAUTH_TOKEN='sk-ant-oat01-x'\nexport QUOTED='it'\\''s'\n"
	if files["agent.env"] != want {
		t.Errorf("agent.env = %q, want %q", files["agent.env"], want)
	}
	var settings struct {
		StatusLine  struct{ Command string }
		Permissions struct{ AdditionalDirectories []string }
	}
	if err := json.Unmarshal([]byte(files["claude-settings.json"]), &settings); err != nil {
		t.Fatalf("claude-settings.json: %v", err)
	}
	// The library and the directory of the notes file, but nothing inside
	// the project, which is the working directory already.
	if dirs := settings.Permissions.AdditionalDirectories; !slices.Equal(dirs, []string{filepath.Join(root, "lib"), root}) {
		t.Errorf("additionalDirectories = %q", dirs)
	}
	if settings.StatusLine.Command != "/usr/local/bin/exile-statusline" {
		t.Errorf("status line = %q", settings.StatusLine.Command)
	}
	for _, line := range []string{
		"- " + spec.Workdir + " (the project directory, your working directory, read and write)",
		"- /home/agent (your home directory)",
		"Network: only HTTPS to api.anthropic.com, *.github.com.",
	} {
		if !strings.Contains(files["access.md"], line) {
			t.Errorf("access.md lacks %q:\n%s", line, files["access.md"])
		}
	}
}

func TestCommandsOtherNetworks(t *testing.T) {
	spec := testSpec(t)
	spec.Network = NetworkNone
	cmds, err := spec.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if agent := find(t, cmds, "create", "--interactive"); !strings.Contains(agent, "--network none") || strings.Contains(agent, "PROXY") {
		t.Errorf("agent command with no network:\n%s", agent)
	}
	if len(cmds) != 4 {
		t.Errorf("got %d commands, want volume create, create, cp, start", len(cmds))
	}

	spec.Network = "bogus"
	if _, err := spec.Commands(); err == nil {
		t.Error("unknown network mode accepted")
	}
}

func TestCommandsErrors(t *testing.T) {
	spec := testSpec(t)
	spec.Mounts[0].Mode = Hidden
	if _, err := spec.Commands(); err == nil {
		t.Error("hidden working directory accepted")
	}

	spec = testSpec(t)
	spec.Mounts = append(spec.Mounts, Mount{Source: spec.Workdir + "/secrets", Target: Home, Mode: ReadOnly})
	if _, err := spec.Commands(); err == nil {
		t.Error("mount over the home volume accepted")
	}

	spec = testSpec(t)
	spec.Env = []string{"=x"}
	if _, err := spec.Commands(); err == nil {
		t.Error("invalid environment variable accepted")
	}
}

func TestNames(t *testing.T) {
	name := NewName("/Users/me/My Project!")
	if !strings.HasPrefix(name, "exile-my-project-") || len(name) != len("exile-my-project-")+4 {
		t.Errorf("NewName = %q", name)
	}
	if name := NewName("/"); !strings.HasPrefix(name, "exile-root-") {
		t.Errorf("NewName(/) = %q", name)
	}
	a, b := HomeVolumeName("/a/proj"), HomeVolumeName("/b/proj")
	if a == b || !strings.HasPrefix(a, "exile-home-proj-") || a != HomeVolumeName("/a/proj") {
		t.Errorf("HomeVolumeName: %q, %q", a, b)
	}
}

func TestNormalizeHost(t *testing.T) {
	for in, want := range map[string]string{
		"API.Anthropic.com":   "api.anthropic.com",
		"example.com.":        "example.com",
		"*.github.com":        "*.github.com",
		" registry.npmjs.org": "registry.npmjs.org",
	} {
		if got, err := NormalizeHost(in); err != nil || got != want {
			t.Errorf("NormalizeHost(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"localhost", "*", "*.com", "exa mple.com", "-a.com", "a..com", "https://x.com", "1.2.3.4:443"} {
		if _, err := NormalizeHost(bad); err == nil {
			t.Errorf("NormalizeHost(%q) succeeded", bad)
		}
	}
}

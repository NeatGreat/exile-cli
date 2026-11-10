// Package container runs coding agents in Docker containers that only see
// explicitly mounted host paths and reach the network through an
// allowlisting proxy.
package container

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	DefaultImage = "exile-agent:latest"
	DefaultAgent = "claude"

	// Home is the agent user's home directory inside the container.
	Home = "/home/agent"

	labelManaged = "exile.managed"
	labelRole    = "exile.role"
	labelSession = "exile.session"
	labelAgent   = "exile.agent"
	labelWorkdir = "exile.workdir"
)

// Agent is a coding agent that can run in the container.
type Agent struct {
	Command []string
	// Hosts are the hosts the agent needs for its API and login. They are
	// always reachable while the agent runs.
	Hosts []string
	// Resume reports whether the agent can continue its last session in the
	// project through exile-resume.
	Resume bool
}

// Agents maps agent names to their definitions.
var Agents = map[string]Agent{
	"claude": {
		Command: []string{"claude"},
		Hosts:   []string{"api.anthropic.com", "console.anthropic.com", "platform.claude.com", "claude.ai"},
		Resume:  true,
	},
	"codex": {
		Command: []string{"codex"},
		Hosts:   []string{"api.openai.com", "auth.openai.com", "chatgpt.com"},
		Resume:  true,
	},
	"cline": {
		Command: []string{"cline"},
		// Cline's own API, WorkOS for signing in with a Cline account and
		// the model catalog.
		Hosts:  []string{"api.cline.bot", "api.workos.com", "models.dev"},
		Resume: true,
	},
	"shell": {Command: []string{"bash"}},
}

// AgentNames returns the known agent names, sorted.
func AgentNames() []string {
	names := make([]string, 0, len(Agents))
	for name := range Agents {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Spec describes an agent session.
type Spec struct {
	Name    string
	Image   string
	Agent   string
	Command []string

	// Workdir is the host directory the agent starts in. It must be the
	// source of one of Mounts.
	Workdir string
	// Mounts is everything the agent can see of the host.
	Mounts []Mount
	// HomeVolume is the volume mounted at Home; empty means a throwaway home.
	HomeVolume string
	// Env holds NAME=VALUE pairs, or NAME to take the value from the host.
	Env []string
	// AgentEnv holds NAME=VALUE pairs from the agent configuration. They are
	// copied into the container rather than passed as options, so they do
	// not show up in "docker inspect".
	AgentEnv []string

	Network NetworkMode
	// Allow lists the hosts reachable through the proxy with NetworkFiltered.
	Allow []string

	// Resource limits in docker's notation; empty or zero means no limit.
	Memory    string
	CPUs      string
	PidsLimit int

	TTY      bool
	UID, GID int
}

func proxyName(session string) string   { return session + "-proxy" }
func networkName(session string) string { return session + "-net" }

// Step is a docker command. Input, if any, is fed to its standard input.
type Step struct {
	Args  []string
	Input []byte
}

// Commands returns the docker commands that set up the session, in order.
// The last one attaches the terminal to the agent.
func (s Spec) Commands() ([]Step, error) {
	mounts, err := Resolve(s.Mounts)
	if err != nil {
		return nil, err
	}
	workdir := ""
	for _, m := range mounts {
		if m.Source == s.Workdir && m.Mode != Hidden {
			workdir = m.Target
		}
		if s.HomeVolume != "" && m.Target == Home {
			return nil, fmt.Errorf("%s would replace the agent's home volume", m.Source)
		}
	}
	if workdir == "" {
		return nil, fmt.Errorf("working directory %s is not mounted", s.Workdir)
	}
	for _, e := range append(slices.Clone(s.Env), s.AgentEnv...) {
		if name, _, _ := strings.Cut(e, "="); name == "" {
			return nil, fmt.Errorf("invalid environment variable %q", e)
		}
	}

	var cmds []Step
	if s.HomeVolume != "" {
		cmds = append(cmds, Step{Args: []string{"volume", "create",
			"--label", labelManaged + "=true", "--label", labelWorkdir + "=" + s.Workdir, s.HomeVolume}})
	}

	agent := []string{"create", "--interactive"}
	if s.TTY {
		agent = append(agent, "--tty")
	}
	agent = append(agent,
		"--name", s.Name,
		"--hostname", "exile",
		"--label", labelManaged+"=true",
		"--label", labelRole+"=agent",
		"--label", labelSession+"="+s.Name,
		"--label", labelAgent+"="+s.Agent,
		"--label", labelWorkdir+"="+s.Workdir,
		// The entrypoint needs these to remap the agent user and the init
		// process to forward signals to it. The agent itself runs
		// unprivileged and cannot regain them.
		"--cap-drop", "ALL",
		"--cap-add", "CHOWN",
		"--cap-add", "DAC_OVERRIDE",
		"--cap-add", "FOWNER",
		"--cap-add", "KILL",
		"--cap-add", "SETUID",
		"--cap-add", "SETGID",
		"--security-opt", "no-new-privileges",
	)
	if s.Memory != "" {
		agent = append(agent, "--memory", s.Memory, "--memory-swap", s.Memory)
	}
	if s.CPUs != "" {
		agent = append(agent, "--cpus", s.CPUs)
	}
	if s.PidsLimit > 0 {
		agent = append(agent, "--pids-limit", strconv.Itoa(s.PidsLimit))
	}

	switch s.Network {
	case NetworkFiltered:
		cmds = append(cmds, Step{Args: []string{"network", "create", "--internal",
			"--label", labelManaged + "=true", "--label", labelSession + "=" + s.Name, networkName(s.Name)}})
		agent = append(agent, "--network", networkName(s.Name))
		for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
			agent = append(agent, "--env", k+"="+proxyURL)
		}
		agent = append(agent,
			"--env", "NO_PROXY=localhost,127.0.0.1",
			"--env", "no_proxy=localhost,127.0.0.1",
			"--env", "NODE_USE_ENV_PROXY=1",
		)
	case NetworkNone:
		agent = append(agent, "--network", "none")
	case NetworkOpen:
	default:
		return nil, fmt.Errorf("unknown network mode %q", s.Network)
	}

	agent = append(agent,
		"--env", "EXILE_UID="+strconv.Itoa(s.UID),
		"--env", "EXILE_GID="+strconv.Itoa(s.GID),
		"--env", "TERM=xterm-256color",
		"--env", "COLORTERM",
		"--env", "EXILE_NETWORK="+string(s.Network),
		"--workdir", workdir,
	)
	if s.HomeVolume != "" {
		agent = append(agent, "--mount", mountOpt("type=volume", "source="+s.HomeVolume, "target="+Home))
	}
	for _, m := range mounts {
		agent = append(agent, "--mount", m.opt())
	}
	for _, e := range s.Env {
		agent = append(agent, "--env", e)
	}
	agent = append(agent, s.Image)
	cmds = append(cmds, Step{Args: append(agent, s.Command...)})
	settings, err := claudeSettings(mounts, workdir)
	if err != nil {
		return nil, err
	}
	input, err := sessionFiles(s.AgentEnv, s.accessNotes(mounts, workdir), settings)
	if err != nil {
		return nil, err
	}
	cmds = append(cmds, Step{Args: []string{"cp", "-", s.Name + ":" + sessionDir}, Input: input})

	if s.Network == NetworkFiltered {
		cmds = append(cmds, s.proxySteps()...)
	}
	return append(cmds, Step{Args: []string{"start", "--attach", "--interactive", s.Name}}), nil
}

// proxySteps returns the docker commands that start the session's proxy and
// connect it to the session's network.
func (s Spec) proxySteps() []Step {
	return []Step{
		{Args: []string{"create",
			"--name", proxyName(s.Name),
			"--label", labelManaged + "=true",
			"--label", labelRole + "=proxy",
			"--label", labelSession + "=" + s.Name,
			"--cap-drop", "ALL",
			"--security-opt", "no-new-privileges",
			"--read-only",
			"--user", "65534:65534",
			"--memory", "64m",
			"--pids-limit", "64",
			"--env", "EXILE_ALLOW=" + strings.Join(s.Allow, ","),
			"--entrypoint", "/usr/local/bin/exile-proxy",
			s.Image,
		}},
		{Args: []string{"network", "connect", "--alias", proxyAlias, networkName(s.Name), proxyName(s.Name)}},
		{Args: []string{"start", proxyName(s.Name)}},
	}
}

// sessionDir is where the entrypoint picks up the files exile prepares for
// the session.
const sessionDir = "/run/exile"

// sessionFiles returns a tar archive for sessionDir: agent.env, a shell file
// that exports vars, if there are any; access.md, which tells the agent what
// it can access; and claude-settings.json, Claude Code's managed settings.
// Only root can read them; the entrypoint puts them in place and deletes
// them.
func sessionFiles(vars []string, notes, settings string) ([]byte, error) {
	var env strings.Builder
	for _, v := range vars {
		name, value, _ := strings.Cut(v, "=")
		fmt.Fprintf(&env, "export %s='%s'\n", name, strings.ReplaceAll(value, "'", `'\''`))
	}
	files := [][2]string{{"access.md", notes}, {"claude-settings.json", settings}}
	if len(vars) > 0 {
		files = append(files, [2]string{"agent.env", env.String()})
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f[0], Mode: 0o400, Size: int64(len(f[1]))}); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(tw, f[1]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// claudeSettings returns Claude Code's managed settings for the session: the
// exile status line, and the granted paths outside the working directory as
// additional working directories, so that Claude Code does not ask before
// reading them. For a granted file that is its directory, which in the
// container holds nothing but granted paths.
func claudeSettings(mounts []Mount, workdir string) (string, error) {
	var dirs []string
	for _, m := range mounts {
		if m.Mode == Hidden || m.Target == workdir || strings.HasPrefix(m.Target, workdir+"/") {
			continue
		}
		dir := m.Target
		if !m.dir {
			dir = path.Dir(dir)
		}
		if dir != "/" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	settings := map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "/usr/local/bin/exile-statusline", "padding": 0},
	}
	if len(dirs) > 0 {
		settings["permissions"] = map[string]any{"additionalDirectories": dirs}
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	return string(b) + "\n", err
}

// accessNotes tells the agent what it can access, so that it finds paths
// outside the project and knows why everything else is missing.
func (s Spec) accessNotes(mounts []Mount, workdir string) string {
	var b strings.Builder
	b.WriteString(`# Exile

You run inside exile, an isolated container on the user's machine. You can
only access the paths below, at the same locations as on the user's
machine. Everything else on the machine is out of reach. If you need
access to more, ask the user to grant it with exile.

`)
	for _, m := range mounts {
		access := map[Mode]string{ReadWrite: "read and write", ReadOnly: "read-only", Hidden: "hidden from you, appears empty"}[m.Mode]
		if m.Target == workdir {
			access = "the project directory, your working directory, " + access
		}
		fmt.Fprintf(&b, "- %s (%s)\n", m.Target, access)
	}
	fmt.Fprintf(&b, "- %s (your home directory)\n\n", Home)
	switch {
	case s.Network == NetworkFiltered && len(s.Allow) > 0:
		fmt.Fprintf(&b, "Network: only HTTPS to %s.\n", strings.Join(s.Allow, ", "))
	case s.Network != NetworkOpen:
		b.WriteString("Network: none, all connections are blocked.\n")
	}
	return b.String()
}

// slug turns the base name of dir into a docker name component.
func slug(dir string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, filepath.Base(dir))
	if s = strings.Trim(s, ".-_"); s == "" {
		return "root"
	}
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// NewName returns a session name derived from the working directory.
func NewName(workdir string) string {
	return fmt.Sprintf("exile-%s-%s", slug(workdir), strings.ToLower(rand.Text()[:4]))
}

// HomeVolumeName returns the name of the project's home volume. Each project
// gets its own, so agents cannot see each other's history or settings.
func HomeVolumeName(workdir string) string {
	sum := sha256.Sum256([]byte(workdir))
	return fmt.Sprintf("exile-home-%s-%s", slug(workdir), hex.EncodeToString(sum[:4]))
}

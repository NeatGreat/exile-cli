package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"exileagent/internal/config"
	"exileagent/internal/container"
	"exileagent/internal/project"
)

// liveSession runs an agent session and applies changed settings while it
// runs: network changes replace the proxy, other changes restart the agent,
// which then continues its session.
type liveSession struct {
	dir   string
	agent string
	args  []string
	// bar runs the agent in tmux below the exile status bar.
	bar bool
	// base holds the settings that do not come from the config files.
	base container.Spec

	mu          sync.Mutex
	resume      bool
	uiURL       string
	running     container.Spec // the settings the agent runs with
	restart     bool
	blocked     []string
	unprotected []string
	lastOpen    time.Time
}

// current computes the session from the current settings.
func (l *liveSession) current() (session, error) {
	cfg, err := config.Load()
	if err != nil {
		return session{}, err
	}
	if err := project.Validate(cfg, l.dir); err != nil {
		return session{}, err
	}
	entries, err := project.Resolve(project.Mounts(cfg, l.dir))
	if err != nil {
		return session{}, err
	}
	hosts := project.Hosts(cfg, l.dir, l.agent)
	agents, err := config.LoadAgents()
	if err != nil {
		return session{}, err
	}
	vars := agents.Env(l.agent)

	l.mu.Lock()
	resume, uiURL := l.resume, l.uiURL
	l.mu.Unlock()

	spec := l.base
	spec.Env = slices.Clone(l.base.Env)
	for _, e := range entries {
		spec.Mounts = append(spec.Mounts, e.Mount)
	}
	for _, h := range hosts {
		spec.Allow = append(spec.Allow, h.Name)
	}
	for _, v := range vars {
		spec.AgentEnv = append(spec.AgentEnv, v.Name+"="+v.Value)
	}

	command := slices.Clone(container.Agents[l.agent].Command)
	if resume {
		command = append([]string{"exile-resume"}, command...)
	}
	command = append(command, l.args...)
	if l.bar {
		command = append([]string{"exile-session"}, command...)
		spec.Env = append(spec.Env, "EXILE_AGENT="+l.agent, "EXILE_PROJECT="+filepath.Base(l.dir))
		if uiURL != "" {
			spec.Env = append(spec.Env, "EXILE_UI=1")
		}
	}
	spec.Command = command
	return session{spec: spec, entries: entries, hosts: hosts, vars: vars, resume: resume, uiURL: uiURL}, nil
}

// run runs the agent until it exits for good, restarting it whenever Apply
// asks for it. It returns the result of the last run, with the hosts
// refused during the whole session.
func (l *liveSession) run(p painter) (container.Result, error) {
	for restarted := false; ; restarted = true {
		s, err := l.current()
		if err != nil {
			return container.Result{}, err
		}
		l.mu.Lock()
		l.running, l.restart = s.spec, false
		l.unprotected = append(l.unprotected, project.Unprotected(s.entries)...)
		l.mu.Unlock()

		if restarted {
			fmt.Fprintln(os.Stderr, p.paint("1;38;5;208", "\n  Applying the new settings; the agent continues its session."))
		}
		printBanner(os.Stderr, p, s)
		res, err := container.Run(s.spec, l.handle)

		l.mu.Lock()
		l.blocked = append(l.blocked, res.Blocked...)
		restart := l.restart
		l.resume = container.Agents[l.agent].Resume
		res.Blocked = unique(l.blocked)
		l.mu.Unlock()
		if err != nil || !restart {
			return res, err
		}
	}
}

// handle carries out a request from the session. The agent can send them
// too, so they must be harmless: opening the settings page shows the user
// the page, but gives the agent nothing. Copying replaces what is on the
// clipboard, as terminals with OSC 52 let any program do, but never reads
// it.
func (l *liveSession) handle(req string) {
	switch name, arg, _ := strings.Cut(req, " "); name {
	case "open-settings":
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.uiURL == "" || time.Since(l.lastOpen) < 2*time.Second {
			return
		}
		l.lastOpen = time.Now()
		openBrowser(l.uiURL)
	case "copy":
		if text, err := base64.StdEncoding.DecodeString(arg); err == nil {
			copyToClipboard(text)
		}
	}
}

// createdProtected returns the protected paths that did not exist when the
// agent started but do now.
func (l *liveSession) createdProtected() []string {
	var created []string
	for _, p := range unique(l.unprotected) {
		if _, err := os.Lstat(p); err == nil {
			created = append(created, p)
		}
	}
	return created
}

// Name implements ui.Session.
func (l *liveSession) Name() string { return l.base.Name }

// Pending implements ui.Session: it reports whether the settings differ
// from those the agent runs with.
func (l *liveSession) Pending() (bool, error) {
	s, err := l.current()
	if err != nil {
		return false, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return !needsNoRestart(s.spec, l.running) || !slices.Equal(s.spec.Allow, l.running.Allow), nil
}

// Apply implements ui.Session.
func (l *liveSession) Apply() (string, error) {
	s, err := l.current()
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case l.restart:
		return "The agent is already restarting.", nil
	case !needsNoRestart(s.spec, l.running):
		l.restart = true
		go container.Stop(l.running.Name)
		return "Restarting the agent with the new settings. It continues its session.", nil
	case slices.Equal(s.spec.Allow, l.running.Allow):
		return "Nothing to apply.", nil
	case l.running.Network != container.NetworkFiltered:
		l.running.Allow = s.spec.Allow
		return "Saved. This session's network is not filtered, so nothing changes now.", nil
	}
	blocked, err := container.ReloadProxy(s.spec)
	l.blocked = append(l.blocked, blocked...)
	if err != nil {
		return "", err
	}
	l.running.Allow = s.spec.Allow
	return "Network settings applied. The agent keeps running.", nil
}

// needsNoRestart reports whether a and b only differ in their allowed hosts,
// which the proxy can pick up without restarting the agent.
func needsNoRestart(a, b container.Spec) bool {
	return reflect.DeepEqual(a.Mounts, b.Mounts) && slices.Equal(a.AgentEnv, b.AgentEnv)
}

func unique(s []string) []string {
	var out []string
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

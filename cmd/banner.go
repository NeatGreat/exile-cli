package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"exileagent/internal/config"
	"exileagent/internal/container"
	"exileagent/internal/project"
)

// logo is "EXILE" in the ANSI Shadow figlet font.
var logo = []string{
	"███████╗██╗  ██╗██╗██╗     ███████╗",
	"██╔════╝╚██╗██╔╝██║██║     ██╔════╝",
	"█████╗   ╚███╔╝ ██║██║     █████╗  ",
	"██╔══╝   ██╔██╗ ██║██║     ██╔══╝  ",
	"███████╗██╔╝ ██╗██║███████╗███████╗",
	"╚══════╝╚═╝  ╚═╝╚═╝╚══════╝╚══════╝",
}

// logoColors fade the logo from orange to purple in the 256-color palette.
var logoColors = []int{214, 208, 203, 198, 163, 129}

// painter applies ANSI styles if enabled.
type painter bool

func (p painter) paint(style, text string) string {
	if !p {
		return text
	}
	return "\033[" + style + "m" + text + "\033[0m"
}

// useColor reports whether f is a terminal that should get colors.
func useColor(f *os.File) bool {
	return isTerminal(f) && os.Getenv("NO_COLOR") == ""
}

// session describes an agent session for the banner.
type session struct {
	spec    container.Spec
	entries []project.Entry
	hosts   []project.Host
	vars    []config.Var
	resume  bool
	uiURL   string
}

// printBanner shows a header above the agent's interface, so it is obvious
// that the session runs in exile and what the agent can reach.
func printBanner(w io.Writer, p painter, s session) {
	fmt.Fprintln(w)
	for i, line := range logo {
		fmt.Fprintf(w, "  %s\n", p.paint(fmt.Sprintf("1;38;5;%d", logoColors[i]), line))
	}
	fmt.Fprintf(w, "  %s\n\n", p.paint("2", "your coding agent, exiled to a container"))

	row := func(label, value string) {
		fmt.Fprintf(w, "  %s %s\n", p.paint("2", fmt.Sprintf("%-8s", label)), value)
	}
	row("agent", p.paint("1", s.spec.Agent))
	if s.resume {
		row("session", "continues the last session, if any "+p.paint("2", "(--new for a fresh one)"))
	} else if container.Agents[s.spec.Agent].Resume {
		row("session", "new")
	}

	workdirMode, extra, hidden, protected := "", 0, 0, 0
	for _, e := range s.entries {
		switch {
		case e.Source == s.spec.Workdir:
			workdirMode = modeText[e.Mode]
		case e.Scope == project.ScopeProtected:
			protected++
		case e.Mode == container.Hidden:
			hidden++
		default:
			extra++
		}
	}
	row("project", tilde(s.spec.Workdir)+"  "+p.paint("2", workdirMode))
	var access []string
	if extra > 0 {
		access = append(access, plural(extra, "more path"))
	}
	if hidden > 0 {
		access = append(access, fmt.Sprintf("%d hidden", hidden))
	}
	if protected > 0 {
		access = append(access, fmt.Sprintf("%d protected read-only", protected))
	}
	if len(access) == 0 {
		access = append(access, "project folder only")
	}
	row("access", strings.Join(access, " · "))

	switch {
	case s.spec.Network == container.NetworkFiltered && len(s.hosts) == 0:
		row("network", "blocked, no hosts allowed")
	case s.spec.Network == container.NetworkFiltered:
		row("network", "HTTPS to "+plural(len(s.hosts), "allowed host"))
	case s.spec.Network == container.NetworkNone:
		row("network", "none")
	case s.spec.Network == container.NetworkOpen:
		row("network", p.paint("1;31", "unrestricted"))
	}
	row("limits", fmt.Sprintf("%s CPUs · %s memory · %d processes", s.spec.CPUs, memoryText(s.spec.Memory), s.spec.PidsLimit))
	if len(s.vars) > 0 {
		names := make([]string, len(s.vars))
		for i, v := range s.vars {
			names[i] = v.Name
		}
		row("config", strings.Join(names, ", "))
	}
	if s.uiURL != "" {
		row("settings", p.paint("4", s.uiURL))
	}
	fmt.Fprintln(w)
}

var modeText = map[container.Mode]string{
	container.ReadWrite: "read & write",
	container.ReadOnly:  "read-only",
	container.Hidden:    "hidden",
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// memoryText formats docker memory sizes such as "1536m" as "1.5 GB".
func memoryText(m string) string {
	mb, err := strconv.Atoi(strings.TrimSuffix(m, "m"))
	switch {
	case m == "":
		return "unlimited"
	case err != nil || !strings.HasSuffix(m, "m"):
		return m
	case mb >= 1024:
		return strconv.FormatFloat(float64(mb)/1024, 'f', 1, 64) + " GB"
	}
	return strconv.Itoa(mb) + " MB"
}

// tilde abbreviates the home directory in p.
func tilde(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && container.Contains(home, p) {
		if rel, err := filepath.Rel(home, p); err == nil {
			return filepath.Join("~", rel)
		}
	}
	return p
}

// setTitle saves the terminal title and replaces it, so the tab shows that
// the session is exiled. restoreTitle brings the saved title back.
func setTitle(w io.Writer, title string) { fmt.Fprintf(w, "\033[22;0t\033]0;%s\007", title) }
func restoreTitle(w io.Writer)           { fmt.Fprint(w, "\033[23;0t") }

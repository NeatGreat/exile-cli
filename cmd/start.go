package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"exileagent/internal/container"
	"exileagent/internal/project"
)

var startOpts struct {
	env        []string
	image      string
	homeVolume string
	tempHome   bool
	network    string
	memory     string
	cpus       string
	pidsLimit  int
	dryRun     bool
	verbose    bool
	newSession bool
	noUI       bool
	noBar      bool
}

var startCmd = &cobra.Command{
	Use:   "start [agent] [-- agent-args...]",
	Short: "Start a coding agent in a container",
	Long: `Start a coding agent in a Docker container and attach the terminal to it.

The agent is claude (default), codex, cline or shell. It only sees the project
directory and the paths granted with "exile add", and only reaches the hosts
granted with "exile allow"; "exile list" shows both. Arguments after -- are
passed on to the agent.

Claude Code, Codex and Cline continue their last session in the project,
so you can quit the agent and pick up where you left off with the next
"exile start". Use --new for a fresh session. With agent arguments, exile
starts the agent exactly as given.

Each project has its own home volume for the agent, so logins and history
survive between runs but are not shared between projects. Host environment
variables are only passed on with --env.`,
	Example: `  exile start
  exile start codex
  exile start cline
  exile start --new
  exile start -- --model opus
  exile start -e GITHUB_TOKEN
  exile start --network none shell`,
	RunE: runStart,
}

func init() {
	f := startCmd.Flags()
	f.StringArrayVarP(&startOpts.env, "env", "e", nil, "pass an environment variable `NAME[=VALUE]`; without a value it is taken from the host")
	f.StringVar(&startOpts.image, "image", container.DefaultImage, "container image")
	f.StringVar(&startOpts.homeVolume, "home-volume", "", "volume for the agent's home directory (default: one per project)")
	f.BoolVar(&startOpts.tempHome, "temp-home", false, "use a throwaway home directory")
	f.StringVar(&startOpts.network, "network", string(container.NetworkFiltered), "network access: filtered (allowlisted HTTPS only), none or open")
	f.StringVar(&startOpts.memory, "memory", "", "memory limit, e.g. 2g (default: 75% of Docker's memory)")
	f.StringVar(&startOpts.cpus, "cpus", "", "CPU limit, e.g. 1.5 (default: 75% of Docker's CPUs)")
	f.IntVar(&startOpts.pidsLimit, "pids-limit", 1024, "maximum number of processes")
	f.BoolVar(&startOpts.dryRun, "dry-run", false, "print the docker commands instead of running them")
	f.BoolVarP(&startOpts.verbose, "verbose", "v", false, "show the agent's access before starting")
	f.BoolVar(&startOpts.newSession, "new", false, "start a new session instead of continuing the last one")
	f.BoolVar(&startOpts.noUI, "no-ui", false, "do not serve the web interface during the session")
	f.BoolVar(&startOpts.noBar, "no-bar", false, "run the agent without the exile status bar (and without tmux)")
	startCmd.MarkFlagsMutuallyExclusive("home-volume", "temp-home")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	agentName, agentArgs, err := splitAgentArgs(cmd, args)
	if err != nil {
		return err
	}
	agent, ok := container.Agents[agentName]
	if !ok {
		return fmt.Errorf("unknown agent %q (available: %s)", agentName, strings.Join(container.AgentNames(), ", "))
	}
	dir, err := projectDir()
	if err != nil {
		return err
	}

	tty := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	l := &liveSession{
		dir:    dir,
		agent:  agentName,
		args:   agentArgs,
		bar:    tty && !startOpts.noBar,
		resume: agent.Resume && !startOpts.newSession && len(agentArgs) == 0,
		base: container.Spec{
			Name:       container.NewName(dir),
			Image:      startOpts.image,
			Agent:      agentName,
			Workdir:    dir,
			HomeVolume: startOpts.homeVolume,
			Env:        startOpts.env,
			Network:    container.NetworkMode(startOpts.network),
			Memory:     startOpts.memory,
			CPUs:       startOpts.cpus,
			PidsLimit:  startOpts.pidsLimit,
			TTY:        tty,
			UID:        os.Getuid(),
			GID:        os.Getgid(),
		},
	}
	switch {
	case startOpts.tempHome:
		l.base.HomeVolume = ""
	case l.base.HomeVolume == "":
		l.base.HomeVolume = container.HomeVolumeName(dir)
	}
	if err := defaultLimits(&l.base); err != nil {
		return err
	}
	// Check the settings before anything starts.
	s, err := l.current()
	if err != nil {
		return err
	}

	if startOpts.dryRun {
		cmds, err := s.spec.Commands()
		if err != nil {
			return err
		}
		for _, c := range cmds {
			line := shellJoin(append([]string{"docker"}, c.Args...))
			if c.Input != nil {
				line += " < (agent configuration)"
			}
			fmt.Println(line)
		}
		return nil
	}
	if l.base.Image == container.DefaultImage {
		if err := ensureImage(l.base.Image); err != nil {
			return err
		}
	}
	if startOpts.verbose {
		if err := printAccess(s.spec, s.entries, s.hosts); err != nil {
			return err
		}
	}
	if !startOpts.noUI {
		if url, srv, err := serveUI(dir, 0, l); err == nil {
			l.uiURL = url
			defer srv.Close()
		}
	}

	title := isTerminal(os.Stderr)
	if title {
		setTitle(os.Stderr, "EXILED · "+agentName+" · "+filepath.Base(dir))
	}
	res, err := l.run(painter(useColor(os.Stderr)))
	if title {
		restoreTitle(os.Stderr)
	}
	if err != nil {
		return err
	}
	report(res.Blocked, l.createdProtected())
	if res.ExitCode != 0 {
		os.Exit(res.ExitCode)
	}
	return nil
}

// defaultLimits sets memory and CPU limits to 75% of what Docker has, so a
// runaway agent cannot starve the Docker VM.
func defaultLimits(spec *container.Spec) error {
	if spec.Memory != "" && spec.CPUs != "" {
		return nil
	}
	cpus, memory, err := container.Resources()
	if err != nil {
		return err
	}
	if spec.Memory == "" {
		spec.Memory = strconv.FormatInt(memory*3/4>>20, 10) + "m"
	}
	if spec.CPUs == "" {
		spec.CPUs = strconv.FormatFloat(max(float64(cpus)*3/4, 1), 'f', 2, 64)
	}
	return nil
}

func printAccess(spec container.Spec, entries []project.Entry, hosts []project.Host) error {
	rows := mountRows(entries)
	if spec.HomeVolume != "" {
		rows = append(rows, row{"rw", "volume " + spec.HomeVolume + " -> " + container.Home, "home"})
	}
	if err := printRows(os.Stderr, row{"MODE", "PATH", "SCOPE"}, rows); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nnetwork %s, memory %s, cpus %s, pids %d\n", spec.Network, spec.Memory, spec.CPUs, spec.PidsLimit)
	if spec.Network == container.NetworkFiltered {
		var names []string
		for _, h := range hosts {
			names = append(names, h.Name)
		}
		fmt.Fprintf(os.Stderr, "hosts %s\n", strings.Join(names, ", "))
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

// report tells the user what the agent tried and did that needs attention.
func report(blocked, created []string) {
	if len(blocked) > 0 {
		fmt.Fprintf(os.Stderr, "exile: blocked connections to %s\n", strings.Join(blocked, ", "))
		fmt.Fprintf(os.Stderr, "exile: allow a host with: exile allow HOST\n")
	}
	for _, p := range created {
		fmt.Fprintf(os.Stderr, "exile: warning: the agent created %s; review it before tools on the host use it\n", p)
	}
}

// splitAgentArgs splits "[agent] [-- agent-args...]".
func splitAgentArgs(cmd *cobra.Command, args []string) (string, []string, error) {
	agent, rest := args, []string(nil)
	if n := cmd.ArgsLenAtDash(); n >= 0 {
		agent, rest = args[:n], args[n:]
	}
	switch len(agent) {
	case 0:
		return container.DefaultAgent, rest, nil
	case 1:
		return agent[0], rest, nil
	}
	return "", nil, errors.New("expected one agent; pass agent arguments after --")
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}

// shellJoin quotes args for display as a shell command.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		safe := a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
		}) < 0
		if safe {
			quoted[i] = a
		} else {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

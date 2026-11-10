package cmd

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"exileagent/internal/ui"
)

var uiOpts struct {
	port   int
	noOpen bool
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Manage access in a web interface",
	Long: `Open a local web interface to manage what agents can access: switch mounts
on and off, add files and directories, and allow hosts. "exile start" also
serves it while the agent runs; the link is in its header.

The interface only listens on 127.0.0.1 and only accepts requests carrying
the secret in the link printed at startup.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := projectDir()
		if err != nil {
			return err
		}
		url, srv, err := serveUI(dir, uiOpts.port, nil)
		if err != nil {
			return err
		}
		fmt.Printf("Exile settings: %s\nPress Ctrl-C to stop.\n", url)
		if !uiOpts.noOpen {
			openBrowser(url)
		}
		<-srv.done
		return srv.err
	},
}

func init() {
	uiCmd.Flags().IntVar(&uiOpts.port, "port", 0, "port to listen on (default: any free port)")
	uiCmd.Flags().BoolVar(&uiOpts.noOpen, "no-open", false, "do not open the browser")
	rootCmd.AddCommand(uiCmd)
}

// uiServer is a web interface running in the background.
type uiServer struct {
	*http.Server
	done chan struct{} // closed when the server stops
	err  error
}

// serveUI starts the web interface for the project in dir on 127.0.0.1 in
// the background and returns its link. session is the agent session running
// in dir, if any. The server never writes to the terminal, so it cannot
// disturb an agent's interface.
func serveUI(dir string, port int, session ui.Session) (string, *uiServer, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", nil, err
	}
	s := &ui.Server{Token: rand.Text(), Project: dir, Addr: ln.Addr().String(), Session: session}
	srv := &uiServer{
		Server: &http.Server{
			Handler:           s.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ErrorLog:          log.New(io.Discard, "", 0),
		},
		done: make(chan struct{}),
	}
	go func() {
		srv.err = srv.Serve(ln)
		close(srv.done)
	}()
	return fmt.Sprintf("http://%s/#%s", s.Addr, s.Token), srv, nil
}

func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	exec.Command(name, url).Start()
}

// copyToClipboard puts text on the clipboard.
func copyToClipboard(text []byte) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		cmd = exec.Command("pbcopy")
		// pbcopy reads text in the encoding of the locale.
		cmd.Env = append(os.Environ(), "LC_CTYPE=UTF-8")
	case os.Getenv("WAYLAND_DISPLAY") != "":
		cmd = exec.Command("wl-copy")
	default:
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = bytes.NewReader(text)
	cmd.Run()
}

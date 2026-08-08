package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// resolveDBPath decides where the SQLite file lives.
//
// An explicit DB_PATH always wins — that is how the container is configured, so
// nothing here changes container behaviour. When it is unset the default has to
// be an absolute path rather than a cwd-relative one: an app launched from
// Finder runs with a working directory of "/", where creating apptracker.db
// fails. If there is no user config dir (a headless box with no HOME), fall
// back to the historical cwd-relative name rather than refusing to start.
func resolveDBPath(getenv func(string) string, userConfigDir func() (string, error)) (string, error) {
	if v := getenv("DB_PATH"); v != "" {
		return v, nil
	}
	dir, err := userConfigDir()
	if err != nil || dir == "" {
		return "apptracker.db", nil
	}
	return filepath.Join(dir, "apptracker", "apptracker.db"), nil
}

// listen binds addr, falling back to an ephemeral port when the address is busy
// and the caller did not ask for that port specifically.
//
// The asymmetry is deliberate. An operator who set PORT wants that port, and
// quietly moving would break whatever maps to it — so that case fails loudly.
// A desktop user who set nothing just wants the app to open, and often has no
// terminal to read a fatal log from.
func listen(addr string, explicitPort bool) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if explicitPort {
		return nil, err
	}
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, err
	}
	log.Printf("port in use, picking a free one instead (%v)", err)
	return net.Listen("tcp", net.JoinHostPort(host, "0"))
}

// shouldOpenBrowser reports whether to launch a browser at startup. Opt-in:
// this same binary runs as a long-lived server, where popping a browser would
// be wrong. The macOS app bundle sets APP_OPEN_BROWSER.
func shouldOpenBrowser(getenv func(string) string) bool {
	return getenv("APP_OPEN_BROWSER") != ""
}

// browserURL turns a listener address into something a browser can open.
// A wildcard bind (":8080", "[::]:8080", "0.0.0.0:8080") is not a destination.
func browserURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://localhost" + addr
	}
	if host == "" || host == "::" || host == "0.0.0.0" {
		host = "localhost"
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// openBrowser is best-effort: failing to open a browser is never a reason to
// stop serving.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open a browser (%v) — open %s yourself", err, url)
	}
}

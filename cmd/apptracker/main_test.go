package main

import (
	"errors"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveDBPathPrefersExplicitEnv(t *testing.T) {
	got, err := resolveDBPath(
		func(string) string { return "/data/apptracker.db" },
		func() (string, error) { t.Fatal("should not consult the config dir"); return "", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/data/apptracker.db" {
		t.Fatalf("got %q, want the explicit DB_PATH", got)
	}
}

// Launched from Finder the working directory is "/", so a cwd-relative default
// would try to create /apptracker.db and fail.
func TestResolveDBPathUsesUserConfigDirWhenUnset(t *testing.T) {
	got, err := resolveDBPath(
		func(string) string { return "" },
		func() (string, error) { return "/Users/x/Library/Application Support", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/Users/x/Library/Application Support", "apptracker", "apptracker.db")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A headless box with no HOME still has to start.
func TestResolveDBPathFallsBackWhenNoConfigDir(t *testing.T) {
	got, err := resolveDBPath(
		func(string) string { return "" },
		func() (string, error) { return "", errors.New("no home") },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "apptracker.db" {
		t.Fatalf("got %q, want the cwd-relative fallback", got)
	}
}

// An explicitly requested port that is taken must fail loudly — silently moving
// would break a port mapping the operator chose.
func TestListenFailsOnBusyExplicitPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	if _, err := listen(busy.Addr().String(), true); err == nil {
		t.Fatal("expected an error for an explicitly requested busy port")
	}
}

// An unset PORT that happens to be taken should move rather than die — on a
// desktop there is often no terminal to show the fatal log.
func TestListenFallsBackOnBusyDefaultPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	ln, err := listen(busy.Addr().String(), false)
	if err != nil {
		t.Fatalf("expected a fallback listener, got %v", err)
	}
	defer ln.Close()
	if ln.Addr().String() == busy.Addr().String() {
		t.Fatal("expected a different port than the busy one")
	}
}

func TestShouldOpenBrowserIsOptIn(t *testing.T) {
	if shouldOpenBrowser(func(string) string { return "" }) {
		t.Fatal("must not open a browser unless asked — this also runs as a server")
	}
	if !shouldOpenBrowser(func(k string) string {
		if k == "APP_OPEN_BROWSER" {
			return "1"
		}
		return ""
	}) {
		t.Fatal("expected opt-in to be honoured")
	}
}

func TestBrowserURLUsesLoopbackForWildcardBind(t *testing.T) {
	got := browserURL("[::]:8080")
	if !strings.Contains(got, "localhost") {
		t.Fatalf("got %q, want a loopback URL a browser can open", got)
	}
	if runtime.GOOS == "" {
		t.Fatal("unreachable")
	}
}

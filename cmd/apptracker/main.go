// Command apptracker is a single-binary, SQLite-backed job application and
// networking tracker with an embedded web UI.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/IamMrCupp/apptracker/internal/api"
	"github.com/IamMrCupp/apptracker/internal/auth"
	"github.com/IamMrCupp/apptracker/internal/store"
	"github.com/IamMrCupp/apptracker/web"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		portSet = os.Getenv("PORT") != ""
		addr    = ":" + env("PORT", "8080")
		passwd  = os.Getenv("APP_PASSWORD")    // empty => open access
		signKey = os.Getenv("APP_SESSION_KEY") // empty => random (sessions reset on restart)
	)

	dbPath, err := resolveDBPath(os.Getenv, os.UserConfigDir)
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create data dir %s: %v", dir, err)
		}
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	authr, err := auth.New(passwd, signKey)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	srv := &api.Server{Store: st, Auth: authr}
	mux := srv.Routes()

	// Serve the embedded SPA for everything the API mux didn't claim.
	fileServer := http.FileServer(http.FS(web.FS()))
	mux.Handle("/", fileServer)

	ln, err := listen(addr, portSet)
	if err != nil {
		log.Fatalf("listen on %s: %v", addr, err)
	}

	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	url := browserURL(ln.Addr().String())
	go func() {
		mode := "open (no password)"
		if authr.Enabled() {
			mode = "password-protected"
		}
		log.Printf("apptracker listening on %s | db=%s | auth=%s", url, dbPath, mode)
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	if shouldOpenBrowser(os.Getenv) {
		openBrowser(url)
	}

	// Graceful shutdown on SIGINT/SIGTERM (k8s sends SIGTERM).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

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
		addr    = ":" + env("PORT", "8080")
		dbPath  = env("DB_PATH", "apptracker.db")
		passwd  = os.Getenv("APP_PASSWORD")    // empty => open access
		signKey = os.Getenv("APP_SESSION_KEY") // empty => random (sessions reset on restart)
	)

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

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		mode := "open (no password)"
		if authr.Enabled() {
			mode = "password-protected"
		}
		log.Printf("apptracker listening on %s | db=%s | auth=%s", addr, dbPath, mode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

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

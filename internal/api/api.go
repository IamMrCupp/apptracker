// Package api wires HTTP handlers for the tracker's JSON API and import/export.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/IamMrCupp/apptracker/internal/auth"
	"github.com/IamMrCupp/apptracker/internal/store"
)

const maxBody = 16 << 20 // 16 MiB cap on request/import bodies

// Server holds dependencies for the API handlers.
type Server struct {
	Store *store.Store
	Auth  *auth.Authenticator
}

// Routes returns a mux with all API endpoints registered.
// The frontend (static files) is mounted by the caller.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Public endpoints (auth handled internally / not required).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("POST /api/login", s.Auth.LoginHandler)
	mux.HandleFunc("POST /api/logout", s.Auth.LogoutHandler)
	mux.HandleFunc("GET /api/session", s.handleSession)

	// Protected API surface.
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/entries", s.handleList)
	protected.HandleFunc("POST /api/entries", s.handleCreate)
	protected.HandleFunc("PUT /api/entries/{id}", s.handleUpdate)
	protected.HandleFunc("DELETE /api/entries/{id}", s.handleDelete)
	protected.HandleFunc("POST /api/clear", s.handleClear)
	protected.HandleFunc("GET /api/export", s.handleExport)
	protected.HandleFunc("POST /api/import", s.handleImport)
	mux.Handle("/api/", s.Auth.Middleware(protected))

	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// handleSession reports whether auth is required and whether the caller is in.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"authRequired": s.Auth.Enabled(),
		"authed":       s.Auth.Authed(r),
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && !store.ValidKind(kind) {
		writeErr(w, http.StatusBadRequest, "invalid kind")
		return
	}
	entries, err := s.Store.List(r.Context(), kind)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) decodeEntry(w http.ResponseWriter, r *http.Request) (store.Entry, bool) {
	var e store.Entry
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	if err := dec.Decode(&e); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return store.Entry{}, false
	}
	if !store.ValidKind(e.Kind) {
		writeErr(w, http.StatusBadRequest, "kind must be 'application' or 'networking'")
		return store.Entry{}, false
	}
	return e, true
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	e, ok := s.decodeEntry(w, r)
	if !ok {
		return
	}
	created, err := s.Store.Create(r.Context(), e)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	e, ok := s.decodeEntry(w, r)
	if !ok {
		return
	}
	updated, err := s.Store.Update(r.Context(), id, e)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	err := s.Store.Delete(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "entry not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && !store.ValidKind(kind) {
		writeErr(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if err := s.Store.Clear(r.Context(), kind); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	entries, err := s.Store.List(r.Context(), "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="apptracker-export.json"`)
		_ = json.NewEncoder(w).Encode(entries)
	case "csv":
		b, err := entriesToCSV(entries)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="apptracker-export.csv"`)
		_, _ = w.Write(b)
	default:
		writeErr(w, http.StatusBadRequest, "format must be json or csv")
	}
}

// handleImport replaces all entries from an uploaded JSON or CSV snapshot.
// format is taken from the ?format= query (default json).
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "body too large or unreadable")
		return
	}

	var entries []store.Entry
	switch format {
	case "json":
		if err := json.Unmarshal(body, &entries); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		for i := range entries {
			if !store.ValidKind(entries[i].Kind) {
				writeErr(w, http.StatusBadRequest, "every entry needs a valid kind")
				return
			}
		}
	case "csv":
		entries, err = csvToEntries(body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "format must be json or csv")
		return
	}

	if err := s.Store.ReplaceAll(r.Context(), entries); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": len(entries)})
}

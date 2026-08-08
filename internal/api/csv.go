package api

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/IamMrCupp/apptracker/internal/store"
)

// csvHeader is the fixed column order for CSV import/export.
var csvHeader = []string{
	"id", "kind", "lane", "entity", "context", "date", "channel",
	"comp", "follow_up", "status", "link", "notes", "created_at", "updated_at",
}

// entriesToCSV serialises entries as CSV bytes.
func entriesToCSV(entries []store.Entry) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}
	for _, e := range entries {
		rec := []string{
			strconv.FormatInt(e.ID, 10), e.Kind, e.Lane, e.Entity, e.Context,
			e.Date, e.Channel, e.Comp, e.FollowUp, e.Status, e.Link, e.Notes,
			e.CreatedAt, e.UpdatedAt,
		}
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// csvToEntries parses CSV bytes into entries. The header row drives column
// mapping so column order is not required to match, only names.
func csvToEntries(data []byte) ([]store.Entry, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // tolerate ragged rows

	header, err := r.Read()
	if err == io.EOF {
		return []store.Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	if _, ok := idx["kind"]; !ok {
		return nil, fmt.Errorf("csv missing required 'kind' column")
	}

	get := func(rec []string, name string) string {
		i, ok := idx[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return rec[i]
	}

	out := []store.Entry{}
	line := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		line++
		e := store.Entry{
			Kind:      get(rec, "kind"),
			Lane:      get(rec, "lane"),
			Entity:    get(rec, "entity"),
			Context:   get(rec, "context"),
			Date:      get(rec, "date"),
			Channel:   get(rec, "channel"),
			Comp:      get(rec, "comp"),
			FollowUp:  get(rec, "follow_up"),
			Status:    get(rec, "status"),
			Link:      get(rec, "link"),
			Notes:     get(rec, "notes"),
			CreatedAt: get(rec, "created_at"),
			UpdatedAt: get(rec, "updated_at"),
		}
		if !store.ValidKind(e.Kind) {
			return nil, fmt.Errorf("line %d: invalid kind %q", line, e.Kind)
		}
		out = append(out, e)
	}
	return out, nil
}

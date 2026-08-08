package store

import (
	"context"
	"errors"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleApp() Entry {
	return Entry{
		Kind:    KindApplication,
		Lane:    "Active",
		Entity:  "Acme Corp",
		Context: "Staff SRE",
		Date:    "2026-08-01",
		Channel: "Referral",
		Comp:    "$210k",
		Status:  "Applied",
		Link:    "https://acme.example/jobs/1",
		Notes:   "remote, no RTO",
	}
}

func TestCreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.Create(ctx, sampleApp())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatal("expected timestamps to be set")
	}

	fetched, err := s.Get(ctx, got.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Entity != "Acme Corp" || fetched.Status != "Applied" {
		t.Fatalf("round-trip mismatch: %+v", fetched)
	}
}

func TestCreateRejectsBadKind(t *testing.T) {
	s := newTestStore(t)
	e := sampleApp()
	e.Kind = "bogus"
	if _, err := s.Create(context.Background(), e); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestListFilterByKind(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}
	net := sampleApp()
	net.Kind = KindNetworking
	net.Entity = "Jane Doe"
	if _, err := s.Create(ctx, net); err != nil {
		t.Fatal(err)
	}

	apps, err := s.List(ctx, KindApplication)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Kind != KindApplication {
		t.Fatalf("expected 1 application, got %d", len(apps))
	}

	all, err := s.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 total, got %d", len(all))
	}
	// newest first
	if all[0].ID < all[1].ID {
		t.Fatal("expected newest-first ordering")
	}
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	created, err := s.Create(ctx, sampleApp())
	if err != nil {
		t.Fatal(err)
	}
	created.Status = "Interviewing"
	updated, err := s.Update(ctx, created.ID, created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "Interviewing" {
		t.Fatalf("status not updated: %q", updated.Status)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatal("created_at should be preserved on update")
	}
}

func TestUpdateMissing(t *testing.T) {
	_, err := newTestStore(t).Update(context.Background(), 999, sampleApp())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	created, err := s.Create(ctx, sampleApp())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := s.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting twice, got %v", err)
	}
}

func TestClearByKind(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}
	net := sampleApp()
	net.Kind = KindNetworking
	if _, err := s.Create(ctx, net); err != nil {
		t.Fatal(err)
	}

	if err := s.Clear(ctx, KindApplication); err != nil {
		t.Fatal(err)
	}
	all, _ := s.List(ctx, "")
	if len(all) != 1 || all[0].Kind != KindNetworking {
		t.Fatalf("expected only networking to remain, got %+v", all)
	}
}

func TestReplaceAll(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}
	newSet := []Entry{
		{Kind: KindApplication, Entity: "One"},
		{Kind: KindNetworking, Entity: "Two"},
	}
	if err := s.ReplaceAll(ctx, newSet); err != nil {
		t.Fatal(err)
	}
	all, _ := s.List(ctx, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 after replace, got %d", len(all))
	}
}

func TestReplaceAllRollsBackOnBadEntry(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.Create(ctx, sampleApp()); err != nil {
		t.Fatal(err)
	}
	bad := []Entry{
		{Kind: KindApplication, Entity: "Good"},
		{Kind: "nope", Entity: "Bad"},
	}
	if err := s.ReplaceAll(ctx, bad); err == nil {
		t.Fatal("expected error")
	}
	// original data must survive the failed import
	all, _ := s.List(ctx, "")
	if len(all) != 1 || all[0].Entity != "Acme Corp" {
		t.Fatalf("expected original row preserved after rollback, got %+v", all)
	}
}

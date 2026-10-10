package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeDNS struct {
	records []Record
	nextID  int
	created []string
	deleted []string
}

func (f *fakeDNS) ListRecords(context.Context) ([]Record, error) {
	return append([]Record(nil), f.records...), nil
}

func (f *fakeDNS) CreateRecord(_ context.Context, r Record) (Record, error) {
	f.nextID++
	r.ID = fmt.Sprint(f.nextID)
	f.records = append(f.records, r)
	f.created = append(f.created, r.Name)
	return r, nil
}

func (f *fakeDNS) DeleteRecord(_ context.Context, id string) error {
	for i, r := range f.records {
		if r.ID == id {
			f.records = append(f.records[:i], f.records[i+1:]...)
			f.deleted = append(f.deleted, r.Name)
			return nil
		}
	}
	return fmt.Errorf("no record %s", id)
}

type fakeContainers struct{ hosts []string }

func (f *fakeContainers) RunningContainers(context.Context) ([]Container, error) {
	var out []Container
	for i, h := range f.hosts {
		out = append(out, Container{
			ID:     fmt.Sprint(i),
			Labels: map[string]string{"traefik.http.routers.r.rule": "Host(`" + h + "`)"},
		})
	}
	return out, nil
}

func newTestReconciler(t *testing.T, cfg Config, dns *fakeDNS, c *fakeContainers) (*Reconciler, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	r := NewReconciler(cfg, dns, c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.now = func() time.Time { return now }
	return r, &now
}

func testConfig() Config {
	return Config{
		Domain:      "example.com",
		Target:      "192.0.2.10",
		RecordType:  "A",
		TTL:         1,
		Comment:     defaultComment,
		DeleteAfter: time.Hour,
	}
}

func TestReconcileCreatesOnlyHostsInDomain(t *testing.T) {
	dns := &fakeDNS{}
	r, _ := newTestReconciler(t, testConfig(), dns, &fakeContainers{hosts: []string{"app.example.com", "other.example.net"}})
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(dns.created) != 1 || dns.created[0] != "app.example.com" {
		t.Fatalf("created = %v", dns.created)
	}
	got := dns.records[0]
	if got.Type != "A" || got.Content != "192.0.2.10" || got.Comment != defaultComment {
		t.Fatalf("record = %+v", got)
	}
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(dns.created) != 1 {
		t.Fatalf("second pass created again: %v", dns.created)
	}
}

func TestReconcileLeavesForeignRecords(t *testing.T) {
	dns := &fakeDNS{records: []Record{
		{ID: "a", Type: "A", Name: "manual.example.com", Content: "192.0.2.99"},
		{ID: "b", Type: "A", Name: "other-server.example.com", Content: "192.0.2.20", Comment: defaultComment},
		{ID: "c", Type: "A", Name: "same-ip-no-comment.example.com", Content: "192.0.2.10"},
	}}
	r, now := newTestReconciler(t, testConfig(), dns, &fakeContainers{hosts: []string{"manual.example.com"}})
	for range 3 {
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(2 * time.Hour)
	}
	if len(dns.created) != 0 || len(dns.deleted) != 0 {
		t.Fatalf("created %v, deleted %v", dns.created, dns.deleted)
	}
}

func TestReconcileDeletesAfterGracePeriod(t *testing.T) {
	dns := &fakeDNS{}
	containers := &fakeContainers{hosts: []string{"app.example.com"}}
	r, now := newTestReconciler(t, testConfig(), dns, containers)
	ctx := context.Background()
	if err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	containers.hosts = nil
	for _, step := range []time.Duration{0, 30 * time.Minute, 29 * time.Minute} {
		*now = now.Add(step)
		if err := r.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(dns.deleted) != 0 {
		t.Fatalf("deleted before the grace period: %v", dns.deleted)
	}

	*now = now.Add(time.Minute)
	if err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dns.deleted) != 1 || dns.deleted[0] != "app.example.com" {
		t.Fatalf("deleted = %v", dns.deleted)
	}
}

func TestReconcileComebackResetsGracePeriod(t *testing.T) {
	dns := &fakeDNS{}
	containers := &fakeContainers{hosts: []string{"app.example.com"}}
	r, now := newTestReconciler(t, testConfig(), dns, containers)
	ctx := context.Background()
	steps := []struct {
		hosts []string
		after time.Duration
	}{
		{[]string{"app.example.com"}, 0},
		{nil, 0},
		{[]string{"app.example.com"}, 50 * time.Minute},
		{nil, time.Minute},
		{nil, 50 * time.Minute},
	}
	for _, s := range steps {
		containers.hosts = s.hosts
		*now = now.Add(s.after)
		if err := r.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(dns.deleted) != 0 {
		t.Fatalf("grace period was not reset by the host coming back: %v", dns.deleted)
	}
}

func TestReconcileRequiresTagsForOwnership(t *testing.T) {
	cfg := testConfig()
	cfg.Tags = []string{"managed-by:traefik-cloudflare-dns"}
	dns := &fakeDNS{records: []Record{
		{ID: "a", Type: "A", Name: "untagged.example.com", Content: "192.0.2.10", Comment: defaultComment},
	}}
	r, now := newTestReconciler(t, cfg, dns, &fakeContainers{})
	for range 2 {
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(2 * time.Hour)
	}
	if len(dns.deleted) != 0 {
		t.Fatalf("deleted a record without the configured tag: %v", dns.deleted)
	}
}

func TestReconcileDryRunWritesNothing(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = true
	cfg.DeleteAfter = 0
	dns := &fakeDNS{records: []Record{
		{ID: "a", Type: "A", Name: "gone.example.com", Content: "192.0.2.10", Comment: defaultComment},
	}}
	r, _ := newTestReconciler(t, cfg, dns, &fakeContainers{hosts: []string{"app.example.com"}})
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(dns.created) != 0 || len(dns.deleted) != 0 {
		t.Fatalf("dry run wrote: created %v, deleted %v", dns.created, dns.deleted)
	}
}

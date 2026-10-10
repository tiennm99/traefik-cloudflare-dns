package main

import (
	"context"
	"log/slog"
	"slices"
	"time"
)

// DNS is the record store the reconciler writes to.
type DNS interface {
	ListRecords(ctx context.Context) ([]Record, error)
	CreateRecord(ctx context.Context, r Record) (Record, error)
	DeleteRecord(ctx context.Context, id string) error
}

// ContainerSource lists the running containers.
type ContainerSource interface {
	RunningContainers(ctx context.Context) ([]Container, error)
}

// Reconciler keeps the zone in line with the hosts of running containers.
//
// A record is owned when its content is this instance's target and it
// carries the configured comment and every configured tag. Only owned
// records are deleted, so records made by hand, or by another instance
// pointing at another server, are never touched.
type Reconciler struct {
	cfg        Config
	dns        DNS
	containers ContainerSource
	log        *slog.Logger
	now        func() time.Time

	// missingSince tracks when each owned record's host was last served.
	missingSince map[string]time.Time
	// warned avoids repeating the same conflict warning every pass.
	warned map[string]bool
}

// NewReconciler returns a reconciler for cfg.
func NewReconciler(cfg Config, dns DNS, containers ContainerSource, log *slog.Logger) *Reconciler {
	return &Reconciler{
		cfg:          cfg,
		dns:          dns,
		containers:   containers,
		log:          log,
		now:          time.Now,
		missingSince: map[string]time.Time{},
		warned:       map[string]bool{},
	}
}

func (r *Reconciler) owns(rec Record) bool {
	if rec.Type != r.cfg.RecordType || rec.Content != r.cfg.Target || rec.Comment != r.cfg.Comment {
		return false
	}
	for _, tag := range r.cfg.Tags {
		if !slices.Contains(rec.Tags, tag) {
			return false
		}
	}
	return true
}

// Reconcile runs one pass: create records for new hosts, and delete owned
// records whose host has been absent for at least DeleteAfter.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	containers, err := r.containers.RunningContainers(ctx)
	if err != nil {
		return err
	}
	desired := map[string]bool{}
	for _, c := range containers {
		for _, h := range HostsFromLabels(c.Labels) {
			if InDomain(h, r.cfg.Domain) {
				desired[h] = true
			}
		}
	}

	records, err := r.dns.ListRecords(ctx)
	if err != nil {
		return err
	}
	byName := map[string][]Record{}
	for _, rec := range records {
		byName[rec.Name] = append(byName[rec.Name], rec)
	}

	for _, host := range sortedKeys(desired) {
		existing := byName[host]
		if slices.ContainsFunc(existing, r.owns) {
			delete(r.missingSince, host)
			continue
		}
		if len(existing) > 0 {
			if !r.warned[host] {
				r.log.Warn("record exists and is not managed by this instance; leaving it alone",
					"host", host, "type", existing[0].Type, "content", existing[0].Content)
				r.warned[host] = true
			}
			continue
		}
		rec := Record{
			Type:    r.cfg.RecordType,
			Name:    host,
			Content: r.cfg.Target,
			TTL:     r.cfg.TTL,
			Proxied: r.cfg.Proxied,
			Comment: r.cfg.Comment,
			Tags:    r.cfg.Tags,
		}
		if r.cfg.DryRun {
			r.log.Info("dry run: would create record", "host", host, "type", rec.Type, "content", rec.Content)
			continue
		}
		if _, err := r.dns.CreateRecord(ctx, rec); err != nil {
			r.log.Error("create record failed", "host", host, "err", err)
			continue
		}
		r.log.Info("created record", "host", host, "type", rec.Type, "content", rec.Content)
	}
	for host := range r.warned {
		if !desired[host] {
			delete(r.warned, host)
		}
	}

	now := r.now()
	owned := map[string]bool{}
	for _, rec := range records {
		if !r.owns(rec) || desired[rec.Name] {
			continue
		}
		owned[rec.Name] = true
		since, tracked := r.missingSince[rec.Name]
		if !tracked {
			r.missingSince[rec.Name] = now
			r.log.Info("host no longer served; record will be deleted if it stays down",
				"host", rec.Name, "delete_after", r.cfg.DeleteAfter)
			since = now
		}
		if now.Sub(since) < r.cfg.DeleteAfter {
			continue
		}
		if r.cfg.DryRun {
			r.log.Info("dry run: would delete record", "host", rec.Name, "down_for", now.Sub(since).Round(time.Second))
			continue
		}
		if err := r.dns.DeleteRecord(ctx, rec.ID); err != nil {
			r.log.Error("delete record failed", "host", rec.Name, "err", err)
			continue
		}
		r.log.Info("deleted record", "host", rec.Name, "down_for", now.Sub(since).Round(time.Second))
		delete(r.missingSince, rec.Name)
	}
	for host := range r.missingSince {
		if !owned[host] {
			delete(r.missingSince, host)
		}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

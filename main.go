// Command traefik-cloudflare-dns creates Cloudflare DNS records for the
// hostnames in Traefik router labels of running Docker containers, and
// deletes the records it created once their host has been down long enough.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	debounce   = 2 * time.Second
	retryAfter = 10 * time.Second
)

func main() {
	cfg, err := LoadConfig(osGetenv)
	if err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))

	docker, err := NewDockerClient(cfg.DockerHost)
	if err != nil {
		log.Error("invalid configuration", "err", err)
		os.Exit(2)
	}
	rec := NewReconciler(cfg, NewCloudflareClient(cfg.APIToken, cfg.ZoneID), docker, log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("starting",
		"domain", cfg.Domain, "target", cfg.Target, "type", cfg.RecordType,
		"proxied", cfg.Proxied, "comment", cfg.Comment, "tags", cfg.Tags,
		"delete_after", cfg.DeleteAfter, "resync_interval", cfg.ResyncInterval,
		"dry_run", cfg.DryRun, "docker_host", cfg.DockerHost)

	trigger := make(chan struct{}, 1)
	go watchEvents(ctx, docker, trigger, log)
	run(ctx, rec, trigger, cfg.ResyncInterval, log)
	log.Info("stopped")
}

// run reconciles once at start, then after each burst of container events,
// on every resync tick, and shortly after a failed pass, until ctx is done.
func run(ctx context.Context, rec *Reconciler, trigger <-chan struct{}, every time.Duration, log *slog.Logger) {
	var retry <-chan time.Time
	pass := func() {
		retry = nil
		if err := rec.Reconcile(ctx); err != nil && ctx.Err() == nil {
			log.Error("reconcile failed; retrying", "err", err, "in", retryAfter)
			retry = time.After(retryAfter)
		}
	}
	pass()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass()
		case <-retry:
			pass()
		case <-trigger:
			select {
			case <-ctx.Done():
				return
			case <-time.After(debounce):
			}
			pass()
		}
	}
}

// watchEvents keeps a Docker event stream open and signals trigger on every
// container start, die or destroy, reconnecting with backoff when it drops.
func watchEvents(ctx context.Context, docker *DockerClient, trigger chan<- struct{}, log *slog.Logger) {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := docker.WatchContainerEvents(ctx, func(action string) {
			log.Debug("container event", "action", action)
			select {
			case trigger <- struct{}{}:
			default:
			}
		})
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		log.Warn("docker event stream ended; reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

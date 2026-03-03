package watcher

import (
	"context"
	"testing"
	"time"

	"github.com/jpvargasdev/magos-dominus/internal/events"
)

func TestNew(t *testing.T) {
	targets := []Target{
		{
			Name: "test-service",
			Image: ImageRef{
				Registry: "ghcr.io",
				Owner:    "owner",
				Name:     "repo",
				Tag:      "latest",
			},
			Policy: "latest",
		},
	}
	em := make(events.ChanEmitter, 10)

	w := New(targets, em)

	if w == nil {
		t.Fatal("New() returned nil")
	}
	if len(w.targets) != 1 {
		t.Errorf("len(targets) = %d, want 1", len(w.targets))
	}
	if w.emitter == nil {
		t.Error("emitter is nil")
	}
}

func TestWatcher_StartCancellation(t *testing.T) {
	targets := []Target{}
	em := make(events.ChanEmitter, 10)
	w := New(targets, em)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx, nil)
	}()

	// Cancel immediately
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Start() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
}

func TestImageRef(t *testing.T) {
	ref := ImageRef{
		Registry: "ghcr.io",
		Owner:    "myorg",
		Name:     "myapp",
		Tag:      "v1.2.3",
	}

	if ref.Registry != "ghcr.io" {
		t.Errorf("Registry = %s, want ghcr.io", ref.Registry)
	}
	if ref.Owner != "myorg" {
		t.Errorf("Owner = %s, want myorg", ref.Owner)
	}
	if ref.Name != "myapp" {
		t.Errorf("Name = %s, want myapp", ref.Name)
	}
	if ref.Tag != "v1.2.3" {
		t.Errorf("Tag = %s, want v1.2.3", ref.Tag)
	}
}

func TestTarget(t *testing.T) {
	target := Target{
		Name: "stacks/app/compose.yml",
		Image: ImageRef{
			Registry: "ghcr.io",
			Owner:    "org",
			Name:     "service",
			Tag:      "latest",
		},
		Policy:   "semver",
		Interval: 60,
	}

	if target.Name != "stacks/app/compose.yml" {
		t.Errorf("Name = %s, want stacks/app/compose.yml", target.Name)
	}
	if target.Policy != "semver" {
		t.Errorf("Policy = %s, want semver", target.Policy)
	}
	if target.Interval != 60 {
		t.Errorf("Interval = %d, want 60", target.Interval)
	}
}

func TestWatcherConfig(t *testing.T) {
	cfg := WatcherConfig{
		Registry:     "ghcr.io",
		DefaultTag:   "latest",
		PollInterval: 5 * time.Minute,
		Targets: []Target{
			{Name: "svc1", Policy: "latest"},
			{Name: "svc2", Policy: "semver"},
		},
	}

	if cfg.Registry != "ghcr.io" {
		t.Errorf("Registry = %s, want ghcr.io", cfg.Registry)
	}
	if cfg.DefaultTag != "latest" {
		t.Errorf("DefaultTag = %s, want latest", cfg.DefaultTag)
	}
	if cfg.PollInterval != 5*time.Minute {
		t.Errorf("PollInterval = %v, want 5m", cfg.PollInterval)
	}
	if len(cfg.Targets) != 2 {
		t.Errorf("len(Targets) = %d, want 2", len(cfg.Targets))
	}
}

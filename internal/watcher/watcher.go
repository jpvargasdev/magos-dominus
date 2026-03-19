package watcher

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/jpvargasdev/magos-dominus/internal/config"
	"github.com/jpvargasdev/magos-dominus/internal/events"
	"github.com/jpvargasdev/magos-dominus/internal/state"
)

type Target struct {
	Name     string   // logical name (service or file reference)
	Image    ImageRef // parsed reference
	Policy   string   // "semver", "latest", "digest", "manual"
	Interval int      // optional: poll interval in seconds (could default)
}

type ImageRef struct {
	Registry string
	Owner    string
	Name     string
	Tag      string
}

type WatcherConfig struct {
	Registry     string
	DefaultTag   string
	PollInterval time.Duration
	Targets      []Target
}

type Config struct {
	Watcher WatcherConfig
}

type Watcher struct {
	targets []Target
	emitter events.Emitter
	itr     *ghinstallation.Transport
}

func New(targets []Target, em events.Emitter, itr *ghinstallation.Transport) *Watcher {
	return &Watcher{targets: targets, emitter: em, itr: itr}
}

func (w *Watcher) Start(ctx context.Context, st *state.File) error {
	ghcr := NewGHCR(w.itr)

	if len(w.targets) == 0 {
		log.Printf("[watcher] no targets configured; idle")
	}

	pollInterval := config.GetPollInterval()
	log.Printf("[watcher] poll interval: %s", pollInterval)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	w.runOnce(ctx, ghcr, st)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[watcher] context canceled, stopping")
			return ctx.Err()
		case <-ticker.C:
			w.runOnce(ctx, ghcr, st)
		}
	}
}

func (w *Watcher) runOnce(ctx context.Context, ghcr *GHCR, st *state.File) {
	for _, t := range w.targets {
		repo := fmt.Sprintf("%s/%s", t.Image.Owner, t.Image.Name)
		refIn := strings.ToLower(t.Image.Tag)

		// For semver, store under a stable "channel" key
		refKey := refIn
		if strings.EqualFold(t.Policy, "semver") {
			refKey = "semver"
		}

		key := state.Key(
			strings.ToLower(t.Image.Registry),
			strings.ToLower(t.Image.Owner),
			strings.ToLower(t.Image.Name),
			refKey,
		)

		prev, ok := st.Get(key)
		etagIn := ""
		if ok {
			etagIn = prev.ETag
		}

		digest, resolvedRef, etagOut, notMod, err := ghcr.HeadDigest(ctx, repo, refIn, etagIn, t.Policy)
		if err != nil {
			log.Printf("[watcher] skip %s:%s: %v", repo, refIn, err)
			continue
		}

		// Log with resolved ref (fixes the confusion)
		log.Printf("[watcher] repo=%s resolvedRef=%s policy=%s notMod=%v", repo, resolvedRef, t.Policy, notMod)

		if notMod {
			st.UpdateChecked(key, t.Policy)
			continue
		}

		// Seed baseline if none
		if !ok {
			st.UpsertDigest(key, digest, etagOut, t.Policy)
			st.Save()
			log.Printf("[watcher] seeded baseline for %s:%s -> %s", repo, resolvedRef, digest)
			continue
		}

		if prev.Digest == digest {
			st.UpsertDigest(key, digest, etagOut, t.Policy)
			continue
		}

		changed := st.UpsertDigest(key, digest, etagOut, t.Policy)
		if changed {
			log.Printf("[watcher] update: %s:%s -> digest=%s", repo, resolvedRef, digest)
			w.emitter.Emit(events.Event{
				Discovered: time.Now().UTC(),
				File:       t.Name,
				Repo:       repo,
				Ref:        resolvedRef, // <- the semver-resolved ref
				Digest:     digest,
				Policy:     t.Policy,
			})
		}
	}
}

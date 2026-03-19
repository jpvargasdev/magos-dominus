package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	pc "github.com/jpvargasdev/magos-dominus/internal/policy"
	"golang.org/x/time/rate"
)

type GHCR struct {
	client  *http.Client
	itr     *ghinstallation.Transport // nil = anonymous tokens only
	mu      sync.Mutex
	tokens  map[string]string
	limiter *rate.Limiter
}

// NewGHCR creates a new GHCR client with rate limiting.
// When itr is non-nil, authenticated tokens are used for private packages.
// Default: 10 requests per second with burst of 5.
func NewGHCR(itr *ghinstallation.Transport) *GHCR {
	return &GHCR{
		client:  http.DefaultClient,
		itr:     itr,
		tokens:  make(map[string]string),
		limiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 5), // 10 req/sec, burst 5
	}
}

func (g *GHCR) HeadDigest(ctx context.Context, repo, ref, etag, policy string) (string, string, string, bool, error) {
	repo = strings.ToLower(repo)
	candidate := ref

	// 1) Policy stage: resolve ref if semver
	if strings.EqualFold(policy, "semver") {
		tags, err := g.ListTags(ctx, repo)
		if err != nil {
			return "", "", "", false, fmt.Errorf("list tags: %w", err)
		}
		latest, err := pc.ResolveSemver(tags)
		if err != nil {
			return "", "", "", false, fmt.Errorf("resolve semver: %w", err)
		}
		candidate = latest
	}

	// 2) Registry stage: fetch manifest headers for candidate
	digest, etagOut, notMod, err := g.getManifestDigest(ctx, repo, candidate, etag)
	if err != nil {
		return "", "", "", false, err
	}
	return digest, candidate, etagOut, notMod, nil
}

func (g *GHCR) getManifestDigest(ctx context.Context, repo, ref, etag string) (string, string, bool, error) {
	// Rate limit to avoid hitting GHCR limits
	if err := g.limiter.Wait(ctx); err != nil {
		return "", "", false, fmt.Errorf("rate limiter: %w", err)
	}

	token, err := g.tokenFor(ctx, repo)
	if err != nil {
		return "", "", false, fmt.Errorf("token: %w", err)
	}

	url := fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repo, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", "", false, fmt.Errorf("new request: %w", err)
	}

	// Public images work without auth, but Bearer is fine either way.
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.oci.image.manifest.v1+json",
	}, ", "))
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified: // 304
		return "", etag, true, nil

	case http.StatusOK: // 200
		digest := resp.Header.Get("Docker-Content-Digest")
		etagOut := resp.Header.Get("Etag")
		if digest == "" && etagOut == "" {
			return "", "", false, fmt.Errorf("no digest/etag in response headers")
		}
		return digest, etagOut, false, nil

	case http.StatusUnauthorized: // 401
		// Likely expired token; drop cached token so next call refreshes.
		g.dropToken(repo)
		return "", "", false, fmt.Errorf("unauthorized")

	case http.StatusNotFound: // 404
		return "", "", false, os.ErrNotExist

	default:
		return "", "", false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}

func (g *GHCR) ListTags(ctx context.Context, repo string) ([]string, error) {
	// Rate limit to avoid hitting GHCR limits
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	token, err := g.tokenFor(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}

	url := fmt.Sprintf("https://ghcr.io/v2/%s/tags/list", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return result.Tags, nil
}

func (g *GHCR) tokenFor(ctx context.Context, repo string) (string, error) {
	g.mu.Lock()
	if tok, ok := g.tokens[repo]; ok && tok != "" {
		g.mu.Unlock()
		return tok, nil
	}
	g.mu.Unlock()

	// Use GitHub App installation token for authenticated access to private packages.
	// Falls back to anonymous token for public packages.
	if g.itr != nil {
		tok, err := g.itr.Token(ctx)
		if err != nil {
			return "", fmt.Errorf("github app token: %w", err)
		}
		g.mu.Lock()
		g.tokens[repo] = tok
		g.mu.Unlock()
		return tok, nil
	}

	// Anonymous pull token (public packages only)
	url := fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.Token == "" {
		return "", fmt.Errorf("empty token from GHCR")
	}

	g.mu.Lock()
	g.tokens[repo] = payload.Token
	g.mu.Unlock()
	return payload.Token, nil
}

func (g *GHCR) dropToken(repo string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// GitHub App tokens are shared across repos; clear all on expiry.
	if g.itr != nil {
		g.tokens = make(map[string]string)
		return
	}
	delete(g.tokens, repo)
}

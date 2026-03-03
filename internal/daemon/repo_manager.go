package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jpvargasdev/magos-dominus/internal/config"
	"github.com/jpvargasdev/magos-dominus/internal/github"
	"github.com/jpvargasdev/magos-dominus/internal/watcher"
)

type RepoManager struct {
	CleanURL string
	Path     string
}

type MagosAnnotation struct {
	File   string
	Line   int
	Image  string
	Policy string
}

func NewRepoManager() (*RepoManager, error) {
	gh, err := config.GetGithubConfig() // MD_REPO is "<owner>/<repo>"
	if err != nil {
		return nil, fmt.Errorf("get github config: %w", err)
	}
	clean := fmt.Sprintf("https://github.com/%s.git", gh.RepoURL)
	repoPath := filepath.Join(os.TempDir(), "git")

	return &RepoManager{
		CleanURL: clean,
		Path:     repoPath,
	}, nil
}

func (r *RepoManager) Sync() error {
	ghCfg, err := config.GetGithubConfig()
	if err != nil {
		return fmt.Errorf("get github config: %w", err)
	}
	gh := github.New(ghCfg.AppId, ghCfg.InstallationId, ghCfg.PrivateKeyPath, ghCfg.RepoURL)
	return gh.CloneOrPull(r.Path)
}

func (r *RepoManager) SyncFresh() error {
	// blow away any previous checkout
	if err := os.RemoveAll(r.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove repo dir: %w", err)
	}
	// now do a normal sync (will clone)
	return r.Sync()
}

func (r *RepoManager) ParseMagosAnnotations() ([]MagosAnnotation, error) {
	var out []MagosAnnotation

	err := filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		ln := 0
		for sc.Scan() {
			ln++
			line := sc.Text()
			if !strings.Contains(line, "image:") || !strings.Contains(line, `{"magos":`) {
				continue
			}

			left, right, ok := strings.Cut(line, "#")
			if !ok {
				continue
			}

			img := strings.TrimSpace(left)
			if idx := strings.Index(img, "image:"); idx >= 0 {
				img = strings.TrimSpace(img[idx+len("image:"):])
			}
			if img == "" {
				continue
			}

			raw := strings.TrimSpace(right)
			jsonStart := strings.Index(raw, "{")
			if jsonStart < 0 {
				continue
			}
			raw = raw[jsonStart:]

			var payload struct {
				Magos struct {
					Policy string `json:"policy"`
					Note   string `json:"note"`
				} `json:"magos"`
			}
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				continue
			}
			policy := strings.TrimSpace(payload.Magos.Policy)
			if policy == "" {
				policy = "manual"
			}

			out = append(out, MagosAnnotation{
				File:   path,
				Line:   ln,
				Image:  img,
				Policy: policy,
			})
		}
		return sc.Err()
	})

	return out, err
}

func (r *RepoManager) BuildReconcilePaths(annos []MagosAnnotation) []watcher.Target {
	var out []watcher.Target
	seen := map[string]struct{}{}

	for _, a := range annos {
		dir := filepath.Dir(a.File)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}

		registry, owner, name, tag := splitImageRef(a.Image)
		out = append(out, watcher.Target{
			Name: a.File,
			Image: watcher.ImageRef{
				Registry: registry,
				Owner:    owner,
				Name:     name,
				Tag:      tag,
			},
			Policy:   a.Policy,
			Interval: 0,
		})
	}

	return out
}

func (r *RepoManager) BuildTargets(annos []MagosAnnotation) []watcher.Target {
	var targets []watcher.Target
	for _, a := range annos {
		if a.Policy == "manual" {
			continue
		}
		registry, owner, name, tag := splitImageRef(a.Image)
		targets = append(targets, watcher.Target{
			Name: a.File,
			Image: watcher.ImageRef{
				Registry: registry,
				Owner:    owner,
				Name:     name,
				Tag:      tag,
			},
			Policy:   a.Policy,
			Interval: 0,
		})
	}
	return targets
}

func splitImageRef(img string) (string, string, string, string) {
	// supports something like ghcr.io/repo/app:0.0.1
	parts := strings.SplitN(img, "/", 3)
	if len(parts) < 3 {
		return "", "", "", ""
	}
	registry, owner, rest := parts[0], parts[1], parts[2]
	nameTag := strings.SplitN(rest, ":", 2)
	name, tag := nameTag[0], "latest"
	if len(nameTag) == 2 {
		tag = nameTag[1]
	}
	return registry, owner, name, tag
}

func (r *RepoManager) CommitAndPush(absPath string, preferPR bool) error {
	ctx := context.Background()
	ghCfg, err := config.GetGithubConfig()
	if err != nil {
		return fmt.Errorf("get github config: %w", err)
	}
	gh := github.New(ghCfg.AppId, ghCfg.InstallationId, ghCfg.PrivateKeyPath, ghCfg.RepoURL)

	// 1) convertir /tmp/git/... -> stacks/lexcodex/lexcodex-compose.yml
	relPath, err := filepath.Rel(r.Path, absPath)
	if err != nil {
		return fmt.Errorf("make relative: %w", err)
	}
	relPath = filepath.ToSlash(relPath)         // GitHub espera forward slashes
	relPath = strings.TrimPrefix(relPath, "/")  // paranoia
	relPath = strings.TrimPrefix(relPath, "./") // más paranoia

	// 2) leer contenido modificado
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read updated file: %w", err)
	}

	// 3) rama destino
	branch := "main"
	if preferPR {
		branch = fmt.Sprintf("magos/auto-%d", time.Now().Unix())
	}

	// 4) commit firmado por la App (vía API)
	msg := fmt.Sprintf("magos: update %s", relPath)
	if _, err := gh.UpdateFileSigned(ctx, relPath, branch, msg, content); err != nil {
		return fmt.Errorf("update file via API: %w", err)
	}

	// 5) si preferís PR, abrilo aquí (ya empujaste la rama con UpdateFile)
	if preferPR {
		// title := msg
		// body := "Automated update from MagosDominus."
		// if _, err := gh.PushAsPR(ctx, r.Path, "main", branch, title, body); err != nil {
		// 	return fmt.Errorf("open PR: %w", err)
		// }
		log.Printf("[repo] pushed to PR")
	}

	return nil
}

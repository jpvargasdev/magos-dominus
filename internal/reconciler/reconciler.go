package reconciler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jpvargasdev/magos-dominus/internal/config"
	"github.com/jpvargasdev/magos-dominus/internal/watcher"
)

func RunReconcile(ctx context.Context, scriptPath, repoRoot, updatedFile, writeMode string) error {
	if scriptPath == "" {
		scriptPath = "./reconcile.sh"
	}

	// guard: script must exist and be executable
	if st, err := os.Stat(scriptPath); err != nil || (st.Mode()&0o111) == 0 {
		return fmt.Errorf("reconcile script missing or not executable: %s", scriptPath)
	}

	// bounded time - configurable via MD_RECONCILE_TIMEOUT
	timeout := config.GetReconcileTimeout()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, scriptPath, repoRoot, updatedFile, writeMode)
	cmd.Env = append(os.Environ(),
		os.Getenv("MD_RUNTIME"),
		// "MD_DRY_RUN=true",
	)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	err := cmd.Run()
	log.Printf("[reconcile] exit=%v output:\n%s", err, out.String())
	if cctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("reconcile timeout")
	}
	return err
}

func RunAll(ctx context.Context, scriptPath, repoRoot string, targets []watcher.Target) error {
	seen := map[string]bool{}
	for _, t := range targets {
		dir := filepath.Dir(t.Name)
		if seen[dir] {
			continue
		}
		seen[dir] = true

		log.Printf("[reconcile] applying folder %s (policy=%s)", dir, t.Policy)
		if err := RunReconcile(ctx, scriptPath, repoRoot, t.Name, t.Policy); err != nil {
			log.Printf("[reconcile] %s failed: %v", dir, err)
		}
	}
	return nil
}

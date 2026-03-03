package reconciler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jpvargasdev/magos-dominus/internal/watcher"
)

func TestRunReconcile_MissingScript(t *testing.T) {
	ctx := context.Background()
	err := RunReconcile(ctx, "/nonexistent/script.sh", "/repo", "file.yml", "latest")

	if err == nil {
		t.Fatal("expected error for missing script")
	}
}

func TestRunReconcile_DefaultScriptPath(t *testing.T) {
	ctx := context.Background()

	// This will fail because ./reconcile.sh doesn't exist in test dir
	err := RunReconcile(ctx, "", "/repo", "file.yml", "latest")

	if err == nil {
		t.Fatal("expected error for missing default script")
	}
}

func TestRunReconcile_ScriptNotExecutable(t *testing.T) {
	// Create a temp script that is NOT executable
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "reconcile.sh")

	err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hello"), 0o644) // not executable
	if err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	ctx := context.Background()
	err = RunReconcile(ctx, scriptPath, "/repo", "file.yml", "latest")

	if err == nil {
		t.Fatal("expected error for non-executable script")
	}
}

func TestRunReconcile_ExecutableScript(t *testing.T) {
	// Create a temp script that IS executable
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "reconcile.sh")

	err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hello"), 0o755) // executable
	if err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	ctx := context.Background()
	err = RunReconcile(ctx, scriptPath, tmpDir, "file.yml", "latest")

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunReconcile_ContextCancellation(t *testing.T) {
	// Create a slow script
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow.sh")

	err := os.WriteFile(scriptPath, []byte("#!/bin/bash\nsleep 10"), 0o755)
	if err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = RunReconcile(ctx, scriptPath, tmpDir, "file.yml", "latest")

	// Script should fail due to cancelled context
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRunReconcile_ScriptWithArgs(t *testing.T) {
	// Create a script that echoes its arguments
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "args.sh")
	outputFile := filepath.Join(tmpDir, "output.txt")

	script := `#!/bin/bash
echo "$1 $2 $3" > ` + outputFile

	err := os.WriteFile(scriptPath, []byte(script), 0o755)
	if err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	ctx := context.Background()
	err = RunReconcile(ctx, scriptPath, "/repo/root", "stacks/app/compose.yml", "semver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output
	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	expected := "/repo/root stacks/app/compose.yml semver\n"
	if string(output) != expected {
		t.Errorf("output = %q, want %q", string(output), expected)
	}
}

func TestRunAll_EmptyTargets(t *testing.T) {
	ctx := context.Background()
	targets := []watcher.Target{}

	err := RunAll(ctx, "/nonexistent.sh", "/repo", targets)

	// Should return nil for empty targets (no scripts to run)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunAll_DeduplicatesByDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "count.sh")
	countFile := filepath.Join(tmpDir, "count.txt")

	// Script that increments a counter
	script := `#!/bin/bash
if [ -f "` + countFile + `" ]; then
  count=$(cat "` + countFile + `")
  echo $((count + 1)) > "` + countFile + `"
else
  echo 1 > "` + countFile + `"
fi`

	err := os.WriteFile(scriptPath, []byte(script), 0o755)
	if err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	// Multiple targets in the SAME directory should only run script once
	targets := []watcher.Target{
		{Name: "stacks/app/compose.yml", Policy: "latest"},
		{Name: "stacks/app/service.yml", Policy: "semver"},
		{Name: "stacks/app/config.yml", Policy: "latest"},
	}

	ctx := context.Background()
	err = RunAll(ctx, scriptPath, tmpDir, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify count is 1 (not 3)
	countBytes, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("failed to read count: %v", err)
	}

	if string(countBytes) != "1\n" {
		t.Errorf("count = %q, want 1 (script ran multiple times)", string(countBytes))
	}
}

func TestRunAll_DifferentDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "count.sh")
	countFile := filepath.Join(tmpDir, "count.txt")

	// Script that increments a counter
	script := `#!/bin/bash
if [ -f "` + countFile + `" ]; then
  count=$(cat "` + countFile + `")
  echo $((count + 1)) > "` + countFile + `"
else
  echo 1 > "` + countFile + `"
fi`

	err := os.WriteFile(scriptPath, []byte(script), 0o755)
	if err != nil {
		t.Fatalf("failed to create test script: %v", err)
	}

	// Targets in DIFFERENT directories should each run script
	targets := []watcher.Target{
		{Name: "stacks/app1/compose.yml", Policy: "latest"},
		{Name: "stacks/app2/compose.yml", Policy: "semver"},
		{Name: "stacks/app3/compose.yml", Policy: "latest"},
	}

	ctx := context.Background()
	err = RunAll(ctx, scriptPath, tmpDir, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify count is 3 (one per directory)
	countBytes, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("failed to read count: %v", err)
	}

	if string(countBytes) != "3\n" {
		t.Errorf("count = %q, want 3 (one per unique directory)", string(countBytes))
	}
}

package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommand(t *testing.T) {
	if rootCmd == nil {
		t.Fatal("rootCmd is nil")
	}

	if rootCmd.Use != "magos-dominus" {
		t.Errorf("Use = %s, want magos-dominus", rootCmd.Use)
	}

	if rootCmd.Short == "" {
		t.Error("Short description is empty")
	}

	if rootCmd.Long == "" {
		t.Error("Long description is empty")
	}
}

func TestVersionCommand(t *testing.T) {
	if versionCmd == nil {
		t.Fatal("versionCmd is nil")
	}

	if versionCmd.Use != "version" {
		t.Errorf("Use = %s, want version", versionCmd.Use)
	}
}

func TestRunCommand(t *testing.T) {
	if runCmd == nil {
		t.Fatal("runCmd is nil")
	}

	if runCmd.Use != "run" {
		t.Errorf("Use = %s, want run", runCmd.Use)
	}

	// Check dry-run flag exists
	flag := runCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Error("dry-run flag not found")
	}
}

func TestCompletionCommand(t *testing.T) {
	if completionCmd == nil {
		t.Fatal("completionCmd is nil")
	}

	if completionCmd.Use != "completion [bash|zsh|fish|powershell]" {
		t.Errorf("Use = %s, want completion [bash|zsh|fish|powershell]", completionCmd.Use)
	}
}

func TestCompletionCommand_Bash(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := completionCmd.RunE(completionCmd, []string{"bash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompletionCommand_Zsh(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := completionCmd.RunE(completionCmd, []string{"zsh"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompletionCommand_Fish(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := completionCmd.RunE(completionCmd, []string{"fish"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompletionCommand_Powershell(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)

	err := completionCmd.RunE(completionCmd, []string{"powershell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompletionCommand_UnsupportedShell(t *testing.T) {
	err := completionCmd.RunE(completionCmd, []string{"unsupported"})
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("error = %v, want 'unsupported shell'", err)
	}
}

func TestGlobalFlags(t *testing.T) {
	// Check config flag exists
	configFlag := rootCmd.PersistentFlags().Lookup("config")
	if configFlag == nil {
		t.Error("config flag not found")
	}
	if configFlag.DefValue != "configs/config.yaml" {
		t.Errorf("config default = %s, want configs/config.yaml", configFlag.DefValue)
	}

	// Check verbose flag exists
	verboseFlag := rootCmd.PersistentFlags().Lookup("verbose")
	if verboseFlag == nil {
		t.Error("verbose flag not found")
	}
	if verboseFlag.DefValue != "false" {
		t.Errorf("verbose default = %s, want false", verboseFlag.DefValue)
	}
}

func TestSubcommands(t *testing.T) {
	cmds := rootCmd.Commands()

	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Use] = true
	}

	if !names["run"] {
		t.Error("run subcommand not found")
	}
	if !names["version"] {
		t.Error("version subcommand not found")
	}
	if !names["completion [bash|zsh|fish|powershell]"] {
		t.Error("completion subcommand not found")
	}
}

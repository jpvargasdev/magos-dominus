package config

import (
	"os"
	"testing"
	"time"
)

func clearEnv() {
	os.Unsetenv("GH_APP_ID")
	os.Unsetenv("GH_INSTALLATION_ID")
	os.Unsetenv("MD_REPO")
	os.Unsetenv("GH_PRIVATE_KEY_PATH")
	os.Unsetenv("MD_PREFER_DIGEST")
	os.Unsetenv("MD_PREFER_PR")
	os.Unsetenv("MD_POLL_INTERVAL")
	os.Unsetenv("MD_RECONCILE_TIMEOUT")
}

func TestGetGitPreferences(t *testing.T) {
	clearEnv()
	defer clearEnv()

	t.Run("defaults to false", func(t *testing.T) {
		cfg, err := GetGitPreferences()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.PreferDigest {
			t.Error("PreferDigest should be false by default")
		}
		if cfg.PreferPR {
			t.Error("PreferPR should be false by default")
		}
	})

	t.Run("reads true values", func(t *testing.T) {
		os.Setenv("MD_PREFER_DIGEST", "true")
		os.Setenv("MD_PREFER_PR", "true")
		defer clearEnv()

		cfg, err := GetGitPreferences()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.PreferDigest {
			t.Error("PreferDigest should be true")
		}
		if !cfg.PreferPR {
			t.Error("PreferPR should be true")
		}
	})
}

func TestGetGithubConfig(t *testing.T) {
	clearEnv()
	defer clearEnv()

	t.Run("missing GH_APP_ID", func(t *testing.T) {
		_, err := GetGithubConfig()
		if err == nil {
			t.Fatal("expected error for missing GH_APP_ID")
		}
	})

	t.Run("invalid GH_APP_ID", func(t *testing.T) {
		os.Setenv("GH_APP_ID", "not-a-number")
		defer clearEnv()

		_, err := GetGithubConfig()
		if err == nil {
			t.Fatal("expected error for invalid GH_APP_ID")
		}
	})

	t.Run("missing GH_INSTALLATION_ID", func(t *testing.T) {
		os.Setenv("GH_APP_ID", "12345")
		defer clearEnv()

		_, err := GetGithubConfig()
		if err == nil {
			t.Fatal("expected error for missing GH_INSTALLATION_ID")
		}
	})

	t.Run("missing MD_REPO", func(t *testing.T) {
		os.Setenv("GH_APP_ID", "12345")
		os.Setenv("GH_INSTALLATION_ID", "67890")
		defer clearEnv()

		_, err := GetGithubConfig()
		if err == nil {
			t.Fatal("expected error for missing MD_REPO")
		}
	})

	t.Run("missing GH_PRIVATE_KEY_PATH", func(t *testing.T) {
		os.Setenv("GH_APP_ID", "12345")
		os.Setenv("GH_INSTALLATION_ID", "67890")
		os.Setenv("MD_REPO", "owner/repo")
		defer clearEnv()

		_, err := GetGithubConfig()
		if err == nil {
			t.Fatal("expected error for missing GH_PRIVATE_KEY_PATH")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		os.Setenv("GH_APP_ID", "12345")
		os.Setenv("GH_INSTALLATION_ID", "67890")
		os.Setenv("MD_REPO", "owner/repo")
		os.Setenv("GH_PRIVATE_KEY_PATH", "/path/to/key.pem")
		defer clearEnv()

		cfg, err := GetGithubConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AppId != 12345 {
			t.Errorf("AppId = %d, want 12345", cfg.AppId)
		}
		if cfg.InstallationId != 67890 {
			t.Errorf("InstallationId = %d, want 67890", cfg.InstallationId)
		}
		if cfg.RepoURL != "owner/repo" {
			t.Errorf("RepoURL = %s, want owner/repo", cfg.RepoURL)
		}
		if cfg.PrivateKeyPath != "/path/to/key.pem" {
			t.Errorf("PrivateKeyPath = %s, want /path/to/key.pem", cfg.PrivateKeyPath)
		}
	})
}

func TestGetPollInterval(t *testing.T) {
	clearEnv()
	defer clearEnv()

	t.Run("default value", func(t *testing.T) {
		d := GetPollInterval()
		if d != DefaultPollInterval {
			t.Errorf("GetPollInterval() = %v, want %v", d, DefaultPollInterval)
		}
	})

	t.Run("custom value", func(t *testing.T) {
		os.Setenv("MD_POLL_INTERVAL", "5m")
		defer clearEnv()

		d := GetPollInterval()
		if d != 5*time.Minute {
			t.Errorf("GetPollInterval() = %v, want 5m", d)
		}
	})

	t.Run("invalid value returns default", func(t *testing.T) {
		os.Setenv("MD_POLL_INTERVAL", "invalid")
		defer clearEnv()

		d := GetPollInterval()
		if d != DefaultPollInterval {
			t.Errorf("GetPollInterval() = %v, want %v", d, DefaultPollInterval)
		}
	})

	t.Run("minimum 10 seconds", func(t *testing.T) {
		os.Setenv("MD_POLL_INTERVAL", "1s")
		defer clearEnv()

		d := GetPollInterval()
		if d != 10*time.Second {
			t.Errorf("GetPollInterval() = %v, want 10s (minimum)", d)
		}
	})
}

func TestGetReconcileTimeout(t *testing.T) {
	clearEnv()
	defer clearEnv()

	t.Run("default value", func(t *testing.T) {
		d := GetReconcileTimeout()
		if d != DefaultReconcileTimeout {
			t.Errorf("GetReconcileTimeout() = %v, want %v", d, DefaultReconcileTimeout)
		}
	})

	t.Run("custom value", func(t *testing.T) {
		os.Setenv("MD_RECONCILE_TIMEOUT", "10m")
		defer clearEnv()

		d := GetReconcileTimeout()
		if d != 10*time.Minute {
			t.Errorf("GetReconcileTimeout() = %v, want 10m", d)
		}
	})

	t.Run("invalid value returns default", func(t *testing.T) {
		os.Setenv("MD_RECONCILE_TIMEOUT", "invalid")
		defer clearEnv()

		d := GetReconcileTimeout()
		if d != DefaultReconcileTimeout {
			t.Errorf("GetReconcileTimeout() = %v, want %v", d, DefaultReconcileTimeout)
		}
	})

	t.Run("minimum 5 seconds", func(t *testing.T) {
		os.Setenv("MD_RECONCILE_TIMEOUT", "1s")
		defer clearEnv()

		d := GetReconcileTimeout()
		if d != 5*time.Second {
			t.Errorf("GetReconcileTimeout() = %v, want 5s (minimum)", d)
		}
	})
}

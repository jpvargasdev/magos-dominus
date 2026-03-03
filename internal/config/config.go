package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultPollInterval     = 1 * time.Minute
	DefaultReconcileTimeout = 2 * time.Minute
)

type Config struct {
	RepoURL        string
	PreferDigest   bool
	PreferPR       bool
	AppId          int64
	InstallationId int64
	PrivateKeyPath string
}

// LoadEnv loads .env file if it exists. It's safe to call multiple times.
// Returns nil if .env doesn't exist (allows env vars to be set externally).
func LoadEnv() error {
	if err := godotenv.Load(); err != nil {
		// .env file is optional - env vars can be set externally (e.g., in k8s)
		if os.IsNotExist(err) {
			return nil
		}
		// Only return error if file exists but can't be parsed
		if _, statErr := os.Stat(".env"); statErr == nil {
			return fmt.Errorf("failed to parse .env file: %w", err)
		}
	}
	return nil
}

func GetGitPreferences() (*Config, error) {
	if err := LoadEnv(); err != nil {
		return nil, err
	}

	return &Config{
		PreferDigest: os.Getenv("MD_PREFER_DIGEST") == "true",
		PreferPR:     os.Getenv("MD_PREFER_PR") == "true",
	}, nil
}

func GetGithubConfig() (*Config, error) {
	if err := LoadEnv(); err != nil {
		return nil, err
	}

	appIdStr := os.Getenv("GH_APP_ID")
	if appIdStr == "" {
		return nil, fmt.Errorf("GH_APP_ID environment variable is required")
	}
	appId, err := strconv.ParseInt(appIdStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GH_APP_ID must be a valid integer: %w", err)
	}

	installationIdStr := os.Getenv("GH_INSTALLATION_ID")
	if installationIdStr == "" {
		return nil, fmt.Errorf("GH_INSTALLATION_ID environment variable is required")
	}
	installationId, err := strconv.ParseInt(installationIdStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GH_INSTALLATION_ID must be a valid integer: %w", err)
	}

	repoURL := os.Getenv("MD_REPO")
	if repoURL == "" {
		return nil, fmt.Errorf("MD_REPO environment variable is required")
	}

	privateKeyPath := os.Getenv("GH_PRIVATE_KEY_PATH")
	if privateKeyPath == "" {
		return nil, fmt.Errorf("GH_PRIVATE_KEY_PATH environment variable is required")
	}

	return &Config{
		RepoURL:        repoURL,
		AppId:          appId,
		InstallationId: installationId,
		PrivateKeyPath: privateKeyPath,
	}, nil
}

// GetPollInterval returns the watcher poll interval from MD_POLL_INTERVAL env var.
// Expects a duration string like "30s", "2m", "1h". Defaults to 1 minute.
func GetPollInterval() time.Duration {
	if err := LoadEnv(); err != nil {
		return DefaultPollInterval
	}

	val := os.Getenv("MD_POLL_INTERVAL")
	if val == "" {
		return DefaultPollInterval
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		return DefaultPollInterval
	}

	// Sanity check: minimum 10 seconds to avoid hammering registries
	if d < 10*time.Second {
		return 10 * time.Second
	}

	return d
}

// GetReconcileTimeout returns the reconcile script timeout from MD_RECONCILE_TIMEOUT env var.
// Expects a duration string like "30s", "5m". Defaults to 2 minutes.
func GetReconcileTimeout() time.Duration {
	if err := LoadEnv(); err != nil {
		return DefaultReconcileTimeout
	}

	val := os.Getenv("MD_RECONCILE_TIMEOUT")
	if val == "" {
		return DefaultReconcileTimeout
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		return DefaultReconcileTimeout
	}

	// Sanity check: minimum 5 seconds
	if d < 5*time.Second {
		return 5 * time.Second
	}

	return d
}

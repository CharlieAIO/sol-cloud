package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	credentialsFileName = "credentials.json"
)

type Credentials struct {
	Fly FlyCredentials `json:"fly"`
}

type FlyCredentials struct {
	AccessToken string    `json:"access_token"`
	OrgSlug     string    `json:"org_slug,omitempty"`
	VerifiedAt  time.Time `json:"verified_at,omitempty"`
}

func credentialsDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("SOL_CLOUD_CONFIG_DIR")); custom != "" {
		return filepath.Join(custom, "sol-cloud"), nil
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "sol-cloud"), nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "sol-cloud"), nil
}

func CredentialsFilePath() (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsFilePath()
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Credentials{}, nil
		}
		return nil, fmt.Errorf("read credentials file %s: %w", path, err)
	}
	if len(content) == 0 {
		return &Credentials{}, nil
	}

	var creds Credentials
	if err := json.Unmarshal(content, &creds); err != nil {
		return nil, fmt.Errorf("decode credentials file %s: %w", path, err)
	}
	creds.Fly.AccessToken = strings.TrimSpace(creds.Fly.AccessToken)
	creds.Fly.OrgSlug = strings.TrimSpace(creds.Fly.OrgSlug)
	return &creds, nil
}

func SaveCredentials(creds *Credentials) error {
	if creds == nil {
		return errors.New("credentials are required")
	}
	creds.Fly.AccessToken = strings.TrimSpace(creds.Fly.AccessToken)
	creds.Fly.OrgSlug = strings.TrimSpace(creds.Fly.OrgSlug)

	path, err := CredentialsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}

	payload, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	payload = append(payload, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return fmt.Errorf("write temp credentials file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp credentials file: %w", err)
	}
	return nil
}

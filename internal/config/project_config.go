package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const projectConfigDirName = "projects"

// ProjectConfigPath returns the hidden per-project config file path for a
// project directory. The path is stable across commands and does not live in
// the project worktree.
func ProjectConfigPath(projectDir string) (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	abs = filepath.Clean(abs)
	hash := sha256.Sum256([]byte(abs))
	name := projectConfigSlug(filepath.Base(abs)) + "-" + hex.EncodeToString(hash[:])[:12] + ".yml"
	return filepath.Join(dir, projectConfigDirName, name), nil
}

// LegacyProjectConfigPath returns the old local config path. It is only used as
// a read-only compatibility fallback.
func LegacyProjectConfigPath(projectDir string) string {
	if strings.TrimSpace(projectDir) == "" {
		projectDir = "."
	}
	return filepath.Join(projectDir, ".sol-cloud.yml")
}

func projectConfigSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "project"
	}
	return slug
}

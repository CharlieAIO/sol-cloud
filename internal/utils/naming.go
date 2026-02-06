package utils

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

const (
	flyAppPrefix       = "sol-cloud-"
	flyAppRandomLength = 8
)

var flyAppNamePattern = regexp.MustCompile(`^sol-cloud-[a-z0-9]{8}$`)

func EnsureFlyAppName(name string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" {
		return GenerateFlyAppName()
	}
	if !flyAppNamePattern.MatchString(clean) {
		return "", fmt.Errorf("app name %q must match %q (example: sol-cloud-1a2b3c4d)", clean, "sol-cloud-<8-char-id>")
	}
	return clean, nil
}

func GenerateFlyAppName() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	bytes := make([]byte, flyAppRandomLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate app name: %w", err)
	}

	suffix := make([]byte, flyAppRandomLength)
	for i, b := range bytes {
		suffix[i] = alphabet[int(b)%len(alphabet)]
	}
	return flyAppPrefix + string(suffix), nil
}

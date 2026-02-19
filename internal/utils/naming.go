package utils

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

const (
	flyAppPrefix          = "sol-cloud-"
	flyAppRandomLength    = 8
	railwayProjectPrefix  = "sol-cloud-"
	railwayProjectRandLen = 8
)

var flyAppNamePattern = regexp.MustCompile(`^sol-cloud-[a-z0-9]{8}$`)
var railwayProjectNamePattern = regexp.MustCompile(`^sol-cloud-[a-z0-9]{8}$`)

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

func EnsureRailwayProjectName(name string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" {
		return GenerateRailwayProjectName()
	}
	if !railwayProjectNamePattern.MatchString(clean) {
		return "", fmt.Errorf("project name %q must match %q (example: sol-cloud-1a2b3c4d)", clean, "sol-cloud-<8-char-id>")
	}
	return clean, nil
}

func GenerateRailwayProjectName() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	b := make([]byte, railwayProjectRandLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate project name: %w", err)
	}

	suffix := make([]byte, railwayProjectRandLen)
	for i, byt := range b {
		suffix[i] = alphabet[int(byt)%len(alphabet)]
	}
	return railwayProjectPrefix + string(suffix), nil
}

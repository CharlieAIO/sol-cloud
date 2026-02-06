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
	stateDirName  = ".sol-cloud"
	stateFileName = "state.json"
)

var (
	ErrNoDeployments      = errors.New("no deployments found in local state")
	ErrDeploymentNotFound = errors.New("deployment not found in local state")
)

// DeploymentRecord tracks a deployed validator instance.
type DeploymentRecord struct {
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	RPCURL       string    `json:"rpc_url"`
	WebSocketURL string    `json:"websocket_url"`
	Region       string    `json:"region"`
	ArtifactsDir string    `json:"artifacts_dir"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// State stores known deployments for local commands like status/destroy.
type State struct {
	LastDeployment string                      `json:"last_deployment"`
	Deployments    map[string]DeploymentRecord `json:"deployments"`
}

// StateFilePath returns the local state file path for a project.
func StateFilePath(projectDir string) string {
	return filepath.Join(projectDir, stateDirName, stateFileName)
}

// LoadState reads local deployment state. Missing files return an empty state.
func LoadState(projectDir string) (*State, error) {
	path := StateFilePath(projectDir)
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyState(), nil
		}
		return nil, fmt.Errorf("read state file %s: %w", path, err)
	}
	if len(content) == 0 {
		return emptyState(), nil
	}

	var state State
	if err := json.Unmarshal(content, &state); err != nil {
		return nil, fmt.Errorf("decode state file %s: %w", path, err)
	}
	if state.Deployments == nil {
		state.Deployments = make(map[string]DeploymentRecord)
	}
	return &state, nil
}

// SaveState persists local deployment state to disk.
func SaveState(projectDir string, state *State) error {
	if state == nil {
		return errors.New("state is required")
	}
	if state.Deployments == nil {
		state.Deployments = make(map[string]DeploymentRecord)
	}

	path := StateFilePath(projectDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	payload = append(payload, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp state file: %w", err)
	}
	return nil
}

// UpsertDeployment inserts or updates a deployment record.
func (s *State) UpsertDeployment(record DeploymentRecord) error {
	if s == nil {
		return errors.New("state is required")
	}
	name := strings.TrimSpace(record.Name)
	if name == "" {
		return errors.New("deployment name is required")
	}
	if s.Deployments == nil {
		s.Deployments = make(map[string]DeploymentRecord)
	}

	now := time.Now().UTC()
	existing, ok := s.Deployments[name]
	if ok && !existing.CreatedAt.IsZero() {
		record.CreatedAt = existing.CreatedAt
	} else if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	s.Deployments[name] = record
	s.LastDeployment = name
	return nil
}

// RemoveDeployment deletes a deployment record if present.
func (s *State) RemoveDeployment(name string) {
	if s == nil || s.Deployments == nil {
		return
	}
	delete(s.Deployments, strings.TrimSpace(name))
	if s.LastDeployment == strings.TrimSpace(name) {
		s.LastDeployment = ""
		for deploymentName := range s.Deployments {
			s.LastDeployment = deploymentName
			break
		}
	}
}

// ResolveDeployment returns a deployment by name, or the latest deployment if name is empty.
func (s *State) ResolveDeployment(name string) (DeploymentRecord, error) {
	if s == nil || len(s.Deployments) == 0 {
		return DeploymentRecord{}, ErrNoDeployments
	}

	target := strings.TrimSpace(name)
	if target == "" {
		target = strings.TrimSpace(s.LastDeployment)
		if target == "" && len(s.Deployments) == 1 {
			for only := range s.Deployments {
				target = only
				break
			}
		}
		if target == "" {
			return DeploymentRecord{}, errors.New("deployment name is required (use --name)")
		}
	}

	record, ok := s.Deployments[target]
	if !ok {
		return DeploymentRecord{}, ErrDeploymentNotFound
	}
	return record, nil
}

func emptyState() *State {
	return &State{
		Deployments: make(map[string]DeploymentRecord),
	}
}

package providers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CharlieAIO/sol-cloud/internal/validator"
)

// Config captures provider-agnostic deployment inputs.
type Config struct {
	Name                string
	OrgSlug             string
	Region              string
	ProjectDir          string
	Validator           validator.Config
	DryRun              bool
	SkipHealthCheck     bool
	HealthCheckTimeout  time.Duration
	HealthCheckInterval time.Duration
	VolumeSize          int  // Volume size in GB, default 10
	SkipVolume          bool // Skip volume creation (ephemeral storage)
}

// Deployment includes canonical endpoints for a deployed validator.
type Deployment struct {
	Name         string
	RPCURL       string
	WebSocketURL string
	Provider     string
	ArtifactsDir string
	DashboardURL string
}

// Status represents high-level health for a validator deployment.
type Status struct {
	Name   string
	State  string
	Slot   uint64
	TPS    float64
	Uptime string
}

// Provider defines deployment lifecycle operations.
type Provider interface {
	Deploy(ctx context.Context, cfg *Config) (*Deployment, error)
	Destroy(ctx context.Context, name string) error
	Status(ctx context.Context, name string) (*Status, error)
	Restart(ctx context.Context, name string) error
}

// airdropEntryTemplateData holds a single airdrop recipient for use in templates.
type airdropEntryTemplateData struct {
	Address string
	Amount  uint64
}

// validatorTemplateData holds validator config fields used in templates.
type validatorTemplateData struct {
	SlotsPerEpoch    uint64
	TicksPerSlot     uint64
	ComputeUnitLimit uint64
	LedgerLimitSize  uint64
	// ClonePrograms is the unified list; the entrypoint auto-detects upgradeable vs plain.
	ClonePrograms []string
	// Legacy fields kept for backwards compatibility.
	CloneAccounts            []string
	CloneUpgradeablePrograms []string
	AirdropAccounts          []airdropEntryTemplateData
	ProgramDeploy            programDeployTemplateData
}

// programDeployTemplateData holds startup program deploy config for templates.
type programDeployTemplateData struct {
	Enabled              bool
	SOPath               string
	ProgramIDKeypairPath string
	UpgradeAuthorityPath string
}

// toAirdropTemplateData converts validator.AirdropEntry slice to template data.
func toAirdropTemplateData(entries []validator.AirdropEntry) []airdropEntryTemplateData {
	out := make([]airdropEntryTemplateData, len(entries))
	for i, e := range entries {
		out[i] = airdropEntryTemplateData{Address: e.Address, Amount: e.Amount}
	}
	return out
}

// NewProvider returns a Provider for the given provider name.
func NewProvider(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "fly":
		return NewFlyProvider(), nil
	case "railway":
		return NewRailwayProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q: valid providers are fly, railway", name)
	}
}

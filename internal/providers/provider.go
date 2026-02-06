package providers

import (
	"context"
	"time"

	"github.com/CharlieAIO/sol-cloud/internal/validator"
)

// Config captures provider-agnostic deployment inputs.
type Config struct {
	Name                string
	Region              string
	ProjectDir          string
	Validator           validator.Config
	DryRun              bool
	SkipHealthCheck     bool
	HealthCheckTimeout  time.Duration
	HealthCheckInterval time.Duration
}

// Deployment includes canonical endpoints for a deployed validator.
type Deployment struct {
	Name         string
	RPCURL       string
	WebSocketURL string
	Provider     string
	ArtifactsDir string
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
}

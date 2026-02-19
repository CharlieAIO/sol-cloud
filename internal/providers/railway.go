package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
)

const (
	defaultRailwayGraphQLURL  = "https://backboard.railway.app/graphql/v2"
	defaultRailwayHTTPTimeout = 30 * time.Second
)

// railwayDeploymentIDs holds Railway resource identifiers persisted after deploy.
type railwayDeploymentIDs struct {
	ProjectID string `json:"project_id"`
	ServiceID string `json:"service_id"`
	Domain    string `json:"domain"`
}

// railwayTemplateData is the template data passed to provider-agnostic templates for Railway.
type railwayTemplateData struct {
	Validator validatorTemplateData
}

// RailwayProvider manages validator deployments on Railway.
type RailwayProvider struct {
	AccessToken string
	HTTPClient  *http.Client
	GraphQLURL  string
}

func NewRailwayProvider() *RailwayProvider {
	return &RailwayProvider{
		GraphQLURL: defaultRailwayGraphQLURL,
	}
}

func (p *RailwayProvider) Deploy(ctx context.Context, cfg *Config) (*Deployment, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("deployment name is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, errors.New("region is required")
	}

	projectDir := cfg.ProjectDir
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		projectDir = wd
	}
	artifactsDir := filepath.Join(projectDir, ".sol-cloud", "deployments", cfg.Name)
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifacts directory: %w", err)
	}
	programDir := filepath.Join(artifactsDir, "program")
	if err := os.MkdirAll(programDir, 0o755); err != nil {
		return nil, fmt.Errorf("create program artifacts directory: %w", err)
	}

	programDeployData, err := prepareProgramDeployData(projectDir, programDir, cfg)
	if err != nil {
		return nil, err
	}

	data := railwayTemplateData{
		Validator: validatorTemplateData{
			SlotsPerEpoch:            cfg.Validator.SlotsPerEpoch,
			TicksPerSlot:             cfg.Validator.TicksPerSlot,
			ComputeUnitLimit:         cfg.Validator.ComputeUnitLimit,
			LedgerLimitSize:          cfg.Validator.LedgerLimitSize,
			CloneAccounts:            append([]string(nil), cfg.Validator.CloneAccounts...),
			CloneUpgradeablePrograms: append([]string(nil), cfg.Validator.CloneUpgradeablePrograms...),
			ProgramDeploy:            programDeployData,
		},
	}

	// Railway does not need fly.toml — it auto-detects the Dockerfile.
	templateTargets := []struct {
		Name string
		Dst  string
	}{
		{Name: "Dockerfile.tmpl", Dst: filepath.Join(artifactsDir, "Dockerfile")},
		{Name: "nginx.conf.tmpl", Dst: filepath.Join(artifactsDir, "nginx.conf")},
		{Name: "entrypoint.sh.tmpl", Dst: filepath.Join(artifactsDir, "entrypoint.sh")},
	}
	for _, target := range templateTargets {
		if err := renderEmbeddedTemplateFile(target.Name, target.Dst, data); err != nil {
			return nil, err
		}
	}

	deployment := &Deployment{
		Name:         cfg.Name,
		Provider:     "railway",
		ArtifactsDir: artifactsDir,
	}

	if cfg.DryRun {
		return deployment, nil
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return nil, fmt.Errorf("railway auth required: run `sol-cloud auth railway`: %w", err)
	}

	logs, domain, projectID, serviceID, deployErr := deployViaRailwayCLI(ctx, token, cfg, artifactsDir)
	logPath := filepath.Join(artifactsDir, "deploy.log")
	if strings.TrimSpace(logs) != "" {
		if writeErr := os.WriteFile(logPath, []byte(logs), 0o644); writeErr != nil {
			if deployErr != nil {
				return nil, fmt.Errorf("%w (also failed to write deploy log: %v)", deployErr, writeErr)
			}
			return nil, fmt.Errorf("write deploy log: %w", writeErr)
		}
	}
	if deployErr != nil {
		if strings.TrimSpace(logs) != "" {
			return nil, fmt.Errorf("%w\nsee deploy log: %s", deployErr, logPath)
		}
		return nil, deployErr
	}

	// Persist Railway IDs for future destroy/status/restart operations.
	ids := railwayDeploymentIDs{
		ProjectID: projectID,
		ServiceID: serviceID,
		Domain:    domain,
	}
	idsJSON, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal railway ids: %w", err)
	}
	idsPath := filepath.Join(artifactsDir, "railway-ids.json")
	if err := os.WriteFile(idsPath, append(idsJSON, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("write railway-ids.json: %w", err)
	}

	if domain != "" {
		deployment.RPCURL = "https://" + domain
		deployment.WebSocketURL = "wss://" + domain
		deployment.DashboardURL = fmt.Sprintf("https://railway.app/project/%s", projectID)
	}

	if !cfg.SkipHealthCheck && deployment.RPCURL != "" {
		timeout := cfg.HealthCheckTimeout
		if timeout <= 0 {
			timeout = defaultHealthCheck
		}
		interval := cfg.HealthCheckInterval
		if interval <= 0 {
			interval = defaultPollInterval
		}
		if err := waitForRPCHealthy(ctx, deployment.RPCURL, timeout, interval); err != nil {
			return nil, fmt.Errorf("deployment completed but RPC health check failed: %w", err)
		}
	}

	return deployment, nil
}

func (p *RailwayProvider) Destroy(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("deployment name is required")
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return fmt.Errorf("railway auth required: run `sol-cloud auth railway`: %w", err)
	}

	ids, err := p.loadIDs(name)
	if err != nil {
		return fmt.Errorf("load railway ids: %w", err)
	}

	if err := destroyRailwayProject(ctx, p.httpClient(), p.graphqlURL(), token, ids.ProjectID); err != nil {
		return fmt.Errorf("destroy railway project: %w", err)
	}
	return nil
}

func (p *RailwayProvider) Status(ctx context.Context, name string) (*Status, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("deployment name is required")
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return nil, fmt.Errorf("railway auth required: run `sol-cloud auth railway`: %w", err)
	}

	ids, err := p.loadIDs(name)
	if err != nil {
		return nil, fmt.Errorf("load railway ids: %w", err)
	}

	state, err := getRailwayServiceStatus(ctx, p.httpClient(), p.graphqlURL(), token, ids.ProjectID, ids.ServiceID)
	if err != nil {
		return nil, err
	}
	return &Status{Name: name, State: state}, nil
}

func (p *RailwayProvider) Restart(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("deployment name is required")
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return fmt.Errorf("railway auth required: run `sol-cloud auth railway`: %w", err)
	}

	ids, err := p.loadIDs(name)
	if err != nil {
		return fmt.Errorf("load railway ids: %w", err)
	}

	if err := restartRailwayService(ctx, p.httpClient(), p.graphqlURL(), token, ids.ProjectID, ids.ServiceID); err != nil {
		return fmt.Errorf("restart railway service: %w", err)
	}
	return nil
}

// ListWorkspaces returns all Railway workspaces accessible with the given token.
func (p *RailwayProvider) ListWorkspaces(ctx context.Context, token string) ([]RailwayWorkspace, error) {
	return ListRailwayWorkspaces(ctx, p.httpClient(), p.graphqlURL(), token)
}

// VerifyAccessToken checks that the Railway token is valid.
// It first tries `me { id }` (works for personal API tokens), then falls back to
// listing projects (works for project/team-scoped tokens that lack `me` access).
func (p *RailwayProvider) VerifyAccessToken(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("railway access token is required")
	}

	resp, err := railwayGraphQLRequest(ctx, p.httpClient(), p.graphqlURL(), token, `query { me { id } }`, nil)
	if err != nil {
		return fmt.Errorf("railway token verification failed: %w", err)
	}
	if len(resp.Errors) == 0 {
		return nil
	}

	// `me` is not accessible for all token types (e.g. project tokens).
	// Fall back to listing projects — any valid token should be able to do this.
	fallback, fallbackErr := railwayGraphQLRequest(ctx, p.httpClient(), p.graphqlURL(), token,
		`query { projects { edges { node { id } } } }`, nil)
	if fallbackErr != nil {
		// Return the original me error, which is more informative.
		return fmt.Errorf("railway token rejected: %s", resp.Errors[0].Message)
	}
	if len(fallback.Errors) > 0 {
		return fmt.Errorf("railway token rejected: %s", fallback.Errors[0].Message)
	}
	return nil
}

func (p *RailwayProvider) resolveAccessToken() (string, error) {
	if token := strings.TrimSpace(p.AccessToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("SOL_CLOUD_RAILWAY_TOKEN")); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("RAILWAY_TOKEN")); token != "" {
		return token, nil
	}

	creds, err := appconfig.LoadCredentials()
	if err != nil {
		return "", fmt.Errorf("load credentials: %w", err)
	}
	if token := strings.TrimSpace(creds.Railway.AccessToken); token != "" {
		return token, nil
	}
	return "", errors.New("no railway access token configured")
}

func (p *RailwayProvider) loadIDs(name string) (*railwayDeploymentIDs, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	idsPath := filepath.Join(wd, ".sol-cloud", "deployments", name, "railway-ids.json")
	data, err := os.ReadFile(idsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", idsPath, err)
	}
	var ids railwayDeploymentIDs
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("decode railway-ids.json: %w", err)
	}
	return &ids, nil
}

func (p *RailwayProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: defaultRailwayHTTPTimeout}
}

func (p *RailwayProvider) graphqlURL() string {
	if u := strings.TrimSpace(p.GraphQLURL); u != "" {
		return u
	}
	return defaultRailwayGraphQLURL
}

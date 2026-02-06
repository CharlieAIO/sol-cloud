package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	tmplassets "github.com/CharlieAIO/sol-cloud/templates"
)

const (
	defaultHealthCheck    = 3 * time.Minute
	defaultPollInterval   = 5 * time.Second
	defaultFlyMachinesAPI = "https://api.machines.dev/v1"
	defaultFlyGraphQLURL  = "https://api.fly.io/graphql"
	defaultHTTPTimeout    = 30 * time.Second
	flyAuthProbeAppName   = "sol-cloud-auth-probe"
)

var flyDomainPattern = regexp.MustCompile(`([a-zA-Z0-9-]+\.fly\.dev)`)
var rpcHealthCheckFn = checkRPCHealth

// FlyProvider manages validator deployments on Fly.io.
type FlyProvider struct {
	AccessToken        string
	HTTPClient         *http.Client
	MachinesAPIBaseURL string
	GraphQLURL         string
}

func NewFlyProvider() *FlyProvider {
	return &FlyProvider{
		MachinesAPIBaseURL: defaultFlyMachinesAPI,
		GraphQLURL:         defaultFlyGraphQLURL,
	}
}

type flyTemplateData struct {
	Name      string
	Region    string
	Validator flyValidatorTemplateData
}

type flyValidatorTemplateData struct {
	SlotsPerEpoch            uint64
	TicksPerSlot             uint64
	ComputeUnitLimit         uint64
	LedgerLimitSize          uint64
	CloneAccounts            []string
	CloneUpgradeablePrograms []string
	ProgramDeploy            flyProgramDeployTemplateData
}

type flyProgramDeployTemplateData struct {
	Enabled              bool
	SOPath               string
	ProgramIDKeypairPath string
	UpgradeAuthorityPath string
}

func (p *FlyProvider) Deploy(ctx context.Context, cfg *Config) (*Deployment, error) {
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

	data := flyTemplateData{
		Name:   cfg.Name,
		Region: cfg.Region,
		Validator: flyValidatorTemplateData{
			SlotsPerEpoch:            cfg.Validator.SlotsPerEpoch,
			TicksPerSlot:             cfg.Validator.TicksPerSlot,
			ComputeUnitLimit:         cfg.Validator.ComputeUnitLimit,
			LedgerLimitSize:          cfg.Validator.LedgerLimitSize,
			CloneAccounts:            append([]string(nil), cfg.Validator.CloneAccounts...),
			CloneUpgradeablePrograms: append([]string(nil), cfg.Validator.CloneUpgradeablePrograms...),
			ProgramDeploy:            programDeployData,
		},
	}

	templateTargets := []struct {
		Name string
		Dst  string
	}{
		{Name: "Dockerfile.tmpl", Dst: filepath.Join(artifactsDir, "Dockerfile")},
		{Name: "fly.toml.tmpl", Dst: filepath.Join(artifactsDir, "fly.toml")},
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
		RPCURL:       fmt.Sprintf("https://%s.fly.dev", cfg.Name),
		WebSocketURL: fmt.Sprintf("wss://%s.fly.dev", cfg.Name),
		Provider:     "fly",
		ArtifactsDir: artifactsDir,
	}

	if cfg.DryRun {
		return deployment, nil
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return nil, fmt.Errorf("fly auth required: run `sol-cloud auth fly`: %w", err)
	}

	deployOutput, host, err := p.deployViaAPI(ctx, token, cfg, artifactsDir)
	logPath := filepath.Join(artifactsDir, "deploy.log")
	if strings.TrimSpace(deployOutput) != "" {
		if writeErr := os.WriteFile(logPath, []byte(deployOutput), 0o644); writeErr != nil {
			if err != nil {
				return nil, fmt.Errorf("%w (also failed to write deploy log: %v)", err, writeErr)
			}
			return nil, fmt.Errorf("write deploy log: %w", writeErr)
		}
	}
	if err != nil {
		if strings.TrimSpace(deployOutput) != "" {
			return nil, fmt.Errorf("%w\nsee deploy log: %s", err, logPath)
		}
		return nil, err
	}
	if host == "" {
		host = fmt.Sprintf("%s.fly.dev", cfg.Name)
	}
	deployment.RPCURL = "https://" + host
	deployment.WebSocketURL = "wss://" + host

	if !cfg.SkipHealthCheck {
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

func (p *FlyProvider) Destroy(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("deployment name is required")
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return fmt.Errorf("fly auth required: run `sol-cloud auth fly`: %w", err)
	}
	if err := p.destroyAppViaAPI(ctx, token, name); err != nil {
		return fmt.Errorf("destroy fly app via API: %w", err)
	}
	return nil
}

func (p *FlyProvider) Status(ctx context.Context, name string) (*Status, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("deployment name is required")
	}

	token, err := p.resolveAccessToken()
	if err != nil {
		return nil, fmt.Errorf("fly auth required: run `sol-cloud auth fly`: %w", err)
	}

	return p.statusViaAPI(ctx, token, name)
}

// VerifyAccessToken validates a Fly access token against the Machines API.
func (p *FlyProvider) VerifyAccessToken(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("fly access token is required")
	}

	err := p.verifyAccessTokenMachinesProbe(ctx, token)
	if err == nil {
		return nil
	}

	var statusErr *flyAPIStatusError
	if errors.As(err, &statusErr) {
		switch statusErr.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("token rejected by Fly API (%d): %s", statusErr.StatusCode, strings.TrimSpace(statusErr.Body))
		case http.StatusForbidden:
			// Could be a valid token with narrower scope; try GraphQL auth check.
			if fallbackErr := p.verifyAccessTokenGraphQL(ctx, token); fallbackErr == nil {
				return nil
			} else {
				return fmt.Errorf("token verification failed: machines API returned 403 and graphql fallback failed: %w", fallbackErr)
			}
		case http.StatusNotFound:
			// /apps/<probe>/machines returns 404 for missing app when auth is accepted.
			if !looksLikeRouteNotFound(statusErr.Body) {
				return nil
			}

			if fallbackErr := p.verifyAccessTokenGraphQL(ctx, token); fallbackErr == nil {
				return nil
			} else {
				return fmt.Errorf("token verification failed: machines endpoint returned route 404 and graphql fallback failed: %w", fallbackErr)
			}
		default:
			if fallbackErr := p.verifyAccessTokenGraphQL(ctx, token); fallbackErr == nil {
				return nil
			}
			return fmt.Errorf("token verification failed: %w", err)
		}
	}
	return fmt.Errorf("token verification failed: %w", err)
}

type flyAPIStatusError struct {
	StatusCode int
	Body       string
}

func (e *flyAPIStatusError) Error() string {
	if e == nil {
		return "fly api error"
	}
	return fmt.Sprintf("fly api returned %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

func (p *FlyProvider) verifyAccessTokenMachinesProbe(ctx context.Context, token string) error {
	url := p.machinesAPIURL("/apps/" + flyAuthProbeAppName + "/machines")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build machines auth probe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("machines auth probe failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &flyAPIStatusError{
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}
}

func (p *FlyProvider) verifyAccessTokenGraphQL(ctx context.Context, token string) error {
	reqBody, err := json.Marshal(map[string]string{
		"query": "query { viewer { id } apps { nodes { id } } }",
	})
	if err != nil {
		return fmt.Errorf("build graphql token check payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphqlURL(), bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build graphql token check request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("graphql token check failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("token rejected by Fly GraphQL API (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected Fly GraphQL response (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return fmt.Errorf("decode graphql token check response: %w", err)
	}
	if len(decoded.Errors) > 0 {
		return fmt.Errorf("graphql token check error: %s", strings.TrimSpace(decoded.Errors[0].Message))
	}
	if len(decoded.Data) == 0 {
		return errors.New("graphql token check returned empty data")
	}
	return nil
}

func (p *FlyProvider) resolveAccessToken() (string, error) {
	if token := strings.TrimSpace(p.AccessToken); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("SOL_CLOUD_FLY_ACCESS_TOKEN")); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("FLY_ACCESS_TOKEN")); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(os.Getenv("FLY_API_TOKEN")); token != "" {
		return token, nil
	}

	creds, err := appconfig.LoadCredentials()
	if err != nil {
		return "", fmt.Errorf("load credentials: %w", err)
	}
	if token := strings.TrimSpace(creds.Fly.AccessToken); token != "" {
		return token, nil
	}
	return "", errors.New("no fly access token configured")
}

func (p *FlyProvider) statusViaAPI(ctx context.Context, token, name string) (*Status, error) {
	states, err := p.fetchMachineStates(ctx, token, name)
	if err != nil {
		return nil, err
	}

	state := "unknown"
	if len(states) == 0 {
		state = "stopped"
	}
	for _, machineState := range states {
		switch machineState {
		case "started", "running":
			return &Status{Name: name, State: "running"}, nil
		case "starting", "created":
			state = "starting"
		case "stopped", "destroyed":
			if state == "unknown" {
				state = "stopped"
			}
		}
	}

	return &Status{Name: name, State: state}, nil
}

func (p *FlyProvider) destroyAppViaAPI(ctx context.Context, token, name string) error {
	status, body, err := p.doMachinesRequest(ctx, token, http.MethodDelete, "/apps/"+name, nil)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("app %q not found", name)
	}
	if status == http.StatusBadRequest || status == http.StatusConflict {
		machines, listErr := p.listMachines(ctx, token, name)
		if listErr == nil {
			for _, machine := range machines {
				if strings.TrimSpace(machine.ID) == "" {
					continue
				}
				_ = p.deleteMachine(ctx, token, name, machine.ID)
			}
			retryStatus, retryBody, retryErr := p.doMachinesRequest(ctx, token, http.MethodDelete, "/apps/"+name, nil)
			if retryErr == nil && retryStatus >= 200 && retryStatus < 300 {
				return nil
			}
			if retryErr != nil {
				return fmt.Errorf("destroy app retry failed: %w", retryErr)
			}
			return fmt.Errorf("destroy request returned status %d: %s", retryStatus, strings.TrimSpace(string(retryBody)))
		}
	}
	return fmt.Errorf("destroy request returned status %d: %s", status, strings.TrimSpace(string(body)))
}

func (p *FlyProvider) fetchMachineStates(ctx context.Context, token, name string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.machinesAPIURL("/apps/"+name+"/machines"), nil)
	if err != nil {
		return nil, fmt.Errorf("build status request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("status request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read status response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("app %q not found", name)
		}
		return nil, fmt.Errorf("status request returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	states := collectMachineStates(body)
	return states, nil
}

type flyMachineStatus struct {
	State  string `json:"state"`
	Status string `json:"status"`
}

func collectMachineStates(payload []byte) []string {
	var machines []flyMachineStatus
	if err := json.Unmarshal(payload, &machines); err == nil {
		return compactStatesFromMachines(machines)
	}

	var wrapped struct {
		Machines []flyMachineStatus `json:"machines"`
	}
	if err := json.Unmarshal(payload, &wrapped); err == nil {
		return compactStatesFromMachines(wrapped.Machines)
	}

	return nil
}

func compactStatesFromMachines(machines []flyMachineStatus) []string {
	states := make([]string, 0, len(machines))
	for _, machine := range machines {
		state := strings.ToLower(strings.TrimSpace(machine.State))
		if state == "" {
			state = strings.ToLower(strings.TrimSpace(machine.Status))
		}
		if state == "" {
			continue
		}
		states = append(states, state)
	}
	return states
}

func (p *FlyProvider) machinesAPIURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(p.MachinesAPIBaseURL), "/")
	if base == "" {
		base = defaultFlyMachinesAPI
	}
	if strings.HasPrefix(path, "/") {
		return base + path
	}
	return base + "/" + path
}

func (p *FlyProvider) graphqlURL() string {
	if strings.TrimSpace(p.GraphQLURL) != "" {
		return strings.TrimSpace(p.GraphQLURL)
	}
	return defaultFlyGraphQLURL
}

func looksLikeRouteNotFound(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	return strings.Contains(lower, "404 page not found") || lower == "not found"
}

func (p *FlyProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func prepareProgramDeployData(projectDir, programDir string, cfg *Config) (flyProgramDeployTemplateData, error) {
	if cfg == nil {
		return flyProgramDeployTemplateData{}, errors.New("config is required")
	}

	programCfg := cfg.Validator.ProgramDeploy
	if !programCfg.HasValues() {
		return flyProgramDeployTemplateData{}, nil
	}
	if !programCfg.Enabled() {
		return flyProgramDeployTemplateData{}, errors.New("program deploy config is incomplete")
	}

	soSrc, err := resolveProjectPath(projectDir, programCfg.SOPath)
	if err != nil {
		return flyProgramDeployTemplateData{}, fmt.Errorf("resolve program_deploy.so_path: %w", err)
	}
	programIDKeypairSrc, err := resolveProjectPath(projectDir, programCfg.ProgramIDKeypairPath)
	if err != nil {
		return flyProgramDeployTemplateData{}, fmt.Errorf("resolve program_deploy.program_id_keypair: %w", err)
	}
	upgradeAuthoritySrc, err := resolveProjectPath(projectDir, programCfg.UpgradeAuthorityPath)
	if err != nil {
		return flyProgramDeployTemplateData{}, fmt.Errorf("resolve program_deploy.upgrade_authority: %w", err)
	}

	soDest := filepath.Join(programDir, "program.so")
	if err := copyFile(soSrc, soDest); err != nil {
		return flyProgramDeployTemplateData{}, fmt.Errorf("copy program binary: %w", err)
	}
	programIDKeypairDest := filepath.Join(programDir, "program-id-keypair.json")
	if err := copyFile(programIDKeypairSrc, programIDKeypairDest); err != nil {
		return flyProgramDeployTemplateData{}, fmt.Errorf("copy program id keypair: %w", err)
	}
	upgradeAuthorityDest := filepath.Join(programDir, "upgrade-authority.json")
	if err := copyFile(upgradeAuthoritySrc, upgradeAuthorityDest); err != nil {
		return flyProgramDeployTemplateData{}, fmt.Errorf("copy upgrade authority keypair: %w", err)
	}

	return flyProgramDeployTemplateData{
		Enabled:              true,
		SOPath:               "/opt/sol-cloud/program/program.so",
		ProgramIDKeypairPath: "/opt/sol-cloud/program/program-id-keypair.json",
		UpgradeAuthorityPath: "/opt/sol-cloud/program/upgrade-authority.json",
	}, nil
}

func resolveProjectPath(projectDir, pathValue string) (string, error) {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return "", errors.New("path is empty")
	}

	clean := pathValue
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(projectDir, clean)
	}
	clean = filepath.Clean(clean)

	info, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", clean)
	}
	return clean, nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
}

func renderEmbeddedTemplateFile(name, dst string, data any) error {
	templateName := filepath.ToSlash(name)
	content, err := tmplassets.ReadFile(templateName)
	if err != nil {
		return fmt.Errorf("read embedded template %s: %w", templateName, err)
	}

	tpl, err := template.New(name).Parse(string(content))
	if err != nil {
		return fmt.Errorf("parse embedded template %s: %w", templateName, err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create file %s: %w", dst, err)
	}
	defer out.Close()

	if err := tpl.Execute(out, data); err != nil {
		return fmt.Errorf("render embedded template %s: %w", templateName, err)
	}
	return nil
}

func extractFlyHost(output string) string {
	matches := flyDomainPattern.FindStringSubmatch(output)
	if len(matches) < 2 {
		return ""
	}
	return strings.ToLower(matches[1])
}

func waitForRPCHealthy(ctx context.Context, rpcURL string, timeout, interval time.Duration) error {
	if strings.TrimSpace(rpcURL) == "" {
		return errors.New("rpc URL is empty")
	}

	healthCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		healthCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		if err := rpcHealthCheckFn(healthCtx, rpcURL); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-healthCtx.Done():
			if lastErr == nil {
				lastErr = healthCtx.Err()
			}
			return fmt.Errorf("rpc endpoint %s did not become healthy: %w", rpcURL, lastErr)
		case <-ticker.C:
		}
	}
}

func checkRPCHealth(ctx context.Context, rpcURL string) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "getHealth",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal health request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded struct {
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("decode health response: %w", err)
	}
	if len(decoded.Error) > 0 && string(decoded.Error) != "null" {
		return fmt.Errorf("rpc returned error: %s", string(decoded.Error))
	}
	if decoded.Result != "ok" {
		return fmt.Errorf("unexpected health result: %q", decoded.Result)
	}
	return nil
}

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
)

const (
	flyRegistryHost       = "registry.fly.io"
	defaultFlyOrgSlug     = "personal"
	defaultMachineTimeout = 180 * time.Second
)

type flyAppResponse struct {
	Name string `json:"name"`
}

type flyAppCreateRequest struct {
	AppName string `json:"app_name"`
	OrgSlug string `json:"org_slug,omitempty"`
}

type flyMachine struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Status string `json:"status"`
}

type flyMachineCreateRequest struct {
	Name   string           `json:"name,omitempty"`
	Region string           `json:"region,omitempty"`
	Config flyMachineConfig `json:"config"`
}

type flyMachineConfig struct {
	Image       string              `json:"image"`
	Services    []flyMachineService `json:"services,omitempty"`
	Restart     *flyMachineRestart  `json:"restart,omitempty"`
	Guest       *flyMachineGuest    `json:"guest,omitempty"`
	Env         map[string]string   `json:"env,omitempty"`
	Metrics     map[string]any      `json:"metrics,omitempty"`
	Checks      map[string]any      `json:"checks,omitempty"`
	Mounts      []map[string]any    `json:"mounts,omitempty"`
	Processes   []map[string]any    `json:"processes,omitempty"`
	Files       []map[string]any    `json:"files,omitempty"`
	Init        map[string]any      `json:"init,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
	AutoDestroy bool                `json:"auto_destroy,omitempty"`
}

type flyMachineService struct {
	Protocol           string                 `json:"protocol"`
	InternalPort       int                    `json:"internal_port"`
	AutoStop           string                 `json:"autostop,omitempty"`
	AutoStart          bool                   `json:"autostart,omitempty"`
	MinMachinesRunning int                    `json:"min_machines_running,omitempty"`
	Concurrency        *flyMachineConcurrency `json:"concurrency,omitempty"`
	Ports              []flyMachinePort       `json:"ports,omitempty"`
}

type flyMachineConcurrency struct {
	Type      string `json:"type"`
	SoftLimit int    `json:"soft_limit,omitempty"`
	HardLimit int    `json:"hard_limit,omitempty"`
}

type flyMachinePort struct {
	Port       int      `json:"port"`
	Handlers   []string `json:"handlers,omitempty"`
	ForceHTTPS bool     `json:"force_https,omitempty"`
}

type flyMachineRestart struct {
	Policy     string `json:"policy"`
	MaxRetries int    `json:"max_retries,omitempty"`
}

type flyMachineGuest struct {
	CPUKind  string `json:"cpu_kind,omitempty"`
	CPUs     int    `json:"cpus,omitempty"`
	MemoryMB int    `json:"memory_mb,omitempty"`
}

func (p *FlyProvider) deployViaAPI(
	ctx context.Context,
	token string,
	cfg *Config,
	artifactsDir string,
) (string, string, error) {
	if err := p.ensureFlyctlInstalled(); err != nil {
		return "", "", err
	}

	orgSlug, err := p.resolveOrgSlug(cfg.OrgSlug)
	if err != nil {
		return "", "", err
	}

	var logs strings.Builder
	logs.WriteString("api deploy started\n")
	logs.WriteString(fmt.Sprintf("app=%s org=%s region=%s\n", cfg.Name, orgSlug, cfg.Region))

	if err := p.ensureAppExists(ctx, token, cfg.Name, orgSlug); err != nil {
		return logs.String(), "", err
	}
	logs.WriteString("app ensured\n")

	if err := p.ensureNetworking(ctx, token, cfg.Name); err != nil {
		return logs.String(), "", err
	}
	logs.WriteString("networking ensured\n")

	env := append(os.Environ(), "FLY_ACCESS_TOKEN="+token)
	if strings.TrimSpace(orgSlug) != "" {
		env = append(env, "FLY_ORG="+orgSlug)
	}
	deployOutput, err := p.runCommandWithEnv(
		ctx,
		artifactsDir,
		"",
		env,
		"flyctl",
		"deploy",
		"--app", cfg.Name,
		"--config", "fly.toml",
		"--remote-only",
		"--ha=false",
		"--wait-timeout=15m",
		"--yes",
	)
	logs.WriteString("\n[flyctl deploy --remote-only]\n")
	logs.WriteString(deployOutput)
	if err != nil {
		return logs.String(), "", commandStageError("fly remote deploy", err, deployOutput)
	}

	host := extractFlyHost(deployOutput)
	if host == "" {
		host = fmt.Sprintf("%s.fly.dev", cfg.Name)
	}
	return logs.String(), host, nil
}

func (p *FlyProvider) resolveOrgSlug(explicit string) (string, error) {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed, nil
	}
	if env := strings.TrimSpace(os.Getenv("SOL_CLOUD_FLY_ORG")); env != "" {
		return env, nil
	}
	creds, err := appconfig.LoadCredentials()
	if err != nil {
		return "", fmt.Errorf("load credentials: %w", err)
	}
	if saved := strings.TrimSpace(creds.Fly.OrgSlug); saved != "" {
		return saved, nil
	}
	return defaultFlyOrgSlug, nil
}

func (p *FlyProvider) ensureFlyctlInstalled() error {
	if _, err := exec.LookPath("flyctl"); err != nil {
		return fmt.Errorf("flyctl not found in PATH: %w (install from https://fly.io/docs/flyctl/install/)", err)
	}
	return nil
}

func (p *FlyProvider) ensureAppExists(ctx context.Context, token, appName, orgSlug string) error {
	payload := flyAppCreateRequest{
		AppName: appName,
		OrgSlug: strings.TrimSpace(orgSlug),
	}
	status, body, err := p.doMachinesRequest(ctx, token, http.MethodPost, "/apps", payload)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		lower := strings.ToLower(string(body))
		if strings.Contains(lower, "already exists") ||
			strings.Contains(lower, "already taken") ||
			strings.Contains(lower, "already been taken") ||
			strings.Contains(lower, "has already been taken") ||
			strings.Contains(lower, "name is taken") {
			return nil
		}
	}
	return fmt.Errorf("create app %q failed (%d): %s", appName, status, strings.TrimSpace(string(body)))
}

func (p *FlyProvider) ensureNetworking(ctx context.Context, token, appName string) error {
	for _, ipType := range []string{"shared_v4", "v6"} {
		if err := p.allocateIPAddress(ctx, token, appName, ipType); err != nil {
			return err
		}
	}
	return nil
}

func (p *FlyProvider) allocateIPAddress(ctx context.Context, token, appName, ipType string) error {
	query := `mutation($input: AllocateIPAddressInput!) { allocateIpAddress(input: $input) { ipAddress { id } } }`
	variables := map[string]any{
		"input": map[string]any{
			"appId": appName,
			"type":  ipType,
		},
	}
	resp, err := p.graphQLRequest(ctx, token, query, variables)
	if err != nil {
		return fmt.Errorf("allocate %s ip: %w", ipType, err)
	}
	for _, graphErr := range resp.Errors {
		msg := strings.ToLower(strings.TrimSpace(graphErr.Message))
		if strings.Contains(msg, "already has") || strings.Contains(msg, "already allocated") {
			return nil
		}
		return fmt.Errorf("allocate %s ip graphql error: %s", ipType, graphErr.Message)
	}
	return nil
}

func (p *FlyProvider) imageRef(appName string) string {
	tag := fmt.Sprintf("deploy-%d", time.Now().UTC().Unix())
	return fmt.Sprintf("%s/%s:%s", flyRegistryHost, appName, tag)
}

func (p *FlyProvider) dockerLogin(ctx context.Context, token string) error {
	input := token
	if !strings.HasSuffix(input, "\n") {
		input += "\n"
	}
	output, err := p.runCommand(ctx, "", input, "docker", "login", flyRegistryHost, "-u", "x", "--password-stdin")
	if err != nil {
		return fmt.Errorf("docker login failed: %w\n%s", err, strings.TrimSpace(output))
	}
	return nil
}

func (p *FlyProvider) listMachines(ctx context.Context, token, appName string) ([]flyMachine, error) {
	status, body, err := p.doMachinesRequest(ctx, token, http.MethodGet, fmt.Sprintf("/apps/%s/machines", appName), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("app %q not found", appName)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("list machines failed (%d): %s", status, strings.TrimSpace(string(body)))
	}

	var machines []flyMachine
	if err := json.Unmarshal(body, &machines); err == nil {
		return machines, nil
	}
	var wrapped struct {
		Machines []flyMachine `json:"machines"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		return wrapped.Machines, nil
	}
	return nil, fmt.Errorf("decode machines response: %s", strings.TrimSpace(string(body)))
}

func (p *FlyProvider) createMachine(ctx context.Context, token, appName, region, imageRef string) (*flyMachine, error) {
	req := flyMachineCreateRequest{
		Name:   "validator",
		Region: region,
		Config: flyMachineConfig{
			Image: imageRef,
			Services: []flyMachineService{
				{
					Protocol:           "tcp",
					InternalPort:       8080,
					AutoStart:          true,
					AutoStop:           "off",
					MinMachinesRunning: 1,
					Concurrency: &flyMachineConcurrency{
						Type:      "connections",
						SoftLimit: 200,
						HardLimit: 250,
					},
					Ports: []flyMachinePort{
						{
							Port:       80,
							Handlers:   []string{"http"},
							ForceHTTPS: true,
						},
						{
							Port:     443,
							Handlers: []string{"tls", "http"},
						},
					},
				},
			},
			Restart: &flyMachineRestart{
				Policy: "on-failure",
			},
			Guest: &flyMachineGuest{
				CPUKind:  "shared",
				CPUs:     1,
				MemoryMB: 2048,
			},
		},
	}

	status, body, err := p.doMachinesRequest(ctx, token, http.MethodPost, fmt.Sprintf("/apps/%s/machines", appName), req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("create machine failed (%d): %s", status, strings.TrimSpace(string(body)))
	}

	var machine flyMachine
	if err := json.Unmarshal(body, &machine); err == nil && strings.TrimSpace(machine.ID) != "" {
		return &machine, nil
	}
	var wrapped struct {
		Machine flyMachine `json:"machine"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && strings.TrimSpace(wrapped.Machine.ID) != "" {
		return &wrapped.Machine, nil
	}
	return nil, fmt.Errorf("create machine response missing id: %s", strings.TrimSpace(string(body)))
}

func (p *FlyProvider) waitForMachineState(ctx context.Context, token, appName, machineID, targetState string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultMachineTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	query := fmt.Sprintf("/apps/%s/machines/%s/wait?state=%s&timeout=%d", appName, machineID, targetState, int(timeout.Seconds()))
	status, body, err := p.doMachinesRequest(waitCtx, token, http.MethodGet, query, nil)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fmt.Errorf("wait for machine state failed (%d): %s", status, strings.TrimSpace(string(body)))
}

func (p *FlyProvider) deleteMachine(ctx context.Context, token, appName, machineID string) error {
	path := fmt.Sprintf("/apps/%s/machines/%s?force=true", appName, machineID)
	status, body, err := p.doMachinesRequest(ctx, token, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("delete machine %s failed (%d): %s", machineID, status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (p *FlyProvider) doMachinesRequest(ctx context.Context, token, method, path string, payload any) (int, []byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(body)
	}

	url := p.machinesAPIURL(path)
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("build request %s %s: %w", method, url, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return 0, nil, fmt.Errorf("read response %s %s: %w", method, url, err)
	}
	return resp.StatusCode, body, nil
}

type flyGraphQLResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (p *FlyProvider) graphQLRequest(ctx context.Context, token, query string, variables map[string]any) (*flyGraphQLResponse, error) {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.graphqlURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read graphql response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("graphql auth rejected (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graphql status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded flyGraphQLResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode graphql response: %w", err)
	}
	return &decoded, nil
}

func (p *FlyProvider) runCommand(ctx context.Context, dir, stdin, name string, args ...string) (string, error) {
	return p.runCommandWithEnv(ctx, dir, stdin, nil, name, args...)
}

func (p *FlyProvider) runCommandWithEnv(ctx context.Context, dir, stdin string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	if len(env) > 0 {
		cmd.Env = env
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func commandStageError(stage string, err error, output string) error {
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("%s failed: %w", stage, err)
	}
	return fmt.Errorf("%s failed: %w\n%s", stage, err, lastNLines(output, 40))
}

func lastNLines(text string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) <= n {
		return text
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

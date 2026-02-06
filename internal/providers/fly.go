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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

const (
	defaultTemplateDir  = "templates"
	defaultFlyctlBin    = "flyctl"
	defaultHealthCheck  = 3 * time.Minute
	defaultPollInterval = 5 * time.Second
)

var flyDomainPattern = regexp.MustCompile(`([a-zA-Z0-9-]+\.fly\.dev)`)
var rpcHealthCheckFn = checkRPCHealth

// FlyProvider manages validator deployments on Fly.io.
type FlyProvider struct {
	TemplateDir string
	FlyctlBin   string
}

func NewFlyProvider() *FlyProvider {
	return &FlyProvider{
		TemplateDir: defaultTemplateDir,
		FlyctlBin:   defaultFlyctlBin,
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
	templateDir := p.templateDir()
	if !filepath.IsAbs(templateDir) {
		templateDir = filepath.Join(projectDir, templateDir)
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

	if err := renderTemplateFile(
		filepath.Join(templateDir, "Dockerfile.tmpl"),
		filepath.Join(artifactsDir, "Dockerfile"),
		data,
	); err != nil {
		return nil, err
	}
	if err := renderTemplateFile(
		filepath.Join(templateDir, "fly.toml.tmpl"),
		filepath.Join(artifactsDir, "fly.toml"),
		data,
	); err != nil {
		return nil, err
	}
	if err := renderTemplateFile(
		filepath.Join(templateDir, "nginx.conf.tmpl"),
		filepath.Join(artifactsDir, "nginx.conf"),
		data,
	); err != nil {
		return nil, err
	}
	if err := renderTemplateFile(
		filepath.Join(templateDir, "entrypoint.sh.tmpl"),
		filepath.Join(artifactsDir, "entrypoint.sh"),
		data,
	); err != nil {
		return nil, err
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

	if _, err := exec.LookPath(p.flyctlBin()); err != nil {
		return nil, fmt.Errorf("flyctl not found in PATH: %w (install from https://fly.io/docs/flyctl/install/)", err)
	}

	createOutput, createErr := p.runFlyctl(ctx, projectDir, "apps", "create", cfg.Name)
	if createErr != nil {
		lower := strings.ToLower(createOutput)
		if !strings.Contains(lower, "already exists") && !strings.Contains(lower, "name is already taken") {
			return nil, fmt.Errorf("create fly app: %w\n%s", createErr, strings.TrimSpace(createOutput))
		}
	}

	deployOutput, deployErr := p.runFlyctl(ctx, artifactsDir, "deploy", "--app", cfg.Name, "--config", "fly.toml", "--remote-only", "--yes")
	if deployErr != nil {
		return nil, fmt.Errorf("deploy fly app: %w\n%s", deployErr, strings.TrimSpace(deployOutput))
	}

	host := extractFlyHost(deployOutput)
	if host == "" {
		host = fmt.Sprintf("%s.fly.dev", cfg.Name)
	}
	deployment.RPCURL = "https://" + host
	deployment.WebSocketURL = "wss://" + host

	if err := os.WriteFile(filepath.Join(artifactsDir, "flyctl-deploy.log"), []byte(deployOutput), 0o644); err != nil {
		return nil, fmt.Errorf("write deploy log: %w", err)
	}

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
	if _, err := exec.LookPath(p.flyctlBin()); err != nil {
		return fmt.Errorf("flyctl not found in PATH: %w", err)
	}

	output, err := p.runFlyctl(ctx, "", "apps", "destroy", name, "--yes")
	if err != nil {
		return fmt.Errorf("destroy fly app: %w\n%s", err, strings.TrimSpace(output))
	}
	return nil
}

func (p *FlyProvider) Status(ctx context.Context, name string) (*Status, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("deployment name is required")
	}
	if _, err := exec.LookPath(p.flyctlBin()); err != nil {
		return nil, fmt.Errorf("flyctl not found in PATH: %w", err)
	}

	output, err := p.runFlyctl(ctx, "", "status", "--app", name)
	if err != nil {
		return nil, fmt.Errorf("fetch fly status: %w\n%s", err, strings.TrimSpace(output))
	}

	state := "unknown"
	lower := strings.ToLower(output)
	if strings.Contains(lower, "running") {
		state = "running"
	}

	return &Status{Name: name, State: state}, nil
}

func (p *FlyProvider) runFlyctl(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, p.flyctlBin(), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (p *FlyProvider) templateDir() string {
	if p.TemplateDir != "" {
		return p.TemplateDir
	}
	return defaultTemplateDir
}

func (p *FlyProvider) flyctlBin() string {
	if p.FlyctlBin != "" {
		return p.FlyctlBin
	}
	return defaultFlyctlBin
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

func renderTemplateFile(src, dst string, data any) error {
	tpl, err := template.ParseFiles(src)
	if err != nil {
		return fmt.Errorf("parse template %s: %w", src, err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create file %s: %w", dst, err)
	}
	defer out.Close()

	if err := tpl.Execute(out, data); err != nil {
		return fmt.Errorf("render template %s: %w", src, err)
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

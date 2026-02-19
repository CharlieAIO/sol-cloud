package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	tmplassets "github.com/CharlieAIO/sol-cloud/templates"
)

func runCommand(ctx context.Context, dir, stdin, name string, args ...string) (string, error) {
	return runCommandWithEnv(ctx, dir, stdin, nil, name, args...)
}

func runCommandWithEnv(ctx context.Context, dir, stdin string, env []string, name string, args ...string) (string, error) {
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

func prepareProgramDeployData(projectDir, programDir string, cfg *Config) (programDeployTemplateData, error) {
	if cfg == nil {
		return programDeployTemplateData{}, errors.New("config is required")
	}

	programCfg := cfg.Validator.ProgramDeploy
	if !programCfg.HasValues() {
		return programDeployTemplateData{}, nil
	}
	if !programCfg.Enabled() {
		return programDeployTemplateData{}, errors.New("program deploy config is incomplete")
	}

	soSrc, err := resolveProjectPath(projectDir, programCfg.SOPath)
	if err != nil {
		return programDeployTemplateData{}, fmt.Errorf("resolve program_deploy.so_path: %w", err)
	}
	programIDKeypairSrc, err := resolveProjectPath(projectDir, programCfg.ProgramIDKeypairPath)
	if err != nil {
		return programDeployTemplateData{}, fmt.Errorf("resolve program_deploy.program_id_keypair: %w", err)
	}
	upgradeAuthoritySrc, err := resolveProjectPath(projectDir, programCfg.UpgradeAuthorityPath)
	if err != nil {
		return programDeployTemplateData{}, fmt.Errorf("resolve program_deploy.upgrade_authority: %w", err)
	}

	soDest := filepath.Join(programDir, "program.so")
	if err := copyFile(soSrc, soDest); err != nil {
		return programDeployTemplateData{}, fmt.Errorf("copy program binary: %w", err)
	}
	programIDKeypairDest := filepath.Join(programDir, "program-id-keypair.json")
	if err := copyFile(programIDKeypairSrc, programIDKeypairDest); err != nil {
		return programDeployTemplateData{}, fmt.Errorf("copy program id keypair: %w", err)
	}
	upgradeAuthorityDest := filepath.Join(programDir, "upgrade-authority.json")
	if err := copyFile(upgradeAuthoritySrc, upgradeAuthorityDest); err != nil {
		return programDeployTemplateData{}, fmt.Errorf("copy upgrade authority keypair: %w", err)
	}

	return programDeployTemplateData{
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

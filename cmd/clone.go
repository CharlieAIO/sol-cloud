package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/spf13/cobra"
)

const defaultCloneSourceRPC = "https://api.mainnet-beta.solana.com"

var base58AddressPattern = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{32,44}$`)
var deployedProgramPattern = regexp.MustCompile(`(?m)Program Id:\s*([1-9A-HJ-NP-Za-km-z]{32,44})`)

var (
	cloneSourceRPC      string
	cloneOutPath        string
	cloneOverwrite      bool
	cloneDeploy         bool
	cloneTargetRPC      string
	cloneDeploymentName string
	cloneKeypairPath    string
	cloneProgramKeypair string
)

var cloneCmd = &cobra.Command{
	Use:   "clone-program <program-id>",
	Short: "Clone a Solana program binary from an RPC endpoint",
	Long:  "Dump a program binary with the Solana CLI and optionally deploy it to a managed validator.",
	Example: `  sol-cloud clone-program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA
  sol-cloud clone-program <program-id> --source-rpc https://api.mainnet-beta.solana.com --out ./artifacts/program.so
  sol-cloud clone-program <program-id> --deploy --name my-validator
  sol-cloud clone-program <program-id> --deploy --target-rpc https://my-validator.fly.dev --keypair ~/.config/solana/id.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		programID := strings.TrimSpace(args[0])
		if !base58AddressPattern.MatchString(programID) {
			return fmt.Errorf("invalid program id: %q", programID)
		}

		if _, err := exec.LookPath("solana"); err != nil {
			return fmt.Errorf("solana CLI not found in PATH: %w", err)
		}

		projectDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		sourceRPC := strings.TrimSpace(cloneSourceRPC)
		if sourceRPC == "" {
			sourceRPC = defaultCloneSourceRPC
		}

		outPath := resolveCloneOutputPath(projectDir, programID, cloneOutPath)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		if !cloneOverwrite {
			if _, err := os.Stat(outPath); err == nil {
				return fmt.Errorf("output file already exists: %s (use --overwrite)", outPath)
			}
		}

		fmt.Fprintf(cmd.OutOrStdout(), "cloning program %s from %s\n", programID, sourceRPC)
		dumpOutput, err := runSolana(cmd.Context(), "program", "dump", "-u", sourceRPC, programID, outPath)
		if err != nil {
			return fmt.Errorf("solana program dump failed: %w\n%s", err, strings.TrimSpace(dumpOutput))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "program binary written: %s\n", outPath)

		if !cloneDeploy {
			return nil
		}

		targetRPC, err := resolveCloneTargetRPC(projectDir, strings.TrimSpace(cloneTargetRPC), strings.TrimSpace(cloneDeploymentName))
		if err != nil {
			return err
		}

		deployArgs := []string{"program", "deploy", "-u", targetRPC, outPath}
		if keypair := strings.TrimSpace(cloneKeypairPath); keypair != "" {
			deployArgs = append(deployArgs, "--keypair", keypair)
		}
		if programKeypair := strings.TrimSpace(cloneProgramKeypair); programKeypair != "" {
			deployArgs = append(deployArgs, "--program-id", programKeypair)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "deploying cloned program to %s\n", targetRPC)
		deployOutput, err := runSolana(cmd.Context(), deployArgs...)
		if err != nil {
			return fmt.Errorf("solana program deploy failed: %w\n%s", err, strings.TrimSpace(deployOutput))
		}

		programIDOut := parseDeployedProgramID(deployOutput)
		if programIDOut == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "deploy completed (could not parse Program Id from output)\n")
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "deployed program id: %s\n", programIDOut)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cloneCmd)

	cloneCmd.Flags().StringVar(&cloneSourceRPC, "source-rpc", defaultCloneSourceRPC, "Source RPC URL used by `solana program dump`")
	cloneCmd.Flags().StringVar(&cloneOutPath, "out", "", "Output path for dumped program binary (.so)")
	cloneCmd.Flags().BoolVar(&cloneOverwrite, "overwrite", false, "Overwrite output file if it exists")
	cloneCmd.Flags().BoolVar(&cloneDeploy, "deploy", false, "Deploy cloned binary to a target validator after dump")
	cloneCmd.Flags().StringVar(&cloneTargetRPC, "target-rpc", "", "Target RPC URL for deploy (overrides local state lookup)")
	cloneCmd.Flags().StringVar(&cloneDeploymentName, "name", "", "Deployment name to resolve target RPC from local state")
	cloneCmd.Flags().StringVar(&cloneKeypairPath, "keypair", "", "Payer keypair path for deploy")
	cloneCmd.Flags().StringVar(&cloneProgramKeypair, "program-id-keypair", "", "Program keypair path used to keep a deterministic program id")
}

func resolveCloneOutputPath(projectDir, programID, configuredPath string) string {
	outPath := strings.TrimSpace(configuredPath)
	if outPath == "" {
		outPath = filepath.Join(projectDir, ".sol-cloud", "programs", programID+".so")
	} else if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(projectDir, outPath)
	}
	return filepath.Clean(outPath)
}

func resolveCloneTargetRPC(projectDir, explicitRPC, deploymentName string) (string, error) {
	if explicitRPC != "" {
		return explicitRPC, nil
	}

	state, err := appconfig.LoadState(projectDir)
	if err != nil {
		return "", fmt.Errorf("load local deployment state: %w", err)
	}
	record, err := state.ResolveDeployment(deploymentName)
	if err != nil {
		if errors.Is(err, appconfig.ErrNoDeployments) {
			return "", errors.New("no deployments found; pass --target-rpc or deploy a validator first")
		}
		if errors.Is(err, appconfig.ErrDeploymentNotFound) {
			return "", fmt.Errorf("deployment %q not found in local state", deploymentName)
		}
		return "", fmt.Errorf("unable to resolve target deployment from local state: %w (pass --name or --target-rpc)", err)
	}
	if strings.TrimSpace(record.RPCURL) == "" {
		return "", fmt.Errorf("deployment %q has no RPC URL in local state", record.Name)
	}
	return record.RPCURL, nil
}

func parseDeployedProgramID(output string) string {
	match := deployedProgramPattern.FindStringSubmatch(output)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func runSolana(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "solana", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

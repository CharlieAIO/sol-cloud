package cmd

import (
	"context"
	"encoding/json"
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
	cloneSourceRPC       string
	cloneOutPath         string
	cloneOverwrite       bool
	cloneDeploy          bool
	cloneTargetRPC       string
	cloneDeploymentName  string
	cloneKeypairPath     string
	cloneProgramKeypair  string
	cloneAccounts        []string
	cloneAccountsDir     string
	cloneIncludeProgData bool
	cloneWriteAcctFlags  bool
)

var cloneCmd = &cobra.Command{
	Use:   "clone-program <program-id>",
	Short: "Clone a Solana program binary from an RPC endpoint",
	Long:  "Dump a program binary with the Solana CLI and optionally deploy it to a managed validator.",
	Example: `  sol-cloud clone-program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA
  sol-cloud clone-program <program-id> --source-rpc https://api.mainnet-beta.solana.com --out ./artifacts/program.so
  sol-cloud clone-program <program-id> --account EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v
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

		accountIDs, collectErr := collectAccountIDsForClone(cmd.Context(), sourceRPC, programID, cloneAccounts, cloneIncludeProgData)
		if collectErr != nil {
			return collectErr
		}
		if len(accountIDs) > 0 {
			accountsDir := resolveCloneAccountsDir(projectDir, programID, cloneAccountsDir)
			if err := os.MkdirAll(accountsDir, 0o755); err != nil {
				return fmt.Errorf("create accounts directory: %w", err)
			}

			records := make([]accountSnapshotRecord, 0, len(accountIDs))
			for _, accountID := range accountIDs {
				out := filepath.Join(accountsDir, accountID+".json")
				accountOutput, err := runSolana(
					cmd.Context(),
					"account",
					"-u", sourceRPC,
					"--output", "json-compact",
					"--output-file", out,
					accountID,
				)
				if err != nil {
					return fmt.Errorf("dump account %s failed: %w\n%s", accountID, err, strings.TrimSpace(accountOutput))
				}
				records = append(records, accountSnapshotRecord{
					Address: accountID,
					Path:    out,
				})
				fmt.Fprintf(cmd.OutOrStdout(), "account snapshot written: %s\n", out)
			}

			if cloneWriteAcctFlags {
				flagsPath, err := writeAccountFlagsFile(accountsDir, records)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "validator account-flags file: %s\n", flagsPath)
				fmt.Fprintln(cmd.OutOrStdout(), "note: load snapshots at validator startup with `solana-test-validator --reset $(cat <flags-file>)`")
			}

			if cloneDeploy {
				fmt.Fprintln(cmd.OutOrStdout(), "note: account snapshots are exported only; loading them requires validator restart with --account flags")
			}
		}

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
	cloneCmd.Flags().StringSliceVar(&cloneAccounts, "account", nil, "Additional account addresses to snapshot from source RPC (repeatable)")
	cloneCmd.Flags().StringVar(&cloneAccountsDir, "accounts-dir", "", "Directory for dumped account snapshots")
	cloneCmd.Flags().BoolVar(&cloneIncludeProgData, "include-programdata", true, "Also snapshot upgradeable programdata account when available")
	cloneCmd.Flags().BoolVar(&cloneWriteAcctFlags, "write-account-flags", true, "Write helper flags file for `solana-test-validator --account ...`")
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

func resolveCloneAccountsDir(projectDir, programID, configuredPath string) string {
	accountsDir := strings.TrimSpace(configuredPath)
	if accountsDir == "" {
		accountsDir = filepath.Join(projectDir, ".sol-cloud", "programs", programID+"-accounts")
	} else if !filepath.IsAbs(accountsDir) {
		accountsDir = filepath.Join(projectDir, accountsDir)
	}
	return filepath.Clean(accountsDir)
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

type accountSnapshotRecord struct {
	Address string
	Path    string
}

func collectAccountIDsForClone(ctx context.Context, sourceRPC, programID string, configured []string, includeProgramData bool) ([]string, error) {
	seen := make(map[string]struct{})
	accounts := make([]string, 0, len(configured)+1)
	for _, raw := range configured {
		accountID := strings.TrimSpace(raw)
		if accountID == "" {
			continue
		}
		if !base58AddressPattern.MatchString(accountID) {
			return nil, fmt.Errorf("invalid account address: %q", accountID)
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		accounts = append(accounts, accountID)
	}

	if includeProgramData {
		progData, err := fetchProgramDataAddress(ctx, sourceRPC, programID)
		if err != nil {
			return nil, err
		}
		if progData != "" {
			if _, ok := seen[progData]; !ok {
				accounts = append(accounts, progData)
			}
		}
	}

	return accounts, nil
}

func fetchProgramDataAddress(ctx context.Context, sourceRPC, programID string) (string, error) {
	output, err := runSolana(ctx, "program", "show", "-u", sourceRPC, "--output", "json", programID)
	if err != nil {
		return "", fmt.Errorf("solana program show failed while resolving programdata account: %w\n%s", err, strings.TrimSpace(output))
	}

	var decoded struct {
		ProgramDataAddress string `json:"programdataAddress"`
	}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		return "", fmt.Errorf("decode program show output: %w", err)
	}

	addr := strings.TrimSpace(decoded.ProgramDataAddress)
	if addr == "" || !base58AddressPattern.MatchString(addr) {
		return "", nil
	}
	return addr, nil
}

func writeAccountFlagsFile(accountsDir string, records []accountSnapshotRecord) (string, error) {
	if len(records) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, fmt.Sprintf("--account %s %s", record.Address, record.Path))
	}
	content := strings.Join(lines, "\n") + "\n"

	path := filepath.Join(accountsDir, "validator-account-flags.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write account-flags file: %w", err)
	}
	return path, nil
}

func runSolana(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "solana", args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

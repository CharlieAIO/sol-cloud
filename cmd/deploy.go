package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/CharlieAIO/sol-cloud/internal/utils"
	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	deployRegion             string
	deployOrg                string
	deployDryRun             bool
	deploySkipHealthCheck    bool
	deployHealthCheckTimeout time.Duration
	deployHealthCheckPoll    time.Duration
	deploySlotsPerEpoch      uint64
	deployTicksPerSlot       uint64
	deployComputeUnitLimit   uint64
	deployLedgerLimitSize    uint64
	deployCloneAccounts      []string
	deployCloneUpPrograms    []string
	deployProgramSOPath      string
	deployProgramIDKeypair   string
	deployProgramIDLegacy    string
	deployUpgradeAuthority   string
	deployVolumeSize         int
	deploySkipVolume         bool
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a Solana validator",
	Long:  "Render deployment artifacts and deploy a Solana validator using app_name from .sol-cloud.yml.",
	Example: `  sol-cloud deploy
  sol-cloud deploy --dry-run
  sol-cloud deploy --region ord --health-timeout 4m
  sol-cloud deploy --slots-per-epoch 216000 --ticks-per-slot 32 --compute-unit-limit 300000
  sol-cloud deploy --clone 11111111111111111111111111111111 --clone-upgradeable-program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA
  sol-cloud deploy --program-so ./programs/my_program.so --program-id-keypair ./keys/program-keypair.json --upgrade-authority ./keys/upgrade-authority.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		providerName := strings.ToLower(strings.TrimSpace(viper.GetString("provider")))
		if providerName == "" {
			providerName = "fly"
		}

		name := strings.TrimSpace(viper.GetString("app_name"))
		if name == "" {
			return fmt.Errorf("app_name is required in .sol-cloud.yml; run `sol-cloud init` first")
		}
		var err error
		switch providerName {
		case "fly":
			name, err = utils.EnsureFlyAppName(name)
			if err != nil {
				return fmt.Errorf("invalid app_name in .sol-cloud.yml: %w", err)
			}
		case "railway":
			name, err = utils.EnsureRailwayProjectName(name)
			if err != nil {
				return fmt.Errorf("invalid app_name in .sol-cloud.yml: %w", err)
			}
		default:
			return fmt.Errorf("unsupported provider %q: valid providers are fly, railway", providerName)
		}

		region := strings.TrimSpace(deployRegion)
		if region == "" {
			region = strings.TrimSpace(viper.GetString("region"))
		}
		if region == "" {
			if providerName == "railway" {
				region = "us-west"
			} else {
				region = "ord"
			}
		}

		projectDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		validatorCfg := validator.Config{
			SlotsPerEpoch:            viper.GetUint64("validator.slots_per_epoch"),
			TicksPerSlot:             viper.GetUint64("validator.ticks_per_slot"),
			ComputeUnitLimit:         viper.GetUint64("validator.compute_unit_limit"),
			LedgerLimitSize:          viper.GetUint64("validator.ledger_limit_size"),
			CloneAccounts:            viper.GetStringSlice("validator.clone_accounts"),
			CloneUpgradeablePrograms: viper.GetStringSlice("validator.clone_upgradeable_programs"),
			ProgramDeploy: validator.ProgramDeployConfig{
				SOPath:               viper.GetString("validator.program_deploy.so_path"),
				ProgramIDKeypairPath: viper.GetString("validator.program_deploy.program_id_keypair"),
				UpgradeAuthorityPath: viper.GetString("validator.program_deploy.upgrade_authority"),
			},
		}
		// Backward compatibility for older configs that used program_id pubkey semantics.
		if strings.TrimSpace(validatorCfg.ProgramDeploy.ProgramIDKeypairPath) == "" {
			validatorCfg.ProgramDeploy.ProgramIDKeypairPath = viper.GetString("validator.program_deploy.program_id")
		}
		validatorCfg.ApplyDefaults()
		if deploySlotsPerEpoch > 0 {
			validatorCfg.SlotsPerEpoch = deploySlotsPerEpoch
		}
		if deployTicksPerSlot > 0 {
			validatorCfg.TicksPerSlot = deployTicksPerSlot
		}
		if deployComputeUnitLimit > 0 {
			validatorCfg.ComputeUnitLimit = deployComputeUnitLimit
		}
		if deployLedgerLimitSize > 0 {
			validatorCfg.LedgerLimitSize = deployLedgerLimitSize
		}
		if cmd.Flags().Changed("clone") {
			validatorCfg.CloneAccounts = append([]string(nil), deployCloneAccounts...)
		}
		if cmd.Flags().Changed("clone-upgradeable-program") {
			validatorCfg.CloneUpgradeablePrograms = append([]string(nil), deployCloneUpPrograms...)
		}
		if cmd.Flags().Changed("program-so") {
			validatorCfg.ProgramDeploy.SOPath = deployProgramSOPath
		}
		if cmd.Flags().Changed("program-id-keypair") {
			validatorCfg.ProgramDeploy.ProgramIDKeypairPath = deployProgramIDKeypair
		}
		if cmd.Flags().Changed("program-id") {
			validatorCfg.ProgramDeploy.ProgramIDKeypairPath = deployProgramIDLegacy
		}
		if cmd.Flags().Changed("upgrade-authority") {
			validatorCfg.ProgramDeploy.UpgradeAuthorityPath = deployUpgradeAuthority
		}
		if err := validatorCfg.Validate(); err != nil {
			return fmt.Errorf("invalid validator config: %w", err)
		}

		volumeSize := deployVolumeSize
		if volumeSize <= 0 {
			volumeSize = 10
		}

		cfg := &providers.Config{
			Name:                name,
			OrgSlug:             firstNonEmpty(strings.TrimSpace(deployOrg), strings.TrimSpace(viper.GetString("org"))),
			Region:              region,
			ProjectDir:          projectDir,
			Validator:           validatorCfg,
			DryRun:              deployDryRun,
			SkipHealthCheck:     deploySkipHealthCheck,
			HealthCheckTimeout:  deployHealthCheckTimeout,
			HealthCheckInterval: deployHealthCheckPoll,
			VolumeSize:          volumeSize,
			SkipVolume:          deploySkipVolume,
		}

		provider, err := providers.NewProvider(providerName)
		if err != nil {
			return err
		}
		if !deployDryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "🚀 Deploying validator via %s...\n", providerName)
		}
		deployment, err := provider.Deploy(cmd.Context(), cfg)
		if err != nil {
			return err
		}

		if deployDryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "dry run complete; deployment files generated")
			fmt.Fprintf(cmd.OutOrStdout(), "app:          %s\n", deployment.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "artifacts:    %s\n", deployment.ArtifactsDir)
			fmt.Fprintf(cmd.OutOrStdout(), "rpc endpoint: %s\n", deployment.RPCURL)
			fmt.Fprintf(cmd.OutOrStdout(), "ws endpoint:  %s\n", deployment.WebSocketURL)
			fmt.Fprintf(cmd.OutOrStdout(), "validator:    slots_per_epoch=%d ticks_per_slot=%d compute_unit_limit=%d ledger_limit_size=%d clone=%d clone_upgradeable_program=%d\n",
				validatorCfg.SlotsPerEpoch,
				validatorCfg.TicksPerSlot,
				validatorCfg.ComputeUnitLimit,
				validatorCfg.LedgerLimitSize,
				len(validatorCfg.CloneAccounts),
				len(validatorCfg.CloneUpgradeablePrograms),
			)
			if validatorCfg.ProgramDeploy.Enabled() {
				fmt.Fprintf(cmd.OutOrStdout(), "program:      so=%s program_id_keypair=%s upgrade_authority=%s\n",
					validatorCfg.ProgramDeploy.SOPath,
					validatorCfg.ProgramDeploy.ProgramIDKeypairPath,
					validatorCfg.ProgramDeploy.UpgradeAuthorityPath,
				)
			}
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), "✅ Validator deployed!")
		fmt.Fprintln(cmd.OutOrStdout(), "🎉 Your Solana RPC + WebSocket endpoints are ready to use:")
		fmt.Fprintf(cmd.OutOrStdout(), "📡 Solana RPC: %s\n", deployment.RPCURL)
		fmt.Fprintf(cmd.OutOrStdout(), "🔌 Solana WS:  %s\n", deployment.WebSocketURL)
		if deployment.DashboardURL != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "🌐 Dashboard:  %s\n", deployment.DashboardURL)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "🧩 App:        %s\n", deployment.Name)
		fmt.Fprintf(cmd.OutOrStdout(), "📁 Artifacts:  %s\n", deployment.ArtifactsDir)
		fmt.Fprintf(cmd.OutOrStdout(), "⚙️ Validator:  slots_per_epoch=%d ticks_per_slot=%d compute_unit_limit=%d ledger_limit_size=%d clone=%d clone_upgradeable_program=%d\n",
			validatorCfg.SlotsPerEpoch,
			validatorCfg.TicksPerSlot,
			validatorCfg.ComputeUnitLimit,
			validatorCfg.LedgerLimitSize,
			len(validatorCfg.CloneAccounts),
			len(validatorCfg.CloneUpgradeablePrograms),
		)
		if validatorCfg.ProgramDeploy.Enabled() {
			fmt.Fprintf(cmd.OutOrStdout(), "program:      so=%s program_id_keypair=%s upgrade_authority=%s\n",
				validatorCfg.ProgramDeploy.SOPath,
				validatorCfg.ProgramDeploy.ProgramIDKeypairPath,
				validatorCfg.ProgramDeploy.UpgradeAuthorityPath,
			)
		}

		state, err := appconfig.LoadState(projectDir)
		if err != nil {
			return fmt.Errorf("load local deployment state: %w", err)
		}
		if err := state.UpsertDeployment(appconfig.DeploymentRecord{
			Name:         deployment.Name,
			Provider:     deployment.Provider,
			RPCURL:       deployment.RPCURL,
			WebSocketURL: deployment.WebSocketURL,
			Region:       region,
			ArtifactsDir: deployment.ArtifactsDir,
			DashboardURL: deployment.DashboardURL,
		}); err != nil {
			return fmt.Errorf("update local deployment state: %w", err)
		}
		if err := appconfig.SaveState(projectDir, state); err != nil {
			return fmt.Errorf("save local deployment state: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "🗂️ State file: %s\n", appconfig.StateFilePath(projectDir))
		fmt.Fprintf(cmd.OutOrStdout(), "💡 Tip:        solana config set --url %s\n", deployment.RPCURL)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&deployRegion, "region", "", "Fly region (overrides region in config)")
	deployCmd.Flags().StringVar(&deployOrg, "org", "", "Org/team identifier (fly: org slug; railway: team id)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "render files but skip API deployment")
	deployCmd.Flags().BoolVar(&deploySkipHealthCheck, "skip-health-check", false, "skip post-deploy RPC health validation")
	deployCmd.Flags().DurationVar(&deployHealthCheckTimeout, "health-timeout", 3*time.Minute, "maximum wait for RPC health")
	deployCmd.Flags().DurationVar(&deployHealthCheckPoll, "health-interval", 5*time.Second, "poll interval for RPC health checks")
	deployCmd.Flags().Uint64Var(&deploySlotsPerEpoch, "slots-per-epoch", 0, "override validator.slots_per_epoch")
	deployCmd.Flags().Uint64Var(&deployTicksPerSlot, "ticks-per-slot", 0, "override validator.ticks_per_slot")
	deployCmd.Flags().Uint64Var(&deployComputeUnitLimit, "compute-unit-limit", 0, "override validator.compute_unit_limit")
	deployCmd.Flags().Uint64Var(&deployLedgerLimitSize, "ledger-limit-size", 0, "override validator.ledger_limit_size")
	deployCmd.Flags().StringSliceVar(&deployCloneAccounts, "clone", nil, "repeatable account pubkey(s) to pass as --clone to solana-test-validator")
	deployCmd.Flags().StringSliceVar(&deployCloneUpPrograms, "clone-upgradeable-program", nil, "repeatable program pubkey(s) to pass as --clone-upgradeable-program")
	deployCmd.Flags().StringVar(&deployProgramSOPath, "program-so", "", "path to .so file to deploy on validator startup (overrides validator.program_deploy.so_path)")
	deployCmd.Flags().StringVar(&deployProgramIDKeypair, "program-id-keypair", "", "path to program id keypair used with --program-id during startup deploy (overrides validator.program_deploy.program_id_keypair)")
	deployCmd.Flags().StringVar(&deployProgramIDLegacy, "program-id", "", "deprecated alias for --program-id-keypair")
	_ = deployCmd.Flags().MarkDeprecated("program-id", "use --program-id-keypair with a keypair path")
	deployCmd.Flags().StringVar(&deployUpgradeAuthority, "upgrade-authority", "", "path to upgrade authority keypair (overrides validator.program_deploy.upgrade_authority)")
	deployCmd.Flags().IntVar(&deployVolumeSize, "volume-size", 10, "size of persistent ledger volume in GB")
	deployCmd.Flags().BoolVar(&deploySkipVolume, "skip-volume", false, "skip volume creation, use ephemeral storage (data loss on restart)")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

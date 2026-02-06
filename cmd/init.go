package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
)

var initForce bool
var errManualRegionSelection = errors.New("manual region selection requested")

type flyRegionOption struct {
	Code string
	Name string
}

var flyRegionOptions = []flyRegionOption{
	{Code: "ord", Name: "Chicago"},
	{Code: "iad", Name: "Ashburn"},
	{Code: "dfw", Name: "Dallas"},
	{Code: "lax", Name: "Los Angeles"},
	{Code: "sjc", Name: "San Jose"},
	{Code: "sea", Name: "Seattle"},
	{Code: "mia", Name: "Miami"},
	{Code: "atl", Name: "Atlanta"},
	{Code: "bos", Name: "Boston"},
	{Code: "yyz", Name: "Toronto"},
	{Code: "gru", Name: "Sao Paulo"},
	{Code: "eze", Name: "Buenos Aires"},
	{Code: "scl", Name: "Santiago"},
	{Code: "lhr", Name: "London"},
	{Code: "ams", Name: "Amsterdam"},
	{Code: "fra", Name: "Frankfurt"},
	{Code: "mad", Name: "Madrid"},
	{Code: "cdg", Name: "Paris"},
	{Code: "waw", Name: "Warsaw"},
	{Code: "otp", Name: "Bucharest"},
	{Code: "bom", Name: "Mumbai"},
	{Code: "sin", Name: "Singapore"},
	{Code: "nrt", Name: "Tokyo"},
	{Code: "hkg", Name: "Hong Kong"},
	{Code: "syd", Name: "Sydney"},
	{Code: "jnb", Name: "Johannesburg"},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Run interactive setup and create .sol-cloud.yml",
	Long:  "Run an interactive setup flow and write a .sol-cloud.yml file in the current directory.",
	Example: `  sol-cloud init
  sol-cloud init --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		const file = ".sol-cloud.yml"
		out := cmd.OutOrStdout()
		reader := bufio.NewReader(cmd.InOrStdin())

		if _, err := os.Stat(file); err == nil {
			if !initForce {
				overwrite, promptErr := promptYesNo(reader, out, ".sol-cloud.yml already exists. Overwrite it?", false)
				if promptErr != nil {
					return promptErr
				}
				if !overwrite {
					fmt.Fprintln(out, "init cancelled; existing .sol-cloud.yml was not changed")
					return nil
				}
			}
		}

		fmt.Fprintln(out, "Sol-Cloud setup")
		fmt.Fprintln(out, "Press Enter to accept defaults.")
		fmt.Fprintln(out)

		appName, err := generateFlyAppName()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Fly app name: %s\n", appName)
		region, err := promptFlyRegion(cmd.InOrStdin(), reader, out, "ord")
		if err != nil {
			return err
		}

		cfg := validator.DefaultConfig()
		customizeRuntime, err := promptYesNo(reader, out, "Customize validator runtime settings?", false)
		if err != nil {
			return err
		}
		if customizeRuntime {
			cfg.SlotsPerEpoch, err = promptUint64(reader, out, "Slots per epoch", cfg.SlotsPerEpoch, true)
			if err != nil {
				return err
			}
			cfg.TicksPerSlot, err = promptUint64(reader, out, "Ticks per slot", cfg.TicksPerSlot, true)
			if err != nil {
				return err
			}
			cfg.ComputeUnitLimit, err = promptUint64(reader, out, "Compute unit limit", cfg.ComputeUnitLimit, true)
			if err != nil {
				return err
			}
			cfg.LedgerLimitSize, err = promptUint64(reader, out, "Ledger limit size", cfg.LedgerLimitSize, true)
			if err != nil {
				return err
			}
		}

		cloneAccounts, err := promptStringList(
			reader,
			out,
			"Clone account list (optional)",
			"Clone account",
		)
		if err != nil {
			return err
		}
		cfg.CloneAccounts = cloneAccounts

		clonePrograms, err := promptStringList(
			reader,
			out,
			"Clone upgradeable program list (optional)",
			"Clone upgradeable program",
		)
		if err != nil {
			return err
		}
		cfg.CloneUpgradeablePrograms = clonePrograms

		configureStartupDeploy, err := promptYesNo(reader, out, "Configure startup program deploy?", false)
		if err != nil {
			return err
		}
		if configureStartupDeploy {
			soPath, promptErr := promptString(reader, out, "Program .so path", "", true)
			if promptErr != nil {
				return promptErr
			}
			programIDKeypair, promptErr := promptString(reader, out, "Program ID keypair path", "", true)
			if promptErr != nil {
				return promptErr
			}
			upgradeAuthorityPath, promptErr := promptString(reader, out, "Upgrade authority keypair path", "", true)
			if promptErr != nil {
				return promptErr
			}
			cfg.ProgramDeploy = validator.ProgramDeployConfig{
				SOPath:               soPath,
				ProgramIDKeypairPath: programIDKeypair,
				UpgradeAuthorityPath: upgradeAuthorityPath,
			}
		}

		cfg.ApplyDefaults()
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid setup values: %w", err)
		}

		escapedAppName := strings.ReplaceAll(appName, `"`, `\"`)
		escapedRegion := strings.ReplaceAll(region, `"`, `\"`)
		cloneAccountsYAML := renderYAMLStringList(cfg.CloneAccounts, "    ")
		cloneProgramsYAML := renderYAMLStringList(cfg.CloneUpgradeablePrograms, "    ")
		escapedSOPath := strings.ReplaceAll(cfg.ProgramDeploy.SOPath, `"`, `\"`)
		escapedProgramIDKeypair := strings.ReplaceAll(cfg.ProgramDeploy.ProgramIDKeypairPath, `"`, `\"`)
		escapedUpgradeAuth := strings.ReplaceAll(cfg.ProgramDeploy.UpgradeAuthorityPath, `"`, `\"`)

		starter := fmt.Sprintf(`provider: fly
app_name: "%s"
region: "%s"
validator:
  slots_per_epoch: %d
  ticks_per_slot: %d
  compute_unit_limit: %d
  ledger_limit_size: %d
  clone_accounts:
%s
  clone_upgradeable_programs:
%s
  program_deploy:
    so_path: "%s"
    program_id_keypair: "%s"
    upgrade_authority: "%s"
`, escapedAppName, escapedRegion, cfg.SlotsPerEpoch, cfg.TicksPerSlot, cfg.ComputeUnitLimit, cfg.LedgerLimitSize, cloneAccountsYAML, cloneProgramsYAML, escapedSOPath, escapedProgramIDKeypair, escapedUpgradeAuth)

		if err := os.WriteFile(file, []byte(starter), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Fprintf(out, "created %s\n", file)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite .sol-cloud.yml if it already exists")
}

func renderYAMLStringList(values []string, indent string) string {
	if len(values) == 0 {
		return indent + "[]"
	}
	lines := make([]string, 0, len(values))
	for _, value := range values {
		escaped := strings.ReplaceAll(value, `"`, `\"`)
		lines = append(lines, fmt.Sprintf("%s- \"%s\"", indent, escaped))
	}
	return strings.Join(lines, "\n")
}

func promptString(reader *bufio.Reader, out io.Writer, label, defaultValue string, required bool) (string, error) {
	for {
		if strings.TrimSpace(defaultValue) == "" {
			fmt.Fprintf(out, "%s: ", label)
		} else {
			fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}

		value := strings.TrimSpace(line)
		if value == "" {
			value = strings.TrimSpace(defaultValue)
		}
		if required && value == "" {
			fmt.Fprintln(out, "value is required")
			if err == io.EOF {
				return "", io.ErrUnexpectedEOF
			}
			continue
		}
		return value, nil
	}
}

func promptStringList(reader *bufio.Reader, out io.Writer, title, itemLabel string) ([]string, error) {
	values := make([]string, 0)
	fmt.Fprintf(out, "%s\n", title)
	fmt.Fprintln(out, "Press Enter on an empty line to finish.")

	for {
		value, err := promptString(reader, out, fmt.Sprintf("%s #%d", itemLabel, len(values)+1), "", false)
		if err != nil {
			return nil, err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return values, nil
		}
		values = append(values, value)
	}
}

func promptYesNo(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultLabel)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}

		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "enter y or n")
			if err == io.EOF {
				return false, io.ErrUnexpectedEOF
			}
		}
	}
}

func promptUint64(reader *bufio.Reader, out io.Writer, label string, defaultValue uint64, required bool) (uint64, error) {
	for {
		value, err := promptString(reader, out, label, strconv.FormatUint(defaultValue, 10), required)
		if err != nil {
			return 0, err
		}
		if value == "" && !required {
			return 0, nil
		}

		parsed, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil {
			fmt.Fprintln(out, "enter a valid positive integer")
			continue
		}
		return parsed, nil
	}
}

func promptFlyRegion(in io.Reader, reader *bufio.Reader, out io.Writer, defaultRegion string) (string, error) {
	defaultRegion = strings.ToLower(strings.TrimSpace(defaultRegion))
	if defaultRegion == "" {
		defaultRegion = "ord"
	}

	region, err := promptFlyRegionArrow(in, out, defaultRegion)
	if err == nil {
		return region, nil
	}
	if errors.Is(err, errManualRegionSelection) {
		custom, promptErr := promptString(reader, out, "Custom Fly region code", defaultRegion, true)
		if promptErr != nil {
			return "", promptErr
		}
		return strings.ToLower(strings.TrimSpace(custom)), nil
	}

	// Fallback for non-interactive terminals: simple code input.
	custom, promptErr := promptString(reader, out, "Fly region code", defaultRegion, true)
	if promptErr != nil {
		return "", promptErr
	}
	return strings.ToLower(strings.TrimSpace(custom)), nil
}

func promptFlyRegionArrow(in io.Reader, out io.Writer, defaultRegion string) (string, error) {
	inFile, ok := in.(*os.File)
	if !ok {
		return "", errors.New("input is not a terminal file")
	}
	if _, ok := out.(*os.File); !ok {
		return "", errors.New("output is not a terminal file")
	}

	restore, err := enableRawTerminalMode(inFile)
	if err != nil {
		return "", err
	}
	defer restore()

	selected := findRegionIndex(defaultRegion)
	if selected < 0 {
		selected = 0
	}
	search := ""

	fmt.Fprintln(out, "Fly region selector: ↑/↓ to move, Enter to confirm, type to filter, Backspace to edit, m for manual")
	for {
		drawCurrentRegion(out, selected, search)

		key, text, readErr := readSelectionKey(inFile)
		if readErr != nil {
			return "", readErr
		}

		switch key {
		case "up":
			selected--
			if selected < 0 {
				selected = len(flyRegionOptions) - 1
			}
		case "down":
			selected++
			if selected >= len(flyRegionOptions) {
				selected = 0
			}
		case "text":
			search += text
			if idx := findRegionMatch(search); idx >= 0 {
				selected = idx
			}
		case "backspace":
			if len(search) > 0 {
				search = search[:len(search)-1]
			}
			if idx := findRegionMatch(search); idx >= 0 {
				selected = idx
			}
		case "enter":
			fmt.Fprint(out, "\r\n")
			return flyRegionOptions[selected].Code, nil
		case "manual":
			fmt.Fprint(out, "\r\n")
			return "", errManualRegionSelection
		case "cancel":
			fmt.Fprint(out, "\r\n")
			return "", errors.New("init cancelled")
		}
	}
}

func drawCurrentRegion(out io.Writer, selected int, search string) {
	option := flyRegionOptions[selected]
	fmt.Fprintf(out, "\r\033[2KFly region: %s (%s)", option.Code, option.Name)
	if strings.TrimSpace(search) != "" {
		fmt.Fprintf(out, " | filter: %s", search)
	}
}

func findRegionIndex(code string) int {
	for i, option := range flyRegionOptions {
		if option.Code == code {
			return i
		}
	}
	return -1
}

func findRegionMatch(query string) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return -1
	}

	for i, option := range flyRegionOptions {
		if option.Code == query {
			return i
		}
	}
	for i, option := range flyRegionOptions {
		if strings.HasPrefix(option.Code, query) {
			return i
		}
	}
	for i, option := range flyRegionOptions {
		if strings.Contains(strings.ToLower(option.Name), query) {
			return i
		}
	}
	return -1
}

func readSelectionKey(inFile *os.File) (string, string, error) {
	buf := make([]byte, 3)
	n, err := inFile.Read(buf[:1])
	if err != nil {
		return "", "", err
	}
	if n == 0 {
		return "", "", io.EOF
	}

	switch buf[0] {
	case '\r', '\n':
		return "enter", "", nil
	case 3:
		return "cancel", "", nil
	case 127, 8:
		return "backspace", "", nil
	case 'm', 'M':
		return "manual", "", nil
	case 27:
		_, err = inFile.Read(buf[1:2])
		if err != nil {
			return "", "", err
		}
		if buf[1] != '[' {
			return "", "", nil
		}
		_, err = inFile.Read(buf[2:3])
		if err != nil {
			return "", "", err
		}
		switch buf[2] {
		case 'A':
			return "up", "", nil
		case 'B':
			return "down", "", nil
		default:
			return "", "", nil
		}
	case 'k', 'K':
		return "up", "", nil
	case 'j', 'J':
		return "down", "", nil
	default:
		ch := strings.ToLower(string(buf[0]))
		if (buf[0] >= 'a' && buf[0] <= 'z') || (buf[0] >= 'A' && buf[0] <= 'Z') || (buf[0] >= '0' && buf[0] <= '9') || buf[0] == '-' {
			return "text", ch, nil
		}
		return "", "", nil
	}
}

func enableRawTerminalMode(inFile *os.File) (func(), error) {
	state, err := sttyGetState(inFile)
	if err != nil {
		return nil, err
	}
	if err := sttySet(inFile, "raw", "-echo"); err != nil {
		return nil, err
	}

	restore := func() {
		_ = sttySet(inFile, state)
	}
	return restore, nil
}

func sttyGetState(inFile *os.File) (string, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = inFile
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func sttySet(inFile *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = inFile
	return cmd.Run()
}

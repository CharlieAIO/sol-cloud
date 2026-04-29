package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/monitor"
	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/CharlieAIO/sol-cloud/internal/ui"
	"github.com/spf13/cobra"
)

var (
	watchName            string
	watchCheckInterval   time.Duration
	watchStuckThreshold  time.Duration
	watchMaxRestarts     int
	watchRestartCooldown time.Duration
	watchAutoRestart     bool
)

var watchCmd = &cobra.Command{
	Use:   "watch [name]",
	Short: "Watch validator and restart if stuck",
	Long: `Monitor a deployed validator for stuck slot progression and automatically restart when detected.

The watcher polls the validator's RPC endpoint at regular intervals to check slot progression.
If the slot hasn't changed for longer than the stuck threshold, it triggers a restart.

By default, the watcher requires user confirmation before restarting. Use --auto-restart to skip prompts.`,
	Example: `  # Watch the last deployed validator (interactive mode)
  sol-cloud watch

  # Watch a specific validator with automatic restarts
  sol-cloud watch my-validator --auto-restart

  # Aggressive monitoring for production
  sol-cloud watch --auto-restart --stuck-threshold 2m --check-interval 20s

  # Run in background
  nohup sol-cloud watch --auto-restart > watcher.log 2>&1 &`,
	Args: cobra.MaximumNArgs(1),
	RunE: runWatch,
}

func init() {
	rootCmd.AddCommand(watchCmd)

	watchCmd.Flags().StringVar(&watchName, "name", "", "Deployment name (defaults to last deployment)")
	watchCmd.Flags().DurationVar(&watchCheckInterval, "check-interval", 30*time.Second, "Polling frequency for slot checks")
	watchCmd.Flags().DurationVar(&watchStuckThreshold, "stuck-threshold", 3*time.Minute, "Duration before slot is considered stuck")
	watchCmd.Flags().IntVar(&watchMaxRestarts, "max-restarts", 0, "Maximum restart attempts (0 = unlimited)")
	watchCmd.Flags().DurationVar(&watchRestartCooldown, "restart-cooldown", 2*time.Minute, "Minimum time between restarts")
	watchCmd.Flags().BoolVar(&watchAutoRestart, "auto-restart", false, "Skip confirmation prompts and restart automatically")
}

func runWatch(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(watchName)
	if name == "" && len(args) > 0 {
		name = strings.TrimSpace(args[0])
	}

	projectDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	state, err := appconfig.LoadState(projectDir)
	if err != nil {
		return fmt.Errorf("load local deployment state: %w", err)
	}

	record, err := resolveStatusRecord(state, name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(record.Provider) == "" {
		record.Provider = "fly"
	}

	out := cmd.OutOrStdout()
	ui.Header(out, "Watch")
	if watchMaxRestarts > 0 {
		ui.Fields(out,
			ui.Field{Label: "Validator", Value: record.Name},
			ui.Field{Label: "RPC", Value: record.RPCURL},
			ui.Field{Label: "Check interval", Value: watchCheckInterval.String()},
			ui.Field{Label: "Stuck threshold", Value: watchStuckThreshold.String()},
			ui.Field{Label: "Max restarts", Value: fmt.Sprintf("%d", watchMaxRestarts)},
			ui.Field{Label: "Restart cooldown", Value: watchRestartCooldown.String()},
			ui.Field{Label: "Auto-restart", Value: fmt.Sprintf("%t", watchAutoRestart)},
		)
	} else {
		ui.Fields(out,
			ui.Field{Label: "Validator", Value: record.Name},
			ui.Field{Label: "RPC", Value: record.RPCURL},
			ui.Field{Label: "Check interval", Value: watchCheckInterval.String()},
			ui.Field{Label: "Stuck threshold", Value: watchStuckThreshold.String()},
			ui.Field{Label: "Max restarts", Value: "unlimited"},
			ui.Field{Label: "Restart cooldown", Value: watchRestartCooldown.String()},
			ui.Field{Label: "Auto-restart", Value: fmt.Sprintf("%t", watchAutoRestart)},
		)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Press Ctrl+C to stop watching")
	fmt.Fprintln(out)

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	watchProvider, err := providers.NewProvider(record.Provider)
	if err != nil {
		return fmt.Errorf("create provider for watch: %w", err)
	}

	watcher := &ValidatorWatcher{
		name:            record.Name,
		rpcURL:          record.RPCURL,
		provider:        watchProvider,
		history:         monitor.NewSlotHistory(watchStuckThreshold),
		checkInterval:   watchCheckInterval,
		maxRestarts:     watchMaxRestarts,
		restartCooldown: watchRestartCooldown,
		autoRestart:     watchAutoRestart,
		input:           cmd.InOrStdin(),
		output:          out,
	}

	return watcher.Run(ctx)
}

// ValidatorWatcher monitors a validator and restarts it when stuck.
type ValidatorWatcher struct {
	name            string
	rpcURL          string
	provider        providers.Provider
	history         *monitor.SlotHistory
	checkInterval   time.Duration
	maxRestarts     int
	restartCooldown time.Duration
	autoRestart     bool
	input           io.Reader
	output          interface{ Write([]byte) (int, error) }

	restartCount    int
	lastRestartTime time.Time
}

// Run starts the watch loop and blocks until context is cancelled or max restarts reached.
func (w *ValidatorWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(w.output, "\nWatcher stopped")
			return nil

		case <-ticker.C:
			if err := w.checkAndRestart(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				fmt.Fprintf(w.output, "warn [%s] check error: %v\n", time.Now().Format("15:04:05"), err)
			}

			// Check if max restarts reached
			if w.maxRestarts > 0 && w.restartCount >= w.maxRestarts {
				fmt.Fprintf(w.output, "\nMax restarts (%d) reached, stopping watcher\n", w.maxRestarts)
				return nil
			}
		}
	}
}

func (w *ValidatorWatcher) checkAndRestart(ctx context.Context) error {
	// Fetch current slot
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var slot uint64
	if err := rpcCall(checkCtx, w.rpcURL, "getSlot", []any{}, &slot); err != nil {
		// RPC unreachable - don't treat as stuck, just log warning
		fmt.Fprintf(w.output, "warn [%s] RPC unreachable: %v\n", time.Now().Format("15:04:05"), err)
		return nil
	}

	// Record slot observation
	w.history.Record(slot)

	// Check if stuck
	stuck, info := w.history.IsStuck()
	if !stuck {
		if w.history.HasProgressed() {
			fmt.Fprintf(w.output, "ok   [%s] slot=%d progressing\n", time.Now().Format("15:04:05"), slot)
		} else {
			fmt.Fprintf(w.output, "wait [%s] slot=%d waiting for progression\n", time.Now().Format("15:04:05"), slot)
		}
		return nil
	}

	// Validator is stuck
	fmt.Fprintf(w.output, "\nalert [%s] stuck detected: %s\n", time.Now().Format("15:04:05"), info.String())

	// Check cooldown
	if !w.lastRestartTime.IsZero() {
		elapsed := time.Since(w.lastRestartTime)
		if elapsed < w.restartCooldown {
			remaining := w.restartCooldown - elapsed
			fmt.Fprintf(w.output, "wait restart cooldown active, remaining=%s\n", remaining.Round(time.Second))
			return nil
		}
	}

	// Confirm restart if not auto mode
	if !w.autoRestart {
		fmt.Fprintf(w.output, "\nRestart validator %q? [y/N]: ", w.name)
		reader := bufio.NewReader(w.input)
		response, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read restart confirmation: %w", err)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Fprintln(w.output, "restart skipped")
			return nil
		}
	}

	// Perform restart
	fmt.Fprintf(w.output, "run  [%s] restarting validator %q\n", time.Now().Format("15:04:05"), w.name)

	restartCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	if err := w.provider.Restart(restartCtx, w.name); err != nil {
		fmt.Fprintf(w.output, "fail restart failed: %v\n", err)
		return fmt.Errorf("restart failed: %w", err)
	}

	w.restartCount++
	w.lastRestartTime = time.Now()

	fmt.Fprintf(w.output, "done [%s] restart successful restart=%d\n", time.Now().Format("15:04:05"), w.restartCount)
	fmt.Fprintln(w.output, "wait validator recovery")
	fmt.Fprintln(w.output, "")

	return nil
}

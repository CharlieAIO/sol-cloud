package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/CharlieAIO/sol-cloud/internal/ui"
	"github.com/spf13/cobra"
)

var (
	statusName    string
	statusTimeout time.Duration
)

var statusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Get validator status",
	Long:  "Show status and health details for a deployed validator.",
	Example: `  sol-cloud status
  sol-cloud status --timeout 30s`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.TrimSpace(statusName)
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
		progress := ui.NewProgress(out, 2)
		progress.Start("Checking provider status")

		providerState := "unknown"
		providerErrText := ""
		provider, providerErr := providers.NewProvider(record.Provider)
		if providerErr != nil {
			providerErrText = providerErr.Error()
		}
		var providerStatus *providers.Status
		if provider != nil {
			var statusErr error
			providerStatus, statusErr = provider.Status(cmd.Context(), record.Name)
			if statusErr != nil {
				providerErrText = statusErr.Error()
			}
		}
		if providerStatus != nil {
			providerState = providerStatus.State
		}

		progress.Step("Checking RPC metrics")
		slot, tps, healthText := "n/a", "n/a", "unreachable"
		statusCtx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
		defer cancel()

		metrics, err := fetchRPCMetrics(statusCtx, record.RPCURL)
		if err == nil {
			slot = fmt.Sprintf("%d", metrics.Slot)
			tps = fmt.Sprintf("%.2f", metrics.TPS)
			healthText = "ok"
		} else if !errors.Is(err, context.DeadlineExceeded) {
			healthText = err.Error()
		} else {
			healthText = "timeout"
		}

		statusLabel := providerState
		if strings.EqualFold(providerState, "running") {
			statusLabel = "running"
		}

		progress.Success("Status loaded")

		operationText := ""
		if operation, ok := state.LatestOperationFor(record.Name); ok {
			when := operation.UpdatedAt.Local().Format(time.RFC3339)
			operationText = fmt.Sprintf("%s %s at %s", operation.Type, operation.Status, when)
			if operation.Message != "" {
				operationText += " (" + operation.Message + ")"
			}
		}

		ui.Header(out, "Status")
		ui.Fields(out,
			ui.Field{Label: "Validator", Value: record.Name},
			ui.Field{Label: "Provider", Value: record.Provider},
			ui.Field{Label: "State", Value: statusLabel},
			ui.Field{Label: "Health", Value: healthText},
			ui.Field{Label: "Slot", Value: slot},
			ui.Field{Label: "TPS", Value: tps},
			ui.Field{Label: "RPC", Value: record.RPCURL},
			ui.Field{Label: "WebSocket", Value: record.WebSocketURL},
			ui.Field{Label: "Dashboard", Value: record.DashboardURL},
			ui.Field{Label: "Last operation", Value: operationText},
		)
		if providerErrText != "" {
			ui.Fields(out, ui.Field{Label: "Provider warning", Value: providerErrText})
		}
		return nil
	},
}

type rpcMetrics struct {
	Slot uint64
	TPS  float64
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().StringVar(&statusName, "name", "", "Deployment name (defaults to last deployment from local state)")
	statusCmd.Flags().DurationVar(&statusTimeout, "timeout", 20*time.Second, "Timeout for RPC metric queries")
}

func resolveStatusRecord(state *appconfig.State, name string) (appconfig.DeploymentRecord, error) {
	record, err := state.ResolveDeployment(name)
	if err == nil {
		return record, nil
	}
	if strings.TrimSpace(name) == "" {
		if errors.Is(err, appconfig.ErrNoDeployments) {
			return appconfig.DeploymentRecord{}, errors.New("no deployments found; run `sol-cloud deploy` or pass --name")
		}
		return appconfig.DeploymentRecord{}, err
	}
	if !errors.Is(err, appconfig.ErrDeploymentNotFound) {
		return appconfig.DeploymentRecord{}, err
	}

	// Allow querying an app by explicit name even if it is not in local state.
	// Default to fly provider for backward compatibility.
	return appconfig.DeploymentRecord{
		Name:         name,
		Provider:     "fly",
		RPCURL:       fmt.Sprintf("https://%s.fly.dev", name),
		WebSocketURL: fmt.Sprintf("wss://%s.fly.dev", name),
	}, nil
}

func fetchRPCMetrics(ctx context.Context, rpcURL string) (*rpcMetrics, error) {
	var health string
	if err := rpcCall(ctx, rpcURL, "getHealth", nil, &health); err != nil {
		return nil, err
	}
	if health != "ok" {
		return nil, fmt.Errorf("unexpected health response: %s", health)
	}

	var slot uint64
	if err := rpcCall(ctx, rpcURL, "getSlot", []any{}, &slot); err != nil {
		return nil, err
	}

	var samples []struct {
		NumTransactions uint64 `json:"numTransactions"`
		SamplePeriodSec uint64 `json:"samplePeriodSecs"`
	}
	if err := rpcCall(ctx, rpcURL, "getRecentPerformanceSamples", []any{1}, &samples); err != nil {
		return nil, err
	}
	if len(samples) == 0 || samples[0].SamplePeriodSec == 0 {
		return nil, errors.New("missing performance samples")
	}

	tps := float64(samples[0].NumTransactions) / float64(samples[0].SamplePeriodSec)
	return &rpcMetrics{Slot: slot, TPS: tps}, nil
}

func rpcCall(ctx context.Context, rpcURL, method string, params any, result any) error {
	if strings.TrimSpace(rpcURL) == "" {
		return errors.New("rpc url is required")
	}
	if params == nil {
		params = []any{}
	}

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return fmt.Errorf("marshal rpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("rpc request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("decode rpc response: %w", err)
	}
	if decoded.Error != nil {
		return fmt.Errorf("rpc error %d: %s", decoded.Error.Code, decoded.Error.Message)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(decoded.Result, result); err != nil {
		return fmt.Errorf("decode rpc result: %w", err)
	}
	return nil
}

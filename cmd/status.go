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
	Args:  cobra.MaximumNArgs(1),
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
		if record.Provider != "fly" {
			return fmt.Errorf("unsupported provider %q: only fly is enabled", record.Provider)
		}
		if strings.TrimSpace(record.RPCURL) == "" {
			record.RPCURL = fmt.Sprintf("https://%s.fly.dev", record.Name)
		}
		if strings.TrimSpace(record.WebSocketURL) == "" {
			record.WebSocketURL = fmt.Sprintf("wss://%s.fly.dev", record.Name)
		}

		providerState := "unknown"
		providerErrText := ""
		provider := providers.NewFlyProvider()
		providerStatus, err := provider.Status(cmd.Context(), record.Name)
		if err == nil {
			providerState = providerStatus.State
		} else {
			providerErrText = err.Error()
		}

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
			statusLabel = "Running ✓"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Validator: %s\n", record.Name)
		fmt.Fprintf(cmd.OutOrStdout(), "Provider:  %s\n", record.Provider)
		fmt.Fprintf(cmd.OutOrStdout(), "Status:    %s\n", statusLabel)
		fmt.Fprintf(cmd.OutOrStdout(), "Health:    %s\n", healthText)
		fmt.Fprintf(cmd.OutOrStdout(), "Slot:      %s\n", slot)
		fmt.Fprintf(cmd.OutOrStdout(), "TPS:       %s\n", tps)
		fmt.Fprintf(cmd.OutOrStdout(), "RPC:       %s\n", record.RPCURL)
		fmt.Fprintf(cmd.OutOrStdout(), "WebSocket: %s\n", record.WebSocketURL)
		if providerErrText != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Provider check warning: %s\n", providerErrText)
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

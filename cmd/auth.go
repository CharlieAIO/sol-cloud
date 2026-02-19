package cmd

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/CharlieAIO/sol-cloud/internal/utils"
	"github.com/spf13/cobra"
)

var (
	authFlyToken      string
	authFlyOrg        string
	authFlySkipVerify bool

	authRailwayToken      string
	authRailwaySkipVerify bool
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage provider authentication",
}

var authFlyCmd = &cobra.Command{
	Use:   "fly",
	Short: "Connect Fly.io with a personal or org access token",
	Long: `Store a Fly access token for API-backed operations.
You can use either a personal token or an organization token.`,
	Example: `  sol-cloud auth fly
  sol-cloud auth fly --token "$FLY_ACCESS_TOKEN"
  sol-cloud auth fly --token "$FLY_ACCESS_TOKEN" --org my-team`,
	RunE: func(cmd *cobra.Command, args []string) error {
		creds, err := appconfig.LoadCredentials()
		if err != nil {
			return err
		}

		token := strings.TrimSpace(authFlyToken)
		reader := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()

		if token == "" {
			fmt.Fprintln(out, "Create a Fly access token (personal or organization):")
			fmt.Fprintln(out, "https://fly.io/user/personal_access_tokens")
			fmt.Fprintln(out, "For organization tokens, use your org token page in Fly dashboard.")
			fmt.Fprintln(out)

			token, err = utils.String(reader, out, "Fly access token", "", true)
			if err != nil {
				return err
			}
			token = strings.TrimSpace(token)
		}

		org := strings.TrimSpace(authFlyOrg)
		if org == "" {
			defaultOrg := strings.TrimSpace(creds.Fly.OrgSlug)
			if defaultOrg == "" {
				defaultOrg = "personal"
			}
			org, err = utils.String(reader, out, "Default Fly org slug", defaultOrg, true)
			if err != nil {
				return err
			}
			org = strings.TrimSpace(org)
		}

		if !authFlySkipVerify {
			verifyCtx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			provider := providers.NewFlyProvider()
			if err := provider.VerifyAccessToken(verifyCtx, token); err != nil {
				return fmt.Errorf("verify fly token: %w", err)
			}
		}

		creds.Fly.AccessToken = token
		creds.Fly.OrgSlug = org
		if !authFlySkipVerify {
			creds.Fly.VerifiedAt = time.Now().UTC()
		}
		if err := appconfig.SaveCredentials(creds); err != nil {
			return err
		}

		_, err = appconfig.CredentialsFilePath()
		if err != nil {
			return err
		}

		fmt.Fprintln(out, "Fly authentication saved.")
		return nil
	},
}

var authRailwayCmd = &cobra.Command{
	Use:   "railway",
	Short: "Connect Railway with an API token",
	Long: `Store a Railway API token for deployments.
Create a token at https://railway.app/account/tokens`,
	Example: `  sol-cloud auth railway
  sol-cloud auth railway --token "$RAILWAY_TOKEN"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		creds, err := appconfig.LoadCredentials()
		if err != nil {
			return err
		}

		token := strings.TrimSpace(authRailwayToken)
		reader := bufio.NewReader(cmd.InOrStdin())
		out := cmd.OutOrStdout()

		if token == "" {
			fmt.Fprintln(out, "Create a Railway API token:")
			fmt.Fprintln(out, "https://railway.app/account/tokens")
			fmt.Fprintln(out)

			token, err = utils.String(reader, out, "Railway API token", "", true)
			if err != nil {
				return err
			}
			token = strings.TrimSpace(token)
		}

		provider := providers.NewRailwayProvider()

		if !authRailwaySkipVerify {
			verifyCtx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()

			if err := provider.VerifyAccessToken(verifyCtx, token); err != nil {
				return fmt.Errorf("verify railway token: %w", err)
			}
		}

		// Fetch workspaces and save the ID so deploy never needs to prompt for it.
		workspaceID := ""
		wsCtx, wsCancel := context.WithTimeout(cmd.Context(), 15*time.Second)
		defer wsCancel()
		workspaces, wsErr := provider.ListWorkspaces(wsCtx, token)
		if wsErr != nil {
			fmt.Fprintf(out, "Note: could not auto-discover workspace (%v)\n", wsErr)
		}

		if len(workspaces) == 1 {
			workspaceID = workspaces[0].ID
			fmt.Fprintf(out, "Workspace: %s\n", workspaces[0].Name)
		} else if len(workspaces) > 1 {
			// Multiple workspaces — let the user pick.
			wsOptions := make([]utils.Option, len(workspaces))
			for i, ws := range workspaces {
				wsOptions[i] = utils.Option{Key: ws.ID, Label: ws.Name}
			}
			selected, selErr := utils.SelectOptionArrow(cmd.InOrStdin(), out, "Workspace", wsOptions, workspaces[0].ID)
			if selErr != nil {
				// Fall back to text prompt listing names.
				fmt.Fprintln(out, "Available workspaces:")
				for _, ws := range workspaces {
					fmt.Fprintf(out, "  %s  (%s)\n", ws.Name, ws.ID)
				}
				selected, selErr = utils.String(reader, out, "Workspace ID", workspaces[0].ID, true)
				if selErr != nil {
					return selErr
				}
			}
			workspaceID = strings.TrimSpace(selected)
		} else {
			// Auto-discovery failed — prompt the user.
			// The workspace ID appears in the Railway dashboard URL:
			// https://railway.com/workspace/<workspaceId>
			defaultID := strings.TrimSpace(creds.Railway.WorkspaceID)
			fmt.Fprintln(out, "Could not auto-discover workspace ID.")
			fmt.Fprintln(out, "Find it in your Railway dashboard URL: https://railway.com/workspace/<workspaceId>")
			fmt.Fprintln(out)
			entered, promptErr := utils.String(reader, out, "Railway workspace ID", defaultID, false)
			if promptErr != nil {
				return promptErr
			}
			workspaceID = strings.TrimSpace(entered)
		}

		creds.Railway.AccessToken = token
		creds.Railway.WorkspaceID = workspaceID
		if !authRailwaySkipVerify {
			creds.Railway.VerifiedAt = time.Now().UTC()
		}
		if err := appconfig.SaveCredentials(creds); err != nil {
			return err
		}

		fmt.Fprintln(out, "Railway authentication saved.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authFlyCmd)
	authCmd.AddCommand(authRailwayCmd)

	authFlyCmd.Flags().StringVar(&authFlyToken, "token", "", "Fly access token (optional; prompts if omitted)")
	authFlyCmd.Flags().StringVar(&authFlyOrg, "org", "", "Default Fly org slug to use for app creation (e.g. personal or your-org)")
	authFlyCmd.Flags().BoolVar(&authFlySkipVerify, "skip-verify", false, "Save token without contacting Fly API")

	authRailwayCmd.Flags().StringVar(&authRailwayToken, "token", "", "Railway API token (optional; prompts if omitted)")
	authRailwayCmd.Flags().BoolVar(&authRailwaySkipVerify, "skip-verify", false, "Save token without contacting Railway API")
}

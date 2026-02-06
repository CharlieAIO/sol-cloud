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

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authFlyCmd)

	authFlyCmd.Flags().StringVar(&authFlyToken, "token", "", "Fly access token (optional; prompts if omitted)")
	authFlyCmd.Flags().StringVar(&authFlyOrg, "org", "", "Default Fly org slug to use for app creation (e.g. personal or your-org)")
	authFlyCmd.Flags().BoolVar(&authFlySkipVerify, "skip-verify", false, "Save token without contacting Fly API")
}

/*
Copyright © 2025 Brian Tong
*/
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	githubapptoken "github.com/tongbpm/github-app-token"
)

var (
	appID          string
	installationID string
	privateKeyPath string
	privateKeyPEM  string
	apiURL         string
	timeout        time.Duration
)

var rootCmd = &cobra.Command{
	Use:   "github-app-token",
	Short: "Create a GitHub App installation access token",
	Long: `Create a short-lived access token for a GitHub App installation.

The app ID, installation ID, and private key can be provided with flags or
the GITHUB_APP_ID, GITHUB_INSTALLATION_ID, and GITHUB_PRIVATE_KEY
environment variables. GITHUB_PRIVATE_KEY may contain the PEM directly or
the path to a PEM file.`,
	Args: cobra.NoArgs,
	RunE: run,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&appID, "app-id", "", "GitHub App ID (or GITHUB_APP_ID)")
	rootCmd.Flags().StringVar(&installationID, "installation-id", "", "GitHub App installation ID (or GITHUB_INSTALLATION_ID)")
	rootCmd.Flags().StringVar(&privateKeyPath, "private-key", "", "path to the GitHub App PEM private key (or GITHUB_PRIVATE_KEY)")
	rootCmd.Flags().StringVar(&privateKeyPEM, "private-key-pem", "", "GitHub App PEM private key contents")
	rootCmd.Flags().StringVar(&apiURL, "api-url", "https://api.github.com", "GitHub API base URL")
	rootCmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")
}

func run(cmd *cobra.Command, _ []string) error {
	appID = valueOrEnvironment(appID, "GITHUB_APP_ID")
	installationID = valueOrEnvironment(installationID, "GITHUB_INSTALLATION_ID")
	keyInput := valueOrEnvironment(privateKeyPath, "GITHUB_PRIVATE_KEY")

	if privateKeyPEM != "" && keyInput != "" {
		return fmt.Errorf("provide only one of --private-key-pem and --private-key")
	}
	if privateKeyPEM != "" {
		keyInput = privateKeyPEM
	}
	if appID == "" {
		return fmt.Errorf("GitHub App ID is required; set --app-id or GITHUB_APP_ID")
	}
	if installationID == "" {
		return fmt.Errorf("GitHub installation ID is required; set --installation-id or GITHUB_INSTALLATION_ID")
	}
	if keyInput == "" {
		return fmt.Errorf("private key is required; set --private-key or GITHUB_PRIVATE_KEY")
	}
	if timeout <= 0 {
		return fmt.Errorf("timeout must be greater than zero")
	}

	privateKey, err := githubapptoken.ParsePrivateKey(keyInput)
	if err != nil {
		return err
	}
	token, err := githubapptoken.CreateInstallationToken(cmd.Context(), githubapptoken.TokenRequest{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKey:     privateKey,
		APIURL:         apiURL,
		Timeout:        timeout,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), token)
	return err
}

func valueOrEnvironment(value, name string) string {
	if value != "" {
		return value
	}
	return os.Getenv(name)
}

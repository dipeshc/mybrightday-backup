package initcmd

import (
	"context"

	"github.com/dipesh/daycare-photos/internal/app"
	"github.com/spf13/cobra"
)

var configPath string

var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration and authentication",
	Long:  `Create a default config.yaml and perform the interactive Google OAuth flow to obtain a token.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return app.RunInit(context.Background(), configPath)
	},
}

func init() {
	Cmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
}

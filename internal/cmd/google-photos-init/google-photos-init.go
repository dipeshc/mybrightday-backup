package googlephotosinit

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/mybrightday-backup/internal/app"
	"github.com/dipesh/mybrightday-backup/pkg/config"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "google-photos-init",
	Short: "Initialize Google Photos authentication",
	Long:  `Set up the required credentials for Google Photos. It will prompt for authorization and save your token.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagsMap := make(map[string]string)
		fields := config.Analyze(&app.InitConfig{}, "")
		for _, f := range fields {
			if cmd.Flags().Changed(f.FlagName) {
				if f.Type == reflect.Bool {
					val, _ := cmd.Flags().GetBool(f.FlagName)
					flagsMap[f.FlagName] = fmt.Sprintf("%t", val)
				} else {
					val, _ := cmd.Flags().GetString(f.FlagName)
					flagsMap[f.FlagName] = val
				}
			}
		}

		configPath := flagsMap["config"]
		if configPath == "" {
			configPath = os.Getenv("CONFIG")
		}
		if configPath == "" {
			configPath = "config.yaml"
		}

		cfg := &app.InitConfig{}
		if err := app.LoadConfig(configPath, cfg); err != nil {
			return err
		}

		cfg.Resolve(flagsMap)
		app.SetupLogging(cfg.Logging.Verbose, cfg.Logging.Format)

		return app.RunGooglePhotosInit(context.Background(), cfg)
	},
}

func init() {
	// Dynamically register all configuration fields as flags.
	fields := config.Analyze(&app.InitConfig{}, "")
	for _, f := range fields {
		desc := fmt.Sprintf("%s (env: %s)", f.Description, f.EnvName)
		if f.Type == reflect.Bool {
			Cmd.Flags().Bool(f.FlagName, false, desc)
		} else {
			Cmd.Flags().String(f.FlagName, "", desc)
		}
	}
}

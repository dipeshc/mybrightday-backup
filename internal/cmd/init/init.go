package initcmd

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/mybrightday-backup/internal/app"
	"github.com/dipesh/mybrightday-backup/pkg/config"
	"github.com/spf13/cobra"
)

var (
	googlePhotos bool
)

var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize configuration and authentication",
	Long:  `Set up the required credentials for MyBrightDay Backup. By default, it prompts for the MyBrightDay session cookie. Use --google-photos to also perform Google Photos authentication.`,
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

		return app.RunInit(context.Background(), googlePhotos, cfg)
	},
}

func init() {
	Cmd.Flags().BoolVar(&googlePhotos, "google-photos", false, "Enable Google Photos authentication flow")

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

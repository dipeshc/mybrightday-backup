package version

import (
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/mybrightday-photos-downloader/internal/app"
	"github.com/dipesh/mybrightday-photos-downloader/pkg/config"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagsMap := make(map[string]string)
		fields := config.Analyze(&app.VersionConfig{}, "")
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

		cfg := &app.VersionConfig{}
		if err := app.LoadConfig(configPath, cfg); err != nil {
			return err
		}

		cfg.Resolve(flagsMap)
		app.SetupLogging(cfg.Logging.Verbose, cfg.Logging.Format)

		fmt.Printf("mybrightday-photos-downloader %s\n", app.Version)
		return nil
	},
}

func init() {
	// Dynamically register all configuration fields as flags.
	fields := config.Analyze(&app.VersionConfig{}, "")
	for _, f := range fields {
		desc := fmt.Sprintf("%s (env: %s)", f.Description, f.EnvName)
		if f.Type == reflect.Bool {
			Cmd.Flags().Bool(f.FlagName, false, desc)
		} else {
			Cmd.Flags().String(f.FlagName, "", desc)
		}
	}
}

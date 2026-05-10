package cmd

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/spf13/cobra"

	"github.com/dipeshc/mybrightday-backup/internal/app"
	appconfig "github.com/dipeshc/mybrightday-backup/internal/config"
	"github.com/dipeshc/mybrightday-backup/internal/logging"
	"github.com/dipeshc/mybrightday-backup/internal/storage/googlephotos"
)

var RootCmd = &cobra.Command{
	Use:           "mbdb",
	Short:         "Back up photos from the MyBrightDay app",
	Long:          "A tool to fetch photos from the MyBrightDay API, inject EXIF metadata, and optionally back them up to Google Photos.",
	Version:       app.Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runDownload,
}

func init() {
	registerFlags(RootCmd, appconfig.Analyze(app.NewDefaultConfig(), ""))
	RootCmd.AddCommand(googlephotos.Command())
}

func Execute() error {
	return RootCmd.Execute()
}

func runDownload(cmd *cobra.Command, args []string) error {
	flagsMap := collectFlags(cmd, appconfig.Analyze(app.NewDefaultConfig(), ""))

	configPath := flagsMap["config"]
	if configPath == "" {
		configPath = os.Getenv("CONFIG")
	}
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg := app.NewDefaultConfig()
	if err := appconfig.Load(configPath, cfg); err != nil {
		return err
	}

	cfg.Resolve(flagsMap)
	logging.Setup(cfg.Logging)

	return app.Download(context.Background(), cfg)
}

// collectFlags extracts changed flag values from cmd into a map keyed by flag name.
func collectFlags(cmd *cobra.Command, fields []appconfig.ConfigField) map[string]string {
	flagsMap := make(map[string]string)
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
	return flagsMap
}

// registerFlags dynamically registers cobra flags from config field metadata.
func registerFlags(cmd *cobra.Command, fields []appconfig.ConfigField) {
	for _, f := range fields {
		desc := fmt.Sprintf("%s (env: %s)", f.Description, f.EnvName)
		if f.Type == reflect.Bool {
			val := false
			if v, ok := f.DefaultValue.(bool); ok {
				val = v
			}
			cmd.Flags().Bool(f.FlagName, val, desc)
		} else {
			val := ""
			if v, ok := f.DefaultValue.(string); ok {
				val = v
			}
			cmd.Flags().String(f.FlagName, val, desc)
		}
	}
}

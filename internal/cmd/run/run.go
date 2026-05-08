package run

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/mybrightday-photos-downloader/internal/app"
	"github.com/dipesh/mybrightday-photos-downloader/pkg/config"
	"github.com/spf13/cobra"
)

var (
	configPath string
)

var Cmd = &cobra.Command{
	Use:   "run",
	Short: "Download daycare photos",
	Long:  `Fetch photos from the MyBrightDay API for a given date, inject EXIF metadata, and save them locally. Optionally upload to Google Photos.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := &app.RunConfig{}
		if err := app.LoadConfig(configPath, cfg); err != nil {
			return err
		}

		flagsMap := make(map[string]string)
		fields := config.Analyze(app.NewDefaultRunConfig(), "")
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

		cfg = &app.RunConfig{}
		if err := app.LoadConfig(configPath, cfg); err != nil {
			return err
		}

		cfg.Resolve(flagsMap)
		app.SetupLogging(cfg.Logging.Verbose, cfg.Logging.Format)

		return app.RunProcess(context.Background(), cfg)

	},
}

func init() {
	// Dynamically register all configuration fields as flags.
	fields := config.Analyze(app.NewDefaultRunConfig(), "")

	for _, f := range fields {
		desc := fmt.Sprintf("%s (env: %s)", f.Description, f.EnvName)
		if f.Type == reflect.Bool {
			val := false
			if v, ok := f.DefaultValue.(bool); ok {
				val = v
			}
			Cmd.Flags().Bool(f.FlagName, val, desc)
		} else {
			val := ""
			if v, ok := f.DefaultValue.(string); ok {
				val = v
			}
			Cmd.Flags().String(f.FlagName, val, desc)
		}
	}
}

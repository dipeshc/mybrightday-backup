package download

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
	Use:   "download",
	Short: "Download daycare photos",
	Long:  `Fetch photos from the MyBrightDay API for a given date, inject EXIF metadata, and save them locally. Optionally upload to Google Photos.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagsMap := make(map[string]string)
		fields := config.Analyze(app.NewDefaultDownloadConfig(), "")
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

		cfg := &app.DownloadConfig{}
		if err := app.LoadConfig(configPath, cfg); err != nil {
			return err
		}

		cfg.Resolve(flagsMap)
		app.SetupLogging(cfg.Logging.Level, cfg.Logging.Format)

		return app.Download(context.Background(), cfg)
	},
}

func init() {
	fields := config.Analyze(app.NewDefaultDownloadConfig(), "")

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

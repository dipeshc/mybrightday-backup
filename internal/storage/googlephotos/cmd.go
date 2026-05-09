package googlephotos

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/spf13/cobra"

	appconfig "github.com/dipesh/mybrightday-backup/internal/config"
	"github.com/dipesh/mybrightday-backup/internal/logging"
)

// initConfig is the combined configuration for the google-photos init subcommand.
type initConfig struct {
	Logging      logging.Config `yaml:"logging"`
	GooglePhotos Config         `yaml:"google_photos"`
}

func newDefaultInitConfig() *initConfig {
	return &initConfig{
		Logging: logging.Config{
			Format: "text-simple",
			Level:  "INFO",
		},
	}
}

// Command returns the "google-photos" cobra command with its subcommands attached.
// Call RootCmd.AddCommand(googlephotos.Command()) to register it.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "google-photos",
		Short: "Manage Google Photos integration",
		Long:  "Commands for managing the Google Photos storage backend.",
	}
	cmd.AddCommand(initCmd())
	return cmd
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Google Photos authentication",
		Long:  "Set up the required credentials for Google Photos. Opens a browser for authorization and saves the token to google_photos_token_secret.",
		RunE: func(cmd *cobra.Command, args []string) error {
			flagsMap := collectFlags(cmd, appconfig.Analyze(newDefaultInitConfig(), ""))

			configPath := flagsMap["config"]
			if configPath == "" {
				configPath = os.Getenv("CONFIG")
			}
			if configPath == "" {
				configPath = "config.yaml"
			}

			cfg := newDefaultInitConfig()
			if err := appconfig.Load(configPath, cfg); err != nil {
				return err
			}

			appconfig.ResolveStruct(reflect.ValueOf(cfg).Elem(), "", "", flagsMap)
			logging.Setup(cfg.Logging)

			return Init(context.Background(), cfg.GooglePhotos)
		},
	}

	registerFlags(cmd, appconfig.Analyze(newDefaultInitConfig(), ""))
	return cmd
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

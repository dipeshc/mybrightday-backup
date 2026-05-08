// Package config implements the config subcommand, which prints the resolved
// run configuration as YAML to standard output.
package config

import (
	"fmt"
	"os"
	"reflect"

	"github.com/dipesh/daycare-photos/internal/app"
	pkgconfig "github.com/dipesh/daycare-photos/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Cmd prints the resolved run configuration as YAML. Flags, environment variables,
// and the config file are all applied before output, so the result matches what
// 'run' would use for the same inputs.
var Cmd = &cobra.Command{
	Use:   "config",
	Short: "Print the resolved run configuration as YAML",
	Long: `Load and resolve the run configuration from file, environment variables,
and flags, then print the result as YAML to standard output.

Useful for generating a config.yaml for the first time:

  daycare-photos config > config.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagsMap := make(map[string]string)
		fields := pkgconfig.Analyze(app.NewDefaultRunConfig(), "")
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

		configPath := os.Getenv("CONFIG")
		if configPath == "" {
			configPath = "config.yaml"
		}

		cfg := &app.RunConfig{}
		if err := app.LoadConfig(configPath, cfg); err != nil {
			return err
		}
		cfg.Resolve(flagsMap)

		out, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("serializing config: %w", err)
		}

		fmt.Fprint(os.Stdout, string(out))
		return nil
	},
}

// init registers all run configuration fields as flags on Cmd, mirroring the
// run command so users can override individual values before printing.
func init() {
	fields := pkgconfig.Analyze(app.NewDefaultRunConfig(), "")
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

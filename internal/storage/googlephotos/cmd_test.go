package googlephotos

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	appconfig "github.com/dipeshc/mybrightday-backup/internal/config"
)

func TestCommandShape(t *testing.T) {
	cmd := Command()
	if cmd.Use != "google-photos" {
		t.Errorf("Use = %q, want google-photos", cmd.Use)
	}

	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Use)
	}
	if len(names) != 1 || names[0] != "init" {
		t.Errorf("subcommands = %v, want [init]", names)
	}
}

func TestRegisterAndCollectFlags(t *testing.T) {
	fields := []appconfig.ConfigField{
		{YamlKey: "name", EnvName: "NAME", FlagName: "name", Description: "a name", Type: reflect.String, DefaultValue: "default"},
		{YamlKey: "dry_run", EnvName: "DRY_RUN", FlagName: "dryrun", Description: "dry run", Type: reflect.Bool, DefaultValue: true},
	}

	cmd := &cobra.Command{Use: "test"}
	registerFlags(cmd, fields)

	if f := cmd.Flags().Lookup("name"); f == nil || f.DefValue != "default" {
		t.Errorf("name flag = %+v, want registered with default", f)
	}
	if f := cmd.Flags().Lookup("dryrun"); f == nil || f.DefValue != "true" {
		t.Errorf("dryrun flag = %+v, want registered bool default true", f)
	}

	// Unchanged flags are excluded from the collected map.
	if got := collectFlags(cmd, fields); len(got) != 0 {
		t.Errorf("collectFlags with no changes = %v, want empty", got)
	}

	if err := cmd.Flags().Set("name", "changed"); err != nil {
		t.Fatalf("setting flag: %v", err)
	}
	if err := cmd.Flags().Set("dryrun", "false"); err != nil {
		t.Fatalf("setting flag: %v", err)
	}

	got := collectFlags(cmd, fields)
	if got["name"] != "changed" || got["dryrun"] != "false" {
		t.Errorf("collectFlags = %v, want changed values stringified", got)
	}
}

func TestInitCommandRunE(t *testing.T) {
	t.Run("already configured exits clean", func(t *testing.T) {
		t.Setenv("CONFIG_FILES_DIR", t.TempDir())
		t.Setenv("CONFIG", "")
		stubInitAuth(t, nil, fmt.Errorf("must not be called"))

		cmd := Command()
		cmd.SetArgs([]string{"init", "--googlephotos.refreshtoken", "already-set"})
		if err := cmd.Execute(); err != nil {
			t.Errorf("Execute = %v, want nil when token already configured", err)
		}
	})

	t.Run("config load failure", func(t *testing.T) {
		t.Setenv("CONFIG_FILES_DIR", t.TempDir())

		cmd := Command()
		cmd.SetArgs([]string{"init", "--config", t.TempDir()})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "reading config file") {
			t.Errorf("err = %v, want reading config file", err)
		}
	})
}

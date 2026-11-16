package cmd

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	appconfig "github.com/dipeshc/mybrightday-backup/internal/config"
)

func TestRegisterAndCollectFlags(t *testing.T) {
	fields := []appconfig.ConfigField{
		{YamlKey: "name", EnvName: "NAME", FlagName: "name", Description: "a name", Type: reflect.String, DefaultValue: "default"},
		{YamlKey: "dry_run", EnvName: "DRY_RUN", FlagName: "dryrun", Description: "dry run", Type: reflect.Bool, DefaultValue: false},
	}

	cmd := &cobra.Command{Use: "test"}
	registerFlags(cmd, fields)

	if f := cmd.Flags().Lookup("name"); f == nil || f.DefValue != "default" {
		t.Errorf("name flag = %+v, want registered with default", f)
	}
	if f := cmd.Flags().Lookup("dryrun"); f == nil || f.DefValue != "false" {
		t.Errorf("dryrun flag = %+v, want registered bool", f)
	}

	if got := collectFlags(cmd, fields); len(got) != 0 {
		t.Errorf("collectFlags with no changes = %v, want empty", got)
	}

	if err := cmd.Flags().Set("dryrun", "true"); err != nil {
		t.Fatalf("setting flag: %v", err)
	}
	got := collectFlags(cmd, fields)
	if got["dryrun"] != "true" {
		t.Errorf("collectFlags = %v, want dryrun=true", got)
	}
}

// runRoot executes the package-global RootCmd with the given args, isolating
// env-derived config and restoring global state afterwards.
func runRoot(t *testing.T, args ...string) error {
	t.Helper()
	t.Setenv("CONFIG_FILES_DIR", t.TempDir())
	t.Setenv("CONFIG", "")
	t.Setenv("MYBRIGHTDAY_REFRESH_TOKEN", "")

	originalLogger := slog.Default()
	var out bytes.Buffer
	RootCmd.SetOut(&out)
	RootCmd.SetErr(&out)
	t.Cleanup(func() {
		slog.SetDefault(originalLogger)
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
		RootCmd.SetArgs(nil)
	})

	// RootCmd is a package global: flag values and Changed markers persist
	// across Execute calls, so reset them to defaults before each run.
	RootCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})

	RootCmd.SetArgs(args)
	return Execute()
}

func TestExecuteHelp(t *testing.T) {
	if err := runRoot(t, "--help"); err != nil {
		t.Errorf("Execute --help = %v, want nil", err)
	}
}

func TestRunDownloadNotLoggedIn(t *testing.T) {
	missingConfig := filepath.Join(t.TempDir(), "absent.yaml")
	err := runRoot(t, "--config", missingConfig)
	if err == nil || !strings.Contains(err.Error(), "mbdb login") {
		t.Errorf("err = %v, want not-logged-in error pointing at mbdb login", err)
	}
}

func TestHelpListsCoreCommandsBeforeOptional(t *testing.T) {
	var out bytes.Buffer
	RootCmd.SetOut(&out)
	t.Cleanup(func() { RootCmd.SetOut(nil); RootCmd.SetArgs(nil) })
	RootCmd.SetArgs([]string{"--help"})
	if err := Execute(); err != nil {
		t.Fatalf("Execute --help: %v", err)
	}
	help := out.String()
	core := strings.Index(help, "Core Commands:")
	login := strings.Index(help, "  login")
	optional := strings.Index(help, "Optional Integrations:")
	gp := strings.Index(help, "  google-photos")
	if core < 0 || login < 0 || optional < 0 || gp < 0 {
		t.Fatalf("help missing expected sections:\n%s", help)
	}
	if !(core < login && login < optional && optional < gp) {
		t.Errorf("help order wrong: core=%d login=%d optional=%d google-photos=%d\n%s", core, login, optional, gp, help)
	}
}

func TestRunDownloadConfigLoadFailure(t *testing.T) {
	err := runRoot(t, "--config", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("err = %v, want reading config file error", err)
	}
}

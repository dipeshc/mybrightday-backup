package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// isolateConfigDir points the secrets-directory lookup at an empty temp dir so
// a real ./config directory cannot leak into tests. Returns the dir for tests
// that want to plant secret files.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CONFIG_FILES_DIR", dir)
	return dir
}

func writeSecret(t *testing.T, dir, relPath, content string) {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating secret dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing secret: %v", err)
	}
}

func TestResolveValuePrecedence(t *testing.T) {
	t.Run("flag wins over everything", func(t *testing.T) {
		dir := isolateConfigDir(t)
		t.Setenv("MY_KEY", "from-env")
		writeSecret(t, dir, "my_key", "from-file")
		if got := ResolveValue("my_key", "my_key", "from-flag", "from-config"); got != "from-flag" {
			t.Errorf("got %q, want from-flag", got)
		}
	})

	t.Run("env wins over file and config", func(t *testing.T) {
		dir := isolateConfigDir(t)
		t.Setenv("MY_KEY", "from-env")
		writeSecret(t, dir, "my_key", "from-file")
		if got := ResolveValue("my_key", "my_key", "", "from-config"); got != "from-env" {
			t.Errorf("got %q, want from-env", got)
		}
	})

	t.Run("file wins over config and is trimmed", func(t *testing.T) {
		dir := isolateConfigDir(t)
		writeSecret(t, dir, "nested/my_key", "  from-file\n")
		if got := ResolveValue("my_key", "nested/my_key", "", "from-config"); got != "from-file" {
			t.Errorf("got %q, want trimmed from-file", got)
		}
	})

	t.Run("config value is the fallback", func(t *testing.T) {
		isolateConfigDir(t)
		if got := ResolveValue("my_key", "my_key", "", "from-config"); got != "from-config" {
			t.Errorf("got %q, want from-config", got)
		}
	})

	t.Run("empty when nothing set", func(t *testing.T) {
		isolateConfigDir(t)
		if got := ResolveValue("my_key", "my_key", "", ""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

type resolveInner struct {
	Token string `yaml:"refresh_token"`
}

type resolveInline struct {
	Enabled bool `yaml:"enabled"`
}

type resolveRoot struct {
	Name      string        `yaml:"name"`
	Latitude  float64       `yaml:"latitude"`
	DryRun    bool          `yaml:"dry_run"`
	MaybeFlag *bool         `yaml:"maybe_flag"`
	Nested    resolveInner  `yaml:"nested"`
	PtrNested *resolveInner `yaml:"ptr_nested"`
	Inline    resolveInline `yaml:",inline"`
	Ignored   string        // no yaml tag: must be skipped
}

func resolve(cfg *resolveRoot, flags map[string]string) {
	ResolveStruct(reflect.ValueOf(cfg).Elem(), "", "", "", flags)
}

func TestResolveStructEnvKeys(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("NAME", "top")
	t.Setenv("LATITUDE", "12.5")
	t.Setenv("DRY_RUN", "true")
	t.Setenv("NESTED_REFRESH_TOKEN", "tok-nested")
	t.Setenv("PTR_NESTED_REFRESH_TOKEN", "tok-ptr")
	t.Setenv("ENABLED", "true")

	cfg := &resolveRoot{Ignored: "untouched"}
	resolve(cfg, nil)

	if cfg.Name != "top" {
		t.Errorf("Name = %q, want top", cfg.Name)
	}
	if cfg.Latitude != 12.5 {
		t.Errorf("Latitude = %f, want 12.5", cfg.Latitude)
	}
	if !cfg.DryRun {
		t.Error("DryRun = false, want true")
	}
	// Pointer leaves are allocated but not resolved: the Ptr branch in
	// ResolveStruct returns before the scalar handling runs.
	if cfg.MaybeFlag == nil || *cfg.MaybeFlag {
		t.Errorf("MaybeFlag = %v, want allocated false", cfg.MaybeFlag)
	}
	if cfg.Nested.Token != "tok-nested" {
		t.Errorf("Nested.Token = %q, want tok-nested", cfg.Nested.Token)
	}
	if cfg.PtrNested == nil || cfg.PtrNested.Token != "tok-ptr" {
		t.Errorf("PtrNested = %+v, want allocated with tok-ptr", cfg.PtrNested)
	}
	if !cfg.Inline.Enabled {
		t.Error("Inline.Enabled = false, want true (inline uses parent prefix)")
	}
	if cfg.Ignored != "untouched" {
		t.Errorf("Ignored = %q, want untouched", cfg.Ignored)
	}
}

func TestResolveStructSecretFilePaths(t *testing.T) {
	dir := isolateConfigDir(t)
	writeSecret(t, dir, "nested/refresh_token", "from-file")

	cfg := &resolveRoot{}
	resolve(cfg, nil)

	if cfg.Nested.Token != "from-file" {
		t.Errorf("Nested.Token = %q, want from-file (slash-separated path)", cfg.Nested.Token)
	}
}

func TestResolveStructFlagKeys(t *testing.T) {
	isolateConfigDir(t)

	// Underscores are stripped from flag segments; nesting uses dots.
	flags := map[string]string{
		"name":                "flag-name",
		"dryrun":              "true",
		"nested.refreshtoken": "flag-token",
	}
	cfg := &resolveRoot{}
	resolve(cfg, flags)

	if cfg.Name != "flag-name" {
		t.Errorf("Name = %q, want flag-name", cfg.Name)
	}
	if !cfg.DryRun {
		t.Error("DryRun = false, want true")
	}
	if cfg.Nested.Token != "flag-token" {
		t.Errorf("Nested.Token = %q, want flag-token", cfg.Nested.Token)
	}
}

func TestResolveStructKeepsConfigValues(t *testing.T) {
	isolateConfigDir(t)

	yes := true
	cfg := &resolveRoot{
		Name:      "from-yaml",
		Latitude:  1.25,
		MaybeFlag: &yes,
		Nested:    resolveInner{Token: "yaml-token"},
	}
	resolve(cfg, nil)

	if cfg.Name != "from-yaml" || cfg.Latitude != 1.25 || cfg.Nested.Token != "yaml-token" {
		t.Errorf("config values not preserved: %+v", cfg)
	}
	if cfg.MaybeFlag == nil || !*cfg.MaybeFlag {
		t.Errorf("MaybeFlag = %v, want preserved true", cfg.MaybeFlag)
	}
}

func TestResolveStructBoolForms(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"yes", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			isolateConfigDir(t)
			t.Setenv("DRY_RUN", tt.value)
			cfg := &resolveRoot{}
			resolve(cfg, nil)
			if cfg.DryRun != tt.want {
				t.Errorf("DryRun with %q = %t, want %t", tt.value, cfg.DryRun, tt.want)
			}
		})
	}
}

func TestResolveStructInvalidFloatIgnored(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("LATITUDE", "not-a-number")
	cfg := &resolveRoot{Latitude: 3.5}
	resolve(cfg, nil)
	if cfg.Latitude != 3.5 {
		t.Errorf("Latitude = %f, want untouched 3.5", cfg.Latitude)
	}
}

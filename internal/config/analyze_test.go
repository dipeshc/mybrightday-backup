package config

import (
	"reflect"
	"testing"
)

type analyzeInner struct {
	Token string `yaml:"refresh_token" desc:"API token"`
}

type analyzeInline struct {
	Enabled bool `yaml:"enabled" desc:"Enable the thing"`
}

type analyzeRoot struct {
	Name     string        `yaml:"name" desc:"Display name"`
	DryRun   bool          `yaml:"dry_run" desc:"Dry run"`
	Skipped  string        `yaml:"-"`
	NoTag    string        ``
	Nested   analyzeInner  `yaml:"nested"`
	PtrInner *analyzeInner `yaml:"ptr_nested"`
	Inline   analyzeInline `yaml:",inline"`
}

func fieldByYamlKey(fields []ConfigField, key string) (ConfigField, bool) {
	for _, f := range fields {
		if f.YamlKey == key {
			return f, true
		}
	}
	return ConfigField{}, false
}

func TestAnalyze(t *testing.T) {
	fields := Analyze(&analyzeRoot{Name: "default-name"}, "")

	if len(fields) == 0 || fields[0].YamlKey != "config" {
		t.Fatalf("first field = %+v, want virtual config field", fields)
	}
	if fields[0].DefaultValue != "config.yaml" {
		t.Errorf("config default = %v, want config.yaml", fields[0].DefaultValue)
	}

	tests := []struct {
		yamlKey  string
		envName  string
		flagName string
	}{
		{"name", "NAME", "name"},
		{"dry_run", "DRY_RUN", "dryrun"},
		{"nested_refresh_token", "NESTED_REFRESH_TOKEN", "nested.refreshtoken"},
		{"ptr_nested_refresh_token", "PTR_NESTED_REFRESH_TOKEN", "ptrnested.refreshtoken"},
		{"enabled", "ENABLED", "enabled"},
	}
	for _, tt := range tests {
		f, ok := fieldByYamlKey(fields, tt.yamlKey)
		if !ok {
			t.Errorf("field %q not found", tt.yamlKey)
			continue
		}
		if f.EnvName != tt.envName || f.FlagName != tt.flagName {
			t.Errorf("field %q = env %q flag %q, want env %q flag %q",
				tt.yamlKey, f.EnvName, f.FlagName, tt.envName, tt.flagName)
		}
	}

	if _, ok := fieldByYamlKey(fields, "skipped"); ok {
		t.Error("yaml:\"-\" field should be skipped")
	}

	name, _ := fieldByYamlKey(fields, "name")
	if name.Description != "Display name" {
		t.Errorf("description = %q, want Display name", name.Description)
	}
	if name.Type != reflect.String {
		t.Errorf("type = %v, want string", name.Type)
	}
	if name.DefaultValue != "default-name" {
		t.Errorf("default = %v, want default-name", name.DefaultValue)
	}
}

func TestAnalyzeNonStruct(t *testing.T) {
	fields := Analyze("not a struct", "")
	if fields != nil {
		t.Errorf("fields = %+v, want nil for non-struct", fields)
	}
}

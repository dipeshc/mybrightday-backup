package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type loadTarget struct {
	Name   string `yaml:"name"`
	Nested struct {
		Value int `yaml:"value"`
	} `yaml:"nested"`
}

func TestLoadValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("name: hello\nnested:\n  value: 42\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	var target loadTarget
	if err := Load(path, &target); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if target.Name != "hello" || target.Nested.Value != 42 {
		t.Errorf("target = %+v, want name=hello value=42", target)
	}
}

func TestLoadMissingFileIsNoop(t *testing.T) {
	target := loadTarget{Name: "untouched"}
	if err := Load(filepath.Join(t.TempDir(), "absent.yaml"), &target); err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if target.Name != "untouched" {
		t.Errorf("target mutated on missing file: %+v", target)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("name: [unclosed"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	var target loadTarget
	err := Load(path, &target)
	if err == nil || !strings.Contains(err.Error(), "parsing config file") {
		t.Errorf("err = %v, want parsing config file error", err)
	}
}

func TestLoadUnreadablePath(t *testing.T) {
	var target loadTarget
	err := Load(t.TempDir(), &target)
	if err == nil || !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("err = %v, want reading config file error", err)
	}
}

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// ResolveValue implements hierarchical configuration lookup.
// Priority: CLI Flag > Env Var > Secrets Directory > Config Value.
func ResolveValue(key string, flagValue string, configValue string) string {
	if flagValue != "" {
		return flagValue
	}

	envKey := strings.ToUpper(key)
	if val := os.Getenv(envKey); val != "" {
		return val
	}

	if configDir := os.Getenv("CONFIG_FILES_DIR"); configDir != "" {
		// Map the config key (e.g., mybrightday_password) to a directory structure
		// (e.g., mybrightday/password) inside the config files directory.
		configRelPath := strings.ReplaceAll(key, "_", string(filepath.Separator))
		configPath := filepath.Join(configDir, configRelPath)

		if b, err := os.ReadFile(configPath); err == nil {
			return strings.TrimSpace(string(b))
		}
	}

	return configValue
}

// ResolveStruct recursively resolves configuration values using reflection based on yaml tags.
func ResolveStruct(v reflect.Value, prefix string, flagPrefix string, flags map[string]string) {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		fieldValue := v.Field(i)
		fieldType := t.Field(i)

		yamlTag := fieldType.Tag.Get("yaml")
		tagParts := strings.Split(yamlTag, ",")
		tagName := tagParts[0]
		isInline := false
		for _, part := range tagParts[1:] {
			if part == "inline" {
				isInline = true
				break
			}
		}

		if tagName == "" && !isInline {
			continue
		}

		key := tagName
		cleanTagName := strings.ReplaceAll(tagName, "_", "")
		flagKey := cleanTagName

		if isInline {
			key = prefix
			flagKey = flagPrefix
		} else if prefix != "" {
			key = prefix + "_" + tagName
			if flagPrefix != "" {
				flagKey = flagPrefix + "." + cleanTagName
			}
		}

		if fieldValue.Kind() == reflect.Struct {
			ResolveStruct(fieldValue, key, flagKey, flags)
			continue
		}

		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
			}
			if fieldValue.Elem().Kind() == reflect.Struct {
				ResolveStruct(fieldValue.Elem(), key, flagKey, flags)
			}
			continue
		}

		var configStr string
		if fieldValue.Kind() == reflect.String {
			configStr = fieldValue.String()
		} else if fieldValue.Kind() == reflect.Float64 {
			if fieldValue.Float() != 0 {
				configStr = fmt.Sprintf("%f", fieldValue.Float())
			}
		} else if fieldValue.Kind() == reflect.Bool {
			configStr = fmt.Sprintf("%t", fieldValue.Bool())
		} else if fieldValue.Kind() == reflect.Ptr && fieldValue.Elem().Kind() == reflect.Bool {
			if !fieldValue.IsNil() {
				configStr = fmt.Sprintf("%t", fieldValue.Elem().Bool())
			}
		}

		resolvedStr := ResolveValue(key, flags[flagKey], configStr)

		if resolvedStr != "" {
			if fieldValue.Kind() == reflect.String {
				fieldValue.SetString(resolvedStr)
			} else if fieldValue.Kind() == reflect.Float64 {
				var f float64
				if _, err := fmt.Sscanf(resolvedStr, "%f", &f); err == nil {
					fieldValue.SetFloat(f)
				}
			} else if fieldValue.Kind() == reflect.Bool {
				fieldValue.SetBool(strings.ToLower(resolvedStr) == "true" || resolvedStr == "1")
			} else if fieldValue.Kind() == reflect.Ptr && fieldValue.Elem().Kind() == reflect.Bool {
				val := strings.ToLower(resolvedStr) == "true" || resolvedStr == "1"
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				fieldValue.Elem().SetBool(val)
			}
		}
	}
}

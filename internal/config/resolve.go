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
// filePath is the slash-separated path used for the secrets directory lookup
// (e.g. "google_photos/refresh_token"), distinct from key which is used for
// the env var lookup (e.g. "GOOGLE_PHOTOS_REFRESH_TOKEN").
func ResolveValue(key string, filePath string, flagValue string, configValue string) string {
	if flagValue != "" {
		return flagValue
	}

	envKey := strings.ToUpper(key)
	if val := os.Getenv(envKey); val != "" {
		return val
	}

	configDir := os.Getenv("CONFIG_FILES_DIR")
	if configDir == "" {
		configDir = "config"
	}
	if b, err := os.ReadFile(filepath.Join(configDir, filePath)); err == nil {
		return strings.TrimSpace(string(b))
	}

	return configValue
}

// ResolveStruct recursively resolves configuration values using reflection based on yaml tags.
// pathPrefix tracks the slash-separated file path (e.g. "google_photos") separately from
// prefix which tracks the underscore-separated env key prefix (e.g. "google_photos").
// They diverge at each nesting level: key uses "_", file path uses "/".
// Supported field kinds: string, float64, bool, struct, and pointer-to-struct.
func ResolveStruct(v reflect.Value, prefix string, pathPrefix string, flagPrefix string, flags map[string]string) {
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
		filePath := tagName
		cleanTagName := strings.ReplaceAll(tagName, "_", "")
		flagKey := cleanTagName

		if isInline {
			key = prefix
			filePath = pathPrefix
			flagKey = flagPrefix
		} else if prefix != "" {
			key = prefix + "_" + tagName
			filePath = pathPrefix + "/" + tagName
			if flagPrefix != "" {
				flagKey = flagPrefix + "." + cleanTagName
			}
		}

		if fieldValue.Kind() == reflect.Struct {
			ResolveStruct(fieldValue, key, filePath, flagKey, flags)
			continue
		}

		// Only struct pointers are resolved; pointer-to-scalar fields are
		// left untouched so nil keeps meaning "never configured".
		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.Type().Elem().Kind() == reflect.Struct {
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				ResolveStruct(fieldValue.Elem(), key, filePath, flagKey, flags)
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
		}

		resolvedStr := ResolveValue(key, filePath, flags[flagKey], configStr)

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
			}
		}
	}
}

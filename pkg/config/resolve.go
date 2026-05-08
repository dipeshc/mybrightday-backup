package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
)

// ResolveValue implements hierarchical configuration lookup.
// Priority: CLI Flag > Env Var > FILE__Env Var > Local File > Config Value.
func ResolveValue(key string, flagValue string, configValue string) string {
	if flagValue != "" {
		return flagValue
	}

	envKey := strings.ToUpper(key)
	if val := os.Getenv(envKey); val != "" {
		return val
	}

	if fileEnv := os.Getenv("FILE__" + envKey); fileEnv != "" {
		if b, err := os.ReadFile(fileEnv); err == nil {
			return strings.TrimSpace(string(b))
		}
	}

	if b, err := os.ReadFile(key); err == nil {
		return strings.TrimSpace(string(b))
	}

	return configValue
}

// ResolveStruct recursively resolves configuration values using reflection based on yaml tags.
func ResolveStruct(v reflect.Value, prefix string, flags map[string]string) {
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
		if isInline {
			key = prefix
		} else if prefix != "" {
			key = prefix + "_" + tagName
		}

		if fieldValue.Kind() == reflect.Struct {
			ResolveStruct(fieldValue, key, flags)
			continue
		}

		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
			}
			if fieldValue.Elem().Kind() == reflect.Struct {
				ResolveStruct(fieldValue.Elem(), key, flags)
			}
			continue
		}

		flagKey := strings.ReplaceAll(key, "_", "-")

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
			}
		}
	}
}

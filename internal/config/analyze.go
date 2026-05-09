package config

import (
	"reflect"
	"strings"
)

// ConfigField contains metadata about a configuration parameter.
type ConfigField struct {
	YamlKey      string
	EnvName      string
	FlagName     string
	Description  string
	Type         reflect.Kind
	DefaultValue interface{}
}

// Analyze traverses a struct and returns its hierarchical configuration fields.
func Analyze(v interface{}, prefix string) []ConfigField {
	return analyze(v, prefix, "", true)
}

func analyze(v interface{}, prefix string, flagPrefix string, isRoot bool) []ConfigField {
	var fields []ConfigField

	// Prepend the virtual 'config' field only at the actual root of the analysis.
	if isRoot {
		fields = append(fields, ConfigField{
			YamlKey:      "config",
			EnvName:      "CONFIG",
			FlagName:     "config",
			Description:  "Path to configuration file",
			Type:         reflect.String,
			DefaultValue: "config.yaml",
		})
	}

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "-" {
			continue
		}
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
		flagName := cleanTagName

		if isInline {
			key = prefix
			flagName = flagPrefix
		} else if prefix != "" {
			if tagName != "" {
				key = prefix + "_" + tagName
				if flagPrefix != "" {
					flagName = flagPrefix + "." + cleanTagName
				}
			} else {
				key = prefix
				flagName = flagPrefix
			}
		}

		desc := field.Tag.Get("desc")

		// Recurse into structs or pointers to structs.
		innerVal := fieldVal
		if innerVal.Kind() == reflect.Ptr {
			if innerVal.IsNil() {
				innerVal = reflect.New(innerVal.Type().Elem())
			}
			innerVal = innerVal.Elem()
		}

		if innerVal.Kind() == reflect.Struct {
			fields = append(fields, analyze(innerVal.Interface(), key, flagName, false)...)
			continue
		}

		fields = append(fields, ConfigField{
			YamlKey:      key,
			EnvName:      strings.ToUpper(key),
			FlagName:     flagName,
			Description:  desc,
			Type:         field.Type.Kind(),
			DefaultValue: fieldVal.Interface(),
		})
	}

	return fields
}

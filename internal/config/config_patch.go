package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// Patch-style persistence for the configuration write funnel.
//
// updateConfigFileAt used to decode the file into a zero Config, run the
// mutator and marshal the WHOLE struct back. The TOML tags carry no omitempty,
// so every key the file did not have came back as an explicit zero value:
// `[Data] Directory = ''` blanked the default data directory (every process
// started afterwards failed with "data.dir is not set"), `Enabled = false`
// shadowed default-on features, and empty strings shadowed values the global
// config supplies. Keys unknown to this binary were dropped as well.
//
// The funnel now patches the file instead: it keeps the file's own parsed tree
// (rawTree), diffs a rendering of the configuration before and after the
// mutation, and writes back only the leaves the mutation changed. Everything
// else in the file — including keys this binary does not know — is kept as it
// was, and nothing the file did not set is added unless the mutator set it.
//
// One case needs care: a mutator that sets a key the file does not have to its
// zero value while the effective value is non-zero (UpdateLLMCache(false) when
// the default is true). Diffing the file struct alone sees false -> false and
// would lose the change. fillAbsentScalars closes that gap by seeding the
// keys the file does not set with their effective layered values before the
// mutation, so that change shows as true -> false, while an untouched seeded
// key shows as unchanged and is never written.

// parseConfigFileTree decodes the config file bytes into a generic tree, in
// the file's own key spelling. Empty input yields an empty tree.
func parseConfigFileTree(data []byte, format string) (map[string]any, error) {
	tree := map[string]any{}
	if len(bytes.TrimSpace(data)) == 0 {
		return tree, nil
	}
	switch format {
	case "toml":
		if err := toml.Unmarshal(data, &tree); err != nil {
			return nil, fmt.Errorf("failed to parse TOML config file: %w", err)
		}
	default:
		if err := json.Unmarshal(data, &tree); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config file: %w", err)
		}
	}
	if tree == nil {
		tree = map[string]any{}
	}
	return tree, nil
}

// decodeConfigFile decodes the config file bytes into a Config. Empty input
// yields a zero Config.
func decodeConfigFile(data []byte, format string) (*Config, error) {
	out := &Config{}
	if len(bytes.TrimSpace(data)) == 0 {
		return out, nil
	}
	switch format {
	case "toml":
		if err := toml.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("failed to parse TOML config file: %w", err)
		}
	default:
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config file: %w", err)
		}
	}
	return out, nil
}

// configFileTree renders c as a generic tree with the same encoder (and so
// the same key names) the file format uses.
func configFileTree(c *Config, format string) (map[string]any, error) {
	var tree map[string]any
	switch format {
	case "toml":
		data, err := toml.Marshal(c)
		if err != nil {
			return nil, err
		}
		if err := toml.Unmarshal(data, &tree); err != nil {
			return nil, err
		}
	default:
		data, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &tree); err != nil {
			return nil, err
		}
	}
	if tree == nil {
		tree = map[string]any{}
	}
	return tree, nil
}

// marshalConfigFileTree encodes the patched tree in the file's format. Keys
// come out sorted (both encoders sort map keys); comments were never preserved
// by this write path.
func marshalConfigFileTree(tree map[string]any, format string) ([]byte, error) {
	switch format {
	case "toml":
		data, err := toml.Marshal(tree)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal TOML config: %w", err)
		}
		return data, nil
	default:
		data, err := json.MarshalIndent(tree, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSON config: %w", err)
		}
		return data, nil
	}
}

// mergeConfigChanges writes into raw every leaf that differs between before
// and after (renderings of the configuration before and after the mutation),
// taking the value to write from persisted (the encrypted rendering of after).
// Tables are walked key by key; anything else, lists included, is compared and
// written whole. It also re-writes an unchanged leaf the file already holds
// when persisted differs from after, which is exactly a plaintext secret being
// encrypted at rest. It reports whether raw was modified.
func mergeConfigChanges(raw, before, after, persisted map[string]any) bool {
	modified := false
	seen := make(map[string]bool, len(before)+len(after))

	visit := func(key string) {
		lower := strings.ToLower(key)
		if seen[lower] {
			return
		}
		seen[lower] = true

		oldVal, hadOld := lookupInsensitive(before, key)
		newVal, hasNew := lookupInsensitive(after, key)
		outVal, hasOut := lookupInsensitive(persisted, key)
		if !hasOut {
			outVal = newVal
		}

		switch {
		case !hasNew:
			// Gone after the mutation: a deleted map entry, or (JSON) an
			// omitempty field set to its zero value. The file must not keep a
			// value the mutation removed.
			if hadOld && !isZeroValue(oldVal) {
				if deleteInsensitive(raw, key) {
					modified = true
				}
			}
		case !hadOld:
			// New after the mutation (e.g. a new map entry). Absent and zero
			// are the same configuration, so a zero value is not written.
			if !isZeroValue(newVal) {
				setInsensitive(raw, key, outVal)
				modified = true
			}
		default:
			oldMap, oldIsMap := oldVal.(map[string]any)
			newMap, newIsMap := newVal.(map[string]any)
			if oldIsMap && newIsMap {
				rawVal, _ := lookupInsensitive(raw, key)
				rawMap, ok := rawVal.(map[string]any)
				if !ok {
					rawMap = map[string]any{}
				}
				outMap, _ := outVal.(map[string]any)
				if mergeConfigChanges(rawMap, oldMap, newMap, outMap) {
					setInsensitive(raw, key, rawMap)
					modified = true
				}
				return
			}
			if !equalValues(oldVal, newVal) {
				setInsensitive(raw, key, outVal)
				modified = true
				return
			}
			// Unchanged by the mutation. Only a value the file already holds
			// can need re-writing: its encrypted-at-rest form.
			if _, inFile := lookupInsensitive(raw, key); inFile && hasOut && !equalValues(outVal, newVal) {
				setInsensitive(raw, key, outVal)
				modified = true
			}
		}
	}

	for key := range before {
		visit(key)
	}
	for key := range after {
		visit(key)
	}
	return modified
}

// setInsensitive sets key in m, replacing an existing entry whose name matches
// case-insensitively so the file's own spelling is kept and no duplicate key
// (e.g. both `data` and `Data`) is produced.
func setInsensitive(m map[string]any, key string, value any) {
	if _, ok := m[key]; ok {
		m[key] = value
		return
	}
	for k := range m {
		if strings.EqualFold(k, key) {
			m[k] = value
			return
		}
	}
	m[key] = value
}

// deleteInsensitive removes every entry of m whose name matches key
// case-insensitively. It reports whether anything was removed.
func deleteInsensitive(m map[string]any, key string) bool {
	removed := false
	for k := range m {
		if strings.EqualFold(k, key) {
			delete(m, k)
			removed = true
		}
	}
	return removed
}

// effectiveLayeredConfig decodes what the loaded layers (defaults, global
// file, project file, overlays, environment) currently resolve to, straight
// from viper and without Load's post-processing. It is nil when viper cannot
// be decoded, in which case no key is seeded.
func effectiveLayeredConfig() *Config {
	var eff Config
	if err := viper.Unmarshal(&eff); err != nil {
		return nil
	}
	return &eff
}

// fillAbsentScalars seeds dst — the config file decoded into a struct — with
// the effective value of every scalar field the file does not set (see the
// comment at the top of this file). Only scalars reached through plain struct
// fields are seeded: map entries and list elements are never invented, so a
// mutator that looks up a provider account or an MCP server by name sees the
// file's own collection. Skipped on purpose:
//   - Telemetry: a global-only setting, and viper can hold a project-local
//     value that Load deliberately discards;
//   - paths covered by a runtime override (--model, --log-file): process-local
//     values the next process will not have.
func fillAbsentScalars(dst, effective *Config, raw map[string]any, format string) {
	if dst == nil || effective == nil {
		return
	}
	fillStructFields(reflect.ValueOf(dst).Elem(), reflect.ValueOf(effective).Elem(), raw, format, "", RuntimeOverrides())
}

func fillStructFields(dst, src reflect.Value, raw map[string]any, format, jsonPrefix string, overrides map[string]any) {
	t := dst.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		fileKey, explicit, skip := structFieldKey(sf, format)
		if skip {
			continue
		}
		jsonKey, _, jsonSkip := structFieldKey(sf, "json")
		dv, sv := dst.Field(i), src.Field(i)

		// An untagged embedded struct is flattened by both encoders.
		if sf.Anonymous && !explicit && dv.Kind() == reflect.Struct {
			fillStructFields(dv, sv, raw, format, jsonPrefix, overrides)
			continue
		}

		jsonPath := strings.ToLower(jsonKey)
		if jsonPrefix != "" {
			jsonPath = jsonPrefix + "." + jsonPath
		}
		if jsonSkip || jsonPath == "telemetry" || runtimeOverrideCovers(overrides, jsonPath) {
			continue
		}

		rawVal, inFile := lookupInsensitive(raw, fileKey)
		switch dv.Kind() {
		case reflect.Struct:
			sub, _ := rawVal.(map[string]any)
			fillStructFields(dv, sv, sub, format, jsonPath, overrides)
		case reflect.Bool, reflect.String,
			reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			if !inFile && dv.CanSet() && dv.IsZero() && !sv.IsZero() {
				dv.Set(sv)
			}
		}
	}
}

// structFieldKey returns the key an encoder uses for sf in the given format
// ("toml" reads the toml tag, anything else the json tag), whether the tag
// named it explicitly, and whether the field is skipped ("-").
func structFieldKey(sf reflect.StructField, format string) (key string, explicit, skip bool) {
	tagName := "json"
	if format == "toml" {
		tagName = "toml"
	}
	tag := sf.Tag.Get(tagName)
	if tag == "-" {
		return "", false, true
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return sf.Name, false, false
	}
	return name, true, false
}

// runtimeOverrideCovers reports whether a recorded runtime override (keys are
// lower-cased dotted paths) sets path or a table containing it, or a key under
// path.
func runtimeOverrideCovers(overrides map[string]any, path string) bool {
	for key := range overrides {
		if key == path || strings.HasPrefix(path, key+".") || strings.HasPrefix(key, path+".") {
			return true
		}
	}
	return false
}

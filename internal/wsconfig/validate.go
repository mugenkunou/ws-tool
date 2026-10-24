package wsconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// strictConfig is WsConfig plus the old key names ws still reads, so a strict
// decode accepts them.
type strictConfig struct {
	WsConfig
	Log json.RawMessage `json:"log"`
}

// Validate checks the content of a ws.json: it must be JSON, its schema must be
// from 1 to CurrentSchema, and it must have no unknown keys. Old key names that
// Load still reads ("log") are accepted. It returns the parsed config.
func Validate(content []byte) (WsConfig, error) {
	cfg := strictConfig{WsConfig: Default()}
	dec := json.NewDecoder(bytes.NewReader(content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return WsConfig{}, fmt.Errorf("ws.json is not valid: %w", err)
	}
	if dec.More() {
		return WsConfig{}, errors.New("ws.json is not valid: more than one JSON value")
	}
	if cfg.Schema <= 0 {
		return WsConfig{}, errors.New("ws.json: schema must be a positive integer")
	}
	if cfg.Schema > CurrentSchema {
		return WsConfig{}, fmt.Errorf("ws.json: unsupported schema %d (this build supports up to %d)", cfg.Schema, CurrentSchema)
	}
	out := cfg.WsConfig
	if cfg.Log != nil && !bytes.Contains(content, []byte(`"session"`)) {
		_ = json.Unmarshal(cfg.Log, &out.Session)
	}
	normalize(&out)
	return out, nil
}

// MissingKeys lists the keys (dotted paths) that the defaults have and content
// lacks. Arrays are leaves. An old "log" key counts as "session".
func MissingKeys(content []byte) ([]string, error) {
	var have map[string]any
	if err := json.Unmarshal(content, &have); err != nil {
		return nil, fmt.Errorf("parsing ws.json: %w", err)
	}
	if _, ok := have["session"]; !ok {
		if log, ok := have["log"]; ok {
			have["session"] = log
		}
	}
	encoded, err := json.Marshal(Default())
	if err != nil {
		return nil, err
	}
	var want map[string]any
	if err := json.Unmarshal(encoded, &want); err != nil {
		return nil, err
	}
	var missing []string
	var walk func(prefix string, want, have map[string]any)
	walk = func(prefix string, want, have map[string]any) {
		for k, w := range want {
			path := prefix + k
			h, ok := have[k]
			if !ok {
				missing = append(missing, path)
				continue
			}
			wm, wIsObj := w.(map[string]any)
			hm, hIsObj := h.(map[string]any)
			if wIsObj && hIsObj {
				walk(path+".", wm, hm)
			}
		}
	}
	walk("", want, have)
	sort.Strings(missing)
	return missing, nil
}

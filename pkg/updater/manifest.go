package updater

import (
	"encoding/json"
	"fmt"
)

// Manifest returns the declaration in the closed field set KOSD-C-18 fixes and
// the author SDK validates (SDK-C-3). The pages come from the same declaration
// the kernel activates, so the packaged contribution and the loaded one cannot
// drift.
func Manifest() (map[string]any, error) {
	declaration := Declaration()
	pages, err := jsonValue(declaration.Contributions.Pages)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":               declaration.Identifier,
		"version":          declaration.Version,
		"frameworkVersion": declaration.FrameworkVersion,
		"contributions":    map[string]any{"pages": pages},
		"dependencies":     map[string]any{},
		// The product owns no object class and writes nothing; its pages are
		// read by the page shell under the caller's session, so it needs no
		// scope of its own (C-18, C-53).
		"permissions": map[string]any{},
		"config":      map[string]any{},
		"status":      map[string]any{},
		"uninstallRetention": map[string]any{
			// A page holds no data of its own; uninstalling the component
			// withdraws both pages and keeps nothing else behind (C-29).
			"contributions": map[string]any{"pages": "delete"},
		},
	}, nil
}

// jsonValue renders one part of the declaration as the JSON the manifest
// carries, so the package ships the wire shape of the contract rather than a
// Go type's zero values.
func jsonValue(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode declaration: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, fmt.Errorf("decode declaration: %w", err)
	}
	return decoded, nil
}

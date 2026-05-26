package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// ConnectCursor adds kinthai routing fields to the Cursor settings JSON.
// Backs up the file before modification.
// Sets cursor.anthropic.baseUrl and cursor.openai.baseUrl at the top level.
func ConnectCursor(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cursor: read config: %w", err)
	}

	if err := backupFile(configPath, data); err != nil {
		return err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("cursor: parse config: %w", err)
	}

	connectCursorBase(root, "cursor.anthropic.baseUrl", "https://api.anthropic.com")
	connectCursorBase(root, "cursor.openai.baseUrl", "https://api.openai.com/v1")

	return writeJSON(configPath, root)
}

// cursorOrigSuffix is appended to a Cursor base-URL key to stash the user's
// original value for restore on disconnect. Cursor reads only the keys it knows,
// so this extra flat key is inert to it.
const cursorOrigSuffix = "._krouterOriginal"

// connectCursorBase rewrites one Cursor base-URL key to route through krouter
// (origin-replace + preserve path, tagged /a/cursor), saving the original.
// synthDefault is used when the key is absent (Cursor's built-in default).
func connectCursorBase(root map[string]any, key, synthDefault string) {
	cur, _ := root[key].(string)
	if cur != "" && !isKrouterBase(cur) {
		if _, has := root[key+cursorOrigSuffix]; !has {
			root[key+cursorOrigSuffix] = cur
		}
	}
	base := cur
	if base == "" {
		base = synthDefault
	}
	root[key] = krouterAppBaseURL("cursor", base)
}

// disconnectCursorBase restores a Cursor base-URL key from its sidecar, or
// deletes it when there was no saved original (krouter introduced the route).
func disconnectCursorBase(root map[string]any, key string) {
	sk := key + cursorOrigSuffix
	if orig, ok := root[sk].(string); ok && orig != "" {
		root[key] = orig
		delete(root, sk)
	} else {
		delete(root, key)
	}
}

// DisconnectCursor removes kinthai routing fields from the Cursor settings JSON.
func DisconnectCursor(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cursor: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("cursor: parse config: %w", err)
	}

	disconnectCursorBase(root, "cursor.anthropic.baseUrl")
	disconnectCursorBase(root, "cursor.openai.baseUrl")

	return writeJSON(configPath, root)
}

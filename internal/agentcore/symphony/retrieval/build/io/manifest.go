// manifest.go 对齐 Python build/io/manifest.py（44 行）：manifest.json 读写。
package io

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/retrieval/build"
)

// WriteManifest 写 manifest.json（mode/count/item_paths/worker_ids，可选 item_type）。
// 对齐 Python write_manifest（manifest.py:13-29）。
func WriteManifest(outputDir string, itemPaths []string, workerIDs []string, mode string, itemType string) error {
	manifest := map[string]any{
		"mode":       mode,
		"count":      len(workerIDs),
		"item_paths": serializeItemPaths(itemPaths),
		"worker_ids": workerIDs,
	}
	if itemType != "" {
		manifest["item_type"] = itemType
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, build.IndexManifestFilename), data, 0o644)
}

// LoadManifest 读 manifest.json；不存在返回空表。
// 对齐 Python load_manifest（manifest.py:32-36）。
func LoadManifest(indexDir string) (map[string]any, error) {
	manifestPath := filepath.Join(indexDir, build.IndexManifestFilename)
	if _, err := os.Stat(manifestPath); err != nil {
		return map[string]any{}, nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// serializeItemPaths 逐路径序列化（passthrough 原样，本地绝对化）。
// 对齐 Python _serialize_item_path（manifest.py:39-43）。
func serializeItemPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		raw := strings.TrimSpace(p)
		if IsPassthroughItemURI(raw) {
			out = append(out, raw)
			continue
		}
		expanded := raw
		if strings.HasPrefix(expanded, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[1:])
			}
		}
		abs, err := filepath.Abs(expanded)
		if err != nil {
			abs = expanded
		}
		out = append(out, abs)
	}
	return out
}

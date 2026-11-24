// config_loader.go 对齐 Python build/io/config_loader.py（33 行）：配置读取与双解析。
package io

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReadConfigText 读配置文本（本地分支；S3 返回登记错误 D-16-4-1）。
// 对齐 Python read_config_text（config_loader.py:12-22）。
func ReadConfigText(source string, description string) (string, error) {
	rawSource := strings.TrimSpace(source)
	if rawSource == "" {
		return "", fmt.Errorf("%s path is empty", description)
	}
	if IsS3URI(rawSource) {
		return "", ErrS3NotImplemented
	}
	expanded := rawSource
	if strings.HasPrefix(expanded, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, expanded[1:])
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		abs = expanded
	}
	if _, serr := os.Stat(abs); serr != nil {
		return "", fmt.Errorf("%s file not found: %s", description, abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseJSONOrYaml 先 JSON 后 YAML 双解析；两者皆失败返回 ValueError 等价。
// 对齐 Python parse_json_or_yaml（config_loader.py:25-33）。
func ParseJSONOrYaml(text string, source string) (any, error) {
	var jv any
	if err := json.Unmarshal([]byte(text), &jv); err == nil {
		return jv, nil
	}
	var yv any
	if err := yaml.Unmarshal([]byte(text), &yv); err != nil {
		return nil, fmt.Errorf("Failed to parse config file: %s", source)
	}
	return yv, nil
}

var _ = errors.New // 保留 errors 引用（ErrS3NotImplemented 定义于 items_jsonl.go）

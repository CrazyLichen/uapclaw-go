package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/resource"
	"gopkg.in/yaml.v3"
)

// logComponent 组件级日志常量（agentcore 域）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadAutoCoordinatingHarnessConfig 从 configPath 加载并校验 orchestrator.yaml
// （对齐 load_auto_coordinating_harness_config；缺失时 bootstrap 默认模板）
func LoadAutoCoordinatingHarnessConfig(configPath string) (AutoCoordinatingHarnessConfig, error) {
	path, err := filepath.Abs(expandTilde(configPath))
	if err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if !bootstrapDefaultConfig(path) {
			return AutoCoordinatingHarnessConfig{}, fmt.Errorf("configuration file is unavailable: %s", path)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	if data == nil {
		data = map[string]any{}
	}
	return AutoCoordinatingHarnessConfigFromDict(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// bootstrapDefaultConfig 缺失时把内嵌默认模板写到 path（对齐 _bootstrap_default_config；
// 失败仅告警并返回 false，错误不吞掉主流程）
func bootstrapDefaultConfig(path string) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("failed to bootstrap default config: cannot create directory")
		return false
	}
	if err := os.WriteFile(path, resource.DefaultConfigTemplate, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("failed to bootstrap default config to target path")
		return false
	}
	return true
}

// expandTilde 展开路径前缀 ~（对齐 Path.expanduser）
func expandTilde(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

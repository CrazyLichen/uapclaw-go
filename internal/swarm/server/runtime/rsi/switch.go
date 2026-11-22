package rsi

// RSI 实验入口开关（web RSI 实验特性）。
// 对应 Python：gateway/common/config.py:795 update_rsi_enabled_in_config
// （rsi.enabled 配置节原子写）+ web 通道入口检查。

import (
	"fmt"
	"os"

	configpkg "github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ──────────────────────────── 常量 ────────────────────────────

// RsiEnabledConfigKey 配置键（rsi.enabled）。
const RsiEnabledConfigKey = "rsi.enabled"

// ──────────────────────────── 导出函数 ────────────────────────────

// RsiEnabledFromConfig 读取 rsi.enabled（缺省 false = 实验入口关闭）。
func RsiEnabledFromConfig(cfg *configpkg.Config) bool {
	defer func() { _ = recover() }()
	if cfg == nil {
		cfg = &configpkg.Config{}
	}
	value := cfg.Get(RsiEnabledConfigKey)
	enabled, _ := value.(bool)
	return enabled
}

// UpdateRsiEnabled 原子更新 rsi.enabled 并落盘（读改写整节，
// 对齐 Python update_rsi_enabled_in_config）。
func UpdateRsiEnabled(cfg *configpkg.Config, enabled bool) error {
	defer func() { _ = recover() }()
	if cfg == nil {
		cfg = &configpkg.Config{}
	}
	section, _ := cfg.Get("rsi").(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	section["enabled"] = enabled
	if err := cfg.Set("rsi", section); err != nil {
		return fmt.Errorf("rsi.enabled 原子更新失败: %w", err)
	}
	return nil
}

// EnvFallbackEnabled 环境变量回退（测试/部署显式开启）。
func EnvFallbackEnabled() bool {
	return os.Getenv("RSI_ENABLED") == "true"
}

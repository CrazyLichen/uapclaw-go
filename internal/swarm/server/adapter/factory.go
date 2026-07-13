package adapter

import (
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sdkEnvVar SDK 选择环境变量名
	sdkEnvVar = "JIUWENSWARM_AGENT_SDK"
	// defaultSDK 默认 SDK 名称
	defaultSDK = "harness"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件
var logComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveSDKChoice 从环境变量解析 SDK 选择。
//
// 对应 Python: resolve_sdk_choice()
//
// 行为：
//   - 未设置或空 → "harness"（默认）
//   - "harness" → "harness"
//   - "pi" → "pi"（预留，尚未实现）
//   - 未知值 → 警告并回退 "harness"
func ResolveSDKChoice() string {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(sdkEnvVar)))
	if raw == "" {
		logger.Debug(logComponent).Str("env_var", sdkEnvVar).Str("default", defaultSDK).Msg("环境变量未设置，使用默认 SDK")
		return defaultSDK
	}

	validSDKs := map[string]bool{"harness": true, "pi": true}
	if validSDKs[raw] {
		logger.Info(logComponent).Str("sdk", raw).Msg("解析 SDK 选择")
		return raw
	}

	logger.Warn(logComponent).Str("raw", raw).Str("default", defaultSDK).Msg("未知 SDK 值，回退到默认")
	return defaultSDK
}

// CreateAdapter 工厂函数，创建 SDK 适配器实例。
//
// 对应 Python: create_adapter(sdk, *, mode)
//
// 参数：
//   - sdk: SDK 名称，若为空则从环境变量解析
//   - mode: 实例模式，"agent"（默认）或 "code"
//
// 路由规则：
//   - sdk="harness" + mode="code" → CodeAdapter
//   - sdk="harness" + 其余 mode → DeepAdapter
//   - sdk="pi" → 错误（尚未实现）
//   - 未知 sdk → 错误
func CreateAdapter(sdk string, mode string) (AgentAdapter, error) {
	sdkName := sdk
	if sdkName == "" {
		sdkName = ResolveSDKChoice()
	}

	switch sdkName {
	case "harness":
		if mode == "code" {
			return NewCodeAdapter(), nil
		}
		return NewDeepAdapter(), nil
	case "pi":
		return nil, fmt.Errorf("SDK %q 尚未实现，当前仅支持 harness", sdkName)
	default:
		return nil, fmt.Errorf("未知 SDK %q，支持: harness, pi (预留)", sdkName)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

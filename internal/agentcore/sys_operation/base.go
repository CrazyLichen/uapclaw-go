package sys_operation

import (
	"context"
	"encoding/json"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseOperation 操作基类，所有子操作（fs/shell/code）的公共父类。
// 对齐 Python BaseOperation：name, mode, description, _run_config。
type BaseOperation struct {
	// name 操作名称（如 "fs", "shell", "code"）
	name string
	// mode 操作模式
	mode OperationMode
	// description 操作描述
	description string
	// runConfig 运行配置（LocalWorkConfig 或 SandboxGatewayConfig）
	runConfig any
}

// ──────────────────────────── 枚举 ────────────────────────────

// OperationMode 操作模式枚举
type OperationMode int

const (
	// OperationModeLocal 本地执行模式
	OperationModeLocal OperationMode = 0
	// OperationModeSandbox 沙箱执行模式
	OperationModeSandbox OperationMode = 1
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseOperation 创建 BaseOperation 实例
func NewBaseOperation(name string, mode OperationMode, description string, runConfig any) BaseOperation {
	return BaseOperation{
		name:        name,
		mode:        mode,
		description: description,
		runConfig:   runConfig,
	}
}

// Name 返回操作名称
func (b *BaseOperation) Name() string {
	return b.name
}

// Mode 返回操作模式
func (b *BaseOperation) Mode() OperationMode {
	return b.mode
}

// Description 返回操作描述
func (b *BaseOperation) Description() string {
	return b.description
}

// RunConfig 返回运行配置
func (b *BaseOperation) RunConfig() any {
	return b.runConfig
}

// ListTools 返回工具卡片列表（由子类实现，基类返回空）
func (b *BaseOperation) ListTools() []*tool.ToolCard {
	return nil
}

// String 返回操作模式的字符串表示
func (m OperationMode) String() string {
	switch m {
	case OperationModeLocal:
		return "local"
	case OperationModeSandbox:
		return "sandbox"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (m OperationMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (m *OperationMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "local":
		*m = OperationModeLocal
	case "sandbox":
		*m = OperationModeSandbox
	default:
		return fmt.Errorf("未知的操作模式: %s", s)
	}
	return nil
}

// FromOperationModeString 将字符串解析为 OperationMode
func FromOperationModeString(s string) OperationMode {
	switch s {
	case "local":
		return OperationModeLocal
	case "sandbox":
		return OperationModeSandbox
	default:
		return OperationModeLocal
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createSysOperationEvent 创建系统操作日志事件。
// 对齐 Python BaseOperation._create_sys_operation_event。
func (b *BaseOperation) createSysOperationEvent(
	ctx context.Context,
	eventType string,
	methodName string,
	methodParams map[string]any,
	methodResult map[string]any,
	execTimeMs float64,
) map[string]any {
	return map[string]any{
		"module_id":           "sys_operation",
		"module_name":         "sys_operation",
		"operation_name":      b.name,
		"operation_mode":      b.mode.String(),
		"operation_desc":      b.description,
		"event_type":          eventType,
		"method_name":         methodName,
		"method_params":       methodParams,
		"method_result":       methodResult,
		"method_exec_time_ms": execTimeMs,
	}
}

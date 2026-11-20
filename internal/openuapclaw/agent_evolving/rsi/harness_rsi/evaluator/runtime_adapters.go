// runtime_adapters.go 提供 RSI 对 Agent Core 与 Harness 公共 API 的自有适配器
// （17.4b1 切片：仅 RSIBashTool + RSISysOperationRail）。
// Python: openjiuwen/rsi/harness_rsi/evaluator/runtime_adapters.py。
// RSITrajectoryRail / RSISkillUseRail / run_agent_with_empty_response_recovery
// 及其依赖 ⤴️ 17.6 评估器章节补齐。

package evaluator

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent RSI evaluator 日志组件。
const logComponent = logger.ComponentAgentCore

// pipefailPrefix bash 管道失败前缀。
// Python: runtime_adapters.py:99 "set -o pipefail;"
const pipefailPrefix = "set -o pipefail;"

// ──────────────────────────── 结构体 ────────────────────────────

// RSIBashToolDecorator 保序 bash 工具装饰器：管道中保留生产者失败。
// Python: runtime_adapters.py:86-102 RSIBashTool（仅覆写 invoke；pipefail=false
// 时行为与基类 BashTool 完全一致）。
type RSIBashToolDecorator struct {
	// Wrapped 被装饰的基类 bash 工具
	Wrapped tool.Tool
	// pipefail 是否启用 pipefail 前缀与 shell_type 强制
	pipefail bool
}

// NewRSIBashToolDecorator 包装基类 bash 工具。
// Python: runtime_adapters.py:89-91 RSIBashTool.__init__
func NewRSIBashToolDecorator(wrapped tool.Tool, pipefail bool) *RSIBashToolDecorator {
	return &RSIBashToolDecorator{Wrapped: wrapped, pipefail: pipefail}
}

// Card 透传工具卡片。
func (d *RSIBashToolDecorator) Card() *tool.ToolCard { return d.Wrapped.Card() }

// Stream 透传流式调用（Python 未覆写 stream）。
func (d *RSIBashToolDecorator) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return d.Wrapped.Stream(ctx, inputs, opts...)
}

// GetCallTimeout 透传调用预算（Python 未覆写 get_call_timeout）。
func (d *RSIBashToolDecorator) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return d.Wrapped.GetCallTimeout(inputs, defaultTimeout)
}

// Invoke pipefail 启用时为命令注入 pipefail 前缀并强制 bash shell。
// Python: runtime_adapters.py:93-102 RSIBashTool.invoke
func (d *RSIBashToolDecorator) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	if !d.pipefail {
		return d.Wrapped.Invoke(ctx, inputs, opts...)
	}

	// Python: adapted = dict(inputs)
	adapted := make(map[string]any, len(inputs))
	for key, value := range inputs {
		adapted[key] = value
	}
	// Python: command = str(adapted.get("command", "") or "").strip()
	command := ""
	if raw, ok := adapted["command"]; ok {
		if str, isStr := raw.(string); isStr {
			command = strings.TrimSpace(str)
		}
	}
	// Python: if command and not command.startswith("set -o pipefail;"): adapted["command"] = f"set -o pipefail; {command}"
	if command != "" && !strings.HasPrefix(command, pipefailPrefix) {
		adapted["command"] = pipefailPrefix + " " + command
	}
	// Python: adapted["shell_type"] = "bash"
	adapted["shell_type"] = "bash"
	return d.Wrapped.Invoke(ctx, adapted, opts...)
}

// RSISysOperationRail 以 RSI 评估策略挂载上游系统操作工具。
// Python: runtime_adapters.py:105-167 RSISysOperationRail（继承 SysOperationRail，
// 覆写 init；Go 经基类扩展点 shellOnly/allowShell/bashDecorator 表达覆写差异）。
type RSISysOperationRail struct {
	rails.SysOperationRail
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRSISysOperationRail 创建 RSI 系统操作护栏。
// Python: runtime_adapters.py:108-119 RSISysOperationRail.__init__
// （shell_only/bash_pipefail/allow_shell 为 RSI 专属；其余 kwargs 透传基类）。
func NewRSISysOperationRail(shellOnly, bashPipefail, allowShell bool, baseOpts ...rails.SysOperationRailOption) *RSISysOperationRail {
	opts := []rails.SysOperationRailOption{
		rails.WithShellOnly(shellOnly),
		rails.WithAllowShell(allowShell),
	}
	if bashPipefail {
		opts = append(opts, rails.WithBashToolDecorator(func(wrapped tool.Tool) tool.Tool {
			return NewRSIBashToolDecorator(wrapped, true)
		}))
	}
	opts = append(opts, baseOpts...)
	return &RSISysOperationRail{SysOperationRail: *rails.NewSysOperationRail(opts...)}
}

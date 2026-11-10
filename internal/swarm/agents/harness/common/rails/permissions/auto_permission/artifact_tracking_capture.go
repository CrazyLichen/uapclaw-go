// 工具执行后的通用捕获。
//
// 对应 Python 代码：_auto_permission/artifact_tracking_capture.py（23 行 / 1 方法）
// Python: class AutoPermissionArtifactCaptureMixin
//
// 职责极窄：清理任务级执行域标记，然后把调用原样转发给被包装的权限轨。
// **不推断生成物**——产物识别由专门的上游逻辑负责，这里只做上下文卫生。
//
// ## 偏差 V-16：`clear_no_host_fallback` 的清除无法回传
//
// Python 的 `clear_no_host_fallback()` 写 ContextVar（任务级全局），调用方无需
// 接收返回值；Go 的 `ClearNoHostFallback(ctx)` 返回派生 ctx，而
// `AfterToolCall(ctx, cbc)` 的 ctx 是入参、`AgentCallbackContext` 也没有 ctx
// 设置器，派生结果无法回传给调用方。
//
// 影响：若上游 ctx 已设 `no_host_fallback=true`，本次清除**不会生效**。
// 默认情形（上游未设）下清除与默认值等价，行为一致。
// 已登记进 spec 的 9.90e 回填清单——修法是为 `AgentCallbackContext` 增加
// ctx 设置器，或改由调用方接收派生 ctx。

package auto_permission

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	runtimesandbox "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// AfterToolCall 工具执行后的钩子：清理执行域标记后转发给被包装的权限轨。
//
// Python: AutoPermissionArtifactCaptureMixin.after_tool_call(*args, **kwargs)
func (r *AutoPermissionRail) AfterToolCall(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	// ⤵️ 9.90e 回填（偏差 V-16）：派生 ctx 需回传给调用方才能真正生效
	_ = runtimesandbox.ClearNoHostFallback(ctx)

	invocation := ExtractInvocation(cbc, nil, map[string]any{})
	_, err := r.callBaseAfterRail(ctx, cbc, invocation)
	return err
}

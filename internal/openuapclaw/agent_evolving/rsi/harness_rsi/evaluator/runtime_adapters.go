// runtime_adapters.go 提供 RSI 对 Agent Core 与 Harness 公共 API 的自有适配器。
// Python: openjiuwen/rsi/harness_rsi/evaluator/runtime_adapters.py（495 行）。
// 已落地：RSIBashTool/RSISysOperationRail（17.4b1）、RSITrajectoryRail（25.2b
// 兑现）、run_agent_with_empty_response_recovery（17.6a）；RSISkillUseRail ⤴️
// 17.6a 后续批次（协议体量大，单独一批）。

package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	evolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
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

// ──────────────────────────── RSITrajectoryRail（17.6a / 25.2b 兑现） ────────────────────────────

// RSITrajectoryRail 保持采集质量对评估证据消费方可见的轨迹轨道。
// Python: runtime_adapters.py:46-84 RSITrajectoryRail(TrajectoryRail)——
// capture_issues 采集 + model/tool 调用计数。Go 侧基座计数与 issues 经
// CaptureIssueObserver 可选接口回调，本轨道以 ext 包装实现观察
// （与 EvolutionRail ext 设计同源偏差，Python 为方法覆写）。
// 生产消费方：per-case 归档轨迹进诊断库（25.2b 登记），供 17.4b1 读件层读证据。
type RSITrajectoryRail struct {
	*evolution.TrajectoryRail
	// issuesMu 采集质量问题集合锁
	issuesMu sync.Mutex
	// captureIssues 采集质量问题码集合（Python self.capture_issues set[str]）
	captureIssues map[string]struct{}
	// modelCallCount 模型调用计数（Python self.model_call_count）
	modelCallCount int
	// toolCallCount 工具调用计数（Python self.tool_call_count）
	toolCallCount int
}

// rsiTrajectoryExt RSITrajectoryRail 的 ext 包装：issues 观察 + 计数，
// 其余委托 TrajectoryRail 自带的归档包装。
type rsiTrajectoryExt struct {
	inner evolution.EvolutionExtension
	rail  *RSITrajectoryRail
}

var _ evolution.EvolutionExtension = (*rsiTrajectoryExt)(nil)
var _ evolution.CaptureIssueObserver = (*rsiTrajectoryExt)(nil)

// NewRSITrajectoryRail 创建 RSI 轨迹轨道。
// Python: runtime_adapters.py:50-62 __init__（trajectory_span_processor/
// trajectory_store 注入——Go 经 TrajectoryRail 选项承载，见文件头偏差登记）。
func NewRSITrajectoryRail(opts ...evolution.EvolutionRailOption) *RSITrajectoryRail {
	inner := evolution.NewTrajectoryRail(opts...)
	rail := &RSITrajectoryRail{
		TrajectoryRail: inner,
		captureIssues:  map[string]struct{}{},
	}
	inner.SetExtension(&rsiTrajectoryExt{inner: inner.GetExtension(), rail: rail})
	return rail
}

// ObserveCaptureIssues 采集 drain 的质量问题码（Python: _drain_for_hook
// self.capture_issues.update(str(issue.get("code", "capture_error")))）。
func (e *rsiTrajectoryExt) ObserveCaptureIssues(issues []map[string]any) {
	e.rail.issuesMu.Lock()
	defer e.rail.issuesMu.Unlock()
	for _, issue := range issues {
		code, _ := issue["code"].(string)
		if code == "" {
			code = "capture_error"
		}
		e.rail.captureIssues[code] = struct{}{}
	}
}

// OnAfterModelCall 模型调用计数后委托内层（Python: after_model_call 计数+super）。
func (e *rsiTrajectoryExt) OnAfterModelCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	e.rail.issuesMu.Lock()
	e.rail.modelCallCount++
	e.rail.issuesMu.Unlock()
	return e.inner.OnAfterModelCall(ctx, cbc)
}

// OnAfterToolCall 工具调用计数后委托内层（Python: after_tool_call 计数+super）。
func (e *rsiTrajectoryExt) OnAfterToolCall(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	e.rail.issuesMu.Lock()
	e.rail.toolCallCount++
	e.rail.issuesMu.Unlock()
	return e.inner.OnAfterToolCall(ctx, cbc)
}

// 其余扩展点委托内层（归档行为由 TrajectoryRail 自带包装承载）。
func (e *rsiTrajectoryExt) OnBeforeInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	return e.inner.OnBeforeInvoke(ctx, cbc)
}

func (e *rsiTrajectoryExt) OnAfterInvoke(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	return e.inner.OnAfterInvoke(ctx, cbc)
}

func (e *rsiTrajectoryExt) OnAfterTaskIteration(ctx context.Context, cbc *interfaces.AgentCallbackContext) error {
	return e.inner.OnAfterTaskIteration(ctx, cbc)
}

func (e *rsiTrajectoryExt) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) error {
	return e.inner.OnAfterEvolutionTriggered(ctx, traj, cbc)
}

func (e *rsiTrajectoryExt) AllowEvolutionTrigger(trigger evolution.EvolutionTriggerPoint, cbc *interfaces.AgentCallbackContext) bool {
	return e.inner.AllowEvolutionTrigger(trigger, cbc)
}

func (e *rsiTrajectoryExt) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *interfaces.AgentCallbackContext) *evolution.EvolutionSnapshot {
	return e.inner.SnapshotForEvolution(ctx, traj, cbc)
}

func (e *rsiTrajectoryExt) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *evolution.EvolutionSnapshot) error {
	return e.inner.RunEvolution(ctx, traj, snapshot)
}

func (e *rsiTrajectoryExt) GetEvolutionTotalTimeoutSecs() float64 {
	return e.inner.GetEvolutionTotalTimeoutSecs()
}

// CaptureIssues 采集质量问题码快照（评估证据消费方读取）。
func (r *RSITrajectoryRail) CaptureIssues() []string {
	r.issuesMu.Lock()
	defer r.issuesMu.Unlock()
	codes := make([]string, 0, len(r.captureIssues))
	for code := range r.captureIssues {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// ModelCallCount 模型调用计数。
func (r *RSITrajectoryRail) ModelCallCount() int {
	r.issuesMu.Lock()
	defer r.issuesMu.Unlock()
	return r.modelCallCount
}

// ToolCallCount 工具调用计数。
func (r *RSITrajectoryRail) ToolCallCount() int {
	r.issuesMu.Lock()
	defer r.issuesMu.Unlock()
	return r.toolCallCount
}

// ──────────────────────────── 空响应恢复（17.6a） ────────────────────────────

// AgentInvokeFunc 单次 agent 调用的注入缝（绑定 agent/session 后的 Runner 调用）。
// 偏差登记：Python run_agent_with_empty_response_recovery(agent, inputs, *, session)
// 内部直接调 Runner.run_agent；Go Runner.RunAgent 签名含 AgentRef/SessionRef/
// modelCtx/envs，闭包注入以对齐调用方装配（17.6b case_backend 接线）。
type AgentInvokeFunc func(ctx context.Context, inputs map[string]any) (map[string]any, error)

// RunAgentWithEmptyResponseRecovery 无可行动输出时重跑一次 RSI 评估 invoke。
// Python: runtime_adapters.py:447-470 run_agent_with_empty_response_recovery。
func RunAgentWithEmptyResponseRecovery(ctx context.Context, invoke AgentInvokeFunc,
	inputs map[string]any) (map[string]any, error) {
	response, err := invoke(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if responseText(response) != "" {
		return response, nil
	}
	logger.Warn(logComponent).Msg("[RSI evaluator] recovering once from an empty agent response")
	return invoke(ctx, map[string]any{
		"query": ("[RECOVERY] The previous turn produced no answer or tool action. " +
			"Do not restart the analysis. Apply the smallest justified workspace " +
			"edit now, then run the acceptance probe."),
	})
}

// responseText 递归抽取响应文本（Python: runtime_adapters.py:473-487
// _response_text——output/content/response/result 四键递归，dict 无命中键时
// JSON 序列化，None→空串）。
func responseText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		if v == nil {
			// Python None 等价物（类型化 nil map 不落入 dict 键循环）。
			return ""
		}
		recognized := false
		for _, key := range []string{"output", "content", "response", "result"} {
			if inner, exists := v[key]; exists {
				recognized = true
				if text := responseText(inner); text != "" {
					return text
				}
			}
		}
		if !recognized {
			raw, err := json.Marshal(v)
			if err != nil {
				return fmt.Sprint(v)
			}
			return string(raw)
		}
		return ""
	default:
		return fmt.Sprint(v)
	}
}

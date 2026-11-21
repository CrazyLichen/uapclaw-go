package codebuddy

import (
	"context"

	"fmt"
	"time"

	"github.com/google/uuid"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// ──────────────────────────── 常量 ────────────────────────────

// AdapterVersion 适配器版本。
// Python 无对应物；dsh 同位常量形态参考 harness.py:36。
const AdapterVersion = "0.1.0"

// ──────────────────────────── 全局变量 ────────────────────────────

// codebuddyCard CodebuddyHarness 的能力卡。
// capabilities = {MCP_TOOLS, GRACEFUL_ABORT, ENDPOINT_MODELS}。
var codebuddyCard = func() harnessprotocol.HarnessCard {
	card, err := harnessprotocol.NewHarnessCardWithOptions("codebuddy", AdapterVersion,
		harnessprotocol.CardOptions{
			Capabilities: map[harnessprotocol.HarnessCapability]bool{
				harnessprotocol.HarnessCapabilityMcpTools:       true,
				harnessprotocol.HarnessCapabilityGracefulAbort:  true,
				harnessprotocol.HarnessCapabilityEndpointModels: true,
			},
		})
	if err != nil {
		panic(fmt.Sprintf("codebuddy card 构造失败: %v", err))
	}
	return card
}()

// ──────────────────────────── 结构体 ────────────────────────────

// CodebuddyHarness 把一个 CodeBuddy Code CLI 子进程适配到协议 v1。
// Python 无对应物（偏差 D-15.22-a）；形态对齐 dsh/harness.py。
type CodebuddyHarness struct {
	// base 15.5 基座（提供 Emit / 14 公开方法）
	base *providers.SerializedTurnHarness
	// config provider 自有配置
	config CodebuddyConfig
	// transportFactory 传输缝（测试注入点）
	transportFactory TransportFactory
	// transport 活跃传输
	transport Transport
	// sessionID CLI 原生会话 id（init 帧取得；--resume 载体）
	sessionID string
	// resumeSessionID 恢复载体（起始 checkpoint 提取）
	resumeSessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodebuddyHarness 构造未启动的适配器（默认子进程传输）。
func NewCodebuddyHarness(config CodebuddyConfig) (*CodebuddyHarness, error) {
	return NewCodebuddyHarnessWithFactory(config, nil)
}

// NewCodebuddyHarnessWithFactory 构造并允许注入传输工厂（nil 用默认子进程）。
func NewCodebuddyHarnessWithFactory(config CodebuddyConfig, factory TransportFactory) (*CodebuddyHarness, error) {
	validated, err := NewCodebuddyConfig(config)
	if err != nil {
		return nil, err
	}
	h := &CodebuddyHarness{
		config:           validated,
		transportFactory: factory,
	}
	if h.transportFactory == nil {
		h.transportFactory = NewProcessTransport
	}
	h.base = providers.NewSerializedTurnHarness(providers.HarnessOptions{
		Card:                codebuddyCard,
		Hooks:               h,
		EventBufferCapacity: validated.EventBufferCapacity,
	})
	return h, nil
}

// ──────────────────────────── 协议面转发（对齐 dsh 包装集） ────────────────────────────

// Card 能力卡。
func (h *CodebuddyHarness) Card() harnessprotocol.HarnessCard { return h.base.Card() }

// State 当前相位。
func (h *CodebuddyHarness) State() harnessprotocol.HarnessState { return h.base.State() }

// ProviderSessionID provider 原生会话标识。
func (h *CodebuddyHarness) ProviderSessionID() *string { return h.base.ProviderSessionID() }

// EventBufferConfig 观测缓冲配置。
func (h *CodebuddyHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return h.base.EventBufferConfig()
}

// Start 开启周期。
func (h *CodebuddyHarness) Start(ctx context.Context, harnessContext harnessprotocol.HarnessContext) error {
	return h.base.Start(ctx, harnessContext)
}

// Stop 停止周期。
func (h *CodebuddyHarness) Stop(ctx context.Context) error { return h.base.Stop(ctx) }

// Events 全量事件游标。
func (h *CodebuddyHarness) Events() harnessprotocol.HarnessEventCursor { return h.base.Events() }

// TurnEvents 单轮事件游标。
func (h *CodebuddyHarness) TurnEvents(turnID *string) harnessprotocol.HarnessEventCursor {
	return h.base.TurnEvents(turnID)
}

// Send 接受输入。
func (h *CodebuddyHarness) Send(ctx context.Context, content harnessprotocol.HarnessInput,
	mode harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	return h.base.Send(ctx, content, mode)
}

// Abort 请求中止（GRACEFUL 已声明，走基座能力面）。
func (h *CodebuddyHarness) Abort(ctx context.Context, mode harnessprotocol.AbortMode) error {
	return h.base.Abort(ctx, mode)
}

// Pause 未声明 pause_resume（基座哨兵）。
func (h *CodebuddyHarness) Pause(ctx context.Context) error { return h.base.Pause(ctx) }

// Resume 未声明 pause_resume（基座哨兵）。
func (h *CodebuddyHarness) Resume(ctx context.Context, query *harnessprotocol.HarnessInput) error {
	return h.base.Resume(ctx, query)
}

// ExportCheckpoint 导出 checkpoint（session_id 载体，--resume 语义）。
func (h *CodebuddyHarness) ExportCheckpoint(ctx context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return h.base.ExportCheckpoint(ctx)
}

// ListModels 未声明 MODEL_DISCOVERY（基座哨兵语义，对齐 dsh harness.go:185-189）。
func (h *CodebuddyHarness) ListModels(_ context.Context) ([]harnessprotocol.ModelOption, error) {
	return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "codebuddy does not support model discovery",
	}
}

// SetModel 未声明模型选择面（基座哨兵）。
func (h *CodebuddyHarness) SetModel(ctx context.Context, selection harnessprotocol.ModelSelection) error {
	return h.base.SetModel(ctx, selection)
}

// ──────────────────────────── providerHooks 实现 ────────────────────────────

// ValidateContext 无宿主 overlay 需求（CLI 自带 --system-prompt 载体缺失，
// 系统提示词经环境注入由宿主后续章节接线；此处恒通过）。
func (h *CodebuddyHarness) ValidateContext(_ context.Context, _ harnessprotocol.HarnessContext) error {
	return nil
}

// OpenSession 启动 CLI 子进程周期。
// Python 无对应物；启动失败错误不携带 stderr 尾巴（对齐 dsh harness.py:126-128
// 敏感信息语义）。
// ⚠️ CLI 2.181.0 实测（偏差 D-15.22-c）：与 Claude Code 不同，CLI 在收到
// 首条 user 输入帧之后才发 init 帧——因此本方法不阻塞读 init，会话标识
// 协商推迟到 ExecuteTurn 首帧（返回 resumeSessionID 或占位标识）。
func (h *CodebuddyHarness) OpenSession(_ context.Context, harnessContext harnessprotocol.HarnessContext) (*string, error) {
	// 步骤 1：解析 cwd（上下文优先，配置兜底）
	cwd := derefString(harnessContext.Cwd)
	if cwd == "" {
		cwd = h.config.Cwd
	}

	// 步骤 2：解析恢复载体（起始 checkpoint 的 session_id）
	if checkpoint := harnessContext.Checkpoint; checkpoint != nil && checkpoint.Provider == "codebuddy" {
		if sessionID, ok := checkpoint.Data["session_id"].(string); ok && sessionID != "" {
			h.resumeSessionID = sessionID
		}
	}

	// 步骤 3：拼装启动参数（零值省略）
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--no-session-persistence",
	}
	if h.config.Model != "" {
		args = append(args, "--model", h.config.Model)
	}
	if h.config.PermissionMode != "" {
		args = append(args, "--permission-mode", h.config.PermissionMode)
	}
	if h.config.McpConfig != "" {
		args = append(args, "--mcp-config", h.config.McpConfig)
	}
	if h.resumeSessionID != "" {
		args = append(args, "--resume", h.resumeSessionID)
	}

	// 步骤 4：启动传输；失败清场且错误不携带 stderr 尾巴
	transport, err := h.transportFactory(TransportOptions{
		CliPath:           h.config.CliPath,
		Args:              args,
		Cwd:               cwd,
		Env:               h.config.Env,
		TurnTimeout:       time.Duration(h.config.TurnTimeoutSeconds) * time.Second,
		InactivityTimeout: time.Duration(h.config.InactivityTimeoutSeconds) * time.Second,
	})
	if err != nil {
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "failed to start the codebuddy CLI runtime",
		}
	}
	h.transport = transport
	// 会话标识协商推迟到 ExecuteTurn 首帧（init 帧在首条 user 输入后到达）；
	// 恢复载体已知时直接返回，否则返回占位标识
	h.sessionID = h.resumeSessionID
	if h.sessionID == "" {
		h.sessionID = newSessionID()
	}
	return &h.sessionID, nil
}

// CloseSession 释放 CLI 子进程。
func (h *CodebuddyHarness) CloseSession(_ context.Context) error {
	transport := h.transport
	h.transport = nil
	if transport != nil {
		return transport.Close()
	}
	return nil
}

// ExecuteTurn 把一个已接受的输入跑到底，期间发射观测事件。
// 读帧循环：逐帧 Consume → Emit；result 帧 → BuildTerminalResult；
// stop 请求 → ABORTED；读帧错误（超时/进程退出）→ FAILED（最小失败分类，
// 偏差 D-15.22-d）。
func (h *CodebuddyHarness) ExecuteTurn(ctx context.Context, turn *providers.PendingTurn) (
	harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	timing := providers.NewTurnTiming()
	// 单轮超时：config 提供时包一层 ctx（读帧循环靠 ctx 取消退出）。
	// Python 无对应物；dsh 的轮超时由 SDK 承担，CLI 形态在此承担。
	if h.config.TurnTimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(h.config.TurnTimeoutSeconds)*time.Second)
		defer cancel()
	}
	transport := h.transport
	sessionID := h.base.ProviderSessionID()
	if transport == nil || sessionID == nil {
		return "", harnessprotocol.TurnResult{}, &harnessprotocol.HarnessProtocolError{
			Msg: "codebuddy session disappeared during an active cycle",
		}
	}
	accumulator := NewCodebuddyTurnAccumulator(turn.TurnID, *sessionID)

	// 步骤 1：外发 user 输入帧（CLI 2.181.0 input 方向实测形状）
	if err := transport.SendFrame(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": inputText(turn.Content.Content)},
			},
		},
	}); err != nil {
		result, buildErr := h.buildFailedResult(accumulator, err, timing.StartedAt)
		if buildErr != nil {
			return "", harnessprotocol.TurnResult{}, buildErr
		}
		return harnessprotocol.TurnEventKindFailed, result, nil
	}

	// 步骤 2：读帧循环；首条 system/init 帧协商真实会话标识（CLI 在首条
	// user 输入后才发 init，见 OpenSession 注释）
	for {
		if turn.StopRequested() {
			result, err := h.buildAbortedResult(accumulator, timing.StartedAt)
			return harnessprotocol.TurnEventKindAborted, result, err
		}
		frame, err := transport.ReadFrame(ctx)
		if err != nil {
			if turn.StopRequested() {
				result, buildErr := h.buildAbortedResult(accumulator, timing.StartedAt)
				return harnessprotocol.TurnEventKindAborted, result, buildErr
			}
			result, buildErr := h.buildFailedResult(accumulator, err, timing.StartedAt)
			if buildErr != nil {
				return "", harnessprotocol.TurnResult{}, buildErr
			}
			return harnessprotocol.TurnEventKindFailed, result, nil
		}
		if frame.Type == "system" && frame.Subtype == "init" {
			// 会话标识协商：CLI 的原生 session_id 覆盖占位值
			if frame.SessionID != "" {
				h.sessionID = frame.SessionID
			}
			continue
		}
		mapped, err := accumulator.Consume(frame)
		if err != nil {
			return "", harnessprotocol.TurnResult{}, err
		}
		for _, item := range mapped {
			if emitErr := h.base.Emit(ctx, item.Payload, providers.EmitOptions{
				Turn:              turn,
				ItemID:            item.ItemID,
				ProviderSessionID: h.sessionID,
			}); emitErr != nil {
				return "", harnessprotocol.TurnResult{}, emitErr
			}
		}
		if frame.Type == "result" {
			kind, result, err := accumulator.BuildTerminalResult(frame, timing.StartedAt)
			if err != nil {
				return "", harnessprotocol.TurnResult{}, err
			}
			h.publishSessionCheckpoint(ctx)
			return kind, result, nil
		}
	}
}

// Steer 未声明 STEER 能力（基座默认体语义，对齐 dsh harness.go:194-199）。
func (h *CodebuddyHarness) Steer(_ context.Context, _ *providers.PendingTurn,
	_ harnessprotocol.HarnessInput, _ string) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "codebuddy does not support steering an active turn",
	}
}

// InterruptTurn GRACEFUL 外发 interrupt 控制帧；FORCE 杀进程。
// Python 无对应物；控制帧形状按 CLI 2.181.0（input 方向 control_request，
// 偏差 D-15.22-c）。
func (h *CodebuddyHarness) InterruptTurn(_ context.Context, _ *providers.PendingTurn,
	mode harnessprotocol.AbortMode) error {
	if h.transport == nil {
		return nil
	}
	if mode == harnessprotocol.AbortModeForce {
		return h.transport.Close()
	}
	requestID := uuid.NewString()
	return h.transport.SendFrame(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    map[string]any{"subtype": "interrupt"},
	})
}

// AuthFallbackPayload 未配置认证回退（基座默认体语义）。
func (h *CodebuddyHarness) AuthFallbackPayload() (harnessprotocol.JsonObject, bool) {
	return nil, false
}

// ReconnectForAuth 无认证回退（对齐 dsh harness.go:206-210）。
func (h *CodebuddyHarness) ReconnectForAuth(_ context.Context, _ bool) error {
	return &harnessprotocol.HarnessProtocolError{
		Msg: "codebuddy has no authentication fallback",
	}
}

// OnAuthEndpointChanged 默认 no-op。
func (h *CodebuddyHarness) OnAuthEndpointChanged(_ context.Context, _ bool) error { return nil }

// ApplyModelSelection 不支持运行期模型切换（重启换 --model 由宿主面承担）。
func (h *CodebuddyHarness) ApplyModelSelection(_ context.Context, _ harnessprotocol.ModelSelection) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "codebuddy does not support model selection",
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAbortedResult 合成 ABORTED 终态（interrupted 必须带 termination 的
// 协议不变量由 NewTurnResult 步骤 7 强制）。
func (h *CodebuddyHarness) buildAbortedResult(accumulator *CodebuddyTurnAccumulator,
	startedAt float64) (harnessprotocol.TurnResult, error) {
	termination, err := harnessprotocol.NewTurnTermination(
		harnessprotocol.TurnTerminationKindUserAbort, strPtr("aborted by host"), nil,
		harnessprotocol.JsonObject{"provider": providerName})
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusInterrupted, harnessprotocol.ResultOptions{
		FinalOutput:  accumulator.LastTextOutput(),
		Termination:  &termination,
		Usage:        accumulator.Usage(),
		StartedAt:    &startedAt,
		ProviderData: harnessprotocol.JsonObject{"provider": providerName},
	})
}

// buildFailedResult 合成 FAILED 终态（最小失败分类，偏差 D-15.22-d）。
func (h *CodebuddyHarness) buildFailedResult(accumulator *CodebuddyTurnAccumulator,
	cause error, startedAt float64) (harnessprotocol.TurnResult, error) {
	message := cause.Error()
	turnError, err := harnessprotocol.NewTurnError(message, nil, nil, nil,
		harnessprotocol.JsonObject{"provider": providerName})
	if err != nil {
		return harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.NewTurnResult(harnessprotocol.TurnStatusFailed, harnessprotocol.ResultOptions{
		Error:        &turnError,
		Usage:        accumulator.Usage(),
		StartedAt:    &startedAt,
		ProviderData: harnessprotocol.JsonObject{"provider": providerName},
	})
}

// publishSessionCheckpoint 把当前会话标识发布为 checkpoint。
func (h *CodebuddyHarness) publishSessionCheckpoint(ctx context.Context) {
	if h.sessionID == "" {
		return
	}
	_, _ = h.base.PublishCheckpoint(ctx,
		harnessprotocol.JsonObject{"session_id": h.sessionID},
		harnessprotocol.CheckpointReasonTurnCompleted, "1")
}

// derefString 空安全字符串解引用（dsh provider.go 同位助手）。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// inputText 从 HarnessInput.Content（JsonValue）提取文本。
func inputText(content harnessprotocol.JsonValue) string {
	if text, ok := content.(string); ok {
		return text
	}
	return ""
}

// strPtr 字符串指针。
func strPtr(value string) *string { return &value }

// newSessionID 生成会话/检查点标识。
func newSessionID() string { return uuid.NewString() }

// 编译期兜底：CodebuddyHarness 必须满足完整协议面。
var _ harnessprotocol.HarnessProtocol = (*CodebuddyHarness)(nil)

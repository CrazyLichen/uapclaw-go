package dsh

import (
	"context"
	"encoding/json"
	"fmt"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// ──────────────────────────── 常量 ────────────────────────────

// AdapterVersion 适配器版本。Python: harness.py:36 ADAPTER_VERSION
const AdapterVersion = "0.3.0"

// dshScanDir dsh 技能发现目录（Python skills.py:19 _SCAN_DIRS["dsh"]）
const dshScanDir = ".dsh/skills"

// ──────────────────────────── 结构体 ────────────────────────────

// SDKHarness SDK 句柄缝（Python: harness.py:118-121 sdk.DeepSeekHarness 四个调用点）。
// 这是 Python importlib.import_module("deepseek_harness") 的 Go 等价缝：
// 默认实现包装 theoneLee SDK；测试注入 fake（偏差 D-dsh-2：Go 侧 SDK 编译期
// 内嵌恒存在，缺依赖分支不可达，错误面由 fake 工厂注入覆盖）。
type SDKHarness interface {
	// StartSession 启动一个可复用会话。Python: harness.py:121 start_session(session_id)
	StartSession(sessionID string) (SDKSession, error)
	// Close 关闭运行时（幂等）。Python: harness.py:143 sdk_harness.close
	Close() error
}

// SDKSession 一个会话句柄。
// Python: session.run(input, on_notification=cb)（harness.py:166-169）
type SDKSession interface {
	// Run 执行一个 prompt 到 idle；onNotification 在 Run 的订阅循环内同步调用
	// （偏差 D-dsh-1：Python 经 run_coroutine_threadsafe 回投事件循环，
	// Go 同步直调，顺序语义一致）。
	Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error)
}

// SDKOptions 传给 SDK 工厂的完整启动选项。
// Python: harness.py:205-228 _sdk_options 的 dict + harness.py:104-113 的
// overlay env/patches 合并结果。
type SDKOptions struct {
	// Config provider 自有配置
	Config DshHarnessConfig
	// Context 活跃周期上下文（cwd/system prompt/env 源）
	Context *harnessprotocol.HarnessContext
	// ExtraEnv overlay 注入的环境变量（Python: options["env"].update(env)）
	ExtraEnv map[string]string
	// ExtraPatches overlay 注入的 patch 路径（Python: options["patches"] 追加）
	ExtraPatches []string
}

// SDKFactory SDK 工厂缝类型（NewDshHarnessWithFactory 注入点）。
type SDKFactory func(options SDKOptions) (SDKHarness, error)

// DshHarness 把一个可复用的 DeepSeek Harness 会话适配到协议 v1。
// 每个宿主 Turn 是一段串行化的 DSH 活动区间：输入接受到 whole-agent idle。
// DSH 自身的 turn/step 记录保持为内部/provider 观测，不重定义公开 Turn 边界。
// Python: harness.py:39-62 DshHarness
type DshHarness struct {
	// SerializedTurnHarness 基座（Card/Emit/14 公开方法由嵌入提升）
	*hp.SerializedTurnHarness
	// config provider 自有配置（Python: _config）
	config DshHarnessConfig
	// sdkFactory SDK 工厂缝（Python: _load_dsh_sdk 的 importlib 等价）
	sdkFactory SDKFactory
	// sdkHarness SDK 句柄（Python: _sdk_harness）
	sdkHarness SDKHarness
	// sdkSession 会话句柄（Python: _sdk_session）
	sdkSession SDKSession
	// overlay 临时 overlay（Python: _overlay）
	overlay *overlayResult
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// dshCard 能力卡（Python: harness.py:48-55 类属性 card）
	dshCard = mustCard()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDshHarness 创建未启动的 DSH 适配器；config 为零值时取 dataclass 缺省。
// Python: harness.py:57-62 __init__（config or DshHarnessConfig()）
func NewDshHarness(config DshHarnessConfig) (*DshHarness, error) {
	return NewDshHarnessWithFactory(config, defaultSDKFactory)
}

// NewDshHarnessWithFactory 以显式 SDK 工厂创建适配器（测试注入 fake 的缝）。
// Python 无对应物：importlib 缝在 Go 侧显式化为构造参数（偏差 D-dsh-2）。
func NewDshHarnessWithFactory(config DshHarnessConfig, factory SDKFactory) (*DshHarness, error) {
	if config.Provider == "" && config.Model == "" {
		config = DefaultDshHarnessConfig()
	} else {
		var err error
		config, err = NewDshHarnessConfig(config)
		if err != nil {
			return nil, err
		}
	}
	if factory == nil {
		factory = defaultSDKFactory
	}
	h := &DshHarness{config: config, sdkFactory: factory}
	h.SerializedTurnHarness = hp.NewSerializedTurnHarness(hp.HarnessOptions{
		Card:                dshCard,
		Hooks:               h,
		EventBufferCapacity: config.EventBufferCapacity,
	})
	return h, nil
}

// Card 能力卡。Python: harness.py:48-55（类属性经 provider.card 转发）
func (h *DshHarness) Card() harnessprotocol.HarnessCard { return dshCard }

// NeedsHostOverlay 报告该上下文是否只能经宿主 overlay 满足。
// Python: harness.py:68-82 _needs_host_overlay
func (h *DshHarness) NeedsHostOverlay(context *harnessprotocol.HarnessContext) bool {
	if len(h.config.Skills) > 0 && h.config.Profile == "sdk-minimal" {
		return true
	}
	if len(context.McpServers) > 0 {
		return true
	}
	return context.SystemPrompt != "" && h.config.SystemPromptEnvVar == ""
}

// ValidateContext 基座校验后追加 DSH SDK 限制。
// Python: harness.py:84-90 _validate_context（super 由基座先做 card.ValidateHost）
func (h *DshHarness) ValidateContext(_ context.Context, harnessContext harnessprotocol.HarnessContext) error {
	// Python: harness.py:86-87 checkpoint 恢复不支持
	if harnessContext.ResumePolicy == harnessprotocol.ResumePolicyRequireResume ||
		harnessContext.Checkpoint != nil {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: "the DSH SDK server cannot restore protocol checkpoints",
		}
	}
	// Python: harness.py:88 mcp_configs(context) 预演
	if _, err := MCPConfigs(&harnessContext); err != nil {
		return err
	}
	// Python: harness.py:89-90 launch_args_override 与 overlay 互斥
	if h.config.LaunchArgsOverride != nil && h.NeedsHostOverlay(&harnessContext) {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: "DSH host overlays require the standard profile launcher",
		}
	}
	return nil
}

// OpenSession 启动一个全新 DSH 子进程/会话周期。
// Python: harness.py:92-135 _open_session
func (h *DshHarness) OpenSession(ctx context.Context, harnessContext harnessprotocol.HarnessContext) (*string, error) {
	_ = ctx
	// Python: harness.py:94-95 install_skills（15.12 provider 接线回填点①）
	cwd := h.config.Cwd
	if harnessContext.Cwd != nil && *harnessContext.Cwd != "" {
		cwd = *harnessContext.Cwd
	}
	if _, err := hp.InstallSkills(h.config.Skills, "dsh", cwd,
		h.config.SkillConflict, dshScanDir); err != nil {
		return nil, err
	}

	// Python: harness.py:97-99 先解析依赖与 SDK 选项，缺失不得留半开周期
	overlayContext := harnessContext
	if overlayContext.Cwd == nil || *overlayContext.Cwd == "" {
		overlayContext.Cwd = &cwd
	}
	overlay, err := WriteOverlay(&overlayContext,
		h.config.SystemPromptEnvVar == "",
		h.config.SystemPromptMode,
		len(h.config.Skills) > 0 && h.config.Profile == "sdk-minimal",
	)
	if err != nil {
		return nil, err
	}
	var extraEnv map[string]string
	var extraPatches []string
	if overlay != nil {
		h.overlay = overlay
		extraEnv = map[string]string{overlay.envName: overlay.envValue}
		extraPatches = []string{overlay.path}
	}

	// Python: harness.py:100-102
	options := SDKOptions{
		Config: h.config, Context: &harnessContext,
		ExtraEnv: extraEnv, ExtraPatches: extraPatches,
	}
	sdkHarness, err := h.sdkFactory(options)
	if err != nil {
		h.cleanupOverlay()
		return nil, err
	}
	sessionID := fmt.Sprintf("dsh-%s", randomHex())
	// Python: harness.py:115-132 启动失败清场且不携带 stderr 尾巴
	// （context.env 与 provider 凭据是敏感的，不得作为异常 cause 保留）
	sdkSession, err := sdkHarness.StartSession(sessionID)
	if err != nil {
		_ = sdkHarness.Close()
		h.cleanupOverlay()
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "failed to start the DeepSeek Harness SDK runtime",
		}
	}
	h.sdkHarness = sdkHarness
	h.sdkSession = sdkSession
	return &sessionID, nil
}

// CloseSession 释放 SDK 运行时并清理 overlay。
// Python: harness.py:137-147 _close_session
func (h *DshHarness) CloseSession(_ context.Context) error {
	sdkHarness := h.sdkHarness
	h.sdkHarness = nil
	h.sdkSession = nil
	var err error
	if sdkHarness != nil {
		// Python: harness.py:142 _close_sdk_quietly（close 失败仅 debug 记录）
		err = sdkHarness.Close()
	}
	h.cleanupOverlay()
	return err
}

// ExecuteTurn 把一个已接受的输入跑到底并发射观测事件。
// Python: harness.py:149-186 _execute_turn
func (h *DshHarness) ExecuteTurn(
	ctx context.Context, turn *hp.PendingTurn,
) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	timing := hp.NewTurnTiming()
	session := h.sdkSession
	sessionID := h.ProviderSessionID()
	if session == nil || sessionID == nil {
		return "", harnessprotocol.TurnResult{}, &harnessprotocol.HarnessProtocolError{
			Msg: "DSH session disappeared during an active cycle",
		}
	}
	accumulator := NewDshTurnAccumulator(turn.TurnID, *sessionID)

	// Python: harness.py:158-163 on_notification：SDK 工作线程 → 事件循环回投；
	// Go 侧回调在 Run 订阅循环内同步调用（偏差 D-dsh-1），直接规约并 emit
	onNotification := func(notification deepseekharness.Notification) {
		mapped, err := accumulator.Consume(notification)
		if err != nil {
			return
		}
		for _, item := range mapped {
			_ = h.emitMapped(ctx, turn, item)
		}
	}

	runResult, err := session.Run(toDshInput(turn.Content), onNotification)
	if err != nil {
		// Python: harness.py:172-181 stop 请求 → 停止结果；否则失败结果
		if turn.StopRequested() {
			result, buildErr := accumulator.BuildStoppedResult(timing.StartedAt, timing.StartedMonotonic)
			return harnessprotocol.TurnEventKindAborted, result, buildErr
		}
		result, buildErr := accumulator.BuildFailedResult(err, timing.StartedAt, timing.StartedMonotonic)
		return harnessprotocol.TurnEventKindFailed, result, buildErr
	}
	return accumulator.BuildTerminalResult(runResult, timing.StartedAt, timing.StartedMonotonic)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Steer DSH 不声明 STEER 能力；基座门卫先行拦截，此为不可达兜底。
// Python: base.py:210-218 _steer 默认体
func (h *DshHarness) Steer(_ context.Context, _ *hp.PendingTurn,
	_ harnessprotocol.HarnessInput, _ string) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: dshCard.Name + " does not support steering",
	}
}

// InterruptTurn DSH 未声明 abort 能力；基座门卫先行拦截，不可达兜底。
// Python: base.py:220-223 _interrupt_turn 默认体
func (h *DshHarness) InterruptTurn(_ context.Context, _ *hp.PendingTurn,
	_ harnessprotocol.AbortMode) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: dshCard.Name + " does not support turn abort",
	}
}

// ListModels DSH 未声明 MODEL_DISCOVERY；不可达兜底。
// Python: base.py:225-227 _list_models 默认体
func (h *DshHarness) ListModels(_ context.Context) ([]harnessprotocol.ModelOption, error) {
	return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: dshCard.Name + " does not support model discovery",
	}
}

// AuthFallbackPayload DSH 无认证回退设置（provider.py:33-34 显式拒绝）。
// Python: base.py:229-239 _auth_fallback_payload 默认体（未配置 → false）
func (h *DshHarness) AuthFallbackPayload() (harnessprotocol.JsonObject, bool) {
	return nil, false
}

// ReconnectForAuth DSH 无回退重连；不可达兜底。
// Python: base.py:241-252 _reconnect_for_auth 默认体
func (h *DshHarness) ReconnectForAuth(_ context.Context, _ bool) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: dshCard.Name + " does not support authentication fallback",
	}
}

// OnAuthEndpointChanged DSH 无端点切换记录；不可达兜底。
// Python: base.py:254-256 _on_auth_endpoint_changed 默认体
func (h *DshHarness) OnAuthEndpointChanged(_ context.Context, _ bool) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: dshCard.Name + " does not support authentication fallback",
	}
}

// ApplyModelSelection DSH 未声明 MODEL_SELECTION；不可达兜底。
// Python: base.py:258-265 _apply_model_selection 默认体
func (h *DshHarness) ApplyModelSelection(_ context.Context,
	_ harnessprotocol.ModelSelection) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: dshCard.Name + " does not support model selection",
	}
}

// emitMapped 发射一条规约事件。Python: harness.py:197-203 _emit_mapped
func (h *DshHarness) emitMapped(ctx context.Context, turn *hp.PendingTurn, mapped mappedDshEvent) error {
	return h.Emit(ctx, mapped.payload, hp.EmitOptions{
		Turn:              turn,
		ItemID:            mapped.itemID,
		ProviderSessionID: mapped.providerSessionID,
	})
}

// cleanupOverlay 清理并置空 overlay。
func (h *DshHarness) cleanupOverlay() {
	if h.overlay != nil {
		h.overlay.cleanup()
		h.overlay = nil
	}
}

// defaultSDKFactory 默认工厂：包装 theoneLee SDK。
// Python: harness.py:118-120 sdk.DeepSeekHarness(**options)；
// _sdk_options 的 filter-None → Go 零值省略（SDK mergeHarnessConfig 等价，
// 偏差 D-dsh-2：缺依赖分支不可达）。
func defaultSDKFactory(options SDKOptions) (SDKHarness, error) {
	config := options.Config
	// Python: harness.py:206-209 env 合并：config.env ← context.env ← 系统提示词变量
	env := map[string]string{}
	for key, value := range config.Env {
		env[key] = value
	}
	for key, value := range options.Context.Env {
		env[key] = value
	}
	for key, value := range options.ExtraEnv {
		env[key] = value
	}
	if options.Context.SystemPrompt != "" && config.SystemPromptEnvVar != "" {
		env[config.SystemPromptEnvVar] = options.Context.SystemPrompt
	}
	// Python: harness.py:215 cwd = context.cwd or self._config.cwd
	cwd := config.Cwd
	if options.Context.Cwd != nil && *options.Context.Cwd != "" {
		cwd = *options.Context.Cwd
	}
	// Python: harness.py:116-120 launch_args_override 分支
	patches := append(append([]string{}, config.Patches...), options.ExtraPatches...)
	sdkConfig := deepseekharness.DeepSeekHarnessConfig{
		Provider:                 config.Provider,
		Model:                    config.Model,
		ReasoningEffort:          config.ReasoningEffort,
		MaxTokens:                config.MaxTokens,
		CWD:                      cwd,
		RuntimeCWD:               config.RuntimeCwd,
		DSHBin:                   config.DshBin,
		DSHHome:                  config.DshHome,
		Profile:                  config.Profile,
		Patches:                  patches,
		Env:                      env,
		InitializeTimeoutSeconds: config.InitializeTimeoutSeconds,
		RequestTimeoutSeconds:    config.RequestTimeoutSeconds,
		ShutdownTimeoutSeconds:   config.ShutdownTimeoutSeconds,
		BaseURL:                  config.BaseURL,
		APIKey:                   config.APIKey,
	}
	if config.LaunchArgsOverride != nil {
		sdkConfig.LaunchArgsOverride = config.LaunchArgsOverride
	}
	return &goSDKHarness{inner: deepseekharness.NewDeepSeekHarness(sdkConfig)}, nil
}

// goSDKHarness SDKHarness 的真实 SDK 包装。
type goSDKHarness struct {
	inner *deepseekharness.DeepSeekHarness
}

// StartSession Python: harness.py:121 start_session(session_id)
func (g *goSDKHarness) StartSession(sessionID string) (SDKSession, error) {
	session, err := g.inner.StartSession(sessionID)
	if err != nil {
		return nil, err
	}
	return &goSDKSession{inner: session}, nil
}

// Close Python: harness.py:143 sdk_harness.close
func (g *goSDKHarness) Close() error { return g.inner.Close() }

// goSDKSession SDKSession 的真实 SDK 包装。
type goSDKSession struct {
	inner *deepseekharness.Session
}

// Run Python: harness.py:166-169 session.run(input, on_notification=...)
func (g *goSDKSession) Run(
	input any, onNotification func(deepseekharness.Notification),
) (*deepseekharness.RunResult, error) {
	return g.inner.Run(input, onNotification)
}

// toDshInput 规约协议输入为 DSH 可接受的形状。
// Python: harness.py:247-253 _to_dsh_input：string 直传 / 全 dict 块数组直传 /
// 其余 JSON 紧凑序列化。
func toDshInput(content harnessprotocol.HarnessInput) any {
	value := harnessprotocol.JsonValueToBuiltin(content.Content)
	if str, ok := value.(string); ok {
		return str
	}
	if list, ok := value.([]any); ok {
		allDict := true
		for _, item := range list {
			if _, isDict := item.(map[string]any); !isDict {
				allDict = false
				break
			}
		}
		if allDict {
			return list
		}
	}
	// Python: json.dumps(value, ensure_ascii=False, separators=(",", ":"))
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(b)
}

// mustCard 构造能力卡（Python harness.py:48-55；构造失败属程序员错误）。
func mustCard() harnessprotocol.HarnessCard {
	card, err := harnessprotocol.NewHarnessCardWithOptions("deepseek-harness", AdapterVersion,
		harnessprotocol.CardOptions{
			Capabilities: map[harnessprotocol.HarnessCapability]bool{
				harnessprotocol.HarnessCapabilityMcpTools:       true,
				harnessprotocol.HarnessCapabilityEndpointModels: true,
			},
			OptionalHostCapabilities: map[harnessprotocol.HostCapability]bool{
				harnessprotocol.HostCapabilityMcpServers: true,
			},
		})
	if err != nil {
		panic(fmt.Sprintf("dsh: card must construct: %v", err))
	}
	return card
}

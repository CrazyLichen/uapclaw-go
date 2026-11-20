package dsh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	providers "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
)

// ──────────────────────────── 常量 ────────────────────────────

// AdapterVersion 适配器版本。
// Python: harness.py:36 ADAPTER_VERSION
const AdapterVersion = "0.3.0"

// dshSkillScanDir dsh 的技能发现目录（Python skills.py:19 _SCAN_DIRS["dsh"]）。
const dshSkillScanDir = ".dsh/skills"

// ──────────────────────────── 全局变量 ────────────────────────────

// dshCard DshHarness 的能力卡（Python: harness.py:48-55 类属性 card）。
// capabilities = {MCP_TOOLS, ENDPOINT_MODELS}；optional host = {MCP_SERVERS}。
var dshCard = func() harnessprotocol.HarnessCard {
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
		panic(fmt.Sprintf("dsh card 构造失败: %v", err))
	}
	return card
}()

// ──────────────────────────── 结构体 ────────────────────────────

// SDKFactory Python importlib.import_module("deepseek_harness") + 构造
// DeepSeekHarness 的 Go 等价缝（harness.py:100+118-120：取 SDK 并按 options
// 构造句柄）。默认实现包装 theoneLee SDK；测试注入 fake。
// 偏差 D-dsh-2：Go 侧 SDK 编译期内嵌恒存在，Python 的「缺依赖 → HarnessError」
// 分支不可达；错误面由 fake 工厂注入覆盖。
type SDKFactory func(options GoSDKOptions) (DshSDKHarness, error)

// DshSDKHarness SDK 句柄缝（Python: harness.py:118-121 sdk.DeepSeekHarness
// 的四个调用点中的三个）。
type DshSDKHarness interface {
	// StartSession Python: harness.py:121 sdk_harness.start_session(session_id)
	StartSession(sessionID string) (DshSDKSession, error)
	// Close Python: harness.py:143 sdk_harness.close（幂等）
	Close() error
}

// DshSDKSession Python: session.run(input, on_notification=cb)
type DshSDKSession interface {
	Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error)
}

// goSDKHarness 真实 SDK 的薄包装（默认工厂实现）。
type goSDKHarness struct {
	inner *deepseekharness.DeepSeekHarness
}

// StartSession 会话句柄包装。
func (g *goSDKHarness) StartSession(sessionID string) (DshSDKSession, error) {
	session, err := g.inner.StartSession(sessionID)
	if err != nil {
		return nil, err
	}
	return &goSDKSession{inner: session}, nil
}

// Close 关闭运行时（幂等由 SDK 保证）。
func (g *goSDKHarness) Close() error { return g.inner.Close() }

// goSDKSession 会话句柄包装。
type goSDKSession struct {
	inner *deepseekharness.Session
}

// Run 一次提示到 idle 的活动区间。
func (g *goSDKSession) Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error) {
	return g.inner.Run(input, onNotification)
}

// DshHarness 把一个可复用的 DeepSeek Harness 会话适配到协议 v1。
//
// 每个宿主 Turn 是一段串行化的 DSH 活动区间（harness.py:40-46 docstring）。
// Python: harness.py:39-62 DshHarness
type DshHarness struct {
	// base 15.5 基座（提供 Emit / 14 公开方法）
	base *providers.SerializedTurnHarness
	// config provider 自有配置
	config DshHarnessConfig
	// sdkFactory SDK 缝（测试注入点）
	sdkFactory SDKFactory
	// sdkHarness 活跃 SDK 句柄（Python: _sdk_harness）
	sdkHarness DshSDKHarness
	// sdkSession 活跃 SDK 会话（Python: _sdk_session）
	sdkSession DshSDKSession
	// overlay 活跃 overlay（Python: _overlay）
	overlay *OverlayResult
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDshHarness 构造未启动的适配器（Python: harness.py:57-62 __init__）。
func NewDshHarness(config DshHarnessConfig) (*DshHarness, error) {
	return NewDshHarnessWithFactory(config, nil)
}

// NewDshHarnessWithFactory 构造并允许注入 SDK 工厂（nil 用默认真实 SDK）。
func NewDshHarnessWithFactory(config DshHarnessConfig, factory SDKFactory) (*DshHarness, error) {
	validated, err := NewDshHarnessConfig(config)
	if err != nil {
		return nil, err
	}
	h := &DshHarness{
		config:     validated,
		sdkFactory: factory,
	}
	if h.sdkFactory == nil {
		h.sdkFactory = defaultSDKFactory
	}
	h.base = providers.NewSerializedTurnHarness(providers.HarnessOptions{
		Card:                dshCard,
		Hooks:               h,
		EventBufferCapacity: validated.EventBufferCapacity,
	})
	return h, nil
}

// ──────────────────────────── 协议面转发（对齐 native 包装集） ────────────────────────────

// Card 能力卡。
func (h *DshHarness) Card() harnessprotocol.HarnessCard { return h.base.Card() }

// State 当前相位。
func (h *DshHarness) State() harnessprotocol.HarnessState { return h.base.State() }

// ProviderSessionID provider 原生会话标识。
func (h *DshHarness) ProviderSessionID() *string { return h.base.ProviderSessionID() }

// EventBufferConfig 观测缓冲配置。
func (h *DshHarness) EventBufferConfig() harnessprotocol.EventBufferConfig {
	return h.base.EventBufferConfig()
}

// Start 开启周期。
func (h *DshHarness) Start(ctx context.Context, harnessContext harnessprotocol.HarnessContext) error {
	return h.base.Start(ctx, harnessContext)
}

// Stop 停止周期。
func (h *DshHarness) Stop(ctx context.Context) error { return h.base.Stop(ctx) }

// Events 全量事件游标。
func (h *DshHarness) Events() harnessprotocol.HarnessEventCursor { return h.base.Events() }

// TurnEvents 单轮事件游标。
func (h *DshHarness) TurnEvents(turnID *string) harnessprotocol.HarnessEventCursor {
	return h.base.TurnEvents(turnID)
}

// Send 接受输入。
func (h *DshHarness) Send(ctx context.Context, content harnessprotocol.HarnessInput,
	mode harnessprotocol.DeliveryMode) (harnessprotocol.SendReceipt, error) {
	return h.base.Send(ctx, content, mode)
}

// Abort 请求中止。
func (h *DshHarness) Abort(ctx context.Context, mode harnessprotocol.AbortMode) error {
	return h.base.Abort(ctx, mode)
}

// Pause 暂停（dsh 未声明，走基座哨兵）。
func (h *DshHarness) Pause(ctx context.Context) error { return h.base.Pause(ctx) }

// Resume 恢复（dsh 未声明，走基座哨兵）。
func (h *DshHarness) Resume(ctx context.Context, query *harnessprotocol.HarnessInput) error {
	return h.base.Resume(ctx, query)
}

// ExportCheckpoint 导出 checkpoint（dsh 不支持）。
func (h *DshHarness) ExportCheckpoint(ctx context.Context) (*harnessprotocol.HarnessCheckpoint, error) {
	return h.base.ExportCheckpoint(ctx)
}

// ListModels 未声明 MODEL_DISCOVERY（Python: base.py:225-227 默认体语义）。
func (h *DshHarness) ListModels(_ context.Context) ([]harnessprotocol.ModelOption, error) {
	return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "deepseek-harness does not support model discovery",
	}
}

// SetModel 未声明模型选择面（基座哨兵）。
func (h *DshHarness) SetModel(ctx context.Context, selection harnessprotocol.ModelSelection) error {
	return h.base.SetModel(ctx, selection)
}

// ──────────────────────────── providerHooks 实现 ────────────────────────────

// NeedsHostOverlay 判断该上下文是否只能经宿主 overlay 服务。
// Python: harness.py:68-82 _needs_host_overlay
func (h *DshHarness) NeedsHostOverlay(context *harnessprotocol.HarnessContext) bool {
	// skills + sdk-minimal profile 需要插件注入
	if len(h.config.Skills) > 0 && h.config.Profile == "sdk-minimal" {
		return true
	}
	if len(context.McpServers) > 0 {
		return true
	}
	return context.SystemPrompt != "" && h.config.SystemPromptEnvVar == ""
}

// ValidateContext 宿主校验 + SDK 限制。
// Python: harness.py:84-90 _validate_context
func (h *DshHarness) ValidateContext(_ context.Context, harnessContext harnessprotocol.HarnessContext) error {
	// Python: harness.py:86-88（REQUIRE_RESUME / checkpoint 不支持）
	if harnessContext.ResumePolicy == harnessprotocol.ResumePolicyRequireResume || harnessContext.Checkpoint != nil {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: "the DSH SDK server cannot restore protocol checkpoints",
		}
	}
	// Python: harness.py:88 mcp_configs(context) 预演
	if _, err := MCPConfigs(&harnessContext); err != nil {
		return err
	}
	// Python: harness.py:89-90（overlay 需要标准 profile launcher）
	if h.config.LaunchArgsOverride != nil && h.NeedsHostOverlay(&harnessContext) {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: "DSH host overlays require the standard profile launcher",
		}
	}
	return nil
}

// OpenSession 启动一段 DSH 子进程/会话周期。
// Python: harness.py:92-135 _open_session
func (h *DshHarness) OpenSession(ctx context.Context, harnessContext harnessprotocol.HarnessContext) (*string, error) {
	// 步骤 1：安装 portable skills（harness.py:94-95；15.12 接线回填点①）
	cwd := derefString(harnessContext.Cwd)
	if cwd == "" {
		cwd = h.config.Cwd
	}
	if _, err := providers.InstallSkills(
		h.config.Skills, "dsh", cwd, h.config.SkillConflict, dshSkillScanDir); err != nil {
		return nil, err
	}

	// 步骤 2：解析可选依赖与纯 SDK 选项，再开可观测的协议周期；
	// 缺 SDK 不得留下半启动的会话（harness.py:97-102）
	options := h.sdkOptions(&harnessContext)
	sessionID := newSessionID()
	overlayContext := harnessContext
	if harnessContext.Cwd == nil || *harnessContext.Cwd == "" {
		// Python: harness.py:103 replace(context, cwd=context.cwd or config.cwd)
		overlayContext.Cwd = &cwd
	}
	overlay, err := WriteOverlay(
		&overlayContext,
		h.config.SystemPromptEnvVar == "",
		h.config.SystemPromptMode,
		len(h.config.Skills) > 0 && h.config.Profile == "sdk-minimal",
	)
	if err != nil {
		return nil, err
	}
	if overlay != nil {
		h.overlay = overlay
		options.Env[overlay.EnvName()] = overlay.EnvValue()
		options.Patches = append(options.Patches, overlay.Path())
	}
	// 步骤 3：启动 SDK harness + 会话；失败清场且错误不携带 stderr 尾巴
	// （harness.py:114-132：context.env 与 provider 凭据是敏感信息）
	sdkHarness, startErr := h.sdkFactory(options)
	if startErr != nil {
		overlay.Cleanup()
		h.overlay = nil
		// Python: harness.py:126-128——SDK transport 错误可能带子进程 stderr
		// 尾巴；不得作为异常 cause 保留（工厂错误 = 构造/启动失败）
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "failed to start the DeepSeek Harness SDK runtime",
		}
	}
	h.sdkHarness = sdkHarness
	session, err := sdkHarness.StartSession(sessionID)
	if err != nil {
		_ = sdkHarness.Close()
		overlay.Cleanup()
		h.overlay = nil
		return nil, &harnessprotocol.HarnessProtocolError{
			Msg: "failed to start the DeepSeek Harness SDK runtime",
		}
	}
	h.sdkSession = session
	return &sessionID, nil
}

// CloseSession 释放 SDK 运行时并清理 overlay。
// Python: harness.py:137-147 _close_session
func (h *DshHarness) CloseSession(_ context.Context) error {
	sdkHarness := h.sdkHarness
	h.sdkHarness = nil
	h.sdkSession = nil
	var closeErr error
	if sdkHarness != nil {
		// Python: harness.py:142-144 _close_sdk_quietly（close 失败仅 debug 日志）
		closeErr = sdkHarness.Close()
	}
	// Python: harness.py:144-147 finally：无论 close 成败都清理 overlay
	if h.overlay != nil {
		h.overlay.Cleanup()
		h.overlay = nil
	}
	return closeErr
}

// ExecuteTurn 把一个已接受的输入跑到底，期间发射观测事件。
// Python: harness.py:149-186 _execute_turn
func (h *DshHarness) ExecuteTurn(ctx context.Context, turn *providers.PendingTurn) (
	harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	timing := providers.NewTurnTiming()
	session := h.sdkSession
	sessionID := h.base.ProviderSessionID()
	if session == nil || sessionID == nil {
		return "", harnessprotocol.TurnResult{}, &harnessprotocol.HarnessProtocolError{
			Msg: "DSH session disappeared during an active cycle",
		}
	}
	accumulator := NewDshTurnAccumulator(turn.TurnID, *sessionID)

	// Python: harness.py:158-163——on_notification 在 SDK 线程回调后经
	// run_coroutine_threadsafe 阻塞回投事件循环（背压）；偏差 D-dsh-1：
	// Go SDK 回调在 Run 订阅循环内同步调用，适配器直接 emit，顺序语义一致。
	onNotification := func(notification deepseekharness.Notification) {
		if err := h.handleNotification(ctx, turn, accumulator, notification); err != nil {
			// 通知映射失败与 Python future.result() 抛错等价：中断 run，
			// 错误经 Run 的 error 路径归约（Python 的 await 抛出由 SDK 捕获）
			panic(err)
		}
	}

	runResult, runErr := session.Run(toDshInput(turn.Content), onNotification)
	if runErr != nil {
		// Python: harness.py:171-181
		if turn.StopRequested() {
			result, err := accumulator.BuildStoppedResult(timing.StartedAt, timing.StartedMonotonic)
			return harnessprotocol.TurnEventKindAborted, result, err
		}
		result, err := accumulator.BuildFailedResult(runErr, timing.StartedAt, timing.StartedMonotonic)
		return harnessprotocol.TurnEventKindFailed, result, err
	}
	return accumulator.BuildTerminalResult(runResult, timing.StartedAt, timing.StartedMonotonic)
}

// Steer 未声明 STEER 能力（Python: base.py:210-218 默认体语义）。
func (h *DshHarness) Steer(_ context.Context, _ *providers.PendingTurn,
	_ harnessprotocol.HarnessInput, _ string) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "deepseek-harness does not support steering an active turn",
	}
}

// InterruptTurn 停止由 SDK 消费的中断（dsh 由 Stop 承担，轮内不可中断）。
// Python: base.py:220-223 默认体语义。
func (h *DshHarness) InterruptTurn(_ context.Context, _ *providers.PendingTurn,
	_ harnessprotocol.AbortMode) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "deepseek-harness does not support aborting an active turn",
	}
}

// AuthFallbackPayload 未配置认证回退（provider.py:24-41 model_config 拒绝
// fallback 的能力面；dsh 的「认证回退」是显式不支持）。
// Python: base.py:229-239 默认体语义。
func (h *DshHarness) AuthFallbackPayload() (harnessprotocol.JsonObject, bool) { return nil, false }

// ReconnectForAuth 无认证回退。
// Python: base.py:241-252 默认体语义。
func (h *DshHarness) ReconnectForAuth(_ context.Context, _ bool) error {
	return &harnessprotocol.HarnessProtocolError{
		Msg: "deepseek-harness has no authentication fallback",
	}
}

// OnAuthEndpointChanged 默认 no-op。
// Python: base.py:254-256 默认体。
func (h *DshHarness) OnAuthEndpointChanged(_ context.Context, _ bool) error { return nil }

// ApplyModelSelection 不支持模型选择。
// Python: base.py:258-265 默认体语义。
func (h *DshHarness) ApplyModelSelection(_ context.Context, _ harnessprotocol.ModelSelection) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "deepseek-harness does not support model selection",
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SDKOptions 纯 SDK 选项（测试可见）。
// Python: harness.py:205-228 _sdk_options
func (h *DshHarness) SDKOptions(context *harnessprotocol.HarnessContext) GoSDKOptions {
	return h.sdkOptions(context)
}

// GoSDKOptions 传给真实 SDK 的选项集（Python _sdk_options 返回 dict 的
// Go 具名形态；filter-None 由各字段的「空值省略」承担，偏差 D-dsh-5 同款
// 零值语义）。
type GoSDKOptions struct {
	Provider                 string
	Model                    string
	ReasoningEffort          string
	MaxTokens                int
	Cwd                      string
	RuntimeCwd               string
	DshBin                   string
	DshHome                  string
	Profile                  string
	Patches                  []string
	Env                      map[string]string
	InitializeTimeoutSeconds float64
	RequestTimeoutSeconds    float64
	ShutdownTimeoutSeconds   float64
	BaseURL                  string
	APIKey                   string
	LaunchArgsOverride       []string
}

// sdkOptions 组装 SDK 选项。
// Python: harness.py:205-228 _sdk_options（步骤原序）
func (h *DshHarness) sdkOptions(context *harnessprotocol.HarnessContext) GoSDKOptions {
	// Python: harness.py:206-207 env = config.env + context.env
	env := make(map[string]string, len(h.config.Env)+len(context.Env))
	for key, value := range h.config.Env {
		env[key] = value
	}
	for key, value := range context.Env {
		env[key] = value
	}
	// Python: harness.py:208-209（system_prompt 经显式环境变量注入）
	if context.SystemPrompt != "" && h.config.SystemPromptEnvVar != "" {
		env[h.config.SystemPromptEnvVar] = context.SystemPrompt
	}
	cwd := derefString(context.Cwd)
	if cwd == "" {
		cwd = h.config.Cwd
	}
	return GoSDKOptions{
		Provider:                 h.config.Provider,
		Model:                    h.config.Model,
		ReasoningEffort:          h.config.ReasoningEffort,
		MaxTokens:                h.config.MaxTokens,
		Cwd:                      cwd,
		RuntimeCwd:               h.config.RuntimeCwd,
		DshBin:                   h.config.DshBin,
		DshHome:                  h.config.DshHome,
		Profile:                  h.config.Profile,
		Patches:                  append([]string{}, h.config.Patches...),
		Env:                      env,
		InitializeTimeoutSeconds: h.config.InitializeTimeoutSeconds,
		RequestTimeoutSeconds:    h.config.RequestTimeoutSeconds,
		ShutdownTimeoutSeconds:   h.config.ShutdownTimeoutSeconds,
		BaseURL:                  h.config.BaseURL,
		APIKey:                   h.config.APIKey,
		LaunchArgsOverride:       h.config.LaunchArgsOverride,
	}
}

// handleNotification 单条通知的规约与发射。
// Python: harness.py:188-203 _handle_notification
func (h *DshHarness) handleNotification(
	ctx context.Context,
	turn *providers.PendingTurn,
	accumulator *DshTurnAccumulator,
	notification deepseekharness.Notification,
) error {
	mapped, err := accumulator.Consume(notification)
	if err != nil {
		return err
	}
	for _, event := range mapped {
		if err := h.emitMapped(ctx, turn, event); err != nil {
			return err
		}
	}
	return nil
}

// emitMapped 单条规约事件的信封组装。
// Python: harness.py:197-203 _emit_mapped
func (h *DshHarness) emitMapped(
	ctx context.Context,
	turn *providers.PendingTurn,
	mapped *MappedDshEvent,
) error {
	opts := providers.EmitOptions{Turn: turn}
	if mapped.ItemID != "" {
		opts.ItemID = mapped.ItemID
	}
	if mapped.ProviderSessionID != "" {
		opts.ProviderSessionID = mapped.ProviderSessionID
	}
	return h.base.Emit(ctx, mapped.Payload, opts)
}

// toDshInput 宿主输入 → DSH 输入形态。
// Python: harness.py:247-253 _to_dsh_input
func toDshInput(content harnessprotocol.HarnessInput) any {
	value := harnessprotocol.JsonValueToBuiltin(content.Content)
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		allBlocks := true
		for _, item := range typed {
			if _, ok := item.(map[string]any); !ok {
				allBlocks = false
				break
			}
		}
		if allBlocks {
			return typed
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

// defaultSDKFactory 真实 SDK 工厂（Python sdk.DeepSeekHarness(**options) 的
// Go 等价；字段映射一比一，零值省略由 SDK merge 逻辑承担）。
func defaultSDKFactory(options GoSDKOptions) (DshSDKHarness, error) {
	return &goSDKHarness{inner: deepseekharness.NewDeepSeekHarness(deepseekharness.DeepSeekHarnessConfig{
		Provider:                 options.Provider,
		Model:                    options.Model,
		ReasoningEffort:          options.ReasoningEffort,
		MaxTokens:                options.MaxTokens,
		CWD:                      options.Cwd,
		RuntimeCWD:               options.RuntimeCwd,
		DSHBin:                   options.DshBin,
		DSHHome:                  options.DshHome,
		Profile:                  options.Profile,
		Patches:                  options.Patches,
		Env:                      options.Env,
		InitializeTimeoutSeconds: options.InitializeTimeoutSeconds,
		RequestTimeoutSeconds:    options.RequestTimeoutSeconds,
		ShutdownTimeoutSeconds:   options.ShutdownTimeoutSeconds,
		BaseURL:                  options.BaseURL,
		APIKey:                   options.APIKey,
		LaunchArgsOverride:       options.LaunchArgsOverride,
	})}, nil
}

// newSessionID 生成 "dsh-<hex>" 会话标识（Python: harness.py:102）。
func newSessionID() string {
	return "dsh-" + strings.ReplaceAll(uuid.New().String(), "-", "")
}

// 编译期断言：包装器满足 SDK 缝接口。
var (
	_ DshSDKHarness = (*goSDKHarness)(nil)
	_ DshSDKSession = (*goSDKSession)(nil)
)

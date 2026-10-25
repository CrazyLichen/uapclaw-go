package cronbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentcron "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UserMessagePublisher wake 用户消息发布接口（破环：message_handler 隐式实现）。
// 对齐 Python: MessageHandler.publish_user_messages。
type UserMessagePublisher interface {
	// PublishUserMessagesNowait 非阻塞发布用户消息
	PublishUserMessagesNowait(msg *schema.Message)
}

// cronToolsBackend 适配 CronTools 到 agentcore CronToolBackend 接口。
// 对齐 Python: _CronToolsCronBackend (cron_runtime.py L18-203)。
type cronToolsBackend struct {
	// tools Agent 侧工具集
	tools *CronTools
	// userPublisher wake 用户消息发布器（对齐 Python message_handler）
	userPublisher UserMessagePublisher
}

// CronRuntimeBridge 为 DeepAgents 解析宿主 cron 后端。
// 对齐 Python: CronRuntimeBridge (cron_runtime.py L333-399)。
type CronRuntimeBridge struct {
	// backendOverride 显式覆盖
	backendOverride agentcron.CronToolBackend
	// resolvedBackend 已解析后端
	resolvedBackend agentcron.CronToolBackend
	// toolsOverride 显式覆盖 CronTools
	toolsOverride *CronTools
	// userPublisherOverride 显式覆盖 wake 发布器
	userPublisherOverride UserMessagePublisher
	// mu 保护 resolvedBackend 惰性解析
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalGatewayPushFunc 全局 E2A 推送函数（app_gateway 装配注入）
	globalGatewayPushFunc func(ctx context.Context, payload map[string]any) error
	// globalUserPublisher 全局 wake 发布器（app_gateway 装配注入）
	globalUserPublisher UserMessagePublisher
	// globalConfigLoader 全局 config 读取器（app_gateway 装配注入）
	globalConfigLoader func() map[string]any
	// globalAgentClient 全局 agent 客户端（app_gateway 装配注入）
	globalAgentClient gwcron.SchedulerAgentClient
	// globalMu 保护全局注入
	globalMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronRuntimeBridge 创建 bridge。
func NewCronRuntimeBridge() *CronRuntimeBridge {
	return &CronRuntimeBridge{}
}

// SetGlobalGatewayPushFunc 全局注入 E2A 推送函数（app_gateway 装配时调用）。
func SetGlobalGatewayPushFunc(fn func(ctx context.Context, payload map[string]any) error) {
	globalMu.Lock()
	globalGatewayPushFunc = fn
	globalMu.Unlock()
}

// SetGlobalUserPublisher 全局注入 wake 发布器。
func SetGlobalUserPublisher(pub UserMessagePublisher) {
	globalMu.Lock()
	globalUserPublisher = pub
	globalMu.Unlock()
}

// SetGlobalConfigLoader 全局注入 config 读取器。
func SetGlobalConfigLoader(fn func() map[string]any) {
	globalMu.Lock()
	globalConfigLoader = fn
	globalMu.Unlock()
}

// SetGlobalAgentClient 全局注入 agent 客户端（内嵌调度器依赖）。
func SetGlobalAgentClient(client gwcron.SchedulerAgentClient) {
	globalMu.Lock()
	globalAgentClient = client
	globalMu.Unlock()
}

// SetBackend 显式覆盖后端。
// 对齐 Python: set_backend。
func (b *CronRuntimeBridge) SetBackend(backend agentcron.CronToolBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backendOverride = backend
	b.resolvedBackend = backend
}

// SetToolsOverrides 显式覆盖 CronTools 与 wake 发布器。
func (b *CronRuntimeBridge) SetToolsOverrides(tools *CronTools, publisher UserMessagePublisher) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toolsOverride = tools
	b.userPublisherOverride = publisher
}

// GetBackend 获取后端（惰性解析；对齐 Python get_backend）。
// Go 无 MessageHandler 单例，改用全局注入的 userPublisher（app_gateway 装配）。
func (b *CronRuntimeBridge) GetBackend() agentcron.CronToolBackend {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.backendOverride != nil {
		return b.backendOverride
	}
	if b.resolvedBackend != nil {
		return b.resolvedBackend
	}

	globalMu.RLock()
	pushFn := globalGatewayPushFunc
	userPublisher := globalUserPublisher
	configLoader := globalConfigLoader
	agentClient := globalAgentClient
	globalMu.RUnlock()

	// publisher 解析：override 优先，其次全局注入（对齐 Python: MessageHandler
	// 同时用于 wake 用户消息发布与 scheduler _push_to_targets 消息推送）
	publisher := userPublisher
	if b.userPublisherOverride != nil {
		publisher = b.userPublisherOverride
	}
	// 调度器 publisher 接口断言还原：MessageHandler 隐式实现双接口
	// （PublishRobotMessages + PublishUserMessagesNowait）；仅实现
	// UserMessagePublisher 的注入方安全降级为 nil → pushToTargets nil 守卫兜底
	var schedulerPublisher gwcron.MessagePublisher
	if mp, ok := publisher.(gwcron.MessagePublisher); ok {
		schedulerPublisher = mp
	}

	if b.toolsOverride != nil {
		b.toolsOverride.SetSchedulerDeps(agentClient, schedulerPublisher, configLoader)
	} else {
		tools := NewCronTools(pushFn)
		tools.SetSchedulerDeps(agentClient, schedulerPublisher, configLoader)
		b.toolsOverride = tools
	}
	backend := &cronToolsBackend{tools: b.toolsOverride, userPublisher: publisher}
	b.resolvedBackend = backend
	logger.Info(logComponent).Msg("[CronRuntimeBridge] CronTools backend initialized successfully")
	return backend
}

// EnsureSchedulerStarted 确保调度器已启动。
// 对齐 Python: ensure_scheduler_started（Go 同步执行，无需事件循环分支）。
func (b *CronRuntimeBridge) EnsureSchedulerStarted() {
	backend := b.GetBackend()
	cronBackend, ok := backend.(*cronToolsBackend)
	if !ok {
		return
	}
	cronBackend.EnsureSchedulerStarted(context.Background())
}

// BuildTools 构建 cron 工具集。
// 对齐 Python: build_tools（create_cron_tools 调用 agentcore 工厂）。
func (b *CronRuntimeBridge) BuildTools(cronCtx *agentcron.CronToolContext, agentID string, language string) []tool.Tool {
	backend := b.GetBackend()
	if backend == nil {
		logger.Warn(logComponent).Msg("[CronRuntimeBridge] cron backend is not ready, skip builtin cron tools")
		return nil
	}
	logger.Info(logComponent).
		Str("tool_scope", cronCtx.ToolScope()).
		Msg("[CronRuntimeBridge] Building cron tools for context")
	tools := agentcron.CreateCronTools(
		backend,
		cronCtx,
		language,
		gwcron.AllTargetChannels(),
		"",
		true,
		agentID,
	)
	// 尾部汇总（对齐 Python L396-398 "Built %d cron tools: %s"）
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Card().Name)
	}
	logger.Info(logComponent).
		Int("count", len(tools)).
		Strs("tools", names).
		Msg("[CronRuntimeBridge] Built cron tools")
	return tools
}

// ──────────────────────────── 非导出函数（backend 11 方法） ────────────────────────────

// ListJobs 列出任务（后端形态转换）。
// 对齐 Python: list_jobs (cron_runtime.py L45-50)。
func (bk *cronToolsBackend) ListJobs(ctx context.Context, includeDisabled bool) ([]map[string]any, error) {
	jobs, err := bk.tools.ListJobs()
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		rows = append(rows, toBackendJob(job))
	}
	if includeDisabled {
		return rows, nil
	}
	filtered := rows[:0]
	for _, job := range rows {
		if boolVal(job["enabled"], true) {
			filtered = append(filtered, job)
		}
	}
	return filtered, nil
}

// GetJob 获取单个任务。
func (bk *cronToolsBackend) GetJob(ctx context.Context, jobID string) (map[string]any, error) {
	job, err := bk.tools.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	return toBackendJob(job), nil
}

// CreateJob 创建任务（legacy 参数映射 + 路由绑定）。
// 对齐 Python: create_job (cron_runtime.py L58-89)。
func (bk *cronToolsBackend) CreateJob(ctx context.Context, params map[string]any, cronCtx *agentcron.CronToolContext) (map[string]any, error) {
	requestID := ""
	if cronCtx != nil {
		if v, ok := cronCtx.Metadata["request_id"]; ok {
			requestID = strVal(v)
		}
	}
	// raw_keys：入参 key 排序列表（对齐 Python L67-76 第 4 字段——
	// 排查 legacy 参数映射问题的关键线索）
	rawKeys := make([]string, 0, len(params))
	for k := range params {
		rawKeys = append(rawKeys, k)
	}
	sort.Strings(rawKeys)
	logger.Info(logComponent).
		Str("channel_id", contextChannelID(cronCtx)).
		Str("session_id", contextSessionID(cronCtx)).
		Str("request_id", requestID).
		Strs("raw_keys", rawKeys).
		Msg("[CronRuntimeBridge] create_job in")

	payload, err := extractLegacyParams(copyMap(params), cronCtx, true)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Str("targets", strVal(payload["targets"])).Str("id", strVal(payload["id"])).
		Str("name", strVal(payload["name"])).
		Msg("[CronRuntimeBridge] create_job mapped")

	ctx = PushCronRoute(ctx, routeFromContext(cronCtx))
	job, err := bk.tools.CreateJob(ctx, payload)
	if err != nil {
		return nil, err
	}
	return toBackendJob(job), nil
}

// UpdateJob 更新任务。
// 对齐 Python: update_job (cron_runtime.py L91-104)。
func (bk *cronToolsBackend) UpdateJob(ctx context.Context, jobID string, patch map[string]any, cronCtx *agentcron.CronToolContext) (map[string]any, error) {
	payload, err := extractLegacyParams(copyMap(patch), cronCtx, false)
	if err != nil {
		return nil, err
	}
	ctx = PushCronRoute(ctx, routeFromContext(cronCtx))
	job, err := bk.tools.UpdateJob(ctx, jobID, payload)
	if err != nil {
		return nil, err
	}
	return toBackendJob(job), nil
}

// DeleteJob 删除任务。
func (bk *cronToolsBackend) DeleteJob(ctx context.Context, jobID string) (bool, error) {
	deleted, err := bk.tools.DeleteJob(ctx, jobID)
	if err != nil {
		return false, err
	}
	return deleted, nil
}

// ToggleJob 启停任务。
func (bk *cronToolsBackend) ToggleJob(ctx context.Context, jobID string, enabled bool) (map[string]any, error) {
	job, err := bk.tools.ToggleJob(ctx, jobID, enabled)
	if err != nil {
		return nil, err
	}
	return toBackendJob(job), nil
}

// PreviewJob 预览下次执行。
func (bk *cronToolsBackend) PreviewJob(ctx context.Context, jobID string, count int) ([]map[string]any, error) {
	rows, err := bk.tools.PreviewJob(jobID, count)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []map[string]any{}, nil
	}
	return rows, nil
}

// RunNow 立即执行（返回 run_id 字符串）。
// 对齐 Python: run_now (cron_runtime.py L117-125)。
func (bk *cronToolsBackend) RunNow(ctx context.Context, jobID string) (string, error) {
	ctx = PushCronRoute(ctx, &CronToolRoute{})
	result, err := bk.tools.RunNow(ctx, jobID)
	if err != nil {
		return "", err
	}
	if runID, ok := result["run_id"].(string); ok {
		return runID, nil
	}
	return "", nil
}

// Status 获取状态（轻量聚合）。
// 对齐 Python: status (cron_runtime.py L127-133)。
func (bk *cronToolsBackend) Status(ctx context.Context) (map[string]any, error) {
	jobs, err := bk.tools.ListJobs()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"running":   false,
		"job_count": len(jobs),
		"run_count": 0,
	}, nil
}

// GetRuns 获取执行记录（当前桥不支持，返回空）。
// 对齐 Python: get_runs (cron_runtime.py L135-137)。
func (bk *cronToolsBackend) GetRuns(ctx context.Context, jobID string, limit int) ([]map[string]any, error) {
	_ = jobID
	_ = limit
	return []map[string]any{}, nil
}

// Wake 唤醒，注入提示文本。
// 对齐 Python: wake (cron_runtime.py L139-169)。
func (bk *cronToolsBackend) Wake(ctx context.Context, text string, cronCtx *agentcron.CronToolContext, mode string) (map[string]any, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("text is required")
	}
	if cronCtx == nil || strings.TrimSpace(cronCtx.ChannelID) == "" {
		return nil, fmt.Errorf("wake requires an active session context")
	}
	if bk.userPublisher == nil {
		return nil, fmt.Errorf("cron wake is unavailable before message handler startup")
	}

	resolvedMode := mode
	if resolvedMode == "" && cronCtx != nil {
		resolvedMode = cronCtx.Mode
	}
	if resolvedMode == "" {
		resolvedMode = "agent.fast"
	}
	params := map[string]any{
		"query":   text,
		"content": text,
		"mode":    resolvedMode,
	}
	msg := schema.NewReqMessage(cronCtx.ChannelID, cronCtx.SessionID, schema.ReqMethodChatSend,
		jsonRawMessage(params),
		schema.WithMetadata(deepCopyMetadata(cronCtx.Metadata)),
	)
	msg.ID = fmt.Sprintf("cron-wake-%d", time.Now().UnixMilli())
	bk.userPublisher.PublishUserMessagesNowait(msg)
	return map[string]any{"queued": true}, nil
}

// EnsureSchedulerStarted 确保调度器启动（backend 侧入口）。
// 对齐 Python: ensure_scheduler_started (cron_runtime.py L171-173)。
func (bk *cronToolsBackend) EnsureSchedulerStarted(ctx context.Context) {
	bk.tools.EnsureScheduler(ctx)
}

// ──────────────────────────── 非导出函数（辅助） ────────────────────────────

// routeFromContext 从 CronToolContext 构造路由。
// 对齐 Python: _route_from_context (cron_runtime.py L25-43)。
func routeFromContext(cronCtx *agentcron.CronToolContext) *CronToolRoute {
	if cronCtx == nil {
		return &CronToolRoute{ChannelID: gwcron.TargetChannelWeb}
	}
	metadata := map[string]any{}
	if cronCtx.Metadata != nil {
		metadata = cronCtx.Metadata
	}
	requestID := strings.TrimSpace(strVal(metadata["request_id"]))
	channelID := strings.TrimSpace(cronCtx.ChannelID)
	if channelID == "" {
		channelID = gwcron.TargetChannelWeb
	}
	sessionID := ""
	if strings.TrimSpace(cronCtx.SessionID) != "" {
		sessionID = strings.TrimSpace(cronCtx.SessionID)
	}
	chatType := strings.TrimSpace(strVal(metadata["chat_type"]))
	return &CronToolRoute{
		RequestID: requestID,
		ChannelID: channelID,
		SessionID: sessionID,
		ChatType:  chatType,
	}
}

// toBackendJob 后端任务形态转换（默认字段注入）。
// 对齐 Python: _to_backend_job (cron_runtime.py L175-203)。
func toBackendJob(job map[string]any) map[string]any {
	row := copyMap(job)
	if _, ok := row["schedule"]; !ok {
		row["schedule"] = map[string]any{
			"kind": "cron",
			"expr": strings.TrimSpace(strVal(row["cron_expr"])),
			"tz":   timezoneOr(row["timezone"], "Asia/Shanghai"),
		}
	}
	if _, ok := row["payload"]; !ok {
		row["payload"] = map[string]any{
			"kind":    "agentTurn",
			"message": strings.TrimSpace(strVal(row["description"])),
		}
	}
	if _, ok := row["delivery"]; !ok {
		targets := strings.TrimSpace(strVal(row["targets"]))
		if targets == "" {
			targets = gwcron.TargetChannelWeb
		}
		row["delivery"] = map[string]any{
			"mode":    "announce",
			"channel": targets,
		}
	}
	if _, ok := row["session_target"]; !ok {
		row["session_target"] = "isolated"
	}
	if _, ok := row["compat_mode"]; !ok {
		row["compat_mode"] = "legacy"
	}
	return row
}

// extractLegacyParams legacy 嵌套参数映射。
// 对齐 Python: _extract_legacy_params (cron_runtime.py L206-330)。
// 无嵌套字段（schedule/payload/delivery 均缺）时原样返回。
func extractLegacyParams(payload map[string]any, cronCtx *agentcron.CronToolContext, requireSchedule bool) (map[string]any, error) {
	data := payload
	if data == nil {
		data = map[string]any{}
	}
	contextChannel := contextChannelID(cronCtx)
	contextTarget := ""
	if contextChannel != "" {
		if strings.HasPrefix(contextChannel, "feishu_enterprise:") {
			contextTarget = gwcron.NormalizeTargetChannelID(contextChannel, gwcron.TargetChannelWeb)
		} else if gwcron.IsValidTargetChannelID(contextChannel) {
			contextTarget = contextChannel
		}
	}
	_, hasSchedule := data["schedule"]
	_, hasPayload := data["payload"]
	_, hasDelivery := data["delivery"]
	if !hasSchedule && !hasPayload && !hasDelivery {
		return data, nil
	}

	schedule, _ := data["schedule"].(map[string]any)
	if schedule == nil {
		schedule = map[string]any{}
	}
	kind := strings.ToLower(strings.TrimSpace(strVal(schedule["kind"])))
	if kind == "" {
		kind = "cron"
	}
	if kind != "cron" {
		return nil, fmt.Errorf("Only cron schedule is supported by the current gateway bridge")
	}

	cronExpr := strings.TrimSpace(firstNonEmpty(
		strVal(schedule["expr"]), strVal(schedule["cron"]), strVal(data["cron_expr"])))
	timezone := timezoneOr(schedule["tz"], timezoneOr(schedule["timezone"], timezoneOr(data["timezone"], "Asia/Shanghai")))
	if strings.TrimSpace(timezone) == "" {
		timezone = "Asia/Shanghai"
	}

	payloadBlock, _ := data["payload"].(map[string]any)
	if payloadBlock == nil {
		payloadBlock = map[string]any{}
	}
	payloadKind := strings.TrimSpace(strVal(payloadBlock["kind"]))
	if payloadKind == "" {
		payloadKind = "agentTurn"
	}
	if payloadKind != "agentTurn" {
		return nil, fmt.Errorf("Only agentTurn cron jobs are supported by the current gateway bridge")
	}
	description := firstNonEmpty(strVal(payloadBlock["message"]), strVal(data["description"]))

	delivery, _ := data["delivery"].(map[string]any)
	if delivery == nil {
		delivery = map[string]any{}
	}
	logger.Info(logComponent).
		Str("delivery_channel", strVal(delivery["channel"])).Str("data_targets", strVal(data["targets"])).
		Str("context_channel_id", contextChannel).
		Msg("[CronRuntimeBridge] _extract_legacy_params")
	targets := strings.TrimSpace(firstNonEmpty(
		strVal(delivery["channel"]), strVal(data["targets"]), contextChannel))
	if targets == "" {
		targets = gwcron.TargetChannelWeb
	}
	// Per-request routing: 隐式 delivery.channel=web 时用当前请求上下文频道替换
	hasContextTarget := contextTarget != ""
	isWebTarget := targets == gwcron.TargetChannelWeb
	_, hasExplicitTargets := data["targets"]
	_, hasDeliveryChannel := delivery["channel"]
	if hasContextTarget && isWebTarget && !hasExplicitTargets && hasDeliveryChannel {
		logger.Info(logComponent).
			Str("from", targets).Str("to", contextTarget).
			Msg("[CronRuntimeBridge] map implicit web target to request context")
		targets = contextTarget
	}
	logger.Info(logComponent).
		Str("targets", targets).
		Msg("[CronRuntimeBridge] _extract_legacy_params resolved")

	out := map[string]any{}
	if cronExpr != "" || requireSchedule {
		out["cron_expr"] = cronExpr
	}
	if timezone != "" || requireSchedule {
		out["timezone"] = timezone
	}
	if description != "" {
		out["description"] = description
	}
	if targets != "" {
		out["targets"] = targets
	}
	if _, ok := data["name"]; ok {
		out["name"] = strings.TrimSpace(strVal(data["name"]))
	}
	if _, ok := data["id"]; ok {
		out["id"] = strings.TrimSpace(strVal(data["id"]))
	}
	if _, ok := data["enabled"]; ok {
		out["enabled"] = boolVal(data["enabled"], false)
	}
	if _, ok := data["wake_offset_seconds"]; ok {
		out["wake_offset_seconds"] = data["wake_offset_seconds"]
	}
	if _, ok := data["deleteAfterRun"]; ok {
		out["delete_after_run"] = boolVal(data["deleteAfterRun"], false)
	}

	// context session_id 注入
	if cronCtx != nil && strings.TrimSpace(cronCtx.SessionID) != "" {
		out["session_id"] = strings.TrimSpace(cronCtx.SessionID)
		logger.Info(logComponent).
			Str("session_id", out["session_id"].(string)).
			Msg("[CronRuntimeBridge] _extract_legacy_params added session_id from context")
	}

	// mode 解析：context.mode → data.mode → agent.fast
	modeResolved := ""
	if cronCtx != nil {
		modeResolved = cronCtx.Mode
	}
	if modeResolved == "" {
		modeResolved = strVal(data["mode"])
	}
	if strings.TrimSpace(modeResolved) == "" {
		modeResolved = "agent.fast"
	}
	out["mode"] = strings.ToLower(strings.TrimSpace(modeResolved))
	return out, nil
}

// contextChannelID 安全取上下文频道。
func contextChannelID(cronCtx *agentcron.CronToolContext) string {
	if cronCtx == nil {
		return ""
	}
	return strings.TrimSpace(cronCtx.ChannelID)
}

// contextSessionID 安全取上下文会话。
func contextSessionID(cronCtx *agentcron.CronToolContext) string {
	if cronCtx == nil {
		return ""
	}
	return strings.TrimSpace(cronCtx.SessionID)
}

// firstNonEmpty 取第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// jsonRawMessage map 转 json.RawMessage。
func jsonRawMessage(m map[string]any) json.RawMessage {
	raw, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// deepCopyMetadata 深拷贝 metadata（对齐 Python deepcopy）。
func deepCopyMetadata(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

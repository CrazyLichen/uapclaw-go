// Package cronbridge 提供 Agent 侧 cron 工具桥接：本地 store 双写 +
// E2A 转发到 Gateway + 委托降级单调度器（方案 B）。
package cronbridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronToolRoute 当前请求同步到 Gateway 时使用的路由。
// 对齐 Python: CronToolRoute (cron_tools.py L34-42)。
// Go 用 ctx values 传递（Python ContextVar）；PushCronRoute 绑定。
type CronToolRoute struct {
	// RequestID 请求标识
	RequestID string
	// ChannelID 频道标识（默认 web）
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// ChatType 群聊标记（"group" 或空）
	ChatType string
}

// CronTools Agent 侧 cron 工具（本地 cron_jobs.json 为 source of truth）。
// 对齐 Python: CronTools (cron_tools.py L44-467)。
type CronTools struct {
	// gatewayPush E2A 推送函数（对齐 Python GatewayPushTransport.send_push）
	gatewayPush func(ctx context.Context, payload map[string]any) error
	// localStore 本地存储（与 Gateway 同路径同文件）
	localStore *gwcron.CronJobStore
	// scheduler 内嵌调度器（standalone fallback）
	scheduler *gwcron.CronSchedulerService
	// schedulerStarted 已尝试启动标志
	schedulerStarted bool
	// agentClient 调度器用 agent 客户端（可空）
	agentClient gwcron.SchedulerAgentClient
	// publisher 调度器用消息发布器（可空）
	publisher gwcron.MessagePublisher
	// configLoader 调度器用 config 读取器（可空）
	configLoader func() map[string]any
	// mu 保护 scheduler/schedulerStarted
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// routeCtxKey 路由上下文 key（ctx values 传递，对齐 Python ContextVar）
type routeCtxKeyType struct{}

var routeCtxKey = routeCtxKeyType{}

// runtimeCronCtxKey 运行时 cron 上下文 key（对齐 Python _bind_runtime_cron_context 的 ContextVar 组）
type runtimeCronCtxKeyType struct{}

var runtimeCronCtxKey = runtimeCronCtxKeyType{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronTools 创建 Agent 侧工具集。
// gatewayPush 为空时 E2A 转发降级为跳过（standalone 形态，对齐 Python WebSocketGatewayPushTransport 兜底）。
func NewCronTools(gatewayPush func(ctx context.Context, payload map[string]any) error) *CronTools {
	return &CronTools{
		gatewayPush: gatewayPush,
		localStore:  gwcron.NewCronJobStore(path.CronJobsPath()),
	}
}

// RuntimeCronContext 运行时 cron 上下文（ctx values 载体，对齐 Python 4 个 ContextVar）。
type RuntimeCronContext struct {
	// ChannelID 频道标识（归一化默认 web）
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// Metadata 扩展元数据（含注入的 request_id）
	Metadata map[string]any
	// Mode 触发模式
	Mode string
}

// BindRuntimeCronContext 绑定运行时 cron 上下文（对齐 Python _bind_runtime_cron_context）。
// 4 个 ContextVar → ctx values：channelID 归一化默认 web、sessionID、metadata 注入 request_id、mode。
// Go ctx 不可变传递，reset 语义天然满足（无需显式 reset）。
func BindRuntimeCronContext(ctx context.Context, channelID, sessionID string, metadata map[string]any, requestID, mode string) context.Context {
	normalizedChannel := strings.TrimSpace(channelID)
	if normalizedChannel == "" {
		normalizedChannel = gwcron.TargetChannelWeb
	}
	normalizedMode := strings.TrimSpace(mode)
	normalizedMetadata := map[string]any{}
	// range nil map 是空迭代（Go 语义安全），无需显式 nil 检查
	for k, v := range metadata {
		normalizedMetadata[k] = v
	}
	if strings.TrimSpace(requestID) != "" {
		normalizedMetadata["request_id"] = strings.TrimSpace(requestID)
	}
	return context.WithValue(ctx, runtimeCronCtxKey, &RuntimeCronContext{
		ChannelID: normalizedChannel,
		SessionID: sessionID,
		Metadata:  normalizedMetadata,
		Mode:      normalizedMode,
	})
}

// FromRuntimeCronContext 从 ctx 读取运行时 cron 上下文（未绑定为 nil）。
// 供 deep_adapter 测试与跨包读取绑定值使用。
func FromRuntimeCronContext(ctx context.Context) *RuntimeCronContext {
	if rcc, ok := ctx.Value(runtimeCronCtxKey).(*RuntimeCronContext); ok {
		return rcc
	}
	return nil
}

// SetSchedulerDeps 注入内嵌调度器依赖（agentClient/publisher/configLoader 可空）。
func (t *CronTools) SetSchedulerDeps(agentClient gwcron.SchedulerAgentClient, publisher gwcron.MessagePublisher, configLoader func() map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agentClient = agentClient
	t.publisher = publisher
	t.configLoader = configLoader
}

// PushCronRoute 绑定路由上下文（进入一轮 Agent 执行前调用）。
// 对齐 Python: push_cron_route。ctx values 不可变，无需 reset（Python Token 配对
// 在 Go 中退化为文档约定：下轮请求绑定新 ctx 即自然隔离）。
func PushCronRoute(ctx context.Context, route *CronToolRoute) context.Context {
	return context.WithValue(ctx, routeCtxKey, route)
}

// EnsureScheduler 确保调度器可用（方案 B 委托降级单调度器）。
// 对齐 Python ensure_scheduler（cron_tools.py L70-119）+ 方案 B 修正。
// 决策链：
//  1. 内嵌已启动且运行中 → 返回内嵌
//  2. 已尝试启动过 → 返回（可能 nil）
//  3. 进程注册表有运行中的 Gateway 调度器 → 委托 Reload → 标记已尝试 → 返回 nil
//  4. 启动内嵌 fallback（standalone 形态，对齐 Python）
func (t *CronTools) EnsureScheduler(ctx context.Context) *gwcron.CronSchedulerService {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 1. 内嵌已启动
	if t.scheduler != nil && t.scheduler.IsRunning() {
		return t.scheduler
	}
	// 2. 已尝试启动
	if t.schedulerStarted {
		return t.scheduler
	}

	// 3. 方案 B：委托 Gateway 调度器
	if svc := gwcron.GetProcessScheduler(); svc != nil && svc.IsRunning() {
		if err := svc.Reload(); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[CronTools] delegated scheduler reload failed")
		} else {
			logger.Debug(logComponent).Msg("[CronTools] delegated scheduler reloaded")
		}
		t.schedulerStarted = true
		return nil
	}

	// 4. standalone 回退（对齐 Python：无进程注册表条目时启动内嵌调度器）
	if t.agentClient == nil {
		logger.Warn(logComponent).
			Msg("[CronTools] Cannot start scheduler: agent client not available")
		t.schedulerStarted = true
		return nil
	}
	svc := gwcron.NewCronSchedulerService(t.localStore, t.agentClient, t.publisher, nil)
	svc.SetConfigLoader(t.configLoader)
	// 对齐 Python except 路径：Start 失败时 _scheduler 已赋值但仍返回 None
	t.scheduler = svc
	t.schedulerStarted = true
	if err := svc.Start(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[CronTools] Failed to start scheduler")
		return nil
	}
	logger.Info(logComponent).Msg("[CronTools] Scheduler started successfully")
	return svc
}

// SendSplit E2A cron 转发。
// 对齐 Python _send_split (cron_tools.py L145-163)。
// payload: {request_id, channel_id, session_id, response_kind: "cron", body: {action, status: "ok", data, message}}。
// 转发结果丢弃（异步回显），本地写即时生效。
func (t *CronTools) SendSplit(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
	r := route(ctx)
	payload := map[string]any{
		"request_id":    r.RequestID,
		"channel_id":    r.ChannelID,
		"session_id":    r.SessionID,
		"response_kind": "cron",
		"body": map[string]any{
			"action":  action,
			"status":  "ok",
			"data":    copyMap(params),
			"message": "",
		},
	}
	if t.gatewayPush != nil {
		if err := t.gatewayPush(ctx, payload); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"action":  action,
		"status":  "forwarded",
		"data":    nil,
		"message": "cron request forwarded to gateway",
	}, nil
}

// ListJobs 列出任务（本地 store）。
func (t *CronTools) ListJobs() ([]map[string]any, error) {
	jobs, err := t.localStore.ListJobs()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.ToDict())
	}
	return out, nil
}

// GetJob 获取单个任务。
func (t *CronTools) GetJob(jobID string) (map[string]any, error) {
	job, err := t.localStore.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	return job.ToDict(), nil
}

// CreateJob 本地写 + E2A 转发 + reload。
// 对齐 Python create_job (cron_tools.py L232-273)。
func (t *CronTools) CreateJob(ctx context.Context, params map[string]any) (map[string]any, error) {
	normalized := copyMap(params)
	delete(normalized, "session_id")
	targets, err := t.normalizeTargetsParam(ctx, normalized["targets"])
	if err != nil {
		return nil, err
	}
	normalized["targets"] = targets
	cronExpr, err := gwcron.NormalizeCronExpr(strings.TrimSpace(strVal(normalized["cron_expr"])))
	if err != nil {
		return nil, err
	}
	normalized["cron_expr"] = cronExpr

	r := route(ctx)
	sessionID := strings.TrimSpace(r.SessionID)
	chatType := r.ChatType

	logger.Info(logComponent).
		Str("route_channel", r.ChannelID).Str("route_session", r.SessionID).
		Str("route_request", r.RequestID).
		Str("input_targets", strVal(params["targets"])).
		Str("normalized_targets", strVal(normalized["targets"])).
		Msg("[CronTools] create_job: route and normalized targets")

	job, err := t.localStore.CreateJob(gwcron.CreateJobParams{
		JobID:             strings.TrimSpace(strVal(normalized["id"])),
		Name:              strings.TrimSpace(strVal(normalized["name"])),
		CronExpr:          strings.TrimSpace(strVal(normalized["cron_expr"])),
		Timezone:          timezoneOr(normalized["timezone"], "Asia/Shanghai"),
		Description:       strVal(normalized["description"]),
		Targets:           strVal(normalized["targets"]),
		Enabled:           boolVal(normalized["enabled"], true),
		WakeOffsetSeconds: intPtrVal(normalized["wake_offset_seconds"]),
		SessionID:         sessionID,
		ChatType:          chatType,
		Mode:              strings.TrimSpace(strVal(normalized["mode"])),
		DeleteAfterRun:    boolPtrVal(normalized["delete_after_run"]),
	})
	if err != nil {
		return nil, err
	}

	// E2A 转发（失败仅 Warn，本地写已生效）
	if _, serr := t.SendSplit(ctx, "create", job.ToDict()); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync create to gateway failed")
	}
	t.reloadScheduler(ctx)
	return job.ToDict(), nil
}

// UpdateJob 本地更新 + E2A 转发 + reload。
// 对齐 Python update_job (cron_tools.py L275-300)。
func (t *CronTools) UpdateJob(ctx context.Context, jobID string, patch map[string]any) (map[string]any, error) {
	normalized := copyMap(patch)
	delete(normalized, "session_id")
	if _, ok := normalized["cron_expr"]; ok {
		cronExpr, err := gwcron.NormalizeCronExpr(strings.TrimSpace(strVal(normalized["cron_expr"])))
		if err != nil {
			return nil, err
		}
		normalized["cron_expr"] = cronExpr
	}
	if _, ok := normalized["targets"]; ok {
		targets, err := t.normalizeTargetsParam(ctx, normalized["targets"])
		if err != nil {
			return nil, err
		}
		normalized["targets"] = targets
		targetsStr := strings.TrimSpace(strVal(normalized["targets"]))
		if strings.HasPrefix(targetsStr, "feishu_enterprise:") {
			r := route(ctx)
			if r.SessionID != "" {
				normalized["session_id"] = r.SessionID
			}
		} else {
			normalized["session_id"] = nil
		}
	}
	r := route(ctx)
	if r.ChatType != "" {
		normalized["chat_type"] = r.ChatType
	} else {
		normalized["chat_type"] = nil
	}
	job, err := t.localStore.UpdateJob(jobID, normalized)
	if err != nil {
		return nil, err
	}
	if _, serr := t.SendSplit(ctx, "update", map[string]any{"job_id": jobID, "patch": normalized}); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync update to gateway failed")
	}
	t.reloadScheduler(ctx)
	return job.ToDict(), nil
}

// DeleteJob 本地删除 + E2A 转发 + reload。
func (t *CronTools) DeleteJob(ctx context.Context, jobID string) (bool, error) {
	deleted, err := t.localStore.DeleteJob(jobID)
	if err != nil {
		return false, err
	}
	if _, serr := t.SendSplit(ctx, "delete", map[string]any{"job_id": jobID}); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync delete to gateway failed")
	}
	t.reloadScheduler(ctx)
	return deleted, nil
}

// ToggleJob 本地启停 + E2A 转发 + reload。
func (t *CronTools) ToggleJob(ctx context.Context, jobID string, enabled bool) (map[string]any, error) {
	job, err := t.localStore.UpdateJob(jobID, map[string]any{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	if _, serr := t.SendSplit(ctx, "toggle", map[string]any{"job_id": jobID, "enabled": enabled}); serr != nil {
		logger.Warn(logComponent).Err(serr).
			Msg("[CronTools] sync toggle to gateway failed")
	}
	t.reloadScheduler(ctx)
	return job.ToDict(), nil
}

// PreviewJob 预览下次执行（本地计算）。
func (t *CronTools) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	job, err := t.localStore.GetJob(jobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("job not found")
	}
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return nil, err
	}
	normalized, err := gwcron.NormalizeCronExpr(job.CronExpr)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	pushDT := time.Now().In(loc)
	for i := 0; i < count; i++ {
		next, err := gwcron.NextFireTime(normalized, pushDT)
		if err != nil {
			if gwcron.IsNoNextDateError(err) {
				break
			}
			return nil, err
		}
		pushDT = next
		if len(out) > 0 && pushDT.Format(time.RFC3339) == strVal(out[len(out)-1]["push_at"]) {
			break
		}
		wakeDT := next.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
		out = append(out, map[string]any{
			"wake_at": wakeDT.Format(time.RFC3339),
			"push_at": pushDT.Format(time.RFC3339),
		})
	}
	return out, nil
}

// RunNow 转发立即执行到 Gateway。
// 对齐 Python run_now（纯转发，本地不执行）。
func (t *CronTools) RunNow(ctx context.Context, jobID string) (map[string]any, error) {
	return t.SendSplit(ctx, "run_now", map[string]any{"job_id": jobID})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// route 从 ctx 读取路由（缺省零值路由，对齐 Python _route() 默认值 web）。
func route(ctx context.Context) *CronToolRoute {
	if r, ok := ctx.Value(routeCtxKey).(*CronToolRoute); ok && r != nil {
		return r
	}
	return &CronToolRoute{ChannelID: gwcron.TargetChannelWeb}
}

// reloadScheduler 重载调度器（写操作后调用）。
// 对齐 Python _reload_scheduler (cron_tools.py L121-129)。
func (t *CronTools) reloadScheduler(ctx context.Context) {
	svc := t.EnsureScheduler(ctx)
	if svc != nil {
		if err := svc.Reload(); err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[CronTools] Failed to reload scheduler")
		}
	}
}

// normalizeTargetsParam targets 参数归一化（显式值 → fallback 推导）。
// 对齐 Python _normalize_targets_param (cron_tools.py L203-222)。
func (t *CronTools) normalizeTargetsParam(ctx context.Context, raw any) (string, error) {
	target := strings.TrimSpace(strVal(raw))
	if gwcron.IsValidTargetChannelID(target) {
		normalized := gwcron.NormalizeTargetChannelID(target, gwcron.TargetChannelWeb)
		logger.Info(logComponent).
			Str("raw", target).Str("normalized", normalized).
			Str("route_channel", route(ctx).ChannelID).
			Msg("[CronTools] normalize targets from explicit value")
		return normalized, nil
	}
	fallback := t.defaultTargetFromChannel(ctx)
	logger.Info(logComponent).
		Str("raw", target).Str("fallback", fallback).
		Str("route_channel", route(ctx).ChannelID).
		Str("request_id", route(ctx).RequestID).
		Msg("[CronTools] normalize targets from fallback")
	return fallback, nil
}

// defaultTargetFromChannel 从路由 channel 推导默认 target。
// 对齐 Python _default_target_from_channel (cron_tools.py L171-191)。
func (t *CronTools) defaultTargetFromChannel(ctx context.Context) string {
	channelRaw := t.resolveChannelID(ctx)
	channel := strings.ToLower(channelRaw)
	if strings.HasPrefix(channel, "feishu_enterprise:") {
		return gwcron.NormalizeTargetChannelID(channelRaw, gwcron.TargetChannelWeb)
	}
	for _, prefix := range []string{"feishu", "wecom", "xiaoyi", "whatsapp", "wechat", "dingtalk", "tui"} {
		if strings.HasPrefix(channel, prefix) {
			return prefix
		}
	}
	return gwcron.TargetChannelWeb
}

// resolveChannelID 解析路由 channel（route.channel_id → request_id 前缀）。
// 对齐 Python _resolve_channel_id (cron_tools.py L193-201)。
func (t *CronTools) resolveChannelID(ctx context.Context) string {
	r := route(ctx)
	channelRaw := strings.TrimSpace(r.ChannelID)
	if channelRaw != "" {
		return channelRaw
	}
	requestID := strings.TrimSpace(r.RequestID)
	if !strings.Contains(requestID, ":") {
		return ""
	}
	// rsplit(":", 1)[0]：截到最后一个冒号为止的完整前缀（非单段）
	if idx := strings.LastIndex(requestID, ":"); idx >= 0 {
		return strings.TrimSpace(requestID[:idx])
	}
	return ""
}

// copyMap 浅拷贝
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// strVal 安全取字符串
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// boolVal 安全取布尔。
// 对齐 Python bool(x) 语义：缺失（nil）→ def；显式 nil 值在 Go map 读取中
// 与缺失不可区分，统一走 def（map 值为 nil 的 JSON null 场景由调用方先 strVal 判空）。
func boolVal(v any, def bool) bool {
	if v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// intPtrVal 安全取 int 指针（nil → nil）
func intPtrVal(v any) *int {
	if v == nil {
		return nil
	}
	switch n := v.(type) {
	case int:
		return &n
	case int64:
		i := int(n)
		return &i
	case float64:
		i := int(n)
		return &i
	}
	return nil
}

// boolPtrVal 安全取 bool 指针
func boolPtrVal(v any) *bool {
	if v == nil {
		return nil
	}
	if b, ok := v.(bool); ok {
		return &b
	}
	return nil
}

// timezoneOr timezone 参数兜底
func timezoneOr(v any, def string) string {
	s := strings.TrimSpace(strVal(v))
	if s == "" {
		return def
	}
	return s
}

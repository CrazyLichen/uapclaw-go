package rsihandlers

// AgentServer RSI 分发层：18 个 rsi.* web method 的统一接线面（B2）。
// 对应 Python：jiuwenswarm/server/rsi/rsi_handlers.py（322 行）+ __init__.py。
//
// - RsiAgentServerHandlers 聚合服务域（RsiServiceContext）+ 推送发送器，
//   Handle 按 req_method 分发；
// - 保持 AgentServer 分发改动最小：dispatch 链新增一个前缀分支。
// - 推送：AgentServer 注入 SendPush 包装（P1 状态钩子 / P2 进度 / P3 树增量）。

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/rsi"
)

// ──────────────────────────── 常量 ────────────────────────────

// 推送事件类型（web 契约 v0.3 §4 P1/P2/P3）。
const (
	// PushStatusChanged P1 状态变更。
	PushStatusChanged = "rsi.training.status.changed"
	// PushProgress P2 进度。
	PushProgress = "rsi.training.progress"
	// PushTreeDelta P3 树增量。
	PushTreeDelta = "rsi.training.tree.delta"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// methodDispatch 18 个 web method → 服务域方法名
// （I1–I13 + control/artifact/tree + Harness lifecycle，逐字对齐 Python）。
var methodDispatch = map[string]string{
	"rsi.dataset.validate":      "dataset_validate",
	"rsi.task.create":           "task_create",
	"rsi.task.list":             "task_list",
	"rsi.task.get":              "task_get",
	"rsi.task.delete":           "task_delete",
	"rsi.training.start":        "training_start",
	"rsi.training.pause":        "training_pause",
	"rsi.training.resume":       "training_resume",
	"rsi.training.terminate":    "training_terminate",
	"rsi.report.get":            "report_get",
	"rsi.usage.get":             "usage_get",
	"rsi.artifact.download":     "artifact_download",
	"rsi.artifact.files.list":   "artifact_files_list",
	"rsi.artifact.files.get":    "artifact_files_get",
	"rsi.tree.get":              "tree_get",
	"rsi.harness.install":       "harness_install",
	"rsi.harness.versions.list": "harness_versions_list",
	"rsi.harness.rollback":      "harness_rollback",
}

// ──────────────────────────── 结构体 ────────────────────────────

// ServiceContext RSI 服务域最小依赖面（internal/swarm/server/runtime/rsi.ServiceContext）。
type ServiceContext = rsi.ServiceContext

// AgentServerHandlers AgentServer 侧 RSI 分发 + 推送发送（依赖注入，便于测试）。
type AgentServerHandlers struct {
	context          *ServiceContext
	sendPush         func(map[string]any) error
	defaultChannelID string
	recoverySummary  map[string]any
}

// NewAgentServerHandlers 构造（sendPush 必填；harnessRefsProvider 可空）。
// 构造期：refs 提供方绑定 + recover_workspace 对账 + P1/P2/P3 推送绑定。
func NewAgentServerHandlers(context *ServiceContext,
	sendPush func(map[string]any) error,
	harnessRefsProvider func(params map[string]any) (string, error),
	defaultChannelID string) *AgentServerHandlers {
	if sendPush == nil {
		sendPush = func(map[string]any) error { return nil }
	}
	h := &AgentServerHandlers{context: context, sendPush: sendPush, defaultChannelID: defaultChannelID}
	// 服务域接线：harness_refs 快照提供方（缺省 auto_harness 激活 refs）。
	if harnessRefsProvider != nil {
		context.BindTaskService(harnessRefsProvider)
	}
	// Provider registration is complete before handlers are constructed.
	// Reconcile durable active tasks before accepting the first RSI call;
	// recovery never re-enqueues work or invokes the engine.
	h.recoverySummary = context.RecoverWorkspace()
	// P1 状态钩子：TaskStore.update_status → send_push（服务侧权威，不依赖引擎事件）。
	h.bindStatusPush()
	// P2/P3 推送：事件链路回调（progress/tree.delta）。
	h.bindEventPush()
	return h
}

// RecoverySummary 构造期对账摘要（_get_rsi_handlers 复用）。
func (h *AgentServerHandlers) RecoverySummary() map[string]any { return h.recoverySummary }

// ──────────────────────────── 导出函数 ────────────────────────────

// Handle 按 req_method 分发；服务域异常 → 统一错误语义载荷。
func (h *AgentServerHandlers) Handle(request map[string]any) map[string]any {
	method := rsiMethodOf(request)
	handlerName := methodDispatch[method]
	if handlerName == "" {
		return map[string]any{"ok": false, "error": fmt.Sprintf("unsupported rsi method: %s", method), "code": "BAD_REQUEST"}
	}
	params := paramsOf(request)
	// 传输会话与公开 method 载荷分离：私有标记供后续 Provider 事件回源 WS。
	if sessionID := strings.TrimSpace(valueOf(request["session_id"])); sessionID != "" {
		if _, ok := params["_rsi_session_id"]; !ok {
			params["_rsi_session_id"] = sessionID
		}
	}
	payload, err := h.invoke(handlerName, params)
	if err != nil {
		return errorPayload(err, method)
	}
	return map[string]any{"ok": true, "payload": payload}
}

// HandleAsync 异步分发（Harness 安装路径须 await 热加载链；Go 侧同步链路
// 保持同一错误归一，入口签名对齐）。
func (h *AgentServerHandlers) HandleAsync(ctx context.Context, request map[string]any) map[string]any {
	return h.Handle(request)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invoke method → 服务域调用（对齐 Python _do_* 十八方法）。
func (h *AgentServerHandlers) invoke(handlerName string, params map[string]any) (any, error) {
	c := h.context
	switch handlerName {
	case "dataset_validate":
		return c.DatasetService.Validate(params, c.AdapterFor(valueOf(params["scenario"]), valueOf(params["artifact_type"])))
	case "task_create":
		result, err := c.TaskService.Create(params)
		if err != nil {
			return nil, err
		}
		c.EnsureRoot(valueOf(result["task_id"]))
		return result, nil
	case "task_list":
		tasks, err := c.TaskService.List(params)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tasks": tasks}, nil
	case "task_get":
		return c.TaskService.Get(params, c.Projector, c.UsageRecorder, c.ArtifactService,
			c.AdapterForTask(valueOf(params["task_id"])))
	case "task_delete":
		return c.TaskService.Delete(params, c.Worker)
	case "training_start":
		return c.TaskService.Start(params, c.Worker)
	case "training_pause":
		return c.TaskService.Pause(params, c.Worker)
	case "training_resume":
		return c.TaskService.Resume(params, c.Worker)
	case "training_terminate":
		return c.TaskService.Terminate(params, c.Worker)
	case "report_get":
		return c.ReportService.Get(params, c.AdapterForTask(valueOf(params["task_id"])))
	case "usage_get":
		return c.UsageService.Get(params, c.AdapterForTask(valueOf(params["task_id"])))
	case "artifact_download":
		return c.ArtifactDownloadService.Locate(params, c.AdapterForTask(valueOf(params["task_id"])))
	case "artifact_files_list":
		return c.ArtifactFilesService.ListFiles(params)
	case "artifact_files_get":
		return c.ArtifactFilesService.ReadFile(params)
	case "tree_get":
		return c.TreeService.Get(params, c.AdapterForTask(valueOf(params["task_id"])))
	case "harness_install":
		taskID := strings.TrimSpace(valueOf(params["task_id"]))
		if taskID == "" {
			return nil, rsi.NewBadRequest("task_id 必填", nil)
		}
		return c.HarnessInstaller.Install(taskID)
	case "harness_versions_list":
		return c.HarnessInstaller.ListVersions(), nil
	case "harness_rollback":
		installationID := strings.TrimSpace(valueOf(params["installation_id"]))
		if installationID == "" {
			return nil, rsi.NewBadRequest("installation_id 必填", nil)
		}
		return c.HarnessInstaller.Rollback(installationID)
	default:
		return nil, rsi.NewBadRequest(fmt.Sprintf("handler not implemented: %s", handlerName), nil)
	}
}

// bindStatusPush P1 状态钩子（FAILED 附 failure_reason，best-effort）。
func (h *AgentServerHandlers) bindStatusPush() {
	h.context.Store.SetStatusChangedCallback(func(taskID, oldStatus, newStatus string) {
		payload := map[string]any{
			"task_id":    taskID,
			"old_status": oldStatus,
			"new_status": newStatus,
			"status":     newStatus,
		}
		if newStatus == "FAILED" {
			func() {
				defer func() { _ = recover() }() // status push is best effort
				task, err := h.context.Store.Get(taskID)
				if err != nil {
					return
				}
				if reason := task.FailureReasonText(); reason != "" {
					payload["failure_reason"] = reason
				}
			}()
		}
		h.push(PushStatusChanged, payload)
	})
}

// bindEventPush P2/P3 事件链路回调。
func (h *AgentServerHandlers) bindEventPush() {
	h.context.RegisterWorkerPush(map[string]func(string, string, map[string]any) error{
		PushProgress: func(eventType, taskID string, payload map[string]any) error {
			merged := map[string]any{"task_id": taskID}
			for k, v := range payload {
				merged[k] = v
			}
			return h.sendPush(h.buildPushMessage(eventType, merged))
		},
		PushTreeDelta: func(eventType, taskID string, payload map[string]any) error {
			merged := map[string]any{"task_id": taskID}
			for k, v := range payload {
				merged[k] = v
			}
			return h.sendPush(h.buildPushMessage(eventType, merged))
		},
	})
}

// buildPushMessage 统一推送消息组装（channel_id/event_type/session_id 回源）。
func (h *AgentServerHandlers) buildPushMessage(eventType string, payload map[string]any) map[string]any {
	inner := map[string]any{"event_type": eventType}
	for k, v := range payload {
		inner[k] = v
	}
	message := map[string]any{
		"channel_id": h.defaultChannelID,
		"payload":    inner,
	}
	taskID := strings.TrimSpace(valueOf(payload["task_id"]))
	if taskID != "" {
		func() {
			defer func() { _ = recover() }() // a late push must not break worker
			task, err := h.context.Store.Get(taskID)
			if err != nil {
				return
			}
			if sessionID := strings.TrimSpace(valueOf(task.Config["rsi_session_id"])); sessionID != "" {
				message["session_id"] = sessionID
			}
		}()
	}
	return message
}

// push 统一推送出口（推送失败仅告警，不中断）。
func (h *AgentServerHandlers) push(eventType string, payload map[string]any) {
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[RSI] push failed: %s", eventType)
			}
		}()
		if err := h.sendPush(h.buildPushMessage(eventType, payload)); err != nil {
			log.Printf("[RSI] push failed: %s", eventType)
		}
	}()
}

// ──────────────────────────── 小助手 ────────────────────────────

// rsiMethodOf 请求 → method 名。
func rsiMethodOf(request map[string]any) string {
	return valueOf(request["req_method"])
}

// paramsOf 请求参数（非 dict 归一空表）。
func paramsOf(request map[string]any) map[string]any {
	params, _ := request["params"].(map[string]any)
	if params == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	for k, v := range params {
		out[k] = v
	}
	return out
}

// valueOf any → string。
func valueOf(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", s)
	}
}

// errorPayload RsiError → 业务码；其他 → INTERNAL_ERROR。
func errorPayload(err error, method string) map[string]any {
	if re, ok := err.(*rsi.RsiError); ok {
		return map[string]any{"ok": false, "error": re.Message, "code": re.Code}
	}
	log.Printf("[RSI] %s failed: %v", method, err)
	return map[string]any{"ok": false, "error": err.Error(), "code": "INTERNAL_ERROR"}
}

package intellirouter

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/routing"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 常量 ────────────────────────────

// 事件桥订阅集合与路由元数据字段白名单
// （Python: intelli_router_model_client.py:342-353）。
var (
	// selectedEvents 最终路由选定事件（Python: _SELECTED_EVENTS）
	selectedEvents = map[string]struct{}{"request_succeeded": {}, "stream_route_selected": {}}
	// failedEvents 路由耗尽事件（Python: _FAILED_EVENTS）
	failedEvents = map[string]struct{}{"all_deployments_exhausted": {}}
	// routeMetadataFields 路由元数据字段白名单（Python: _ROUTE_METADATA_FIELDS）
	routeMetadataFields = []string{
		"model_group_id", "route_id", "model_id", "model_name", "routed_model",
		"provider", "attempt", "fallback_reason", "ttft",
	}
)

// ──────────────────────────── 结构体 ────────────────────────────

// CoreRoutingEventBridge 把 intelli_router 路由事件桥接进 openjiuwen 回调。
// Python: intelli_router_model_client.py:338-432 _CoreRoutingEventBridge
type CoreRoutingEventBridge struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// Name 处理器名（Python: :356-358 name property）。
func (b *CoreRoutingEventBridge) Name() string { return "CoreRoutingEventBridge" }

// HandleEvent 处理路由事件：request_retried 记失败换路日志；ROUTING_RECORD_EVENTS
// 同步失败记录；最终路由/耗尽事件触发 LLM_ROUTE 回调。
// Python: :359-374 handle_event
func (b *CoreRoutingEventBridge) HandleEvent(event observability.RoutingEvent) {
	eventType := string(event.EventType)
	if eventType == "request_retried" {
		logRouteFailover(event)
	}
	if _, ok := routing.RoutingRecordEvents[eventType]; ok {
		// 路由器不保留失败原因；为错误消息记录它们
		// （duck-typed 事件以 map 承载，routing.eventField 直接读取）
		routing.RecordRoutingEvent(eventToMap(event))
	}
	payload := payloadFromEvent(event)
	if payload == nil {
		return
	}
	// Python: :370-373 trigger(LLMCallEvents.LLM_ROUTE, route_metadata=payload, routing_event=event)
	//（EventHandler 接口不带 ctx，Python 无对应；Go 用 Background 承载）
	callback.Trigger(context.Background(), string(callback.LLMRoute), map[string]any{
		"route_metadata": payload,
		"routing_event":  event,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// logRouteFailover 记录路由器尝试下一个候选前失败的路由。
// Python: :383-404 _log_route_failover（字段逐条对齐）。
func logRouteFailover(event observability.RoutingEvent) {
	extra := event.Extra
	routeID := stringOr(extra["route_id"], event.DeploymentID)
	provider := stringOr(extra["provider"], event.Provider)
	errorType := stringOr(extra["fallback_reason"], nil)
	loggerWarn(fmt.Sprintf(
		"model group route failed, trying the next candidate: "+
			"model_group_id=%s route_id=%s model_name=%s provider=%s "+
			"attempt=%v/%v error_type=%v error_message=%v",
		extra["model_group_id"], routeID, extra["model_name"], provider,
		event.Attempt, event.TotalAttempts, errorType, event.ErrorMessage,
	))
}

// payloadFromEvent 组装 LLM_ROUTE 回调载荷。
// Python: :406-432 _payload_from_event
func payloadFromEvent(event observability.RoutingEvent) map[string]any {
	eventType := string(event.EventType)
	var status string
	switch {
	case inStrSet(eventType, selectedEvents):
		status = "selected"
	case inStrSet(eventType, failedEvents):
		status = "failed"
	default:
		return nil
	}

	extra := event.Extra
	groupID, _ := extra["model_group_id"].(string)
	if groupID == "" {
		return nil
	}

	payload := map[string]any{}
	for _, key := range routeMetadataFields {
		if v, exists := extra[key]; exists && v != nil {
			payload[key] = v
		}
	}
	payload["status"] = status
	// Python: :427-431 attempt/total_attempts/error_type/error_message/chunk_count
	if event.Attempt > 0 {
		if _, exists := payload["attempt"]; !exists {
			payload["attempt"] = event.Attempt
		}
	}
	if event.TotalAttempts > 0 {
		if _, exists := payload["total_attempts"]; !exists {
			payload["total_attempts"] = event.TotalAttempts
		}
	}
	if event.ErrorMessage != nil && *event.ErrorMessage != "" {
		if _, exists := payload["error_message"]; !exists {
			payload["error_message"] = *event.ErrorMessage
		}
	}
	if _, exists := payload["result_type"]; !exists {
		if status == "failed" {
			payload["result_type"] = "error"
		} else {
			payload["result_type"] = "answer"
		}
	}
	return payload
}

// eventToMap 把 RoutingEvent 转成 routing.RecordRoutingEvent 可读的 map
// （键名对齐 Python 事件属性：event_type/deployment_id/error_type/
// error_message/extra；Go 侧无对应独立函数）。
func eventToMap(event observability.RoutingEvent) map[string]any {
	return map[string]any{
		"event_type":    string(event.EventType),
		"deployment_id": derefStr(event.DeploymentID),
		"error_type":    derefStr(event.ErrorType),
		"error_message": derefStr(event.ErrorMessage),
		"extra":         event.Extra,
	}
}

// stringOr 第一个非空字符串（后备链）。
func stringOr(values ...any) any {
	for _, v := range values {
		switch s := v.(type) {
		case string:
			if s != "" {
				return s
			}
		case *string:
			if s != nil && *s != "" {
				return *s
			}
		}
	}
	return nil
}

// inStrSet 集合成员判断。
func inStrSet(s string, set map[string]struct{}) bool {
	_, ok := set[s]
	return ok
}

// loggerWarn 组件级 Warn 日志（与 client.go logWarn 同名避让：桥内独立包装）。
func loggerWarn(msg string) { logWarn(msg) }

// 本文件对齐 Python jiuwenswarm/server/personal_context/ws_handler.py（412 行，一比一）。
// PersonalContext WebSocket 请求分发——复用既有 E2A wire 路径，不引入独立线协议。
// Go 侧适配（登记 spec 偏差）：Python 的 ws+send_lock 发送面改为 wireSender 接口
// （server.AgentServer 经 sendToGateway 实现）；响应/分帧经 e2a wire codec 构造。

package personalcontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// validationErr 参数校验错误（Python ValueError 分支，ws_handler.py:383-393 → wire code=BAD_REQUEST）
type validationErr struct {
	// msg 校验消息
	msg string
}

// Error 实现 error
func (e *validationErr) Error() string { return e.msg }

// ValidationError 校验错误标记（供 codeForError 判别）
func (e *validationErr) ValidationError() bool { return true }

// newValidationError 构造校验错误
func newValidationError(format string, args ...any) error {
	return &validationErr{msg: fmt.Sprintf(format, args...)}
}

// ──────────────────────────── 结构体 ────────────────────────────

// WireSender wire 发送面（Python ws + send_lock + send_wire_payload 的对应物；
// 实现方：server.AgentServer.sendToGateway）
type WireSender interface {
	// SendWire 发送一段已编码 wire JSON；返回 false 表示发送失败
	SendWire(data []byte) bool
}

// RuntimeEnabledChanged 智能体使用开关变化回调（Python runtime_enabled_changed 参数，
// ws_handler.py:231；best-effort，Rail 刷新失败不使 API 调用失败）
type RuntimeEnabledChanged func(enabled bool) error

// ──────────────────────────── 常量 ────────────────────────────

// personalContextRequestMethods 26 个可分发 method 集合（Python PERSONAL_CONTEXT_REQUEST_METHODS，
// ws_handler.py:33-62 frozenset；成员顺序按 runtime/fetch/context 三族罗列）
var personalContextRequestMethods = map[schema.ReqMethod]bool{
	schema.ReqMethodPersonalContextRuntimeStatus:               true,
	schema.ReqMethodPersonalContextRuntimeStartCollection:      true,
	schema.ReqMethodPersonalContextRuntimeStopCollection:       true,
	schema.ReqMethodPersonalContextRuntimeStartAgentUse:        true,
	schema.ReqMethodPersonalContextRuntimeStopAgentUse:         true,
	schema.ReqMethodPersonalContextRuntimeSetMasterEnabled:     true,
	schema.ReqMethodPersonalContextRuntimeGetConfig:            true,
	schema.ReqMethodPersonalContextRuntimePatchConfig:          true,
	schema.ReqMethodPersonalContextRuntimeSelectModel:          true,
	schema.ReqMethodPersonalContextFetchListServices:           true,
	schema.ReqMethodPersonalContextFetchCreateService:          true,
	schema.ReqMethodPersonalContextFetchDeleteService:          true,
	schema.ReqMethodPersonalContextFetchPatchService:           true,
	schema.ReqMethodPersonalContextFetchStartService:           true,
	schema.ReqMethodPersonalContextFetchStopService:            true,
	schema.ReqMethodPersonalContextFetchRunAll:                 true,
	schema.ReqMethodPersonalContextFetchRunOne:                 true,
	schema.ReqMethodPersonalContextFetchStopRun:                true,
	schema.ReqMethodPersonalContextFetchGetRunStatus:           true,
	schema.ReqMethodPersonalContextFetchGetAuthorizationStatus: true,
	schema.ReqMethodPersonalContextFetchAuthorizeProvider:      true,
	schema.ReqMethodPersonalContextContextStreamGraph:          true,
	schema.ReqMethodPersonalContextContextStreamTree:           true,
	schema.ReqMethodPersonalContextContextSearchPages:          true,
	schema.ReqMethodPersonalContextContextGetNode:              true,
	schema.ReqMethodPersonalContextContextGetSource:            true,
}

// streamChunkBatchSize 图谱分帧批量（Python ws_handler.py:188/196 的 200）
const streamChunkBatchSize = 200

// streamDepthMin/streamDepthMax depth 取值边界（ws_handler.py:169）
const (
	streamDepthMin = 1
	streamDepthMax = 10
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsPersonalContextMethod 判断是否 personal_context 分发面 method（Python 集合成员判断）
func IsPersonalContextMethod(method schema.ReqMethod) bool {
	return personalContextRequestMethods[method]
}

// HandlePersonalContextRequest 执行一个已解析的 PersonalContext 请求
// （Python handle_personal_context_request，ws_handler.py:344-412）。
// 非流式：返回 (resp, nil) 由调用方写回；流式（stream_graph/stream_tree）：内部逐帧发送后返回 (nil, nil)。
func HandlePersonalContextRequest(host *PersonalContextHostAPI, sender WireSender, request *schema.AgentRequest, runtimeEnabledChanged RuntimeEnabledChanged) (*schema.AgentResponse, error) {
	switch request.ReqMethod {
	case schema.ReqMethodPersonalContextContextStreamGraph, schema.ReqMethodPersonalContextContextStreamTree:
		// Python host_api.py:355-370：流式方法必须 is_stream=true
		if !request.IsStream {
			validationErr := newValidationError("%s requires is_stream=true", request.ReqMethod)
			sendPersonalContextError(host, sender, request, validationErr, "BAD_REQUEST", "")
			return nil, nil
		}
		err := streamGraph(host, sender, request, request.ReqMethod == schema.ReqMethodPersonalContextContextStreamTree)
		if err != nil {
			sendPersonalContextError(host, sender, request, err, codeForError(err), statusForError(err))
		}
		return nil, nil
	}
	payload, err := execute(host, request, runtimeEnabledChanged)
	if err != nil {
		LogError(err)
		sendPersonalContextError(host, sender, request, err, codeForError(err), statusForError(err))
		return nil, nil
	}
	sendPersonalContextResult(sender, request, payload)
	return nil, nil
}

// ──────────────────────────── 非导出函数（Python 模块级 helper 同序） ────────────────────────────

// payloadValue 结果归一（Python _payload，ws_handler.py:65-74）
func payloadValue(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{"ok": true}, nil
	}
	if m, ok := value.(map[string]any); ok {
		return m, nil
	}
	return nil, newValidationError("PersonalContext Host result must be an object")
}

// textParam 非空字符串参数（Python _text，ws_handler.py:77-81）
func textParam(params map[string]any, name string) (string, error) {
	value, _ := params[name].(string)
	if value == "" || strings.TrimSpace(value) == "" {
		return "", newValidationError("%s must be a non-empty string", name)
	}
	return strings.TrimSpace(value), nil
}

// sendPersonalContextResult 发送成功响应（Python _send_result，ws_handler.py:84-114）
func sendPersonalContextResult(sender WireSender, request *schema.AgentRequest, payload map[string]any) {
	if request.IsStream {
		chunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID, payload,
			schema.WithChunkIsComplete(true))
		sendWire(sender, e2a.EncodeAgentChunkForWire(chunk, request.RequestID, 0, true), request.RequestID)
		return
	}
	resp := schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true), schema.WithResponsePayload(payload))
	sendWire(sender, e2a.EncodeAgentResponseForWire(resp, request.RequestID, 0), request.RequestID)
}

// sendPersonalContextError 发送错误响应（Python _send_error，ws_handler.py:117-154）
func sendPersonalContextError(host *PersonalContextHostAPI, sender WireSender, request *schema.AgentRequest, err error, code string, status string) {
	LogError(err)
	language := host.ErrorLanguage()
	message := LocalizeError(err, language, "")
	if code == "BAD_REQUEST" {
		message = LocalizeError(err, language, "parameter_invalid") // Python: reason="parameter_invalid"（ws_handler.py:390）
	}
	payload := map[string]any{"error": message, "code": code}
	if status != "" {
		payload["status"] = status
	}
	if request.IsStream {
		payload["event_type"] = "chat.error" // Python ws_handler.py:130
		chunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID, payload,
			schema.WithChunkIsComplete(true))
		sendWire(sender, e2a.EncodeAgentChunkForWire(chunk, request.RequestID, 0, true), request.RequestID)
		return
	}
	resp := schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false), schema.WithResponsePayload(payload))
	sendWire(sender, e2a.EncodeAgentResponseForWire(resp, request.RequestID, 0), request.RequestID)
}

// streamGraph 流式发送图谱四事件（Python _stream_graph，ws_handler.py:157-224）
func streamGraph(host *PersonalContextHostAPI, sender WireSender, request *schema.AgentRequest, tree bool) error {
	params, _ := mustParams(request)
	rootID := params["root_id"]
	if rootID != nil {
		if s, ok := rootID.(string); !ok || strings.TrimSpace(s) == "" {
			return newValidationError("root_id must be null or a non-empty string")
		}
	}
	depth := 3
	if raw, present := params["depth"]; present {
		d, ok := toInt(raw)
		if !ok || d < streamDepthMin || d > streamDepthMax {
			return newValidationError("depth must be an integer between 1 and 10")
		}
		depth = d
	}
	normalizedRoot := ""
	if s, ok := rootID.(string); ok {
		normalizedRoot = strings.TrimSpace(s)
	}
	var graph map[string]any
	var herr *HostError
	if tree {
		graph, herr = host.GetTree(rootIDAny(normalizedRoot), depth)
	} else {
		graph, herr = host.GetGraph(rootIDAny(normalizedRoot), depth)
	}
	if herr != nil {
		return herr
	}
	nodes, okN := graph["nodes"].([]any)
	edges, okE := graph["edges"].([]any)
	if !okN || !okE {
		return newValidationError("PersonalContext graph nodes and edges must be arrays")
	}
	events := []map[string]any{
		{
			"event_type":    "personal_context.context.start",
			"context_ready": boolOr(graph["context_ready"]),
			"root_id":       rootIDAny(normalizedRoot),
			"depth":         depth,
		},
	}
	for start := 0; start < len(nodes); start += streamChunkBatchSize {
		stop := start + streamChunkBatchSize
		if stop > len(nodes) {
			stop = len(nodes)
		}
		events = append(events, map[string]any{
			"event_type": "personal_context.context.nodes",
			"nodes":      nodes[start:stop],
		})
	}
	for start := 0; start < len(edges); start += streamChunkBatchSize {
		stop := start + streamChunkBatchSize
		if stop > len(edges) {
			stop = len(edges)
		}
		events = append(events, map[string]any{
			"event_type": "personal_context.context.edges",
			"edges":      edges[start:stop],
		})
	}
	events = append(events, map[string]any{
		"event_type": "personal_context.context.end",
		"node_count": len(nodes),
		"edge_count": len(edges),
	})
	for sequence, event := range events {
		chunk := schema.NewAgentResponseChunk(request.RequestID, request.ChannelID, event,
			schema.WithChunkIsComplete(sequence == len(events)-1))
		if !sendWire(sender, e2a.EncodeAgentChunkForWire(chunk, request.RequestID, sequence, true), request.RequestID) {
			return nil // Python 发送失败即停止流
		}
	}
	return nil
}

// execute 26 method 分发（Python _execute，ws_handler.py:227-319；分支顺序一致）
func execute(host *PersonalContextHostAPI, request *schema.AgentRequest, runtimeEnabledChanged RuntimeEnabledChanged) (map[string]any, error) {
	method := request.ReqMethod
	params, perr := mustParams(request)
	if perr != nil {
		return nil, perr
	}
	switch method {
	case schema.ReqMethodPersonalContextRuntimeStatus:
		return payloadValue(host.GetStatus())
	case schema.ReqMethodPersonalContextRuntimeStartCollection:
		return hostPayload(host.SetCollectionEnabled(true))
	case schema.ReqMethodPersonalContextRuntimeStopCollection:
		return hostPayload(host.SetCollectionEnabled(false))
	case schema.ReqMethodPersonalContextRuntimeStartAgentUse:
		result, herr := host.SetAgentUseEnabled(true)
		if herr != nil {
			return nil, herr
		}
		if nerr := notifyRuntimeEnabled(runtimeEnabledChanged, true); nerr != nil {
			return nil, nerr
		}
		return payloadValue(result)
	case schema.ReqMethodPersonalContextRuntimeStopAgentUse:
		result, herr := host.SetAgentUseEnabled(false)
		if herr != nil {
			return nil, herr
		}
		if nerr := notifyRuntimeEnabled(runtimeEnabledChanged, false); nerr != nil {
			return nil, nerr
		}
		return payloadValue(result)
	case schema.ReqMethodPersonalContextRuntimeSetMasterEnabled:
		enabled, ok := params["enabled"].(bool)
		if !ok {
			return nil, newValidationError("enabled must be a boolean")
		}
		result, herr := host.SetMasterEnabled(enabled)
		if herr != nil {
			return nil, herr
		}
		if nerr := notifyRuntimeEnabled(runtimeEnabledChanged, enabled); nerr != nil {
			return nil, nerr
		}
		return payloadValue(result)
	case schema.ReqMethodPersonalContextRuntimeGetConfig:
		return hostAnyPayload(host.GetRuntimeConfig())
	case schema.ReqMethodPersonalContextRuntimePatchConfig:
		patch, _ := params["patch"].(map[string]any)
		return hostAnyPayload(host.PatchRuntimeConfig(patch))
	case schema.ReqMethodPersonalContextRuntimeSelectModel:
		return hostAnyPayload(host.SelectModel(params["model_index"]))
	case schema.ReqMethodPersonalContextFetchListServices:
		services, herr := host.ListFetchServices()
		if herr != nil {
			return nil, herr
		}
		anyServices := make([]any, len(services))
		for i, s := range services {
			anyServices[i] = s
		}
		return map[string]any{"services": anyServices}, nil
	case schema.ReqMethodPersonalContextFetchCreateService:
		service, ok := params["service"].(map[string]any)
		if !ok {
			return nil, newValidationError("service must be an object")
		}
		return hostAnyPayload(host.CreateFetchService(service))
	case schema.ReqMethodPersonalContextFetchDeleteService:
		serviceID, terr := textParam(params, "service_id")
		if terr != nil {
			return nil, terr
		}
		if herr := host.DeleteFetchService(serviceID); herr != nil {
			return nil, herr
		}
		return map[string]any{"ok": true}, nil
	case schema.ReqMethodPersonalContextFetchPatchService:
		serviceID, terr := textParam(params, "service_id")
		if terr != nil {
			return nil, terr
		}
		patch, _ := params["patch"].(map[string]any)
		return hostAnyPayload(host.PatchFetchService(serviceID, patch))
	case schema.ReqMethodPersonalContextFetchStartService, schema.ReqMethodPersonalContextFetchStopService:
		serviceID, terr := textParam(params, "service_id")
		if terr != nil {
			return nil, terr
		}
		if herr := host.SetFetchServiceEnabled(serviceID, method == schema.ReqMethodPersonalContextFetchStartService); herr != nil {
			return nil, herr
		}
		return map[string]any{"ok": true}, nil
	case schema.ReqMethodPersonalContextFetchRunAll:
		return hostAnyPayload(host.RunFetch(nil))
	case schema.ReqMethodPersonalContextFetchRunOne:
		serviceID, terr := textParam(params, "service_id")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.RunFetch(serviceID))
	case schema.ReqMethodPersonalContextFetchStopRun:
		serviceID, terr := textParam(params, "service_id")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.StopFetchRun(serviceID))
	case schema.ReqMethodPersonalContextFetchGetRunStatus:
		return hostAnyPayload(host.GetFetchRunStatus(params["service_id"], params["run_id"]))
	case schema.ReqMethodPersonalContextFetchGetAuthorizationStatus:
		provider, terr := textParam(params, "provider")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.GetAuthorizationStatus(provider))
	case schema.ReqMethodPersonalContextFetchAuthorizeProvider:
		credentials, _ := params["credentials"].(map[string]any)
		if raw, present := params["credentials"]; present && raw != nil {
			c, ok := raw.(map[string]any)
			if !ok {
				return nil, newValidationError("credentials must be an object")
			}
			credentials = c
		}
		reauthorize := false
		if raw, present := params["reauthorize"]; present {
			b, ok := raw.(bool)
			if !ok {
				return nil, newValidationError("reauthorize must be a boolean")
			}
			reauthorize = b
		}
		provider, terr := textParam(params, "provider")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.AuthorizeProvider(provider, credentials, reauthorize))
	case schema.ReqMethodPersonalContextContextSearchPages:
		query, terr := textParam(params, "query")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.SearchGraph(query))
	case schema.ReqMethodPersonalContextContextGetNode:
		nodeID, terr := textParam(params, "node_id")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.GetGraphPage(nodeID))
	case schema.ReqMethodPersonalContextContextGetSource:
		sourceID, terr := textParam(params, "source_id")
		if terr != nil {
			return nil, terr
		}
		return hostAnyPayload(host.GetSource(sourceID))
	default:
		return nil, newValidationError("unknown PersonalContext method")
	}
}

// notifyRuntimeEnabled best-effort 通知（Python _notify_runtime_enabled，ws_handler.py:322-341）
func notifyRuntimeEnabled(callback RuntimeEnabledChanged, enabled bool) error {
	if callback == nil {
		return nil
	}
	if err := callback(enabled); err != nil {
		logger.Warn(logComponent).Str("error_type", errorTypeName(err)).Msg("PersonalContext runtime Rail refresh failed")
	}
	return nil
}

// ──────────────────────────── wire/错误小工具 ────────────────────────────

// sendWire 编码并发送（Python async with send_lock: send_wire_payload；Go 侧发送串行化由
// AgentServer.sendToGateway 内部保证）
func sendWire(sender WireSender, wire map[string]any, requestID string) bool {
	data, err := json.Marshal(wire)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("personal_context wire JSON encode failed")
		return false
	}
	return sender.SendWire(data)
}

// mustParams 解析 params 为 map（Go request.Params 是 RawMessage；Python 是 dict）
func mustParams(request *schema.AgentRequest) (map[string]any, error) {
	if len(request.Params) == 0 {
		return map[string]any{}, nil
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return nil, newValidationError("params must be an object")
	}
	if params == nil {
		params = map[string]any{}
	}
	return params, nil
}

// hostPayload *HostError → error 适配（payload 已校验为 map）
func hostPayload(result map[string]any, herr *HostError) (map[string]any, error) {
	if herr != nil {
		return nil, herr
	}
	return result, nil
}

// hostAnyPayload result 为 any 的适配（nil 视为 {"ok": true}，对齐 Python _payload）
func hostAnyPayload(result map[string]any, herr *HostError) (map[string]any, error) {
	if herr != nil {
		return nil, herr
	}
	if result == nil {
		return map[string]any{"ok": true}, nil
	}
	return result, nil
}

// codeForError 错误→wire code（Python _send_error 调用点：ValueError→BAD_REQUEST、
// BaseError→str(exc.code)、其余 INTERNAL_ERROR，ws_handler.py:383-412）
func codeForError(err error) string {
	var host *HostError
	switch {
	case asValidationError(err):
		return "BAD_REQUEST"
	case errorsAsHost(err, &host):
		return host.Code
	default:
		return "INTERNAL_ERROR"
	}
}

// statusForError 错误→wire status（仅 HostError 携带 status.name）
func statusForError(err error) string {
	var host *HostError
	if errorsAsHost(err, &host) {
		return host.StatusName
	}
	return ""
}

// asValidationError 参数校验类错误（Python ValueError 分支）
func asValidationError(err error) bool {
	var v *validationErr
	return errors.As(err, &v)
}

// isHostErrorValue 是否为 *HostError 值
func isHostErrorValue(err error) bool {
	var host *HostError
	return errorsAsHost(err, &host)
}

// errorsAsHost errors.As 薄封装
func errorsAsHost(err error, target **HostError) bool {
	return errors.As(err, target)
}

// rootIDAny 空串归一为 nil（Python root_id=None 语义，ws_handler.py:171）
func rootIDAny(v string) any {
	if v == "" {
		return nil
	}
	return v
}

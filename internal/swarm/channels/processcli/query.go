package processcli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// queryShutdownTimeoutSeconds 查询路径关闭 Runtime 的窗口。
	// Python: query.py:25 SHUTDOWN_TIMEOUT_SECONDS = 5.0
	queryShutdownTimeoutSeconds = 5.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DispatchQuery 只走公有查询；绝不为查询获取 Agent 或接管别的渠道的会话。
// Python: query.py:29-54 dispatch_query
//
// 三条路径（顺序即语义）：
//  1. model.list / mode.list —— 无参目录查询
//  2. session.get —— 单条会话（带 {"session": ...} 包裹，缺失时为 null）
//  3. 其余 5 个 —— session.list / model.resolve / mode.resolve /
//     permission.get / mcp.validate
//
// 未知操作返回错误（上游是 KeyError，由 run_query 的兜底 except 转成 RUNTIME_FAILED）。
func DispatchQuery(
	client *InProcessRuntimeClient, request *protocol.OneShotQueryInput,
) (map[string]any, error) {
	if request == nil {
		return nil, errors.New("query request is nil")
	}
	params := request.Params
	if params == nil {
		params = map[string]any{}
	}

	// Python: :34-39 catalogs + getattr(client, name)()
	switch request.Operation {
	case "model.list":
		return modelCatalogToDict(client.ListModelCapabilities(stringParam(params, "current_selection"))), nil
	case "mode.list":
		return modeCatalogToDict(client.ListModeCapabilities()), nil
	}

	// Python: :40-42 session.get
	if request.Operation == "session.get" {
		session, err := client.GetSession(swruntime.SessionGetInput{
			ChannelID: processCliChannelID,
			SessionID: stringParam(params, "session_id"),
		})
		if err != nil {
			return nil, err
		}
		if session == nil {
			return map[string]any{"session": nil}, nil
		}
		return map[string]any{"session": sessionSummaryToDict(session)}, nil
	}

	// Python: :43-54 operations 表
	switch request.Operation {
	case "session.list":
		result, err := client.ListSessions(swruntime.SessionListInput{
			ChannelID: processCliChannelID,
			Limit:     intParam(params, "limit"),
			Offset:    intParam(params, "offset"),
			Search:    stringParam(params, "search"),
		})
		if err != nil {
			return nil, err
		}
		return sessionListToDict(result), nil
	case "model.resolve":
		descriptor, err := client.ResolveModelCapability(stringParam(params, "requested"))
		if err != nil {
			return nil, err
		}
		return modelDescriptorToDict(descriptor), nil
	case "mode.resolve":
		descriptor, err := client.ResolveModeCapability(stringParam(params, "requested"))
		if err != nil {
			return nil, err
		}
		return modeDescriptorToDict(descriptor), nil
	case "permission.get":
		snapshot, err := client.GetPermissionSnapshot(swruntime.PermissionSnapshotInput{
			ChannelID: processCliChannelID,
			SessionID: optionalStringParam(params, "session_id"),
		})
		if err != nil {
			return nil, err
		}
		return permissionSnapshotToDict(snapshot), nil
	case "mcp.validate":
		result, err := client.ValidateMcpReferences(stringSliceParam(params, "references"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"valid": result.Valid}, nil
	}
	return nil, fmt.Errorf("unsupported query operation: %s", request.Operation)
}

// QueryFailure 构造一次查询失败结果。
// Python: query.py:57-68 query_failure
func QueryFailure(requestID, code, message string, exitCode int) *protocol.OneShotQueryResult {
	// Python: :60-62 status = {124: TIMED_OUT, 130: CANCELLED}.get(exit_code, FAILED)
	status := string(protocol.StatusFailed)
	switch exitCode {
	case 124:
		status = string(protocol.StatusTimedOut)
	case 130:
		status = string(protocol.StatusCancelled)
	}
	return &protocol.OneShotQueryResult{
		RequestID: requestID,
		Status:    status,
		ExitCode:  exitCode,
		Error:     &protocol.RuntimeErrorInfo{Code: code, Message: message},
	}
}

// RunQuery 启动并关闭恰好一个 Runtime，即使启动只完成一部分。
// Python: query.py:71-146 run_query
func RunQuery(
	ctx context.Context,
	request *protocol.OneShotQueryInput,
	requestID string,
	clientFactory func() *InProcessRuntimeClient,
) *protocol.OneShotQueryResult {
	// Python: :78-81 初始失败结果 + 超时预算
	client := (*InProcessRuntimeClient)(nil)
	// Python: :79-81 初始失败结果；后续每个分支都会覆盖，故此处只声明
	var result *protocol.OneShotQueryResult

	deadline := time.Time{}
	if request.TimeoutSeconds != nil && *request.TimeoutSeconds > 0 {
		deadline = time.Now().Add(time.Duration(*request.TimeoutSeconds * float64(time.Second)))
	}

	// Python: :83-106 try 块
	if factory := clientFactoryOrDefault(clientFactory); factory != nil {
		client = factory()
	}
	if client == nil {
		result = QueryFailure(requestID, "RUNTIME_FAILED", "Runtime query failed.", 1)
	} else if startErr := client.Start(); startErr != nil {
		// Python: :117-121 兜底 except：取 error.code 或 RUNTIME_FAILED
		result = QueryFailure(requestID, errorCodeOf(startErr), "Runtime query failed.", 1)
	} else {
		data, dispatchErr := DispatchQuery(client, request)
		if dispatchErr != nil {
			// Python: :116-121
			result = QueryFailure(requestID, errorCodeOf(dispatchErr), "Runtime query failed.", 1)
		} else if !deadline.IsZero() && time.Now().After(deadline) {
			// Python: :88-102 同步目录 I/O 不给 asyncio 定时器让路，
			// 故在取消定时器之前先查一次是否已超时
			result = QueryFailure(requestID, "TIMEOUT", "Command timed out.", 124)
		} else {
			// Python: :104-106 OneShotQueryResult(request_id, operation, data)
			result = &protocol.OneShotQueryResult{
				RequestID: requestID,
				Operation: &request.Operation,
				Status:    string(protocol.StatusCompleted),
				ExitCode:  0,
				Data:      data,
			}
		}
	}

	// Python: :122-124 finally: defer_command_signals()
	DeferCommandSignals(nil)

	// Python: :124-145 关闭 Runtime，失败时改写为 SHUTDOWN_FAILED
	if client != nil {
		if closeErr := client.Close(); closeErr != nil {
			result = withCleanupError(result, "runtime_close")
		}
	}

	// Python: :146 return replace(result, operation=request.operation)
	if request != nil {
		result.Operation = &request.Operation
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// withCleanupError 把清理失败并入结果的 error.details.cleanup_errors。
// Python: query.py:129-144
//
//	info = result.error or RuntimeErrorInfo(code="SHUTDOWN_FAILED", ...)
//	result = replace(result, status=result.status if result.error else FAILED,
//	                 exit_code=result.exit_code or 1, data=None,
//	                 error=replace(info, details={... "cleanup_errors": (name,)}))
func withCleanupError(result *protocol.OneShotQueryResult, name string) *protocol.OneShotQueryResult {
	info := result.Error
	if info == nil {
		info = &protocol.RuntimeErrorInfo{
			Code:    "SHUTDOWN_FAILED",
			Message: "Runtime cleanup failed.",
		}
	}
	details := map[string]any{}
	for k, v := range info.Details {
		details[k] = v
	}
	existing, _ := details["cleanup_errors"].([]any)
	details["cleanup_errors"] = append(existing, name)

	// Python: status 与 exit_code 的保留条件
	status := result.Status
	if result.Error == nil {
		status = string(protocol.StatusFailed)
	}
	exitCode := result.ExitCode
	if exitCode == 0 {
		exitCode = 1
	}
	copied := *result
	copied.Status = status
	copied.ExitCode = exitCode
	copied.Data = nil
	copied.Error = &protocol.RuntimeErrorInfo{
		Code:      info.Code,
		Message:   info.Message,
		Retryable: info.Retryable,
		Details:   details,
	}
	return &copied
}

// errorCodeOf 取错误的 code（对齐 Python getattr(error, "code", "RUNTIME_FAILED")）。
func errorCodeOf(err error) string {
	var boundary *ProcessCliBoundaryError
	if errors.As(err, &boundary) {
		return boundary.Code()
	}
	// Python: getattr(error, "code", "RUNTIME_FAILED")，非空字符串才采用
	if coder, ok := err.(interface{ Code() string }); ok {
		if code := coder.Code(); code != "" {
			return code
		}
	}
	return "RUNTIME_FAILED"
}

// modelCatalogToDict 序列化模型目录。
// Python: model_catalog.py:65-71 ModelCatalogResult.to_dict
func modelCatalogToDict(result swruntime.ModelCatalogResult) map[string]any {
	models := make([]any, 0, len(result.Models))
	for i := range result.Models {
		models = append(models, modelDescriptorToDict(result.Models[i]))
	}
	return map[string]any{
		"models":               models,
		"current_selection":    result.CurrentSelection,
		"current_display_name": result.CurrentDisplayName,
	}
}

// modelDescriptorToDict 序列化单个模型描述。
// Python: model_catalog.py:43-54 RuntimeModelDescriptor.to_dict
func modelDescriptorToDict(d swruntime.RuntimeModelDescriptor) map[string]any {
	return map[string]any{
		"selection_key":   d.SelectionKey,
		"display_name":    d.DisplayName,
		"model_name":      d.ModelName,
		"provider":        d.Provider,
		"reasoning_level": d.ReasoningLevel,
		"is_default":      d.IsDefault,
		"is_agentos":      d.IsAgentOS,
		"is_current":      d.IsCurrent,
	}
}

// modeCatalogToDict 序列化模式目录。
// Python: mode_catalog.py:55-56 ModeCatalogResult.to_dict
func modeCatalogToDict(result swruntime.ModeCatalogResult) map[string]any {
	modes := make([]any, 0, len(result.Modes))
	for i := range result.Modes {
		modes = append(modes, modeDescriptorToDict(result.Modes[i]))
	}
	return map[string]any{"modes": modes}
}

// modeDescriptorToDict 序列化单个模式描述。
// Python: mode_catalog.py:39-46 RuntimeModeDescriptor.to_dict
func modeDescriptorToDict(d swruntime.RuntimeModeDescriptor) map[string]any {
	return map[string]any{
		"mode":                              d.Mode,
		"work_mode":                         d.WorkMode,
		"is_plan":                           d.IsPlan,
		"supports_custom_agent_definitions": d.SupportsCustomAgentDefinitions,
	}
}

// sessionSummaryToDict 序列化会话摘要。
// Python: session_catalog.py:64-76 SessionSummary.to_dict
func sessionSummaryToDict(s *swruntime.SessionSummary) map[string]any {
	if s == nil {
		return nil
	}
	return map[string]any{
		"session_id":      s.SessionID,
		"channel_id":      s.ChannelID,
		"title":           s.Title,
		"mode":            s.Mode,
		"work_mode":       s.WorkMode,
		"project_id":      s.ProjectID,
		"project_dir":     s.ProjectDir,
		"model":           s.Model,
		"created_at":      s.CreatedAt,
		"last_message_at": s.LastMessageAt,
		"message_count":   s.MessageCount,
	}
}

// sessionListToDict 序列化一页会话。
// Python: session_catalog.py:89-95 SessionListResult.to_dict
func sessionListToDict(result swruntime.SessionListResult) map[string]any {
	sessions := make([]any, 0, len(result.Sessions))
	for i := range result.Sessions {
		sessions = append(sessions, sessionSummaryToDict(&result.Sessions[i]))
	}
	return map[string]any{
		"sessions": sessions,
		"total":    result.Total,
		"limit":    result.Limit,
		"offset":   result.Offset,
	}
}

// permissionSnapshotToDict 序列化权限快照。
// Python: permission_catalog.py:100-108 PermissionSnapshotResult.to_dict
//
// 六个字段：scope / session_id / global / user / session / effective
// （后四个是 PermissionLayerSnapshot 字典）。
func permissionSnapshotToDict(result swruntime.PermissionSnapshotResult) map[string]any {
	return map[string]any{
		"scope":      result.Scope,
		"session_id": result.SessionID,
		"global":     permissionLayerToDict(result.GlobalLayer),
		"user":       permissionLayerToDict(result.UserLayer),
		"session":    permissionLayerToDict(result.SessionLayer),
		"effective":  permissionLayerToDict(result.Effective),
	}
}

// permissionLayerToDict 序列化一层权限。
// Python: permission_catalog.py:81-86 PermissionLayerSnapshot.to_dict
func permissionLayerToDict(layer swruntime.PermissionLayerSnapshot) map[string]any {
	tools := make([]any, 0, len(layer.Tools))
	for _, tool := range layer.Tools {
		tools = append(tools, map[string]any{"name": tool.Name, "level": tool.Level})
	}
	rules := make([]any, 0, len(layer.Rules))
	for _, rule := range layer.Rules {
		rules = append(rules, map[string]any{
			"id":          rule.RuleID,
			"tools":       stringSliceToAny(rule.Tools),
			"pattern":     rule.Pattern,
			"action":      rule.Action,
			"severity":    rule.Severity,
			"description": rule.Description,
			"match_type":  rule.MatchType,
		})
	}
	return map[string]any{
		"enabled": layer.Enabled,
		"tools":   tools,
		"rules":   rules,
	}
}

// clientFactoryOrDefault 取客户端工厂；nil 时用默认构造。
// Python: query.py:75 client_factory: Callable[[], InProcessRuntimeClient] = InProcessRuntimeClient
func clientFactoryOrDefault(factory func() *InProcessRuntimeClient) func() *InProcessRuntimeClient {
	if factory != nil {
		return factory
	}
	return func() *InProcessRuntimeClient { return NewInProcessRuntimeClient(nil) }
}

// stringParam 安全取字符串参数。
func stringParam(params map[string]any, key string) string {
	value, _ := params[key].(string)
	return value
}

// optionalStringParam 取可空字符串参数。
func optionalStringParam(params map[string]any, key string) *string {
	value, ok := params[key].(string)
	if !ok {
		return nil
	}
	return &value
}

// intParam 取整数参数（缺失返回 0）。
func intParam(params map[string]any, key string) int {
	switch value := params[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

// stringSliceParam 取字符串列表参数。
func stringSliceParam(params map[string]any, key string) []string {
	switch value := params[key].(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// stringSliceToAny []string → []any。
func stringSliceToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

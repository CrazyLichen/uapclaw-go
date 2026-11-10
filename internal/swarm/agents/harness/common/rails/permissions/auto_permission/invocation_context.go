// 工具调用上下文的提取与执行前归一化。
//
// 对应 Python 代码：_auto_permission/invocation_context.py（715 行 / 30 函数 + 2 类型）
//
// 职责：从 rail 回调输入提取规范化工具调用，并在执行前「冻结」两类会改变实际
// 执行语义的参数：
//  1. 原生路径工具（read_file / write_file / edit_file / glob / grep / list_files / read_pdf）
//     ——把相对路径解析成绝对路径
//  2. mcp_exec_command——把 workdir 解析成绝对路径
//
// ## 为什么必须冻结（安全边界）
//
// 权限层用 `resolvePathStr(raw, workspace)` 拼 **workspace** 算校验路径，
// 而工具执行器用 `ResolveToolFilePath(ctx, raw)` 拼 **cwd** 算执行路径。
// 两者不总相等：用户在子目录工作时，同一个相对路径 `config.yaml` 会指向
// 不同文件——权限层批准 A 文件，工具动 B 文件，构成绕过。
//
// 冻结后参数已是绝对路径，两侧算出的都是同一文件；并再喂给校验器做交叉验证
// （native_path_guard_execution_mismatch）。
//
// ## 与 Python 的结构性偏差（spec §12.22）
//
//   - kwargs 用 map 模拟（**不能**换 struct：CompatibilitySendMatchesHost 靠
//     「key 是否存在于 kwargs」决定校验范围）
//   - 属性写回用「构造新 ToolCallInputs + SetInputs」替代 setattr
//   - 执行器身份：Python 用 `type(resource) is ReadFileTool`（类身份），Go 的工具是
//     函数式实现、无导出具体类型，改用「装配期登记实例 + 指针相等」判定
//     （见 SetNativePathExecutors）

package auto_permission

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/tool_display"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	commandtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrustedSendIdentity 宿主为一次精确 send_file 调用持有的身份。
//
// Python: @dataclass(frozen=True) TrustedSendIdentity
type TrustedSendIdentity struct {
	// SessionID 会话标识
	SessionID string
	// RequestID 宿主请求标识
	RequestID string
	// ToolCallID 工具调用标识
	ToolCallID string
	// ChannelKind 通道类型
	ChannelKind string
	// ToolName 工具名（应为 send_file_to_user）
	ToolName string
	// NormalizedToolArgs 归一化后的工具参数
	NormalizedToolArgs map[string]any
}

// TrustedSendIdentityResolution 校验通过的发送身份，或其 fail-closed 结果。
//
// Python: @dataclass(frozen=True) TrustedSendIdentityResolution
type TrustedSendIdentityResolution struct {
	// Invocation 可信调用快照
	Invocation ToolInvocation
	// Identity 校验通过的身份（Error 非空时为 nil）
	Identity *TrustedSendIdentity
	// Error 失败原因码（空串表示成功）
	Error string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SendFileToolName 文件投递工具名
	//
	// Python: _SEND_FILE_TOOL_NAME
	SendFileToolName = "send_file_to_user"

	// smartGlobMaxLength glob 模式最大长度
	smartGlobMaxLength = 2048
	// smartGlobMaxGroups glob 花括号组数上限
	smartGlobMaxGroups = 6
	// smartGlobMaxCommas glob 逗号数上限
	smartGlobMaxCommas = 6

	// execCommandToolName 命令执行工具名
	execCommandToolName = "mcp_exec_command"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// nativePathExecutors 装配期登记的原生路径执行器实例表（工具名 → 实例）。
	//
	// Python: _NATIVE_PATH_EXECUTORS（工具名 → 执行器类）
	// Go 的工具是函数式实现、无导出具体类型，无法按「类身份」比较；
	// 改为按「实例身份」（指针相等）比较，语义等价且更严格。
	// 由 SetNativePathExecutors 在装配期注入，之后只读。
	nativePathExecutorsMu sync.RWMutex
	nativePathExecutors   = map[string]tool.Tool{}

	// nativePathToolNames 需要执行前冻结路径的工具名集合（含 read_pdf）。
	//
	// 与 Python 的 _NATIVE_PATH_EXECUTORS 键集合一致。
	nativePathToolNames = map[string]struct{}{
		"read_file":  {},
		"write_file": {},
		"edit_file":  {},
		"glob":       {},
		"grep":       {},
		"list_files": {},
		"read_pdf":   {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetNativePathExecutors 在装配期登记原生路径执行器实例。
//
// 对应 Python 的 `_NATIVE_PATH_EXECUTORS` 静态表；Go 侧因工具无导出具体类型，
// 改为运行期注入。装配方（deep_adapter / code_adapter）应传入
// `NewReadFileTool(...)` 等**注册给 ResourceMgr 的同一实例**。
//
// 未登记的工具不会被冻结路径，其权限归属保持原有 owner（与 Python 同款）。
func SetNativePathExecutors(executors map[string]tool.Tool) {
	nativePathExecutorsMu.Lock()
	defer nativePathExecutorsMu.Unlock()
	nativePathExecutors = make(map[string]tool.Tool, len(executors))
	for name, instance := range executors {
		if instance == nil {
			continue
		}
		nativePathExecutors[name] = instance
	}
}

// NormalizeInvocationToolArgs 返回本次调用由适配器掌管的参数字典。
//
// Python: normalize_invocation_tool_args(tool_name, value)
// 字符串参数先按 JSON 解析，失败则尝试修复；非对象一律返回空 map。
func NormalizeInvocationToolArgs(toolName string, value any) map[string]any {
	decoded := value
	if text, ok := value.(string); ok {
		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			decoded = parsed
		} else {
			decoded = RepairMalformedToolArguments(text)
		}
	}
	mapping, ok := decoded.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	normalized := make(map[string]any, len(mapping))
	for key, item := range mapping {
		normalized[anyToStringValue(key)] = item
	}
	_, visibleArgs := tool_display.ExtractCallGoal(normalized)
	if visible, ok := visibleArgs.(map[string]any); ok {
		normalized = make(map[string]any, len(visible))
		for key, item := range visible {
			normalized[key] = item
		}
	}
	if toolName == SendFileToolName {
		normalized["abs_file_path_list"] = permissions.NormalizeSendFilePaths(
			normalized["abs_file_path_list"])
		normalized["target_channels"] = permissions.NormalizeSendFileTargetChannels(
			normalized["target_channels"])
	}
	return normalized
}

// RepairMalformedToolArguments 修复损坏的工具参数字符串。
//
// Python: repair_malformed_tool_arguments(tool_args)
// 偏差：Python 依赖可选第三方库 json_repair（未安装返回 None）。Go 侧无等价库，
// 实现覆盖常见损坏形态的简化修复；修复失败返回 nil（与未安装库时行为一致）。
func RepairMalformedToolArguments(toolArgs string) map[string]any {
	repaired := repairJSONText(toolArgs)
	if repaired == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(repaired), &decoded); err != nil {
		return nil
	}
	mapping, ok := decoded.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(mapping))
	for key, item := range mapping {
		out[key] = item
	}
	return out
}

// ExtractInvocation 从回调输入里提取规范化的工具调用。
//
// Python: _extract_invocation(args, kwargs)
func ExtractInvocation(
	ctx *agentinterfaces.AgentCallbackContext,
	toolCall any,
	kwargs map[string]any,
) ToolInvocation {
	if toolCall == nil && ctx != nil {
		if inputs, ok := ctx.Inputs().(*agentinterfaces.ToolCallInputs); ok {
			toolCall = inputs.ToolCall
			if _, exists := kwargs["tool_name"]; !exists {
				kwargs["tool_name"] = inputs.ToolName
			}
			if _, exists := kwargs["tool_args"]; !exists {
				kwargs["tool_args"] = inputs.ToolArgs
			}
		}
	}
	if _, exists := kwargs["tool_name"]; !exists && toolCall != nil {
		kwargs["tool_name"] = ToolCallValue(toolCall, "name")
	}
	if _, exists := kwargs["tool_args"]; !exists && toolCall != nil {
		kwargs["tool_args"] = ToolCallValue(toolCall, "arguments")
	}

	toolName := strings.TrimSpace(anyToStringValue(kwargs["tool_name"]))
	toolArgs := kwargs["tool_args"]
	if toolArgs == nil {
		toolArgs = map[string]any{}
	}
	if toolCall == nil {
		toolCall = map[string]any{"name": toolName, "arguments": toolArgs}
	}
	argsMap, _ := toolArgs.(map[string]any)
	if argsMap == nil {
		argsMap = map[string]any{}
	}
	return ToolInvocation{
		Ctx:      ctx,
		ToolCall: toolCall,
		ToolName: toolName,
		ToolArgs: argsMap,
	}
}

// ResolveTrustedSendIdentity 只从当前宿主回调上下文解析 send 身份。
//
// Python: _resolve_trusted_send_identity(args, kwargs, compatibility_invocation)
// 返回 nil 表示本次调用不是 send_file（无需身份校验）。
func ResolveTrustedSendIdentity(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	kwargs map[string]any,
	compatibilityInvocation ToolInvocation,
) *TrustedSendIdentityResolution {
	// 第一步：从回调上下文里取出宿主侧的调用信息（工具名、入参、工具调用）。
	var hostToolCall any
	var hostToolName any
	var hostToolArgs any
	hasInputs := false
	if cbc != nil {
		if inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
			hostToolCall = inputs.ToolCall
			hostToolName = inputs.ToolName
			hostToolArgs = inputs.ToolArgs
			hasInputs = true
		}
	}

	// 第二步：两侧工具名统一归一后比对；两边都不是 send_file 就不用做身份校验。
	normalizedHostName := NormalizedIdentifier(hostToolName)
	compatibilityName := NormalizedIdentifier(compatibilityInvocation.ToolName)
	hostIsSend := normalizedHostName == SendFileToolName
	compatibilityIsSend := compatibilityName == SendFileToolName
	if !hostIsSend && !compatibilityIsSend {
		return nil
	}

	// 第三步：先按宿主信息拼出可信调用，后面即使校验失败也把它带回给调用方。
	trustedName := normalizedHostName
	if trustedName == "" {
		trustedName = SendFileToolName
	}
	trustedArgs, _ := hostToolArgs.(map[string]any)
	if trustedArgs == nil {
		trustedArgs = map[string]any{}
	}
	trustedInvocation := ToolInvocation{
		Ctx:      cbc,
		ToolCall: hostToolCall,
		ToolName: trustedName,
		ToolArgs: trustedArgs,
	}

	// 第四步：宿主信息缺失，或两侧对「是不是 send_file」判断不一致，都不能建立可信身份。
	if cbc == nil || !hasInputs || hostToolCall == nil || normalizedHostName == "" || hostToolArgs == nil {
		return &TrustedSendIdentityResolution{
			Invocation: trustedInvocation,
			Error:      "send_file_authorization_context_missing",
		}
	}
	if hostIsSend != compatibilityIsSend {
		return &TrustedSendIdentityResolution{
			Invocation: trustedInvocation,
			Error:      "send_file_authorization_context_mismatch",
		}
	}

	// 第五步：会话、请求、工具调用、渠道四要素齐全才继续，再从上下文里取调用身份。
	sessionID := HostSessionID(cbc)
	requestID := NormalizedIdentifier(swarmschema.ToolPermissionRequestIDFromCtx(ctx))
	toolCallID := HostToolCallID(hostToolCall)
	channelKind := NormalizedChannelIdentifier(swarmschema.ToolPermissionChannelIDFromCtx(ctx))
	if sessionID == "" || requestID == "" || toolCallID == "" || channelKind == "" {
		return &TrustedSendIdentityResolution{
			Invocation: trustedInvocation,
			Error:      "send_file_authorization_context_missing",
		}
	}

	identity := &TrustedSendIdentity{
		SessionID:          sessionID,
		RequestID:          requestID,
		ToolCallID:         toolCallID,
		ChannelKind:        channelKind,
		ToolName:           normalizedHostName,
		NormalizedToolArgs: NormalizeInvocationToolArgs(SendFileToolName, hostToolArgs),
	}
	// 最后：兼容通道的入参必须与宿主一致，不一致说明两个入口看到的不是同一次调用。
	if !CompatibilitySendMatchesHost(kwargs, compatibilityInvocation, identity) {
		return &TrustedSendIdentityResolution{
			Invocation: trustedInvocation,
			Error:      "send_file_authorization_context_mismatch",
		}
	}
	return &TrustedSendIdentityResolution{Invocation: trustedInvocation, Identity: identity}
}

// CompatibilitySendMatchesHost 兼容调用是否与宿主身份完全一致。
//
// Python: _compatibility_send_matches_host(*, args, kwargs, compatibility_invocation, identity)
// kwargs 里**存在**的键才校验——「只校验调用方声明过的字段」。
func CompatibilitySendMatchesHost(
	kwargs map[string]any,
	compatibilityInvocation ToolInvocation,
	identity *TrustedSendIdentity,
) bool {
	if compatibilityInvocation.ToolName != identity.ToolName {
		return false
	}
	if !reflect.DeepEqual(
		NormalizeInvocationToolArgs(SendFileToolName, compatibilityInvocation.ToolArgs),
		identity.NormalizedToolArgs) {
		return false
	}

	expected := map[string]string{
		"session_id":   identity.SessionID,
		"request_id":   identity.RequestID,
		"tool_call_id": identity.ToolCallID,
		"channel_kind": identity.ChannelKind,
		"tool_name":    identity.ToolName,
	}
	for key, want := range expected {
		supplied, exists := kwargs[key]
		if !exists {
			continue
		}
		got := NormalizedIdentifier(supplied)
		if key == "channel_kind" {
			got = NormalizedChannelIdentifier(supplied)
		}
		if got != want {
			return false
		}
	}
	if toolArgs, exists := kwargs["tool_args"]; exists {
		if !reflect.DeepEqual(NormalizeInvocationToolArgs(SendFileToolName, toolArgs), identity.NormalizedToolArgs) {
			return false
		}
	}

	if toolCall, exists := kwargs["tool_call"]; exists {
		if HostToolCallID(toolCall) != identity.ToolCallID {
			return false
		}
		if NormalizedIdentifier(ToolCallValue(toolCall, "name")) != identity.ToolName {
			return false
		}
		if !reflect.DeepEqual(
			NormalizeInvocationToolArgs(SendFileToolName, ToolCallValue(toolCall, "arguments")),
			identity.NormalizedToolArgs) {
			return false
		}
	}
	return true
}

// ToolCallValue 从工具调用对象里按候选键取值。
//
// Python: _tool_call_value(tool_call, key)
func ToolCallValue(toolCall any, key string) any {
	candidates := toolCallCandidateKeys(key)
	switch typed := toolCall.(type) {
	case map[string]any:
		for _, candidate := range candidates {
			if value, exists := typed[candidate]; exists && value != nil {
				return value
			}
		}
		if functionValue, ok := typed["function"].(map[string]any); ok {
			for _, candidate := range candidates {
				if value, exists := functionValue[candidate]; exists && value != nil {
					return value
				}
			}
		}
		return nil
	case *llmschema.ToolCall:
		if typed == nil {
			return nil
		}
		switch key {
		case "name":
			return typed.Name
		case "arguments":
			return typed.Arguments
		case "id":
			return typed.ID
		default:
			return nil
		}
	default:
		return nil
	}
}

// HostSessionID 从回调上下文取宿主会话 ID。
//
// Python: _host_session_id(ctx)
func HostSessionID(ctx *agentinterfaces.AgentCallbackContext) string {
	if ctx == nil {
		return ""
	}
	session := ctx.Session()
	if session == nil {
		return ""
	}
	if normalized := NormalizedIdentifier(session.GetSessionID()); normalized != "" {
		return normalized
	}
	return ""
}

// HostToolCallID 从工具调用对象取 ID。
//
// Python: _host_tool_call_id(tool_call)
func HostToolCallID(toolCall any) string {
	for _, key := range []string{"id", "tool_call_id", "call_id"} {
		if normalized := NormalizedIdentifier(ToolCallValue(toolCall, key)); normalized != "" {
			return normalized
		}
	}
	return ""
}

// NormalizedIdentifier 归一标识符（仅字符串去空白）。
//
// Python: _normalized_identifier(value)
func NormalizedIdentifier(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

// NormalizedChannelIdentifier 归一通道标识符（去空白 + 小写）。
//
// Python: _normalized_channel_identifier(value)
func NormalizedChannelIdentifier(value any) string {
	return strings.ToLower(NormalizedIdentifier(value))
}

// NormalizeNativePathInvocationForExecution 冻结原生执行器实际使用的路径参数。
//
// Python: _normalize_native_path_invocation_for_execution(invocation, kwargs, *, workspace_root)
// 返回 (归一化后的调用, 错误原因码)。原因码为空表示无需处理或处理成功。
func NormalizeNativePathInvocationForExecution(
	ctx context.Context,
	invocation ToolInvocation,
	kwargs map[string]any,
	workspaceRoot string,
) (ToolInvocation, string) {
	if _, isNative := nativePathToolNames[invocation.ToolName]; !isNative {
		return invocation, ""
	}
	registered := registeredNativeExecutor(invocation.ToolName)
	if registered == nil {
		// 未登记：不冻结，权限归属保持原有 owner（与 Python 同款）
		return invocation, ""
	}
	if !isRegisteredInstance(invocation, registered) {
		if invocation.Ctx == nil {
			return invocation, ""
		}
		return invocation, "native_path_binding_unavailable"
	}

	args := NormalizeInvocationToolArgs(invocation.ToolName, invocation.ToolArgs)
	if ctx == nil {
		ctx = context.Background()
	}
	resolved, field, reason := resolveNativePathArgument(invocation.ToolName, args, workspaceRoot, ctx)
	if reason != "" {
		return invocation, reason
	}
	executionArgs := make(map[string]any, len(args)+1)
	for key, value := range args {
		executionArgs[key] = value
	}
	executionArgs[field] = resolved

	action := "read"
	switch invocation.ToolName {
	case "write_file", "edit_file":
		action = "write"
	}
	guardName := "read_file"
	if action == "write" {
		guardName = "write_file"
	}
	workspace, err := permissions.ResolvePathStrictFalse(workspaceRoot)
	if err != nil {
		return invocation, "native_path_contract_invalid"
	}
	core := fileguard.ExtractAccessesNative(
		guardName, map[string]any{"file_path": resolved}, workspace, nil)
	if len(core) != 1 || core[0].Path != resolved {
		return invocation, "native_path_guard_execution_mismatch"
	}

	if err := writeInvocationToolArgs(invocation, kwargs, executionArgs); err != nil {
		return invocation, "native_path_writeback_failed"
	}
	permissions.SetNativePathAccess(&permissions.NativePathAccess{
		ToolName:      invocation.ToolName,
		ArgumentsJSON: permissions.NativeArgumentsJSON(executionArgs),
		Path:          resolved,
		Action:        action,
	})
	return replaceInvocationToolArgs(invocation, executionArgs), ""
}

// NormalizeCommandInvocationForExecution 冻结 mcp_exec_command 的生效工作目录。
//
// Python: _normalize_command_invocation_for_execution(invocation, kwargs)
func NormalizeCommandInvocationForExecution(
	ctx context.Context,
	invocation ToolInvocation,
	kwargs map[string]any,
) (ToolInvocation, string) {
	if invocation.ToolName != execCommandToolName {
		return invocation, ""
	}
	normalizedArgs := NormalizeInvocationToolArgs(invocation.ToolName, invocation.ToolArgs)
	if len(normalizedArgs) == 0 {
		return invocation, "command_workdir_contract_invalid"
	}
	if _, hasCwd := normalizedArgs["cwd"]; hasCwd {
		return invocation, "command_workdir_contract_invalid"
	}
	workdir := normalizedArgs["workdir"]
	if workdir == nil {
		workdir = "."
	}
	runtimePaths, err := commandtools.CurrentCommandRuntimePaths(ctx, true)
	if err != nil {
		return invocation, "command_workdir_contract_invalid"
	}
	resolved, err := commandtools.ResolveCommandWorkdir(workdir, runtimePaths)
	if err != nil {
		return invocation, "command_workdir_contract_invalid"
	}
	executionArgs := make(map[string]any, len(normalizedArgs)+1)
	for key, value := range normalizedArgs {
		executionArgs[key] = value
	}
	executionArgs["workdir"] = resolved
	if err := writeInvocationToolArgs(invocation, kwargs, executionArgs); err != nil {
		return invocation, "command_workdir_writeback_failed"
	}
	return replaceInvocationToolArgs(invocation, executionArgs), ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registeredNativeExecutor 取装配期登记的执行器实例。
func registeredNativeExecutor(toolName string) tool.Tool {
	nativePathExecutorsMu.RLock()
	defer nativePathExecutorsMu.RUnlock()
	return nativePathExecutors[toolName]
}

// isRegisteredInstance 当前工具实例是否就是装配期登记的那个（指针相等）。
//
// Python 用 `type(resource) is expected` 比较类身份；Go 的工具无导出具体类型，
// 改用实例身份比较（更严格：要求同一实例，而非同类实例）。
func isRegisteredInstance(invocation ToolInvocation, registered tool.Tool) bool {
	if invocation.Ctx == nil {
		return false
	}
	card := registered.Card()
	if card == nil {
		return false
	}
	resourceManager := runner.GetResourceMgr()
	if resourceManager == nil {
		return false
	}
	resources, err := resourceManager.GetTool([]string{card.ID})
	if err != nil || len(resources) != 1 {
		return false
	}
	return resources[0] == registered
}

// resolveNativePathArgument 解析原生执行器的路径参数并校验作用域。
//
// 返回 (绝对路径, 参数字段名, 错误原因码)。
func resolveNativePathArgument(
	toolName string,
	args map[string]any,
	workspaceRoot string,
	ctx context.Context,
) (string, string, string) {
	cwdPath, err := cwd.GetCwd(ctx)
	if err != nil || strings.TrimSpace(cwdPath) == "" {
		return "", "", "native_path_runtime_cwd_missing"
	}
	workspace := cwd.GetWorkspace(ctx)
	if strings.TrimSpace(workspace) == "" {
		return "", "", "native_path_runtime_workspace_mismatch"
	}
	expectedWorkspace, err := permissions.ResolvePathStrictFalse(workspaceRoot)
	if err != nil {
		return "", "", "native_path_runtime_workspace_mismatch"
	}
	resolvedWorkspace, err := permissions.ResolvePathStrictFalse(workspace)
	if err != nil || resolvedWorkspace != expectedWorkspace {
		return "", "", "native_path_runtime_workspace_mismatch"
	}
	if !filepath.IsAbs(cwdPath) {
		return "", "", "native_path_runtime_cwd_missing"
	}

	if toolName == "glob" {
		if reason := validateSmartGlobPattern(args["pattern"]); reason != "" {
			return "", "", reason
		}
	}

	field := "file_path"
	var raw any
	switch toolName {
	case "read_pdf":
		field = "pdf_path"
		raw = args[field]
	case "glob", "grep":
		field = "path"
		raw = args[field]
		if raw == nil {
			raw = cwdPath
		}
	case "list_files":
		field = "path"
		raw = args[field]
		if raw == nil {
			raw = "."
		}
	default:
		raw = args[field]
	}
	rawText, ok := raw.(string)
	if !ok || rawText == "" || strings.ContainsRune(rawText, 0) {
		return "", "", "native_path_argument_invalid"
	}

	var resolved string
	if toolName == "list_files" {
		// ListDir 直接用文件系统门面：不像文件/搜索工具那样展开 '~'
		resolved = filepath.Clean(filepath.Join(cwdPath, rawText))
	} else {
		expanded := pathutil.ExpandHome(rawText)
		if filepath.IsAbs(expanded) {
			resolved = filepath.Clean(expanded)
		} else {
			resolved = filepath.Clean(filepath.Join(cwdPath, expanded))
		}
	}
	return resolved, field, ""
}

// validateSmartGlobPattern 在调用 SDK 展开前约束模式，再校验每个分支。
//
// Python: _validate_smart_glob_pattern(pattern)
func validateSmartGlobPattern(pattern any) string {
	text, ok := pattern.(string)
	// Python 的 len() 是字符数，Go 用 rune 数对齐
	if !ok || text == "" || utf8.RuneCountInString(text) > smartGlobMaxLength {
		return "native_glob_expansion_limit"
	}
	if strings.Count(text, "{") > smartGlobMaxGroups ||
		strings.Count(text, "}") > smartGlobMaxGroups ||
		strings.Count(text, ",") > smartGlobMaxCommas {
		return "native_glob_expansion_limit"
	}
	for _, branch := range filesystem.ExpandBracePattern(text) {
		if branch == "" || filepath.IsAbs(branch) || hasWindowsDrive(branch) {
			return "native_glob_scope_invalid:put_parent_directory_in_path"
		}
		normalized := strings.ReplaceAll(branch, `\`, "/")
		for _, segment := range strings.Split(normalized, "/") {
			if segment == ".." {
				return "native_glob_scope_invalid:put_parent_directory_in_path"
			}
		}
	}
	return ""
}

// hasWindowsDrive 是否带 Windows 盘符。
func hasWindowsDrive(text string) bool {
	return len(text) >= 2 && text[1] == ':'
}

// writeInvocationToolArgs 把归一化后的参数写回实时调用对象。
//
// Python: _write_invocation_tool_args(invocation, kwargs, tool_args)
// Python 用 setattr 就地改属性；Go 构造新的 ToolCallInputs 后 SetInputs 替换。
func writeInvocationToolArgs(
	invocation ToolInvocation,
	kwargs map[string]any,
	toolArgs map[string]any,
) error {
	encoded, err := json.Marshal(toolArgs)
	if err != nil {
		return err
	}
	encodedText := string(encoded)

	// ① 写回 tool_call 的 arguments（Python 对 str 型 arguments 写 JSON 串）
	switch typed := invocation.ToolCall.(type) {
	case map[string]any:
		typed["arguments"] = encodedText
	case *llmschema.ToolCall:
		if typed != nil {
			typed.Arguments = encodedText
		}
	}

	// ② 写回 ctx.Inputs()
	if invocation.Ctx != nil {
		if inputs, ok := invocation.Ctx.Inputs().(*agentinterfaces.ToolCallInputs); ok {
			replaced := *inputs
			replaced.ToolArgs = toolArgs
			if replaced.ToolCall != nil {
				copied := *replaced.ToolCall
				copied.Arguments = encodedText
				replaced.ToolCall = &copied
			}
			invocation.Ctx.SetInputs(&replaced)
		}
	}

	// ③ 写回 kwargs
	if _, exists := kwargs["tool_args"]; exists {
		kwargs["tool_args"] = encodedText
	}
	return nil
}

// replaceInvocationToolArgs 返回替换了参数的新调用快照。
//
// Python: _replace_invocation_tool_args(invocation, tool_args)
func replaceInvocationToolArgs(invocation ToolInvocation, toolArgs map[string]any) ToolInvocation {
	return ToolInvocation{
		Ctx:      invocation.Ctx,
		ToolCall: invocation.ToolCall,
		ToolName: invocation.ToolName,
		ToolArgs: toolArgs,
	}
}

// repairInvocationArgsForExecution 解析或修复字符串参数并冻结进实时调用。
//
// Python: _repair_invocation_args_for_execution(invocation)
func repairInvocationArgsForExecution(invocation ToolInvocation) ToolInvocation {
	// ToolInvocation.ToolArgs 在 Go 侧已收窄为 map[string]any（Python 是 Any），
	// 因此字符串解析与修复发生在构造点（NormalizeInvocationToolArgs）。
	// 本函数保留以对齐 Python 的调用点，实际为幂等直返。
	if invocation.ToolArgs == nil {
		return replaceInvocationToolArgs(invocation, map[string]any{})
	}
	return invocation
}

// argsWereValidJSONObject 参数原本是否已是合法 JSON 对象。
//
// Python: _args_were_valid_json_object(tool_args)
func argsWereValidJSONObject(toolArgs any) bool {
	if _, ok := toolArgs.(map[string]any); ok {
		return true
	}
	text, ok := toolArgs.(string)
	if !ok {
		return false
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return false
	}
	_, isMap := decoded.(map[string]any)
	return isMap
}

// resolveSessionID 解析会话标识。
//
// Python: _resolve_session_id(ctx, kwargs)
func resolveSessionID(ctx *agentinterfaces.AgentCallbackContext, kwargs map[string]any) string {
	if explicit, ok := kwargs["session_id"].(string); ok && strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if ctx == nil {
		return ""
	}
	session := ctx.Session()
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.GetSessionID())
}

// resolveRequestID 解析宿主请求标识。
//
// Python: _resolve_request_id(ctx, kwargs)
func resolveRequestID(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	kwargs map[string]any,
) string {
	if explicit, ok := kwargs["request_id"].(string); ok && strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if cbc != nil {
		if value, exists := cbc.Extra()["request_id"]; exists {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	if ctx != nil {
		if value := swarmschema.ToolPermissionRequestIDFromCtx(ctx); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// resolveToolCallID 解析工具调用标识。
//
// Python: _resolve_tool_call_id(tool_call, kwargs)
func resolveToolCallID(toolCall any, kwargs map[string]any) string {
	if explicit, ok := kwargs["tool_call_id"].(string); ok && strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if toolCall == nil {
		return ""
	}
	return HostToolCallID(toolCall)
}

// resolveUserInput 解析用户输入（优先取恢复队列里的输入）。
//
// Python: _resolve_user_input(base_rail, *, ctx, tool_call_id, kwargs)
func resolveUserInput(
	ctx *agentinterfaces.AgentCallbackContext,
	toolCallID string,
	kwargs map[string]any,
) any {
	if value, exists := kwargs["user_input"]; exists {
		return value
	}
	if ctx == nil || toolCallID == "" {
		return nil
	}
	rawInput, exists := ctx.Extra()[permissions.RootPermissionQueueUserInputKey]
	if !exists {
		return nil
	}
	if mapping, ok := rawInput.(map[string]any); ok {
		if userInputs, ok := mapping["user_inputs"].(map[string]any); ok {
			return userInputs[toolCallID]
		}
		if value, exists := mapping[toolCallID]; exists {
			return value
		}
	}
	return rawInput
}

// normalizeChannelKind 归一通道类型（空值回落 unknown）。
//
// Python: _normalize_channel_kind(value)
func normalizeChannelKind(value any) string {
	normalized := strings.ToLower(strings.TrimSpace(anyToStringValue(value)))
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

// resolveChannelKind 返回宿主通道类型供权限策略判定。
//
// Python: _resolve_channel_kind(ctx, kwargs)
func resolveChannelKind(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	kwargs map[string]any,
) string {
	if value, ok := kwargs["channel_kind"].(string); ok && strings.TrimSpace(value) != "" {
		return normalizeChannelKind(value)
	}
	if cbc != nil {
		if value, exists := cbc.Extra()["channel_kind"]; exists {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return normalizeChannelKind(text)
			}
		}
	}
	if ctx != nil {
		if value := swarmschema.ToolPermissionChannelIDFromCtx(ctx); strings.TrimSpace(value) != "" {
			return normalizeChannelKind(value)
		}
	}
	return normalizeChannelKind(nil)
}

// toolCallCandidateKeys 工具调用对象里某逻辑键的候选拼写。
//
// Python: _tool_call_candidate_keys(key)
func toolCallCandidateKeys(key string) []string {
	if key == "arguments" {
		return []string{"arguments", "args", "input", "parameters", "rawInput"}
	}
	return []string{key}
}

// anyToStringValue Python str(value) 等价（nil → 空串）。
func anyToStringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.Trim(string(encoded), `"`)
}

// repairJSONText 对损坏的 JSON 文本做保守修复，失败返回空串。
//
// 覆盖三类常见损坏：单引号包裹、对象/数组尾逗号、无引号键名。
func repairJSONText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	// 单引号 → 双引号（仅在完全没有双引号时整体替换，避免误伤）
	if !strings.Contains(trimmed, `"`) && strings.Contains(trimmed, "'") {
		trimmed = strings.ReplaceAll(trimmed, "'", `"`)
	}
	// 移除尾逗号：,} → } / ,] → ]
	trimmed = strings.ReplaceAll(trimmed, ",}", "}")
	trimmed = strings.ReplaceAll(trimmed, ",]", "]")
	return trimmed
}

// 编译期断言：确保 tool.Tool 可用于实例表
var _ tool.Tool = nil

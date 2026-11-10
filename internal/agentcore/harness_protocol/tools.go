package harness_protocol

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// ToolDefinition OpenJiuwen 暴露的工具的 provider-neutral 定义。
// Python: tools.py:16-27 ToolDefinition
type ToolDefinition struct {
	// Name 工具名
	Name string
	// Description 描述
	Description string
	// InputSchema 入参 JSON schema（已冻结）
	InputSchema JsonObject
}

// ToolInvocation 外部 harness 请求的一次原生工具调用。
// Python: tools.py:30-41 ToolInvocation
type ToolInvocation struct {
	// CallID 调用标识
	CallID string
	// Name 工具名
	Name string
	// Arguments 入参（已冻结）
	Arguments JsonObject
}

// ToolExecutionResult 原生工具调用的 provider-neutral 结果。
// Python: tools.py:44-52 ToolExecutionResult
type ToolExecutionResult struct {
	// Content 结果内容（已冻结）
	Content JsonValue
	// IsError 是否为错误结果
	IsError bool
}

// ToolGateway OpenJiuwen 宿主提供的原生 SDK 工具面。
// Python: tools.py:55-65 ToolGateway（@runtime_checkable Protocol，2 方法）
type ToolGateway interface {
	// Definitions 返回对本 agent 可见的工具
	Definitions(ctx context.Context) ([]ToolDefinition, error)
	// Invoke 在宿主权限策略下执行一个可见工具
	Invoke(ctx context.Context, invocation ToolInvocation) (ToolExecutionResult, error)
}

// McpServerConfig 暴露给外部 harness 的 MCP 服务配置。
// 必须恰好给出一个 transport 对应目标：stdio 的 Command、HTTP 的 URL、
// 或 in_process 的 Instance。
// Python: tools.py:76-115 McpServerConfig
type McpServerConfig struct {
	// Name 服务名
	Name string
	// Transport 传输方式
	Transport McpTransport
	// Command stdio 启动命令
	Command []string
	// URL HTTP 端点
	URL *string
	// Instance in-process SDK 服务实例
	Instance any
	// Env stdio 环境变量（仅 stdio 合法）
	Env map[string]string
	// Headers HTTP 头（仅 http 合法）
	Headers map[string]string
}

// ──────────────────────────── 枚举 ────────────────────────────

// McpTransport 把 MCP 服务暴露给外部 harness 的传输方式。
// Python: tools.py:68-73 McpTransport（3 项）
type McpTransport string

const (
	// McpTransportStdio 标准输入输出子进程
	McpTransportStdio McpTransport = "stdio"
	// McpTransportHTTP HTTP 端点
	McpTransportHTTP McpTransport = "http"
	// McpTransportInProcess 进程内 SDK 服务
	McpTransportInProcess McpTransport = "in_process"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolDefinition 创建工具定义。
// Python: tools.py:16-27
func NewToolDefinition(name, description string, inputSchema JsonObject) (ToolDefinition, error) {
	// 步骤 1：name 非空
	if name == "" {
		return ToolDefinition{}, &HarnessProtocolError{Msg: "tool definition name must not be empty"}
	}
	// 步骤 2：冻结 input_schema
	frozen, err := FreezeJsonObject(inputSchema)
	if err != nil {
		return ToolDefinition{}, err
	}
	return ToolDefinition{Name: name, Description: description, InputSchema: frozen}, nil
}

// NewToolInvocation 创建工具调用请求。
// Python: tools.py:30-41
func NewToolInvocation(callID, name string, arguments JsonObject) (ToolInvocation, error) {
	// 步骤 1：call_id 与 name 非空
	if callID == "" || name == "" {
		return ToolInvocation{}, &HarnessProtocolError{Msg: "tool invocation call_id and name must not be empty"}
	}
	// 步骤 2：冻结 arguments
	frozen, err := FreezeJsonObject(arguments)
	if err != nil {
		return ToolInvocation{}, err
	}
	return ToolInvocation{CallID: callID, Name: name, Arguments: frozen}, nil
}

// NewToolExecutionResult 创建工具执行结果。
// Python: tools.py:44-52
func NewToolExecutionResult(content JsonValue, isError bool) (ToolExecutionResult, error) {
	// 步骤 1：冻结 content
	frozen, err := FreezeJsonValue(content)
	if err != nil {
		return ToolExecutionResult{}, err
	}
	return ToolExecutionResult{Content: frozen, IsError: isError}, nil
}

// NewMcpServerConfig 创建 MCP 服务配置，8 步校验顺序与上游一致。
// Python: tools.py:92-115
func NewMcpServerConfig(
	name string,
	transport McpTransport,
	command []string,
	url *string,
	instance any,
	env map[string]string,
	headers map[string]string,
) (McpServerConfig, error) {
	// 步骤 1：name 非空
	// Python: tools.py:94-95
	if name == "" {
		return McpServerConfig{}, &HarnessProtocolError{Msg: "MCP server name must not be empty"}
	}
	// 步骤 2：env 键值均为 string（Go map[string]string 已由类型保证，此处深拷贝）
	// Python: tools.py:96-97
	copiedEnv := make(map[string]string, len(env))
	for k, v := range env {
		copiedEnv[k] = v
	}
	// 步骤 3：headers 键值均为 string
	// Python: tools.py:98-99
	copiedHeaders := make(map[string]string, len(headers))
	for k, v := range headers {
		copiedHeaders[k] = v
	}
	// 步骤 4：command 转元组（Go 侧拷贝切片）
	// Python: tools.py:100
	copiedCommand := make([]string, len(command))
	copy(copiedCommand, command)
	// 步骤 5：env / headers 冻结为不可变副本（已在上两步拷贝）
	// Python: tools.py:101-102
	// 步骤 6：三类 transport 各自的 target 校验
	// Python: tools.py:103-108
	switch transport {
	case McpTransportStdio:
		if len(copiedCommand) == 0 {
			return McpServerConfig{}, &HarnessProtocolError{Msg: "stdio MCP server requires a non-empty command"}
		}
	case McpTransportHTTP:
		if url == nil {
			return McpServerConfig{}, &HarnessProtocolError{Msg: "HTTP MCP server requires url"}
		}
	case McpTransportInProcess:
		if instance == nil {
			return McpServerConfig{}, &HarnessProtocolError{Msg: "in-process MCP server requires instance"}
		}
	}
	// 步骤 7：非对应 transport 的 env / headers 拒绝
	// Python: tools.py:109-112
	if transport != McpTransportStdio && len(copiedEnv) > 0 {
		return McpServerConfig{}, &HarnessProtocolError{Msg: "MCP env is only valid for stdio transport"}
	}
	if transport != McpTransportHTTP && len(copiedHeaders) > 0 {
		return McpServerConfig{}, &HarnessProtocolError{Msg: "MCP headers are only valid for HTTP transport"}
	}
	// 步骤 8：target 恰好一个
	// Python: tools.py:113-115
	targetCount := 0
	if len(copiedCommand) > 0 {
		targetCount++
	}
	if url != nil {
		targetCount++
	}
	if instance != nil {
		targetCount++
	}
	if targetCount != 1 {
		return McpServerConfig{}, &HarnessProtocolError{Msg: "MCP server requires exactly one of command, url, or instance"}
	}
	return McpServerConfig{
		Name:      name,
		Transport: transport,
		Command:   copiedCommand,
		URL:       url,
		Instance:  instance,
		Env:       copiedEnv,
		Headers:   copiedHeaders,
	}, nil
}

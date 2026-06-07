package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// openapiTool OpenAPI 路由对应的可调用工具。
//
// 对应 Python: fastmcp.server.openapi.OpenAPITool
type openapiTool struct {
	// name 工具名称
	name string
	// description 工具描述
	description string
	// method HTTP 方法
	method string
	// path URL 路径模板
	path string
	// parameters 参数引用列表
	parameters openapi3.Parameters
	// requestBody 请求体引用
	requestBody *openapi3.RequestBodyRef
	// baseURL 基础 URL
	baseURL string
	// tags 标签集合
	tags []string
	// inputSchema 输入 JSON Schema
	inputSchema map[string]any
}

// OpenApiClient OpenAPI 规格解析的 MCP 客户端。
//
// 与其他 MCP 客户端不同，OpenApiClient 不使用 MCP 协议，
// 而是解析 OpenAPI 规格文件，将每个 HTTP 路由转换为可调用的 MCP 工具。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/openapi_client.py (OpenApiClient)
type OpenApiClient struct {
	config     *types.McpServerConfig
	serverName string
	// isConnected 是否已连接（即 OpenAPI 规格是否已加载）
	isConnected bool
	// httpClient HTTP 客户端
	httpClient *http.Client
	// baseURL 基础 URL
	baseURL string
	// tools 已解析的工具列表
	tools map[string]*openapiTool
	// toolOrder 工具名称列表（保持插入顺序）
	toolOrder []string
	// usedNames 工具名称去重计数
	usedNames map[string]int
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenApiClient 创建 OpenAPI 客户端。
func NewOpenApiClient(config *types.McpServerConfig) *OpenApiClient {
	return &OpenApiClient{
		config:     config,
		serverName: config.ServerName,
		httpClient: &http.Client{},
		tools:      make(map[string]*openapiTool),
		usedNames:  make(map[string]int),
	}
}

// Connect 加载 OpenAPI 规格文件并解析为 MCP 工具。
//
// 支持逗号分隔的多文件路径（如 "openapi.json,openapi.yaml"）。
// 每个文件可以是 JSON 或 YAML 格式。
//
// 对应 Python: OpenApiClient.connect()
func (c *OpenApiClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isConnected {
		logger.Info(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Msg("OpenAPI 客户端已连接，跳过重复连接")
		return nil
	}

	files := strings.Split(c.config.ServerPath, ",")
	for _, filePath := range files {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" {
			continue
		}

		logger.Info(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Str("file_path", filePath).
			Msg("正在加载 OpenAPI 规格文件")

		spec, err := loadOpenAPISpec(filePath)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("server_name", c.serverName).
				Str("file_path", filePath).
				Err(err).
				Msg("加载 OpenAPI 规格文件失败")
			return exception.BuildError(
				exception.StatusToolOpenapiClientExecutionError,
				exception.WithParam("reason", fmt.Sprintf("加载 OpenAPI 规格文件 %q 失败: %v", filePath, err)),
			)
		}

		// 提取 base URL
		baseURL := extractBaseURL(spec)
		if baseURL != "" {
			c.baseURL = baseURL
		}

		// 解析路由并转换为工具
		if err := c.parseRoutes(spec); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("server_name", c.serverName).
				Err(err).
				Msg("解析 OpenAPI 路由失败")
			return exception.BuildError(
				exception.StatusToolOpenapiClientExecutionError,
				exception.WithParam("reason", fmt.Sprintf("解析 OpenAPI 路由失败: %v", err)),
			)
		}
	}

	c.isConnected = true
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Int("tool_count", len(c.tools)).
		Msg("OpenAPI 客户端连接成功")
	return nil
}

// Disconnect 断开 OpenAPI 客户端连接，清理已解析的工具。
//
// 对应 Python: OpenApiClient.disconnect()
func (c *OpenApiClient) Disconnect(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tools = make(map[string]*openapiTool)
	c.toolOrder = nil
	c.usedNames = make(map[string]int)
	c.isConnected = false

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("OpenAPI 客户端已断开连接")
	return nil
}

// ListTools 列出 OpenAPI 规格解析出的工具。
//
// 对应 Python: OpenApiClient.list_tools()
func (c *OpenApiClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cards := make([]*types.McpToolCard, 0, len(c.toolOrder))
	for _, name := range c.toolOrder {
		tool, ok := c.tools[name]
		if !ok {
			continue
		}
		inputParams := openapiInputSchemaToParams(tool.inputSchema)
		card := types.NewMcpToolCard(
			tool.name,
			tool.description,
			c.serverName,
			inputParams,
			types.WithMcpToolCardServerID(c.config.ServerID),
		)
		cards = append(cards, card)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Int("tool_count", len(cards)).
		Msg("OpenAPI 列出工具成功")
	return cards, nil
}

// CallTool 调用 OpenAPI 工具（执行 HTTP 请求）。
//
// 对应 Python: OpenApiClient.call_tool()
func (c *OpenApiClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	c.mu.Lock()
	tool, ok := c.tools[toolName]
	c.mu.Unlock()

	if !ok {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Msg("OpenAPI 工具不存在")
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("工具 %q 不存在", toolName)),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("tool_name", toolName).
		Str("method", tool.method).
		Str("path", tool.path).
		Msg("OpenAPI 调用工具")

	result, err := c.executeHTTPRequest(ctx, tool, arguments)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Err(err).
			Msg("OpenAPI 工具执行失败")
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("调用工具 %q 失败: %v", toolName, err)),
			exception.WithCause(err),
		)
	}

	return result, nil
}

// GetToolInfo 获取指定工具信息。
//
// 对应 Python: OpenApiClient.get_tool_info()
func (c *OpenApiClient) GetToolInfo(ctx context.Context, toolName string) (*types.McpToolCard, error) {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, card := range tools {
		if card.Name == toolName {
			return card, nil
		}
	}
	return nil, exception.BuildError(
		exception.StatusToolOpenapiClientExecutionError,
		exception.WithParam("reason", fmt.Sprintf("工具 %q 不存在", toolName)),
	)
}

// ListResources OpenAPI 客户端不支持资源，返回空列表。
//
// 对应 Python: OpenApiClient.list_resources() → []
func (c *OpenApiClient) ListResources(_ context.Context) ([]any, error) {
	return []any{}, nil
}

// ReadResource OpenAPI 客户端不支持资源读取，返回 nil。
//
// 对应 Python: OpenApiClient.read_resource() → None
func (c *OpenApiClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}

// Close 关闭客户端（等价于 Disconnect）。
func (c *OpenApiClient) Close() error {
	return c.Disconnect(context.Background())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译期检查：OpenApiClient 实现 McpClient 接口
var _ types.McpClient = (*OpenApiClient)(nil)

// loadOpenAPISpec 从文件加载 OpenAPI 规格定义。
//
// 对应 Python: load_conf()
func loadOpenAPISpec(filePath string) (*openapi3.T, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 检查文件存在性
	info, err := os.Lstat(absPath)
	if err != nil {
		return nil, fmt.Errorf("文件不存在: %s", absPath)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("路径是目录而非文件: %s", absPath)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("不允许符号链接: %s", absPath)
	}

	// 检查文件扩展名
	ext := strings.ToLower(filepath.Ext(absPath))
	if ext != ".json" && ext != ".yaml" && ext != ".yml" {
		return nil, fmt.Errorf("仅支持 .json/.yaml/.yml，当前扩展名: %s", ext)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("解析 OpenAPI 规格失败: %w", err)
	}

	// 校验规格
	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("校验 OpenAPI 规格失败: %w", err)
	}

	return doc, nil
}

// extractBaseURL 从 OpenAPI 规格中提取 base URL。
func extractBaseURL(doc *openapi3.T) string {
	if len(doc.Servers) == 0 {
		return ""
	}
	return doc.Servers[0].URL
}

// parseRoutes 解析 OpenAPI 路由并转换为工具。
func (c *OpenApiClient) parseRoutes(doc *openapi3.T) error {
	pathsMap := doc.Paths.Map()
	// 按照路径排序保证确定性
	pathKeys := doc.Paths.InMatchingOrder()

	for _, path := range pathKeys {
		pathItem := pathsMap[path]
		if pathItem == nil {
			continue
		}

		operations := []struct {
			method string
			op     *openapi3.Operation
		}{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"DELETE", pathItem.Delete},
			{"PATCH", pathItem.Patch},
			{"OPTIONS", pathItem.Options},
			{"HEAD", pathItem.Head},
		}

		for _, op := range operations {
			if op.op == nil {
				continue
			}

			toolName := c.generateToolName(op.op, op.method, path)
			description := generateDescription(op.op, op.method, path)
			inputSchema := buildInputSchema(op.op)

			tool := &openapiTool{
				name:        toolName,
				description: description,
				method:      op.method,
				path:        path,
				parameters:  op.op.Parameters,
				requestBody: op.op.RequestBody,
				baseURL:     c.baseURL,
				tags:        op.op.Tags,
				inputSchema: inputSchema,
			}

			c.tools[toolName] = tool
			c.toolOrder = append(c.toolOrder, toolName)
		}
	}
	return nil
}

// generateToolName 为 OpenAPI 操作生成工具名称。
//
// 对应 Python: OpenApiClient._generate_tool_name()
func (c *OpenApiClient) generateToolName(op *openapi3.Operation, method, path string) string {
	var name string
	if op.OperationID != "" {
		// 取 __ 分隔的第一段
		parts := strings.SplitN(op.OperationID, "__", 2)
		name = parts[0]
	} else if op.Summary != "" {
		name = op.Summary
	} else {
		name = fmt.Sprintf("%s_%s", method, path)
	}

	// 截断到 64 字符
	if len(name) > 64 {
		name = name[:64]
	}

	// 去重
	return c.getUniqueName(name)
}

// getUniqueName 确保工具名称唯一，冲突时追加计数后缀。
//
// 对应 Python: OpenApiClient._get_unique_name()
func (c *OpenApiClient) getUniqueName(name string) string {
	c.usedNames[name]++
	count := c.usedNames[name]

	if count == 1 {
		return name
	}

	newName := fmt.Sprintf("%s_%d", name, count)
	logger.Debug(logger.ComponentAgentCore).
		Str("original_name", name).
		Str("new_name", newName).
		Msg("工具名称冲突，使用新名称")
	return newName
}

// generateDescription 为 OpenAPI 操作生成工具描述。
//
// 对应 Python: format_simple_description()
func generateDescription(op *openapi3.Operation, method, path string) string {
	desc := op.Description
	if desc == "" {
		desc = op.Summary
	}
	if desc == "" {
		desc = fmt.Sprintf("Executes %s %s", method, path)
	}
	return desc
}

// buildInputSchema 从 OpenAPI 操作构建输入 JSON Schema。
func buildInputSchema(op *openapi3.Operation) map[string]any {
	properties := make(map[string]any)
	required := make([]string, 0)

	// 处理路径/查询/请求头参数
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		if param.Schema == nil || param.Schema.Value == nil {
			continue
		}

		prop := buildPropertyFromSchema(param.Schema.Value)
		prop["description"] = param.Description
		prop["in"] = string(param.In)
		properties[param.Name] = prop

		if param.Required {
			required = append(required, param.Name)
		}
	}

	// 处理请求体
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for mediaType, content := range op.RequestBody.Value.Content {
			if content == nil || content.Schema == nil || content.Schema.Value == nil {
				continue
			}
			// 使用 JSON content type
			if mediaType == "application/json" {
				bodyProps := buildPropertiesFromSchema(content.Schema.Value)
				for k, v := range bodyProps {
					properties[k] = v
				}
				if content.Schema.Value.Required != nil {
					required = append(required, content.Schema.Value.Required...)
				}
				break
			}
		}
	}

	if len(properties) == 0 {
		return nil
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// buildPropertyFromSchema 从 OpenAPI Schema 构建 JSON Schema 属性。
func buildPropertyFromSchema(schema *openapi3.Schema) map[string]any {
	prop := map[string]any{
		"type": schema.Type,
	}
	if schema.Description != "" {
		prop["description"] = schema.Description
	}
	if schema.Enum != nil {
		prop["enum"] = schema.Enum
	}
	if schema.Default != nil {
		prop["default"] = schema.Default
	}
	// 处理数组 items
	if schema.Items != nil && schema.Items.Value != nil {
		prop["items"] = buildPropertyFromSchema(schema.Items.Value)
	}
	return prop
}

// buildPropertiesFromSchema 从 OpenAPI Schema 构建属性映射。
func buildPropertiesFromSchema(schema *openapi3.Schema) map[string]any {
	props := make(map[string]any)
	for name, propRef := range schema.Properties {
		if propRef == nil || propRef.Value == nil {
			continue
		}
		props[name] = buildPropertyFromSchema(propRef.Value)
	}
	return props
}

// executeHTTPRequest 执行 HTTP 请求。
func (c *OpenApiClient) executeHTTPRequest(ctx context.Context, tool *openapiTool, arguments map[string]any) (any, error) {
	// 构建完整 URL
	reqURL := c.baseURL + tool.path

	// 替换路径参数并收集查询参数
	queryParams := make(map[string]string)
	for _, paramRef := range tool.parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		param := paramRef.Value
		switch param.In {
		case "path":
			if val, ok := arguments[param.Name]; ok {
				reqURL = strings.ReplaceAll(reqURL, "{"+param.Name+"}", fmt.Sprintf("%v", val))
				delete(arguments, param.Name)
			}
		case "query":
			if val, ok := arguments[param.Name]; ok {
				queryParams[param.Name] = fmt.Sprintf("%v", val)
				delete(arguments, param.Name)
			}
		}
	}

	// 添加查询参数到 URL
	if len(queryParams) > 0 {
		parts := make([]string, 0, len(queryParams))
		for k, v := range queryParams {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + strings.Join(parts, "&")
		} else {
			reqURL += "?" + strings.Join(parts, "&")
		}
	}

	// 构建请求体（剩余参数作为 JSON body）
	var bodyReader io.Reader
	if len(arguments) > 0 {
		bodyBytes, err := json.Marshal(arguments)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = strings.NewReader(string(bodyBytes))
	}

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, tool.method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	// 设置 Content-Type
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// 设置认证头
	for k, v := range c.config.AuthHeaders {
		req.Header.Set(k, v)
	}

	// 执行请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("执行 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 HTTP 响应失败: %w", err)
	}

	// 返回 MCP 兼容格式
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": string(respBody),
			},
		},
		"status_code": resp.StatusCode,
	}, nil
}

// openapiInputSchemaToParams 将 OpenAPI 输入 JSON Schema 转换为参数列表。
func openapiInputSchemaToParams(inputSchema map[string]any) []*commonschema.Param {
	if inputSchema == nil {
		return nil
	}

	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		return nil
	}

	requiredSet := make(map[string]bool)
	if req, ok := inputSchema["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}

	params := make([]*commonschema.Param, 0, len(props))
	for name, prop := range props {
		p := jsonSchemaPropToParam(name, prop, requiredSet[name])
		if p != nil {
			params = append(params, p)
		}
	}
	return params
}

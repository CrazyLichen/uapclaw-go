package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenApiClient 基于 OpenAPI 规格的 MCP 客户端。
//
// 不使用 mcp-go ClientSession，而是基于 kin-openapi 自行实现 OpenAPI→MCP 转换。
// 读取 OpenAPI/YAML 文件，将每个 Operation 转换为 MCP 工具，通过 HTTP 调用。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/openapi_client.py (OpenApiClient)
type OpenApiClient struct {
	config      *types.McpServerConfig
	serverName  string
	httpClient  *http.Client
	tools       map[string]*openAPIToolInfo  // tool_name → 路由+参数信息
	toolCards   []*types.McpToolCard          // Connect 时解析的工具列表
	baseURL     string                        // OpenAPI spec 中的服务器地址
	isConnected bool
}

// openAPIToolInfo OpenAPI 工具路由信息（非导出）。
type openAPIToolInfo struct {
	method      string                   // GET/POST/PUT/DELETE/PATCH
	path        string                   // /api/v1/items
	description string
	parameters  []openapiParameterInfo   // path/query/header/cookie 参数
	requestBody *openapiRequestBodyInfo  // 请求体信息
}

// openapiParameterInfo OpenAPI 参数信息。
type openapiParameterInfo struct {
	name     string
	in       string // path, query, header, cookie
	required bool
	schema   map[string]any
}

// openapiRequestBodyInfo OpenAPI 请求体信息。
type openapiRequestBodyInfo struct {
	contentType string
	schema      map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// usedNames 用于生成唯一的工具名称。
var usedNames map[string]int

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenApiClient 创建 OpenAPI 客户端。
func NewOpenApiClient(config *types.McpServerConfig) *OpenApiClient {
	return &OpenApiClient{
		config:     config,
		serverName: config.ServerName,
		tools:      make(map[string]*openAPIToolInfo),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Compile-time check: OpenApiClient implements McpClient.
var _ types.McpClient = (*OpenApiClient)(nil)

// Connect 读取 OpenAPI 文件并解析工具列表。
func (c *OpenApiClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	usedNames = make(map[string]int)
	c.tools = make(map[string]*openAPIToolInfo)
	c.toolCards = nil

	files := strings.Split(c.config.ServerPath, ",")
	for _, filePath := range files {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" {
			continue
		}

		spec, err := loadOpenAPISpec(filePath)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("server_name", c.serverName).
				Str("file_path", filePath).
				Err(err).
				Msg("OpenAPI 规格文件加载失败")
			return exception.BuildError(
				exception.StatusToolOpenapiClientExecutionError,
				exception.WithParam("reason", fmt.Sprintf("加载 OpenAPI 文件失败: %v", err)),
			)
		}

		// 提取 base URL
		if len(spec.Servers) > 0 && spec.Servers[0].URL != "" {
			c.baseURL = spec.Servers[0].URL
		}

		// 遍历所有路径和操作
		if spec.Paths == nil {
			continue
		}
		for path, pathItem := range spec.Paths.Map() {
			if pathItem == nil {
				continue
			}
			for method, op := range pathItem.Operations() {
				if op == nil {
					continue
				}
				c.registerOperation(method, path, op)
			}
		}
	}

	// 创建 HTTP 客户端
	c.httpClient = &http.Client{}
	c.isConnected = true

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Int("tool_count", len(c.tools)).
		Msg("OpenAPI 客户端连接成功")
	return nil
}

// Disconnect 断开 OpenAPI 客户端连接。
func (c *OpenApiClient) Disconnect(_ context.Context) error {
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
		c.httpClient = nil
	}
	c.isConnected = false
	c.tools = nil
	c.toolCards = nil
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("OpenAPI 客户端已断开连接")
	return nil
}

// ListTools 列出 OpenAPI 解析出的工具列表。
func (c *OpenApiClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	return c.toolCards, nil
}

// CallTool 调用 OpenAPI 工具。
func (c *OpenApiClient) CallTool(_ context.Context, toolName string, arguments map[string]any) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	info, ok := c.tools[toolName]
	if !ok {
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("工具 %q 不存在", toolName)),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("tool_name", toolName).
		Str("method", info.method).
		Str("path", info.path).
		Msg("OpenAPI 调用工具")

	// 构造请求 URL
	reqURL := c.baseURL + info.path
	var bodyReader io.Reader

	// 替换路径参数
	for _, param := range info.parameters {
		if param.in == "path" {
			val, ok := arguments[param.name]
			if ok {
				reqURL = strings.ReplaceAll(reqURL, "{"+param.name+"}", fmt.Sprintf("%v", val))
				delete(arguments, param.name)
			}
		}
	}

	// 拼接查询参数
	var queryParts []string
	for _, param := range info.parameters {
		if param.in == "query" {
			val, ok := arguments[param.name]
			if ok {
				queryParts = append(queryParts, fmt.Sprintf("%s=%v", param.name, val))
				delete(arguments, param.name)
			}
		}
	}
	if len(queryParts) > 0 {
		reqURL += "?" + strings.Join(queryParts, "&")
	}

	// 构造请求体
	if info.requestBody != nil && len(arguments) > 0 {
		bodyBytes, err := json.Marshal(arguments)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("server_name", c.serverName).
				Str("tool_name", toolName).
				Err(err).
				Msg("OpenAPI 请求体序列化失败")
			return nil, exception.BuildError(
				exception.StatusToolOpenapiClientExecutionError,
				exception.WithParam("reason", fmt.Sprintf("序列化请求体失败: %v", err)),
			)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest(info.method, reqURL, bodyReader)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Err(err).
			Msg("OpenAPI 创建 HTTP 请求失败")
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("创建请求失败: %v", err)),
		)
	}

	// 设置请求头
	if info.requestBody != nil {
		req.Header.Set("Content-Type", info.requestBody.contentType)
	}
	for _, param := range info.parameters {
		if param.in == "header" {
			val, ok := arguments[param.name]
			if ok {
				req.Header.Set(param.name, fmt.Sprintf("%v", val))
				delete(arguments, param.name)
			}
		}
	}

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Err(err).
			Msg("OpenAPI HTTP 请求失败")
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("HTTP 请求失败: %v", err)),
		)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Err(err).
			Msg("OpenAPI 读取响应失败")
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("读取响应失败: %v", err)),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("tool_name", toolName).
		Int("status_code", resp.StatusCode).
		Msg("OpenAPI 工具调用完成")

	// 返回 MCP 格式结果
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": string(respBody),
			},
		},
	}, nil
}

// GetToolInfo 获取指定工具信息。
func (c *OpenApiClient) GetToolInfo(_ context.Context, toolName string) (*types.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	for _, card := range c.toolCards {
		if card.Name == toolName {
			return card, nil
		}
	}
	return nil, fmt.Errorf("tool %q not found in openapi server %q", toolName, c.serverName)
}

// ListResources OpenAPI 客户端不支持 MCP 资源。
func (c *OpenApiClient) ListResources(_ context.Context) ([]any, error) {
	return nil, nil
}

// ReadResource OpenAPI 客户端不支持 MCP 资源。
func (c *OpenApiClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}

// Close 关闭客户端（等价于 Disconnect）。
func (c *OpenApiClient) Close() error {
	return c.Disconnect(context.Background())
}

// registerOperation 将 OpenAPI Operation 注册为 MCP 工具。
func (c *OpenApiClient) registerOperation(method, path string, op *openapi3.Operation) {
	// 生成工具名称
	toolName := generateToolName(method, path, op)
	toolName = getUniqueName(toolName)

	// 提取描述
	description := op.Description
	if description == "" {
		description = op.Summary
	}
	if description == "" {
		description = fmt.Sprintf("执行 %s %s", method, path)
	}

	// 解析参数
	var params []openapiParameterInfo
	for _, paramRef := range op.Parameters {
		if paramRef == nil || paramRef.Value == nil {
			continue
		}
		p := paramRef.Value
		var schemaMap map[string]any
		if p.Schema != nil && p.Schema.Value != nil {
			schemaMap = oapiSchemaToMap(p.Schema.Value)
		}
		params = append(params, openapiParameterInfo{
			name:     p.Name,
			in:       p.In,
			required: p.Required,
			schema:   schemaMap,
		})
	}

	// 解析请求体
	var reqBody *openapiRequestBodyInfo
	if op.RequestBody != nil && op.RequestBody.Value != nil {
		for contentType, mediaType := range op.RequestBody.Value.Content {
			if mediaType == nil || mediaType.Schema == nil || mediaType.Schema.Value == nil {
				continue
			}
			reqBody = &openapiRequestBodyInfo{
				contentType: contentType,
				schema:      oapiSchemaToMap(mediaType.Schema.Value),
			}
			break // 使用第一个 content type
		}
	}

	// 构造输入参数列表
	inputParams := buildInputParams(params, reqBody)

	// 注册工具信息
	c.tools[toolName] = &openAPIToolInfo{
		method:      method,
		path:        path,
		description: description,
		parameters:  params,
		requestBody: reqBody,
	}

	// 创建工具卡片
	card := types.NewMcpToolCard(
		toolName,
		description,
		c.serverName,
		inputParams,
		types.WithMcpToolCardServerID(c.config.ServerID),
	)
	c.toolCards = append(c.toolCards, card)
}

// generateToolName 根据操作信息生成工具名称。
func generateToolName(method, path string, op *openapi3.Operation) string {
	// 优先使用 operationId
	if op.OperationID != "" {
		name := strings.SplitN(op.OperationID, "__", 2)[0]
		if len(name) > 64 {
			name = name[:64]
		}
		return name
	}
	// 其次使用 summary
	if op.Summary != "" {
		if len(op.Summary) > 64 {
			return op.Summary[:64]
		}
		return op.Summary
	}
	// 最后使用 method + path
	name := method + "_" + path
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "{", "")
	name = strings.ReplaceAll(name, "}", "")
	name = strings.ReplaceAll(name, "-", "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// getUniqueName 生成唯一的工具名称（避免重名）。
func getUniqueName(name string) string {
	usedNames[name]++
	if usedNames[name] == 1 {
		return name
	}
	newName := fmt.Sprintf("%s_%d", name, usedNames[name])
	logger.Debug(logger.ComponentAgentCore).
		Str("original_name", name).
		Str("new_name", newName).
		Msg("OpenAPI 工具名称冲突，已重命名")
	return newName
}

// buildInputParams 根据 OpenAPI 参数和请求体构造输入参数列表。
func buildInputParams(params []openapiParameterInfo, reqBody *openapiRequestBodyInfo) []*schema.Param {
	var result []*schema.Param

	for _, p := range params {
		paramType := schemaTypeFromOpenAPI(p.schema)
		result = append(result, &schema.Param{
			Name:        p.name,
			Description: fmt.Sprintf("%s 参数（%s）", p.in, p.name),
			Type:        paramType,
			Required:    p.required,
		})
	}

	if reqBody != nil {
		// 将请求体的属性展开为顶层参数
		props, ok := reqBody.schema["properties"].(map[string]any)
		if ok {
			requiredSet := make(map[string]bool)
			if reqArr, ok := reqBody.schema["required"].([]any); ok {
				for _, r := range reqArr {
					if rs, ok := r.(string); ok {
						requiredSet[rs] = true
					}
				}
			}
			for propName, propVal := range props {
				propMap, ok := propVal.(map[string]any)
				if !ok {
					continue
				}
				paramType := schemaTypeFromOpenAPI(propMap)
				desc, _ := propMap["description"].(string)
				result = append(result, &schema.Param{
					Name:        propName,
					Description: desc,
					Type:        paramType,
					Required:    requiredSet[propName],
				})
			}
		} else {
			// 请求体是简单类型，创建单个 body 参数
			result = append(result, &schema.Param{
				Name:        "body",
				Description: "请求体",
				Type:        schemaTypeFromOpenAPI(reqBody.schema),
				Required:    true,
			})
		}
	}

	return result
}

// schemaTypeFromOpenAPI 将 OpenAPI schema 映射为 schema.ParamType。
func schemaTypeFromOpenAPI(s map[string]any) schema.ParamType {
	if s == nil {
		return schema.ParamTypeString
	}
	typeStr, _ := s["type"].(string)
	switch typeStr {
	case "string":
		return schema.ParamTypeString
	case "boolean":
		return schema.ParamTypeBoolean
	case "integer":
		return schema.ParamTypeInteger
	case "number":
		return schema.ParamTypeNumber
	case "array":
		return schema.ParamTypeArray
	case "object":
		return schema.ParamTypeObject
	default:
		return schema.ParamTypeString
	}
}

// oapiSchemaToMap 将 openapi3.Schema 转换为 map[string]any。
func oapiSchemaToMap(s *openapi3.Schema) map[string]any {
	if s == nil {
		return nil
	}
	result := map[string]any{}
	if s.Type != nil {
		// Schema.Type 是 *Types，取第一个类型字符串
		for _, t := range *s.Type {
			result["type"] = t
			break
		}
	}
	if s.Description != "" {
		result["description"] = s.Description
	}
	if s.Format != "" {
		result["format"] = s.Format
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			if v != nil && v.Value != nil {
				props[k] = oapiSchemaToMap(v.Value)
			}
		}
		result["properties"] = props
	}
	if len(s.Required) > 0 {
		result["required"] = s.Required
	}
	if s.Items != nil && s.Items.Value != nil {
		result["items"] = oapiSchemaToMap(s.Items.Value)
	}
	return result
}

// loadOpenAPISpec 从文件加载 OpenAPI 规格。
func loadOpenAPISpec(filePath string) (*openapi3.T, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 检查文件是否存在
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("文件不存在: %s", absPath)
	}

	// 读取文件内容
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 预检查文件格式
	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".json", ".yaml", ".yml":
		// 支持的格式
	default:
		return nil, fmt.Errorf("不支持的文件扩展名 %q，仅支持 .json/.yaml/.yml", ext)
	}

	// 验证文件内容为有效的 dict
	var raw map[string]any
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("JSON 解析失败: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("YAML 解析失败: %w", err)
		}
	}
	if raw == nil {
		return nil, fmt.Errorf("文件内容为空或不是有效的字典格式")
	}

	// 使用 kin-openapi 加载并验证
	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("OpenAPI 加载失败: %w", err)
	}

	// 验证规格（仅警告，不阻断）
	ctx := context.Background()
	if err := spec.Validate(ctx); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("file_path", filePath).
			Err(err).
			Msg("OpenAPI 规格验证有警告，继续使用")
	}

	return spec, nil
}

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	tools       map[string]*openAPIToolInfo // tool_name → 路由+参数信息
	toolCards   []*types.McpToolCard        // Connect 时解析的工具列表
	baseURL     string                      // OpenAPI spec 中的服务器地址
	isConnected bool
	usedNames   map[string]int // 用于生成唯一的工具名称（实例属性，避免并发问题）
}

// openAPIToolInfo OpenAPI 工具路由信息（非导出）。
type openAPIToolInfo struct {
	method       string // GET/POST/PUT/DELETE/PATCH
	path         string // /api/v1/items
	description  string
	parameters   []openapiParameterInfo  // path/query/header/cookie 参数
	requestBody  *openapiRequestBodyInfo // 请求体信息
	outputSchema map[string]any          // 输出 JSON Schema（从 responses 提取）
}

// openapiParameterInfo OpenAPI 参数信息。
type openapiParameterInfo struct {
	name        string
	in          string // path, query, header, cookie
	required    bool
	schema      map[string]any
	description string
	style       string // OpenAPI style 属性（如 "deepObject"）
	explode     *bool  // OpenAPI explode 属性
}

// openapiRequestBodyInfo OpenAPI 请求体信息。
type openapiRequestBodyInfo struct {
	contentType string
	schema      map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期检查：OpenApiClient 实现 McpClient 接口
var _ types.McpClient = (*OpenApiClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenApiClient 创建 OpenAPI 客户端。
func NewOpenApiClient(config *types.McpServerConfig) *OpenApiClient {
	return &OpenApiClient{
		config:     config,
		serverName: config.ServerName,
		tools:      make(map[string]*openAPIToolInfo),
		usedNames:  make(map[string]int),
	}
}

// Connect 读取 OpenAPI 文件并解析工具列表。
func (c *OpenApiClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
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
				c.registerOperation(method, path, op, spec.Components, spec.OpenAPI)
			}
		}
	}

	// 创建 HTTP 客户端（默认 60s 超时 + 环境代理 + 禁止自动重定向）
	c.httpClient = &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
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

	// 使用 Schema 驱动的请求构建
	req, err := buildRequestFromSchema(info.method, c.baseURL, info.path, info.parameters, info.requestBody, arguments)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Err(err).
			Msg("OpenAPI 请求构建失败")
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("构建请求失败: %v", err)),
		)
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
	defer func() { _ = resp.Body.Close() }()

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

	// HTTP 错误处理：4xx/5xx 返回结构化错误
	if resp.StatusCode >= 400 {
		// 截取响应体片段，避免过长
		snippet := string(respBody)
		if len(snippet) > 500 {
			snippet = snippet[:500] + "...(truncated)"
		}
		return nil, exception.BuildError(
			exception.StatusToolOpenapiClientExecutionError,
			exception.WithParam("reason", fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)),
			exception.WithParam("status_code", fmt.Sprintf("%d", resp.StatusCode)),
			exception.WithParam("response_body", snippet),
		)
	}

	// 处理 output schema 包装
	textContent := string(respBody)
	var structuredContent map[string]any
	if info.outputSchema != nil {
		if wrapResult, _ := info.outputSchema["x-fastmcp-wrap-result"].(bool); wrapResult {
			// 非 object 类型的响应：
			// - textContent 保持原始值（不双重编码）
			// - structuredContent 放包装后的结构化数据
			//
			// 与 Python 的差异：Python 将 wrapped 结果作为 structured_content 返回，
			// content 仍是原始文本；Go 先 wrap 为 {"result": ...} 再序列化放入 text。
			// 当前保留 Go 行为，后续如需对齐 Python 可调整。
			var parsed any
			if json.Unmarshal(respBody, &parsed) == nil {
				structuredContent = map[string]any{"result": parsed}
				// textContent 保持原始响应文本，不包装
			}
		} else {
			// object 类型：尝试作为 structuredContent
			var parsed any
			if json.Unmarshal(respBody, &parsed) == nil {
				if m, ok := parsed.(map[string]any); ok {
					structuredContent = m
				}
			}
		}
	}

	// 返回 MCP 格式结果
	result := map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": textContent,
			},
		},
	}
	if structuredContent != nil {
		result["structuredContent"] = structuredContent
	}
	return result, nil
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerOperation 将 OpenAPI Operation 注册为 MCP 工具。
func (c *OpenApiClient) registerOperation(method, path string, op *openapi3.Operation, components *openapi3.Components, openAPIVersion string) {
	// 生成工具名称
	toolName := generateToolName(method, path, op)
	toolName = c.getUniqueName(toolName)

	// 提取基础描述
	baseDesc := op.Description
	if baseDesc == "" {
		baseDesc = op.Summary
	}
	if baseDesc == "" {
		baseDesc = fmt.Sprintf("执行 %s %s", method, path)
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
			name:        p.Name,
			in:          p.In,
			required:    p.Required,
			schema:      schemaMap,
			description: p.Description,
			style:       p.Style,
			explode:     p.Explode,
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

	// 使用 formatSimpleDescription 增强描述
	description := formatSimpleDescription(baseDesc, params, reqBody)

	// 提取输出 Schema（含 $defs 收集）
	var schemaDefinitions map[string]map[string]any
	if components != nil && len(components.Schemas) > 0 {
		schemaDefinitions = make(map[string]map[string]any, len(components.Schemas))
		for name, schemaRef := range components.Schemas {
			if schemaRef != nil && schemaRef.Value != nil {
				schemaDefinitions[name] = oapiSchemaToMap(schemaRef.Value)
			}
		}
	}
	outputSchema := extractOutputSchema(op, schemaDefinitions, openAPIVersion)

	// 构造输入参数列表
	inputParams := buildInputParams(params, reqBody)

	// 注册工具信息
	c.tools[toolName] = &openAPIToolInfo{
		method:       method,
		path:         path,
		description:  description,
		parameters:   params,
		requestBody:  reqBody,
		outputSchema: outputSchema,
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
func (c *OpenApiClient) getUniqueName(name string) string {
	c.usedNames[name]++
	if c.usedNames[name] == 1 {
		return name
	}
	newName := fmt.Sprintf("%s_%d", name, c.usedNames[name])
	logger.Debug(logger.ComponentAgentCore).
		Str("original_name", name).
		Str("new_name", newName).
		Msg("OpenAPI 工具名称冲突，已重命名")
	return newName
}

// extractOutputSchema 从 OpenAPI Operation 的响应中提取输出 JSON Schema。
//
// 对照 Python: fastmcp.utilities.openapi.extract_output_schema_from_responses
// 逻辑：按 200→201→202→204→任意2xx 优先级查找成功响应，
// 从中选择 JSON 兼容的 Content-Type，用 oapiSchemaToMap 转换。
// 如果 schema type 不是 object，则包装为 MCP 要求的 object 格式。
// 收集 components/schemas 中引用到的定义，附加到输出的 $defs 字段。
func extractOutputSchema(op *openapi3.Operation, schemaDefinitions map[string]map[string]any, openAPIVersion string) map[string]any {
	if op == nil || op.Responses == nil {
		return nil
	}

	// 按优先级查找成功响应
	successCodes := []int{200, 201, 202, 204}
	var resp *openapi3.Response
	for _, code := range successCodes {
		if ref := op.Responses.Status(code); ref != nil && ref.Value != nil {
			resp = ref.Value
			break
		}
	}
	// 兜底：查找任意 2xx 响应
	if resp == nil {
		for _, key := range op.Responses.Keys() {
			if len(key) == 3 && key[0] == '2' {
				if ref := op.Responses.Value(key); ref != nil && ref.Value != nil {
					resp = ref.Value
					break
				}
			}
		}
	}
	if resp == nil {
		return nil
	}

	// 选择 JSON 兼容的 Content-Type
	jsonTypes := []string{
		"application/json",
		"application/vnd.api+json",
		"application/hal+json",
		"application/ld+json",
		"text/json",
	}
	var oapiSchema *openapi3.Schema
	for _, ct := range jsonTypes {
		if mediaType, ok := resp.Content[ct]; ok && mediaType != nil && mediaType.Schema != nil && mediaType.Schema.Value != nil {
			oapiSchema = mediaType.Schema.Value
			break
		}
	}
	// 兜底：取第一个可用的 Content-Type
	if oapiSchema == nil {
		for _, mediaType := range resp.Content {
			if mediaType != nil && mediaType.Schema != nil && mediaType.Schema.Value != nil {
				oapiSchema = mediaType.Schema.Value
				break
			}
		}
	}
	if oapiSchema == nil {
		return nil
	}

	// 转换为 map
	outputSchema := oapiSchemaToMap(oapiSchema)
	if outputSchema == nil {
		return nil
	}

	// OpenAPI → JSON Schema 转换（处理 nullable/oneOf 等 OpenAPI 特有构造）
	if openAPIVersion != "" && strings.HasPrefix(openAPIVersion, "3") {
		outputSchema = convertOpenAPISchemaToJSONSchema(outputSchema, openAPIVersion)
	}

	// 处理 $ref：将 #/components/schemas/ 替换为 #/$defs/
	replaceSchemaRefs(outputSchema)

	// 顶层 $ref 内联展开：如果整个 schema 就是一个 $ref，且 $defs 中有定义，则展开
	if refPath, ok := outputSchema["$ref"].(string); ok && schemaDefinitions != nil {
		if strings.HasPrefix(refPath, "#/$defs/") {
			schemaName := strings.TrimPrefix(refPath, "#/$defs/")
			if def, exists := schemaDefinitions[schemaName]; exists {
				outputSchema = deepCopyMap(def)
				replaceSchemaRefs(outputSchema)
			}
		}
	}

	// 如果 schema type 不是 object，包装为 MCP 要求的 object 格式
	typeVal := outputSchema["type"]
	if typeVal != "object" {
		wrapped := map[string]any{
			"type":                  "object",
			"properties":            map[string]any{"result": outputSchema},
			"required":              []string{"result"},
			"x-fastmcp-wrap-result": true,
		}
		outputSchema = wrapped
	}

	// 附加 $defs：只收集 outputSchema 中被 $ref 引用到的 schema 定义
	referencedDefs := make(map[string]bool)
	collectReferencedDefs(outputSchema, referencedDefs)

	// 递归展开：被引用的定义内部可能引用其他定义，需要从 schemaDefinitions 中查找并补充
	// 使用迭代方式直到没有新增引用
	prevCount := 0
	for len(referencedDefs) > prevCount {
		prevCount = len(referencedDefs)
		for name := range referencedDefs {
			if def, exists := schemaDefinitions[name]; exists {
				collectReferencedDefs(def, referencedDefs)
			}
		}
	}

	if len(referencedDefs) > 0 {
		defs := make(map[string]any, len(referencedDefs))
		for name := range referencedDefs {
			if def, exists := schemaDefinitions[name]; exists {
				copied := deepCopyMap(def)
				replaceSchemaRefs(copied)
				// OpenAPI → JSON Schema 转换
				if openAPIVersion != "" && strings.HasPrefix(openAPIVersion, "3") {
					copied = convertOpenAPISchemaToJSONSchema(copied, openAPIVersion)
				}
				defs[name] = copied
			}
		}
		outputSchema["$defs"] = defs
	}

	return outputSchema
}

// collectReferencedDefs 递归收集 schema 中所有被 $ref 引用的定义名称。
//
// 扫描 map 中所有 "$ref" 字段，提取 "#/$defs/Xxx" 格式的定义名称，
// 并递归处理嵌套的 map 和 slice 中的引用。
func collectReferencedDefs(m map[string]any, collected map[string]bool) {
	for key, val := range m {
		if key == "$ref" {
			if refStr, ok := val.(string); ok && strings.HasPrefix(refStr, "#/$defs/") {
				defName := strings.TrimPrefix(refStr, "#/$defs/")
				collected[defName] = true
			}
		} else if nested, ok := val.(map[string]any); ok {
			collectReferencedDefs(nested, collected)
		} else if arr, ok := val.([]any); ok {
			for _, item := range arr {
				if nested, ok := item.(map[string]any); ok {
					collectReferencedDefs(nested, collected)
				}
			}
		}
	}
}

// replaceSchemaRefs 递归替换 map 中的 $ref 引用。
//
// 将 #/components/schemas/Xxx 替换为 #/$defs/Xxx（JSON Schema 格式）。
// 处理 properties、items、anyOf/allOf/oneOf、additionalProperties 中的嵌套 $ref。
// additionalProperties 为 map[string]any 时由通用 map 递归分支处理；
// 为 bool（true）时无需替换，直接跳过。
func replaceSchemaRefs(m map[string]any) {
	for key, val := range m {
		if key == "$ref" {
			if refStr, ok := val.(string); ok && strings.HasPrefix(refStr, "#/components/schemas/") {
				m[key] = "#/$defs/" + strings.TrimPrefix(refStr, "#/components/schemas/")
			}
		} else if nested, ok := val.(map[string]any); ok {
			replaceSchemaRefs(nested)
		} else if arr, ok := val.([]any); ok {
			for _, item := range arr {
				if nested, ok := item.(map[string]any); ok {
					replaceSchemaRefs(nested)
				}
			}
		}
	}
}

// deepCopyMap 深拷贝 map[string]any，避免共享引用导致副作用。
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice 深拷贝 []any。
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// formatSimpleDescription 为 OpenAPI 工具生成增强描述。
//
// 对照 Python: fastmcp.utilities.openapi.format_simple_description
// 极简化策略：只追加有 description 的 path 参数信息。
// query/body 信息已包含在 inputSchema 中，description 不需要重复。
func formatSimpleDescription(baseDesc string, params []openapiParameterInfo, _ *openapiRequestBodyInfo) string {
	descParts := []string{baseDesc}

	// 只追加有 description 的 path 参数信息
	var pathParams []openapiParameterInfo
	for _, p := range params {
		if p.in == "path" && p.description != "" {
			pathParams = append(pathParams, p)
		}
	}
	if len(pathParams) > 0 {
		descParts = append(descParts, "\n\n**Path Parameters:**")
		for _, p := range pathParams {
			descParts = append(descParts, fmt.Sprintf("\n- **%s**: %s", p.name, p.description))
		}
	}

	return strings.Join(descParts, "")
}

// buildRequestFromSchema 根据 OpenAPI Schema 构建 HTTP 请求。
//
// 对照 Python: fastmcp.utilities.openapi.director.RequestDirector.build
// 支持 allOf 合并、oneOf/anyOf 取首项、嵌套对象构建、数组查询参数展开。
func buildRequestFromSchema(
	method, baseURL, pathTemplate string,
	params []openapiParameterInfo,
	reqBody *openapiRequestBodyInfo,
	arguments map[string]any,
) (*http.Request, error) {
	// 参数分发：根据 openapiParameterInfo.in 将 arguments 分发到各位置
	pathParams := make(map[string]any)
	queryParams := make(map[string]any)
	headerParams := make(map[string]any)
	consumedKeys := make(map[string]bool)

	for _, p := range params {
		val, ok := arguments[p.name]
		if !ok {
			continue
		}
		consumedKeys[p.name] = true
		switch p.in {
		case "path":
			pathParams[p.name] = val
		case "query":
			// 处理 deepObject 风格：param[key]=value
			if p.style == "deepObject" {
				if obj, ok := val.(map[string]any); ok {
					deepParams := formatDeepObjectParameter(obj, p.name)
					for k, v := range deepParams {
						queryParams[k] = v
					}
				}
			} else {
				queryParams[p.name] = val
			}
		case "header":
			headerParams[p.name] = val
		}
	}

	// 剩余未匹配参数视为 body 属性
	// 对于带 __body 后缀的参数名，去掉后缀后放入 body
	bodyArgs := make(map[string]any)
	for k, v := range arguments {
		if consumedKeys[k] {
			continue
		}
		if strings.HasSuffix(k, "__body") {
			// 去掉 __body 后缀，还原为原始属性名
			originalName := strings.TrimSuffix(k, "__body")
			bodyArgs[originalName] = v
		} else {
			bodyArgs[k] = v
		}
	}

	// 构建请求 URL
	reqURL := baseURL + pathTemplate
	for k, v := range pathParams {
		reqURL = strings.ReplaceAll(reqURL, "{"+k+"}", url.PathEscape(fmt.Sprintf("%v", v)))
	}

	// 拼接查询参数（支持数组展开）
	var queryParts []string
	for k, v := range queryParams {
		switch val := v.(type) {
		case []any:
			// 数组展开为 ?key=val1&key=val2（explode=true 默认行为）
			for _, item := range val {
				queryParts = append(queryParts, fmt.Sprintf("%s=%v", k, item))
			}
		default:
			queryParts = append(queryParts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if len(queryParts) > 0 {
		// 确保查询参数顺序稳定
		sort.Strings(queryParts)
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + strings.Join(queryParts, "&")
		} else {
			reqURL += "?" + strings.Join(queryParts, "&")
		}
	}

	// 构建请求体
	var bodyReader io.Reader
	if reqBody != nil && len(bodyArgs) > 0 {
		resolvedBody := resolveRequestBody(bodyArgs, reqBody.schema)
		bodyBytes, err := json.Marshal(resolvedBody)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// 创建 HTTP 请求
	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	if reqBody != nil && bodyReader != nil {
		req.Header.Set("Content-Type", reqBody.contentType)
	}
	for k, v := range headerParams {
		req.Header.Set(k, fmt.Sprintf("%v", v))
	}

	return req, nil
}

// resolveRequestBody 根据请求体 Schema 解析并构建请求体对象。
//
// 支持 allOf 合并、oneOf/anyOf 取首项、嵌套对象递归构建。
func resolveRequestBody(bodyArgs map[string]any, schemaMap map[string]any) map[string]any {
	if schemaMap == nil {
		return bodyArgs
	}

	// 处理 allOf：合并所有子 schema 的属性
	if allOf, ok := schemaMap["allOf"].([]map[string]any); ok && len(allOf) > 0 {
		merged := make(map[string]any)
		for _, subSchema := range allOf {
			subResult := resolveRequestBody(bodyArgs, subSchema)
			for k, v := range subResult {
				merged[k] = v
			}
		}
		return merged
	}
	// 处理 allOf（[]any 格式，来自 oapiSchemaToMap）
	if allOfAny, ok := schemaMap["allOf"].([]any); ok && len(allOfAny) > 0 {
		merged := make(map[string]any)
		for _, item := range allOfAny {
			if subSchema, ok := item.(map[string]any); ok {
				subResult := resolveRequestBody(bodyArgs, subSchema)
				for k, v := range subResult {
					merged[k] = v
				}
			}
		}
		return merged
	}

	// 处理 oneOf：取第一个子 schema 作为模板
	if oneOfAny, ok := schemaMap["oneOf"].([]any); ok && len(oneOfAny) > 0 {
		if first, ok := oneOfAny[0].(map[string]any); ok {
			return resolveRequestBody(bodyArgs, first)
		}
	}

	// 处理 anyOf：取第一个子 schema 作为模板
	if anyOfAny, ok := schemaMap["anyOf"].([]any); ok && len(anyOfAny) > 0 {
		if first, ok := anyOfAny[0].(map[string]any); ok {
			return resolveRequestBody(bodyArgs, first)
		}
	}

	// 处理 object 类型：根据 properties 构建嵌套结构
	if props, ok := schemaMap["properties"].(map[string]any); ok {
		result := make(map[string]any)
		for propName, propVal := range props {
			if val, exists := bodyArgs[propName]; exists {
				if propMap, ok := propVal.(map[string]any); ok && propMap["type"] == "object" {
					// 嵌套对象：递归构建
					if nested, ok := val.(map[string]any); ok {
						result[propName] = resolveRequestBody(nested, propMap)
					} else {
						result[propName] = val
					}
				} else {
					result[propName] = val
				}
			}
		}
		// 包含 bodyArgs 中不在 properties 中的额外字段
		for k, v := range bodyArgs {
			if _, exists := props[k]; !exists {
				result[k] = v
			}
		}
		return result
	}

	// 简单扁平 object：直接使用 bodyArgs
	return bodyArgs
}

// buildInputParams 根据 OpenAPI 参数和请求体构造输入参数列表。
//
// 对照 Python: fastmcp._combine_schemas_and_map_params
// 当 path/query/header 参数与 body 属性同名时，加 __location 后缀消除歧义。
// 使用 oapiMapToParam 递归转换，保留完整的 JSON Schema 信息（nullable/约束/嵌套/anyOf/allOf/oneOf）。
func buildInputParams(params []openapiParameterInfo, reqBody *openapiRequestBodyInfo) []*schema.Param {
	var result []*schema.Param

	// 收集 path/query/header 参数名，用于冲突检测
	locationParamNames := make(map[string]string) // 名称 → 位置
	for _, p := range params {
		if p.in == "path" || p.in == "query" || p.in == "header" {
			locationParamNames[p.name] = p.in
		}
		param := oapiMapToParam(p.name, p.schema, p.required)
		// 补充位置描述（OpenAPI 参数可能没有 description）
		if param.Description == "" {
			param.Description = fmt.Sprintf("%s 参数（%s）", p.in, p.name)
		}
		result = append(result, param)
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
				// 检查是否与 path/query/header 参数同名，同名则加 __body 后缀
				name := propName
				isConflict := false
				if _, exists := locationParamNames[propName]; exists {
					name = propName + "__body"
					isConflict = true
				}
				param := oapiMapToParam(name, propMap, requiredSet[propName])
				// 冲突参数补充描述
				if isConflict && param.Description == "" {
					param.Description = fmt.Sprintf("请求体属性 %s（与 %s 参数同名，加后缀区分）", propName, locationParamNames[propName])
				}
				result = append(result, param)
			}
		} else {
			// 请求体是简单类型或 $ref，创建单个 body 参数
			param := oapiMapToParam("body", reqBody.schema, true)
			if param.Description == "" {
				param.Description = "请求体"
			}
			result = append(result, param)
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
	return schemaTypeFromTypeStr(typeStr)
}

// schemaTypeFromTypeStr 将类型字符串映射为 schema.ParamType。
func schemaTypeFromTypeStr(typeStr string) schema.ParamType {
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

// oapiMapToParam 递归地将 OpenAPI schema map 转换为 *schema.Param。
//
// 保留所有 JSON Schema 标准字段（nullable/enum/约束/嵌套/anyOf/allOf/oneOf），
// 丢弃 OpenAPI 特有字段（discriminator/readOnly/writeOnly/xml/externalDocs/deprecated）。
func oapiMapToParam(name string, schemaMap map[string]any, required bool) *schema.Param {
	if schemaMap == nil {
		return &schema.Param{Name: name, Type: schema.ParamTypeString, Required: required}
	}

	p := &schema.Param{
		Name:     name,
		Required: required,
	}

	// 解析 type：支持字符串 "string" 和数组 ["string", "null"] 两种格式
	// oapiSchemaToMap 在 Nullable=true 时输出 type 数组，原始 schema 中 type 为字符串
	switch t := schemaMap["type"].(type) {
	case string:
		p.Type = schemaTypeFromTypeStr(t)
	case []string:
		// Nullable 展开：["string", "null"] → ParamTypeString + Nullable=true
		for _, ts := range t {
			if ts != "null" {
				p.Type = schemaTypeFromTypeStr(ts)
			}
		}
		for _, ts := range t {
			if ts == "null" {
				p.Nullable = true
				break
			}
		}
	case []any:
		// []any 格式的 type 数组
		for _, item := range t {
			if ts, ok := item.(string); ok && ts != "null" {
				p.Type = schemaTypeFromTypeStr(ts)
			}
		}
		for _, item := range t {
			if ts, ok := item.(string); ok && ts == "null" {
				p.Nullable = true
				break
			}
		}
	default:
		p.Type = schema.ParamTypeString
	}

	// 基础字段
	p.Description, _ = schemaMap["description"].(string)
	if defVal, ok := schemaMap["default"]; ok {
		p.Default = defVal
	}

	// Nullable（OpenAPI 3.0 原始格式，oapiSchemaToMap 不会输出此字段，但原始 schema 可能含此字段）
	if nullable, _ := schemaMap["nullable"].(bool); nullable {
		p.Nullable = true
	}

	// 枚举
	if enumVals, ok := schemaMap["enum"].([]any); ok && len(enumVals) > 0 {
		p.Enum = enumVals
	}

	// 约束字段
	if v, ok := schemaMap["minLength"].(float64); ok && v > 0 {
		p.MinLength = int(v)
	}
	if v, ok := schemaMap["maxLength"].(float64); ok && v > 0 {
		p.MaxLength = int(v)
	}
	if v, ok := schemaMap["pattern"].(string); ok {
		p.Pattern = v
	}
	if v, ok := schemaMap["minimum"].(float64); ok {
		p.Minimum = v
	}
	if v, ok := schemaMap["maximum"].(float64); ok {
		p.Maximum = v
	}
	if v, ok := schemaMap["format"].(string); ok {
		p.Format = v
	}

	// 嵌套：Array → Items 递归
	if p.Type == schema.ParamTypeArray {
		if itemsMap, ok := schemaMap["items"].(map[string]any); ok {
			p.Items = oapiMapToParam("", itemsMap, false)
		}
	}

	// 嵌套：Object → Properties 递归
	if p.Type == schema.ParamTypeObject {
		if props, ok := schemaMap["properties"].(map[string]any); ok {
			// 收集 required 列表
			requiredSet := make(map[string]bool)
			if reqArr, ok := schemaMap["required"].([]any); ok {
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
				p.Properties = append(p.Properties, oapiMapToParam(propName, propMap, requiredSet[propName]))
			}
		}
	}

	// 组合 Schema（JSON Schema 标准关键字）
	if anyOfArr, ok := schemaMap["anyOf"].([]any); ok {
		for _, item := range anyOfArr {
			if m, ok := item.(map[string]any); ok {
				p.AnyOf = append(p.AnyOf, oapiMapToParam("", m, false))
			}
		}
	}
	if allOfArr, ok := schemaMap["allOf"].([]any); ok {
		for _, item := range allOfArr {
			if m, ok := item.(map[string]any); ok {
				p.AllOf = append(p.AllOf, oapiMapToParam("", m, false))
			}
		}
	}
	if oneOfArr, ok := schemaMap["oneOf"].([]any); ok {
		for _, item := range oneOfArr {
			if m, ok := item.(map[string]any); ok {
				p.OneOf = append(p.OneOf, oapiMapToParam("", m, false))
			}
		}
	}

	// OpenAPI 特有字段（discriminator/readOnly/writeOnly/xml/externalDocs/deprecated）
	// 不属于 JSON Schema 标准，直接丢弃，不需要存入 Param

	return p
}

// oapiSchemaToMap 将 openapi3.Schema 转换为 map[string]any。
//
// 支持组合 Schema（allOf/oneOf/anyOf）和 Nullable 字段转换。
func oapiSchemaToMap(s *openapi3.Schema) map[string]any {
	if s == nil {
		return nil
	}
	result := map[string]any{}

	// 处理 Nullable：OpenAPI 3.0 nullable=true → JSON Schema type 含 "null"
	if s.Nullable {
		if s.Type != nil && len(*s.Type) > 0 {
			types := make([]string, 0, 2)
			for _, t := range *s.Type {
				types = append(types, t)
			}
			types = append(types, "null")
			result["type"] = types
		}
	} else if s.Type != nil {
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
	if s.Default != nil {
		result["default"] = s.Default
	}
	if len(s.Enum) > 0 {
		result["enum"] = s.Enum
	}
	// 提取约束字段
	if s.MinLength > 0 {
		result["minLength"] = s.MinLength
	}
	if s.MaxLength != nil {
		result["maxLength"] = *s.MaxLength
	}
	if s.Pattern != "" {
		result["pattern"] = s.Pattern
	}
	if s.Min != nil {
		result["minimum"] = *s.Min
	}
	if s.Max != nil {
		result["maximum"] = *s.Max
	}
	if s.ExclusiveMin.IsSet() {
		result["exclusiveMinimum"] = true
	}
	if s.ExclusiveMax.IsSet() {
		result["exclusiveMaximum"] = true
	}
	if s.MinItems > 0 {
		result["minItems"] = s.MinItems
	}
	if s.MaxItems != nil {
		result["maxItems"] = *s.MaxItems
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

	// 组合 Schema：allOf/oneOf/anyOf
	if len(s.AllOf) > 0 {
		allOf := make([]map[string]any, 0, len(s.AllOf))
		for _, ref := range s.AllOf {
			if ref != nil && ref.Value != nil {
				allOf = append(allOf, oapiSchemaToMap(ref.Value))
			}
		}
		result["allOf"] = allOf
	}
	if len(s.OneOf) > 0 {
		oneOf := make([]map[string]any, 0, len(s.OneOf))
		for _, ref := range s.OneOf {
			if ref != nil && ref.Value != nil {
				oneOf = append(oneOf, oapiSchemaToMap(ref.Value))
			}
		}
		result["oneOf"] = oneOf
	}
	if len(s.AnyOf) > 0 {
		anyOf := make([]map[string]any, 0, len(s.AnyOf))
		for _, ref := range s.AnyOf {
			if ref != nil && ref.Value != nil {
				anyOf = append(anyOf, oapiSchemaToMap(ref.Value))
			}
		}
		result["anyOf"] = anyOf
	}

	return result
}

// loadOpenAPISpec 从文件加载 OpenAPI 规格。
func loadOpenAPISpec(filePath string) (*openapi3.T, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 检查符号链接（安全防护，对齐 Python: path.is_symlink() 检查）
	info, err := os.Lstat(absPath)
	if err != nil {
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("拒绝加载符号链接文件: %s", absPath)
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

// convertOpenAPISchemaToJSONSchema 将 OpenAPI Schema 转换为 JSON Schema 格式。
//
// 对应 Python: fastmcp.utilities.openapi.json_schema_converter.convert_openapi_schema_to_json_schema
//
// 主要转换：
//   - nullable=true → type 数组 ["string", "null"]
//   - oneOf → anyOf（JSON Schema 语义更宽松）
//   - 移除 OpenAPI 特有字段：discriminator, readOnly, writeOnly, xml, externalDocs, deprecated
//   - 递归处理嵌套 schema
func convertOpenAPISchemaToJSONSchema(schema map[string]any, openAPIVersion string) map[string]any {
	if schema == nil {
		return nil
	}

	result := deepCopyMap(schema)

	// 处理 nullable（仅 OpenAPI 3.0）
	if strings.HasPrefix(openAPIVersion, "3.0") {
		if nullable, _ := result["nullable"].(bool); nullable {
			delete(result, "nullable")
			if typ, ok := result["type"].(string); ok {
				result["type"] = []string{typ, "null"}
			} else if types, ok := result["type"].([]any); ok {
				hasNull := false
				for _, t := range types {
					if t == "null" {
						hasNull = true
						break
					}
				}
				if !hasNull {
					result["type"] = append(types, "null")
				}
			} else if _, hasOneOf := result["oneOf"]; hasOneOf {
				result["anyOf"] = result["oneOf"]
				delete(result, "oneOf")
				result["anyOf"] = append(result["anyOf"].([]any), map[string]any{"type": "null"})
			} else if _, hasAnyOf := result["anyOf"]; hasAnyOf {
				result["anyOf"] = append(result["anyOf"].([]any), map[string]any{"type": "null"})
			}
			// enum 中追加 null
			if enumVals, ok := result["enum"].([]any); ok {
				result["enum"] = append(enumVals, nil)
			}
		}
	}

	// oneOf 转 anyOf
	if _, hasOneOf := result["oneOf"]; hasOneOf {
		result["anyOf"] = result["oneOf"]
		delete(result, "oneOf")
	}

	// 移除 OpenAPI 特有字段
	openAPISpecificFields := []string{"nullable", "discriminator", "readOnly", "writeOnly", "xml", "externalDocs", "deprecated"}
	for _, field := range openAPISpecificFields {
		delete(result, field)
	}

	// 递归处理嵌套 schema
	for _, field := range []string{"properties", "items", "additionalProperties", "not"} {
		if nested, ok := result[field].(map[string]any); ok {
			result[field] = convertOpenAPISchemaToJSONSchema(nested, openAPIVersion)
		}
	}
	for _, field := range []string{"allOf", "anyOf", "oneOf"} {
		if arr, ok := result[field].([]any); ok {
			converted := make([]any, 0, len(arr))
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					converted = append(converted, convertOpenAPISchemaToJSONSchema(m, openAPIVersion))
				} else {
					converted = append(converted, item)
				}
			}
			result[field] = converted
		}
	}

	// properties 需要遍历所有值
	if props, ok := result["properties"].(map[string]any); ok {
		for k, v := range props {
			if m, ok := v.(map[string]any); ok {
				props[k] = convertOpenAPISchemaToJSONSchema(m, openAPIVersion)
			}
		}
	}

	return result
}

// formatDeepObjectParameter 将 deepObject 风格的参数序列化为 param[key]=value 格式。
//
// 对应 Python: fastmcp.utilities.openapi.formatters.format_deep_object_parameter
//
// OpenAPI 3.0 deepObject 样式 + explode=true：
//   - 输入：{"id": "123", "type": "user"}, paramName="filter"
//   - 输出：{"filter[id]": "123", "filter[type]": "user"}
func formatDeepObjectParameter(paramValue map[string]any, paramName string) map[string]string {
	if paramValue == nil {
		return nil
	}
	result := make(map[string]string, len(paramValue))
	for key, value := range paramValue {
		bracketedKey := fmt.Sprintf("%s[%s]", paramName, key)
		result[bracketedKey] = fmt.Sprintf("%v", value)
	}
	return result
}

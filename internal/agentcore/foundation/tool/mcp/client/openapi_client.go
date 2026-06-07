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

// 编译期检查：OpenApiClient 实现 McpClient 接口
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

	// 处理 output schema 包装
	textContent := string(respBody)
	if info.outputSchema != nil {
		if wrapResult, _ := info.outputSchema["x-fastmcp-wrap-result"].(bool); wrapResult {
			// 非 object 类型的响应需要包装为 {"result": <原始值>}
			var parsed any
			if json.Unmarshal(respBody, &parsed) == nil {
				wrappedResult, _ := json.Marshal(map[string]any{"result": parsed})
				textContent = string(wrappedResult)
			}
		}
	}

	// 返回 MCP 格式结果
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": textContent,
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

	// 提取输出 Schema
	outputSchema := extractOutputSchema(op)

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

// extractOutputSchema 从 OpenAPI Operation 的响应中提取输出 JSON Schema。
//
// 对照 Python: fastmcp.utilities.openapi.extract_output_schema_from_responses
// 逻辑：按 200→201→202→204→任意2xx 优先级查找成功响应，
// 从中选择 JSON 兼容的 Content-Type，用 oapiSchemaToMap 转换。
// 如果 schema type 不是 object，则包装为 MCP 要求的 object 格式。
func extractOutputSchema(op *openapi3.Operation) map[string]any {
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

	// 处理 $ref：将 #/components/schemas/ 替换为 #/$defs/
	replaceSchemaRefs(outputSchema)

	// 如果 schema type 不是 object，包装为 MCP 要求的 object 格式
	typeVal, _ := outputSchema["type"]
	if typeVal != "object" {
		wrapped := map[string]any{
			"type":                  "object",
			"properties":            map[string]any{"result": outputSchema},
			"required":              []string{"result"},
			"x-fastmcp-wrap-result": true,
		}
		return wrapped
	}

	return outputSchema
}

// replaceSchemaRefs 递归替换 map 中的 $ref 引用。
//
// 将 #/components/schemas/Xxx 替换为 #/$defs/Xxx（JSON Schema 格式）。
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
			queryParams[p.name] = val
		case "header":
			headerParams[p.name] = val
		}
	}

	// 剩余未匹配参数视为 body 属性
	bodyArgs := make(map[string]any)
	for k, v := range arguments {
		if !consumedKeys[k] {
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

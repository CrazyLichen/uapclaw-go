package client

import (
	"fmt"
	"net/url"

	mcp "github.com/mark3labs/mcp-go/mcp"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// callToolResultToMap 将 mcp-go 的 CallToolResult 转为 map[string]any，
// 供 ExtractMCPToolResultContent 使用。
func callToolResultToMap(result *mcp.CallToolResult) map[string]any {
	if result == nil {
		return nil
	}

	contents := make([]any, 0, len(result.Content))
	for _, c := range result.Content {
		contents = append(contents, contentToMap(c))
	}

	m := map[string]any{
		"content": contents,
		"isError": result.IsError,
	}
	if result.StructuredContent != nil {
		m["structuredContent"] = result.StructuredContent
	}
	return m
}

// contentToMap 将 mcp-go 的 Content 接口转为 map[string]any。
func contentToMap(c mcp.Content) map[string]any {
	if c == nil {
		return nil
	}

	switch v := c.(type) {
	case mcp.TextContent:
		return map[string]any{
			"type": v.Type,
			"text": v.Text,
		}
	case mcp.ImageContent:
		return map[string]any{
			"type":     v.Type,
			"data":     v.Data,
			"mimeType": v.MIMEType,
		}
	case mcp.AudioContent:
		return map[string]any{
			"type":     v.Type,
			"data":     v.Data,
			"mimeType": v.MIMEType,
		}
	case mcp.EmbeddedResource:
		return map[string]any{
			"type":     v.Type,
			"resource": resourceContentsToMap(v.Resource),
		}
	case mcp.ResourceLink:
		return map[string]any{
			"type":        v.Type,
			"uri":         v.URI,
			"name":        v.Name,
			"description": v.Description,
			"mimeType":    v.MIMEType,
		}
	default:
		return map[string]any{
			"type": "unknown",
		}
	}
}

// resourceContentsToMap 将 ResourceContents 接口转为 map[string]any。
func resourceContentsToMap(rc mcp.ResourceContents) map[string]any {
	if rc == nil {
		return nil
	}

	switch v := rc.(type) {
	case mcp.TextResourceContents:
		return map[string]any{
			"uri":      v.URI,
			"mimeType": v.MIMEType,
			"text":     v.Text,
		}
	case mcp.BlobResourceContents:
		return map[string]any{
			"uri":      v.URI,
			"mimeType": v.MIMEType,
			"blob":     v.Blob,
		}
	default:
		return map[string]any{
			"type": "unknown",
		}
	}
}

// jsonSchemaToParams 将 JSON Schema 转换为参数列表。
func jsonSchemaToParams(inputSchema mcp.ToolInputSchema) []*commonschema.Param {
	if inputSchema.Properties == nil {
		return nil
	}

	params := make([]*commonschema.Param, 0, len(inputSchema.Properties))
	requiredSet := make(map[string]bool, len(inputSchema.Required))
	for _, r := range inputSchema.Required {
		requiredSet[r] = true
	}

	for name, prop := range inputSchema.Properties {
		p := jsonSchemaPropToParam(name, prop, requiredSet[name])
		if p != nil {
			params = append(params, p)
		}
	}
	return params
}

// jsonSchemaPropToParam 将单个 JSON Schema 属性转换为 *commonschema.Param。
func jsonSchemaPropToParam(name string, prop any, required bool) *commonschema.Param {
	propMap, ok := prop.(map[string]any)
	if !ok {
		return commonschema.NewStringParam(name, "", required)
	}

	typeStr, _ := propMap["type"].(string)
	desc, _ := propMap["description"].(string)

	switch typeStr {
	case "string":
		p := commonschema.NewStringParam(name, desc, required)
		applyStringConstraints(p, propMap)
		return p
	case "boolean":
		return commonschema.NewBooleanParam(name, desc, required)
	case "integer":
		p := commonschema.NewIntegerParam(name, desc, required)
		applyNumericConstraints(p, propMap)
		return p
	case "number":
		p := commonschema.NewNumberParam(name, desc, required)
		applyNumericConstraints(p, propMap)
		return p
	case "array":
		var items *commonschema.Param
		if itemsRaw, ok := propMap["items"]; ok {
			items = jsonSchemaPropToParam("items", itemsRaw, false)
		}
		return commonschema.NewArrayParam(name, desc, required, items)
	case "object":
		var properties []*commonschema.Param
		if propsRaw, ok := propMap["properties"]; ok {
			if propsMap, ok := propsRaw.(map[string]any); ok {
				objRequired := make(map[string]bool)
				if reqRaw, ok := propMap["required"]; ok {
					if reqArr, ok := reqRaw.([]any); ok {
						for _, r := range reqArr {
							if rs, ok := r.(string); ok {
								objRequired[rs] = true
							}
						}
					}
				}
				for propName, propVal := range propsMap {
					p := jsonSchemaPropToParam(propName, propVal, objRequired[propName])
					if p != nil {
						properties = append(properties, p)
					}
				}
			}
		}
		return commonschema.NewObjectParam(name, desc, required, properties)
	default:
		p := commonschema.NewStringParam(name, desc, required)
		applyStringConstraints(p, propMap)
		return p
	}
}

// applyStringConstraints 从 JSON Schema propMap 提取字符串约束字段到 Param。
func applyStringConstraints(p *commonschema.Param, propMap map[string]any) {
	if v, ok := propMap["minLength"].(float64); ok && v > 0 {
		p.MinLength = int(v)
	}
	if v, ok := propMap["maxLength"].(float64); ok && v > 0 {
		p.MaxLength = int(v)
	}
	if v, ok := propMap["pattern"].(string); ok {
		p.Pattern = v
	}
	if v, ok := propMap["format"].(string); ok {
		p.Format = v
	}
}

// applyNumericConstraints 从 JSON Schema propMap 提取数值约束字段到 Param。
func applyNumericConstraints(p *commonschema.Param, propMap map[string]any) {
	if v, ok := propMap["minimum"].(float64); ok {
		p.Minimum = v
	}
	if v, ok := propMap["maximum"].(float64); ok {
		p.Maximum = v
	}
	if v, ok := propMap["format"].(string); ok {
		p.Format = v
	}
}

// mergeQueryParams 将 queryParams 合并到 baseURL 中。
//
// 对照 Python: AuthHeaderAndQueryProvider.async_auth_flow 中 copy_merge_params
// 如果 baseURL 已有查询参数，追加而非覆盖（同名键覆盖）。
func mergeQueryParams(baseURL string, queryParams map[string]string) (string, error) {
	if len(queryParams) == 0 {
		return baseURL, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("解析 URL 失败: %w", err)
	}
	q := u.Query()
	for k, v := range queryParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

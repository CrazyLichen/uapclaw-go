package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ListMcpResourcesMetadataProvider list_mcp_resources 工具元数据提供者
type ListMcpResourcesMetadataProvider struct{}

// ReadMcpResourceMetadataProvider read_mcp_resource 工具元数据提供者
type ReadMcpResourceMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// listMcpResourcesDescription list_mcp_resources 工具双语描述
var listMcpResourcesDescription = map[string]string{
	"cn": "列出指定 MCP 服务器上可用的资源列表。",
	"en": "List available resources exposed by the specified MCP server.",
}

// readMcpResourceDescription read_mcp_resource 工具双语描述
var readMcpResourceDescription = map[string]string{
	"cn": "读取指定 MCP 服务器上某个资源的内容。",
	"en": "Read the content of a specific resource from the specified MCP server.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetListMcpResourcesMetadataProviderInputParams 构建 list_mcp_resources 工具的参数 Schema
func GetListMcpResourcesMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"server_id": {"cn": "MCP 服务器的 server_id", "en": "The server_id of the MCP server"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server_id": map[string]any{"type": "string", "description": d("server_id")},
		},
		"required": []any{"server_id"},
	}
}

// GetReadMcpResourceMetadataProviderInputParams 构建 read_mcp_resource 工具的参数 Schema
func GetReadMcpResourceMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"server_id": {"cn": "MCP 服务器的 server_id", "en": "The server_id of the MCP server"},
		"uri":       {"cn": "要读取的资源 URI", "en": "The URI of the resource to read"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server_id": map[string]any{"type": "string", "description": d("server_id")},
			"uri":       map[string]any{"type": "string", "description": d("uri")},
		},
		"required": []any{"server_id", "uri"},
	}
}

func (p *ListMcpResourcesMetadataProvider) GetName() string { return "list_mcp_resources" }
func (p *ListMcpResourcesMetadataProvider) GetDescription(language string) string {
	if d, ok := listMcpResourcesDescription[language]; ok {
		return d
	}
	return listMcpResourcesDescription["cn"]
}
func (p *ListMcpResourcesMetadataProvider) GetInputParams(language string) map[string]any {
	return GetListMcpResourcesMetadataProviderInputParams(language)
}

func (p *ReadMcpResourceMetadataProvider) GetName() string { return "read_mcp_resource" }
func (p *ReadMcpResourceMetadataProvider) GetDescription(language string) string {
	if d, ok := readMcpResourceDescription[language]; ok {
		return d
	}
	return readMcpResourceDescription["cn"]
}
func (p *ReadMcpResourceMetadataProvider) GetInputParams(language string) map[string]any {
	return GetReadMcpResourceMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&ListMcpResourcesMetadataProvider{})
	RegisterToolProvider(&ReadMcpResourceMetadataProvider{})
}

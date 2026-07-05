package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// codingMemoryReadDescription coding_memory_read 工具双语描述
var codingMemoryReadDescription = map[string]string{
	"cn": "按 offset/limit 读取 coding_memory/ 下记忆文件的部分内容（用于分页阅读）。",
	"en": "Read a portion of a memory file under coding_memory/ using offset/limit (for paging).",
}

// codingMemoryWriteDescription coding_memory_write 工具双语描述
var codingMemoryWriteDescription = map[string]string{
	"cn": "写入记忆内容到 coding_memory/ 下的 Markdown 文件（要求 frontmatter）。",
	"en": "Write memory content to a markdown file under coding_memory/ (frontmatter required).",
}

// codingMemoryEditDescription coding_memory_edit 工具双语描述
var codingMemoryEditDescription = map[string]string{
	"cn": "在 coding_memory/ 下的记忆文件中做精确字符串替换（old_text → new_text）。",
	"en": "Perform an exact string replacement inside a coding memory file (old_text → new_text).",
}

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryReadMetadataProvider coding_memory_read 工具元数据提供者
type CodingMemoryReadMetadataProvider struct{}

// CodingMemoryWriteMetadataProvider coding_memory_write 工具元数据提供者
type CodingMemoryWriteMetadataProvider struct{}

// CodingMemoryEditMetadataProvider coding_memory_edit 工具元数据提供者
type CodingMemoryEditMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCodingMemoryReadMetadataProviderInputParams 构建 coding_memory_read 工具的参数 Schema
func GetCodingMemoryReadMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":   {"cn": "coding_memory/ 下的目标文件路径（相对路径）", "en": "Target path under coding_memory/ (relative path)"},
		"offset": {"cn": "从第几行开始读取（可选）", "en": "Line offset to start reading from (optional)"},
		"limit":  {"cn": "最多读取多少行（可选）", "en": "Maximum number of lines to read (optional)"},
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
			"path":   map[string]any{"type": "string", "description": d("path")},
			"offset": map[string]any{"type": "integer", "description": d("offset")},
			"limit":  map[string]any{"type": "integer", "description": d("limit")},
		},
		"required": []any{"path"},
	}
}

// GetCodingMemoryWriteMetadataProviderInputParams 构建 coding_memory_write 工具的参数 Schema
func GetCodingMemoryWriteMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":    {"cn": "coding_memory/ 下的目标文件路径（相对路径）", "en": "Target path under coding_memory/ (relative path)"},
		"content": {"cn": "要写入的内容（含 frontmatter）", "en": "Content to write (with frontmatter)"},
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
			"path":    map[string]any{"type": "string", "description": d("path")},
			"content": map[string]any{"type": "string", "description": d("content")},
		},
		"required": []any{"path", "content"},
	}
}

// GetCodingMemoryEditMetadataProviderInputParams 构建 coding_memory_edit 工具的参数 Schema
func GetCodingMemoryEditMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":     {"cn": "coding_memory/ 下的目标文件路径（相对路径）", "en": "Target path under coding_memory/ (relative path)"},
		"old_text": {"cn": "要替换的原始文本", "en": "Original text to replace"},
		"new_text": {"cn": "替换后的新文本", "en": "New replacement text"},
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
			"path":     map[string]any{"type": "string", "description": d("path")},
			"old_text": map[string]any{"type": "string", "description": d("old_text")},
			"new_text": map[string]any{"type": "string", "description": d("new_text")},
		},
		"required": []any{"path", "old_text", "new_text"},
	}
}

func (p *CodingMemoryReadMetadataProvider) GetName() string { return "coding_memory_read" }
func (p *CodingMemoryReadMetadataProvider) GetDescription(language string) string {
	if d, ok := codingMemoryReadDescription[language]; ok {
		return d
	}
	return codingMemoryReadDescription["cn"]
}
func (p *CodingMemoryReadMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCodingMemoryReadMetadataProviderInputParams(language)
}

func (p *CodingMemoryWriteMetadataProvider) GetName() string { return "coding_memory_write" }
func (p *CodingMemoryWriteMetadataProvider) GetDescription(language string) string {
	if d, ok := codingMemoryWriteDescription[language]; ok {
		return d
	}
	return codingMemoryWriteDescription["cn"]
}
func (p *CodingMemoryWriteMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCodingMemoryWriteMetadataProviderInputParams(language)
}

func (p *CodingMemoryEditMetadataProvider) GetName() string { return "coding_memory_edit" }
func (p *CodingMemoryEditMetadataProvider) GetDescription(language string) string {
	if d, ok := codingMemoryEditDescription[language]; ok {
		return d
	}
	return codingMemoryEditDescription["cn"]
}
func (p *CodingMemoryEditMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCodingMemoryEditMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&CodingMemoryReadMetadataProvider{})
	RegisterToolProvider(&CodingMemoryWriteMetadataProvider{})
	RegisterToolProvider(&CodingMemoryEditMetadataProvider{})
}

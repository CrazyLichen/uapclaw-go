package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// memorySearchDescription memory_search 工具双语描述
var memorySearchDescription = map[string]string{
	"cn": "在长期记忆中检索过往信息（决策、偏好、人物、日期、TODO 等），返回相关片段与引用线索。",
	"en": "Search long-term memory (prior decisions, preferences, people, dates, todos) and return relevant snippets and references.",
}

// memoryGetDescription memory_get 工具双语描述
var memoryGetDescription = map[string]string{
	"cn": "按行号切片读取 memory/ 下的记忆 Markdown 文件内容（from_line + lines）。",
	"en": "Read a slice of a memory markdown file under memory/ (from_line + lines).",
}

// writeMemoryDescription write_memory 工具双语描述
var writeMemoryDescription = map[string]string{
	"cn": "写入记忆内容到 memory/ 下的 Markdown 文件；支持覆盖写或追加写（append）。",
	"en": "Write memory content to a markdown file under memory/; supports overwrite or append.",
}

// editMemoryDescription edit_memory 工具双语描述
var editMemoryDescription = map[string]string{
	"cn": "在 memory/ 下的记忆文件中做精确字符串替换（old_text → new_text）。",
	"en": "Perform an exact string replacement inside a memory file (old_text → new_text).",
}

// readMemoryDescription read_memory 工具双语描述
var readMemoryDescription = map[string]string{
	"cn": "按 offset/limit 读取 memory/ 下记忆文件的部分内容（用于分页阅读）。",
	"en": "Read a portion of a memory file under memory/ using offset/limit (for paging).",
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySearchMetadataProvider memory_search 工具元数据提供者
type MemorySearchMetadataProvider struct{}

// MemoryGetMetadataProvider memory_get 工具元数据提供者
type MemoryGetMetadataProvider struct{}

// WriteMemoryMetadataProvider write_memory 工具元数据提供者
type WriteMemoryMetadataProvider struct{}

// EditMemoryMetadataProvider edit_memory 工具元数据提供者
type EditMemoryMetadataProvider struct{}

// ReadMemoryMetadataProvider read_memory 工具元数据提供者
type ReadMemoryMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemorySearchMetadataProviderInputParams 构建 memory_search 工具的参数 Schema
func GetMemorySearchMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"query":       {"cn": "检索关键词或问题", "en": "Search query string"},
		"max_results": {"cn": "最多返回条数（可选）", "en": "Maximum number of results (optional)"},
		"min_score":   {"cn": "最小相关度阈值（可选）", "en": "Minimum relevance score threshold (optional)"},
		"session_key": {"cn": "会话键（可选，用于上下文隔离/过滤）", "en": "Session key (optional, for scoping/filtering)"},
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
			"query":       map[string]any{"type": "string", "description": d("query")},
			"max_results": map[string]any{"type": "integer", "description": d("max_results")},
			"min_score":   map[string]any{"type": "number", "description": d("min_score")},
			"session_key": map[string]any{"type": "string", "description": d("session_key")},
		},
		"required": []any{"query"},
	}
}

// GetMemoryGetMetadataProviderInputParams 构建 memory_get 工具的参数 Schema
func GetMemoryGetMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":      {"cn": "memory/ 下的目标文件路径（相对路径）", "en": "Target path under memory/ (relative path)"},
		"from_line": {"cn": "起始行号（可选）", "en": "Starting line number (optional)"},
		"lines":     {"cn": "读取行数（可选）", "en": "Number of lines to read (optional)"},
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
			"path":      map[string]any{"type": "string", "description": d("path")},
			"from_line": map[string]any{"type": "integer", "description": d("from_line")},
			"lines":     map[string]any{"type": "integer", "description": d("lines")},
		},
		"required": []any{"path"},
	}
}

// GetWriteMemoryMetadataProviderInputParams 构建 write_memory 工具的参数 Schema
func GetWriteMemoryMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":    {"cn": "memory/ 下的目标文件路径（相对路径）", "en": "Target path under memory/ (relative path)"},
		"content": {"cn": "要写入的内容", "en": "Content to write"},
		"append":  {"cn": "是否追加写入（默认 false）", "en": "Append to file instead of overwrite (default false)"},
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
			"append":  map[string]any{"type": "boolean", "description": d("append")},
		},
		"required": []any{"path", "content"},
	}
}

// GetEditMemoryMetadataProviderInputParams 构建 edit_memory 工具的参数 Schema
func GetEditMemoryMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":     {"cn": "memory/ 下的目标文件路径（相对路径）", "en": "Target path under memory/ (relative path)"},
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

// GetReadMemoryMetadataProviderInputParams 构建 read_memory 工具的参数 Schema
func GetReadMemoryMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":   {"cn": "memory/ 下的目标文件路径（相对路径）", "en": "Target path under memory/ (relative path)"},
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

func (p *MemorySearchMetadataProvider) GetName() string { return "memory_search" }
func (p *MemorySearchMetadataProvider) GetDescription(language string) string {
	if d, ok := memorySearchDescription[language]; ok {
		return d
	}
	return memorySearchDescription["cn"]
}
func (p *MemorySearchMetadataProvider) GetInputParams(language string) map[string]any {
	return GetMemorySearchMetadataProviderInputParams(language)
}

func (p *MemoryGetMetadataProvider) GetName() string { return "memory_get" }
func (p *MemoryGetMetadataProvider) GetDescription(language string) string {
	if d, ok := memoryGetDescription[language]; ok {
		return d
	}
	return memoryGetDescription["cn"]
}
func (p *MemoryGetMetadataProvider) GetInputParams(language string) map[string]any {
	return GetMemoryGetMetadataProviderInputParams(language)
}

func (p *WriteMemoryMetadataProvider) GetName() string { return "write_memory" }
func (p *WriteMemoryMetadataProvider) GetDescription(language string) string {
	if d, ok := writeMemoryDescription[language]; ok {
		return d
	}
	return writeMemoryDescription["cn"]
}
func (p *WriteMemoryMetadataProvider) GetInputParams(language string) map[string]any {
	return GetWriteMemoryMetadataProviderInputParams(language)
}

func (p *EditMemoryMetadataProvider) GetName() string { return "edit_memory" }
func (p *EditMemoryMetadataProvider) GetDescription(language string) string {
	if d, ok := editMemoryDescription[language]; ok {
		return d
	}
	return editMemoryDescription["cn"]
}
func (p *EditMemoryMetadataProvider) GetInputParams(language string) map[string]any {
	return GetEditMemoryMetadataProviderInputParams(language)
}

func (p *ReadMemoryMetadataProvider) GetName() string { return "read_memory" }
func (p *ReadMemoryMetadataProvider) GetDescription(language string) string {
	if d, ok := readMemoryDescription[language]; ok {
		return d
	}
	return readMemoryDescription["cn"]
}
func (p *ReadMemoryMetadataProvider) GetInputParams(language string) map[string]any {
	return GetReadMemoryMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&MemorySearchMetadataProvider{})
	RegisterToolProvider(&MemoryGetMetadataProvider{})
	RegisterToolProvider(&WriteMemoryMetadataProvider{})
	RegisterToolProvider(&EditMemoryMetadataProvider{})
	RegisterToolProvider(&ReadMemoryMetadataProvider{})
}

package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ReadFileMetadataProvider 工具元数据提供者
type ReadFileMetadataProvider struct{}

// WriteFileMetadataProvider 工具元数据提供者
type WriteFileMetadataProvider struct{}

// EditFileMetadataProvider 工具元数据提供者
type EditFileMetadataProvider struct{}

// GlobMetadataProvider 工具元数据提供者
type GlobMetadataProvider struct{}

// ListDirMetadataProvider 工具元数据提供者
type ListDirMetadataProvider struct{}

// GrepMetadataProvider 工具元数据提供者
type GrepMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// readFileDescription read_file 工具双语描述
var readFileDescription = map[string]string{
	"cn": "增强版文件读取工具。支持文本、图片、PDF 与 Jupyter Notebook。",
	"en": "Enhanced file reader for text, images, PDFs, and Jupyter notebooks.",
}

// writeFileDescription write_file 工具双语描述
var writeFileDescription = map[string]string{
	"cn": "写入文件内容。如果文件已存在，将完全覆盖。",
	"en": "Write file contents. Overwrites existing files only after a full read_file call.",
}

// editFileDescription edit_file 工具双语描述
var editFileDescription = map[string]string{
	"cn": "增强版文件编辑工具，对已有文件执行精确的字符串替换操作，仅传输差量。\n\n核心行为：\n- 前置读取要求：编辑前必须通过 read_file 完整读取过该文件\n- 唯一性验证：old_string 须唯一匹配；多个匹配时须设置 replace_all=true 或提供更多上下文\n- 引号容错：自动尝试直引号与弯引号互转后匹配\n- 去消毒处理：自动将 HTML 实体（&lt; &gt; &amp; 等）还原为原始字符后匹配\n- 新文件创建：old_string='' 且目标文件不存在时创建新文件\n- 格式化处理：自动去除 new_string 行尾空白（.md/.mdx 文件除外）；保留文件原有行尾风格（LF/CRLF）\n- 外部修改检测：写入前通过时间戳 + 文件大小双重校验，若文件被外部修改则拒绝写入\n\n拒绝条件：文件超过 1 GiB / old_string 与 new_string 相同 / .ipynb 文件 / 文件不存在且 old_string 非空 / 文件已存在且 old_string 为空",
	"en": "Enhanced file edit tool. Performs exact string replacement on existing files, transmitting only the diff.\n\nCore behaviour:\n- Pre-read requirement: file must be fully read via read_file before editing\n- Uniqueness validation: old_string must match exactly once; set replace_all=true or add more context when multiple matches exist\n- Quote tolerance: automatically retries with straight/curly quote substitution\n- XML desanitization: reverses HTML entity encoding (&lt; &gt; &amp; etc.) before matching\n- New file creation: old_string='' and non-existent target path creates the file\n- Formatting: strips trailing whitespace from new_string lines (except .md/.mdx); preserves original EOL style (LF/CRLF)\n- External modification detection: rejects writes when mtime + size have changed since last read\n\nRejected when: file > 1 GiB / old_string == new_string / .ipynb file / file missing with non-empty old_string / file exists with empty old_string",
}

// globDescription glob 工具双语描述
var globDescription = map[string]string{
	"cn": "使用 glob 模式查找文件。",
	"en": "Find files using glob patterns with structured results, optional path input, and default result truncation.",
}

// listDirDescription list_files 工具双语描述
var listDirDescription = map[string]string{
	"cn": "列出目录内容。",
	"en": "List directory contents.",
}

// grepDescription grep 工具双语描述
var grepDescription = map[string]string{
	"cn": "在文件中搜索内容。支持正则表达式。",
	"en": "Search file contents with regex, structured output modes, pagination, context lines, file-type filters, and glob filters.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetReadFileMetadataProviderInputParams 构建 read_file 工具的参数 Schema
func GetReadFileMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"file_path": {"cn": "要读取的绝对路径", "en": "Absolute path of the file to read"},
		"offset":    {"cn": "要跳过的行数（0 表示从头读取）。仅在文件过大无法一次读完时提供", "en": "Number of lines to skip before reading (0 = start of file). Only provide when the file is too large to read at once"},
		"limit":     {"cn": "最多读取的行数（默认及上限均为 2000）。仅在文件过大无法一次读完时提供", "en": "Maximum number of lines to read (default and cap: 2000). Only provide when the file is too large to read at once"},
		"pages":     {"cn": "PDF 专属页码范围，例如 '1-5'、'3'、'10-20'。每次最多 20 页", "en": "PDF-only page range, e.g. '1-5', '3', '10-20'. Maximum 20 pages per request"},
		"caption":   {"cn": "可选。读取 skills/… 下的图片时，填入 SKILL.md 中的图片说明文字（Markdown alt），用于多模态用户提示。", "en": "Optional. When reading an image under skills/, pass the figure caption (markdown alt text from SKILL.md) for the multimodal user prompt."},
	}
	dd := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{"type": "string", "description": dd("file_path")},
			"offset":    map[string]any{"type": "integer", "description": dd("offset")},
			"limit":     map[string]any{"type": "integer", "description": dd("limit")},
			"pages":     map[string]any{"type": "string", "description": dd("pages")},
			"caption":   map[string]any{"type": "string", "description": dd("caption")},
		},
		"required": []any{"file_path"},
	}
}

// GetWriteFileMetadataProviderInputParams 构建 write_file 工具的参数 Schema
func GetWriteFileMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"file_path": {"cn": "要写入的文件路径", "en": "Absolute path of the file to write"},
		"content":   {"cn": "要写入的内容", "en": "Content to write"},
	}
	dd := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{"type": "string", "description": dd("file_path")},
			"content":   map[string]any{"type": "string", "description": dd("content")},
		},
		"required": []any{"file_path", "content"},
	}
}

// GetEditFileMetadataProviderInputParams 构建 edit_file 工具的参数 Schema
func GetEditFileMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"file_path":   {"cn": "目标文件的绝对路径", "en": "Absolute path to the target file"},
		"old_string":  {"cn": "要替换的原始文本（空字符串可用于创建新文件或向空文件写入内容）。必须在文件中唯一匹配，否则须设置 replace_all=true 或提供更多上下文", "en": "The text to replace (empty string creates a new file or writes to an empty file). Must match exactly once unless replace_all=true or more context is provided"},
		"new_string":  {"cn": "替换后的文本，必须与 old_string 不同", "en": "The replacement text, must differ from old_string"},
		"replace_all": {"cn": "是否替换文件中所有匹配项，默认 false", "en": "Replace all occurrences of old_string in the file, default false"},
	}
	dd := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path":   map[string]any{"type": "string", "description": dd("file_path")},
			"old_string":  map[string]any{"type": "string", "description": dd("old_string")},
			"new_string":  map[string]any{"type": "string", "description": dd("new_string")},
			"replace_all": map[string]any{"type": "boolean", "description": dd("replace_all")},
		},
		"required": []any{"file_path", "old_string", "new_string"},
	}
}

// GetGlobMetadataProviderInputParams 构建 glob 工具的参数 Schema
func GetGlobMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"pattern": {"cn": "glob 模式（如 *.py, **/*.js）", "en": "Glob pattern (e.g. *.py, **/*.js)"},
		"path":    {"cn": "搜索目录，省略时默认当前工作目录", "en": "Directory to search. Defaults to the current working directory when omitted"},
	}
	dd := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string", "description": dd("pattern")},
			"path":    map[string]any{"type": "string", "description": dd("path")},
		},
		"required": []any{"pattern"},
	}
}

// GetListDirMetadataProviderInputParams 构建 list_files 工具的参数 Schema
func GetListDirMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"path":        {"cn": "目录路径", "en": "Directory path"},
		"show_hidden": {"cn": "显示隐藏文件", "en": "Show hidden files"},
	}
	dd := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": dd("path")},
			"show_hidden": map[string]any{"type": "boolean", "description": dd("show_hidden")},
		},
		"required": []any{},
	}
}

// GetGrepMetadataProviderInputParams 构建 grep 工具的参数 Schema
func GetGrepMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"pattern":     {"cn": "搜索模式（正则表达式）", "en": "Search pattern (regular expression)"},
		"path":        {"cn": "搜索路径（文件或目录），默认为当前工作目录", "en": "Search path (file or directory). Defaults to the current working directory"},
		"ignore_case": {"cn": "忽略大小写（兼容旧字段）", "en": "Ignore case (legacy compatibility alias)"},
		"glob":        {"cn": "glob 过滤模式，例如 *.py 或 *.{ts,tsx}", "en": "Glob filter pattern such as *.py or *.{ts,tsx}"},
		"output_mode": {"cn": "输出模式：content、files_with_matches 或 count，默认 content", "en": "Output mode: content, files_with_matches, or count. Defaults to content"},
		"-B":          {"cn": "每个匹配前显示的上下文行数，仅在 content 模式生效", "en": "Lines of leading context before each match; only used in content mode"},
		"-A":          {"cn": "每个匹配后显示的上下文行数，仅在 content 模式生效", "en": "Lines of trailing context after each match; only used in content mode"},
		"-C":          {"cn": "每个匹配前后都显示的上下文行数，仅在 content 模式生效", "en": "Lines of context before and after each match; only used in content mode"},
		"context":     {"cn": "-C 的别名，用于设置前后对称上下文行数", "en": "Alias of -C for symmetric context lines"},
		"-n":          {"cn": "在 content 模式显示行号，默认 true", "en": "Show line numbers in content mode. Defaults to true"},
		"-i":          {"cn": "大小写不敏感搜索", "en": "Case-insensitive search"},
		"type":        {"cn": "文件类型过滤，例如 py、js、ts，需要 rg", "en": "File type filter such as py, js, or ts. Requires rg"},
		"head_limit":  {"cn": "只返回前 N 条记录或行。0 表示不限制，默认 250", "en": "Return only the first N entries or lines. Use 0 for unlimited. Defaults to 250"},
		"offset":      {"cn": "先跳过前 N 条记录或行，再应用 head_limit，默认 0", "en": "Skip the first N entries or lines before applying head_limit. Defaults to 0"},
		"multiline":   {"cn": "启用多行正则模式，需要 rg", "en": "Enable multiline regex mode. Requires rg"},
	}
	dd := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     map[string]any{"type": "string", "description": dd("pattern")},
			"path":        map[string]any{"type": "string", "description": dd("path")},
			"ignore_case": map[string]any{"type": "boolean", "description": dd("ignore_case")},
			"glob":        map[string]any{"type": "string", "description": dd("glob")},
			"output_mode": map[string]any{"type": "string", "enum": []any{"content", "files_with_matches", "count"}, "description": dd("output_mode")},
			"-B":          map[string]any{"type": "integer", "description": dd("-B")},
			"-A":          map[string]any{"type": "integer", "description": dd("-A")},
			"-C":          map[string]any{"type": "integer", "description": dd("-C")},
			"context":     map[string]any{"type": "integer", "description": dd("context")},
			"-n":          map[string]any{"type": "boolean", "description": dd("-n")},
			"-i":          map[string]any{"type": "boolean", "description": dd("-i")},
			"type":        map[string]any{"type": "string", "description": dd("type")},
			"head_limit":  map[string]any{"type": "integer", "description": dd("head_limit")},
			"offset":      map[string]any{"type": "integer", "description": dd("offset")},
			"multiline":   map[string]any{"type": "boolean", "description": dd("multiline")},
		},
		"required": []any{"pattern"},
	}
}

func (p *ReadFileMetadataProvider) GetName() string { return "read_file" }
func (p *ReadFileMetadataProvider) GetDescription(language string) string {
	if d, ok := readFileDescription[language]; ok {
		return d
	}
	return readFileDescription["cn"]
}
func (p *ReadFileMetadataProvider) GetInputParams(language string) map[string]any {
	return GetReadFileMetadataProviderInputParams(language)
}

func (p *WriteFileMetadataProvider) GetName() string { return "write_file" }
func (p *WriteFileMetadataProvider) GetDescription(language string) string {
	if d, ok := writeFileDescription[language]; ok {
		return d
	}
	return writeFileDescription["cn"]
}
func (p *WriteFileMetadataProvider) GetInputParams(language string) map[string]any {
	return GetWriteFileMetadataProviderInputParams(language)
}

func (p *EditFileMetadataProvider) GetName() string { return "edit_file" }
func (p *EditFileMetadataProvider) GetDescription(language string) string {
	if d, ok := editFileDescription[language]; ok {
		return d
	}
	return editFileDescription["cn"]
}
func (p *EditFileMetadataProvider) GetInputParams(language string) map[string]any {
	return GetEditFileMetadataProviderInputParams(language)
}

func (p *GlobMetadataProvider) GetName() string { return "glob" }
func (p *GlobMetadataProvider) GetDescription(language string) string {
	if d, ok := globDescription[language]; ok {
		return d
	}
	return globDescription["cn"]
}
func (p *GlobMetadataProvider) GetInputParams(language string) map[string]any {
	return GetGlobMetadataProviderInputParams(language)
}

func (p *ListDirMetadataProvider) GetName() string { return "list_files" }
func (p *ListDirMetadataProvider) GetDescription(language string) string {
	if d, ok := listDirDescription[language]; ok {
		return d
	}
	return listDirDescription["cn"]
}
func (p *ListDirMetadataProvider) GetInputParams(language string) map[string]any {
	return GetListDirMetadataProviderInputParams(language)
}

func (p *GrepMetadataProvider) GetName() string { return "grep" }
func (p *GrepMetadataProvider) GetDescription(language string) string {
	if d, ok := grepDescription[language]; ok {
		return d
	}
	return grepDescription["cn"]
}
func (p *GrepMetadataProvider) GetInputParams(language string) map[string]any {
	return GetGrepMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&ReadFileMetadataProvider{})
	RegisterToolProvider(&WriteFileMetadataProvider{})
	RegisterToolProvider(&EditFileMetadataProvider{})
	RegisterToolProvider(&GlobMetadataProvider{})
	RegisterToolProvider(&ListDirMetadataProvider{})
	RegisterToolProvider(&GrepMetadataProvider{})
}

package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// codeDescription code 工具双语描述
var codeDescription = map[string]string{
	"cn": `执行代码（Python 或 JavaScript）。`,
	"en": `Execute code (Python or JavaScript).`,
}

// ──────────────────────────── 结构体 ────────────────────────────

// CodeMetadataProvider code 工具元数据提供者
type CodeMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCodeMetadataProviderInputParams 构建 code 工具的参数 Schema
func GetCodeMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"code":     {"cn": "要执行的代码", "en": "Code to execute"},
		"language": {"cn": "编程语言，支持 python 或 javascript，默认 python", "en": "Programming language, supports python or javascript, default python"},
		"timeout":  {"cn": "超时时间（秒），默认 300，上限 3600", "en": "Timeout in seconds, default 300, max 3600"},
	}
	desc := func(key string) string {
		if d, ok := p[key][lang]; ok {
			return d
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code":     map[string]any{"type": "string", "description": desc("code")},
			"language": map[string]any{"type": "string", "description": desc("language")},
			"timeout":  map[string]any{"type": "integer", "description": desc("timeout")},
		},
		"required": []any{"code"},
	}
}

func (p *CodeMetadataProvider) GetName() string { return "code" }
func (p *CodeMetadataProvider) GetDescription(language string) string {
	if desc, ok := codeDescription[language]; ok {
		return desc
	}
	return codeDescription["cn"]
}
func (p *CodeMetadataProvider) GetInputParams(language string) map[string]any {
	return GetCodeMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&CodeMetadataProvider{}) }

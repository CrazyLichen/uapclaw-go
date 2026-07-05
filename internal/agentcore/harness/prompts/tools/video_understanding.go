package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// videoUnderstandingDescription video_understanding 工具双语描述
var videoUnderstandingDescription = map[string]string{
	"cn": "理解视频内容并回答用户问题，支持远程视频 URL 或本地视频文件路径。",
	"en": "Understand video content and answer user queries. Supports remote video URLs or local video file paths.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// VideoUnderstandingMetadataProvider video_understanding 工具元数据提供者
type VideoUnderstandingMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetVideoUnderstandingMetadataProviderInputParams 构建 video_understanding 工具的参数 Schema
func GetVideoUnderstandingMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"query":           {"cn": "用户关于视频内容的问题", "en": "User query about the video content"},
		"video_path":      {"cn": "本地视频路径或远程视频 URL", "en": "Local video path or remote video URL"},
		"model":           {"cn": "可选，指定模型名称", "en": "Optional model name"},
		"max_tokens":      {"cn": "可选，最大输出 token 数", "en": "Optional maximum output tokens"},
		"temperature":     {"cn": "可选，采样温度", "en": "Optional sampling temperature"},
		"timeout_seconds": {"cn": "可选，请求超时时间（秒）", "en": "Optional timeout in seconds"},
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
			"query":           map[string]any{"type": "string", "description": d("query")},
			"video_path":      map[string]any{"type": "string", "description": d("video_path")},
			"model":           map[string]any{"type": "string", "description": d("model")},
			"max_tokens":      map[string]any{"type": "integer", "description": d("max_tokens")},
			"temperature":     map[string]any{"type": "number", "description": d("temperature")},
			"timeout_seconds": map[string]any{"type": "integer", "description": d("timeout_seconds")},
		},
		"required": []any{"query", "video_path"},
	}
}

func (p *VideoUnderstandingMetadataProvider) GetName() string { return "video_understanding" }
func (p *VideoUnderstandingMetadataProvider) GetDescription(language string) string {
	if d, ok := videoUnderstandingDescription[language]; ok {
		return d
	}
	return videoUnderstandingDescription["cn"]
}
func (p *VideoUnderstandingMetadataProvider) GetInputParams(language string) map[string]any {
	return GetVideoUnderstandingMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&VideoUnderstandingMetadataProvider{}) }

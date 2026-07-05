package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// audioTranscriptionDescription audio_transcription 工具双语描述
var audioTranscriptionDescription = map[string]string{
	"cn": "转写本地音频文件或公网音频 URL，提取音频中的语音文本内容。",
	"en": "Transcribe a local audio file or public audio URL into text.",
}

// audioQuestionAnsweringDescription audio_question_answering 工具双语描述
var audioQuestionAnsweringDescription = map[string]string{
	"cn": "理解音频内容并回答问题，适合语音、访谈、播客和普通音频内容分析。",
	"en": "Understand audio content and answer questions about speech or general audio.",
}

// audioMetadataDescription audio_metadata 工具双语描述
var audioMetadataDescription = map[string]string{
	"cn": "识别音频时长，并在配置了 ACR 信息时尝试识别歌曲标题、歌手和发布时间。",
	"en": "Inspect audio duration and optionally identify song metadata when ACR credentials are configured.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// AudioTranscriptionMetadataProvider 工具元数据提供者
type AudioTranscriptionMetadataProvider struct{}

// AudioQuestionAnsweringMetadataProvider 工具元数据提供者
type AudioQuestionAnsweringMetadataProvider struct{}

// AudioMetadataMetadataProvider 工具元数据提供者
type AudioMetadataMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetAudioTranscriptionMetadataProviderInputParams 构建工具的参数 Schema
func GetAudioTranscriptionMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"audio_path_or_url": {"cn": "本地音频路径或公网 http(s) 音频 URL，不支持 sandbox-only 路径", "en": "Local audio path or public http(s) audio URL; sandbox-only paths are not supported"},
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
			"audio_path_or_url": map[string]any{"type": "string", "description": d("audio_path_or_url")},
		},
		"required": []any{"audio_path_or_url"},
	}
}

// GetAudioQuestionAnsweringMetadataProviderInputParams 构建工具的参数 Schema
func GetAudioQuestionAnsweringMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"audio_path_or_url": {"cn": "本地音频路径或公网 http(s) 音频 URL，不支持 sandbox-only 路径", "en": "Local audio path or public http(s) audio URL; sandbox-only paths are not supported"},
		"question":          {"cn": "要基于音频内容回答的问题", "en": "Question to answer based on the audio content"},
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
			"audio_path_or_url": map[string]any{"type": "string", "description": d("audio_path_or_url")},
			"question":          map[string]any{"type": "string", "description": d("question")},
		},
		"required": []any{"audio_path_or_url", "question"},
	}
}

// GetAudioMetadataMetadataProviderInputParams 构建工具的参数 Schema
func GetAudioMetadataMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"audio_path_or_url": {"cn": "本地音频路径或公网 http(s) 音频 URL，不支持 sandbox-only 路径", "en": "Local audio path or public http(s) audio URL; sandbox-only paths are not supported"},
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
			"audio_path_or_url": map[string]any{"type": "string", "description": d("audio_path_or_url")},
		},
		"required": []any{"audio_path_or_url"},
	}
}

func (p *AudioTranscriptionMetadataProvider) GetName() string { return "audio_transcription" }
func (p *AudioTranscriptionMetadataProvider) GetDescription(language string) string {
	if d, ok := audioTranscriptionDescription[language]; ok {
		return d
	}
	return audioTranscriptionDescription["cn"]
}
func (p *AudioTranscriptionMetadataProvider) GetInputParams(language string) map[string]any {
	return GetAudioTranscriptionMetadataProviderInputParams(language)
}

func (p *AudioQuestionAnsweringMetadataProvider) GetName() string { return "audio_question_answering" }
func (p *AudioQuestionAnsweringMetadataProvider) GetDescription(language string) string {
	if d, ok := audioQuestionAnsweringDescription[language]; ok {
		return d
	}
	return audioQuestionAnsweringDescription["cn"]
}
func (p *AudioQuestionAnsweringMetadataProvider) GetInputParams(language string) map[string]any {
	return GetAudioQuestionAnsweringMetadataProviderInputParams(language)
}

func (p *AudioMetadataMetadataProvider) GetName() string { return "audio_metadata" }
func (p *AudioMetadataMetadataProvider) GetDescription(language string) string {
	if d, ok := audioMetadataDescription[language]; ok {
		return d
	}
	return audioMetadataDescription["cn"]
}
func (p *AudioMetadataMetadataProvider) GetInputParams(language string) map[string]any {
	return GetAudioMetadataMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&AudioTranscriptionMetadataProvider{})
	RegisterToolProvider(&AudioQuestionAnsweringMetadataProvider{})
	RegisterToolProvider(&AudioMetadataMetadataProvider{})
}

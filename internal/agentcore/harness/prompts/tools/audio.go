package tools

// ──────────────────────────── 结构体 ────────────────────────────

// AudioTranscriptionMetadataProvider 工具元数据提供者
type AudioTranscriptionMetadataProvider struct{}

// AudioQuestionAnsweringMetadataProvider 工具元数据提供者
type AudioQuestionAnsweringMetadataProvider struct{}

// AudioMetadataMetadataProvider 工具元数据提供者
type AudioMetadataMetadataProvider struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

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

// audioPathOrURLDesc 音频路径/URL 参数的双语描述（三个音频工具共用）。
var audioPathOrURLDesc = map[string]string{
	"cn": "本地音频路径或公网 http(s) 音频 URL，不支持 sandbox-only 路径",
	"en": "Local audio path or public http(s) audio URL; sandbox-only paths are not supported",
}

// audioQuestionDesc 音频问答问题的双语描述。
var audioQuestionDesc = map[string]string{
	"cn": "要基于音频内容回答的问题",
	"en": "Question to answer based on the audio content",
}

// audioMetadataDescription audio_metadata 工具双语描述
var audioMetadataDescription = map[string]string{
	"cn": "识别音频时长，并在配置了 ACR 信息时尝试识别歌曲标题、歌手和发布时间。",
	"en": "Inspect audio duration and optionally identify song metadata when ACR credentials are configured.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetAudioTranscriptionMetadataProviderInputParams 构建工具的参数 Schema
func GetAudioTranscriptionMetadataProviderInputParams(language string) map[string]any {
	return buildAudioInputParams(language, map[string]map[string]string{
		"audio_path_or_url": audioPathOrURLDesc,
	}, []any{"audio_path_or_url"})
}

// GetAudioQuestionAnsweringMetadataProviderInputParams 构建工具的参数 Schema
func GetAudioQuestionAnsweringMetadataProviderInputParams(language string) map[string]any {
	return buildAudioInputParams(language, map[string]map[string]string{
		"audio_path_or_url": audioPathOrURLDesc,
		"question":          audioQuestionDesc,
	}, []any{"audio_path_or_url", "question"})
}

// GetAudioMetadataMetadataProviderInputParams 构建工具的参数 Schema
func GetAudioMetadataMetadataProviderInputParams(language string) map[string]any {
	return buildAudioInputParams(language, map[string]map[string]string{
		"audio_path_or_url": audioPathOrURLDesc,
	}, []any{"audio_path_or_url"})
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

// buildAudioInputParams 按语言生成音频工具的参数 Schema。
//
// props 为「参数名 → 双语描述」，required 为必填参数（顺序与 Python 声明一致）；
// 非法语言回落到中文，某个参数缺该语言描述时同样回落中文。
func buildAudioInputParams(language string, props map[string]map[string]string, required []any) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	properties := make(map[string]any, len(props))
	for key, desc := range props {
		text, ok := desc[lang]
		if !ok {
			text = desc["cn"]
		}
		properties[key] = map[string]any{"type": "string", "description": text}
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

func init() {
	RegisterToolProvider(&AudioTranscriptionMetadataProvider{})
	RegisterToolProvider(&AudioQuestionAnsweringMetadataProvider{})
	RegisterToolProvider(&AudioMetadataMetadataProvider{})
}

package dashscope

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

// DashScopeResponse DashScope 原生 API 通用响应外层（图片/语音/视频共用）。
//
// DashScope 原生协议响应格式：
//
//	{"status_code": 200, "request_id": "xxx", "code": "", "message": "", "output": {...}, "usage": {...}}
//
// 对应 Python: dashscope SDK Response 对象的 status_code/code/message/output 属性
type DashScopeResponse struct {
	// StatusCode HTTP 状态码，200 表示成功
	StatusCode int `json:"status_code"`
	// RequestID 请求唯一标识
	RequestID string `json:"request_id"`
	// Code 错误码（成功时为空）
	Code string `json:"code,omitempty"`
	// Message 错误消息（成功时为空）
	Message string `json:"message,omitempty"`
	// Output 业务输出（不同接口结构不同，使用 RawMessage 延迟解析）
	Output json.RawMessage `json:"output,omitempty"`
	// Usage 用量信息（使用 RawMessage 延迟解析）
	Usage json.RawMessage `json:"usage,omitempty"`
}

// MultiModalOutput DashScope 多模态 API 输出（图片/语音共用）。
//
// 对应 Python: response.output（MultiModalConversation 响应）
type MultiModalOutput struct {
	// Choices 选项列表
	Choices []MultiModalChoice `json:"choices"`
}

// MultiModalChoice 多模态 API 响应选项。
type MultiModalChoice struct {
	// FinishReason 完成原因（"stop" 等）
	FinishReason string `json:"finish_reason"`
	// Message 消息内容
	Message *MultiModalMessage `json:"message"`
}

// MultiModalMessage 多模态 API 响应消息。
type MultiModalMessage struct {
	// Role 角色类型
	Role string `json:"role"`
	// Content 内容列表（文本/图片/音频混合）
	Content []ContentItem `json:"content"`
}

// ContentItem 多模态内容项（文本/图片/音频三选一）。
//
// DashScope 原生协议使用 {"text": "..."} / {"image": "url"} / {"audio": "url"} 格式。
type ContentItem struct {
	// Text 文本内容
	Text string `json:"text,omitempty"`
	// Image 图片 URL
	Image string `json:"image,omitempty"`
	// Audio 音频 URL 或 Base64 数据
	Audio string `json:"audio,omitempty"`
}

// VideoOutput DashScope 视频生成 API 输出。
//
// 对应 Python: response.output（VideoSynthesis 响应）
type VideoOutput struct {
	// VideoURL 生成的视频 URL
	VideoURL string `json:"video_url"`
}

// AudioOutput DashScope 语音生成 API 输出。
//
// 对应 Python: response.output.audio（Cosyvoice 语音合成响应）
type AudioOutput struct {
	// URL 音频文件 URL
	URL string `json:"url,omitempty"`
	// Base64 编码的音频数据
	Data string `json:"data,omitempty"`
}

// VideoUsage 视频生成用量信息。
type VideoUsage struct {
	// Duration 视频时长
	Duration any `json:"duration,omitempty"`
	// OutputVideoDuration 输出视频时长
	OutputVideoDuration any `json:"output_video_duration,omitempty"`
	// Size 视频分辨率
	Size string `json:"size,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────

// DashScope 默认 API 域名
const (
	// defaultDashScopeHost DashScope 默认 API 主机地址
	defaultDashScopeHost = "https://dashscope.aliyuncs.com"

	// compatibleModeMarker OpenAI 兼容模式路径标识
	compatibleModeMarker = "/compatible-mode/"
)

// DashScope 原生 API 路径
const (
	// multiModalPath 多模态生成 API 路径（图片/语音）
	multiModalPath = "/api/v1/services/aigc/multimodal-generation/generation"

	// videoSynthesisPath 视频生成 API 路径
	videoSynthesisPath = "/api/v1/services/aigc/video-generation/generation"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// DashScopeVoices DashScope Cosyvoice 语音合成支持的声音列表。
//
// 对应 Python: DASHSCOPE_VOICE 常量
var DashScopeVoices = []string{
	"Cherry", "Serena", "Ethan", "Chelsie", "Momo", "Vivian", "Moon", "Maia",
	"Kai", "Nofish", "Bella", "Jennifer", "Ryan", "Katerina", "Aiden",
	"Eldric Sage", "Mia", "Mochi", "Bellona", "Vincent", "Bunny", "Neil",
	"Elias", "Arthur", "Nini", "Ebona", "Seren", "Pip", "Stella", "Bodega",
	"Sonrisa", "Alek", "Dolce", "Sohee", "Ono Anna", "Lenn", "Emilien",
	"Andre", "Radio Gol", "Jada", "Dylan", "Li", "Marcus", "Roy", "Peter",
	"Sunny", "Eric", "Rocky", "Kiki",
}

// DashScopeLanguageTypes DashScope Cosyvoice 语音合成支持的语言类型。
//
// 对应 Python: DASHSCOPE_LANGUAGE_TYPE 常量
var DashScopeLanguageTypes = []string{
	"Chinese", "English", "German", "Italian", "Portuguese",
	"Spanish", "Japanese", "Korean", "French", "Russian",
}

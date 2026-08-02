package schema

// ──────────────────────────── 结构体 ────────────────────────────

// TranscriptionResponse 音频转写响应
type TranscriptionResponse struct {
	// Text 转写文本
	Text string `json:"text"`
}

// TranscriptionParams 音频转写参数
type TranscriptionParams struct {
	// Model 模型名称
	Model string
	// Language 语言代码（可选）
	Language string
	// Timeout 超时秒数
	Timeout float64
	// Extra 额外参数
	Extra map[string]any
}

// TranscribeAudioOption 音频转写选项函数
type TranscribeAudioOption func(*TranscriptionParams)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTranscriptionParams 创建 TranscriptionParams（默认零值，通过 opts 填充）。
func NewTranscriptionParams(opts ...TranscribeAudioOption) *TranscriptionParams {
	p := &TranscriptionParams{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithTranscriptionModel 设置转写模型名称
func WithTranscriptionModel(model string) TranscribeAudioOption {
	return func(p *TranscriptionParams) { p.Model = model }
}

// WithTranscriptionLanguage 设置转写语言
func WithTranscriptionLanguage(lang string) TranscribeAudioOption {
	return func(p *TranscriptionParams) { p.Language = lang }
}

// WithTranscriptionTimeout 设置超时秒数
func WithTranscriptionTimeout(timeout float64) TranscribeAudioOption {
	return func(p *TranscriptionParams) { p.Timeout = timeout }
}

// WithTranscriptionExtra 设置额外参数
func WithTranscriptionExtra(extra map[string]any) TranscribeAudioOption {
	return func(p *TranscriptionParams) { p.Extra = extra }
}

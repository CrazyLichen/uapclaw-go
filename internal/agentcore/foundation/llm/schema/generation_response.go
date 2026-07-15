package schema

// ──────────────────────────── 结构体 ────────────────────────────

// GenerationResponse 多模态生成响应基类，所有具体生成响应类型的公共父类。
//
// 仅包含一个 Model 字段，表示用于生成的模型名称。
// 本身不直接使用，而是通过嵌入到具体的子类型中来提供公共字段。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/generation_response.py (GenerationResponse)
type GenerationResponse struct {
	// Model 用于生成的模型名称
	Model string `json:"model,omitempty"`
}

// ImageGenerationResponse 图片生成响应，包含生成的图片 URL 列表或 Base64 编码数据。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/generation_response.py (ImageGenerationResponse)
type ImageGenerationResponse struct {
	GenerationResponse
	// Images 生成的图片 URL 列表
	Images []string `json:"images,omitempty"`
	// ImagesBase64 生成的图片 Base64 编码列表
	ImagesBase64 []string `json:"images_base64,omitempty"`
	// Created 创建时间戳（Unix 秒）
	Created *int64 `json:"created,omitempty"`
}

// AudioGenerationResponse 音频/语音生成响应，包含生成的音频 URL 或二进制数据。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/generation_response.py (AudioGenerationResponse)
type AudioGenerationResponse struct {
	GenerationResponse
	// AudioURL 生成的音频 URL
	AudioURL *string `json:"audio_url,omitempty"`
	// AudioData 生成的二进制音频数据，JSON 序列化时自动编码为 Base64
	AudioData []byte `json:"audio_data,omitempty"`
	// Duration 音频时长（秒）
	Duration *float64 `json:"duration,omitempty"`
	// Format 音频格式，默认 "mp3"
	Format string `json:"format,omitempty"`
}

// VideoGenerationResponse 视频生成响应，包含生成的视频 URL 或二进制数据。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/generation_response.py (VideoGenerationResponse)
type VideoGenerationResponse struct {
	GenerationResponse
	// VideoURL 生成的视频 URL
	VideoURL *string `json:"video_url,omitempty"`
	// VideoData 生成的二进制视频数据，JSON 序列化时自动编码为 Base64
	VideoData []byte `json:"video_data,omitempty"`
	// Duration 视频时长（秒）
	Duration *float64 `json:"duration,omitempty"`
	// Resolution 视频分辨率，如 "1920x1080"
	Resolution string `json:"resolution,omitempty"`
	// Format 视频格式，默认 "mp4"
	Format string `json:"format,omitempty"`
}

// GenerationResponseOption GenerationResponse 的函数式选项。
type GenerationResponseOption func(*GenerationResponse)

// ImageGenerationResponseOption ImageGenerationResponse 的函数式选项。
type ImageGenerationResponseOption func(*ImageGenerationResponse)

// AudioGenerationResponseOption AudioGenerationResponse 的函数式选项。
type AudioGenerationResponseOption func(*AudioGenerationResponse)

// VideoGenerationResponseOption VideoGenerationResponse 的函数式选项。
type VideoGenerationResponseOption func(*VideoGenerationResponse)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithModel 设置生成响应使用的模型名称。
func WithModel(model string) GenerationResponseOption {
	return func(r *GenerationResponse) {
		r.Model = model
	}
}

// WithImages 设置生成的图片 URL 列表。
func WithImages(images []string) ImageGenerationResponseOption {
	return func(r *ImageGenerationResponse) {
		r.Images = images
	}
}

// WithImagesBase64 设置生成的图片 Base64 编码列表。
func WithImagesBase64(imagesBase64 []string) ImageGenerationResponseOption {
	return func(r *ImageGenerationResponse) {
		r.ImagesBase64 = imagesBase64
	}
}

// WithCreated 设置创建时间戳（Unix 秒）。
func WithCreated(created int64) ImageGenerationResponseOption {
	return func(r *ImageGenerationResponse) {
		r.Created = &created
	}
}

// WithImageModel 设置图片生成响应使用的模型名称。
func WithImageModel(model string) ImageGenerationResponseOption {
	return func(r *ImageGenerationResponse) {
		r.Model = model
	}
}

// WithAudioURL 设置生成的音频 URL。
func WithAudioURL(audioURL string) AudioGenerationResponseOption {
	return func(r *AudioGenerationResponse) {
		r.AudioURL = &audioURL
	}
}

// WithAudioData 设置生成的二进制音频数据。
func WithAudioData(audioData []byte) AudioGenerationResponseOption {
	return func(r *AudioGenerationResponse) {
		r.AudioData = audioData
	}
}

// WithDuration 设置音频时长（秒）。
func WithDuration(duration float64) AudioGenerationResponseOption {
	return func(r *AudioGenerationResponse) {
		r.Duration = &duration
	}
}

// WithAudioFormat 设置音频格式，如 "mp3"、"wav"。
func WithAudioFormat(format string) AudioGenerationResponseOption {
	return func(r *AudioGenerationResponse) {
		r.Format = format
	}
}

// WithAudioModel 设置音频生成响应使用的模型名称。
func WithAudioModel(model string) AudioGenerationResponseOption {
	return func(r *AudioGenerationResponse) {
		r.Model = model
	}
}

// WithVideoURL 设置生成的视频 URL。
func WithVideoURL(videoURL string) VideoGenerationResponseOption {
	return func(r *VideoGenerationResponse) {
		r.VideoURL = &videoURL
	}
}

// WithVideoData 设置生成的二进制视频数据。
func WithVideoData(videoData []byte) VideoGenerationResponseOption {
	return func(r *VideoGenerationResponse) {
		r.VideoData = videoData
	}
}

// WithVideoDuration 设置视频时长（秒）。
func WithVideoDuration(duration float64) VideoGenerationResponseOption {
	return func(r *VideoGenerationResponse) {
		r.Duration = &duration
	}
}

// WithResolution 设置视频分辨率，如 "1920x1080"。
func WithResolution(resolution string) VideoGenerationResponseOption {
	return func(r *VideoGenerationResponse) {
		r.Resolution = resolution
	}
}

// WithVideoFormat 设置视频格式，如 "mp4"、"avi"。
func WithVideoFormat(format string) VideoGenerationResponseOption {
	return func(r *VideoGenerationResponse) {
		r.Format = format
	}
}

// WithVideoModel 设置视频生成响应使用的模型名称。
func WithVideoModel(model string) VideoGenerationResponseOption {
	return func(r *VideoGenerationResponse) {
		r.Model = model
	}
}

// NewGenerationResponse 创建多模态生成响应基类实例。
//
// 对应 Python: GenerationResponse(model=...)
func NewGenerationResponse(opts ...GenerationResponseOption) *GenerationResponse {
	resp := &GenerationResponse{}
	for _, opt := range opts {
		opt(resp)
	}
	return resp
}

// NewImageGenerationResponse 创建图片生成响应实例。
// Model 字段通过 WithImageModel 选项设置。
//
// 对应 Python: ImageGenerationResponse(model=..., images=..., images_base64=..., created=...)
func NewImageGenerationResponse(opts ...ImageGenerationResponseOption) *ImageGenerationResponse {
	resp := &ImageGenerationResponse{}
	for _, opt := range opts {
		opt(resp)
	}
	return resp
}

// NewAudioGenerationResponse 创建音频生成响应实例。
// Format 默认为 "mp3"，Model 字段通过 WithAudioModel 选项设置。
//
// 对应 Python: AudioGenerationResponse(model=..., audio_url=..., audio_data=..., format="mp3")
func NewAudioGenerationResponse(opts ...AudioGenerationResponseOption) *AudioGenerationResponse {
	resp := &AudioGenerationResponse{
		Format: "mp3",
	}
	for _, opt := range opts {
		opt(resp)
	}
	return resp
}

// NewVideoGenerationResponse 创建视频生成响应实例。
// Format 默认为 "mp4"，Model 字段通过 WithVideoModel 选项设置。
//
// 对应 Python: VideoGenerationResponse(model=..., video_url=..., duration=..., resolution=..., format="mp4")
func NewVideoGenerationResponse(opts ...VideoGenerationResponseOption) *VideoGenerationResponse {
	resp := &VideoGenerationResponse{
		Format: "mp4",
	}
	for _, opt := range opts {
		opt(resp)
	}
	return resp
}

// ──────────────────────────── 非导出函数 ────────────────────────────

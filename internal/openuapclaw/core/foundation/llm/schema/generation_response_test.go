package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewGenerationResponse 测试 GenerationResponse 基类构造
func TestNewGenerationResponse(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		resp := NewGenerationResponse()
		if resp.Model != "" {
			t.Errorf("Model 应为空字符串，实际为 %q", resp.Model)
		}
	})

	t.Run("WithModel", func(t *testing.T) {
		resp := NewGenerationResponse(WithModel("gpt-4"))
		if resp.Model != "gpt-4" {
			t.Errorf("Model 应为 %q，实际为 %q", "gpt-4", resp.Model)
		}
	})
}

// TestNewImageGenerationResponse 测试 ImageGenerationResponse 构造
func TestNewImageGenerationResponse(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		resp := NewImageGenerationResponse()
		if resp.Model != "" {
			t.Errorf("Model 应为空字符串，实际为 %q", resp.Model)
		}
		if resp.Images != nil {
			t.Errorf("Images 应为 nil，实际为 %v", resp.Images)
		}
		if resp.ImagesBase64 != nil {
			t.Errorf("ImagesBase64 应为 nil，实际为 %v", resp.ImagesBase64)
		}
		if resp.Created != nil {
			t.Errorf("Created 应为 nil，实际为 %v", resp.Created)
		}
	})

	t.Run("全部选项", func(t *testing.T) {
		created := int64(1700000000)
		resp := NewImageGenerationResponse(
			WithImageModel("dall-e-3"),
			WithImages([]string{"https://example.com/img1.png", "https://example.com/img2.png"}),
			WithImagesBase64([]string{"iVBORw0KGgo=", "iVBORw0KGg2="}),
			WithCreated(created),
		)
		if resp.Model != "dall-e-3" {
			t.Errorf("Model 应为 %q，实际为 %q", "dall-e-3", resp.Model)
		}
		if len(resp.Images) != 2 {
			t.Fatalf("Images 长度应为 2，实际为 %d", len(resp.Images))
		}
		if resp.Images[0] != "https://example.com/img1.png" {
			t.Errorf("Images[0] 应为 %q，实际为 %q", "https://example.com/img1.png", resp.Images[0])
		}
		if len(resp.ImagesBase64) != 2 {
			t.Fatalf("ImagesBase64 长度应为 2，实际为 %d", len(resp.ImagesBase64))
		}
		if resp.Created == nil || *resp.Created != created {
			t.Errorf("Created 应为 %d，实际为 %v", created, resp.Created)
		}
	})
}

// TestNewAudioGenerationResponse 测试 AudioGenerationResponse 构造
func TestNewAudioGenerationResponse(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		resp := NewAudioGenerationResponse()
		if resp.Model != "" {
			t.Errorf("Model 应为空字符串，实际为 %q", resp.Model)
		}
		if resp.Format != "mp3" {
			t.Errorf("Format 默认应为 %q，实际为 %q", "mp3", resp.Format)
		}
		if resp.AudioURL != nil {
			t.Errorf("AudioURL 应为 nil，实际为 %v", resp.AudioURL)
		}
		if resp.AudioData != nil {
			t.Errorf("AudioData 应为 nil，实际为 %v", resp.AudioData)
		}
		if resp.Duration != nil {
			t.Errorf("Duration 应为 nil，实际为 %v", resp.Duration)
		}
	})

	t.Run("全部选项", func(t *testing.T) {
		audioURL := "https://example.com/audio.mp3"
		audioData := []byte("fake-audio-data")
		duration := 3.5

		resp := NewAudioGenerationResponse(
			WithAudioModel("tts-1"),
			WithAudioURL(audioURL),
			WithAudioData(audioData),
			WithDuration(duration),
			WithAudioFormat("wav"),
		)
		if resp.Model != "tts-1" {
			t.Errorf("Model 应为 %q，实际为 %q", "tts-1", resp.Model)
		}
		if resp.AudioURL == nil || *resp.AudioURL != audioURL {
			t.Errorf("AudioURL 应为 %q，实际为 %v", audioURL, resp.AudioURL)
		}
		if string(resp.AudioData) != string(audioData) {
			t.Errorf("AudioData 不匹配")
		}
		if resp.Duration == nil || *resp.Duration != duration {
			t.Errorf("Duration 应为 %f，实际为 %v", duration, resp.Duration)
		}
		if resp.Format != "wav" {
			t.Errorf("Format 应为 %q，实际为 %q", "wav", resp.Format)
		}
	})
}

// TestNewVideoGenerationResponse 测试 VideoGenerationResponse 构造
func TestNewVideoGenerationResponse(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		resp := NewVideoGenerationResponse()
		if resp.Model != "" {
			t.Errorf("Model 应为空字符串，实际为 %q", resp.Model)
		}
		if resp.Format != "mp4" {
			t.Errorf("Format 默认应为 %q，实际为 %q", "mp4", resp.Format)
		}
		if resp.VideoURL != nil {
			t.Errorf("VideoURL 应为 nil，实际为 %v", resp.VideoURL)
		}
		if resp.VideoData != nil {
			t.Errorf("VideoData 应为 nil，实际为 %v", resp.VideoData)
		}
		if resp.Duration != nil {
			t.Errorf("Duration 应为 nil，实际为 %v", resp.Duration)
		}
		if resp.Resolution != "" {
			t.Errorf("Resolution 应为空字符串，实际为 %q", resp.Resolution)
		}
	})

	t.Run("全部选项", func(t *testing.T) {
		videoURL := "https://example.com/video.mp4"
		videoData := []byte("fake-video-data")
		duration := 5.0

		resp := NewVideoGenerationResponse(
			WithVideoModel("wan-v2"),
			WithVideoURL(videoURL),
			WithVideoData(videoData),
			WithVideoDuration(duration),
			WithResolution("1920x1080"),
			WithVideoFormat("avi"),
		)
		if resp.Model != "wan-v2" {
			t.Errorf("Model 应为 %q，实际为 %q", "wan-v2", resp.Model)
		}
		if resp.VideoURL == nil || *resp.VideoURL != videoURL {
			t.Errorf("VideoURL 应为 %q，实际为 %v", videoURL, resp.VideoURL)
		}
		if string(resp.VideoData) != string(videoData) {
			t.Errorf("VideoData 不匹配")
		}
		if resp.Duration == nil || *resp.Duration != duration {
			t.Errorf("Duration 应为 %f，实际为 %v", duration, resp.Duration)
		}
		if resp.Resolution != "1920x1080" {
			t.Errorf("Resolution 应为 %q，实际为 %q", "1920x1080", resp.Resolution)
		}
		if resp.Format != "avi" {
			t.Errorf("Format 应为 %q，实际为 %q", "avi", resp.Format)
		}
	})
}

// TestGenerationResponseJSON 测试 JSON 序列化与反序列化
func TestGenerationResponseJSON(t *testing.T) {
	t.Run("GenerationResponse_序列化_空字段", func(t *testing.T) {
		resp := NewGenerationResponse()
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		// Model 为空字符串 + omitempty，不应出现在 JSON 中
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if _, ok := m["model"]; ok {
			t.Errorf("空 Model 不应出现在 JSON 中，实际为 %s", string(data))
		}
	})

	t.Run("GenerationResponse_序列化_有Model", func(t *testing.T) {
		resp := NewGenerationResponse(WithModel("gpt-4"))
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if m["model"] != "gpt-4" {
			t.Errorf("model 应为 %q，实际为 %v", "gpt-4", m["model"])
		}
	})

	t.Run("ImageGenerationResponse_序列化", func(t *testing.T) {
		created := int64(1700000000)
		resp := NewImageGenerationResponse(
			WithImageModel("dall-e-3"),
			WithImages([]string{"https://example.com/img1.png"}),
			WithCreated(created),
		)
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if m["model"] != "dall-e-3" {
			t.Errorf("model 应为 %q，实际为 %v", "dall-e-3", m["model"])
		}
		imgs, ok := m["images"].([]any)
		if !ok || len(imgs) != 1 {
			t.Errorf("images 长度应为 1，实际为 %v", m["images"])
		}
		if m["created"] != float64(created) {
			t.Errorf("created 应为 %d，实际为 %v", created, m["created"])
		}
		// ImagesBase64 未设置，不应出现
		if _, ok := m["images_base64"]; ok {
			t.Errorf("未设置的 images_base64 不应出现在 JSON 中")
		}
	})

	t.Run("AudioGenerationResponse_序列化", func(t *testing.T) {
		audioURL := "https://example.com/audio.mp3"
		resp := NewAudioGenerationResponse(
			WithAudioModel("tts-1"),
			WithAudioURL(audioURL),
			WithDuration(3.5),
		)
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if m["model"] != "tts-1" {
			t.Errorf("model 应为 %q，实际为 %v", "tts-1", m["model"])
		}
		if m["audio_url"] != audioURL {
			t.Errorf("audio_url 应为 %q，实际为 %v", audioURL, m["audio_url"])
		}
		if m["format"] != "mp3" {
			t.Errorf("format 应为 %q，实际为 %v", "mp3", m["format"])
		}
	})

	t.Run("VideoGenerationResponse_序列化", func(t *testing.T) {
		videoURL := "https://example.com/video.mp4"
		resp := NewVideoGenerationResponse(
			WithVideoModel("wan-v2"),
			WithVideoURL(videoURL),
			WithResolution("1920x1080"),
		)
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if m["model"] != "wan-v2" {
			t.Errorf("model 应为 %q，实际为 %v", "wan-v2", m["model"])
		}
		if m["video_url"] != videoURL {
			t.Errorf("video_url 应为 %q，实际为 %v", videoURL, m["video_url"])
		}
		if m["format"] != "mp4" {
			t.Errorf("format 应为 %q，实际为 %v", "mp4", m["format"])
		}
		if m["resolution"] != "1920x1080" {
			t.Errorf("resolution 应为 %q，实际为 %v", "1920x1080", m["resolution"])
		}
	})
}

// TestGenerationResponseJSONRoundTrip 测试 JSON 往返一致性
func TestGenerationResponseJSONRoundTrip(t *testing.T) {
	t.Run("GenerationResponse", func(t *testing.T) {
		original := NewGenerationResponse(WithModel("gpt-4"))
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var restored GenerationResponse
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if restored.Model != original.Model {
			t.Errorf("Model 往返不一致: 期望 %q，实际 %q", original.Model, restored.Model)
		}
	})

	t.Run("ImageGenerationResponse", func(t *testing.T) {
		created := int64(1700000000)
		original := NewImageGenerationResponse(
			WithImageModel("dall-e-3"),
			WithImages([]string{"https://example.com/img1.png"}),
			WithImagesBase64([]string{"iVBORw0KGgo="}),
			WithCreated(created),
		)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var restored ImageGenerationResponse
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if restored.Model != original.Model {
			t.Errorf("Model 往返不一致: 期望 %q，实际 %q", original.Model, restored.Model)
		}
		if len(restored.Images) != len(original.Images) {
			t.Errorf("Images 往返不一致: 期望 %d，实际 %d", len(original.Images), len(restored.Images))
		}
		if len(restored.ImagesBase64) != len(original.ImagesBase64) {
			t.Errorf("ImagesBase64 往返不一致: 期望 %d，实际 %d", len(original.ImagesBase64), len(restored.ImagesBase64))
		}
		if restored.Created == nil || *restored.Created != *original.Created {
			t.Errorf("Created 往返不一致: 期望 %d，实际 %v", *original.Created, restored.Created)
		}
	})

	t.Run("AudioGenerationResponse", func(t *testing.T) {
		audioURL := "https://example.com/audio.mp3"
		duration := 3.5
		original := NewAudioGenerationResponse(
			WithAudioModel("tts-1"),
			WithAudioURL(audioURL),
			WithDuration(duration),
		)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var restored AudioGenerationResponse
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if restored.Model != original.Model {
			t.Errorf("Model 往返不一致: 期望 %q，实际 %q", original.Model, restored.Model)
		}
		if restored.AudioURL == nil || *restored.AudioURL != audioURL {
			t.Errorf("AudioURL 往返不一致: 期望 %q，实际 %v", audioURL, restored.AudioURL)
		}
		if restored.Duration == nil || *restored.Duration != duration {
			t.Errorf("Duration 往返不一致: 期望 %f，实际 %v", duration, restored.Duration)
		}
		if restored.Format != "mp3" {
			t.Errorf("Format 往返不一致: 期望 %q，实际 %q", "mp3", restored.Format)
		}
	})

	t.Run("VideoGenerationResponse", func(t *testing.T) {
		videoURL := "https://example.com/video.mp4"
		duration := 5.0
		original := NewVideoGenerationResponse(
			WithVideoModel("wan-v2"),
			WithVideoURL(videoURL),
			WithVideoDuration(duration),
			WithResolution("1920x1080"),
		)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var restored VideoGenerationResponse
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if restored.Model != original.Model {
			t.Errorf("Model 往返不一致: 期望 %q，实际 %q", original.Model, restored.Model)
		}
		if restored.VideoURL == nil || *restored.VideoURL != videoURL {
			t.Errorf("VideoURL 往返不一致: 期望 %q，实际 %v", videoURL, restored.VideoURL)
		}
		if restored.Duration == nil || *restored.Duration != duration {
			t.Errorf("Duration 往返不一致: 期望 %f，实际 %v", duration, restored.Duration)
		}
		if restored.Resolution != "1920x1080" {
			t.Errorf("Resolution 往返不一致: 期望 %q，实际 %q", "1920x1080", restored.Resolution)
		}
		if restored.Format != "mp4" {
			t.Errorf("Format 往返不一致: 期望 %q，实际 %q", "mp4", restored.Format)
		}
	})
}

// TestGenerationResponseByteBase64 测试 []byte 字段的 Base64 编解码
func TestGenerationResponseByteBase64(t *testing.T) {
	t.Run("AudioData_Base64编解码", func(t *testing.T) {
		audioData := []byte("fake-audio-binary-data")
		original := NewAudioGenerationResponse(
			WithAudioModel("tts-1"),
			WithAudioData(audioData),
		)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}

		// 验证 JSON 中 audio_data 为 Base64 字符串
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		audioDataStr, ok := m["audio_data"].(string)
		if !ok {
			t.Fatalf("audio_data 应为字符串，实际为 %T", m["audio_data"])
		}
		if audioDataStr == "" {
			t.Error("audio_data 不应为空字符串")
		}

		// 往返验证
		var restored AudioGenerationResponse
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if string(restored.AudioData) != string(audioData) {
			t.Errorf("AudioData 往返不一致: 期望 %q，实际 %q", string(audioData), string(restored.AudioData))
		}
	})

	t.Run("VideoData_Base64编解码", func(t *testing.T) {
		videoData := []byte("fake-video-binary-data")
		original := NewVideoGenerationResponse(
			WithVideoModel("wan-v2"),
			WithVideoData(videoData),
		)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}

		// 验证 JSON 中 video_data 为 Base64 字符串
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		videoDataStr, ok := m["video_data"].(string)
		if !ok {
			t.Fatalf("video_data 应为字符串，实际为 %T", m["video_data"])
		}
		if videoDataStr == "" {
			t.Error("video_data 不应为空字符串")
		}

		// 往返验证
		var restored VideoGenerationResponse
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if string(restored.VideoData) != string(videoData) {
			t.Errorf("VideoData 往返不一致: 期望 %q，实际 %q", string(videoData), string(restored.VideoData))
		}
	})

	t.Run("空byte切片omitempty", func(t *testing.T) {
		// 空 []byte 也应被 omitempty 忽略
		resp := NewAudioGenerationResponse(WithAudioData([]byte{}))
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if _, ok := m["audio_data"]; ok {
			t.Errorf("空 AudioData 不应出现在 JSON 中，实际为 %s", string(data))
		}
	})
}

// TestGenerationResponseEmbeddedModel 测试嵌入 GenerationResponse.Model 的序列化
func TestGenerationResponseEmbeddedModel(t *testing.T) {
	t.Run("嵌入Model字段在JSON顶层", func(t *testing.T) {
		resp := NewImageGenerationResponse(WithImageModel("dall-e-3"))
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		// 验证 model 字段出现在 JSON 顶层（而非嵌套在 GenerationResponse 下）
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("解析 JSON 失败: %v", err)
		}
		if m["model"] != "dall-e-3" {
			t.Errorf("嵌入的 Model 应出现在 JSON 顶层，实际为 %v", m["model"])
		}
		// 不应有嵌套对象
		if _, ok := m["GenerationResponse"]; ok {
			t.Error("不应出现嵌套的 GenerationResponse 键")
		}
	})

	t.Run("三种子类型嵌入Model", func(t *testing.T) {
		img := NewImageGenerationResponse(WithImageModel("dall-e-3"))
		audio := NewAudioGenerationResponse(WithAudioModel("tts-1"))
		video := NewVideoGenerationResponse(WithVideoModel("wan-v2"))

		for name, resp := range map[string]struct{ Model string }{
			"ImageGenerationResponse": {Model: img.Model},
			"AudioGenerationResponse": {Model: audio.Model},
			"VideoGenerationResponse": {Model: video.Model},
		} {
			if resp.Model == "" {
				t.Errorf("%s 的 Model 不应为空", name)
			}
		}
	})
}

// TestGenerationResponseUnmarshalFromJSON 测试从原始 JSON 反序列化
func TestGenerationResponseUnmarshalFromJSON(t *testing.T) {
	t.Run("ImageGenerationResponse_从JSON", func(t *testing.T) {
		jsonStr := `{"model":"dall-e-3","images":["https://example.com/img1.png"],"images_base64":["iVBORw0KGgo="],"created":1700000000}`
		var resp ImageGenerationResponse
		if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if resp.Model != "dall-e-3" {
			t.Errorf("Model 应为 %q，实际为 %q", "dall-e-3", resp.Model)
		}
		if len(resp.Images) != 1 || resp.Images[0] != "https://example.com/img1.png" {
			t.Errorf("Images 不匹配: %v", resp.Images)
		}
		if len(resp.ImagesBase64) != 1 || resp.ImagesBase64[0] != "iVBORw0KGgo=" {
			t.Errorf("ImagesBase64 不匹配: %v", resp.ImagesBase64)
		}
		if resp.Created == nil || *resp.Created != 1700000000 {
			t.Errorf("Created 应为 1700000000，实际为 %v", resp.Created)
		}
	})

	t.Run("AudioGenerationResponse_从JSON", func(t *testing.T) {
		jsonStr := `{"model":"tts-1","audio_url":"https://example.com/audio.mp3","format":"wav","duration":3.5}`
		var resp AudioGenerationResponse
		if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if resp.Model != "tts-1" {
			t.Errorf("Model 应为 %q，实际为 %q", "tts-1", resp.Model)
		}
		if resp.AudioURL == nil || *resp.AudioURL != "https://example.com/audio.mp3" {
			t.Errorf("AudioURL 不匹配: %v", resp.AudioURL)
		}
		if resp.Format != "wav" {
			t.Errorf("Format 应为 %q，实际为 %q", "wav", resp.Format)
		}
		if resp.Duration == nil || *resp.Duration != 3.5 {
			t.Errorf("Duration 应为 3.5，实际为 %v", resp.Duration)
		}
	})

	t.Run("VideoGenerationResponse_从JSON", func(t *testing.T) {
		jsonStr := `{"model":"wan-v2","video_url":"https://example.com/video.mp4","resolution":"1920x1080","format":"mp4","duration":5}`
		var resp VideoGenerationResponse
		if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if resp.Model != "wan-v2" {
			t.Errorf("Model 应为 %q，实际为 %q", "wan-v2", resp.Model)
		}
		if resp.VideoURL == nil || *resp.VideoURL != "https://example.com/video.mp4" {
			t.Errorf("VideoURL 不匹配: %v", resp.VideoURL)
		}
		if resp.Resolution != "1920x1080" {
			t.Errorf("Resolution 应为 %q，实际为 %q", "1920x1080", resp.Resolution)
		}
		if resp.Format != "mp4" {
			t.Errorf("Format 应为 %q，实际为 %q", "mp4", resp.Format)
		}
		if resp.Duration == nil || *resp.Duration != 5.0 {
			t.Errorf("Duration 应为 5.0，实际为 %v", resp.Duration)
		}
	})

	t.Run("AudioGenerationResponse_默认Format", func(t *testing.T) {
		// JSON 中缺少 format 字段时，反序列化后 Format 应为空字符串
		// （构造函数的默认值 "mp3" 仅在通过 NewAudioGenerationResponse 创建时生效）
		jsonStr := `{"model":"tts-1"}`
		var resp AudioGenerationResponse
		if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if resp.Format != "" {
			t.Errorf("从 JSON 反序列化时 Format 应为空字符串（零值），实际为 %q", resp.Format)
		}
	})
}

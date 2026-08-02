package multimodal

import (
	"context"
	"fmt"
	"os"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AudioTranscriptionInput audio_transcription 工具的输入参数
type AudioTranscriptionInput struct {
	// AudioPathOrURL 本地音频路径或公网 http(s) 音频 URL
	AudioPathOrURL string `json:"audio_path_or_url"`
}

// AudioQAInput audio_question_answering 工具的输入参数
type AudioQAInput struct {
	// AudioPathOrURL 本地音频路径或公网 http(s) 音频 URL
	AudioPathOrURL string `json:"audio_path_or_url"`
	// Question 要基于音频内容回答的问题
	Question string `json:"question"`
}

// AudioMetadataInput audio_metadata 工具的输入参数
type AudioMetadataInput struct {
	// AudioPathOrURL 本地音频路径或公网 http(s) 音频 URL
	AudioPathOrURL string `json:"audio_path_or_url"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAudioTranscriptionTool 创建音频转写工具。
//
// 对齐 Python: AudioTranscriptionTool.__init__ + AudioTranscriptionTool.invoke
// 使用 TranscribeAudio 接口调用 /audio/transcriptions
func NewAudioTranscriptionTool(
	client modelclients.BaseModelClient,
	config *hschema.AudioModelConfig,
	language, agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("audio_transcription", "AudioTranscriptionTool", language, nil, agentID)

	fn := func(ctx context.Context, input AudioTranscriptionInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: AudioTranscriptionTool.invoke 的 try/except + finally 清理
		result, err := func() (map[string]any, error) {
			// 校验配置（对齐 Python: _require_audio_model_config）
			if config == nil || config.BaseURL == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalAudioConfigInvalid,
					exception.WithMsg("audio model config invalid: base_url is required"),
				)
			}

			// 解析路径（可能下载到临时文件）
			audioPath, shouldDelete, err := ResolveAudioPath(ctx, input.AudioPathOrURL, config)
			if err != nil {
				return nil, err
			}
			defer func() {
				if shouldDelete {
					_ = os.Remove(audioPath)
				}
			}()

			// 调用转写（对齐 Python: _invoke_audio_transcription(config, audio_path)）
			resp, err := client.TranscribeAudio(ctx, audioPath,
				llmschema.WithTranscriptionModel(config.TranscriptionModel),
			)
			if err != nil {
				return nil, err
			}

			return map[string]any{
				"text":  resp.Text,
				"model": config.TranscriptionModel,
			}, nil
		}()
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "audio_transcription").
				Msg("AudioTranscriptionTool 调用失败")
			return nil, exception.NewBaseError(
				exception.StatusToolMultimodalAudioInvokeFailed,
				exception.WithMsg(err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewAudioQATool 创建音频问答工具。
//
// 对齐 Python: AudioQuestionAnsweringTool.__init__ + AudioQuestionAnsweringTool.invoke
// 使用 input_audio content block + chat.completions
func NewAudioQATool(
	client modelclients.BaseModelClient,
	config *hschema.AudioModelConfig,
	language, agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("audio_question_answering", "AudioQuestionAnsweringTool", language, nil, agentID)

	fn := func(ctx context.Context, input AudioQAInput, opts ...tool.ToolOption) (map[string]any, error) {
		result, err := func() (map[string]any, error) {
			// 校验配置
			if config == nil || config.BaseURL == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalAudioConfigInvalid,
					exception.WithMsg("audio model config invalid: base_url is required"),
				)
			}

			// 解析路径
			audioPath, shouldDelete, err := ResolveAudioPath(ctx, input.AudioPathOrURL, config)
			if err != nil {
				return nil, err
			}
			defer func() {
				if shouldDelete {
					_ = os.Remove(audioPath)
				}
			}()

			// 编码音频 + 获取时长（对齐 Python: _encode_audio_file + _get_audio_duration）
			encoded, format, err := EncodeAudioFile(audioPath)
			if err != nil {
				return nil, err
			}
			duration, _ := GetAudioDuration(audioPath)

			// 构造 input_audio 消息（对齐 Python: _invoke_audio_question_answering）
			// system 消息: "You are a helpful assistant specializing in audio analysis."
			// user 消息: text part + input_audio part
			audioPart := llmschema.ContentPart{
				Type:       "input_audio",
				InputAudio: &llmschema.InputAudio{Data: encoded, Format: format},
			}
			questionText := fmt.Sprintf("Answer the following question based on the given audio information:\n\n%s", input.Question)
			textPart := llmschema.ContentPart{Type: "text", Text: questionText}
			userMsg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(textPart, audioPart))
			systemMsg := llmschema.NewSystemMessage(audioQASystemPrompt)

			messages := modelclients.NewMessagesParam(systemMsg, userMsg)

			// 调用 chat.completions（对齐 Python: client.chat.completions.create）
			resp, err := client.Invoke(ctx, messages,
				modelclients.WithInvokeModel(config.QAModel),
			)
			if err != nil {
				return nil, err
			}

			answer := ExtractResponseText(resp)
			if answer == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalAudioInvokeFailed,
					exception.WithMsg("audio question answering returned empty content"),
				)
			}

			return map[string]any{
				"answer":            answer,
				"duration_seconds":  duration,
				"model":             config.QAModel,
			}, nil
		}()
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "audio_question_answering").
				Msg("AudioQATool 调用失败")
			return nil, exception.NewBaseError(
				exception.StatusToolMultimodalAudioInvokeFailed,
				exception.WithMsg(err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewAudioMetadataTool 创建音频元数据工具。
//
// 对齐 Python: AudioMetadataTool.__init__ + AudioMetadataTool.invoke
// 获取时长 + ACRCloud 识别
func NewAudioMetadataTool(
	client modelclients.BaseModelClient,
	config *hschema.AudioModelConfig,
	language, agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("audio_metadata", "AudioMetadataTool", language, nil, agentID)

	fn := func(ctx context.Context, input AudioMetadataInput, opts ...tool.ToolOption) (map[string]any, error) {
		result, err := func() (map[string]any, error) {
			// 校验配置
			if config == nil || config.BaseURL == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalAudioConfigInvalid,
					exception.WithMsg("audio model config invalid: base_url is required"),
				)
			}

			// 解析路径
			audioPath, shouldDelete, err := ResolveAudioPath(ctx, input.AudioPathOrURL, config)
			if err != nil {
				return nil, err
			}
			defer func() {
				if shouldDelete {
					_ = os.Remove(audioPath)
				}
			}()

			// 获取时长（对齐 Python: _get_audio_duration）
			duration, _ := GetAudioDuration(audioPath)

			// 初始化元数据（对齐 Python: result = {"duration_seconds": round(duration, 2), ...}）
			metadata := map[string]any{
				"duration_seconds": roundDuration(duration),
				"title":            nil,
				"artist":           nil,
				"release_date":     nil,
				"score":            nil,
				"identified":       false,
				"note":             nil,
			}

			// ACR 识别（对齐 Python: _invoke_audio_metadata 的条件判断）
			if config.ACRAccessKey == "" || config.ACRAccessSecret == "" {
				// 对齐 Python: "Title and artist identification is disabled because ACR credentials are not configured."
				metadata["note"] = "Title and artist identification is disabled because ACR credentials are not configured."
				return metadata, nil
			}

			if duration > 15.0 {
				// 对齐 Python: "Audio metadata identification works best for clips shorter than 15 seconds."
				metadata["note"] = "Audio metadata identification works best for clips shorter than 15 seconds."
				return metadata, nil
			}

			// 调用 ACR（对齐 Python: _invoke_audio_metadata）
			acrResult, err := InvokeACRMetadata(ctx, audioPath, config)
			if err == nil && acrResult != nil {
				// 合并 ACR 结果
				for k, v := range acrResult {
					if k == "duration_seconds" {
						continue // 时长用我们自己计算的
					}
					metadata[k] = v
				}
				metadata["duration_seconds"] = roundDuration(duration)
				metadata["identified"] = true
			} else {
				metadata["note"] = fmt.Sprintf("ACR identification failed: %s", err.Error())
			}

			return metadata, nil
		}()
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "audio_metadata").
				Msg("AudioMetadataTool 调用失败")
			return nil, exception.NewBaseError(
				exception.StatusToolMultimodalAudioInvokeFailed,
				exception.WithMsg(err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// CreateAudioTools 创建音频工具集。
//
// 对齐 Python: create_audio_tools(language, audio_model_config, agent_id) -> list[Tool]
func CreateAudioTools(
	client modelclients.BaseModelClient,
	config *hschema.AudioModelConfig,
	language, agentID string,
) []tool.Tool {
	return []tool.Tool{
		NewAudioTranscriptionTool(client, config, language, agentID),
		NewAudioQATool(client, config, language, agentID),
		NewAudioMetadataTool(client, config, language, agentID),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// roundDuration 四舍五入到2位小数（对齐 Python: round(duration, 2)）
func roundDuration(d float64) float64 {
	return float64(int(d*100+0.5)) / 100
}

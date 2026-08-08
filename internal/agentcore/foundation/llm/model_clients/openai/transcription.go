package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// audioTranscriptionsPath 音频转写 API 路径
const audioTranscriptionsPath = "/audio/transcriptions"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TranscribeAudio 调用 OpenAI 音频转写 API，将音频文件转换为文本。
//
// 对齐 Python: _invoke_audio_transcription(config, audio_path)
// 使用 multipart/form-data POST /audio/transcriptions endpoint
func (c *OpenAIModelClient) TranscribeAudio(
	ctx context.Context,
	audioPath string,
	opts ...llmschema.TranscribeAudioOption,
) (*llmschema.TranscriptionResponse, error) {
	params := llmschema.NewTranscriptionParams(opts...)

	// 1. 确定模型名称（优先使用参数指定，降级为客户端配置）
	model := params.Model
	if model == "" && c.ModelConfig != nil {
		model = c.ModelConfig.ModelName
	}
	if model == "" {
		model = "whisper-1"
	}

	// 2. 打开音频文件
	file, err := os.Open(audioPath)
	if err != nil {
		logger.Error(logComponent).Str("audio_path", audioPath).Err(err).
			Str("event_type", "LLM_CALL_ERROR").Str("method", "TranscribeAudio").
			Msg("打开音频文件失败")
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("打开音频文件失败: %s", audioPath)),
		)
	}
	defer file.Close()

	// 3. 构造 multipart/form-data 请求体
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// 添加 model 字段
	if err := writer.WriteField("model", model); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("写入 model 字段失败: %s", err.Error())),
		)
	}

	// 添加 language 字段（可选）
	if params.Language != "" {
		if err := writer.WriteField("language", params.Language); err != nil {
			return nil, exception.NewBaseError(
				exception.StatusModelCallFailed,
				exception.WithMsg(fmt.Sprintf("写入 language 字段失败: %s", err.Error())),
			)
		}
	}

	// 添加 file 字段（音频文件）
	fileName := filepath.Base(audioPath)
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("创建 file 字段失败: %s", err.Error())),
		)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("写入音频数据失败: %s", err.Error())),
		)
	}

	// 关闭 multipart writer（必须，写入结尾标记）
	if err := writer.Close(); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("关闭 multipart writer 失败: %s", err.Error())),
		)
	}

	// 4. 构建 API URL
	apiURL := strings.TrimRight(c.ClientConfig.APIBase, "/") + audioTranscriptionsPath

	// 5. 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &requestBody)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("创建 HTTP 请求失败: %s", err.Error())),
		)
	}

	// 设置请求头
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.ClientConfig.APIKey)

	// 合并自定义请求头
	effectiveHeaders := c.BuildEffectiveHeaders(nil)
	for k, v := range effectiveHeaders {
		req.Header.Set(k, v)
	}

	// 6. 构建 HTTP 客户端
	client, err := buildHTTPClient(nil, c.ClientConfig.VerifySSL, c.ClientConfig.SSLCert)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("构建 HTTP 客户端失败: %s", err.Error())),
		)
	}

	// 7. 日志记录（对齐 Python: llm_logger.info 事件记录）
	logger.Info(logComponent).
		Str("event_type", "llm_call_start").
		Str("model_name", model).
		Str("method", "TranscribeAudio").
		Str("audio_path", audioPath).
		Str("model_provider", c.ClientConfig.ClientProvider).
		Msg("音频转写请求已就绪")

	// 8. 发送请求
	resp, err := client.Do(req)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").
			Str("method", "TranscribeAudio").Str("model_name", model).Err(err).
			Msg("音频转写 HTTP 请求失败")
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("音频转写 HTTP 请求失败: %s", err.Error())),
		)
	}
	defer func() { _ = resp.Body.Close() }()

	// 9. 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		return nil, c.HandleHTTPError(resp)
	}

	// 10. 解析响应
	var transcriptionResp llmschema.TranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&transcriptionResp); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").
			Str("method", "TranscribeAudio").Str("model_name", model).Err(err).
			Msg("音频转写响应解析失败")
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("音频转写响应解析失败: %s", err.Error())),
		)
	}

	// 11. 检查空响应（对齐 Python: if not text: raise ValueError）
	if transcriptionResp.Text == "" {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("音频转写返回空内容"),
		)
	}

	// 12. 日志记录成功
	logger.Info(logComponent).
		Str("event_type", "llm_call_end").
		Str("model_name", model).
		Str("method", "TranscribeAudio").
		Int("text_length", len(transcriptionResp.Text)).
		Msg("音频转写完成")

	return &transcriptionResp, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

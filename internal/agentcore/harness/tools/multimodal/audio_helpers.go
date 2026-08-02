package multimodal

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultUserAgent 默认 User-Agent（对齐 Python: DEFAULT_USER_AGENT）
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// audioQASystemPrompt 音频问答系统提示词（对齐 Python: _invoke_audio_question_answering 中的 system prompt）
const audioQASystemPrompt = "You are a helpful assistant specializing in audio analysis."

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveAudioPath 解析音频路径，URL 下载到临时文件。
//
// 对齐 Python: _resolve_audio_path(audio_path_or_url, config)
// 返回 (localPath, shouldDelete, error)
func ResolveAudioPath(
	ctx context.Context,
	audioPathOrURL string,
	config *hschema.AudioModelConfig,
) (string, bool, error) {
	// 1. sandbox 路径检查（对齐 Python: if SANDBOX_PATH_MARKER in audio_path_or_url）
	if strings.Contains(audioPathOrURL, sandboxPathMarker) {
		return "", false, exception.NewBaseError(
			exception.StatusToolMultimodalAudioConfigInvalid,
			exception.WithMsg("audio tools cannot access sandbox-only paths. Use a local path outside the sandbox or an https URL."),
		)
	}

	// 2. HTTP URL → 下载到临时文件（对齐 Python: requests.get → tempfile.NamedTemporaryFile）
	if isHTTPURL(audioPathOrURL) {
		tmpFile, err := downloadAudioToTemp(ctx, audioPathOrURL, config)
		if err != nil {
			return "", false, err
		}
		return tmpFile, true, nil
	}

	// 3. 本地文件 → 验证存在性（对齐 Python: if not audio_path.exists()）
	if _, err := os.Stat(audioPathOrURL); err != nil {
		return "", false, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("audio path does not exist or is not a file: %s", audioPathOrURL)),
		)
	}
	return audioPathOrURL, false, nil
}

// GetAudioDuration 获取音频时长（秒）。
//
// 对齐 Python: _get_audio_duration(audio_path)
// WAV → Go 标准库解析 header；非 WAV → ffprobe（如果可用）；
// 全部失败 → 返回 0（Go 降级策略比 Python ValueError 更友好）
func GetAudioDuration(audioPath string) (float64, error) {
	// 1. 尝试 WAV 解析
	duration, err := parseWAVDuration(audioPath)
	if err == nil && duration > 0 {
		return duration, nil
	}

	// 2. 降级：ffprobe
	duration, err = ffprobeDuration(audioPath)
	if err == nil && duration > 0 {
		return duration, nil
	}

	// 3. 全部失败 → 返回 0（不返回错误，对齐 Python 行为但更友好）
	logger.Warn(logComponent).Str("audio_path", audioPath).
		Msg("无法获取音频时长，将返回 0")
	return 0, nil
}

// EncodeAudioFile 将音频文件 base64 编码，推断格式。
//
// 对齐 Python: _encode_audio_file(audio_path)
// 返回 (encodedString, format)
func EncodeAudioFile(audioPath string) (string, string, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", "", exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("读取音频文件失败: %s", err.Error())),
		)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	format := guessAudioFormat(audioPath, data)
	return encoded, format, nil
}

// InvokeACRMetadata 调用 ACRCloud 识别音频元数据。
//
// 对齐 Python: _invoke_audio_metadata(config, audio_path)
// HMAC-SHA1 签名 + multipart POST
func InvokeACRMetadata(
	ctx context.Context,
	audioPath string,
	config *hschema.AudioModelConfig,
) (map[string]any, error) {
	// 1. 计算 HMAC-SHA1 签名（对齐 Python: hmac.new + base64.b64encode）
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	stringToSign := "POST\n/v1/identify\n" + config.ACRAccessKey + "\naudio\n1\n" + timestamp
	mac := hmac.New(sha1.New, []byte(config.ACRAccessSecret))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// 2. 读取音频数据
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("读取音频文件失败: %s", err.Error())),
		)
	}

	// 3. 推断音频 MIME 格式（对齐 Python: mime_type, _ = mimetypes.guess_type → file_format）
	fileFormat := guessAudioFormat(audioPath, audioData)

	// 4. 构造 multipart/form-data POST 请求
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// 添加 data 字段
	_ = writer.WriteField("access_key", config.ACRAccessKey)
	_ = writer.WriteField("sample_bytes", fmt.Sprintf("%d", len(audioData)))
	_ = writer.WriteField("timestamp", timestamp)
	_ = writer.WriteField("signature", signature)
	_ = writer.WriteField("data_type", "audio")
	_ = writer.WriteField("signature_version", "1")

	// 添加 sample 字段（音频文件）
	// 对齐 Python: ("sample", (os.path.basename(audio_path), audio_file, file_format))
	// CreateFormFile 使用推断的 MIME 格式作为 Content-Type
	mimeType := "audio/" + fileFormat
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="sample"; filename="%s"`, filepath.Base(audioPath)))
	h.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(h)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("创建 sample 字段失败: %s", err.Error())),
		)
	}
	if _, err := io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("写入音频数据失败: %s", err.Error())),
		)
	}

	if err := writer.Close(); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("关闭 multipart writer 失败: %s", err.Error())),
		)
	}

	// 5. 发送 POST 请求到 ACRCloud
	timeout := time.Duration(config.HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.ACRBaseURL, &requestBody)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("创建 ACR HTTP 请求失败: %s", err.Error())),
		)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").
			Str("method", "InvokeACRMetadata").Err(err).
			Msg("ACRCloud HTTP 请求失败")
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("ACRCloud HTTP 请求失败: %s", err.Error())),
		)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("ACRCloud HTTP %d: non-success status", resp.StatusCode)),
		)
	}

	// 6. 解析响应（对齐 Python: payload.get("metadata", {}) → humming/music 提取）
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("读取 ACR 响应体失败: %s", err.Error())),
		)
	}

	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("解析 ACR JSON 响应失败: %s", err.Error())),
		)
	}

	metadata := payload["metadata"]
	if metadata == nil {
		return map[string]any{
			"identified": false,
			"note":       "No metadata found for the given audio file.",
		}, nil
	}

	metadataMap, ok := metadata.(map[string]any)
	if !ok {
		return map[string]any{
			"identified": false,
			"note":       "metadata format unexpected",
		}, nil
	}

	// 对齐 Python: humming → 排序取最佳; music → 取 music[0]
	result := map[string]any{"identified": false}

	if humming, ok := metadataMap["humming"].([]any); ok && len(humming) > 0 {
		// 按持续时间降序排序（对齐 Python: sorted by duration_ms）
		best := findBestHumming(humming)
		if best != nil {
			result["title"] = best["title"]
			result["artist"] = extractFirstArtistName(best)
			result["release_date"] = best["release_date"]
			result["score"] = best["score"]
			result["identified"] = true
			return result, nil
		}
	}

	if music, ok := metadataMap["music"].([]any); ok && len(music) > 0 {
		best := music[0]
		if bestMap, ok := best.(map[string]any); ok {
			result["title"] = bestMap["title"]
			result["artist"] = extractFirstArtistName(bestMap)
			result["release_date"] = bestMap["release_date"]
			result["identified"] = true
			return result, nil
		}
	}

	result["note"] = "No metadata found for the given audio file."
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// downloadAudioToTemp 从 URL 下载音频到临时文件。
//
// 对齐 Python: requests.get(stream=True) → tempfile.NamedTemporaryFile
func downloadAudioToTemp(
	ctx context.Context,
	audioURL string,
	config *hschema.AudioModelConfig,
) (string, error) {
	timeout := time.Duration(config.HTTPTimeout) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("创建下载请求失败: %s", err.Error())),
		)
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("下载音频失败: %s", err.Error())),
		)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("下载音频 HTTP %d: %s", resp.StatusCode, resp.Status)),
		)
	}

	// 推断扩展名（对齐 Python: _get_audio_extension）
	contentType := resp.Header.Get("Content-Type")
	suffix := getAudioExtension(audioURL, contentType)

	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "audio_*"+suffix)
	if err != nil {
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("创建临时文件失败: %s", err.Error())),
		)
	}

	// 流式写入，检查大小限制（对齐 Python: bytes_written > config.max_audio_bytes）
	maxBytes := config.MaxAudioBytes
	if maxBytes <= 0 {
		maxBytes = 25 * 1024 * 1024 // 25MB 默认值
	}
	bytesWritten := 0
	buf := make([]byte, 64*1024) // 64KB chunks（对齐 Python: chunk_size=1024*64）
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			bytesWritten += n
			if bytesWritten > maxBytes {
				_ = tmpFile.Close()
				_ = os.Remove(tmpFile.Name())
				return "", exception.NewBaseError(
					exception.StatusToolMultimodalAudioInvokeFailed,
					exception.WithMsg("audio file exceeds size limit"),
				)
			}
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpFile.Name())
				return "", exception.NewBaseError(
					exception.StatusToolMultimodalAudioInvokeFailed,
					exception.WithMsg(fmt.Sprintf("写入临时文件失败: %s", writeErr.Error())),
				)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFile.Name())
			return "", exception.NewBaseError(
				exception.StatusToolMultimodalAudioInvokeFailed,
				exception.WithMsg(fmt.Sprintf("读取下载流失败: %s", err.Error())),
			)
		}
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", exception.NewBaseError(
			exception.StatusToolMultimodalAudioInvokeFailed,
			exception.WithMsg(fmt.Sprintf("关闭临时文件失败: %s", err.Error())),
		)
	}

	logger.Info(logComponent).
		Str("audio_url", audioURL).
		Str("temp_path", tmpFile.Name()).
		Int("bytes_written", bytesWritten).
		Msg("音频下载到临时文件完成")

	return tmpFile.Name(), nil
}

// parseWAVDuration 解析 WAV 文件获取时长。
//
// 对齐 Python: wave.open → frames/rate = duration
func parseWAVDuration(audioPath string) (float64, error) {
	ext := strings.ToLower(filepath.Ext(audioPath))
	if ext != ".wav" && ext != ".wave" {
		return 0, fmt.Errorf("not a WAV file")
	}

	data, err := os.ReadFile(audioPath)
	if err != nil {
		return 0, err
	}

	// WAV 文件结构:
	// RIFF header (12 bytes): "RIFF" + size + "WAVE"
	// fmt chunk (24+ bytes): "fmt " + size + audioFormat + numChannels + sampleRate + byteRate + blockAlign + bitsPerSample
	if len(data) < 44 {
		return 0, fmt.Errorf("WAV file too short")
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a valid WAV file")
	}

	// 找 fmt chunk
	offset := 12
	for offset < len(data)-8 {
		chunkID := string(data[offset:offset+4])
		chunkSize := uint32(data[offset+4]) | uint32(data[offset+5])<<8 | uint32(data[offset+6])<<16 | uint32(data[offset+7])<<24
		if chunkID == "fmt " {
			if offset+24 > len(data) {
				return 0, fmt.Errorf("fmt chunk too short")
			}
			sampleRate := uint32(data[offset+12]) | uint32(data[offset+13])<<8 | uint32(data[offset+14])<<16 | uint32(data[offset+15])<<24
			byteRate := uint32(data[offset+16]) | uint32(data[offset+17])<<8 | uint32(data[offset+18])<<16 | uint32(data[offset+19])<<24
			blockAlign := uint16(data[offset+20]) | uint16(data[offset+21])<<8

			// 找 data chunk
			dataOffset := offset + 8 + int(chunkSize)
			for dataOffset < len(data)-8 {
				dataChunkID := string(data[dataOffset:dataOffset+4])
				dataChunkSize := uint32(data[dataOffset+4]) | uint32(data[dataOffset+5])<<8 | uint32(data[dataOffset+6])<<16 | uint32(data[dataOffset+7])<<24
				if dataChunkID == "data" {
					if byteRate > 0 && blockAlign > 0 {
						duration := float64(dataChunkSize) / float64(byteRate)
						if duration > 0 {
							return duration, nil
						}
					}
					// 降级用 sampleRate 和总字节
					if sampleRate > 0 && blockAlign > 0 {
						frames := dataChunkSize / uint32(blockAlign)
						duration := float64(frames) / float64(sampleRate)
						if duration > 0 {
							return duration, nil
						}
					}
				}
				dataOffset += 8 + int(dataChunkSize)
			}
		}
		offset += 8 + int(chunkSize)
	}

	return 0, fmt.Errorf("could not parse WAV duration")
}

// ffprobeDuration 使用 ffprobe 获取音频时长。
//
// 对齐 Python: 使用 mutagen 降级（Go 用 ffprobe 替代 mutagen）
func ffprobeDuration(audioPath string) (float64, error) {
	// 检查 ffprobe 是否可用
	_, err := os.Stat("/usr/bin/ffprobe")
	if err != nil {
		return 0, fmt.Errorf("ffprobe not available")
	}

	output, err := execCommand("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		audioPath,
	)
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %s", err.Error())
	}

	durationStr := strings.TrimSpace(output)
	if durationStr == "" || durationStr == "N/A" {
		return 0, fmt.Errorf("ffprobe returned no duration")
	}

	duration, err := parseFloat(durationStr)
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration parse failed: %s", err.Error())
	}

	return duration, nil
}

// guessAudioFormat 推断音频格式（对齐 Python: _encode_audio_file 的 mime 推断 + format_mapping）
func guessAudioFormat(filePath string, data []byte) string {
	// 先尝试 MIME 检测
	if len(data) >= 512 {
		detected := http.DetectContentType(data[:512])
		if strings.HasPrefix(detected, "audio/") {
			return audioMIMEToFormat(detected)
		}
	}

	// 降级到扩展名推断
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp3", ".mpeg":
		return "mp3"
	case ".wav", ".wave":
		return "wav"
	case ".m4a":
		return "m4a"
	case ".aac":
		return "aac"
	case ".ogg":
		return "ogg"
	case ".flac":
		return "flac"
	case ".wma":
		return "wma"
	default:
		return "mp3"
	}
}

// audioMIMEToFormat 将 MIME type 转为格式字符串（对齐 Python: format_mapping）
func audioMIMEToFormat(mimeType string) string {
	// audio/mpeg → mp3, audio/wav → wav, audio/wave → wav, 其他 → 原名
	parts := strings.Split(mimeType, "/")
	if len(parts) != 2 {
		return "mp3"
	}
	sub := parts[1]
	switch sub {
	case "mpeg":
		return "mp3"
	case "wav", "wave":
		return "wav"
	default:
		return sub
	}
}

// getAudioExtension 推断音频文件扩展名（对齐 Python: _get_audio_extension）
func getAudioExtension(urlStr, contentType string) string {
	// 先从 URL 路径推断
	parsedURL, err := urlParse(urlStr)
	if err == nil {
		path := strings.ToLower(parsedURL)
		audioExts := []string{".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac", ".wma"}
		for _, ext := range audioExts {
			if strings.HasSuffix(path, ext) {
				return ext
			}
		}
	}

	// 从 Content-Type 推断
	loweredType := strings.ToLower(contentType)
	if strings.Contains(loweredType, "mp3") || strings.Contains(loweredType, "mpeg") {
		return ".mp3"
	}
	if strings.Contains(loweredType, "wav") {
		return ".wav"
	}
	if strings.Contains(loweredType, "m4a") {
		return ".m4a"
	}
	if strings.Contains(loweredType, "aac") {
		return ".aac"
	}
	if strings.Contains(loweredType, "ogg") {
		return ".ogg"
	}
	if strings.Contains(loweredType, "flac") {
		return ".flac"
	}
	return ".mp3"
}

// urlParse 从 URL 解析路径
func urlParse(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Path, nil
}

// execCommand 执行外部命令（可被测试替换）
var execCommand = func(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// parseFloat 安全的 float64 解析
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// findBestHumming 在 humming 列表中找到最佳匹配（对齐 Python: sorted by duration_ms, reverse）
func findBestHumming(humming []any) map[string]any {
	var best map[string]any
	bestDuration := 0.0
	for _, item := range humming {
		if itemMap, ok := item.(map[string]any); ok {
			durationMs := 0.0
			if dm, ok := itemMap["duration_ms"].(float64); ok {
				durationMs = dm
			}
			if durationMs > bestDuration {
				bestDuration = durationMs
				best = itemMap
			}
		}
	}
	return best
}

// extractFirstArtistName 从 artists 数组提取第一个 artist 的 name（对齐 Python: artists[0]["name"]）
func extractFirstArtistName(item map[string]any) string {
	if artists, ok := item["artists"].([]any); ok && len(artists) > 0 {
		if first, ok := artists[0].(map[string]any); ok {
			if name, ok := first["name"].(string); ok {
				return name
			}
		}
	}
	return ""
}

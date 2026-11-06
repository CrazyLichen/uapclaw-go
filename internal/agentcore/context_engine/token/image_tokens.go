package token

import (
	"encoding/base64"
	"math"
	"os"
	"strconv"
	"strings"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultImagePlaceholderTokens 读不到尺寸时的图像占位 token 数
	// （典型全高手机截图：85 + 8 块 × 170）
	DefaultImagePlaceholderTokens = 1445
	// imageBaseTokens OpenAI 高精度计费的基础 token
	imageBaseTokens = 85
	// imageTileTokens OpenAI 高精度计费每 512px 块的 token
	imageTileTokens = 170
	// lowDetailTokens detail=low 的固定计费
	lowDetailTokens = 85
	// claudePatchSize Anthropic 28×28 patch 计费
	claudePatchSize = 28
	// claudeMaxImageTokens Anthropic 单图上限
	claudeMaxImageTokens = 1568
	// imageHeaderBase64Chars 嗅探头部时最多解码的 base64 字符数（≈64KB）
	imageHeaderBase64Chars = 87360
	// imagePlaceholderEnv 图像占位 token 的环境变量覆盖键
	imagePlaceholderEnv = "TIKTOKEN_IMAGE_PLACEHOLDER_TOKENS"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateImageTokens 按尺寸估算图像 token：取 OpenAI 分块与 Anthropic patch 两种计费的较大者。
//
// 不嗅探模型名（路由别名下不可靠），宁可估高不估低。
//
// Python: tiktoken_counter._estimate_image_tokens()
func EstimateImageTokens(width, height int) int {
	if width <= 0 || height <= 0 {
		return DefaultImagePlaceholderTokens
	}
	openAI := openAITileTokens(width, height)
	claude := claudePatchTokens(width, height)
	if openAI > claude {
		return openAI
	}
	return claude
}

// ImageBlockTokens 返回图像内容块的计费 token。
//
// 优先级：环境变量覆盖 → detail=low 固定 85 → 尺寸（显式标注优先，其次解 base64 头）
// → DefaultImagePlaceholderTokens。
//
// Python: tiktoken_counter._image_block_tokens()
func ImageBlockTokens(part llm_schema.ContentPart) int {
	if override, ok := imagePlaceholderOverride(); ok {
		return override
	}
	if imageDetail(part) == "low" {
		return lowDetailTokens
	}
	if width, height, ok := imageBlockDimensions(part); ok {
		return EstimateImageTokens(width, height)
	}
	return DefaultImagePlaceholderTokens
}

// IsImagePart 判断内容块是否为图像块。
func IsImagePart(part llm_schema.ContentPart) bool {
	switch part.Type {
	case "image", "image_url":
		return true
	}
	return part.ImageURL != nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// imagePlaceholderOverride 读取环境变量覆盖值。
func imagePlaceholderOverride() (int, bool) {
	raw := strings.TrimSpace(os.Getenv(imagePlaceholderEnv))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// openAITileTokens OpenAI 高精度：先缩进 2048，最短边缩到 768，再按 512 分块。
func openAITileTokens(width, height int) int {
	w, h := float64(width), float64(height)
	if longest := math.Max(w, h); longest > 2048 {
		w, h = w*2048/longest, h*2048/longest
	}
	if shortest := math.Min(w, h); shortest > 768 {
		w, h = w*768/shortest, h*768/shortest
	}
	tiles := math.Ceil(w/512) * math.Ceil(h/512)
	return imageBaseTokens + imageTileTokens*int(tiles)
}

// claudePatchTokens Anthropic：每 28×28 一个 token，封顶 1568。
func claudePatchTokens(width, height int) int {
	tokens := int(math.Ceil(float64(width)/claudePatchSize) * math.Ceil(float64(height)/claudePatchSize))
	if tokens > claudeMaxImageTokens {
		return claudeMaxImageTokens
	}
	return tokens
}

// imageDetail 取 detail 级别（image_url.detail 优先）。
func imageDetail(part llm_schema.ContentPart) string {
	if part.ImageURL != nil && part.ImageURL.Detail != "" {
		return strings.ToLower(part.ImageURL.Detail)
	}
	return "auto"
}

// imageBlockDimensions 取图像尺寸：显式标注优先，其次解 base64 头。
func imageBlockDimensions(part llm_schema.ContentPart) (int, int, bool) {
	if width, height, ok := explicitDimensions(part); ok {
		return width, height, true
	}
	payload := imageBase64Payload(part)
	if payload == "" {
		return 0, 0, false
	}
	if len(payload) > imageHeaderBase64Chars {
		payload = payload[:imageHeaderBase64Chars]
	}
	header, err := base64.StdEncoding.DecodeString(padBase64(payload))
	if err != nil {
		return 0, 0, false
	}
	for _, parser := range []func([]byte) (int, int, bool){pngDimensions, jpegDimensions, gifDimensions} {
		if width, height, ok := parser(header); ok {
			return width, height, true
		}
	}
	return 0, 0, false
}

// explicitDimensions 读取生产方标注的尺寸（width/height 或 dimensions）。
func explicitDimensions(part llm_schema.ContentPart) (int, int, bool) {
	if part.Width != nil && part.Height != nil && *part.Width > 0 && *part.Height > 0 {
		return *part.Width, *part.Height, true
	}
	value := strings.TrimSpace(strings.ToLower(part.Dimensions))
	if value == "" {
		return 0, 0, false
	}
	left, right, found := strings.Cut(value, "x")
	if !found {
		return 0, 0, false
	}
	width, errW := strconv.Atoi(strings.TrimSpace(left))
	height, errH := strconv.Atoi(strings.TrimSpace(right))
	if errW != nil || errH != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// imageBase64Payload 从常见图像块形态里取出 base64 载荷；远程 URL 返回空串。
func imageBase64Payload(part llm_schema.ContentPart) string {
	if part.ImageURL == nil {
		return ""
	}
	url := part.ImageURL.URL
	if !strings.HasPrefix(url, "data:") {
		return ""
	}
	_, payload, found := strings.Cut(url, "base64,")
	if !found {
		return ""
	}
	return payload
}

// padBase64 补齐 base64 长度（截断后可能缺 padding）。
func padBase64(payload string) string {
	if remainder := len(payload) % 4; remainder != 0 {
		payload += strings.Repeat("=", 4-remainder)
	}
	return payload
}

// pngDimensions 解析 PNG IHDR 宽高。
func pngDimensions(header []byte) (int, int, bool) {
	if len(header) < 24 || header[0] != 0x89 || header[1] != 'P' || header[2] != 'N' || header[3] != 'G' {
		return 0, 0, false
	}
	width := int(header[16])<<24 | int(header[17])<<16 | int(header[18])<<8 | int(header[19])
	height := int(header[20])<<24 | int(header[21])<<16 | int(header[22])<<8 | int(header[23])
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// gifDimensions 解析 GIF 逻辑屏幕宽高（小端）。
func gifDimensions(header []byte) (int, int, bool) {
	if len(header) < 10 || string(header[0:3]) != "GIF" {
		return 0, 0, false
	}
	width := int(header[6]) | int(header[7])<<8
	height := int(header[8]) | int(header[9])<<8
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// jpegDimensions 扫描 JPEG 段直到 SOF，取帧高宽。
func jpegDimensions(header []byte) (int, int, bool) {
	if len(header) < 4 || header[0] != 0xFF || header[1] != 0xD8 {
		return 0, 0, false
	}
	offset := 2
	for offset+9 <= len(header) {
		if header[offset] != 0xFF {
			offset++
			continue
		}
		marker := header[offset+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			offset += 2
			continue
		}
		segmentLength := int(header[offset+2])<<8 | int(header[offset+3])
		if segmentLength < 2 {
			return 0, 0, false
		}
		// SOF0..SOF15，去掉 DHT(C4)/JPG(C8)/DAC(CC)
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			height := int(header[offset+5])<<8 | int(header[offset+6])
			width := int(header[offset+7])<<8 | int(header[offset+8])
			if width <= 0 || height <= 0 {
				return 0, 0, false
			}
			return width, height, true
		}
		offset += 2 + segmentLength
	}
	return 0, 0, false
}

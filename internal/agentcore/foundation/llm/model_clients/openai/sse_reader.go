package openai

import (
	"bufio"
	"fmt"
	"io"
)

// ──────────────────────────── 常量 ────────────────────────────

// sseDataPrefix SSE 数据行前缀
const sseDataPrefix = "data: "

// sseDoneMarker SSE 流结束标记
const sseDoneMarker = "[DONE]"

// sseCommentPrefix SSE 注释行前缀
const sseCommentPrefix = ":"

// ──────────────────────────── 结构体 ────────────────────────────

// SSEReader 从 HTTP 响应体读取 SSE (Server-Sent Events) 事件流。
//
// SSE 协议格式：
//
//	data: {"id":"...","choices":[...]}\n\n
//	data: {"id":"...","choices":[...]}\n\n
//	data: [DONE]\n\n
//
// 本读取器：
//   - 逐行读取，提取 "data: " 前缀后的 JSON 内容
//   - 遇到 "data: [DONE]" 返回 io.EOF 表示流结束
//   - 跳过注释行（以 ":" 开头）和空行
//   - 跳过非 "data:" 前缀的事件字段行（如 "event:", "id:", "retry:" 等）
type SSEReader struct {
	scanner *bufio.Scanner
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSSEReader 从 io.Reader 创建 SSE 读取器。
func NewSSEReader(r io.Reader) *SSEReader {
	scanner := bufio.NewScanner(r)
	// 设置较大的缓冲区，防止长行被截断
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &SSEReader{scanner: scanner}
}

// ReadEvent 读取下一个 SSE 事件，返回 data 字段的 JSON 内容。
//
// 返回值：
//   - data: SSE data 字段内容（不含 "data: " 前缀）
//   - io.EOF: 流正常结束（收到 "data: [DONE]" 或底层读取完毕）
//   - 其他 error: 读取或解析异常
func (r *SSEReader) ReadEvent() (string, error) {
	for r.scanner.Scan() {
		line := r.scanner.Text()

		// 跳过空行（SSE 事件分隔符）
		if line == "" {
			continue
		}

		// 跳过注释行（以 ":" 开头）
		if len(line) > 0 && line[0] == sseCommentPrefix[0] {
			continue
		}

		// 跳过非 data 前缀的事件字段行（如 "event:", "id:", "retry:" 等）
		if len(line) < len(sseDataPrefix) || line[:len(sseDataPrefix)] != sseDataPrefix {
			continue
		}

		// 提取 data 内容
		data := line[len(sseDataPrefix):]

		// 检查流结束标记
		if data == sseDoneMarker {
			return "", io.EOF
		}

		return data, nil
	}

	// scanner 停止，检查是否有错误
	if err := r.scanner.Err(); err != nil {
		return "", fmt.Errorf("SSE 读取错误: %w", err)
	}

	// 正常结束（未收到 [DONE] 但流已关闭）
	return "", io.EOF
}

// ──────────────────────────── 非导出函数 ────────────────────────────

//go:build test

package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// invokeReadFile 调用工具并返回结果 map。
func invokeReadFile(t *testing.T, multimodal bool, inputs map[string]any) map[string]any {
	t.Helper()
	tl := NewReadFileTool(sys_operation.NewLocalSysOperation(nil), "cn", "agent-test", multimodal)
	res, err := tl.Invoke(context.Background(), inputs)
	require.NoError(t, err)
	return res
}

// writePNG 生成指定尺寸的 PNG 文件，用于图片读取分支。
func writePNG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y += 7 {
		for x := 0; x < width; x += 7 {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, png.Encode(f, img))
}

// ──────────────────────────── 用例 ────────────────────────────

// TestReadFileTool_缺少路径 空 file_path 直接回错误。
func TestReadFileTool_缺少路径(t *testing.T) {
	res := invokeReadFile(t, false, map[string]any{"file_path": ""})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "file_path is required")
}

// TestReadFileTool_设备路径被拒 读取块设备路径直接拒绝。
func TestReadFileTool_设备路径被拒(t *testing.T) {
	res := invokeReadFile(t, false, map[string]any{"file_path": "/proc/self/fd/0"})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "device file")
}

// TestReadFileTool_二进制文件被拒 二进制扩展名不得按文本读取。
func TestReadFileTool_二进制文件被拒(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.bin")
	require.NoError(t, os.WriteFile(path, []byte{0x00, 0x01}, 0o644))

	res := invokeReadFile(t, false, map[string]any{"file_path": path})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "Binary files cannot be read")
}

// TestReadFileTool_PDF页码非法 PDF 页码范围格式错误或超出上限时拒绝。
func TestReadFileTool_PDF页码非法(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644))

	bad := invokeReadFile(t, false, map[string]any{"file_path": path, "pages": "abc"})
	assert.Equal(t, false, bad["success"])
	assert.Contains(t, fmt.Sprint(bad["error"]), "Invalid PDF page range format")

	tooMany := invokeReadFile(t, false, map[string]any{"file_path": path, "pages": "1-50"})
	assert.Equal(t, false, tooMany["success"])
	assert.Contains(t, fmt.Sprint(tooMany["error"]), "exceeds the maximum")

	unbounded := invokeReadFile(t, false, map[string]any{"file_path": path, "pages": "5-"})
	assert.Equal(t, false, unbounded["success"])
	assert.Contains(t, fmt.Sprint(unbounded["error"]), "exceeds the maximum")
}

// TestReadFileTool_文本读取成功 返回内容、行数并登记读取状态。
func TestReadFileTool_文本读取成功(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("l1\nl2\nl3\n"), 0o644))

	res := invokeReadFile(t, false, map[string]any{"file_path": path})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["content"], "l1")
	assert.Equal(t, path, data["file_path"])
	assert.EqualValues(t, 4, data["line_count"])

	state, found := GetFileReadState(path)
	require.True(t, found)
	assert.False(t, state.IsPartial)
}

// TestReadFileTool_部分读取 指定 limit 时登记为部分读取。
func TestReadFileTool_部分读取(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	require.NoError(t, os.WriteFile(path, []byte("l1\nl2\nl3\n"), 0o644))

	res := invokeReadFile(t, false, map[string]any{"file_path": path, "limit": 2})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	state, found := GetFileReadState(path)
	require.True(t, found)
	assert.True(t, state.IsPartial)
}

// TestReadFileTool_文件不存在 底层读取失败时回错误而非 panic。
func TestReadFileTool_文件不存在(t *testing.T) {
	res := invokeReadFile(t, false, map[string]any{"file_path": filepath.Join(t.TempDir(), "missing.txt")})
	assert.Equal(t, false, res["success"])
	assert.NotEmpty(t, res["error"])
}

// TestReadFileTool_笔记本读取 ipynb 单元格渲染为 Markdown 片段。
func TestReadFileTool_笔记本读取(t *testing.T) {
	notebook := map[string]any{
		"cells": []map[string]any{
			{
				"cell_type": "markdown",
				"source":    []string{"# Title\n", "body"},
			},
			{
				"cell_type": "code",
				"source":    []string{"print(1)"},
				"outputs": []map[string]any{
					{"output_type": "stream", "text": []string{"1\n"}},
				},
			},
		},
	}
	raw, err := json.Marshal(notebook)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "nb.ipynb")
	require.NoError(t, os.WriteFile(path, raw, 0o644))

	res := invokeReadFile(t, false, map[string]any{"file_path": path})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	content, _ := data["content"].(string)
	assert.Contains(t, content, "## Cell 1 [markdown]")
	assert.Contains(t, content, "# Title")
	assert.Contains(t, content, "### Outputs")
}

// TestReadFileTool_图片关闭多模态 图片按文本摘要返回且不附带 data_url。
func TestReadFileTool_图片关闭多模态(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pic.png")
	writePNG(t, path, 2000, 1600)

	res := invokeReadFile(t, false, map[string]any{"file_path": path})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	content, _ := data["content"].(string)
	assert.Contains(t, content, "Image file read:")
	assert.Contains(t, content, "format: png")
	assert.Contains(t, content, "dimensions: 2000x1600")
	assert.Contains(t, content, "native image multimodal input is disabled")

	multimodal, ok := data["multimodal"].([]any)
	require.True(t, ok)
	assert.Empty(t, multimodal)
}

// TestReadFileTool_图片开启多模态 附带 data URL 与传输尺寸。
func TestReadFileTool_图片开启多模态(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pic.png")
	writePNG(t, path, 2000, 1600)

	res := invokeReadFile(t, true, map[string]any{"file_path": path})
	require.Equal(t, true, res["success"], fmt.Sprint(res["error"]))

	data, ok := res["data"].(map[string]any)
	require.True(t, ok)
	rawMultimodal, ok := data["multimodal"].([]any)
	require.True(t, ok)
	require.Len(t, rawMultimodal, 1)
	part, ok := rawMultimodal[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "image", part["type"])
	assert.True(t, strings.HasPrefix(fmt.Sprint(part["data_url"]), "data:image/png;base64,"))
	assert.Greater(t, part["width"], float64(0))
	assert.Greater(t, part["height"], float64(0))
}

// TestReadFileTool_图片文件为空 空图片回错误。
func TestReadFileTool_图片文件为空(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.png")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	res := invokeReadFile(t, false, map[string]any{"file_path": path})
	assert.Equal(t, false, res["success"])
	assert.Contains(t, fmt.Sprint(res["error"]), "图片文件为空")
}

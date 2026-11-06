package filesystem

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestPNG 生成指定尺寸的纯色 PNG 字节。
func newTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码测试 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

// TestEstimateImageTokens_按尺寸计费 验证工具侧估算与上下文计费同口径。
func TestEstimateImageTokens_按尺寸计费(t *testing.T) {
	data := newTestPNG(t, 1024, 768)
	got := estimateImageTokens(data)
	if want := token.EstimateImageTokens(1024, 768); got != want {
		t.Fatalf("estimateImageTokens = %d, want %d（应与上下文计费一致）", got, want)
	}
	// 与尺寸直接换算的结果一致（1024×768 → Claude patch 1036 更高）
	if got != 1036 {
		t.Fatalf("1024x768 应按尺寸算出 1036，got %d", got)
	}
}

// TestEstimateImageTokens_坏数据回落启发式 验证解码失败仍有可用估算。
func TestEstimateImageTokens_坏数据回落启发式(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 800)
	if got := estimateImageTokens(data); got < 1 {
		t.Fatalf("坏数据应回落到启发式估算（>=1），got %d", got)
	}
}

// TestImageDisplayDimensions 验证传输尺寸解码。
func TestImageDisplayDimensions(t *testing.T) {
	width, height := imageDisplayDimensions(newTestPNG(t, 320, 240))
	if width != 320 || height != 240 {
		t.Fatalf("got %dx%d, want 320x240", width, height)
	}
	if width, height := imageDisplayDimensions([]byte{1, 2, 3}); width != 0 || height != 0 {
		t.Fatalf("坏数据应返回 0,0，got %dx%d", width, height)
	}
}

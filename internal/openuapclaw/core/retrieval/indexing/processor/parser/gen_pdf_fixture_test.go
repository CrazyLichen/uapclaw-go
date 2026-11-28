//go:build gen

package parser

// 本文件仅在 -tags gen 下编译：生成确定性多页 pdf fixture。
// 生成命令：go test -tags gen -run TestGenPDFFixture ./...
// 产物坐标已知（写入时确定），供 pdfcoord/pdf_parser 测试硬编码断言。
// 30.3 计划 Task 0：原计划放 testdata/gen main 包，但 testdata 目录被
// go 工具链忽略无法 go run，改用构建标签测试函数（等价效果）。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/srijanmukherjee/gomupdf"
	"github.com/srijanmukherjee/gomupdf/geometry"
)

// fixturePDFPath fixture 产物路径（与既有 testdata 惯例同级）
const fixturePDFPath = "testdata/fixture_pdf_multipage.pdf"

// TestGenPDFFixture 生成两页确定性 pdf：
// 第 1 页两行文本；第 2 页一行文本 + 一张嵌入图（placement 100,100→200,200）。
func TestGenPDFFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("gen 工具不进 short 模式")
	}
	doc, err := gomupdf.NewPDF()
	if err != nil {
		t.Fatalf("NewPDF: %v", err)
	}
	defer doc.Close()

	// 第 1 页：两行已知文本
	if err := doc.NewPage(595, 842); err != nil {
		t.Fatalf("NewPage 1: %v", err)
	}
	p1, err := doc.LoadPage(0)
	if err != nil {
		t.Fatalf("LoadPage 0: %v", err)
	}
	if err := p1.InsertText(72, 100, 12, "Alpha Bravo Charlie"); err != nil {
		t.Fatalf("InsertText 1-1: %v", err)
	}
	if err := p1.InsertText(72, 130, 12, "知识库原文定位测试第一页"); err != nil {
		t.Fatalf("InsertText 1-2: %v", err)
	}

	// 第 2 页：一行文本 + 一张嵌入图
	if err := doc.NewPage(595, 842); err != nil {
		t.Fatalf("NewPage 2: %v", err)
	}
	p2, err := doc.LoadPage(1)
	if err != nil {
		t.Fatalf("LoadPage 1: %v", err)
	}
	if err := p2.InsertText(72, 100, 12, "Delta Echo on page two"); err != nil {
		t.Fatalf("InsertText 2-1: %v", err)
	}
	if err := p2.InsertImage(geometry.NewRect(100, 100, 200, 200), oneRedPNG()); err != nil {
		t.Fatalf("InsertImage: %v", err)
	}

	if err := doc.EzSave(fixturePDFPath); err != nil {
		t.Fatalf("EzSave: %v", err)
	}
	if _, err := os.Stat(fixturePDFPath); err != nil {
		t.Fatalf("fixture 未落盘: %v", err)
	}
	t.Logf("fixture 已生成: %s", fixturePDFPath)
}

// oneRedPNG 返回 1x1 红色 PNG 字节（标准库生成，确定性）
func oneRedPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

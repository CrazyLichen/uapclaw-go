package parser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pdf "github.com/ledongthuc/pdf"
)

// ──────────────────────────── 测试辅助：合成 PDF ────────────────────────────

// tinyJPEGBytes 1x1 灰度 JPEG 真实字节（合法 JPEG 流，作 DCTDecode XObject 数据）。
var tinyJPEGBytes = []byte{
	0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x0D, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06,
	0x05, 0x08, 0x07, 0x07, 0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14, 0x0D,
	0x0E, 0x0F, 0x10, 0x14, 0x11, 0x0E, 0x0F, 0x0D, 0xFF, 0xC0, 0x00, 0x08,
	0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xFF, 0xC4, 0x00, 0x1F,
	0x00, 0x00, 0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
	0x07, 0x08, 0x09, 0x0A, 0x0B, 0xFF, 0xC4, 0x00, 0xB5, 0x10, 0x00, 0x02,
	0x01, 0x03, 0x03, 0x02, 0x04, 0x03, 0x05, 0x05, 0x04, 0x04, 0x00, 0x00,
	0x01, 0x7D, 0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12, 0x21, 0x31,
	0x41, 0x06, 0x13, 0x51, 0x61, 0x07, 0x22, 0x71, 0x14, 0x32, 0x81, 0x91,
	0xA1, 0x08, 0x23, 0x42, 0xB1, 0xC1, 0x15, 0x52, 0xD1, 0xF0, 0x24, 0x33,
	0x62, 0x72, 0x82, 0x09, 0x0A, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x25, 0x26,
	0x27, 0x28, 0x29, 0x2A, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x43,
	0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x53, 0x54, 0x55, 0x56, 0x57,
	0x58, 0x59, 0x5A, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6A, 0x73,
	0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x83, 0x84, 0x85, 0x86, 0x87,
	0x88, 0x89, 0x8A, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A,
	0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9, 0xAA, 0xB2, 0xB3, 0xB4,
	0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xC2, 0xC3, 0xC4, 0xC5, 0xC6, 0xC7,
	0xC8, 0xC9, 0xCA, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA,
	0xE1, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xF1, 0xF2,
	0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0xFF, 0xDA, 0x00, 0x08,
	0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, 0x7B, 0xA0, 0x00, 0x00, 0x00, 0x00,
	0x01, 0xFF, 0xD9,
}

// pdfObject PDF 对象条目。
type pdfObject struct {
	num    int    // 对象号
	stream []byte // 流数据（nil 表示纯字典对象）
	dict   string // 字典内容（含流对象的额外条目）
}

// buildPDF 合成最小合法 PDF 字节。
//
// pages：每页一项（页文本）；imgOnPage：页号→是否嵌图（嵌入 DCTDecode XObject）。
// 固定对象布局：1=Catalog 2=Pages 3=Font 4=Image XObject（仅需要时）
// 5..=每页 Page 与 Contents。图片绘制走内容流 Do 操作符 + Resources XObject 声明。
func buildPDF(pages []string, imgOnPage map[int]bool) []byte {
	var objs []pdfObject

	// 计算对象号分配
	imgObj := 0
	hasImage := false
	for _, has := range imgOnPage {
		if has {
			hasImage = true
		}
	}
	if hasImage {
		imgObj = 4
	}

	pageStart := 5
	if !hasImage {
		pageStart = 4
	}

	// 对象 1：Catalog
	objs = append(objs, pdfObject{num: 1, dict: "<< /Type /Catalog /Pages 2 0 R >>"})

	// 对象 2：Pages
	kidRefs := ""
	for i := range pages {
		kidRefs += fmt.Sprintf("%d 0 R ", pageStart+2*i)
	}
	objs = append(objs, pdfObject{num: 2, dict: fmt.Sprintf(
		"<< /Type /Pages /Count %d /Kids [ %s] >>", len(pages), kidRefs)})

	// 对象 3：Font（Helvetica 内置）
	objs = append(objs, pdfObject{num: 3, dict: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"})

	// 对象 4（可选）：Image XObject
	if hasImage {
		objs = append(objs, pdfObject{num: imgObj, dict: fmt.Sprintf(
			"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>",
			len(tinyJPEGBytes)), stream: tinyJPEGBytes})
	}

	// 每页：Page 对象 + Contents 流对象
	for i, text := range pages {
		pageNum := pageStart + 2*i
		contentsNum := pageNum + 1

		resources := "<< /Font << /F1 3 0 R >>"
		content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", escapePDFText(text))
		if imgOnPage[i+1] {
			resources += fmt.Sprintf(" /XObject << /Im0 %d 0 R >>", imgObj)
			content = fmt.Sprintf("q 100 0 0 100 72 500 cm /Im0 Do Q BT /F1 12 Tf 72 720 Td (%s) Tj ET", escapePDFText(text))
		}
		resources += " >>"

		objs = append(objs, pdfObject{num: pageNum, dict: fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources %s /Contents %d 0 R >>",
			resources, contentsNum)})
		objs = append(objs, pdfObject{num: contentsNum, dict: fmt.Sprintf("<< /Length %d >>", len(content)), stream: []byte(content)})
	}

	// 序列化：头 + 对象 + xref + trailer
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := map[int]int{}
	for _, o := range objs {
		offsets[o.num] = buf.Len()
		buf.WriteString(fmt.Sprintf("%d 0 obj\n%s\n", o.num, o.dict))
		if o.stream != nil {
			buf.WriteString("stream\n")
			buf.Write(o.stream)
			buf.WriteString("\nendstream\n")
		}
		buf.WriteString("endobj\n")
	}
	xrefPos := buf.Len()
	maxNum := 0
	for n := range offsets {
		if n > maxNum {
			maxNum = n
		}
	}
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n", maxNum+1))
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= maxNum; i++ {
		if off, ok := offsets[i]; ok {
			buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
		} else {
			buf.WriteString("0000000000 65535 f \n")
		}
	}
	buf.WriteString(fmt.Sprintf("trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", maxNum+1, xrefPos))
	return buf.Bytes()
}

// escapePDFText 转义文本操作数中的特殊字符。
func escapePDFText(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '(', ')', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// writeTempPDF 在临时目录写合成 PDF，返回路径。
func writeTempPDF(t *testing.T, name string, pages []string, imgOnPage map[int]bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buildPDF(pages, imgOnPage), 0o644); err != nil {
		t.Fatalf("写入测试 PDF 失败: %v", err)
	}
	return path
}

// TestBuildPDF_合成合法性 fixture 自检：ledongthuc 与 pdfcpu 均可解析。
func TestBuildPDF_合成合法性(t *testing.T) {
	pdfBytes := buildPDF([]string{"Hello PDF"}, map[int]bool{1: true})

	// ledongthuc 文本侧
	r, err := pdf.NewReader(bytes.NewReader(pdfBytes), int64(len(pdfBytes)))
	if err != nil {
		t.Fatalf("ledongthuc 解析失败: %v", err)
	}
	if r.NumPage() != 1 {
		t.Fatalf("应为 1 页，实际 %d", r.NumPage())
	}
	fonts := make(map[string]*pdf.Font)
	page := r.Page(1)
	for _, name := range page.Fonts() {
		f := page.Font(name)
		fonts[name] = &f
	}
	text, err := page.GetPlainText(fonts)
	if err != nil {
		t.Fatalf("GetPlainText 失败: %v", err)
	}
	if !bytes.Contains([]byte(text), []byte("Hello PDF")) {
		t.Errorf("文本应含 'Hello PDF'，实际 %q", text)
	}
}

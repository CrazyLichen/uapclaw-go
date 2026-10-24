package parser

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	pdf "github.com/ledongthuc/pdf"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助：合成 PDF ────────────────────────────

// tinyJPEGBytes 1x1 灰度 JPEG 真实字节（Go 标准库 jpeg.Encode 产物，
// 作 DCTDecode XObject 数据——保证 image.DecodeConfig 可解码断言成立）。
var tinyJPEGBytes = []byte{
	255, 216, 255, 219, 0, 132, 0, 5, 3, 4, 4, 4, 3, 5, 4, 4, 4, 5, 5, 5,
	6, 7, 12, 8, 7, 7, 7, 7, 15, 11, 11, 9, 12, 17, 15, 18, 18, 17, 15, 17,
	17, 19, 22, 28, 23, 19, 20, 26, 21, 17, 17, 24, 33, 24, 26, 29, 29, 31,
	31, 31, 19, 23, 34, 36, 34, 30, 36, 28, 30, 31, 30, 1, 5, 5, 5, 7, 6,
	7, 14, 8, 8, 14, 30, 20, 17, 20, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30,
	30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30,
	30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30,
	30, 30, 30, 30, 255, 192, 0, 11, 8, 0, 1, 0, 1, 1, 1, 17, 0, 255, 196,
	0, 210, 0, 0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3,
	4, 5, 6, 7, 8, 9, 10, 11, 16, 0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0,
	0, 1, 125, 1, 2, 3, 0, 4, 17, 5, 18, 33, 49, 65, 6, 19, 81, 97, 7, 34,
	113, 20, 50, 129, 145, 161, 8, 35, 66, 177, 193, 21, 82, 209, 240, 36,
	51, 98, 114, 130, 9, 10, 22, 23, 24, 25, 26, 37, 38, 39, 40, 41, 42, 52,
	53, 54, 55, 56, 57, 58, 67, 68, 69, 70, 71, 72, 73, 74, 83, 84, 85, 86,
	87, 88, 89, 90, 99, 100, 101, 102, 103, 104, 105, 106, 115, 116, 117,
	118, 119, 120, 121, 122, 131, 132, 133, 134, 135, 136, 137, 138, 146,
	147, 148, 149, 150, 151, 152, 153, 154, 162, 163, 164, 165, 166, 167,
	168, 169, 170, 178, 179, 180, 181, 182, 183, 184, 185, 186, 194, 195,
	196, 197, 198, 199, 200, 201, 202, 210, 211, 212, 213, 214, 215, 216,
	217, 218, 225, 226, 227, 228, 229, 230, 231, 232, 233, 234, 241, 242,
	243, 244, 245, 246, 247, 248, 249, 250, 255, 218, 0, 8, 1, 1, 0, 0, 63,
	0, 248, 202, 191, 255, 217,
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
//
// 文本走裸 Td/Tj（不带 BT/ET）：ledongthuc 的 BT 操作符会注入前导 "\n"
// （page.go showText("\n")），真实 PDF 渲染器无此行为；去掉 BT/ET 使
// GetPlainText 输出即页面文本本身，断言无需容忍前导换行。
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
		content := fmt.Sprintf("/F1 12 Tf 72 720 Td (%s) Tj", escapePDFText(text))
		if imgOnPage[i+1] {
			resources += fmt.Sprintf(" /XObject << /Im0 %d 0 R >>", imgObj)
			content = fmt.Sprintf("q 100 0 0 100 72 500 cm /Im0 Do Q /F1 12 Tf 72 720 Td (%s) Tj", escapePDFText(text))
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

	// pdfcpu 图片侧：图片通道分桶断言（TestExtractImagesByPage_* 覆盖，
	// 此处仅保证 fixture 不被 pdfcpu 文档级拒绝——经图片用例间接验证）
}

// ──────────────────────────── extractImagesByPage 测试 ────────────────────────────

// TestExtractImagesByPage_单页带图 单图分桶 + 落盘字节可解码。
func TestExtractImagesByPage_单页带图(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfBytes := buildPDF([]string{"page one"}, map[int]bool{1: true})
	imgByPage := extractImagesByPage(t.Context(), pdfBytes, savedImageDir, "doc.pdf")

	if len(imgByPage[1]) != 1 {
		t.Fatalf("第 1 页应提取 1 张图，实际 %d", len(imgByPage[1]))
	}
	imgPath := imgByPage[1][0]
	if filepath.Base(imgPath) != "doc.pdf__page_1__img_0.jpg" {
		t.Errorf("落盘名应为 doc.pdf__page_1__img_0.jpg（原生扩展名），实际 %q", filepath.Base(imgPath))
	}
	// 字节可 image.Decode（解码后 JPEG）
	data, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("读落盘图失败: %v", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Errorf("落盘字节应可解码，实际 err: %v", err)
	}
	if cfg.Width != 1 || cfg.Height != 1 {
		t.Errorf("尺寸应为 1x1，实际 %dx%d", cfg.Width, cfg.Height)
	}
}

// TestExtractImagesByPage_多页分桶 页码归属正确。
func TestExtractImagesByPage_多页分桶(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfBytes := buildPDF([]string{"p1", "p2", "p3"}, map[int]bool{1: true, 3: true})
	imgByPage := extractImagesByPage(t.Context(), pdfBytes, savedImageDir, "m.pdf")

	if len(imgByPage[1]) != 1 || len(imgByPage[3]) != 1 {
		t.Fatalf("页 1/3 各应 1 图，实际 %d/%d", len(imgByPage[1]), len(imgByPage[3]))
	}
	if len(imgByPage[2]) != 0 {
		t.Errorf("页 2 无图，实际 %d", len(imgByPage[2]))
	}
}

// TestExtractImagesByPage_损坏输入软失败 非法字节返回空 map 不报错。
func TestExtractImagesByPage_损坏输入软失败(t *testing.T) {
	imgByPage := extractImagesByPage(t.Context(), []byte("not a pdf at all"), savedImageDir, "x.pdf")
	if len(imgByPage) != 0 {
		t.Errorf("损坏输入应返回空 map，实际 %v", imgByPage)
	}
}

// ──────────────────────────── ParseContent 测试 ────────────────────────────

// newPDFParserOrFatal 构造 PDFParser。
func newPDFParserOrFatal(t *testing.T) *PDFParser {
	t.Helper()
	p, err := NewPDFParser()
	if err != nil {
		t.Fatalf("构造 PDFParser 失败: %v", err)
	}
	return p
}

// TestPDFParser_ParseContent_双通道 文本+图片 caption 拼接。
func TestPDFParser_ParseContent_双通道(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfPath := writeTempPDF(t, "doc.pdf", []string{"page one text", "page two text"}, map[int]bool{1: true})

	// caption 固定返回（fakeClient 走真实 Invoke 链——13.15 captionClient 复用）
	model := newCaptionModel(t, nil, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("image caption"), nil
	})

	p := newPDFParserOrFatal(t)
	content, err := p.ParseContent(t.Context(), pdfPath, model)
	if err != nil {
		t.Fatalf("ParseContent 不应报错: %v", err)
	}

	// 断言拼接顺序：页1文本 → caption → 页2文本（Python 同页+追加序语义）
	want := "page one text\nimage caption\npage two text"
	if content != want {
		t.Errorf("content 应为:\n%q\n实际:\n%q", want, content)
	}
	// 落盘验证
	if _, err := os.Stat(filepath.Join("images", "doc.pdf__page_1__img_0.jpg")); err != nil {
		t.Errorf("图片应落盘 images/doc.pdf__page_1__img_0.jpg: %v", err)
	}
}

// TestPDFParser_ParseContent_禁用态 nil client 只出文本。
func TestPDFParser_ParseContent_禁用态(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfPath := writeTempPDF(t, "plain.pdf", []string{"only text"}, map[int]bool{1: true})

	p := newPDFParserOrFatal(t)
	content, err := p.ParseContent(t.Context(), pdfPath, nil)
	if err != nil {
		t.Fatalf("禁用态不应报错: %v", err)
	}
	if content != "only text" {
		t.Errorf("禁用态应只含文本，实际 %q", content)
	}
}

// TestPDFParser_ParseContent_空结果软失败 空白页返回空串。
func TestPDFParser_ParseContent_空结果软失败(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfPath := writeTempPDF(t, "blank.pdf", []string{""}, nil)

	p := newPDFParserOrFatal(t)
	content, err := p.ParseContent(t.Context(), pdfPath, nil)
	if err != nil {
		t.Fatalf("空结果不应报错: %v", err)
	}
	if content != "" {
		t.Errorf("空结果应返回空串（对齐 Python None falsy），实际 %q", content)
	}
}

// TestPDFParser_ParseContent_损坏输入软失败 非 PDF 字节整体软失败。
func TestPDFParser_ParseContent_损坏输入软失败(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	badPath := filepath.Join(dir, "bad.pdf")
	if err := os.WriteFile(badPath, []byte("garbage bytes not pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := newPDFParserOrFatal(t)
	content, err := p.ParseContent(t.Context(), badPath, nil)
	if err != nil {
		t.Fatalf("损坏输入应软失败不报错（对齐 Python except→None），实际 err: %v", err)
	}
	if content != "" {
		t.Errorf("软失败应返回空串，实际 %q", content)
	}
}

// TestPDFParser_Parse_模板集成 Parse 经 BaseParser 包装单文档。
func TestPDFParser_Parse_模板集成(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	pdfPath := writeTempPDF(t, "integ.pdf", []string{"integ text"}, nil)

	p := newPDFParserOrFatal(t)
	docs, err := p.Parse(t.Context(), pdfPath, WithDocID("pdf-doc-1"))
	if err != nil {
		t.Fatalf("Parse 不应报错: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("应包装 1 个 Document，实际 %d", len(docs))
	}
	if docs[0].ID != "pdf-doc-1" || docs[0].Text != "integ text" {
		t.Errorf("Document 应透传 DocID 与文本，实际 ID=%q Text=%q", docs[0].ID, docs[0].Text)
	}
}

// TestPDFParser_注册 .pdf 注册 / .PDF 小写归一跳过。
func TestPDFParser_注册(t *testing.T) {
	if _, ok := getParserFactory(".pdf"); !ok {
		t.Error(".pdf 应已注册")
	}
	if _, ok := getParserFactory(".PDF"); ok {
		t.Error(".PDF 应被 first-wins 小写归一跳过（13.16 既定语义）")
	}
	factory, _ := getParserFactory(".pdf")
	if _, ok := factory().(*PDFParser); !ok {
		t.Error("工厂应产出 *PDFParser")
	}
}

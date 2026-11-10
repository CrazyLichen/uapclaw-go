package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// cwdWithWorkspace 构造绑定了 workspace 的 ctx（测试辅助）。
func cwdWithWorkspace(dir string) context.Context {
	state := cwd.InitCwd(dir, cwd.WithWorkspace(dir))
	return cwd.WithCwdState(context.Background(), state)
}

// ──────────────────────────── 常量 ────────────────────────────

func TestReadPdf_常量(t *testing.T) {
	if DefaultMaxChars != 50_000 {
		t.Errorf("DefaultMaxChars = %d, want 50000", DefaultMaxChars)
	}
	if maxCharsCeiling != 200_000 {
		t.Errorf("maxCharsCeiling = %d, want 200000", maxCharsCeiling)
	}
	if maxPagesPerCall != 100 {
		t.Errorf("maxPagesPerCall = %d, want 100", maxPagesPerCall)
	}
}

// ──────────────────────────── 用例： parse page ranges ───────────────────────────────

func TestParsePageRanges_nil与空(t *testing.T) {
	if got := parsePageRanges(nil); got != nil {
		t.Errorf("nil -> %v, want nil", got)
	}
	if got := parsePageRanges(""); got != nil {
		t.Errorf("empty -> %v, want nil", got)
	}
	if got := parsePageRanges("   "); got != nil {
		t.Errorf("blank -> %v, want nil", got)
	}
}

func TestParsePageRanges_单整数(t *testing.T) {
	got := parsePageRanges(3)
	if len(got) != 1 || got[0] != 3 {
		t.Errorf("got %v, want [3]", got)
	}
}

func TestParsePageRanges_bool拒绝(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("bool should raise ValueError")
		}
	}()
	parsePageRanges(true)
}

func TestParsePageRanges_零与负数(t *testing.T) {
	for _, v := range []int{0, -1} {
		func(v int) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%d should raise ValueError", v)
				}
			}()
			parsePageRanges(v)
		}(v)
	}
}

func TestParsePageRanges_范围字符串(t *testing.T) {
	got := parsePageRanges("1-5,8,10-12")
	want := []int{1, 2, 3, 4, 5, 8, 10, 11, 12}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestParsePageRanges_去重排序(t *testing.T) {
	got := parsePageRanges("5,1,3,1,5")
	want := []int{1, 3, 5}
	if len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParsePageRanges_列表递归(t *testing.T) {
	got := parsePageRanges([]any{3, "1-2", 5})
	want := []int{1, 2, 3, 5}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParsePageRanges_空列表返回nil(t *testing.T) {
	if got := parsePageRanges([]any{}); got != nil {
		t.Errorf("empty list -> %v, want nil", got)
	}
}

func TestParsePageRanges_非法范围(t *testing.T) {
	cases := []string{"5-2", "0-3", "a-b", "1-", "x", "0"}
	for _, in := range cases {
		func(in string) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%q should raise ValueError", in)
				}
			}()
			parsePageRanges(in)
		}(in)
	}
}

func TestParsePageRanges_跳过空段(t *testing.T) {
	got := parsePageRanges("1,,3,")
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("got %v, want [1 3]", got)
	}
}

// ──────────────────────────── 用例： format page list ───────────────────────────────

func TestFormatPageList(t *testing.T) {
	cases := []struct {
		in   []int
		want string
	}{
		{nil, ""},
		{[]int{}, ""},
		{[]int{1}, "1"},
		{[]int{1, 2, 3}, "1-3"},
		{[]int{1, 3, 7}, "1,3,7"},
		{[]int{1, 2, 3, 7, 8, 10}, "1-3,7-8,10"},
		{[]int{3, 1, 2}, "1-3"},
	}
	for _, c := range cases {
		if got := formatPageList(c.in); got != c.want {
			t.Errorf("formatPageList(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ──────────────────────────── 用例： normalize request ───────────────────────────────

func TestNormalizeReadPdfRequest_默认(t *testing.T) {
	req := normalizeReadPdfRequest(map[string]any{"pdf_path": "  /a/b.pdf  "})
	if req.PDFPath != "/a/b.pdf" {
		t.Errorf("PDFPath = %q", req.PDFPath)
	}
	if req.MaxChars != DefaultMaxChars {
		t.Errorf("MaxChars = %d", req.MaxChars)
	}
	if req.Pages != nil {
		t.Errorf("Pages = %v, want nil", req.Pages)
	}
}

func TestNormalizeReadPdfRequest_钳制(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{0, 1000},
		{500, 1000},
		{999_999, 200_000},
		{"not-a-number", 50_000},
		{nil, 50_000},
		{30_000, 30_000},
	}
	for _, c := range cases {
		req := normalizeReadPdfRequest(map[string]any{"pdf_path": "a.pdf", "max_chars": c.in})
		if req.MaxChars != c.want {
			t.Errorf("max_chars=%v -> %d, want %d", c.in, req.MaxChars, c.want)
		}
	}
}

func TestNormalizeReadPdfRequest_空路径(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("empty pdf_path should raise ValueError")
		}
	}()
	normalizeReadPdfRequest(map[string]any{"pdf_path": ""})
}

func TestNormalizeReadPdfRequest_pages(t *testing.T) {
	req := normalizeReadPdfRequest(map[string]any{"pdf_path": "a.pdf", "pages": "1-3"})
	if len(req.Pages) != 3 {
		t.Errorf("Pages = %v", req.Pages)
	}
}

// ──────────────────────────── 用例： resolve pdf path ───────────────────────────────

func TestResolvePDFPath_绝对路径校验(t *testing.T) {
	ctx := context.Background()
	defer func() {
		if r := recover(); r == nil {
			t.Error("nonexistent file should raise")
		}
	}()
	resolvePDFPath(ctx, "/definitely/not/exists.pdf")
}

func TestResolvePDFPath_非pdf后缀(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("non-pdf should raise")
		}
	}()
	resolvePDFPath(context.Background(), p)
}

func TestResolvePDFPath_目录而非文件(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.pdf")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("directory should raise")
		}
	}()
	resolvePDFPath(context.Background(), p)
}

func TestResolvePDFPath_空路径(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("empty should raise")
		}
	}()
	resolvePDFPath(context.Background(), "  ")
}

func TestResolvePDFPath_相对路径无workspace(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("relative path without workspace should raise")
		}
	}()
	resolvePDFPath(context.Background(), "a.pdf")
}

func TestResolvePDFPath_相对路径有workspace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(p, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := cwdWithWorkspace(dir)
	got := resolvePDFPath(ctx, "a.pdf")
	if got != filepath.Clean(p) {
		t.Errorf("got %q, want %q", got, p)
	}
}

// ──────────────────────────── read_pdf 工具 ────────────────────────────

func TestNewReadPDFTool_card(t *testing.T) {
	tl := NewReadPDFTool()
	if tl == nil {
		t.Fatal("tool should not be nil")
	}
}

func TestReadPDF_空路径错误(t *testing.T) {
	// 直接测核心函数，避免依赖 tool 框架反序列化
	got := readPDFCore(context.Background(), map[string]any{"pdf_path": ""})
	if !strings.Contains(got, "[ERROR]: read_pdf failed:") {
		t.Errorf("got %q", got)
	}
}

func TestReadPDF_不存在文件错误(t *testing.T) {
	got := readPDFCore(context.Background(), map[string]any{"pdf_path": "/no/such/file.pdf"})
	if !strings.Contains(got, "PDF file does not exist") {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── 真实 PDF 读取 ────────────────────────────

// minimalPDF 构造一个含 pageCount 页的最小 PDF（每页一段文本）。
// 不依赖外部工具，使用未压缩的 content stream。
func minimalPDFWithText(pageCount int, textForPage func(int) string) []byte {
	objects := []string{}
	// 1 Catalog, 2 Pages, 3 Font, 每页 2 个对象（Page + Content）
	pageObjStart := 4
	kids := []string{}
	for i := 0; i < pageCount; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", pageObjStart+i*2))
	}
	objects = append(objects,
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n",
			strings.Join(kids, " "), pageCount),
		"3 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
	)
	for i := 0; i < pageCount; i++ {
		pageObj := pageObjStart + i*2
		contentObj := pageObj + 1
		objects = append(objects,
			fmt.Sprintf("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>\nendobj\n",
				pageObj, contentObj),
		)
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET\n", textForPage(i+1))
		objects = append(objects,
			fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n",
				contentObj, len(stream), stream),
		)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for _, obj := range objects {
		offsets = append(offsets, buf.Len())
		buf.WriteString(obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, xref)
	return buf.Bytes()
}

// minimalPDF 构造每页含 "Page number N" 文本的最小 PDF。
func minimalPDF(pageCount int) []byte {
	return minimalPDFWithText(pageCount, func(n int) string {
		return fmt.Sprintf("Page number %d", n)
	})
}

func writeTestPDF(t *testing.T, pageCount int) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.pdf")
	if err := os.WriteFile(p, minimalPDF(pageCount), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadPDFSync_全文读取(t *testing.T) {
	p := writeTestPDF(t, 2)
	req := ReadPdfRequest{PDFPath: p, MaxChars: DefaultMaxChars}
	got := readPDFSync(context.Background(), req)

	if !strings.Contains(got, "total pages: 2") {
		t.Errorf("header missing: %q", got)
	}
	if !strings.Contains(got, "--- Page 1 ---") || !strings.Contains(got, "--- Page 2 ---") {
		t.Errorf("page blocks missing: %q", got)
	}
	if !strings.Contains(got, "Page number 1") || !strings.Contains(got, "Page number 2") {
		t.Errorf("page text missing: %q", got)
	}
	if strings.Contains(got, "Partial read") {
		t.Errorf("full read should not report partial: %q", got)
	}
}

func TestReadPDFSync_指定页码(t *testing.T) {
	p := writeTestPDF(t, 3)
	req := ReadPdfRequest{PDFPath: p, Pages: []int{2}, MaxChars: DefaultMaxChars}
	got := readPDFSync(context.Background(), req)

	if !strings.Contains(got, "--- Page 2 ---") {
		t.Errorf("page 2 missing: %q", got)
	}
	if strings.Contains(got, "--- Page 1 ---") || strings.Contains(got, "--- Page 3 ---") {
		t.Errorf("unselected pages leaked: %q", got)
	}
	if !strings.Contains(got, "Partial read: 2 of 3 pages were NOT read") {
		t.Errorf("partial read disclosure missing: %q", got)
	}
	if !strings.Contains(got, "Omit `pages` to read the whole document in one call.") {
		t.Errorf("remedy missing: %q", got)
	}
}

func TestReadPDFSync_越界页码(t *testing.T) {
	p := writeTestPDF(t, 2)
	req := ReadPdfRequest{PDFPath: p, Pages: []int{1, 5}, MaxChars: DefaultMaxChars}
	got := readPDFSync(context.Background(), req)

	if !strings.Contains(got, "requested page(s) 5 exceed total page count 2") {
		t.Errorf("out-of-range note missing: %q", got)
	}
}

func TestReadPDFSync_maxChars截断(t *testing.T) {
	p := writeTestPDF(t, 3)
	req := ReadPdfRequest{PDFPath: p, MaxChars: 1000}
	got := readPDFSync(context.Background(), req)
	// 3 页短文本不该触发截断
	if strings.Contains(got, "Truncated at max_chars") {
		t.Errorf("unexpected truncation: %q", got)
	}

	req2 := ReadPdfRequest{PDFPath: p, MaxChars: 1000}
	_ = req2
}

func TestReadPDFSync_空文本层(t *testing.T) {
	// content stream 无可见文本的 PDF，应报告 no text layer
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.pdf")
	data := minimalPDFWithText(1, func(int) string { return "" })
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	req := ReadPdfRequest{PDFPath: p, MaxChars: DefaultMaxChars}
	got := readPDFSync(context.Background(), req)
	if !strings.Contains(got, "[no text layer on this page]") {
		t.Errorf("empty text layer not flagged: %q", got)
	}
	if !strings.Contains(got, "Pages without extractable text: 1") {
		t.Errorf("summary missing: %q", got)
	}
}

func TestReadPDFCore_正常路径(t *testing.T) {
	p := writeTestPDF(t, 1)
	got := readPDFCore(context.Background(), map[string]any{
		"pdf_path":  p,
		"max_chars": 50_000,
	})
	if strings.HasPrefix(got, "[ERROR]") {
		t.Errorf("unexpected error: %q", got)
	}
	if !strings.Contains(got, "total pages: 1") {
		t.Errorf("header missing: %q", got)
	}
}

func TestReadPDFCore_pages与maxChars(t *testing.T) {
	p := writeTestPDF(t, 2)
	got := readPDFCore(context.Background(), map[string]any{
		"pdf_path":  p,
		"pages":     "1",
		"max_chars": "999999",
	})
	if strings.HasPrefix(got, "[ERROR]") {
		t.Errorf("unexpected error: %q", got)
	}
	if !strings.Contains(got, "Partial read") {
		t.Errorf("partial disclosure missing: %q", got)
	}
}

// ──────────────────────────── 补充：覆盖率 ────────────────────────────

func TestParsePageRanges_数值类型分支(t *testing.T) {
	if got := parsePageRanges(int64(7)); len(got) != 1 || got[0] != 7 {
		t.Errorf("int64 -> %v", got)
	}
	if got := parsePageRanges(float64(4)); len(got) != 1 || got[0] != 4 {
		t.Errorf("float64 -> %v", got)
	}
	if got := parsePageRanges([]int{2, 5, 2}); len(got) != 2 || got[0] != 2 || got[1] != 5 {
		t.Errorf("[]int -> %v", got)
	}
}

func TestToIntDefault_各类型(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{nil, 999}, {int(5), 5}, {int64(6), 6}, {float64(7.9), 7},
		{float32(3.2), 3}, {true, 1}, {false, 0},
		{" 42 ", 42}, {"bad", 999}, {struct{}{}, 999},
	}
	for _, c := range cases {
		if got := toIntDefault(c.in, 999); got != c.want {
			t.Errorf("toIntDefault(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPartition_无分隔(t *testing.T) {
	a, b := partition("abc", "-")
	if a != "abc" || b != "" {
		t.Errorf("got (%q,%q)", a, b)
	}
}

func TestExpandHome_波浪线(t *testing.T) {
	got := expandHome("~/a.pdf")
	if strings.HasPrefix(got, "~") {
		t.Errorf("~/ should expand, got %q", got)
	}
	if got2 := expandHome("/abs/a.pdf"); got2 != "/abs/a.pdf" {
		t.Errorf("absolute unchanged: %q", got2)
	}
}

func TestFormatPageListOrNone_空(t *testing.T) {
	if got := formatPageListOrNone(nil); got != "none" {
		t.Errorf("got %q, want none", got)
	}
	if got := formatPageListOrNone([]int{1, 2}); got != "1-2" {
		t.Errorf("got %q", got)
	}
}

func TestReadPDFSync_maxChars触发截断(t *testing.T) {
	p := writeTestPDF(t, 3)
	// max_chars 钳制下限 1000，但直接构造 ReadPdfRequest 可绕过钳制测内部截断
	req := ReadPdfRequest{PDFPath: p, MaxChars: 20}
	got := readPDFSync(context.Background(), req)
	if !strings.Contains(got, "Truncated at max_chars=20") {
		t.Errorf("truncation note missing: %q", got)
	}
	if !strings.Contains(got, "unread pages") {
		t.Errorf("unread pages note missing: %q", got)
	}
	if !strings.Contains(got, "[... page truncated at max_chars ...]") {
		t.Errorf("page truncation marker missing: %q", got)
	}
}

func TestReadPDFSync_remaining耗尽分支(t *testing.T) {
	p := writeTestPDF(t, 3)
	req := ReadPdfRequest{PDFPath: p, MaxChars: 1}
	got := readPDFSync(context.Background(), req)
	if !strings.Contains(got, "Truncated at max_chars=1") {
		t.Errorf("got %q", got)
	}
}

func TestReadPDFSync_超过单次页数上限(t *testing.T) {
	p := writeTestPDF(t, 105)
	req := ReadPdfRequest{PDFPath: p, MaxChars: DefaultMaxChars}
	got := readPDFSync(context.Background(), req)
	if !strings.Contains(got, "Partial read") {
		t.Errorf("over-cap partial disclosure missing: %q", got[:200])
	}
	if !strings.Contains(got, "At most 100 pages are read per call") {
		t.Errorf("cap remedy missing: %q", got[:400])
	}
}

func TestReadPDFSync_无选中页(t *testing.T) {
	p := writeTestPDF(t, 2)
	// 全部页码越界 → selected 为空，header 显示 none
	req := ReadPdfRequest{PDFPath: p, Pages: []int{99}, MaxChars: DefaultMaxChars}
	got := readPDFSync(context.Background(), req)
	if !strings.Contains(got, "reading pages: none") {
		t.Errorf("got %q", got)
	}
}

func TestNewReadPDFTool_实际调用(t *testing.T) {
	tl := NewReadPDFTool()
	if tl == nil {
		t.Fatal("nil tool")
	}
	// 通过 card 校验名称与描述含常量值
	card := tl.Card()
	if card == nil {
		t.Fatal("nil card")
	}
	if card.Name != "read_pdf" {
		t.Errorf("name = %q", card.Name)
	}
	if !strings.Contains(card.Description, "100 pages") {
		t.Errorf("description should interpolate maxPagesPerCall: %q", card.Description)
	}
	if !strings.Contains(card.Description, "50000") {
		t.Errorf("description should interpolate DefaultMaxChars: %q", card.Description)
	}
}

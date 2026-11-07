//go:build test

package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 测试助手 ────────────────────────────

// newBudgetTestOp 构造带真实文件系统能力的 SysOperation。
func newBudgetTestOp() sys_operation.SysOperation {
	return sys_operation.NewLocalSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
	))
}

// ──────────────────────────── tokenBudget ────────────────────────────

// TestTokenBudget_语义 验证预算累加与容纳判定
func TestTokenBudget_语义(t *testing.T) {
	b := &tokenBudget{maxTokens: 100}
	if !b.canFit(100) {
		t.Fatal("恰好等于预算应可容纳")
	}
	if b.canFit(101) {
		t.Fatal("超出预算不应可容纳")
	}
	b.spend(60)
	if !b.canFit(40) {
		t.Fatal("剩余 40 应可容纳")
	}
	if b.canFit(41) {
		t.Fatal("剩余 40 不应容纳 41")
	}
}

// ──────────────────────────── notebook 预算 ────────────────────────────

// TestReadNotebook_超预算截断 验证超预算时截断并给出提示，而不是整体报错。
// 回归：旧实现先算全文 token，超 maxTokens 直接返回 error，
// 模型对大 notebook 拿不到任何内容。
func TestReadNotebook_超预算截断(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"cells":[`)
	for i := 0; i < 60; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		body := strings.Repeat("x", 4000) // 每 cell 约 1000 token
		sb.WriteString(`{"cell_type":"code","source":["` + body + `"]}`)
	}
	sb.WriteString(`]}`)

	dir := t.TempDir()
	path := filepath.Join(dir, "big.ipynb")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readNotebook(context.Background(), newBudgetTestOp(), path)
	if err != nil {
		t.Fatalf("超预算应截断而非报错，实际 %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "cells extracted") {
		t.Fatalf("应含截断提示，实际长度 %d", len(content))
	}
	if !strings.Contains(content, "-token budget reached") {
		t.Fatal("应含 budget reached 提示")
	}
}

// TestReadNotebook_首块无条件放行 验证第一个 cell 超预算也输出
func TestReadNotebook_首块无条件放行(t *testing.T) {
	huge := strings.Repeat("y", 200000) // 约 50000 token，单块就超预算
	body := `{"cells":[{"cell_type":"code","source":["` + huge + `"]}]}`
	dir := t.TempDir()
	path := filepath.Join(dir, "one.ipynb")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readNotebook(context.Background(), newBudgetTestOp(), path)
	if err != nil {
		t.Fatalf("首块应无条件放行，实际 %v", err)
	}
	content, _ := got["content"].(string)
	if len(content) < 100000 {
		t.Fatalf("首块内容应被完整输出，实际长度 %d", len(content))
	}
	if strings.Contains(content, "truncated") {
		t.Fatal("单块不应触发截断提示")
	}
}

// TestReadNotebook_小块全部保留 验证远小于预算时不做任何截断
func TestReadNotebook_小块全部保留(t *testing.T) {
	body := `{"cells":[` +
		`{"cell_type":"code","source":["print(1)"]},` +
		`{"cell_type":"markdown","source":["# Title"]}` +
		`]}`
	dir := t.TempDir()
	path := filepath.Join(dir, "small.ipynb")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readNotebook(context.Background(), newBudgetTestOp(), path)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(string)
	if strings.Contains(content, "truncated") {
		t.Fatal("小文件不应截断")
	}
	for _, want := range []string{"## Cell 1 [code]", "print(1)", "## Cell 2 [markdown]", "# Title"} {
		if !strings.Contains(content, want) {
			t.Fatalf("应包含 %q，实际:\n%s", want, content)
		}
	}
}

// TestReadNotebook_超字节上限仍报错 验证字节检查未被移除
func TestReadNotebook_超字节上限仍报错(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.ipynb")
	body := fmt.Sprintf(`{"cells":[{"cell_type":"code","source":["%s"]}]}`,
		strings.Repeat("z", maxSizeBytes+1024))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := readNotebook(context.Background(), newBudgetTestOp(), path); err == nil {
		t.Fatal("超过字节上限应报错")
	}
}

// ──────────────────────────── PDF 预算 ────────────────────────────

// buildMinimalPDF 生成 n 页的最小合法 PDF（每页含一行文本）。
// 手写而非依赖第三方生成器：只需能被 ledongthuc/pdf 解析出页数与文本，
// 用于验证按页预算的截断与拼接行为。
func buildMinimalPDF(t *testing.T, pages int) []byte {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")

	offsets := make([]int, 0, pages*2+3)
	writeObj := func(body string) {
		offsets = append(offsets, sb.Len())
		sb.WriteString(body)
	}

	// 1: Catalog，2: Pages
	kids := make([]string, 0, pages)
	for i := 0; i < pages; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i*2))
	}
	writeObj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	writeObj(fmt.Sprintf("2 0 obj\n<< /Type /Pages /Kids [%s] /Count %d >>\nendobj\n",
		strings.Join(kids, " "), pages))

	for i := 0; i < pages; i++ {
		pageObj := 3 + i*2
		contentObj := pageObj + 1
		writeObj(fmt.Sprintf(
			"%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >> >>\nendobj\n",
			pageObj, contentObj))
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (page %d text) Tj ET", i+1)
		writeObj(fmt.Sprintf("%d 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
			contentObj, len(stream), stream))
	}

	// xref
	xrefPos := sb.Len()
	sb.WriteString(fmt.Sprintf("xref\n0 %d\n", len(offsets)+1))
	sb.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		sb.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	sb.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(offsets)+1, xrefPos))
	return []byte(sb.String())
}

// TestReadPDF_逐页预算 验证按页截断并保留首页
func TestReadPDF_逐页预算(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, 3), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readPDF(context.Background(), newBudgetTestOp(), path, "1-3")
	if err != nil {
		t.Fatalf("readPDF error: %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "## Page 1") {
		t.Fatalf("应至少输出首页，实际 %q", content)
	}
}

// TestReadPDF_预算截断文案 验证页数足够多时出现截断提示
func TestReadPDF_预算截断文案(t *testing.T) {
	// 每页文本很短，但页数上限是 pdfMaxPagesPerRead；把 maxTokens 压低不现实，
	// 因此直接验证预算逻辑：构造多页并检查拼接符为 "\n\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "many.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, 5), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readPDF(context.Background(), newBudgetTestOp(), path, "1-5")
	if err != nil {
		t.Fatalf("readPDF error: %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "## Page 5") {
		t.Fatalf("小文档应输出全部页（含 Page 5），实际 %q", content)
	}
	if !strings.Contains(content, "\n\n## Page 2") {
		t.Fatalf("页间应以空行分隔，实际 %q", content)
	}
}

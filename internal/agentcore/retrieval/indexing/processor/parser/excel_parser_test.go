package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/xuri/excelize/v2"
)

// rerankerDocuments 测试别名（缩短断言签名行）。
type rerankerDocuments = reranker.Document

// goldenDoc 黄金基准条目。
type goldenDoc struct {
	ID       string         `json:"id"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata"`
}

// TestRowsToDocuments_golden对拍 测试 xlsx/csv/tsv 黄金基准全对拍。
func TestRowsToDocuments_golden对拍(t *testing.T) {
	data, err := os.ReadFile("testdata/fixture_excel.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Xlsx []goldenDoc `json:"xlsx"`
		Csv  []goldenDoc `json:"csv"`
		Tsv  []goldenDoc `json:"tsv"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// xlsx（include_header=true）
	p, err := NewExcelParser()
	if err != nil {
		t.Fatalf("NewExcelParser: %v", err)
	}
	docs, err := p.Parse(context.Background(), "testdata/fixture_excel.xlsx", WithDocID("base"))
	if err != nil {
		t.Fatalf("Parse xlsx: %v", err)
	}
	assertDocsEqual(t, docs, golden.Xlsx, "xlsx")

	// csv 逗号分隔输入
	docs, err = p.Parse(context.Background(), "testdata/fixture_csv.csv", WithDocID("base"))
	if err != nil {
		t.Fatalf("Parse csv: %v", err)
	}
	assertDocsEqual(t, docs, golden.Csv, "csv")

	// tsv 制表符分隔输入
	docs, err = p.Parse(context.Background(), "testdata/fixture_tsv.tsv", WithDocID("base"))
	if err != nil {
		t.Fatalf("Parse tsv: %v", err)
	}
	assertDocsEqual(t, docs, golden.Tsv, "tsv")
}

// assertDocsEqual 逐字段对拍。
func assertDocsEqual(t *testing.T, docs []rerankerDocuments, want []goldenDoc, label string) {
	t.Helper()
	if len(docs) != len(want) {
		t.Fatalf("[%s] docs 数 = %d, want %d", label, len(docs), len(want))
	}
	for i := range want {
		if docs[i].ID != want[i].ID {
			t.Errorf("[%s] docs[%d].ID = %q, want %q", label, i, docs[i].ID, want[i].ID)
		}
		if docs[i].Text != want[i].Text {
			t.Errorf("[%s] docs[%d].Text = %q, want %q", label, i, docs[i].Text, want[i].Text)
		}
		for k, v := range want[i].Metadata {
			if got, ok := docs[i].Metadata[k]; !ok {
				t.Errorf("[%s] docs[%d].Metadata 缺 %q", label, i, k)
			} else if fmt.Sprint(got) != fmt.Sprint(v) {
				t.Errorf("[%s] docs[%d].Metadata[%q] = %v, want %v", label, i, k, got, v)
			}
		}
	}
}

// TestExcelParserParse_文件不存在 测试 155103 错误码。
func TestExcelParserParse_文件不存在(t *testing.T) {
	p, _ := NewExcelParser()
	_, err := p.Parse(context.Background(), "testdata/不存在.xlsx")
	if err == nil {
		t.Fatal("应返回 155103")
	}
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFileNotFound {
		t.Errorf("status = %v, want StatusRetrievalIndexingFileNotFound", be.Status())
	}
}

// TestExcelParserParse_ENOTDIR路径归155103 路径组件非目录（ENOTDIR）时，
// 对齐 Python os.path.exists 吞 OSError→False 触发 155103（13.11 偏差 #11
// 同步修复——旧 IsNotExist 惯例会放行该场景）。
func TestExcelParserParse_ENOTDIR路径归155103(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := NewExcelParser()
	_, err := p.Parse(context.Background(), filepath.Join(blocker, "sub.xlsx"))
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFileNotFound {
		t.Errorf("status = %v, want StatusRetrievalIndexingFileNotFound", be.Status())
	}
}

// TestExcelParserParse_坏xlsx 测试 155104 错误码（非 zip 文件走 xlsx 分支）。
func TestExcelParserParse_坏xlsx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.xlsx")
	os.WriteFile(path, []byte("not a zip"), 0o644)
	p, _ := NewExcelParser()
	_, err := p.Parse(context.Background(), path)
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFormatNotSupport {
		t.Errorf("status = %v, want StatusRetrievalIndexingFormatNotSupport", be.Status())
	}
}

// TestExcelParserParse_GBK失败155104 测试 CSV 非 UTF-8 解码失败 → 155104。
func TestExcelParserParse_GBK失败155104(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gbk.csv")
	// GBK 编码字节（非合法 UTF-8）
	os.WriteFile(path, []byte{0xd6, 0xd0, 0xce, 0xc4, 0x2c, 0x31, 0x0a}, 0o644)
	p, _ := NewExcelParser()
	_, err := p.Parse(context.Background(), path)
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("应为 *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFormatNotSupport {
		t.Errorf("status = %v, want StatusRetrievalIndexingFormatNotSupport", be.Status())
	}
	// cause 链（对齐 Python cause=e）
	if be.Cause() == nil {
		t.Error("155104 应携带 cause 链")
	}
}

// TestExcelParserSupports 测试 supports 覆写。
func TestExcelParserSupports(t *testing.T) {
	p, _ := NewExcelParser()
	cases := []struct {
		in   string
		want bool
	}{
		{"a.XLSX", true}, {"a.xlsx", true}, {"a.csv", true}, {"a.tsv", true},
		{"a.CSV", true}, {"a.TSV", true}, {"a.docx", false}, {"", false},
		{"noext", false},
	}
	for _, c := range cases {
		if got := p.Supports(c.in); got != c.want {
			t.Errorf("Supports(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestExcelParserParse_空xlsx 测试空 sheet：返回空切片非 error。
func TestExcelParserParse_空xlsx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.xlsx")
	// 用 excelize 造空 workbook（仅默认 Sheet1 无任何行——iter_rows 产出空）
	f := excelize.NewFile()
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	f.Close()
	p, _ := NewExcelParser()
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("空 xlsx 不返回 error: %v", err)
	}
	// Sheet1 无数据行：rows 空 → headers 空 → 列文档 0，行文档 0 → 空
	if len(docs) != 0 {
		t.Fatalf("空 sheet → 0 docs, got %d", len(docs))
	}
}

// TestExcelCellValue_日期 测试日期 cell 的 openpyxl 类型语义（str(datetime) 格式）。
func TestExcelCellValue_日期(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "date.xlsx")
	f := excelize.NewFile()
	// 写入日期（excelize SetCellValue time.Time 自动设日期样式）
	if err := f.SetCellValue("Sheet1", "A1", time.Date(2025, 1, 1, 12, 30, 5, 0, time.UTC)); err != nil {
		t.Fatalf("SetCellValue: %v", err)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	f.Close()

	// Python 对拍：openpyxl data_only=True 读日期 → datetime(2025,1,1,12,30,5)
	// → str() → "2025-01-01 12:30:05"
	sheets, err := excelSheetRows(path)
	if err != nil {
		t.Fatalf("excelSheetRows: %v", err)
	}
	if len(sheets) == 0 || len(sheets[0].rows) == 0 {
		t.Fatal("应有数据行")
	}
	v := cellStr(sheets[0].rows[0][0])
	if v != "2025-01-01 12:30:05" {
		t.Errorf("cellStr(日期) = %q, want %q（Python str(datetime) 微秒 0 省略）", v, "2025-01-01 12:30:05")
	}
}

// TestExcelCellValue_类型还原 测试 xlsx cell 的 openpyxl 类型语义（int/float/bool/string/nil）。
func TestExcelCellValue_类型还原(t *testing.T) {
	sheets, err := excelSheetRows("testdata/fixture_excel.xlsx")
	if err != nil {
		t.Fatalf("excelSheetRows: %v", err)
	}
	if len(sheets) != 2 {
		t.Fatalf("sheets = %d, want 2（Data + Calc）", len(sheets))
	}
	if sheets[0].name != "Data" || sheets[1].name != "Calc" {
		t.Errorf("sheet 名 = %q/%q, want Data/Calc", sheets[0].name, sheets[1].name)
	}
	rows := sheets[0].rows
	if len(rows) != 5 {
		t.Fatalf("Data 行数 = %d, want 5", len(rows))
	}
	// 表头：str/None/str（空 cell → cellNone）
	if rows[0][0] != cellString("Name") || rows[0][1] != noneCell || rows[0][2] != cellString("Age") {
		t.Errorf("表头 = %v, want [Name None Age]", rows[0])
	}
	// int cell 整数单元格
	if v, ok := rows[1][2].(cellInt); !ok || v != 30 {
		t.Errorf("rows[1][2] = %#v, want cellInt 30", rows[1][2])
	}
	// float cell 浮点数单元格
	if v, ok := rows[3][0].(cellFloat); !ok || v != 3.5 {
		t.Errorf("rows[3][0] = %#v, want cellFloat 3.5", rows[3][0])
	}
	// 大数 int 化（%.16g 存储字面量无 ./e/E → int，对齐 openpyxl _cast_number）
	if v, ok := rows[3][1].(cellInt); !ok || v != 1000000000000000 {
		t.Errorf("rows[3][1] = %#v, want cellInt 1000000000000000（存储整数字面量）", rows[3][1])
	}
	// bool cell 布尔单元格
	if v, ok := rows[3][2].(cellBool); !ok || v != cellBool(true) {
		t.Errorf("rows[3][2] = %#v, want cellBool true", rows[3][2])
	}
	// Calc sheet：0.3 float
	if v, ok := sheets[1].rows[1][0].(cellFloat); !ok || v != 0.3 {
		t.Errorf("Calc rows[1][0] = %#v, want cellFloat 0.3", sheets[1].rows[1][0])
	}
}

// TestRowsToDocuments_includeHeaderFalse 测试 include_header=false 分支（Python 基准锁定）。
//
// Python 直跑基准：行文档仅非空值、无表头前缀；全空行 continue；
// 列文档无 "Column name:" 前缀，纯 values join；空列空串。
func TestRowsToDocuments_includeHeaderFalse(t *testing.T) {
	rows := [][]cellValue{
		{cellString("Name"), cellString(""), cellString("Age")},
		{cellString("alice"), cellString("x"), cellInt(30)},
		{cellString(""), cellString(""), cellString("")},
		{cellString("bob"), cellString(""), cellString("")},
	}
	docs := rowsToDocuments(rows, "S", "b", 0, false)
	if len(docs) != 5 {
		t.Fatalf("docs = %d, want 5（2 行 + 3 列，Python 基准）", len(docs))
	}
	// 行文档：仅非空值
	if docs[0].ID != "b_s0_r2" || docs[0].Text != "alice, x, 30" {
		t.Errorf("docs[0] = %q %q, want b_s0_r2 'alice, x, 30'", docs[0].ID, docs[0].Text)
	}
	// 全空行 continue（r3 缺席）
	if docs[1].ID != "b_s0_r4" || docs[1].Text != "bob" {
		t.Errorf("docs[1] = %q %q, want b_s0_r4 'bob'", docs[1].ID, docs[1].Text)
	}
	// 列文档：纯 values join，无前缀
	if docs[2].ID != "b_s0_c0" || docs[2].Text != "alice, bob" {
		t.Errorf("docs[2] = %q %q, want b_s0_c0 'alice, bob'", docs[2].ID, docs[2].Text)
	}
	// 空表头列回落 Column 2（metadata column_name）+ 非空过滤
	if docs[3].ID != "b_s0_c1" || docs[3].Text != "x" {
		t.Errorf("docs[3] = %q %q, want b_s0_c1 'x'", docs[3].ID, docs[3].Text)
	}
	if docs[3].Metadata["column_name"] != "Column 2" {
		t.Errorf("docs[3].Metadata[column_name] = %v, want Column 2", docs[3].Metadata["column_name"])
	}
	// 值列
	if docs[4].ID != "b_s0_c2" || docs[4].Text != "30" {
		t.Errorf("docs[4] = %q %q, want b_s0_c2 '30'", docs[4].ID, docs[4].Text)
	}
}

// TestRowsToDocuments_空表头行 测试表头边界（Python 基准锁定）。
//
// 全空串表头行（[”,”]）：headers 为非空列表不触发 Column N 补齐，
// 空表头格在列文档循环回落 Column N。
// 空 rows[0]（[]）：headers=[] → range(0) 空 → 0 docs。
func TestRowsToDocuments_空表头行(t *testing.T) {
	// 全空串表头
	docs := rowsToDocuments([][]cellValue{{cellString(""), cellString("")}, {cellString("v1"), cellString("v2")}}, "S", "b", 0, true)
	if len(docs) != 2 {
		t.Fatalf("docs = %d, want 2（0 行 + 2 列，Python 基准）", len(docs))
	}
	if docs[0].ID != "b_s0_c0" || docs[0].Text != "Column name: Column 1. Values: v1" {
		t.Errorf("docs[0] = %q %q", docs[0].ID, docs[0].Text)
	}
	if docs[1].ID != "b_s0_c1" || docs[1].Text != "Column name: Column 2. Values: v2" {
		t.Errorf("docs[1] = %q %q", docs[1].ID, docs[1].Text)
	}

	// 空首行（headers 空 → 0 docs）
	docs2 := rowsToDocuments([][]cellValue{{}, {cellString("v1")}}, "S", "b", 0, true)
	if len(docs2) != 0 {
		t.Fatalf("空首行 docs = %d, want 0", len(docs2))
	}

	// rows 整体为空
	docs3 := rowsToDocuments(nil, "S", "b", 0, true)
	if len(docs3) != 0 {
		t.Fatalf("空 rows docs = %d, want 0", len(docs3))
	}
}

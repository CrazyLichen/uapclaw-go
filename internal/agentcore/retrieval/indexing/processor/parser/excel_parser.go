package parser

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExcelParser Excel(.xlsx)/CSV/TSV 文件解析器。
//
// 覆写整体 Parse + Supports（对齐 Python ExcelParser 两处覆写）：
// 首个产出 N>1 Document 的解析器（每行/每列各一文档）+ 首个硬错误路径
// 解析器（155103 文件不存在 / 155104 解析失败）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/excel_parser.py (ExcelParser)
type ExcelParser struct {
	// BaseParser 默认适配器（LazyParse/Process 继承；Parse 整体覆写）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 ExcelParser 实现 Parser
var _ Parser = (*ExcelParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExcelParser 构造 ExcelParser（对齐 Python __init__ super().__init__）。
func NewExcelParser() (*ExcelParser, error) {
	p := &ExcelParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// Parse 解析 Excel/CSV/TSV 为行/列 Document。
//
// 对齐 Python parse 整体覆写：
//   - 文件不存在 → 155103（error_msg=f"File {doc} does not exist"）
//   - CSV/TSV 解码或语法失败 → 155104（cause 链 + logger.exception）
//   - xlsx 解析失败 → 155104（cause 链 + logger.exception）
//   - 成功 → Info 日志（含 documents 数）+ Document 列表
//
// include_header 经 opts.Extra["include_header"] 传入（对齐 Python kwargs.get，
// 缺省 true）。base_id 语义对齐 Python doc_id or doc or "tabular"。
//
// Python: excel_parser.py ExcelParser.parse
func (p *ExcelParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	_ = ctx // 对齐 Python 同步语义（asyncio.to_thread 的 Go 等价物为直接调用）
	o := NewParseOptions(opts...)

	if _, err := os.Stat(doc); os.IsNotExist(err) {
		// 对齐 Python: raise build_error(RETRIEVAL_INDEXING_FILE_NOT_FOUND, error_msg=f"File {doc} does not exist")
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFileNotFound,
			exception.WithMsg("File "+doc+" does not exist"),
		)
	}

	ext := strings.ToLower(filepath.Ext(doc))
	baseID := o.DocID
	if baseID == "" {
		baseID = doc
	}
	if baseID == "" {
		baseID = "tabular"
	}

	includeHeader := true
	if v, ok := o.Extra["include_header"]; ok {
		if b, ok := v.(bool); ok {
			includeHeader = b
		}
	}

	var documents []reranker.Document

	if ext == ".csv" || ext == ".tsv" {
		delimiter := ","
		if ext == ".tsv" {
			delimiter = "\t"
		}
		rows, err := loadDelimitedRows(doc, delimiter)
		if err != nil {
			// 对齐 Python: logger.exception("Failed to parse %s %s: %s", ext.upper().lstrip("."), doc, e)
			logger.Error(logComponent).
				Str("ext", strings.TrimPrefix(strings.ToUpper(ext), ".")).
				Str("doc", doc).
				Err(err).
				Msg("failed to parse delimited file")
			return nil, exception.BuildError(
				exception.StatusRetrievalIndexingFormatNotSupport,
				exception.WithMsg("Parse failed for "+doc+": "+err.Error()),
				exception.WithCause(err),
			)
		}
		sheetName := filepath.Base(doc)
		if sheetName == "" {
			sheetName = "default"
		}
		// rows [][]string → [][]any 包装（rowsToDocuments 消费 any）
		anyRows := make([][]any, len(rows))
		for i, row := range rows {
			r := make([]any, len(row))
			for j, c := range row {
				r[j] = c
			}
			anyRows[i] = r
		}
		documents = rowsToDocuments(anyRows, sheetName, baseID, 0, includeHeader)
	} else {
		sheets, err := excelSheetRows(doc)
		if err != nil {
			// 对齐 Python: logger.exception("Failed to parse Excel %s: %s", doc, e)
			logger.Error(logComponent).
				Str("doc", doc).
				Err(err).
				Msg("failed to parse Excel")
			return nil, exception.BuildError(
				exception.StatusRetrievalIndexingFormatNotSupport,
				exception.WithMsg("Excel parse failed for "+doc+": "+err.Error()),
				exception.WithCause(err),
			)
		}
		for si, sheet := range sheets {
			documents = append(documents, rowsToDocuments(sheet.rows, sheet.name, baseID, si, includeHeader)...)
		}
	}

	// 对齐 Python: logger.info("Parsed %s: %d documents (rows + columns)", doc, len(documents))
	logger.Info(logComponent).
		Str("doc", doc).
		Int("documents", len(documents)).
		Msg("parsed documents (rows + columns)")
	return documents, nil
}

// Supports 对齐 Python supports 覆写：空串 false；小写后缀匹配。
//
// Python: excel_parser.py ExcelParser.supports
func (p *ExcelParser) Supports(doc string) bool {
	if doc == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(doc))
	switch ext {
	case ".xlsx", ".csv", ".tsv":
		return true
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rowsToDocuments 对齐 Python _rows_to_documents（黄金基准锁定语义）。
//
// 行文档：include_header 且表头非空 → "h: v" 无条件 append（值空也产出，
// golden base_s0_r5 "Name: , Age: " 实证）；空表头整格跳过；
// include_header=false 仅非空值 append；parts 空 continue。
// zip 短边截断（headers/row 逐对，超出短边不产出）。
// 列文档：空表头列回落 "Column N"；values 逐行取 col_idx<len(row) 后
// 非空过滤；include_header=true → "Column name: X. Values: ..." 或
// "(empty)"；false → join（空列空串）。
//
// Python: excel_parser.py _rows_to_documents
func rowsToDocuments(rows [][]any, sheetName, baseID string, sheetIndex int, includeHeader bool) []reranker.Document {
	var docs []reranker.Document
	if len(rows) == 0 {
		return docs
	}

	headers := make([]string, len(rows[0]))
	for i, v := range rows[0] {
		headers[i] = cellStr(v)
	}
	// 对齐 Python: if not headers: headers = [f"Column {i+1}" ...]
	//（headers 全空列表时按首行长度补列名；rows[0] 非空则逐格判，
	// 空表头格在列文档循环里回落 "Column N"——两处行为经黄金基准锁定）
	if len(headers) == 0 {
		n := len(rows[0])
		headers = make([]string, n)
		for i := range headers {
			headers[i] = fmt.Sprintf("Column %d", i+1)
		}
	}
	dataRows := rows[1:]

	// 行文档（row_idx 从 2 起，对齐 Python enumerate(start=2)）
	for rowIdx, row := range dataRows {
		rowIdx += 2
		var parts []string
		n := len(headers)
		if len(row) < n {
			n = len(row) // zip 短边截断
		}
		for colIdx := 0; colIdx < n; colIdx++ {
			h := headers[colIdx]
			v := cellStr(row[colIdx])
			if includeHeader && h != "" {
				parts = append(parts, h+": "+v)
			} else if !includeHeader && v != "" {
				parts = append(parts, v)
			}
		}
		if len(parts) == 0 {
			continue
		}
		text := strings.Join(parts, ", ")
		docID := fmt.Sprintf("%s_s%d_r%d", baseID, sheetIndex, rowIdx)
		docs = append(docs, reranker.Document{
			ID:   docID,
			Text: text,
			Metadata: map[string]any{
				"sheet_name":  sheetName,
				"row_index":   rowIdx,
				"source_type": "row",
			},
		})
	}

	// 列文档
	for colIdx, colName := range headers {
		if colName == "" {
			colName = fmt.Sprintf("Column %d", colIdx+1)
		}
		var values []string
		for _, row := range dataRows {
			if colIdx < len(row) {
				if v := cellStr(row[colIdx]); v != "" {
					values = append(values, v)
				}
			}
		}
		var text string
		if includeHeader {
			if len(values) > 0 {
				text = fmt.Sprintf("Column name: %s. Values: %s", colName, strings.Join(values, ", "))
			} else {
				text = fmt.Sprintf("Column name: %s. Values: (empty)", colName)
			}
		} else {
			text = strings.Join(values, ", ")
		}
		docID := fmt.Sprintf("%s_s%d_c%d", baseID, sheetIndex, colIdx)
		docs = append(docs, reranker.Document{
			ID:   docID,
			Text: text,
			Metadata: map[string]any{
				"sheet_name":   sheetName,
				"column_name":  colName,
				"source_type":  "column",
			},
		})
	}

	return docs
}

// loadDelimitedRows 对齐 Python _load_delimited_rows。
//
// utf-8-sig：BOM 剥离 + 非法 UTF-8 字节 → error（对齐 Python
// UnicodeDecodeError → 155104 路径）。
//
// Python: excel_parser.py _load_delimited_rows
func loadDelimitedRows(path, delimiter string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// utf-8-sig BOM 剥离
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid UTF-8 bytes in %s", path)
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = []rune(delimiter)[0]
	r.FieldsPerRecord = -1 // 行长度可变（对齐 Python csv.reader 默认宽松）
	return r.ReadAll()
}

// init 注册到 registry（对齐 Python @register_parser([".xlsx", ".XLSX", ".csv", ".CSV", ".tsv", ".TSV"])）。
func init() {
	RegisterParser([]string{".xlsx", ".XLSX", ".csv", ".CSV", ".tsv", ".TSV"}, func() Parser {
		p, _ := NewExcelParser()
		return p
	})
}

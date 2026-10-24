package parser

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// excelSheet 单 sheet 数据（名称 + 类型化行）。
type excelSheet struct {
	// name sheet 名称
	name string
	// rows 类型化单元格行（openpyxl values_only 语义）
	rows [][]any
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// pyDateTimeFormat Python str(datetime) 格式（微秒非 0 时补）
	pyDateTimeFormat = "2006-01-02 15:04:05"
)

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// pyFloatStr 还原 Python str(float) 记法。
//
// Python repr 规则（CPython float_repr）：
//   - abs ∈ [1e-4, 1e16) 用普通记法（最短往返），整数值补 ".0"
//   - 其余用 e 记法（Python 风格：1e+16 / 1e-05，指数最少两位）
//   - nan/inf/-inf 特殊字面
//
// Go strconv.FormatFloat('f'/'e', -1, 64) 同为最短往返算法，d 格式区域
// 内与 Python repr 等价；e 记法 Go 指数也是两位（1e-05 同形）。
func pyFloatStr(f float64) string {
	if math.IsNaN(f) {
		return "nan"
	}
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	abs := math.Abs(f)
	// 零值与 [1e-4, 1e16) 走 f 记法（对齐 CPython decpt 判定：decpt > 16
	// 或 decpt <= -4 才 e 记法；0.0 的 decpt=1 特例走 f）
	if abs == 0 || (abs >= 1e-4 && abs < 1e16) {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		// 整数值补 .0（Python "2.0" 风格；-0.0 保留负号）
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	// e 记法：Go "1e+16"/"1e-05" 与 Python 同形（指数两位、正号保留）
	return strconv.FormatFloat(f, 'e', -1, 64)
}

// cellStr 对齐 Python _cell_str：nil→""，str(value).strip()。
//
// 逐类型模拟 Python str()：
//   - int/int64 → 十进制
//   - float64 → pyFloatStr
//   - bool → "True"/"False"
//   - time.Time → Python str(datetime) 格式（微秒非 0 补 .%06d）
//   - string → 原文（strip）
//
// Python: excel_parser.py _cell_str
func cellStr(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return pyFloatStr(val)
	case bool:
		if val {
			return "True"
		}
		return "False"
	case time.Time:
		s := val.Format(pyDateTimeFormat)
		if val.Nanosecond() != 0 {
			s += fmt.Sprintf(".%06d", val.Nanosecond()/1000)
		}
		return s
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", val))
	}
}

// excelSheetRows excelize adapter：读 xlsx 产 sheet 列表（名称 + [][]any）。
//
// 对齐 openpyxl load_workbook(read_only=True, data_only=True) +
// iter_rows(values_only=True)：
//   - 逐 sheet 顺序（excelize GetSheetList 同序）
//   - 行语义：Rows() 迭代器按 XML row 元素产出（空 row 也保留——
//     openpyxl 对空 cell 行产出全 None 行；excelize GetRows 会裁尾空行，
//     故不用 GetRows）
//   - 列宽语义：openpyxl 每行 pad 到全表 max_column 宽（缺 cell → None）；
//     Go 先扫全表最大列宽，每行补齐后逐轴还原类型
//   - cell 值还原类型（对齐 openpyxl _cast_number）：数字字面量
//     无 ./e/E → int，否则 float64；bool cell → bool；空 cell → nil；
//     日期样式 cell → ExcelDateToTime 还原 time.Time
func excelSheetRows(filePath string) ([]excelSheet, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var sheets []excelSheet
	for _, sheet := range f.GetSheetList() {
		rows, err := readSheetRows(f, sheet)
		if err != nil {
			return nil, err
		}
		sheets = append(sheets, excelSheet{name: sheet, rows: rows})
	}
	return sheets, nil
}

// readSheetRows 读单 sheet 为类型化行（满宽 pad）。
//
// 两遍扫描：第一遍收原始字面量行并记最大列宽；第二遍按轴还原类型。
func readSheetRows(f *excelize.File, sheet string) ([][]any, error) {
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()

	// 第一遍：Rows 迭代器按 XML 行元素产出（空行保留为空切片）
	var rawRows [][]string
	maxCols := 0
	for it.Next() {
		cols, err := it.Columns()
		if err != nil {
			return nil, err
		}
		if len(cols) > maxCols {
			maxCols = len(cols)
		}
		rawRows = append(rawRows, cols)
	}

	// 第二遍：pad 到满宽 + 逐轴类型还原（对齐 openpyxl iter_rows 满宽语义）
	rows := make([][]any, len(rawRows))
	for ri := range rawRows {
		typed := make([]any, maxCols)
		for ci := 0; ci < maxCols; ci++ {
			axis, _ := excelize.CoordinatesToCellName(ci+1, ri+1)
			typed[ci] = excelCellValue(f, sheet, axis)
		}
		rows[ri] = typed
	}
	return rows, nil
}

// excelCellValue 还原单 cell 的 openpyxl 类型语义。
//
// excelize GetCellType 返回枚举：Bool/Number/String/InlineString/Error。
// 原始字面量经 Options{RawCellValue: true} 取（GetCellValue 默认按
// numFmt 显示格式化，如 1000000000000000 → "1E+15"）。
// Number cell：字面量无 ./e/E → int，否则 float64（对齐 openpyxl
// _cast_number 按 xlsx 存储字面量推断 int/float）。
// 日期样式 cell（numFmt 为日期类）：序列数 → ExcelDateToTime 还原
// time.Time（对齐 openpyxl from_excel，同 1900 epoch 与补正算法）。
func excelCellValue(f *excelize.File, sheet, axis string) any {
	ct, err := f.GetCellType(sheet, axis)
	if err != nil {
		return nil
	}
	rawOpts := excelize.Options{RawCellValue: true}
	switch ct {
	case excelize.CellTypeBool:
		v, err := f.GetCellValue(sheet, axis, rawOpts)
		if err != nil {
			return nil
		}
		// excelize bool 原始值 "1"/"0"（t="b" 存储字面量）
		switch v {
		case "1":
			return true
		case "0":
			return false
		}
		return strings.EqualFold(v, "TRUE")
	case excelize.CellTypeNumber, excelize.CellTypeUnset:
		raw, err := f.GetCellValue(sheet, axis, rawOpts)
		if err != nil || raw == "" {
			return nil
		}
		// 日期样式判定（对齐 openpyxl style_id in date_formats → from_excel）
		if isDateCell(f, sheet, axis) {
			if fv, err := strconv.ParseFloat(raw, 64); err == nil {
				if t, err := excelize.ExcelDateToTime(fv, false); err == nil {
					return t
				}
			}
		}
		// 对齐 openpyxl _cast_number：无 ./e/E → int，否则 float
		if !strings.ContainsAny(raw, ".eE") {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return int(n)
			}
		}
		if fv, err := strconv.ParseFloat(raw, 64); err == nil {
			return fv
		}
		return raw
	default:
		// String/InlineString/Error → 原文
		v, err := f.GetCellValue(sheet, axis, rawOpts)
		if err != nil || v == "" {
			return nil
		}
		return v
	}
}

// isDateCell 判定 cell 是否日期样式（对齐 openpyxl is_date_format）。
//
// openpyxl 判定：numFmtId 为内置日期类（14..22/45..47）或自定义
// code 含 [dmhys] token。excelize GetStyle 返回 Style{NumFmt, CustomNumFmt}，
// 内置 code 表与 openpyxl BUILTIN_FORMATS 同源（OOXML 规范内置）。
func isDateCell(f *excelize.File, sheet, axis string) bool {
	styleID, err := f.GetCellStyle(sheet, axis)
	if err != nil {
		return false
	}
	style, err := f.GetStyle(styleID)
	if err != nil || style == nil {
		return false
	}
	// 自定义 numFmt：code 含日期 token（对齐 openpyxl is_date_format 的
	// 正则 (?<![_\\])[dmhysDMHYS]——Go 无 lookbehind，逐字符扫描近似，
	// 跳过 [_\] 前缀转义）
	if style.CustomNumFmt != nil && *style.CustomNumFmt != "" {
		return dateTokensInCode(*style.CustomNumFmt)
	}
	// 内置 numFmt ID 判定（14..22 / 45..47 为 OOXML 内置日期格式）
	switch style.NumFmt {
	case 14, 15, 16, 17, 18, 19, 20, 21, 22, 45, 46, 47:
		return true
	}
	return false
}

// dateTokensInCode 扫描 numFmt code 是否含未被转义的日期 token。
//
// 对齐 openpyxl is_date_format：仅看第一段（";" 前），剥引号串后
// 找 [dmhysDMHYS] 字符（前缀 _ 或 \ 转义的跳过）。
func dateTokensInCode(code string) bool {
	if i := strings.IndexByte(code, ';'); i >= 0 {
		code = code[:i]
	}
	var inQuote bool
	var prev byte
	for i := 0; i < len(code); i++ {
		c := code[i]
		if c == '"' {
			inQuote = !inQuote
			prev = c
			continue
		}
		if inQuote {
			prev = c
			continue
		}
		if prev != '_' && prev != '\\' {
			switch c {
			case 'd', 'm', 'h', 'y', 's', 'D', 'M', 'H', 'Y', 'S':
				return true
			}
		}
		prev = c
	}
	return false
}

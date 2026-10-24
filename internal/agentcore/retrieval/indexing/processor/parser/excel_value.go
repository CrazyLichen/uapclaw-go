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
	rows [][]cellValue
}

// cellValue Excel 单元格值的密封 sum type。
//
// 对齐 openpyxl iter_rows(values_only=True) 的类型空间
// [None|str|int|float|bool|datetime]，每个 Python 类型一个变体。
// 密封机制：非导出标记方法 isCellValue()，包外无法新增变体，
// 类型分支穷尽性由 cellStr 的 default 兜底（新变体漏写 case 时
// 落入 default 而非静默错误）。
//
// 取代 [][]any：异构 cell 值的 Go 等价表达收敛为编译期可枚举的
// 6 变体密封接口，下游（rowsToDocuments/cellStr）不再做发散的
// any 类型断言。
type cellValue interface {
	// isCellValue 密封标记（非导出，包外不可实现）
	isCellValue()
}

// cellNone 空 cell（对齐 Python None——openpyxl 空 cell 产出 None）。
type cellNone struct{}

// cellString 文本 cell（对齐 Python str）。
type cellString string

// cellInt 整数 cell（对齐 openpyxl _cast_number 的 int 分支）。
type cellInt int

// cellFloat 浮点 cell（对齐 Python float）。
type cellFloat float64

// cellBool 布尔 cell（对齐 Python bool）。
type cellBool bool

// cellTime 日期时间 cell（对齐 Python datetime.datetime）。
type cellTime time.Time

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// pyDateTimeFormat Python str(datetime) 格式（微秒非 0 时补）
	pyDateTimeFormat = "2006-01-02 15:04:05"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// noneCell 空 cell 单例（对齐 Python None；interface nil 语义由
// cellNone 变体显式承载，避免 nil 接口与 nil 值混淆）。
var noneCell = cellNone{}

// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// isCellValue 密封标记实现（cellNone）。
func (cellNone) isCellValue() {}

// isCellValue 密封标记实现（cellString）。
func (cellString) isCellValue() {}

// isCellValue 密封标记实现（cellInt）。
func (cellInt) isCellValue() {}

// isCellValue 密封标记实现（cellFloat)。
func (cellFloat) isCellValue() {}

// isCellValue 密封标记实现（cellBool）。
func (cellBool) isCellValue() {}

// isCellValue 密封标记实现（cellTime）。
func (cellTime) isCellValue() {}

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

// cellStr 对齐 Python _cell_str：None→""，str(value).strip()。
//
// 逐变体模拟 Python str()：
//   - cellInt → 十进制
//   - cellFloat → pyFloatStr
//   - cellBool → "True"/"False"
//   - cellTime → Python str(datetime) 格式（微秒非 0 补 .%06d）
//   - cellString → 原文（strip）
func cellStr(v cellValue) string {
	switch val := v.(type) {
	case nil:
		return ""
	case cellNone:
		return ""
	case cellString:
		return strings.TrimSpace(string(val))
	case cellInt:
		return strconv.Itoa(int(val))
	case cellFloat:
		return pyFloatStr(float64(val))
	case cellBool:
		if val {
			return "True"
		}
		return "False"
	case cellTime:
		t := time.Time(val)
		s := t.Format(pyDateTimeFormat)
		if t.Nanosecond() != 0 {
			s += fmt.Sprintf(".%06d", t.Nanosecond()/1000)
		}
		return s
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", val))
	}
}

// excelSheetRows excelize adapter：读 xlsx 产 sheet 列表（名称 + [][]cellValue）。
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
	defer f.Close()

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
func readSheetRows(f *excelize.File, sheet string) ([][]cellValue, error) {
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, err
	}
	defer it.Close()

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
	rows := make([][]cellValue, len(rawRows))
	for ri := range rawRows {
		typed := make([]cellValue, maxCols)
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
func excelCellValue(f *excelize.File, sheet, axis string) cellValue {
	ct, err := f.GetCellType(sheet, axis)
	if err != nil {
		return noneCell
	}
	rawOpts := excelize.Options{RawCellValue: true}
	switch ct {
	case excelize.CellTypeBool:
		v, err := f.GetCellValue(sheet, axis, rawOpts)
		if err != nil {
			return noneCell
		}
		// excelize bool 原始值 "1"/"0"（t="b" 存储字面量）
		switch v {
		case "1":
			return cellBool(true)
		case "0":
			return cellBool(false)
		}
		return cellBool(strings.EqualFold(v, "TRUE"))
	case excelize.CellTypeNumber, excelize.CellTypeUnset:
		raw, err := f.GetCellValue(sheet, axis, rawOpts)
		if err != nil || raw == "" {
			return noneCell
		}
		// 日期样式判定（对齐 openpyxl style_id in date_formats → from_excel）
		if isDateCell(f, sheet, axis) {
			if fv, err := strconv.ParseFloat(raw, 64); err == nil {
				if t, err := excelize.ExcelDateToTime(fv, false); err == nil {
					return cellTime(t)
				}
			}
		}
		// 对齐 openpyxl _cast_number：无 ./e/E → int，否则 float
		if !strings.ContainsAny(raw, ".eE") {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return cellInt(n)
			}
		}
		if fv, err := strconv.ParseFloat(raw, 64); err == nil {
			return cellFloat(fv)
		}
		return cellString(raw)
	default:
		// String/InlineString/Error → 原文
		v, err := f.GetCellValue(sheet, axis, rawOpts)
		if err != nil || v == "" {
			return noneCell
		}
		return cellString(v)
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

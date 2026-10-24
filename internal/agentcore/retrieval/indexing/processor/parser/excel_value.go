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
//   - cell 值还原类型（对齐 openpyxl _cast_number）：数字字面量
//     无 ./e/E → int，否则 float64；bool cell → bool；空 cell → nil
//   - 日期 cell：excelize 按数字返回 + style 判断（见 excelCellValue）
func excelSheetRows(filePath string) ([]excelSheet, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sheets []excelSheet
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		// GetRows 丢类型——逐 cell 按原始类型还原
		typed := make([][]any, len(rows))
		for ri, row := range rows {
			typedRow := make([]any, len(row))
			for ci := range row {
				axis, _ := excelize.CoordinatesToCellName(ci+1, ri+1)
				typedRow[ci] = excelCellValue(f, sheet, axis)
			}
			typed[ri] = typedRow
		}
		sheets = append(sheets, excelSheet{name: sheet, rows: typed})
	}
	return sheets, nil
}

// excelCellValue 还原单 cell 的 openpyxl 类型语义。
//
// excelize GetCellType 返回枚举：Bool/Number/String/InlineString/Error。
// Number cell：字面量无 ./e/E → int，否则 float64（对齐 openpyxl
// _cast_number 按 xlsx 存储字面量推断 int/float）。
// 日期 cell（s 样式为日期格式）：excelize GetCellValue 返回
// Excel 1900 序列数字，需按样式格式化——本层还原为数字，
// 日期格式判定留给 GetCellType/样式路径（Task 8 日期测试覆盖）。
func excelCellValue(f *excelize.File, sheet, axis string) any {
	ct, err := f.GetCellType(sheet, axis)
	if err != nil {
		return nil
	}
	switch ct {
	case excelize.CellTypeBool:
		v, err := f.GetCellValue(sheet, axis)
		if err != nil {
			return nil
		}
		// excelize bool 显示 "TRUE"/"FALSE"
		return strings.EqualFold(v, "TRUE")
	case excelize.CellTypeNumber, excelize.CellTypeUnset:
		raw, err := f.GetCellValue(sheet, axis)
		if err != nil || raw == "" {
			return nil
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
		v, err := f.GetCellValue(sheet, axis)
		if err != nil {
			return nil
		}
		if v == "" {
			return nil
		}
		return v
	}
}

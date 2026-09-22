package file_connector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JSONFileConnector 通用 JSON 文件 I/O 连接器。
//
// 处理文件读写操作，对数据结构无关——调用方负责序列化/反序列化。
// 自动创建父目录，UTF-8 编码。
//
// Python: openjiuwen/extensions/context_evolver/core/file_connector/json_file_connector.py
type JSONFileConnector struct {
	indent      int
	ensureASCII bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultIndent 默认缩进空格数，对齐 Python JSONFileConnector(indent=2)
	defaultIndent = 2
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJSONFileConnector 创建 JSON 文件连接器。
// 对齐 Python JSONFileConnector(indent=2, ensure_ascii=False)。
func NewJSONFileConnector(opts ...JSONFileConnectorOption) *JSONFileConnector {
	c := &JSONFileConnector{
		indent:      defaultIndent,
		ensureASCII: false, // 对齐 Python 默认 ensure_ascii=False，保留中文
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ──────────────────────────── 导出方法 ────────────────────────────

// SaveToFile 保存字典数据到 JSON 文件。
// 对齐 Python JSONFileConnector.save_to_file(file_path, data)。
// 自动创建父目录（对齐 Python mkdir(parents=True, exist_ok=True)），覆盖写入。
func (c *JSONFileConnector) SaveToFile(filePath string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	indentStr := strings.Repeat(" ", c.indent)
	raw, err := json.MarshalIndent(data, "", indentStr)
	if err != nil {
		return fmt.Errorf("JSON 编码失败: %w", err)
	}

	// 对齐 Python json.dump(ensure_ascii=True)：
	// Go 的 json.Marshal 默认不转义非 ASCII，需手动处理
	if c.ensureASCII {
		raw = escapeNonASCII(raw)
	}

	raw = append(raw, '\n')

	if err := os.WriteFile(filePath, raw, 0o644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// LoadFromFile 从 JSON 文件加载字典数据。
// 对齐 Python JSONFileConnector.load_from_file(file_path)。
// Python 是 @staticmethod，Go 保留为实例方法（更自然的 Go 风格）。
func (c *JSONFileConnector) LoadFromFile(filePath string) (map[string]any, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("JSON 解码失败: %w", err)
	}
	return data, nil
}

// Exists 检查文件是否存在。
// 对齐 Python JSONFileConnector.exists(file_path)。
func (c *JSONFileConnector) Exists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// Delete 删除文件。返回是否实际删除。
// 对齐 Python JSONFileConnector.delete(file_path)。
func (c *JSONFileConnector) Delete(filePath string) (bool, error) {
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("删除文件失败: %w", err)
	}
	return true, nil
}

// Indent 获取当前缩进空格数。
func (c *JSONFileConnector) Indent() int {
	return c.indent
}

// EnsureASCII 获取是否转义非 ASCII 字符。
func (c *JSONFileConnector) EnsureASCII() bool {
	return c.ensureASCII
}

// String 实现 Stringer 接口。
func (c *JSONFileConnector) String() string {
	return fmt.Sprintf("JSONFileConnector(indent=%d, ensure_ascii=%v)", c.indent, c.ensureASCII)
}

// ──────────────────────────── 非导出类型 ────────────────────────────

// JSONFileConnectorOption JSON 文件连接器配置选项。
type JSONFileConnectorOption func(*JSONFileConnector)

// ──────────────────────────── 导出选项函数 ────────────────────────────

// WithIndent 设置缩进空格数。
func WithIndent(n int) JSONFileConnectorOption {
	return func(c *JSONFileConnector) { c.indent = n }
}

// WithEnsureASCII 设置是否转义非 ASCII 字符。
// Python 默认 ensure_ascii=False（保留中文），Go 对齐此默认值。
func WithEnsureASCII(ensure bool) JSONFileConnectorOption {
	return func(c *JSONFileConnector) { c.ensureASCII = ensure }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// escapeNonASCII 将 JSON 字节流中的非 ASCII 字符转义为 \uXXXX。
// 对齐 Python json.dump(ensure_ascii=True) 的行为。
// 只处理 JSON 字符串值内部的非 ASCII 字符，不影响 JSON 结构字符。
func escapeNonASCII(data []byte) []byte {
	var result []byte
	i := 0
	inString := false

	for i < len(data) {
		b := data[i]

		// 追踪是否在 JSON 字符串内部
		if !inString {
			if b == '"' {
				inString = true
				result = append(result, b)
				i++
				continue
			}
			result = append(result, b)
			i++
			continue
		}

		// 在字符串内部
		if b == '\\' {
			// 转义序列，跳过下一个字节
			result = append(result, b)
			i++
			if i < len(data) {
				result = append(result, data[i])
				// 处理 \uXXXX 转义（可能后跟 surrogate pair \uXXXX\uXXXX）
				if data[i] == 'u' && i+4 < len(data) {
					result = append(result, data[i+1:i+5]...)
					i += 4
				}
				i++
			}
			continue
		}

		if b == '"' {
			inString = false
			result = append(result, b)
			i++
			continue
		}

		// 检查是否是非 ASCII 字符
		if b >= 0x80 {
			// 解码 UTF-8 rune
			r, size := utf8.DecodeRune(data[i:])
			if r != utf8.RuneError {
				if r <= 0xFFFF {
					// BMP 字符直接 \uXXXX
					result = append(result, []byte(fmt.Sprintf(`\u%04X`, r))...)
				} else {
					// 超过 BMP 的字符使用 UTF-16 surrogate pair
					// 对齐 Python json.dump(ensure_ascii=True) 行为
					r -= 0x10000
					hi := 0xD800 + (r >> 10)
					lo := 0xDC00 + (r & 0x3FF)
					result = append(result, []byte(fmt.Sprintf(`\u%04X\u%04X`, hi, lo))...)
				}
				i += size
			} else {
				result = append(result, b)
				i++
			}
			continue
		}

		result = append(result, b)
		i++
	}

	return result
}

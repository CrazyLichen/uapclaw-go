package checkpointer

import (
	"encoding/json"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Serializer 类型化序列化器接口。
// 对应 Python: openjiuwen/core/graph/store/serde.py (Serializer)
//
// DumpsTyped 返回 (格式标签, 字节流)，LoadsTyped 从格式标签和字节流反序列化。
// 格式标签标识序列化格式（"json"/"gob"），便于存储时记录格式，读取时按格式反序列化。
type Serializer interface {
	// DumpsTyped 序列化对象，返回 (格式标签, 字节流, 错误)
	DumpsTyped(obj any) (string, []byte, error)
	// LoadsTyped 反序列化对象，根据格式标签和字节流恢复
	LoadsTyped(formatTag string, data []byte) (any, error)
}

// serdeTuple 序列化元组，对应 Python 的 tuple[str, bytes]
type serdeTuple struct {
	// FormatTag 序列化格式标签
	FormatTag string
	// Data 序列化后的字节流
	Data []byte
}

// JSONSerializer JSON 序列化器实现。
// 对应 Python: openjiuwen/core/graph/store/serde.py (JsonSerializer)
type JSONSerializer struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJSONSerializer 创建 JSON 序列化器
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// DumpsTyped 序列化为 JSON，返回 ("json", jsonBytes)
func (s *JSONSerializer) DumpsTyped(obj any) (string, []byte, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return "", nil, err
	}
	return "json", data, nil
}

// LoadsTyped 从 JSON 反序列化，仅处理格式标签为 "json" 的数据。
// 其他格式标签返回 (nil, nil)。
func (s *JSONSerializer) LoadsTyped(formatTag string, data []byte) (any, error) {
	if formatTag != "json" {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

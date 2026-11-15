package store

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Serializer 序列化抽象：dumps/loads 携带类型名元组。
// Python: store/serde.py:11-19
type Serializer interface {
	// DumpsTyped 序列化并返回 (类型名, 字节)
	DumpsTyped(obj any) (string, []byte, error)
	// LoadsTyped 反序列化到 target（显式还原目标，Python pickle 原对象语义的 Go 等价，偏差 D-12）；
	// data 为 nil 或类型不匹配时不动 target 并返回 nil error（对齐 Python 返回 None）
	LoadsTyped(dataType string, data []byte, target any) error
}

// JSONSerializer JSON 实现。功能上承担 Python PickleSerializer 的默认可用角色
// （偏差 D-1：Go 无 pickle）。Python 的 JsonSerializer 类本身完整实现、照抄其行为。
// Python: store/serde.py:22-32
type JSONSerializer struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// DumpsTyped 序列化对象为 JSON 字节。
// ⚠️ 必须用 json.Encoder + SetEscapeHTML(false)：默认 json.Marshal 会把 < > &
// 转义为 \u003c/\u003e/\u0026，与 Python ensure_ascii=False 字节不一致（偏差 D-9）。
func (s *JSONSerializer) DumpsTyped(obj any) (string, []byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return "json", nil, err
	}
	// Encoder.Encode 末尾带 \n，json.dumps 无换行，剥掉对齐
	data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return "json", data, nil
}

// LoadsTyped 反序列化到 target：data 为 nil 或类型不匹配不动 target 返回 nil，对齐 Python 返回 None
func (s *JSONSerializer) LoadsTyped(dataType string, data []byte, target any) error {
	if data == nil {
		return nil
	}
	if dataType != "json" {
		return nil
	}
	return json.Unmarshal(data, target)
}

// CreateSerializer 按类型名构造序列化器。
// "json" 复刻 Python raise 语义拒绝（Python 侧类已实现但工厂刻意未开放，不自作主张解锁）；
// "pickle" 无 Go 对应物，返回 JSONSerializer 承担（偏差 D-1）。
// 上游解锁跟踪：实施 8.3c 时核对 Python serde.py 是否已放行 json，见 8.3c 行 ⤴️ 标注。
// Python: store/serde.py:44-50
func CreateSerializer(typeName string) (Serializer, error) {
	switch typeName {
	case "json":
		return nil, fmt.Errorf("json is not yet supported")
	case "pickle":
		return &JSONSerializer{}, nil
	default:
		return nil, fmt.Errorf("Unknown serializer type: %s", typeName)
	}
}

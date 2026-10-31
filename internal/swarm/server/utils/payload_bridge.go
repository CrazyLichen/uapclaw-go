package utils

// payload_bridge.go 流式 payload 的 map 桥接（对齐 Python `payload.model_dump(mode="json")`）。
//
// 背景：Python 的 `OutputSchema.payload` 声明为 `Any`（openjiuwen/core/session/stream/base.py:33），
// 生产端既可能是 dict，也可能是 pydantic 模型（如 ControllerOutputChunk.payload 是
// ControllerOutputPayload 模型、workflow 交互是 InteractionOutput 对象）；消费者靠
// duck typing（`getattr` / `isinstance(payload, dict)`）兜底。
//
// Go 没有 duck typing，消费端的等价落点就是本函数：**把任意结构化 payload 收敛成 map**，
// 下游只按键读取。这比"逐类型枚举 case"更彻底——新增 payload 结构体无需再改消费端。

import (
	"encoding/json"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PayloadToMap 把流 payload 统一桥接为 map[string]any。
//
// 处理顺序：
//  1. nil → nil
//  2. map[string]any → 原样返回（主流形态，零开销）
//  3. 实现 ToMap() map[string]any 的类型 → 走快路径
//  4. 结构体 / 结构体指针 → json.Marshal→Unmarshal，形状对齐 Python model_dump(mode="json")
//  5. 其他（string / 数字 / 切片 / 映射以外的引用类型）→ nil，与原先类型断言失败行为一致
//
// 注意：结构体若缺 json tag，往返后键名保持 Go 字段名（PascalCase），
// 与 Python 小写字段名不一致——需要消费端按小写键读取的类型必须显式补 tag。
func PayloadToMap(payload any) map[string]any {
	if payload == nil {
		return nil
	}
	if m, ok := payload.(map[string]any); ok {
		return m
	}
	if tm, ok := payload.(interface{ ToMap() map[string]any }); ok {
		return tm.ToMap()
	}

	// 按形态判定：只桥接 struct / *struct；nil 指针返回 nil
	rv := reflect.ValueOf(payload)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}

	b, err := json.Marshal(payload)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("payload_type", reflect.TypeOf(payload).String()).
			Msg("PayloadToMap: payload marshal failed")
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("payload_type", reflect.TypeOf(payload).String()).
			Msg("PayloadToMap: payload unmarshal failed")
		return nil
	}
	return m
}

// PayloadsToMaps 批量桥接 payload 切片，nil 元素会被跳过。
//
// 用于「interactions 列表」这类「元素形态可能是 map 也可能是结构体」的场景，
// 同时承担 Python `_iter_interactions` 的嵌套列表展开职责（interrupt_helpers.py L342-348）：
// 嵌套的 []any / []map[string]any 会被递归摊平，下游拿到的恒为平坦 []map[string]any。
func PayloadsToMaps(items []any) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch nested := item.(type) {
		case []any:
			// 对齐 Python _iter_interactions 的嵌套列表展开
			result = append(result, PayloadsToMaps(nested)...)
		case []map[string]any:
			result = append(result, nested...)
		default:
			if m := PayloadToMap(item); m != nil {
				result = append(result, m)
			}
		}
	}
	return result
}

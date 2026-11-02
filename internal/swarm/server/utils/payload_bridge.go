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

// ──────────────────────────── 常量 ────────────────────────────

// defaultInteractionMaxDepth 交互载荷递归的默认最大深度。
//
// Python 在 _find_interaction_payloads 内硬编码 `_depth > 8`；Go 把同一常量复用到
// 桥接摊平递归（T-04），避免两处深度上限各自漂移。
const defaultInteractionMaxDepth = 8

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
	m, _ := PayloadToMapChecked(payload)
	return m
}

// PayloadToMapChecked 与 PayloadToMap 相同，但区分两类 nil：
//   - payload 本身不是可桥接形态（string/数字/切片等）→ (nil, nil)，调用方按原语义处理；
//   - 结构体桥接失败（marshal/unmarshal 出错）→ (nil, error)。
//
// T-03：调用方（流式 chunk 解析）据此丢弃该 chunk 并记 Error 日志，而不是把
// nil 兜成空 map、产出 content="map[]" 的伪帧推给前端。
func PayloadToMapChecked(payload any) (map[string]any, error) {
	if payload == nil {
		return nil, nil
	}
	if m, ok := payload.(map[string]any); ok {
		return m, nil
	}
	if tm, ok := payload.(interface{ ToMap() map[string]any }); ok {
		return tm.ToMap(), nil
	}

	// 按形态判定：只桥接 struct / *struct；nil 指针返回 nil
	rv := reflect.ValueOf(payload)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, nil
	}

	b, err := json.Marshal(payload)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("payload_type", reflect.TypeOf(payload).String()).
			Msg("PayloadToMap: payload marshal failed")
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("payload_type", reflect.TypeOf(payload).String()).
			Msg("PayloadToMap: payload unmarshal failed")
		return nil, err
	}
	return m, nil
}

// PayloadsToMaps 批量桥接 payload 切片，nil 元素会被跳过。
//
// 用于「interactions 列表」这类「元素形态可能是 map 也可能是结构体」的场景，
// 同时承担 Python `_iter_interactions` 的嵌套列表展开职责（interrupt_helpers.py L342-348）：
// 嵌套的 []any / []map[string]any 会被递归摊平，下游拿到的恒为平坦 []map[string]any。
func PayloadsToMaps(items []any) []map[string]any {
	return payloadsToMapsDepth(items, 0)
}

// payloadsToMapsDepth 带深度上限的摊平实现。
//
// T-04：Python 的 _iter_interactions 同样无条件递归，但上游 _find_interaction_payloads
// 有 `_depth > 8` 兜底；Go 若不加限制，畸形自引用切片会直接栈溢出（fatal，不可 recover，
// Python 只是 RecursionError）。深度上限复用 defaultInteractionMaxDepth，与
// FindInteractionPayloads 的 MaxDepth 默认值同源。
func payloadsToMapsDepth(items []any, depth int) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	if depth > defaultInteractionMaxDepth {
		logger.Warn(logComponent).
			Int("max_depth", defaultInteractionMaxDepth).
			Msg("PayloadsToMaps: nesting too deep, truncated")
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch nested := item.(type) {
		case []any:
			// 对齐 Python _iter_interactions 的嵌套列表展开
			result = append(result, payloadsToMapsDepth(nested, depth+1)...)
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

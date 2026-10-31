package utils

// payload_bridge.go 流式 payload 的 map 桥接（对齐 Python `payload.model_dump(mode="json")`）。
//
// 背景：Python 流式 chunk 的 payload 恒为 dict，`_parse_stream_chunk` 可直接按键取值；
// Go 侧生产端存在两种形态——map[string]any（agent 层）与 *cschema.ControllerOutputPayload
// 结构体指针（task_loop/executor.go、session_spawn_executor.go、task_scheduler.go）。
// 消费端若只做 `payload.(map[string]any)` 断言，结构体形态会被整块丢弃，
// 表现为 task_failed/task_completion 等事件静默丢失（前端只剩 idle → end_turn）。

import (
	"encoding/json"

	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PayloadToMap 把流 payload 桥接为 map[string]any。
//
// 支持三种形态：
//   - nil → nil
//   - map[string]any → 原样返回
//   - *ControllerOutputPayload / ControllerOutputPayload → json.Marshal→Unmarshal
//     产 map，形状与 Python dict 逐键一致（type/data/metadata，DataFrame 经
//     MarshalDataFrames 多态序列化为 {text}/{data}/{name,...}）
//
// 其他类型返回 nil（与调用方原先的类型断言失败行为一致）。
func PayloadToMap(payload any) map[string]any {
	if payload == nil {
		return nil
	}
	if m, ok := payload.(map[string]any); ok {
		return m
	}
	// 指针与值类型都桥接（生产端为指针；值类型分支对齐 Python
	// isinstance 的宽容度，防御未来构造点变化）
	switch cop := payload.(type) {
	case *cschema.ControllerOutputPayload:
		if cop == nil {
			return nil
		}
		return marshalControllerOutputPayload(cop)
	case cschema.ControllerOutputPayload:
		return marshalControllerOutputPayload(&cop)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalControllerOutputPayload 桥接单个 ControllerOutputPayload 到 map。
func marshalControllerOutputPayload(cop *cschema.ControllerOutputPayload) map[string]any {
	b, err := json.Marshal(cop)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("PayloadToMap: ControllerOutputPayload marshal failed")
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("PayloadToMap: ControllerOutputPayload unmarshal failed")
		return nil
	}
	return m
}

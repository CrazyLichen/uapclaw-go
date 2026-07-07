// Package web 提供 Gateway Web 通道的 WebSocket 帧协议定义与编解码函数。
//
// 帧协议定义了 Web 前端与 Gateway 之间 WebSocket 通信的三种帧类型：
//   - req 帧：客户端发起请求（type=req, id, method, params）
//   - res 帧：服务端响应请求（type=res, id, ok, payload, error?, code?）
//   - event 帧：服务端推送事件（type=event, event, payload, seq?, stream_id?）
//
// 文件目录：
//
//	web/
//	├── frame.go           # 帧协议类型定义和编解码函数
//	└── frame_test.go      # 帧协议单元测试
//
// 对应 Python 代码：jiuwenswarm/gateway/channel_manager/web/web_connect.py
package web

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReqFrame 请求帧，客户端发送到服务端的请求。
//
// 字段布局对齐 Python WebChannel._handle_raw_message 解析逻辑：
//
//	{"type": "req", "id": "<uuid>", "method": "chat.send", "params": {...}}
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/web/web_connect.py (入站 JSON 解析)
type ReqFrame struct {
	// Type 帧类型，固定为 "req"
	Type string `json:"type"`
	// ID 请求唯一标识（UUID）
	ID string `json:"id"`
	// Method RPC 方法名（如 "chat.send"）
	Method string `json:"method"`
	// Params 请求参数（延迟解析，由上层按 method 解码）
	Params json.RawMessage `json:"params"`
}

// ResFrame 响应帧，服务端回复客户端的请求。
//
// 字段布局对齐 Python WebChannel.send_response 构建逻辑：
//
//	{"type": "res", "id": "<req_id>", "ok": true, "payload": {...}}
//	{"type": "res", "id": "<req_id>", "ok": false, "payload": {}, "error": "...", "code": "BAD_REQUEST"}
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/web/web_connect.py (send_response)
type ResFrame struct {
	// Type 帧类型，固定为 "res"
	Type string `json:"type"`
	// ID 对应请求的标识
	ID string `json:"id"`
	// OK 是否成功
	OK bool `json:"ok"`
	// Payload 响应负载（成功时携带数据，失败时为空 map）
	Payload map[string]any `json:"payload"`
	// Error 错误描述（仅 ok=false 时存在）
	Error string `json:"error,omitempty"`
	// Code 错误码（仅 ok=false 时可能存在，如 "BAD_REQUEST"）
	Code string `json:"code,omitempty"`
}

// EventFrame 事件帧，服务端向客户端推送的事件。
//
// 字段布局对齐 Python WebChannel.send_event 构建逻辑：
//
//	{"type": "event", "event": "chat.chunk", "payload": {...}, "seq": 1, "stream_id": "xxx"}
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/web/web_connect.py (send_event)
type EventFrame struct {
	// Type 帧类型，固定为 "event"
	Type string `json:"type"`
	// Event 事件名称（如 "chat.chunk"、"chat.final"）
	Event string `json:"event"`
	// Payload 事件负载
	Payload map[string]any `json:"payload"`
	// Seq 流式序号（可选，用于流式事件的排序）
	Seq int `json:"seq,omitempty"`
	// StreamID 流式标识（可选，用于关联同一次流式响应的多个事件）
	StreamID string `json:"stream_id,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// FrameType 帧类型枚举。
//
// 定义 WebSocket 帧协议的三种帧类型：req（请求）、res（响应）、event（事件）。
// 与 Python WebChannel 中的 type 字段值一一对应。
type FrameType string

const (
	// FrameTypeReq 请求帧
	FrameTypeReq FrameType = "req"
	// FrameTypeRes 响应帧
	FrameTypeRes FrameType = "res"
	// FrameTypeEvent 事件帧
	FrameTypeEvent FrameType = "event"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DecodeFrame 从 JSON 字节流解码为通用 map，提取帧类型。
//
// 不做完整帧解析，仅返回原始 map 供上层按 type 分发。
// 对齐 Python json.loads(raw) 后 data.get("type") 的两步解析模式。
func DecodeFrame(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("帧数据为空")
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("JSON 解码失败: %w", err)
	}
	return m, nil
}

// DecodeReqFrame 从 JSON 字节流解码为 ReqFrame。
//
// 对齐 Python _handle_raw_message 中 json.loads(raw) 后
// 校验 type=="req"、id 为 str、method 为 str 的解析逻辑。
func DecodeReqFrame(data []byte) (*ReqFrame, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("帧数据为空")
	}
	var f ReqFrame
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("JSON 解码 ReqFrame 失败: %w", err)
	}
	return &f, nil
}

// Validate 校验 ReqFrame 的必填字段。
//
// 校验规则（对齐 Python _handle_raw_message）：
//  1. type 必须为 "req"
//  2. id 必须非空字符串
//  3. method 必须非空字符串
func (f *ReqFrame) Validate() error {
	if f.Type != string(FrameTypeReq) {
		return fmt.Errorf("ReqFrame.type 必须为 %q, 实际为 %q", FrameTypeReq, f.Type)
	}
	if f.ID == "" {
		return fmt.Errorf("ReqFrame.id 不能为空")
	}
	if f.Method == "" {
		return fmt.Errorf("ReqFrame.method 不能为空")
	}
	return nil
}

// NewResFrame 构造响应帧。
//
// 对齐 Python WebChannel.send_response 中 frame 字典构建逻辑：
//   - ok=true 时仅设置 payload
//   - ok=false 时额外设置 error 和可选 code
func NewResFrame(reqID string, ok bool, payload map[string]any, errMsg string, code string) *ResFrame {
	f := &ResFrame{
		Type:    string(FrameTypeRes),
		ID:      reqID,
		OK:      ok,
		Payload: payload,
	}
	if !ok {
		if errMsg == "" {
			f.Error = "request failed"
		} else {
			f.Error = errMsg
		}
		if code != "" {
			f.Code = code
		}
	}
	return f
}

// NewEventFrame 构造事件帧。
//
// 对齐 Python WebChannel.send_event 中 frame 字典构建逻辑：
//   - seq 和 stream_id 仅在非零值时序列化（omitempty）
func NewEventFrame(event string, payload map[string]any, seq int, streamID string) *EventFrame {
	return &EventFrame{
		Type:     string(FrameTypeEvent),
		Event:    event,
		Payload:  payload,
		Seq:      seq,
		StreamID: streamID,
	}
}

// Encode 编码 ResFrame 为 JSON 字节流。
func (f *ResFrame) Encode() ([]byte, error) {
	return json.Marshal(f)
}

// Encode 编码 EventFrame 为 JSON 字节流。
func (f *EventFrame) Encode() ([]byte, error) {
	return json.Marshal(f)
}

// Encode 编码 ReqFrame 为 JSON 字节流。
func (f *ReqFrame) Encode() ([]byte, error) {
	return json.Marshal(f)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

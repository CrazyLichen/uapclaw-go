package videotask

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// AckTimeout ACK 等待上限。
// 对齐 bridge.py:14 ACK_TIMEOUT = 15。
const AckTimeout = 15 * time.Second

// BridgeEvent 与网关写侧共享的线协议事件名。
// 对齐 bridge.py:15 EVENT = "voice.task.command"。
const BridgeEvent = "voice.task.command"

// ──────────────────────────── 全局变量 ────────────────────────────

// pending ACK 登记表：command_id → (identity, result 通道)。
// 对齐 bridge.py:13 _pending（Python asyncio.Future → Go buffered chan）。
var pending = struct {
	sync.RWMutex
	m map[string]*pendingAck
}{m: map[string]*pendingAck{}}

type pendingAck struct {
	identity map[string]any
	result   chan map[string]any
	once     sync.Once
}

// resolveACK 结果经 RPC 参数带回；Python future.set_result → chan 发送。
func (p *pendingAck) settle(result map[string]any) {
	p.once.Do(func() {
		if result == nil {
			result = map[string]any{}
		}
		p.result <- result
		close(p.result)
	})
}

// SendRuntimePush 推送缝（对齐 Python send_runtime_push）。
// 装配层注入；delivered=false 语义由返回 error 表达（Python False → error）。
var SendRuntimePush func(msg map[string]any) bool

// ──────────────────────────── 结构体 ────────────────────────────

// RemoteTaskEndpoint AgentServer 侧检查点协议句柄；不含任务数据库与缓存态。
// 对齐 bridge.py:19-50 RemoteTaskEndpoint。
type RemoteTaskEndpoint struct {
	identity map[string]any
}

// NewRemoteTaskEndpoint 从请求参数解析受管任务绑定身份。
// 对齐 bridge.py:20-27 __init__（缺 endpoint/task_id 即不可用）。
func NewRemoteTaskEndpoint(params map[string]any, requestID, sessionID, userID string) (*RemoteTaskEndpoint, error) {
	binding, _ := params["managed_task_binding"].(map[string]any)
	if binding == nil || binding["endpoint"] == nil || binding["endpoint"] == "" ||
		binding["task_id"] == nil || binding["task_id"] == "" {
		return nil, errors.New("Managed task execution binding is unavailable")
	}
	identity := map[string]any{}
	for k, v := range binding {
		identity[k] = v
	}
	identity["owner"] = userID
	identity["session_id"] = sessionID
	identity["request_id"] = requestID
	return &RemoteTaskEndpoint{identity: identity}, nil
}

// Call 调用网关侧账本命令并等 ACK。
// 对齐 bridge.py:29-49 call（push EVENT 帧 → 等 ACK → 错误透传；finally 注销）。
func (r *RemoteTaskEndpoint) Call(action string, data map[string]any) (map[string]any, error) {
	commandID := fmt.Sprintf("task-checkpoint-%d-%d", time.Now().UnixNano(), syncID.Add(1))
	command := map[string]any{"command_id": commandID, "action": action}
	for k, v := range r.identity {
		command[k] = v
	}
	for k, v := range data {
		command[k] = v
	}
	ack := &pendingAck{identity: r.identity, result: make(chan map[string]any, 1)}
	pending.Lock()
	pending.m[commandID] = ack
	pending.Unlock()
	defer func() {
		pending.Lock()
		delete(pending.m, commandID)
		pending.Unlock()
	}()
	// Python: send_runtime_push({... payload: {event_type: EVENT, **command}})
	if SendRuntimePush == nil {
		return nil, errors.New("Task checkpoint could not reach Gateway")
	}
	delivered := SendRuntimePush(map[string]any{
		"request_id": r.identity["request_id"], "channel_id": "video_tool",
		"session_id": r.identity["session_id"],
		"payload": func() map[string]any {
			payload := map[string]any{"event_type": BridgeEvent}
			for k, v := range command {
				payload[k] = v
			}
			return payload
		}(),
	})
	if !delivered {
		return nil, errors.New("Task checkpoint could not reach Gateway")
	}
	select {
	case result := <-ack.result:
		if errText, ok := result["error"]; ok && errText != nil && errText != "" {
			return nil, errors.New(fmt.Sprint(errText))
		}
		return result, nil
	case <-time.After(AckTimeout):
		return nil, errors.New("Task checkpoint acknowledgement timed out")
	}
}

// ResolveCheckpointAck 处理 ACK RPC：匹配登记并结算结果。
// 对齐 bridge.py:52-68 resolve_checkpoint_ack（身份三元组校验，不匹配报错）。
func ResolveCheckpointAck(params map[string]any, userID, sessionID string) (bool, error) {
	commandID := ""
	if v, ok := params["command_id"].(string); ok {
		commandID = v
	}
	pending.RLock()
	ack := pending.m[commandID]
	pending.RUnlock()
	if ack == nil {
		return false, nil
	}
	executionRequestID := ""
	if v, ok := params["execution_request_id"].(string); ok {
		executionRequestID = v
	}
	if userID != toStringAny(ack.identity["owner"]) ||
		sessionID != toStringAny(ack.identity["session_id"]) ||
		executionRequestID != toStringAny(ack.identity["request_id"]) {
		return false, errors.New("Task checkpoint acknowledgement identity mismatch")
	}
	result, _ := params["result"].(map[string]any)
	ack.settle(result)
	return true, nil
}

// toStringAny nil 安全字符串化（bridge 内部辅助；Python 侧无对应实现）。
func toStringAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// syncID 命令 ID 原子序号。
var syncID atomic.Int64

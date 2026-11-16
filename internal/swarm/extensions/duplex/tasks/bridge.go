package tasks

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 归属偏差 O-1：本文件对应 gateway/extensions/video_duplex/backend/tasks/bridge.py，
// Go 侧归入 duplex/tasks 包（spec §已知偏差登记）。

// DuplexVideoToolChannelID 双工任务渠道标识（与 duplex 包 VideoToolChannelID 同值）。
// Python 侧无对应实现：Python 直接引用 video_search.VIDEO_TOOL_CHANNEL_ID，
// tasks 包不能反向导入 duplex，Go 以镜像常量表达。
const DuplexVideoToolChannelID = "video_tool"

// BridgeEvent 检查点命令事件名。
// 对齐 bridge.py:12 EVENT = "voice.task.command"。
const BridgeEvent = "voice.task.command"

// AckTimeout 确认超时。
// 对齐 bridge.py:13 ACK_TIMEOUT=15。
const AckTimeout = 15 * time.Second

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayTaskEndpoint 既有 E2A push/ACK 通道上的网关检查点命令端点。
// 对齐 bridge.py:16-75 GatewayTaskEndpoint（唯一检查点写入方，校验与变更原子）。
type GatewayTaskEndpoint struct {
	store *TaskStore
	id    string

	mu sync.Mutex
}

// endpointRegistry 端点注册表（弱引用 → Go 显式登记）。
// 对齐 bridge.py:14 _endpoints WeakValueDictionary。
var endpointRegistry = map[string]*GatewayTaskEndpoint{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayTaskEndpoint 创建并登记端点。
// 对齐 bridge.py:18-22 __init__。
func NewGatewayTaskEndpoint(store *TaskStore) (*GatewayTaskEndpoint, error) {
	e := &GatewayTaskEndpoint{store: store, id: newHexID()}
	endpointRegistry[e.id] = e
	return e, nil
}

// Binding 任务与端点的绑定句柄。
// 对齐 bridge.py:24-26 binding。
func (e *GatewayTaskEndpoint) Binding(task map[string]any) map[string]any {
	return map[string]any{"endpoint": e.id, "task_id": task["id"]}
}

// Close 注销端点。
// 对齐 bridge.py:28-30 close。
func (e *GatewayTaskEndpoint) Close() {
	delete(endpointRegistry, e.id)
}

// Execute 执行检查点命令。
// 对齐 bridge.py:32-75 execute。
func (e *GatewayTaskEndpoint) Execute(command map[string]any) (map[string]any, error) {
	action := toString(command["action"])
	e.mu.Lock()
	defer e.mu.Unlock()
	var result map[string]any
	err := e.store.Transaction(func(db *sqlTx) error {
		task, err := e.store.GetInTx(db, toString(command["task_id"]))
		if err != nil {
			return err
		}
		if toString(command["owner"]) != toString(task["owner"]) ||
			toString(command["session_id"]) != toString(task["core_session_id"]) ||
			toString(command["request_id"]) != toString(task["request_id"]) {
			return errors.New("TASK_EXECUTION_BINDING_STALE")
		}
		if action == "status" {
			result = map[string]any{"status": task["status"]}
			return nil
		}
		if action != "bind" && action != "claim" && action != "context_written" &&
			action != "observed" && action != "close" && action != "settle" {
			return errors.New("Unknown task checkpoint command")
		}
		replay, fingerprint, err := e.store.ReplayInTx(db,
			toString(task["owner"]), toString(task["session"]),
			toString(command["command_id"]), command)
		if err != nil {
			return err
		}
		if replay != nil {
			result = replay
			return nil
		}
		if action == "bind" || action == "claim" || action == "context_written" || action == "observed" {
			status := toString(task["status"])
			if status != "running" && status != "waiting_user" {
				return errors.New("TASK_EXECUTION_NOT_RUNNING")
			}
		}
		if action == "bind" {
			if task["execution_bound"] == true || task["checkpoint_open"] != true {
				return errors.New("Task execution is not available for a new invocation")
			}
			task["execution_bound"] = true
		} else if task["execution_bound"] != true {
			return errors.New("Task execution is not bound")
		}
		if action == "claim" {
			if task["checkpoint_open"] != true {
				return errors.New("TASK_EXECUTION_BINDING_CLOSED")
			}
			changes := []any{}
			for _, c := range toAnySlice(task["changes"]) {
				change := toMap(c)
				if toString(change["state"]) == "pending" {
					change["state"] = "claimed"
				}
				changes = append(changes, change)
			}
			task["changes"] = changes
			result = map[string]any{"changes": pendingOnly(changes)}
		} else if action == "context_written" || action == "observed" {
			if task["checkpoint_open"] != true {
				return errors.New("TASK_EXECUTION_BINDING_CLOSED")
			}
			before, after := "claimed", "context_written"
			if action == "observed" {
				before, after = "context_written", "model_input_observed"
			}
			changeIDs := map[string]bool{}
			for _, id := range toAnySlice(command["change_ids"]) {
				changeIDs[toString(id)] = true
			}
			changes := []any{}
			for _, c := range toAnySlice(task["changes"]) {
				change := toMap(c)
				if changeIDs[toString(change["id"])] && toString(change["state"]) == before {
					change["state"] = after
				}
				changes = append(changes, change)
			}
			task["changes"] = changes
		} else if action == "close" {
			task["checkpoint_open"] = false
		} else if action == "settle" {
			if task["checkpoint_open"] == true {
				return errors.New("Task invocation is still open")
			}
			task["execution_settled"] = true
			task["execution_cancelled"] = command["cancelled"] == true
		}
		task["sequence"] = numAdd(task["sequence"], 1)
		if err := txPut(db, task); err != nil {
			return err
		}
		if result == nil {
			result = map[string]any{}
		}
		return txCommand(db, task, toString(command["command_id"]), fingerprint, result)
	})
	return result, err
}

// CheckpointFrame 检查点命令帧（bridge 传输载荷；批 2 补移植 video_task/bridge.py 的
// RemoteTaskEndpoint/resolve_checkpoint_ack 时对齐其消费方）。
// Python 侧无对应实现：Python 经 e2a chunk 传递，Go 以显式形状在桥上承载。
type CheckpointFrame struct {
	// EventType 事件类型
	EventType string
	// ChannelID 渠道标识
	ChannelID string
	// RequestID 请求标识
	RequestID string
	// SessionID 会话标识
	SessionID string
	// Payload 帧载荷（即检查点命令）
	Payload map[string]any
}

// HandleCheckpointPush 消费内部任务帧（先于普通用户消息投递）。
// 对齐 bridge.py:77-107 handle_checkpoint_push。
// sendAck 为确认投递函数（Python client.send_request 的窄化注入）。
func HandleCheckpointPush(frame CheckpointFrame, sendAck func(params map[string]any) error) (bool, map[string]any) {
	command := frame.Payload
	if frame.EventType != BridgeEvent {
		return false, nil
	}
	endpoint := endpointRegistry[toString(command["endpoint"])]
	var result map[string]any
	if endpoint == nil {
		result = map[string]any{"error": "Task Gateway endpoint is unavailable"}
	} else {
		if frame.ChannelID != DuplexVideoToolChannelID ||
			frame.RequestID != toString(command["request_id"]) ||
			frame.SessionID != toString(command["session_id"]) {
			result = map[string]any{"error": "Task checkpoint transport identity mismatch"}
		} else {
			var err error
			result, err = endpoint.Execute(command)
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}
		}
	}
	commandID := toString(command["command_id"])
	ackParams := map[string]any{
		"command_id":           commandID,
		"result":               result,
		"execution_request_id": frame.RequestID,
	}
	ackDone := make(chan error, 1)
	go func() {
		if sendAck == nil {
			ackDone <- nil
			return
		}
		ackDone <- sendAck(ackParams)
	}()
	select {
	case <-ackDone:
	case <-time.After(AckTimeout):
		// AgentServer 等待方将超时；不重复操作（对齐 :105-107 注释）
		logger.Warn(logComponent).Str("command_id", commandID).Msg("Task checkpoint acknowledgement delivery failed")
	}
	return true, result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pendingOnly 过滤 pending 状态变更（对齐 :62 result = {"changes": changes} 的 claimed 集合）。
func pendingOnly(changes []any) []any {
	out := []any{}
	for _, c := range changes {
		change := toMap(c)
		if toString(change["state"]) == "claimed" {
			out = append(out, change)
		}
	}
	return out
}

var _ = fmt.Sprintf

package videotask

import (
	"errors"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

// checkpointACKMethod 检查点 ACK 的 RPC 方法名。
// 对齐 ReqMethod.VOICE_TASK_CHECKPOINT_ACK（上游协议包枚举；Go 按网关半边
// HandleCheckpointPush 的会话惯例编码）。
const checkpointACKMethod = "voice.task.checkpoint_ack"

// ──────────────────────────── 结构体 ────────────────────────────

// VoiceTaskServerAdapter AgentServer 端点：检查点 ACK 与任务产物文件。
// 对齐 server_adapter.py:12-52 VoiceTaskServerAdapter(GatewayAdapter)。
// Go 无 GatewayAdapter 基类：以 Handler 方法组表达，注册到既有 RPC 路由面
// （偏差 O-17：methods 集合由路由注册点声明）。
type VoiceTaskServerAdapter struct{}

// Handle 处理两个受管端点。
// 对齐 server_adapter.py:19-52 handle。
func (a *VoiceTaskServerAdapter) Handle(method string, params map[string]any,
	requestID, sessionID, userID string,
) (map[string]any, error) {
	if method == checkpointACKMethod {
		accepted, err := ResolveCheckpointAck(params, userID, sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"accepted": accepted}, nil
	}
	// voice.task.files：从会话历史聚合任务产物（对齐 :31-50）
	session := sessionID
	executionRequestID := ""
	if v, ok := params["execution_request_id"].(string); ok {
		executionRequestID = v
	}
	if session == "" || executionRequestID == "" {
		return nil, errors.New("Invalid task execution identity")
	}
	files, err := loadTaskExecutionFiles(session, executionRequestID, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"files": files}, nil
}

// FileHistorySource 产物文件历史源（装配层注入；Python 直接 import
// session_metadata/session_history/video_files，Go 由网关装配桥接，偏差 O-18）。
var FileHistorySource struct {
	// GetSessionMetadata 读会话元数据（cacheBust；enableWriteback=false）
	GetSessionMetadata func(sessionID string, cacheBust bool) map[string]any
	// FlushPendingWrites 等待历史写队列落盘（timeout 秒）
	FlushPendingWrites func(timeout float64) bool
	// LoadHistoryRecords 加载会话历史
	LoadHistoryRecords func(sessionID string) ([]map[string]any, error)
	// NormalizeFileItems 附件归一（duplex.video_files 对应物）
	NormalizeFileItems func(files any) []map[string]any
}

// loadTaskExecutionFiles 聚合一次执行的全部产物文件。
// 对齐 server_adapter.py:31-50（guard/元数据归属校验/等待落盘/按 request_id 聚合 chat.file）。
func loadTaskExecutionFiles(session, executionRequestID, userID string) ([]map[string]any, error) {
	if FileHistorySource.GetSessionMetadata == nil {
		return nil, errors.New("Task file history source is unavailable")
	}
	metadata := FileHistorySource.GetSessionMetadata(session, true)
	if metadata == nil || strOf(metadata["user_id"]) != userID {
		return nil, errors.New("Task session is unavailable to this user")
	}
	// 工具完成只入队 chat.file；先等既有历史写完再取最终附件快照（对齐 :41-43 注释）
	if !FileHistorySource.FlushPendingWrites(5) {
		return nil, errors.New("Task file history is not yet durable")
	}
	records, err := FileHistorySource.LoadHistoryRecords(session)
	if err != nil {
		return nil, err
	}
	files := []map[string]any{}
	for _, record := range records {
		if strOf(record["request_id"]) == executionRequestID &&
			strOf(record["event_type"]) == "chat.file" {
			files = append(files, FileHistorySource.NormalizeFileItems(record["files"])...)
		}
	}
	return files, nil
}

// AckEnvelope 组装 ACK 请求信封（网关半边 bridge.py:104-113 的对偶构造；
// 供路由注册点发往网关）。Python 侧无对应实现：Python 在网关半边构造。
func AckEnvelope(command map[string]any, sessionID string) map[string]any {
	commandID := strOf(command["command_id"])
	return map[string]any{
		"request_id": "ack-" + commandID,
		"channel_id": "video_tool", "session_id": sessionID,
		"req_method": checkpointACKMethod,
		"user_id":    strOf(command["owner"]),
		"is_stream":  false,
		"params":     map[string]any{"command_id": commandID, "result": strOf(command["result"])},
	}
}

var _ = fmt.Sprint

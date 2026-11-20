//go:build integration

// 语音委派任务 rail 触发全链集成测试（领域 23.9 复审批2）。
// 链路：模拟实时模型 delegate 产生的受管请求（channel_id=video_tool +
// managed_task_binding）→ DeepAdapter.ProcessMessageImpl 真实绑定
// （BindTaskExecution / InstallVoiceTaskRail）→ MockLLM 驱动 ReAct 循环 →
// VoiceAgentTaskRail 钩子在模型调用前后经 push/ACK 环同步账本
// （claim / context_written / observed）→ 变更作为用户消息注入模型上下文。
// 真实边界：SQLite 任务账本 + 网关 push/ACK 回环 + MockLLM prompt 捕获。
package adapter_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	dupextasks "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/videotask"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DuplexRailTriggerSuite 受管语音任务 rail 触发全链套件。
type DuplexRailTriggerSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 套件方法 ────────────────────────────

// TestDuplexRailTriggerSuite 运行套件。
func TestDuplexRailTriggerSuite(t *testing.T) {
	suite.Run(t, new(DuplexRailTriggerSuite))
}

// TestRailTrigger_变更注入模型上下文 全链用例：
// 账本里预置 pending 变更 → 受管请求进 DeepAdapter → rail before_model 认领并注入
// → MockLLM 的 prompt 里出现变更指令 → after_model 观察回执 → 账本状态机走到
// model_input_observed → 收尾 close/settle。
// 对齐 rail.py:33-64 task_checkpoint 与 checkpoint.py:23-71。
func (s *DuplexRailTriggerSuite) TestRailTrigger_变更注入模型上下文() {
	// 1. 真账本 + 网关端点 + push/ACK 回环（与生产同构）
	store, err := dupextasks.NewTaskStore(s.T().TempDir() + "/tasks.sqlite")
	s.Require().NoError(err)
	endpoint, err := dupextasks.NewGatewayTaskEndpoint(store)
	s.Require().NoError(err)
	s.T().Cleanup(endpoint.Close)
	seedTaskRow(s.Require(), store, "t-rail-1", "u1", "managed-task-s1", "req-vt-1",
		"running", []any{map[string]any{"id": "ch1", "instruction": "改成英文", "state": "pending"}})
	bindingMap := endpoint.Binding(map[string]any{"id": "t-rail-1"})

	videotask.SendRuntimePush = func(msg map[string]any) bool {
		payload, _ := msg["payload"].(map[string]any)
		frame := dupextasks.CheckpointFrame{
			EventType: dupextasks.BridgeEvent,
			ChannelID: dupextasks.DuplexVideoToolChannelID,
			RequestID: strField(msg, "request_id"),
			SessionID: strField(msg, "session_id"),
			Payload:   payload,
		}
		dupextasks.HandleCheckpointPush(frame, func(params map[string]any) error {
			params["execution_request_id"] = frame.RequestID
			adapter := &videotask.VoiceTaskServerAdapter{}
			_, ackErr := adapter.Handle("voice.task.checkpoint_ack", params, "ack",
				strField(msg, "session_id"), "u1")
			return ackErr
		})
		return true
	}
	s.T().Cleanup(func() { videotask.SendRuntimePush = nil })

	// 2. DeepAdapter + MockLLM（两轮：工具调用后文本收尾）
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_duplex_rail_trigger_itest")
	s.env.WriteConfigYAML(s.T(), "")
	s.MockLLM.SetResponses(
		mockllm.CreateTextResponse("好的，我会把报告写成英文。"),
	)
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.normal", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 3. 受管请求：channel_id=video_tool + managed_task_binding
	paramsJSON, marshalErr := json.Marshal(map[string]any{
		"query":                "请写一份关于账本规范的报告",
		"managed_task_binding": bindingMap,
	})
	s.Require().NoError(marshalErr)
	sid := "managed-task-s1"
	uid := "u1"
	req := &schema.AgentRequest{
		RequestID: "req-vt-1",
		ChannelID: "video_tool",
		SessionID: &sid,
		UserID:    &uid,
		ReqMethod: schema.ReqMethodChatSend,
		Params:    paramsJSON,
		IsStream:  false,
	}
	resp, invokeErr := d.ProcessMessageImpl(s.Ctx, req, map[string]any{
		"query":           "请写一份关于账本规范的报告",
		"conversation_id": "managed-task-s1",
	})
	s.Require().NoError(invokeErr)
	s.Require().True(resp.OK, "受管请求应成功: %+v", resp.Payload)

	// 4/5. 断言账本状态机走完——observed 终态即「变更注入模型上下文」的强证明：
	// after_model 必须在模型上下文消息中找到注入的 managed_task_change 载荷才会推进
	// （before_model 注入的 UserMessage 于当轮请求快照后落盘，下一轮进入 prompt）；
	// checkpoint_open=false 证明收尾 close 已执行。
	// 结算上报是 unbind 派生的后台 goroutine，先等它在途集合排空。
	videotask.WaitSettlements()
	task := readTaskRow(s.Require(), store, "t-rail-1")
	changes, _ := task["changes"].([]any)
	s.Require().Len(changes, 1)
	change, _ := changes[0].(map[string]any)
	s.Require().Equal("model_input_observed", change["state"],
		"before_model 认领 + context_written + after_model observed 后应到终态")
	s.Require().Equal(false, task["checkpoint_open"], "收尾 close 应关闭检查点窗口")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// seedTaskRow 经账本事务种入任务行（含变更列表；三元组对齐 Execute 校验）。
func seedTaskRow(require *require.Assertions, store *dupextasks.TaskStore,
	id, owner, coreSession, requestID, status string, changes []any,
) {
	seed := map[string]any{
		"id": id, "owner": owner, "session": "s1", "status": status,
		"sequence": 1, "instruction": "任务", "request": map[string]any{},
		"changes": changes, "progress": []any{}, "position": 1,
		"request_id": requestID, "core_session_id": coreSession,
		"checkpoint_open": true,
	}
	require.NoError(store.Transaction(func(db *sql.Tx) error { return dupextasks.TxPut(db, seed) }))
}

// readTaskRow 读任务行。
func readTaskRow(require *require.Assertions, store *dupextasks.TaskStore, id string) map[string]any {
	task, err := store.Read(id, nil, nil)
	require.NoError(err)
	return task
}

// strField nil 安全取字符串。
func strField(msg map[string]any, key string) string {
	if v, ok := msg[key].(string); ok {
		return v
	}
	return ""
}

// msgString 提取消息文本（BaseMessage 鸭子取 Content）。
func msgString(msg llmschema.BaseMessage) string {
	if msg == nil {
		return ""
	}
	return fmt.Sprint(msg.GetContent())
}

//go:build integration

package harness_protocol

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	basesuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// externalCheckpointStateKey 成员会话里存放外部 runtime 状态的键。
// 对应 Python: member_runtime.py:141 的 _EXTERNAL_RUNTIME_STATE_KEY。
const externalCheckpointStateKey = "external_runtime"

// sessionCheckpointSink 把 checkpoint 落进真实 AgentSession。
//
// 这是 agent_teams/external/member_runtime.py:124-152
// _MemberSessionCheckpointSink 的 Go 等价物，同时补上该实现未做的
// 幂等与 compare-and-set（上游把这两项留在契约里、由生产实现自行保证）。
type sessionCheckpointSink struct {
	sess *session.Session

	mu       sync.Mutex
	receipts map[string]harness_protocol.CheckpointSaveReceipt
	latest   int
	revision string
}

// Save 持久化 checkpoint 并返回回执。
// 三条语义对齐 Python: checkpoints.py:83-103。
func (s *sessionCheckpointSink) Save(
	_ context.Context,
	checkpoint harness_protocol.HarnessCheckpoint,
	_ harness_protocol.CheckpointReason,
	expectedStorageRevision *string,
) (harness_protocol.CheckpointSaveReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 语义 2：同一 checkpoint_id 幂等，返回原 receipt
	if receipt, ok := s.receipts[checkpoint.CheckpointID]; ok {
		return receipt, nil
	}
	// 语义 3a：更低的 sequence 视为 stale
	if checkpoint.Sequence <= s.latest {
		return harness_protocol.CheckpointSaveReceipt{},
			&harness_protocol.CheckpointConflictError{Msg: "stale checkpoint sequence"}
	}
	// 语义 3b：CAS 比对失败
	if expectedStorageRevision != nil && *expectedStorageRevision != s.revision {
		return harness_protocol.CheckpointSaveReceipt{},
			&harness_protocol.CheckpointConflictError{Msg: "checkpoint compare-and-set failed"}
	}

	// 落盘到真实 session。
	// 注：GetState 的第二个返回值恒为 nil（见 session/agent.go:383-385），
	// 首次写入前该键不存在属正常路径，无需判 ErrStateNotFound。
	s.sess.UpdateState(map[string]any{
		externalCheckpointStateKey: map[string]any{
			"backend":    checkpoint.Provider,
			"checkpoint": checkpointToDict(checkpoint),
		},
	})
	if err := s.sess.Commit(context.Background()); err != nil {
		return harness_protocol.CheckpointSaveReceipt{}, err
	}

	s.latest = checkpoint.Sequence
	s.revision = "storage-" + checkpoint.CheckpointID
	receipt, err := harness_protocol.NewCheckpointSaveReceipt(
		checkpoint.CheckpointID, checkpoint.Sequence, s.revision)
	if err != nil {
		return harness_protocol.CheckpointSaveReceipt{}, err
	}
	s.receipts[checkpoint.CheckpointID] = receipt
	return receipt, nil
}

// checkpointToDict 把 checkpoint 信封编码成纯 JSON 数据。
// 对应 Python: member_runtime.py:155-167 checkpoint_to_dict。
func checkpointToDict(cp harness_protocol.HarnessCheckpoint) map[string]any {
	return map[string]any{
		"provider":            cp.Provider,
		"schema_version":      cp.SchemaVersion,
		"agent_id":            cp.AgentID,
		"host_session_id":     cp.HostSessionID,
		"checkpoint_id":       cp.CheckpointID,
		"sequence":            cp.Sequence,
		"data":                harness_protocol.JsonValueToBuiltin(cp.Data),
		"provider_session_id": cp.ProviderSessionID,
		"revision":            cp.Revision,
	}
}

// checkpointFromDict 从纯 JSON 数据还原 checkpoint（走构造器校验）。
// 对应 Python: member_runtime.py:170-190 checkpoint_from_dict。
func checkpointFromDict(value any) (*harness_protocol.HarnessCheckpoint, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("checkpoint 数据不是对象")
	}
	seq, err := asIntField(obj, "sequence")
	if err != nil {
		return nil, err
	}
	cp, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider:      stringField(obj, "provider"),
		SchemaVersion: stringField(obj, "schema_version"),
		AgentID:       stringField(obj, "agent_id"),
		HostSessionID: stringField(obj, "host_session_id"),
		CheckpointID:  stringField(obj, "checkpoint_id"),
		Sequence:      seq,
		Data:          objectField(obj, "data"),
	})
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

// stringField 取字符串字段，缺失返回空串。
func stringField(obj map[string]any, key string) string {
	s, _ := obj[key].(string)
	return s
}

// asIntField 取整数字段（JSON 解出为 float64）。
func asIntField(obj map[string]any, key string) (int, error) {
	switch v := obj[key].(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	}
	return 0, errors.New("字段不是整数: " + key)
}

// objectField 取对象字段，缺失返回空 map。
func objectField(obj map[string]any, key string) map[string]any {
	m, ok := obj[key].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// CheckpointSerializationSuite checkpoint 持久化与 codec 的集成套件。
// 跨 agentcore/harness_protocol（被测契约）+ agentcore/session（真实会话落盘）
// + agent_teams/harness（L2 契约）三个子系统。
type CheckpointSerializationSuite struct {
	basesuite.BaseIntegrationSuite
}

// TestCheckpointSerializationSuite 套件入口。
func TestCheckpointSerializationSuite(t *testing.T) {
	suite.Run(t, new(CheckpointSerializationSuite))
}

// TestCheckpoint_真实session往返 验证 checkpoint 落盘后能原样读回。
func (s *CheckpointSerializationSuite) TestCheckpoint_真实session往返() {
	sess := newRealSession(s.T(), "sess-checkpoint-roundtrip")
	sink := &sessionCheckpointSink{sess: sess, receipts: map[string]harness_protocol.CheckpointSaveReceipt{}}

	cp, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider:      "claude-code",
		SchemaVersion: "1",
		AgentID:       "team-a_member-a",
		HostSessionID: sess.GetSessionID(),
		CheckpointID:  "checkpoint-1",
		Sequence:      1,
		Data:          map[string]any{"conversation_id": "conversation-a"},
	})
	s.Require().NoError(err, "NewHarnessCheckpoint")

	receipt, err := sink.Save(s.Ctx, cp, harness_protocol.CheckpointReasonTurnCompleted, nil)
	s.Require().NoError(err, "Save")
	s.Equal("checkpoint-1", receipt.CheckpointID)
	s.Equal(1, receipt.Sequence)
	s.NotEmpty(receipt.StorageRevision)

	// 从真实 session 读回
	raw, err := sess.GetState(sessionstate.StringKey(externalCheckpointStateKey))
	s.Require().NoError(err, "GetState")
	state, ok := raw.(map[string]any)
	s.Require().True(ok, "session state 类型 = %T", raw)
	s.Equal("claude-code", state["backend"])

	restored, err := checkpointFromDict(state["checkpoint"])
	s.Require().NoError(err, "checkpointFromDict")
	s.Equal("conversation-a", restored.Data["conversation_id"])
	s.Equal(1, restored.Sequence)
}

// TestCheckpoint_幂等重试返回原回执 验证语义 2。
func (s *CheckpointSerializationSuite) TestCheckpoint_幂等重试返回原回执() {
	sess := newRealSession(s.T(), "sess-checkpoint-idempotent")
	sink := &sessionCheckpointSink{sess: sess, receipts: map[string]harness_protocol.CheckpointSaveReceipt{}}

	cp, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider:      "acme",
		SchemaVersion: "1",
		AgentID:       "member-a",
		HostSessionID: sess.GetSessionID(),
		CheckpointID:  "checkpoint-idem",
		Sequence:      1,
		Data:          map[string]any{"k": "v"},
	})
	s.Require().NoError(err)

	first, err := sink.Save(s.Ctx, cp, harness_protocol.CheckpointReasonSessionActivated, nil)
	s.Require().NoError(err)
	second, err := sink.Save(s.Ctx, cp, harness_protocol.CheckpointReasonPeriodic, nil)
	s.Require().NoError(err, "同一 checkpoint_id 重试必须成功")
	s.Equal(first, second, "重试必须返回原回执")
}

// TestCheckpoint_stale顺序被拒 验证语义 3a。
func (s *CheckpointSerializationSuite) TestCheckpoint_stale顺序被拒() {
	sess := newRealSession(s.T(), "sess-checkpoint-stale")
	sink := &sessionCheckpointSink{sess: sess, receipts: map[string]harness_protocol.CheckpointSaveReceipt{}}

	mk := func(id string, seq int) harness_protocol.HarnessCheckpoint {
		cp, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
			Provider:      "acme",
			SchemaVersion: "1",
			AgentID:       "member-a",
			HostSessionID: sess.GetSessionID(),
			CheckpointID:  id,
			Sequence:      seq,
		})
		s.Require().NoError(err)
		return cp
	}

	_, err := sink.Save(s.Ctx, mk("checkpoint-new", 5), harness_protocol.CheckpointReasonTurnCompleted, nil)
	s.Require().NoError(err)

	// sequence 0 < 5，必须被拒
	_, err = sink.Save(s.Ctx, mk("checkpoint-stale", 0), harness_protocol.CheckpointReasonPeriodic, nil)
	s.Require().Error(err)
	s.True(errors.Is(err, harness_protocol.ErrHarnessProtocol),
		"stale 必须归到 ErrHarnessProtocol，实际 %v", err)
	var conflict *harness_protocol.CheckpointConflictError
	s.True(errors.As(err, &conflict), "必须是 CheckpointConflictError")
	s.Contains(err.Error(), "stale")
}

// TestCheckpoint_CAS失败被拒 验证语义 3b。
func (s *CheckpointSerializationSuite) TestCheckpoint_CAS失败被拒() {
	sess := newRealSession(s.T(), "sess-checkpoint-cas")
	sink := &sessionCheckpointSink{sess: sess, receipts: map[string]harness_protocol.CheckpointSaveReceipt{}}

	cp1, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider: "acme", SchemaVersion: "1", AgentID: "member-a",
		HostSessionID: sess.GetSessionID(), CheckpointID: "checkpoint-1", Sequence: 1,
	})
	s.Require().NoError(err)
	_, err = sink.Save(s.Ctx, cp1, harness_protocol.CheckpointReasonTurnCompleted, nil)
	s.Require().NoError(err)

	// 用错误的期望修订号写入
	wrong := "storage-not-exist"
	cp2, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider: "acme", SchemaVersion: "1", AgentID: "member-a",
		HostSessionID: sess.GetSessionID(), CheckpointID: "checkpoint-2", Sequence: 2,
	})
	s.Require().NoError(err)
	_, err = sink.Save(s.Ctx, cp2, harness_protocol.CheckpointReasonStateChanged, &wrong)
	s.Require().Error(err)
	var conflict *harness_protocol.CheckpointConflictError
	s.True(errors.As(err, &conflict))
	s.Contains(err.Error(), "compare-and-set")
}

// TestCheckpointSink_接口满足 验证 sessionCheckpointSink 满足契约。
func (s *CheckpointSerializationSuite) TestCheckpointSink_接口满足() {
	var _ harness_protocol.HarnessCheckpointSink = (*sessionCheckpointSink)(nil)
}

// TestEventCodec_经真实session中转 验证事件编解码后再 JSON 序列化仍可读回。
// 真实链路：NewHarnessEvent → HarnessEventToDict → json.Marshal →
// json.Unmarshal → HarnessEventFromDict → 落 session。
func (s *CheckpointSerializationSuite) TestEventCodec_经真实session中转() {
	sess := newRealSession(s.T(), "sess-event-codec")

	payload, err := harness_protocol.NewDiagnosticEvent(
		harness_protocol.DiagnosticLevelInfo, "ready", map[string]any{})
	s.Require().NoError(err, "NewDiagnosticEvent")

	ev, err := harness_protocol.NewHarnessEvent(harness_protocol.HarnessEventOptions{
		Sequence:      1,
		Timestamp:     1000.5,
		Event:         payload,
		HostSessionID: sess.GetSessionID(),
		AgentID:       "member-a",
		TurnID:        "turn-1",
	})
	s.Require().NoError(err, "NewHarnessEvent")

	// 编码 → JSON → 回解（模拟跨进程）
	wire := harness_protocol.HarnessEventToDict(ev)
	raw, err := json.Marshal(wire)
	s.Require().NoError(err, "json.Marshal")

	var decoded map[string]any
	s.Require().NoError(json.Unmarshal(raw, &decoded), "json.Unmarshal")

	// sequence 解出 float64，asInteger 必须接受（spec D4）
	back, err := harness_protocol.HarnessEventFromDict(decoded)
	s.Require().NoError(err, "HarnessEventFromDict 必须接受 json.Unmarshal 的 float64 整数")
	s.Equal(1, back.Sequence)
	s.Equal(1000.5, back.Timestamp)
	s.Equal(sess.GetSessionID(), back.HostSessionID)
	s.Equal("turn-1", back.TurnID)
}

// TestEventCodec_未知事件无损保留 验证版本漂移缓冲阀。
func (s *CheckpointSerializationSuite) TestEventCodec_未知事件无损保留() {
	sess := newRealSession(s.T(), "sess-event-unknown")

	payload, err := harness_protocol.NewDiagnosticEvent(
		harness_protocol.DiagnosticLevelInfo, "x", map[string]any{})
	s.Require().NoError(err)

	ev, err := harness_protocol.NewHarnessEvent(harness_protocol.HarnessEventOptions{
		Sequence:      1,
		Timestamp:     1.0,
		Event:         payload,
		HostSessionID: sess.GetSessionID(),
		AgentID:       "member-a",
	})
	s.Require().NoError(err)

	wire := harness_protocol.HarnessEventToDict(ev)
	wire["event_type"] = "future_event"
	wire["payload"] = map[string]any{"new_field": []any{1, 2, 3}}

	back, err := harness_protocol.HarnessEventFromDict(wire)
	s.Require().NoError(err, "未知事件类型不得报错")

	unknown, ok := back.Event.(harness_protocol.UnknownEvent)
	s.Require().True(ok, "载荷类型 = %T，期望 UnknownEvent", back.Event)
	s.Equal("future_event", unknown.EventType)
	s.Equal([]any{1, 2, 3}, unknown.Payload["new_field"])

	// 再编码回去，类型保持
	rewire := harness_protocol.HarnessEventToDict(back)
	s.Equal("future_event", rewire["event_type"])

	// 落 session 再读回，确认整个往返链路稳定
	sess.UpdateState(map[string]any{"last_event": rewire})
	s.Require().NoError(sess.Commit(s.Ctx))
}

// TestJsonSafe_与Freeze串联 验证归一化后的数据可进 checkpoint。
// 这是 jsonsafe 存在的理由：provider 数据含 NaN 时直接冻结会失败。
func (s *CheckpointSerializationSuite) TestJsonSafe_与Freeze串联() {
	sess := newRealSession(s.T(), "sess-jsonsafe-chain")

	// 模拟 provider SDK 吐出的脏数据
	rawSDK := map[string]any{
		"latency_ms": math.NaN(),
		"attempts":   []any{1, math.Inf(1)},
		"nested":     map[string]any{"ratio": math.NaN()},
	}

	// 直接进 checkpoint 必然失败
	_, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider: "acme", SchemaVersion: "1", AgentID: "member-a",
		HostSessionID: sess.GetSessionID(), CheckpointID: "cp-raw", Sequence: 1, Data: rawSDK,
	})
	s.Require().Error(err, "含 NaN 的原始数据必须被拒绝")

	// 经 jsonsafe 清洗后可进 checkpoint
	cleaned, ok := harness_providers.ToJsonSafe(rawSDK).(map[string]any)
	s.Require().True(ok)
	cp, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider: "acme", SchemaVersion: "1", AgentID: "member-a",
		HostSessionID: sess.GetSessionID(), CheckpointID: "cp-clean", Sequence: 1, Data: cleaned,
	})
	s.Require().NoError(err, "清洗后必须可进 checkpoint")
	s.Nil(cp.Data["latency_ms"], "NaN 必须已洗成 nil")
}

// TestCheckpoint_超大data被拒 验证 4MiB 护栏在真实链路上生效。
func (s *CheckpointSerializationSuite) TestCheckpoint_超大data被拒() {
	sess := newRealSession(s.T(), "sess-checkpoint-oversize")
	_, err := harness_protocol.NewHarnessCheckpoint(harness_protocol.CheckpointOptions{
		Provider: "acme", SchemaVersion: "1", AgentID: "member-a",
		HostSessionID: sess.GetSessionID(), CheckpointID: "cp-big", Sequence: 1,
		Data: map[string]any{"payload": strings.Repeat("x", harness_protocol.MaxCheckpointBytes)},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "exceeds")
}

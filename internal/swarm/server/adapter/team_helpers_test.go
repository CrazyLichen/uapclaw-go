package adapter

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── stripDirective 测试 ────────────────────────────

func TestStripDirective(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		prefix   string
		wantQ    string
		wantBool bool
	}{
		{"正常剥离", "/hide_dm hello", "/hide_dm", "hello", true},
		{"前缀无空格不剥离", "/hide_dm_world", "/hide_dm", "/hide_dm_world", false},
		{"前缀后无内容", "/hide_dm", "/hide_dm", "", true},
		{"前缀不匹配", "/evolve hello", "/hide_dm", "/evolve hello", false},
		{"前导空格", "  /debug test", "/debug", "test", true},
		{"空 query", "", "/hide_dm", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQ, gotBool := stripDirective(tt.query, tt.prefix)
			if gotQ != tt.wantQ || gotBool != tt.wantBool {
				t.Errorf("stripDirective(%q, %q) = (%q, %v), want (%q, %v)",
					tt.query, tt.prefix, gotQ, gotBool, tt.wantQ, tt.wantBool)
			}
		})
	}
}

// ──────────────────────────── extractQueryDirectives 测试 ────────────────────────────

func TestExtractQueryDirectives(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantQ    string
		wantHide bool
		wantDbg  bool
	}{
		{"组合指令", "/hide_dm /debug hello", "hello", true, true},
		{"仅 hide_dm", "/hide_dm hello", "hello", true, false},
		{"仅 debug", "/debug hello", "hello", false, true},
		{"无指令", "hello world", "hello world", false, false},
		{"debug在前", "/debug /hide_dm hello", "/hide_dm hello", false, true},
		{"指令后无内容", "/hide_dm /debug", "", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQ, gotHide, gotDbg := extractQueryDirectives(tt.query)
			if gotQ != tt.wantQ || gotHide != tt.wantHide || gotDbg != tt.wantDbg {
				t.Errorf("extractQueryDirectives(%q) = (%q, %v, %v), want (%q, %v, %v)",
					tt.query, gotQ, gotHide, gotDbg, tt.wantQ, tt.wantHide, tt.wantDbg)
			}
		})
	}
}

// ──────────────────────────── resolveChannelID 测试 ────────────────────────────

func TestResolveChannelID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"空串", "", "default"},
		{"纯空格", "  ", "default"},
		{"default", "default", "default"},
		{"自定义", "web", "web"},
		{"前导空格", " web ", "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveChannelID(tt.in); got != tt.want {
				t.Errorf("resolveChannelID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ──────────────────────────── isLeaderOutput 测试 ────────────────────────────

func TestIsLeaderOutput(t *testing.T) {
	tests := []struct {
		name  string
		chunk map[string]any
		want  bool
	}{
		{"nil chunk", nil, true},
		{"runtime_ready message", map[string]any{"type": "message", "payload": map[string]any{"event_type": "team.runtime_ready"}}, true},
		{"completed message", map[string]any{"type": "message", "payload": map[string]any{"event_type": "team.completed"}}, true},
		{"runtime_ready type", map[string]any{"type": "team.runtime_ready"}, true},
		{"LEADER role", map[string]any{"role": "leader"}, true},
		{"LEADER role uppercase", map[string]any{"role": "LEADER"}, true},
		{"nil role 默认 leader", map[string]any{"type": "message"}, true},
		{"TEAMMATE role", map[string]any{"role": "teammate"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLeaderOutput(tt.chunk); got != tt.want {
				t.Errorf("isLeaderOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ──────────────────────────── isTeammateOutput 测试 ────────────────────────────

func TestIsTeammateOutput(t *testing.T) {
	tests := []struct {
		name  string
		chunk map[string]any
		want  bool
	}{
		{"nil chunk", nil, false},
		{"nil role", map[string]any{"type": "message"}, false},
		{"TEAMMATE role", map[string]any{"role": "teammate"}, true},
		{"TEAMMATE uppercase", map[string]any{"role": "TEAMMATE"}, true},
		{"LEADER role", map[string]any{"role": "leader"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTeammateOutput(tt.chunk); got != tt.want {
				t.Errorf("isTeammateOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ──────────────────────────── enrichTeammateEvent 测试 ────────────────────────────

func TestEnrichTeammateEvent(t *testing.T) {
	t.Run("有 source_member", func(t *testing.T) {
		parsed := map[string]any{"event_type": "chat.delta"}
		chunk := map[string]any{"source_member": "alice"}
		result := enrichTeammateEvent(parsed, chunk)
		if result["role"] != "teammate" {
			t.Errorf("role = %v, want teammate", result["role"])
		}
		if result["member_name"] != "alice" {
			t.Errorf("member_name = %v, want alice", result["member_name"])
		}
	})
	t.Run("无 source_member", func(t *testing.T) {
		parsed := map[string]any{"event_type": "chat.delta"}
		chunk := map[string]any{}
		result := enrichTeammateEvent(parsed, chunk)
		if result["role"] != "teammate" {
			t.Errorf("role = %v, want teammate", result["role"])
		}
		if _, ok := result["member_name"]; ok {
			t.Error("member_name should not be set")
		}
	})
	t.Run("nil parsed", func(t *testing.T) {
		chunk := map[string]any{"source_member": "bob"}
		result := enrichTeammateEvent(nil, chunk)
		if result["role"] != "teammate" {
			t.Errorf("role = %v, want teammate", result["role"])
		}
	})
}

// ──────────────────────────── isDuplicateAskUserQuestion 测试 ────────────────────────────

func TestIsDuplicateAskUserQuestion(t *testing.T) {
	t.Run("非 ask_user_question", func(t *testing.T) {
		emitted := map[string]bool{}
		parsed := map[string]any{"event_type": "chat.delta", "request_id": "r1"}
		if isDuplicateAskUserQuestion(parsed, emitted) {
			t.Error("should not be duplicate for non-ask_user_question")
		}
		if len(emitted) != 0 {
			t.Error("emitted should remain empty")
		}
	})
	t.Run("首次 ask_user_question", func(t *testing.T) {
		emitted := map[string]bool{}
		parsed := map[string]any{"event_type": "chat.ask_user_question", "request_id": "r1"}
		if isDuplicateAskUserQuestion(parsed, emitted) {
			t.Error("first ask_user_question should not be duplicate")
		}
		if !emitted["r1"] {
			t.Error("r1 should be added to emitted")
		}
	})
	t.Run("重复 ask_user_question", func(t *testing.T) {
		emitted := map[string]bool{"r1": true}
		parsed := map[string]any{"event_type": "chat.ask_user_question", "request_id": "r1"}
		if !isDuplicateAskUserQuestion(parsed, emitted) {
			t.Error("duplicate ask_user_question should be detected")
		}
	})
	t.Run("空 request_id", func(t *testing.T) {
		emitted := map[string]bool{}
		parsed := map[string]any{"event_type": "chat.ask_user_question", "request_id": ""}
		if isDuplicateAskUserQuestion(parsed, emitted) {
			t.Error("empty request_id should not be duplicate")
		}
	})
}

// ──────────────────────────── approvalChunkFromEvent 测试 ────────────────────────────

func TestApprovalChunkFromEvent(t *testing.T) {
	t.Run("有效审批事件", func(t *testing.T) {
		evt := map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": "r1",
			"questions":  []any{map[string]any{"id": "q1"}},
		}
		result := approvalChunkFromEvent(evt)
		if result == nil {
			t.Fatal("should return non-nil for valid approval event")
		}
	})
	t.Run("非审批事件", func(t *testing.T) {
		evt := map[string]any{"event_type": "chat.delta"}
		if approvalChunkFromEvent(evt) != nil {
			t.Error("should return nil for non-approval event")
		}
	})
	t.Run("缺 request_id", func(t *testing.T) {
		evt := map[string]any{"event_type": "chat.ask_user_question", "questions": []any{1}}
		if approvalChunkFromEvent(evt) != nil {
			t.Error("should return nil for missing request_id")
		}
	})
	t.Run("缺 questions", func(t *testing.T) {
		evt := map[string]any{"event_type": "chat.ask_user_question", "request_id": "r1"}
		if approvalChunkFromEvent(evt) != nil {
			t.Error("should return nil for missing questions")
		}
	})
}

// ──────────────────────────── approvalResultFromEventOrItems 测试 ────────────────────────────

func TestApprovalResultFromEventOrItems(t *testing.T) {
	t.Run("有 approval_chunk", func(t *testing.T) {
		event := map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": "r1",
			"questions":  []any{map[string]any{"id": "q1"}},
		}
		result := approvalResultFromEventOrItems("mySkill", event, nil, "no changes", "invalid")
		if result["result_type"] != "answer" {
			t.Error("result_type should be answer for approval chunk")
		}
		chunks, _ := result["approval_chunks"].([]map[string]any)
		if len(chunks) != 1 {
			t.Error("should have 1 approval chunk")
		}
	})
	t.Run("无 approval_chunk 有 items", func(t *testing.T) {
		result := approvalResultFromEventOrItems("mySkill", nil, []map[string]any{{"action": "item1"}}, "no changes", "invalid")
		if result["result_type"] != "error" {
			t.Errorf("result_type = %v, want error", result["result_type"])
		}
	})
	t.Run("都无", func(t *testing.T) {
		result := approvalResultFromEventOrItems("mySkill", nil, nil, "no changes", "invalid")
		if result["result_type"] != "answer" {
			t.Errorf("result_type = %v, want answer", result["result_type"])
		}
		if result["output"] != "no changes" {
			t.Errorf("output = %v, want 'no changes'", result["output"])
		}
	})
}

// ──────────────────────────── teamProcessingDoneChunk 测试 ────────────────────────────

func TestTeamProcessingDoneChunk(t *testing.T) {
	chunk := teamProcessingDoneChunk("r1", "ch1", "s1")
	if chunk.RequestID != "r1" {
		t.Errorf("RequestID = %v, want r1", chunk.RequestID)
	}
	payload := chunk.Payload
	if payload["event_type"] != "chat.processing_status" {
		t.Errorf("event_type = %v", payload["event_type"])
	}
	if payload["is_complete"] != true {
		t.Error("is_complete should be true")
	}
}

// ──────────────────────────── broadcastEvent 测试 ────────────────────────────

func TestBroadcastEvent(t *testing.T) {
	// 清理全局状态
	pendingWaitersMu.Lock()
	pendingWaiters = make(map[[2]string][]pendingWaiter)
	pendingWaitersMu.Unlock()

	ch1 := make(chan map[string]any, 8)
	ch2 := make(chan map[string]any, 8)

	registerWaiter("ch1", "s1", "r1", ch1)
	registerWaiter("ch1", "s1", "r2", ch2)

	evt := map[string]any{"event_type": "chat.delta", "content": "hello"}
	broadcastEvent("ch1", "s1", evt)

	// 验证两个 waiter 都收到了事件
	select {
	case got := <-ch1:
		if got["content"] != "hello" {
			t.Errorf("ch1 got %v", got)
		}
	default:
		t.Error("ch1 should receive event")
	}
	select {
	case got := <-ch2:
		if got["content"] != "hello" {
			t.Errorf("ch2 got %v", got)
		}
	default:
		t.Error("ch2 should receive event")
	}

	unregisterWaiter("ch1", "s1", "r1")
	unregisterWaiter("ch1", "s1", "r2")

	// 验证注销后无 waiter
	pendingWaitersMu.RLock()
	key := [2]string{"ch1", "s1"}
	if _, ok := pendingWaiters[key]; ok {
		t.Error("waiters should be cleaned up after unregister")
	}
	pendingWaitersMu.RUnlock()
}

// ──────────────────────────── registerWaiter/unregisterWaiter 并发测试 ────────────────────────────

func TestRegisterUnregisterWaiter_并发安全(t *testing.T) {
	pendingWaitersMu.Lock()
	pendingWaiters = make(map[[2]string][]pendingWaiter)
	pendingWaitersMu.Unlock()

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := make(chan map[string]any, 8)
			rid := fmt.Sprintf("r%d", i)
			registerWaiter("ch", "s1", rid, ch)
			unregisterWaiter("ch", "s1", rid)
		}(i)
	}
	wg.Wait()

	pendingWaitersMu.RLock()
	key := [2]string{"ch", "s1"}
	if _, ok := pendingWaiters[key]; ok {
		t.Error("all waiters should be unregistered")
	}
	pendingWaitersMu.RUnlock()
}

// ──────────────────────────── SyncTeamIdentityMetadata 测试 ────────────────────────────

func TestSyncTeamIdentityMetadata_非创建类型(t *testing.T) {
	// 非 CREATE / NEW_TEAM_IN_SESSION → 不应更新
	SyncTeamIdentityMetadata(context.Background(), "web", "test_non_create_session", "team", "my-team", "RESUME")
	// 无 panic 即通过（不会写入元数据因为 kind 不匹配）
}

func TestSyncTeamIdentityMetadata_创建类型(t *testing.T) {
	// CREATE kind → 应更新元数据
	sessionID := fmt.Sprintf("test_team_create_%d", time.Now().UnixNano())
	SyncTeamIdentityMetadata(context.Background(), "web", sessionID, "team", "my-team", "CREATE")
	metadata := sessionmd.GetSessionMetadata(sessionID)
	if metadata == nil {
		t.Fatal("metadata should be created for CREATE kind")
	}
	teamName, _ := metadata["team_name"].(string)
	if teamName != "my-team" {
		t.Errorf("team_name = %q, want 'my-team'", teamName)
	}
}

// ──────────────────────────── onTeamWatcherDone 测试 ────────────────────────────

func TestOnTeamWatcherDone(t *testing.T) {
	// 仅验证无 panic
	onTeamWatcherDone("channel-1", "session-1")
}

// ──────────────────────────── HandleTeamEvolveListCommand 测试 ────────────────────────────

func TestHandleTeamEvolveListCommand_非evolveList(t *testing.T) {
	result := HandleTeamEvolveListCommand(context.Background(), "web", "s1", "/evolve mySkill")
	if result != nil {
		t.Error("should return nil for non /evolve_list command")
	}
}

// ──────────────────────────── HandleTeamSlashCommand 测试 ────────────────────────────

func TestHandleTeamSlashCommand_未知命令(t *testing.T) {
	result := HandleTeamSlashCommand(context.Background(), "web", "s1", "/unknown")
	if result != nil {
		t.Error("should return nil for unknown slash command")
	}
}

func TestHandleTeamSlashCommand_evolve无参(t *testing.T) {
	// /evolve 无参数 → 在 rail 为 nil 时返回 error
	result := HandleTeamSlashCommand(context.Background(), "web", "s1", "/evolve")
	if result == nil {
		t.Fatal("should return non-nil for /evolve with no skill_name")
	}
	// 因为 session "s1" 没有 rail，所以会返回 rail not found 错误
	if result["result_type"] != "error" {
		t.Errorf("result_type = %v, want error (no rail)", result["result_type"])
	}
}

// ──────────────────────────── ResolveTeamRebuildFollowup 测试 ────────────────────────────

func TestResolveTeamRebuildFollowup_非rebuild(t *testing.T) {
	prompt, errMsg := ResolveTeamRebuildFollowup(context.Background(), "web", "s1", "/evolve mySkill")
	if prompt != "" || errMsg != "" {
		t.Error("should return empty for non /evolve_rebuild command")
	}
}

func TestResolveTeamRebuildFollowup_缺skillName(t *testing.T) {
	// /evolve_rebuild 无 skill_name → 在 rail 为 nil 时返回 rail 错误
	_, errMsg := ResolveTeamRebuildFollowup(context.Background(), "web", "s1", "/evolve_rebuild")
	if errMsg == "" {
		t.Error("should return error for /evolve_rebuild without skill_name and no rail")
	}
}

// ──────────────────────────── groupTeamEvolutionApprovals 测试 ────────────────────────────

func TestGroupTeamEvolutionApprovals(t *testing.T) {
	t.Run("有 approval 事件", func(t *testing.T) {
		events := []map[string]any{
			{"event_type": "chat.ask_user_question", "request_id": "r1", "questions": []any{1}},
			{"event_type": "chat.delta"},
			{"event_type": "chat.ask_user_question", "request_id": "r2", "questions": []any{2}},
		}
		grouped, _ := groupTeamEvolutionApprovals("s1", events)
		if len(grouped) != 2 {
			t.Errorf("grouped count = %d, want 2", len(grouped))
		}
		if len(grouped["r1"]) != 1 {
			t.Errorf("r1 count = %d, want 1", len(grouped["r1"]))
		}
	})
	t.Run("无 approval 事件", func(t *testing.T) {
		events := []map[string]any{
			{"event_type": "chat.delta"},
		}
		grouped, _ := groupTeamEvolutionApprovals("s1", events)
		if len(grouped) != 0 {
			t.Errorf("grouped count = %d, want 0", len(grouped))
		}
	})
	t.Run("缺 request_id", func(t *testing.T) {
		events := []map[string]any{
			{"event_type": "chat.ask_user_question", "questions": []any{1}},
		}
		grouped, _ := groupTeamEvolutionApprovals("s1", events)
		if len(grouped) != 0 {
			t.Errorf("grouped count = %d, want 0 (missing request_id)", len(grouped))
		}
	})
	t.Run("从 _evolution_meta 提取 request_id", func(t *testing.T) {
		events := []map[string]any{
			{"event_type": "chat.ask_user_question", "questions": []any{1}, "_evolution_meta": map[string]any{"request_id": "meta_r1"}},
		}
		grouped, _ := groupTeamEvolutionApprovals("s1", events)
		if len(grouped) != 1 {
			t.Errorf("grouped count = %d, want 1 (from _evolution_meta)", len(grouped))
		}
		if _, ok := grouped["meta_r1"]; !ok {
			t.Error("should find request_id from _evolution_meta")
		}
	})
}

// ──────────────────────────── Used 列条件拼接（M-32）测试 ────────────────────────────

// TestHandleTeamEvolveListCommand_Used列零展示时强制00 测试 M-32 条件拼接。
//
// times_presented==0 但 times_used>0（数据异常防御）→ Used 列强制 "0/0"
// 而非 "3/0"（对齐 Python: used_str = f"{used}/{presented}" if stats.times_presented else "0/0"）。
func TestHandleTeamEvolveListCommand_Used列零展示时强制00(t *testing.T) {
	channelID := "web-m32-test"
	sessionID := "sess-m32-zero-presented"

	tm := team.GetTeamManager(channelID)
	// 先建技能目录（AppendRecord 要求技能已存在于 store 搜索路径），
	// 再用同一目录构造 rail（EvolutionStore 的 baseDirs 构造时捕获）
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "m32skill"), 0o755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}
	rail := coreevolution.NewTeamSkillEvolutionRail([]string{tmpDir}, nil, "test-model", "zh")
	tm.RegisterTeamSkillRail(sessionID, rail)

	// 写入一条 times_presented=0 但 times_used=3 的记录（防御性数据异常场景）
	rec := checkpointing.EvolutionRecord{
		ID:     "ev_m32_1",
		Source: "test",
		Change: checkpointing.EvolutionPatch{Section: "core", Content: "m32 test content"},
		Score:  0.8,
		UsageStats: &checkpointing.UsageStats{
			TimesPresented: 0,
			TimesUsed:      3,
			TimesPositive:  1,
			TimesNegative:  0,
		},
	}
	if err := rail.EvolutionStore().AppendRecord(t.Context(), "m32skill", rec); err != nil {
		t.Fatalf("AppendRecord 失败: %v", err)
	}

	result := HandleTeamEvolveListCommand(t.Context(), channelID, sessionID, "/evolve_list m32skill")
	if result == nil {
		t.Fatal("应返回结果")
	}
	if result["result_type"] != "answer" {
		t.Fatalf("result_type = %v, want answer", result["result_type"])
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "| 0/0 |") {
		t.Errorf("Used 列应为 0/0（零展示强制），输出:\n%s", output)
	}
	if strings.Contains(output, "| 3/0 |") {
		t.Errorf("Used 列不应为 3/0（times_presented==0 时不应拼接 used/presented），输出:\n%s", output)
	}
}

// TestHandleTeamEvolveListCommand_Used列正常拼接 测试非零展示时正常 used/presented。
func TestHandleTeamEvolveListCommand_Used列正常拼接(t *testing.T) {
	channelID := "web-m32-test2"
	sessionID := "sess-m32-normal"

	tm := team.GetTeamManager(channelID)
	tmpDir2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir2, "m32skill2"), 0o755); err != nil {
		t.Fatalf("mkdir 失败: %v", err)
	}
	rail := coreevolution.NewTeamSkillEvolutionRail([]string{tmpDir2}, nil, "test-model", "zh")
	tm.RegisterTeamSkillRail(sessionID, rail)

	rec := checkpointing.EvolutionRecord{
		ID:     "ev_m32_2",
		Source: "test",
		Change: checkpointing.EvolutionPatch{Section: "core", Content: "normal stats"},
		Score:  0.9,
		UsageStats: &checkpointing.UsageStats{
			TimesPresented: 5,
			TimesUsed:      2,
			TimesPositive:  2,
			TimesNegative:  0,
		},
	}
	if err := rail.EvolutionStore().AppendRecord(t.Context(), "m32skill2", rec); err != nil {
		t.Fatalf("AppendRecord 失败: %v", err)
	}

	result := HandleTeamEvolveListCommand(t.Context(), channelID, sessionID, "/evolve_list m32skill2")
	if result == nil {
		t.Fatal("应返回结果")
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "| 2/5 |") {
		t.Errorf("Used 列应为 2/5（正常拼接），输出:\n%s", output)
	}
	if !strings.Contains(output, "| +2/-0 |") {
		t.Errorf("Effect 列应为 +2/-0，输出:\n%s", output)
	}
}

// ──────────────────────────── resolveEvolutionEventTimeoutSec（M-35）测试 ────────────────────────────

// TestResolveEvolutionEventTimeoutSec_rail有效值 测试 M-35 实装。
//
// rail 带 SDK 超时 720s → 返回 720+5（宽限 grace）= 725，
// 不再走原空壳的 fallback。
func TestResolveEvolutionEventTimeoutSec_rail有效值(t *testing.T) {
	rail := coreevolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "zh",
		coreevolution.WithTeamSkillEvolutionTimeout(720.0))

	got := resolveEvolutionEventTimeoutSec(rail, 900.0)
	want := 720.0 + evolutionlogic.TeamEvolutionEventTimeoutGraceSec
	if got != want {
		t.Errorf("resolveEvolutionEventTimeoutSec = %v, want %v (720+grace)", got, want)
	}
}

// TestResolveEvolutionEventTimeoutSec_rail非法值回退 测试 Inf/NaN/<=0 回退 fallback。
func TestResolveEvolutionEventTimeoutSec_rail非法值回退(t *testing.T) {
	tests := []struct {
		name    string
		timeout float64
	}{
		{"零", 0},
		{"负值", -10.0},
		{"NaN", math.NaN()},
		{"正Inf", math.Inf(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rail := coreevolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "zh",
				coreevolution.WithTeamSkillEvolutionTimeout(tt.timeout))
			got := resolveEvolutionEventTimeoutSec(rail, 900.0)
			if got != 900.0 {
				t.Errorf("非法值 %v 应回退 fallback 900，实际 %v", tt.timeout, got)
			}
		})
	}
}

// TestResolveEvolutionEventTimeoutSec_nilRail 测试 nil rail 回退。
func TestResolveEvolutionEventTimeoutSec_nilRail(t *testing.T) {
	if got := resolveEvolutionEventTimeoutSec(nil, 900.0); got != 900.0 {
		t.Errorf("nil rail 应回退 fallback 900，实际 %v", got)
	}
}

// TestResolveEvolutionEventTimeoutSec_默认rail值 测试默认 rail（720s）+ grace。
func TestResolveEvolutionEventTimeoutSec_默认rail值(t *testing.T) {
	rail := coreevolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "zh")
	// 默认 evolutionTotalTimeoutSec = 720 → 725
	got := resolveEvolutionEventTimeoutSec(rail, 900.0)
	if got != 725.0 {
		t.Errorf("默认 rail 应返回 720+5=725，实际 %v", got)
	}
}

// ──────────────────────────── splitMaxN（M-37）测试 ────────────────────────────

// TestSplitMaxN_表驱动 验证 Python str.split(maxsplit=n) 语义。
func TestSplitMaxN_表驱动(t *testing.T) {
	tests := []struct {
		name string
		input string
		n     int
		want  []string
	}{
		{"连续空格消耗_1段", "/evolve  skill  with   spaces", 1, []string{"/evolve", "skill  with   spaces"}},
		{"连续空格消耗_2段", "/evolve  skill  with   spaces", 2, []string{"/evolve", "skill", "with   spaces"}},
		{"tab混合分割", "/evolve\tskill\tquery", 2, []string{"/evolve", "skill", "query"}},
		{"首部空白", "  /evolve skill", 1, []string{"/evolve", "skill"}},
		{"尾部空白保留在末段", "/evolve skill  ", 1, []string{"/evolve", "skill  "}},
		{"空串", "", 2, nil},
		{"纯空白", "   ", 2, nil},
		{"只有命令无参数", "/evolve", 2, []string{"/evolve"}},
		{"只有命令加空白", "/evolve ", 2, []string{"/evolve"}},
		{"恰好n段无剩余", "a b c", 2, []string{"a", "b", "c"}},
		{"少于n段", "a b", 2, []string{"a", "b"}},
		{"中文内容", "/evolve 技能 查询内容  含空格", 2, []string{"/evolve", "技能", "查询内容  含空格"}},
		{"n=1两级模式", "skill_name user query here", 1, []string{"skill_name", "user query here"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitMaxN(tt.input, tt.n)
			if len(got) != len(tt.want) {
				t.Fatalf("splitMaxN(%q, %d) = %v (len %d), want %v (len %d)",
					tt.input, tt.n, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitMaxN(%q, %d)[%d] = %q, want %q", tt.input, tt.n, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestSplitMaxN_对照Python 验证关键语义差异点。
//
// Python: "a  b".split(maxsplit=1) → ['a', 'b']（消耗连续空白）
// strings.SplitN("a  b", " ", 1) → ["a  b"]（只切第一处）→ 错误
// strings.SplitN("a  b", " ", 2) → ["a", " b"]（保留空元素/前导空格）→ 错误
func TestSplitMaxN_对照Python(t *testing.T) {
	// "a  b"（两个空格）maxsplit=1 → Python ['a','b']
	got := splitMaxN("a  b", 1)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitMaxN(\"a  b\", 1) = %v, want [a b]", got)
	}

	// "a b  c"（b 后两空格）maxsplit=2 → Python ['a','b','c']——末段消耗前导空白
	got = splitMaxN("a b  c", 2)
	if len(got) != 3 || got[2] != "c" {
		t.Errorf("splitMaxN(\"a b  c\", 2) = %v, want [a b c]", got)
	}

	// 内部空白仅在末段保留："a b c  d" maxsplit=2 → ['a','b','c  d']
	got = splitMaxN("a b c  d", 2)
	if len(got) != 3 || got[2] != "c  d" {
		t.Errorf("splitMaxN(\"a b c  d\", 2) = %v, want [a b \"c  d\"]", got)
	}
}

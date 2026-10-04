package adapter

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

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
	onTeamWatcherDone("session-1")
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

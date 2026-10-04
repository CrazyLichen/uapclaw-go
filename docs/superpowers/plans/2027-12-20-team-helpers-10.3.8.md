# 10.3.8 TeamHelpers 辅助函数实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Python `team_helpers.py` 中不依赖 TeamRunner（9.85）和 Swarm Team（10.6.19-23）的辅助函数，对齐 Python 一比一逻辑。

**Architecture:** 新建 `team_helpers.go`（包级函数，对齐 Python 模块级函数风格）+ 增强 `deep_adapter_team.go`（`*DeepAdapter` 方法，补充 directives/slash 分支 + ⤵️ 桩方法）。`evolution/helpers.go` 已有 `GroupEvolutionApprovals` 等函数可复用。

**Tech Stack:** Go 1.22+, `sync.RWMutex`（替代 Python asyncio 的并发保护）, `sessionmd` 包（元数据读写）, `team` 包（TeamManager）

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| Create | `internal/swarm/server/adapter/team_helpers.go` | 包级辅助函数：常量+全局状态+纯函数+事件判断+审批+广播+元数据+team slash+回调 |
| Create | `internal/swarm/server/adapter/team_helpers_test.go` | team_helpers.go 的测试 |
| Modify | `internal/swarm/server/adapter/deep_adapter_team.go` | 增强 processTeamMessageStream + 新增 ⤵️ 桩方法 |
| Modify | `internal/swarm/server/adapter/doc.go` | 文件目录添加 team_helpers.go 条目 |

---

### Task 1: 常量 + 全局状态 + pendingWaiter 结构体

**Files:**
- Create: `internal/swarm/server/adapter/team_helpers.go`

- [ ] **Step 1: 创建 team_helpers.go，写入常量、结构体、全局变量**

```go
package adapter

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pendingWaiter 单个等待条目。
// 对齐 Python: _pending_waiters 中的 (request_id, asyncio.Queue) 对
type pendingWaiter struct {
	// RequestID 请求标识
	RequestID string
	// Ch 事件接收通道
	Ch chan map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// hideDMPrefix 隐藏 DM 指令前缀。
	// Python: _HIDE_DM_PREFIX = "/hide_dm"
	hideDMPrefix = "/hide_dm"
	// debugPrefix 调试指令前缀。
	// Python: _DEBUG_PREFIX = "/debug"
	debugPrefix = "/debug"
	// streamTraceEnvKey 流追踪环境变量。
	// Python: _STREAM_TRACE_ENV_KEY = "JIUWENSWARM_TEAM_STREAM_TRACE"
	streamTraceEnvKey = "JIUWENSWARM_TEAM_STREAM_TRACE"
)

// teamCreateKinds 团队创建类型集合。
// 对齐 Python: _TEAM_CREATE_KINDS = {RunActionKind.CREATE.value, RunActionKind.NEW_TEAM_IN_SESSION.value}
// Go 端无 RunActionKind 枚举，直接用字符串常量集合。
var teamCreateKinds = map[string]bool{
	"CREATE":              true,
	"NEW_TEAM_IN_SESSION": true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pendingWaitersMu 保护 pendingWaiters 的读写锁。
	// Python 用 asyncio（单线程事件循环）无需锁，Go 需要 sync.RWMutex 保护并发安全。
	pendingWaitersMu sync.RWMutex
	// pendingWaiters 等待事件广播的请求队列。
	// key: [2]string{channelID, sessionID}，value: []pendingWaiter
	// 对齐 Python: _pending_waiters: dict[tuple[str, str], list[tuple[str, asyncio.Queue]]]
	pendingWaiters = make(map[[2]string][]pendingWaiter)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/ 2>&1 | head -20`
Expected: 编译成功（可能有 unused import 警告，后续步骤会使用）

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/team_helpers.go
git commit -m "feat(10.3.8): team_helpers.go 常量+全局状态+pendingWaiter 结构体"
```

---

### Task 2: 纯函数 — stripDirective / extractQueryDirectives / resolveChannelID

**Files:**
- Modify: `internal/swarm/server/adapter/team_helpers.go`
- Create: `internal/swarm/server/adapter/team_helpers_test.go`

- [ ] **Step 1: 在 team_helpers.go 非导出函数区追加 3 个纯函数**

```go
// stripDirective 从 query 开头去除 slash 指令前缀。
// 仅当 query 以 prefix 开头且后跟空格或结尾时才去除。
// 对齐 Python: _strip_directive(query, prefix) (line 71-82)
func stripDirective(query, prefix string) (string, bool) {
	stripped := strings.TrimLeft(query, " \t")
	if !strings.HasPrefix(stripped, prefix) {
		return query, false
	}
	remainder := stripped[len(prefix):]
	if len(remainder) > 0 && remainder[0] != ' ' {
		return query, false
	}
	return strings.TrimLeft(remainder, " \t"), true
}

// extractQueryDirectives 从首次 team 请求中提取并去除所有 slash 指令。
// 返回 (cleanedQuery, hideDM, debug)。
// 对齐 Python: _extract_query_directives(query) (line 85-92)
func extractQueryDirectives(query string) (string, bool, bool) {
	query, hideDM := stripDirective(query, hideDMPrefix)
	query, debug := stripDirective(query, debugPrefix)
	return query, hideDM, debug
}

// resolveChannelID 规范化 channelID，空串回退为 "default"。
// 对齐 Python: _resolve_channel_id(channel_id) (line 129-130)
func resolveChannelID(channelID string) string {
	id := strings.TrimSpace(channelID)
	if id == "" {
		return "default"
	}
	return id
}
```

- [ ] **Step 2: 创建 team_helpers_test.go，写入纯函数测试**

```go
package adapter

import "testing"

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
		{"反序", "/debug /hide_dm hello", "hello", true, true},
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
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestStripDirective|TestExtractQueryDirectives|TestResolveChannelID" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/team_helpers.go internal/swarm/server/adapter/team_helpers_test.go
git commit -m "feat(10.3.8): stripDirective/extractQueryDirectives/resolveChannelID + 测试"
```

---

### Task 3: 事件判断函数 — isLeaderOutput / isTeammateOutput / enrichTeammateEvent / isDuplicateAskUserQuestion

**Files:**
- Modify: `internal/swarm/server/adapter/team_helpers.go`
- Modify: `internal/swarm/server/adapter/team_helpers_test.go`

先确认 TeamRole 常量：

```go
// Go 中 TeamRole 的值（对齐 Python TeamRole.LEADER.value / TeamRole.TEAMMATE.value）
// 在 agent_teams/schema/team/ 包中已定义
```

- [ ] **Step 1: 在 team_helpers.go 非导出函数区追加 4 个事件判断函数**

需要新增 import: `"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/team"` (注意：这个包名与已有的 `team` 包冲突，需要用别名)

```go
// isLeaderOutput 判断 team OutputSchema chunk 是否应展示给 claw 用户。
// 对齐 Python: _is_leader_output(chunk) (line 237-257)
//
// 规则：
//   - chunk["type"]=="message" 且 payload 中 event_type 为 "team.runtime_ready"/"team.completed" → true
//   - chunk["type"]=="team.runtime_ready" → true
//   - chunk["role"] 为 TeamRole.LEADER → true
//   - role 为 nil → true（默认视为 leader）
//   - 其他 → false
func isLeaderOutput(chunk map[string]any) bool {
	if chunk == nil {
		return true
	}
	chunkType, _ := chunk["type"].(string)
	payload, _ := chunk["payload"].(map[string]any)

	// team.runtime_ready 和 team.completed 是 leader 级控制事件
	if chunkType == "message" && payload != nil {
		if evtType, _ := payload["event_type"].(string); evtType == "team.runtime_ready" || evtType == "team.completed" {
			return true
		}
	}
	if chunkType == "team.runtime_ready" {
		return true
	}

	role := chunk["role"]
	if role == nil {
		return true
	}
	// 检查 role == TeamRole.LEADER
	if roleStr, ok := role.(string); ok {
		return strings.TrimSpace(strings.ToLower(roleStr)) == "leader"
	}
	return false
}

// isTeammateOutput 判断 team OutputSchema chunk 是否来自 teammate。
// 对齐 Python: _is_teammate_output(chunk) (line 260-268)
func isTeammateOutput(chunk map[string]any) bool {
	if chunk == nil {
		return false
	}
	role := chunk["role"]
	if role == nil {
		return false
	}
	if roleStr, ok := role.(string); ok {
		return strings.TrimSpace(strings.ToLower(roleStr)) == "teammate"
	}
	return false
}

// enrichTeammateEvent 丰富 teammate 事件，添加 role 和 member_name 字段。
// 对齐 Python: _enrich_teammate_event(parsed, chunk) (line 271-278)
func enrichTeammateEvent(parsed map[string]any, chunk map[string]any) map[string]any {
	if parsed == nil {
		parsed = map[string]any{}
	}
	parsed["role"] = "teammate"
	// Python: source_member = getattr(chunk, "source_member", None)
	if sourceMember, _ := chunk["source_member"].(string); sourceMember != "" {
		parsed["member_name"] = sourceMember
	}
	return parsed
}

// isDuplicateAskUserQuestion 去重 chat.ask_user_question 事件。
// 对齐 Python: _is_duplicate_ask_user_question(parsed, emitted_request_ids) (line 281-293)
//
// 若 event_type != "chat.ask_user_question" 或 request_id 为空 → false（不去重）。
// 若 request_id 已在集合中 → true（重复）。
// 否则加入集合返回 false。
func isDuplicateAskUserQuestion(parsed map[string]any, emittedRequestIDs map[string]bool) bool {
	if parsed == nil {
		return false
	}
	evtType, _ := parsed["event_type"].(string)
	if evtType != "chat.ask_user_question" {
		return false
	}
	requestID := strings.TrimSpace(strFromAny(parsed["request_id"]))
	if requestID == "" {
		return false
	}
	if emittedRequestIDs[requestID] {
		return true
	}
	emittedRequestIDs[requestID] = true
	return false
}

// strFromAny 从 any 值中提取字符串。
func strFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
```

注意：`strFromAny` 在 `evolution/helpers.go` 中也有同签名的非导出函数，但在不同包中，不会冲突。如果更倾向复用，可以改为从 `evolution` 包调用——但 `evolution.StrFromAny` 是非导出的，所以在 `adapter` 包中直接定义即可。

- [ ] **Step 2: 在 team_helpers_test.go 追加 4 组测试**

```go
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
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestIsLeaderOutput|TestIsTeammateOutput|TestEnrichTeammateEvent|TestIsDuplicateAskUserQuestion" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/team_helpers.go internal/swarm/server/adapter/team_helpers_test.go
git commit -m "feat(10.3.8): isLeaderOutput/isTeammateOutput/enrichTeammateEvent/isDuplicateAskUserQuestion + 测试"
```

---

### Task 4: 审批辅助 + 完成状态 + 广播 + waiter 注册/注销

**Files:**
- Modify: `internal/swarm/server/adapter/team_helpers.go`
- Modify: `internal/swarm/server/adapter/team_helpers_test.go`

- [ ] **Step 1: 在 team_helpers.go 追加审批辅助、完成状态、广播、waiter 函数**

```go
// approvalChunkFromEvent 从事件中提取审批 chunk。
// 对齐 Python: _approval_chunk_from_event(evt) (line 199-209)
//
// 检查 event_type == "chat.ask_user_question" 且 request_id 和 questions 有效。
// Go 端直接对 map[string]any 操作（Python 调 parse_stream_chunk，但 Go 端的事件已是解析后的 dict）。
func approvalChunkFromEvent(evt map[string]any) map[string]any {
	if evt == nil {
		return nil
	}
	evtType, _ := evt["event_type"].(string)
	if evtType != "chat.ask_user_question" {
		return nil
	}
	requestID, _ := evt["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		return nil
	}
	questions, _ := evt["questions"].([]any)
	if len(questions) == 0 {
		return nil
	}
	return evt
}

// approvalResultFromEventOrItems 构建审批结果 dict。
// 对齐 Python: _approval_result_from_event_or_items(...) (line 212-234)
//
// 优先从 event 提取 approval_chunk；次选 items 非空时返回 invalid_output；最后返回 no_changes_output。
func approvalResultFromEventOrItems(skillName string, event any, items []any, noChangesOutput, invalidOutput string) map[string]any {
	approvalChunk := approvalChunkFromEvent(eventAsMap(event))
	if approvalChunk != nil {
		questions, _ := approvalChunk["questions"].([]any)
		return map[string]any{
			"output":          fmt.Sprintf("Skill '%s' 演进请求已生成，请在审批弹框中确认。", skillName),
			"result_type":     "answer",
			"approval_chunks": []map[string]any{approvalChunk},
			"question_count":  len(questions),
		}
	}
	if len(items) > 0 {
		return map[string]any{
			"output":      invalidOutput,
			"result_type": "error",
		}
	}
	return map[string]any{
		"output":      noChangesOutput,
		"result_type": "answer",
	}
}

// eventAsMap 将 any 转为 map[string]any（对齐 Python 中 event 可以是 dict 或对象）。
func eventAsMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// teamProcessingDoneChunk 构建 chat.processing_status(is_complete=True) chunk。
// 对齐 Python: _team_processing_done_chunk(request_id, channel_id, session_id) (line 296-311)
func teamProcessingDoneChunk(requestID, channelID, sessionID string) *agentschema.AgentResponseChunk {
	return &agentschema.AgentResponseChunk{
		RequestID: requestID,
		ChannelID: channelID,
		Payload: map[string]any{
			"event_type":   "chat.processing_status",
			"session_id":   sessionID,
			"is_processing": false,
			"is_complete":  true,
		},
		IsComplete: false,
	}
}

// broadcastEvent 向所有等待同一 (channelID, sessionID) 的请求队列广播事件。
// 对齐 Python: _broadcast_event(channel_id, session_id, event) (line 181-196)
func broadcastEvent(channelID, sessionID string, event map[string]any) {
	key := [2]string{resolveChannelID(channelID), sessionID}
	pendingWaitersMu.RLock()
	waiters := pendingWaiters[key]
	pendingWaitersMu.RUnlock()

	for _, w := range waiters {
		// 浅拷贝，避免多个 waiter 共享同一 map 被修改
		copied := make(map[string]any, len(event))
		for k, v := range event {
			copied[k] = v
		}
		select {
		case w.Ch <- copied:
		default:
			logger.Debug(logComponent).
				Str("channel_id", key[0]).
				Str("session_id", sessionID).
				Str("request_id", w.RequestID).
				Msg("broadcastEvent: channel full, dropping event")
		}
	}
}

// registerWaiter 注册一个请求等待者。
func registerWaiter(channelID, sessionID, requestID string, ch chan map[string]any) {
	key := [2]string{resolveChannelID(channelID), sessionID}
	pendingWaitersMu.Lock()
	defer pendingWaitersMu.Unlock()
	pendingWaiters[key] = append(pendingWaiters[key], pendingWaiter{
		RequestID: requestID,
		Ch:        ch,
	})
}

// unregisterWaiter 注销一个请求等待者。
func unregisterWaiter(channelID, sessionID, requestID string) {
	key := [2]string{resolveChannelID(channelID), sessionID}
	pendingWaitersMu.Lock()
	defer pendingWaitersMu.Unlock()
	waiters := pendingWaiters[key]
	for i, w := range waiters {
		if w.RequestID == requestID {
			pendingWaiters[key] = append(waiters[:i], waiters[i+1:]...)
			break
		}
	}
	if len(pendingWaiters[key]) == 0 {
		delete(pendingWaiters, key)
	}
}

// groupTeamEvolutionApprovals 审批分组（team 版包装）。
// 对齐 Python: _group_team_evolution_approvals(session_id, events) (line 314-328)
//
// 包装 evolution.GroupEvolutionApprovals，提供 warnMissingRequestID 回调。
func groupTeamEvolutionApprovals(sessionID string, events []map[string]any) (map[string][]map[string]any, []string) {
	warnFn := func(warnSessionID string) {
		logger.Warn(logComponent).
			Str("session_id", warnSessionID).
			Msg("team evolution approval missing request_id")
	}
	return adapterEvolution.GroupEvolutionApprovals(sessionID, events, warnFn), nil
}
```

注意：`groupTeamEvolutionApprovals` 调用 `adapterEvolution.GroupEvolutionApprovals`，需要确认 import 别名。`evolution` 子包的 import 需要别名以避免与 `agentcore/harness/rails/evolution` 冲突：

在文件头部 import 区添加：
```go
adapterEvolution "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution"
```

- [ ] **Step 2: 在 team_helpers_test.go 追加测试**

```go
// ──────────────────────────── approvalChunkFromEvent 测试 ────────────────────────────

func TestApprovalChunkFromEvent(t *testing.T) {
	t.Run("有效审批事件", func(t *testing.T) {
		evt := map[string]any{
			"event_type":  "chat.ask_user_question",
			"request_id":  "r1",
			"questions":   []any{map[string]any{"id": "q1"}},
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
		result := approvalResultFromEventOrItems("mySkill", nil, []any{"item1"}, "no changes", "invalid")
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
	payload, _ := chunk.Payload.(map[string]any)
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
```

需要在 test 文件中增加 import `"sync"` 和 `"fmt"`。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestApprovalChunk|TestApprovalResult|TestTeamProcessingDoneChunk|TestBroadcastEvent|TestRegisterUnregisterWaiter" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/team_helpers.go internal/swarm/server/adapter/team_helpers_test.go
git commit -m "feat(10.3.8): approvalChunkFromEvent/approvalResult/teamProcessingDoneChunk/broadcastEvent/registerWaiter + 测试"
```

---

### Task 5: SyncTeamIdentityMetadata + onTeamWatcherDone

**Files:**
- Modify: `internal/swarm/server/adapter/team_helpers.go`
- Modify: `internal/swarm/server/adapter/team_helpers_test.go`

- [ ] **Step 1: 在 team_helpers.go 导出函数区追加 SyncTeamIdentityMetadata，非导出区追加 onTeamWatcherDone**

```go
// SyncTeamIdentityMetadata 持久化 team 身份元数据（仅新创建的 team 会话）。
// 对齐 Python: sync_team_identity_metadata(*) (line 95-127)
//
// 仅在 activationKind 属于 teamCreateKinds 时执行。
// 已有 team_name 不匹配时保留现有元数据并 warn。
func SyncTeamIdentityMetadata(ctx context.Context, channelID, sessionID, mode, readyTeamName, activationKind string) {
	normalizedKind := strings.TrimSpace(activationKind)
	if !teamCreateKinds[normalizedKind] {
		return
	}

	metadata := sessionmd.GetSessionMetadata(sessionID)
	existingTeamName := ""
	if metadata != nil {
		if tn, ok := metadata["team_name"].(string); ok {
			existingTeamName = strings.TrimSpace(tn)
		}
	}

	if existingTeamName != "" && existingTeamName != readyTeamName {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("existing_team_name", existingTeamName).
			Str("new_team_name", readyTeamName).
			Str("activation_kind", normalizedKind).
			Msg("SyncTeamIdentityMetadata: team session identity mismatch, keep existing metadata")
		return
	}

	resolvedChannel := resolveChannelID(channelID)
	sessionmd.UpdateSessionMetadata(sessionmd.SessionMetadataUpdate{
		SessionID: sessionID,
		ChannelID: &resolvedChannel,
		Mode:      &mode,
		TeamName:  &readyTeamName,
	})
}
```

```go
// onTeamWatcherDone evolution 观察任务完成回调。
// 对齐 Python: _on_team_watcher_done(task) (line 1086-1098)
func onTeamWatcherDone(sessionID string) {
	logger.Info(logComponent).Str("session_id", sessionID).Msg("onTeamWatcherDone: team evolution watcher done")
}
```

- [ ] **Step 2: 在 team_helpers_test.go 追加测试**

```go
// ──────────────────────────── SyncTeamIdentityMetadata 测试 ────────────────────────────

func TestSyncTeamIdentityMetadata_非创建类型(t *testing.T) {
	// 非 CREATE / NEW_TEAM_IN_SESSION → 不应更新
	// 用一个不存在的 session 验证不会写入
	SyncTeamIdentityMetadata(context.Background(), "web", "test_non_create_session", "team", "my-team", "RESUME")
	// 无 panic 即通过（不会写入元数据因为 kind 不匹配）
}

func TestSyncTeamIdentityMetadata_创建类型(t *testing.T) {
	// CREATE kind → 应更新元数据
	// 注意：此测试会真实写入 session 元数据，使用临时 session ID 避免冲突
	sessionID := fmt.Sprintf("test_team_create_%d", time.Now().UnixNano())
	SyncTeamIdentityMetadata(context.Background(), "web", sessionID, "team", "my-team", "CREATE")
	// 验证元数据已写入
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
```

需要在 test 文件中增加 import `"time"`。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestSyncTeamIdentityMetadata|TestOnTeamWatcherDone" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/team_helpers.go internal/swarm/server/adapter/team_helpers_test.go
git commit -m "feat(10.3.8): SyncTeamIdentityMetadata + onTeamWatcherDone + 测试"
```

---

### Task 6: Team Slash 命令 — handleTeamEvolveListCommand / handleTeamSlashCommand / resolveTeamRebuildFollowup

**Files:**
- Modify: `internal/swarm/server/adapter/team_helpers.go`
- Modify: `internal/swarm/server/adapter/team_helpers_test.go`

- [ ] **Step 1: 在 team_helpers.go 导出函数区追加 3 个 team slash 函数**

```go
// HandleTeamEvolveListCommand 处理 /evolve_list <skill_name> 命令（team 版）。
// 对齐 Python: _handle_team_evolve_list_command(channel_id, session_id, query) (line 421-491)
//
// 通过 TeamManager.GetTeamSkillRail 获取 rail，查询 store 展示经验摘要。
// 非 /evolve_list 前缀返回 nil。
func HandleTeamEvolveListCommand(ctx context.Context, channelID, sessionID, query string) map[string]any {
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/evolve_list") {
		return nil
	}

	tm := team.GetTeamManager(channelID)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		return map[string]any{
			"output":      "团队技能演进记录不可用：未找到 TeamSkillEvolutionRail。",
			"result_type": "error",
		}
	}

	store := rail.EvolutionStore()
	parts := strings.Fields(stripped)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	if skillName == "" || strings.HasPrefix(skillName, "--") {
		return map[string]any{
			"output":      "请指定 Skill 名称：`/evolve_list <skill_name>`",
			"result_type": "error",
		}
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return map[string]any{
			"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
			"result_type": "error",
		}
	}

	records := store.GetRecordsByScore(ctx, skillName, nil)
	if len(records) == 0 {
		return map[string]any{
			"output":      fmt.Sprintf("Skill '%s' 暂无演进经验。", skillName),
			"result_type": "answer",
		}
	}

	// 构建摘要表格
	var avgScore float64
	for _, r := range records {
		avgScore += r.Score
	}
	avgScore /= float64(len(records))

	lines := []string{
		fmt.Sprintf("📊 Skill \"%s\" — 经验库摘要\n", skillName),
		fmt.Sprintf("共 %d 条经验 | 平均分：%.2f\n", len(records), avgScore),
		"| # | Score | Section | Content (preview) |",
		"|---|---:|---|---|",
	}
	for i, record := range records {
		section := strings.ReplaceAll(record.Change.Section, "|", "\\|")
		preview := record.Change.Content
		if idx := strings.Index(preview, "\n"); idx >= 0 {
			preview = preview[:idx]
		}
		if len(preview) > 40 {
			preview = preview[:40]
		}
		preview = strings.ReplaceAll(preview, "|", "\\|")
		lines = append(lines, fmt.Sprintf("| %d | %.2f | %s | %s |", i+1, record.Score, section, preview))
	}
	lines = append(lines, fmt.Sprintf("\n提示：使用 /evolve_simplify %s 执行智能整理", skillName))

	return map[string]any{
		"output":      strings.Join(lines, "\n"),
		"result_type": "answer",
	}
}

// HandleTeamSlashCommand 处理 team 模式专用 slash 命令。
// 对齐 Python: _handle_team_slash_command(channel_id, session_id, query) (line 494-608)
//
// 依次检查：/evolve_list → /evolve_simplify → /evolve。
// 非 team slash 命令返回 nil。
func HandleTeamSlashCommand(ctx context.Context, channelID, sessionID, query string) map[string]any {
	// 1. /evolve_list
	listResult := HandleTeamEvolveListCommand(ctx, channelID, sessionID, query)
	if listResult != nil {
		return listResult
	}

	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/evolve_simplify") && stripped != "/evolve" && !strings.HasPrefix(stripped, "/evolve ") {
		return nil
	}

	tm := team.GetTeamManager(channelID)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		return map[string]any{
			"output":      "团队技能演进不可用：未找到 TeamSkillEvolutionRail。",
			"result_type": "error",
		}
	}

	store := rail.EvolutionStore()

	// 2. /evolve_simplify
	if strings.HasPrefix(stripped, "/evolve_simplify") {
		parts := strings.Fields(stripped)
		skillName := ""
		if len(parts) > 1 {
			skillName = parts[1]
		}
		var userIntent *string
		if len(parts) > 2 {
			intent := strings.Join(parts[2:], " ")
			userIntent = &intent
		}

		if skillName == "" {
			return map[string]any{
				"output":      "请指定 Skill 名称：`/evolve_simplify <skill_name> [user_intent]`",
				"result_type": "error",
			}
		}

		if !store.SkillExists(ctx, skillName) {
			available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
			if available == "" {
				available = "（无可用 Skill）"
			}
			return map[string]any{
				"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
				"result_type": "error",
			}
		}

		simplifyResult, err := rail.RequestSimplify(ctx, skillName, userIntent)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Msg("HandleTeamSlashCommand: evolve_simplify failed")
			return map[string]any{
				"output":      fmt.Sprintf("团队技能整理分析失败：%s", err),
				"result_type": "error",
			}
		}

		return approvalResultFromEventOrItems(
			skillName,
			simplifyResult.ApprovalEvent,
			func() []any {
				if simplifyResult.Actions != nil {
					return simplifyResult.Actions
				}
				return nil
			}(),
			fmt.Sprintf("Skill '%s' 经验库状态良好，无需整理。", skillName),
			fmt.Sprintf("Skill '%s' 精简方案已生成，但审批事件为空或格式无效。", skillName),
		)
	}

	// 3. /evolve
	parts := strings.Fields(stripped)
	if len(parts) < 2 {
		return map[string]any{
			"output":      "请补充演进意图：`/evolve <skill_name> <user_query>`",
			"result_type": "error",
		}
	}
	skillName := strings.TrimSpace(parts[1])
	userQuery := ""
	if len(parts) > 2 {
		userQuery = strings.TrimSpace(strings.Join(parts[2:], " "))
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return map[string]any{
			"output":      fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available),
			"result_type": "error",
		}
	}

	if userQuery == "" {
		return map[string]any{
			"output":      "请补充演进意图：`/evolve <skill_name> <user_query>`",
			"result_type": "error",
		}
	}

	evolveResult, err := rail.RequestUserEvolution(ctx, skillName, userQuery, false)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("HandleTeamSlashCommand: evolve failed")
		return map[string]any{
			"output":      fmt.Sprintf("团队技能演进请求失败：%s", err),
			"result_type": "error",
		}
	}

	return approvalResultFromEventOrItems(
		skillName,
		evolveResult.ApprovalEvent,
		nil, // Python: items=list(getattr(evolve_result, "records", []) or [])
		fmt.Sprintf("Skill '%s' 未生成新的团队技能演进经验。", skillName),
		fmt.Sprintf("Skill '%s' 已生成团队技能演进经验，但审批事件为空或格式无效。", skillName),
	)
}

// ResolveTeamRebuildFollowup 解析 /evolve_rebuild 命令，返回 followup_prompt。
// 对齐 Python: _resolve_team_rebuild_followup(channel_id, session_id, query) (line 382-418)
func ResolveTeamRebuildFollowup(ctx context.Context, channelID, sessionID, query string) (string, string) {
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/evolve_rebuild") {
		return "", ""
	}

	tm := team.GetTeamManager(channelID)
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		return "", "团队技能重建不可用：未找到 TeamSkillEvolutionRail。"
	}

	store := rail.EvolutionStore()
	parts := strings.Fields(stripped)
	skillName := ""
	if len(parts) > 1 {
		skillName = parts[1]
	}
	var userIntent *string
	if len(parts) > 2 {
		intent := strings.Join(parts[2:], " ")
		userIntent = &intent
	}

	if skillName == "" {
		return "", "请指定 Skill 名称：`/evolve_rebuild <skill_name> [user_intent]`"
	}

	if !store.SkillExists(ctx, skillName) {
		available := strings.Join(skillruntime.FilterVisibleSkillNames(store.ListSkillNames(ctx)), "、")
		if available == "" {
			available = "（无可用 Skill）"
		}
		return "", fmt.Sprintf("未找到 Skill '%s'。当前可用：%s", skillName, available)
	}

	followupPrompt, err := rail.RequestRebuild(ctx, skillName, userIntent, 0.5)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("ResolveTeamRebuildFollowup: evolve_rebuild failed")
		return "", fmt.Sprintf("团队技能重建分析失败：%s", err)
	}

	if followupPrompt == "" {
		return "", fmt.Sprintf("Skill '%s' 未生成可执行的重建指令。", skillName)
	}

	return followupPrompt, ""
}
```

- [ ] **Step 2: 在 team_helpers_test.go 追加 team slash 测试**

这些函数依赖 TeamManager 和 TeamSkillEvolutionRail，单元测试中无法直接构造真实 rail。采用**接口层测试**：测试非 /evolve 前缀返回 nil、/evolve 无参报错等逻辑分支。完整集成测试在 integration tag 下进行。

```go
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
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestHandleTeamEvolveListCommand|TestHandleTeamSlashCommand|TestResolveTeamRebuildFollowup" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/team_helpers.go internal/swarm/server/adapter/team_helpers_test.go
git commit -m "feat(10.3.8): HandleTeamEvolveListCommand/HandleTeamSlashCommand/ResolveTeamRebuildFollowup + 测试"
```

---

### Task 7: 增强 deep_adapter_team.go — 补充 directives + slash 分支 + ⤵️ 桩方法

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go`

- [ ] **Step 1: 增强 processTeamMessageStream 中的 query directives 和 team slash 命令分支**

读取现有 `processTeamMessageStream` 方法，在其 `go func()` 内部、`isFirstRequest` 判断后补充：

```go
	// 在 go func() 内，isFirstRequest 判断后补充：

	if isFirstRequest {
		queryText := paramsString(inputs, "query", "")
		cleanedQuery, hideDM, debug := extractQueryDirectives(queryText)
		if hideDM || debug {
			logger.Info(logComponent).
				Str("session_id", sessionID).
				Str("channel_id", channelID).
				Bool("hide_dm", hideDM).
				Bool("debug", debug).
				Msg("processTeamMessageStream: query directives captured for first team request")
		}
		// 对齐 Python: query 变量替换为 cleanedQuery
		_ = cleanedQuery
		_ = hideDM
		_ = debug
	}

	// 步骤 4: 处理 team slash 命令
	// 对齐 Python: slash_result = await _handle_team_slash_command(channel_id, session_id, query_text)
	queryText := paramsString(inputs, "query", "")
	slashResult := HandleTeamSlashCommand(ctx, channelID, sessionID, queryText)
	if slashResult != nil {
		// 审批 chunks
		if approvalChunks, ok := slashResult["approval_chunks"].([]map[string]any); ok && len(approvalChunks) > 0 {
			for _, chunk := range approvalChunks {
				ch <- &agentschema.AgentResponseChunk{
					RequestID:  *req.RequestID,
					ChannelID:  channelID,
					Payload:    chunk,
					IsComplete: false,
				}
			}
			ch <- teamProcessingDoneChunk(*req.RequestID, channelID, sessionID)
			ch <- &agentschema.AgentResponseChunk{
				RequestID:  *req.RequestID,
				ChannelID:  channelID,
				Payload:    map[string]any{"event_type": "chat.done"},
				IsComplete: true,
			}
			return
		}
		// 非审批结果（final 或 error）
		resultType, _ := slashResult["result_type"].(string)
		content, _ := slashResult["output"].(string)
		var payload map[string]any
		if resultType == "error" {
			payload = map[string]any{"event_type": "chat.error", "error": content}
		} else {
			payload = map[string]any{"event_type": "chat.final", "content": content}
		}
		ch <- &agentschema.AgentResponseChunk{
			RequestID:  *req.RequestID,
			ChannelID:  channelID,
			Payload:    payload,
			IsComplete: false,
		}
		ch <- teamProcessingDoneChunk(*req.RequestID, channelID, sessionID)
		ch <- &agentschema.AgentResponseChunk{
			RequestID:  *req.RequestID,
			ChannelID:  channelID,
			IsComplete: true,
		}
		return
	}
```

注意：需要确认 `req.RequestID` 是 `*string` 还是 `string`——查看 `AgentRequest` 结构体中 `RequestID` 字段类型。当前代码使用 `*req.SessionID`，说明 `SessionID` 是 `*string`，但 `RequestID` 需确认。如果也是 `*string`，需要用 `ptrToStr` 或直接解引用。

- [ ] **Step 2: 新增 ⤵️ 桩方法**

```go
// ensureMonitorForActiveRuntime 在 runtime 就绪后挂载 TeamMonitorHandler。
// 对齐 Python: ensure_monitor_for_active_runtime(channel_id, session_id, team_name, hide_dm)
// ⤵️(#9.85): 依赖 Runner.get_agent_team_monitor
func (d *DeepAdapter) ensureMonitorForActiveRuntime(ctx context.Context, channelID, sessionID, teamName string, hideDM bool) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("team_name", teamName).
		Msg("ensureMonitorForActiveRuntime: ⤵️(#9.85) pending Runner.get_agent_team_monitor")
}

// ensureTeamEvolutionWatcher 启动 team evolution 监控。
// 对齐 Python: ensure_team_evolution_watcher(channel_id, session_id, *, source)
// 检查逻辑可落地，启动 goroutine 处 ⤵️(#9.85)
func (d *DeepAdapter) ensureTeamEvolutionWatcher(ctx context.Context, channelID, sessionID, source string) {
	tm := team.GetTeamManager(channelID)

	// 检查已有 watcher
	cancelFn := tm.GetTeamEvolutionWatcher(sessionID)
	if cancelFn != nil {
		logger.Info(logComponent).
			Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: evolution monitor already running")
		return
	}

	// 获取 rail
	rail := tm.GetTeamSkillRail(sessionID)
	if rail == nil {
		logger.Warn(logComponent).
			Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: no TeamSkillEvolutionRail found, deferred")
		return
	}

	// 检查 auto_scan
	if !rail.AutoScan() {
		logger.Info(logComponent).
			Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
			Msg("ensureTeamEvolutionWatcher: auto_scan disabled, skipped")
		return
	}

	// ⤵️(#9.85): 启动 watcher goroutine
	logger.Info(logComponent).
		Str("channel_id", channelID).Str("session_id", sessionID).Str("source", source).
		Msg("ensureTeamEvolutionWatcher: ⤵️(#9.85) pending watchTeamEvolutionAndPush goroutine")
}

// consumeStreamWithQuery 后台消费 team 流并广播事件。
// 对齐 Python: _consume_stream_with_query(channel_id, session_id, team_spec, initial_query, *, round_id, envs)
// ⤵️(#9.85): 依赖 Runner.run_agent_team_streaming
func (d *DeepAdapter) consumeStreamWithQuery(ctx context.Context, channelID, sessionID string, teamSpec any, initialQuery string) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("consumeStreamWithQuery: ⤵️(#9.85) pending Runner.run_agent_team_streaming")
}

// consumeMonitorEvents 后台消费 monitor 事件并广播。
// 对齐 Python: _consume_monitor_events(channel_id, session_id, monitor_handler)
// ⤵️(#10.6): 依赖 TeamMonitorHandler 事件流
func (d *DeepAdapter) consumeMonitorEvents(ctx context.Context, channelID, sessionID string) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("consumeMonitorEvents: ⤵️(#10.6) pending TeamMonitorHandler events")
}

// watchTeamEvolutionAndPushTeam 后台监控 TeamSkillEvolutionRail 并推送状态。
// 对齐 Python: _watch_team_evolution_and_push(channel_id, session_id, rail)
// ⤵️(#9.85): 依赖 Runner 流式接口
func (d *DeepAdapter) watchTeamEvolutionAndPushTeam(ctx context.Context, channelID, sessionID string, rail *evolution.TeamSkillEvolutionRail) {
	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("watchTeamEvolutionAndPushTeam: ⤵️(#9.85) pending evolution watcher implementation")
}
```

需要在 `deep_adapter_team.go` 的 import 中补充 `"context"` 和 `evolution` 包（如尚未导入）。

- [ ] **Step 3: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/ 2>&1 | head -30`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_team.go
git commit -m "feat(10.3.8): processTeamMessageStream 补充 directives+slash 分支 + ⤵️ 桩方法"
```

---

### Task 8: 更新 doc.go + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/swarm/server/adapter/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 在 doc.go 文件目录中添加 team_helpers.go 条目**

在 `deep_adapter_team.go` 条目前添加：

```
//	├── team_helpers.go            # Team 辅助函数：directives/事件判断/广播/slash命令/元数据同步
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 10.3.8 的状态**

将 10.3.7-11 行中的 `TeamHelpers☐` 改为 `TeamHelpers🔄(辅助函数✅/主流程⤵️9.85)`。

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(10.3.8): 更新 doc.go 文件目录 + IMPLEMENTATION_PLAN 状态"
```

---

### Task 9: 全量编译 + 测试验证

- [ ] **Step 1: 运行全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行 adapter 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -v -count=1`
Expected: 所有测试 PASS

- [ ] **Step 3: 运行测试覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/server/adapter/`
Expected: 覆盖率不低于之前

- [ ] **Step 4: 最终提交**

```bash
git add -A
git commit -m "feat(10.3.8): TeamHelpers 辅助函数实现完成 — 全量编译+测试验证通过"
```

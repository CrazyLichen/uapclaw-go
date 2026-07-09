# JiuWenClaw 门面实现计划（层级 0+1）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 JiuWenClaw Agent 统一门面的核心逻辑（结构体改造 + 辅助函数 + 方法实现），层级 2-4 先 stub 并标注 ⤵️ 回填点。

**Architecture:** JiuWenClaw 作为 Agent 统一门面，持有 AgentAdapter（延迟初始化）、SessionManager、SkillManager（stub）。请求流程：前端→Gateway→AgentServer→AgentManager.GetAgent→JiuWenClaw.ProcessMessage/Stream→ensureAdapter→buildInputs→SessionManager.SubmitAndWait→adapter.ProcessMessageImpl/StreamImpl。

**Tech Stack:** Go 1.22+, gorilla/websocket, go-chi/chi, testify, zerolog

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/swarm/server/runtime/build_user_prompt.go` | BuildUserPrompt 函数 |
| 新建 | `internal/swarm/server/runtime/build_user_prompt_test.go` | 测试 |
| 新建 | `internal/swarm/server/runtime/build_inputs.go` | BuildInputs 方法 |
| 新建 | `internal/swarm/server/runtime/build_inputs_test.go` | 测试 |
| 新建 | `internal/swarm/server/runtime/session_history.go` | AppendHistoryRecord + 写入队列 + worker |
| 新建 | `internal/swarm/server/runtime/session_history_test.go` | 测试 |
| 修改 | `internal/swarm/server/runtime/jiowenclaw.go` | 结构体改造 + 全部方法实现 |
| 修改 | `internal/swarm/server/runtime/jiowenclaw_test.go` | 更新测试 |
| 修改 | `internal/swarm/server/runtime/doc.go` | 更新文件目录 |

---

### Task 1: BuildUserPrompt 函数实现

**Files:**
- Create: `internal/swarm/server/runtime/build_user_prompt.go`
- Create: `internal/swarm/server/runtime/build_user_prompt_test.go`

- [ ] **Step 1: 写 build_user_prompt_test.go 的失败测试**

```go
package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUserPrompt_中文基本格式(t *testing.T) {
	result := BuildUserPrompt("你好", nil, "web", "zh", nil, nil)
	assert.Contains(t, result, "你收到一条消息：")
	assert.Contains(t, result, `"content":"你好"`)
	assert.Contains(t, result, `"source":"web"`)
	assert.Contains(t, result, `"preferred_response_language":"zh"`)
	assert.Contains(t, result, `"type":"user input"`)
}

func TestBuildUserPrompt_英文基本格式(t *testing.T) {
	result := BuildUserPrompt("hello", nil, "web", "en", nil, nil)
	assert.Contains(t, result, "You receive a new message:")
	assert.Contains(t, result, `"content":"hello"`)
	assert.Contains(t, result, `"source":"web"`)
}

func TestBuildUserPrompt_cron模式(t *testing.T) {
	result := BuildUserPrompt("查询状态", nil, "cron", "zh", nil, nil)
	assert.Contains(t, result, "必须输出查询到的内容")
	assert.Contains(t, result, `"source":"system"`)
	assert.Contains(t, result, `"type":"cron"`)
}

func TestBuildUserPrompt_heartbeat模式(t *testing.T) {
	result := BuildUserPrompt("心跳检查", nil, "heartbeat", "zh", nil, nil)
	assert.Contains(t, result, `"source":"system"`)
	assert.Contains(t, result, `"type":"heartbeat"`)
}

func TestBuildUserPrompt_带files(t *testing.T) {
	files := map[string]any{"file1.py": "changed"}
	result := BuildUserPrompt("修改了文件", files, "web", "zh", nil, nil)
	assert.Contains(t, result, `"files_updated_by_user"`)
}

func TestBuildUserPrompt_带trustedDirs(t *testing.T) {
	dirs := []string{"/home/user/project"}
	result := BuildUserPrompt("hello", nil, "web", "en", dirs, nil)
	assert.Contains(t, result, `"trusted_dirs"`)
}

func TestBuildUserPrompt_带interactionContext(t *testing.T) {
	metadata := map[string]any{"interaction_context": "之前的对话上下文"}
	result := BuildUserPrompt("继续", nil, "web", "zh", nil, metadata)
	// interaction_context 作为前缀出现在 prompt 开头
	assert.True(t, strings.HasPrefix(result, "\n之前的对话上下文\n\n"))
}

func TestBuildUserPrompt_含时区和时间戳(t *testing.T) {
	result := BuildUserPrompt("test", nil, "web", "zh", nil, nil)
	assert.Contains(t, result, `"timezone":"Asia/Shanghai"`)
	assert.Contains(t, result, `"timestamp"`)
}

func TestBuildUserPrompt_skillsUseSlashCommand(t *testing.T) {
	result := BuildUserPrompt("/skills use web_search, 搜索Go语言", nil, "web", "zh", nil, nil)
	assert.Contains(t, result, `"skills_to_use"`)
	// query 部分应被替换为 "搜索Go语言"
	assert.Contains(t, result, `"content":"搜索Go语言"`)
}

func TestBuildUserPrompt_输出为合法JSON(t *testing.T) {
	result := BuildUserPrompt("test content", nil, "web", "zh", nil, nil)
	// 去掉 interaction_prefix 和 prompt_prefix，剩余部分应为合法 JSON
	lines := strings.SplitN(result, "\n", -1)
	// 找到第一个 { 开头的行
	jsonStart := -1
	for i, line := range lines {
		if strings.Contains(line, "{") {
			jsonStart = i
			break
		}
	}
	if jsonStart >= 0 {
		// 从 prompt_prefix 后面提取 JSON 部分
		idx := strings.Index(result, "{")
		if idx >= 0 {
			jsonStr := result[idx:]
			var parsed map[string]any
			err := json.Unmarshal([]byte(jsonStr), &parsed)
			require.NoError(t, err, "BuildUserPrompt 输出应该是合法 JSON")
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -run TestBuildUserPrompt -v -count=1 2>&1 | head -20`
Expected: 编译错误（BuildUserPrompt 未定义）

- [ ] **Step 3: 写 build_user_prompt.go 实现**

```go
package runtime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// skillsUseRegex /skills use 斜杠命令匹配正则。
var skillsUseRegex = regexp.MustCompile(`^/skills use\s+(?P<skill_names>[^,]+)\s*,\s*(?P<query>.*)$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildUserPrompt 将用户 query 包装为结构化 JSON prompt。
//
// 返回格式: interactionPrefix + promptPrefix + json.dumps(userMessageContext)
//
// 对齐 Python: build_user_prompt(content, files, channel, language, *, trusted_dirs, metadata)
func BuildUserPrompt(content string, files map[string]any, channel string, language string,
	trustedDirs []string, metadata map[string]any) string {
	// 1. interaction_context 前缀
	interactionPrefix := ""
	if metadata != nil {
		if ctx, ok := metadata["interaction_context"]; ok {
			if ctxStr, ok := ctx.(string); ok && strings.TrimSpace(ctxStr) != "" {
				interactionPrefix = fmt.Sprintf("\n%s\n\n", ctxStr)
			}
		}
	}

	// 2. /skills use 斜杠命令解析
	skillsToUse, newContent := handleSkillsUseSlashCommand(content)
	if newContent != "" {
		content = newContent
	}

	// 3. 按语言+channel 构建 prompt 前缀
	var prompt string
	if language == "zh" {
		prompt = "你收到一条消息：\n"
		if channel == "cron" {
			prompt = "你收到一条消息，对于查询类任务必须输出查询到的内容，不要只回复确认或只记录到memory：\n"
		}
	} else {
		prompt = "You receive a new message:\n"
		if channel == "cron" {
			prompt = "You receive a new message. For query tasks, you must output the queried content—don't just reply with confirmation or only record to memory:\n"
		}
	}

	// 4. 构建 userMessageContext
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	nowStr := now.Format("2006-01-02 15:04:05")

	userMessageContext := map[string]any{
		"source":                      channel,
		"timezone":                    "Asia/Shanghai",
		"timestamp":                   nowStr,
		"preferred_response_language": language,
		"content":                     content,
		"type":                        "user input",
	}

	// cron/heartbeat 特殊处理
	if channel == "cron" || channel == "heartbeat" {
		userMessageContext["source"] = "system"
		userMessageContext["type"] = channel
	}

	// files_updated_by_user
	if channel != "cron" && channel != "heartbeat" {
		if filesJSON, err := json.Marshal(files); err == nil {
			userMessageContext["files_updated_by_user"] = string(filesJSON)
		}
	}

	// skills_to_use
	if len(skillsToUse) > 0 {
		userMessageContext["skills_to_use"] = skillsToUse
	}

	// trusted_dirs
	if len(trustedDirs) > 0 {
		if dirsJSON, err := json.Marshal(trustedDirs); err == nil {
			userMessageContext["trusted_dirs"] = string(dirsJSON)
		}
	}

	// 5. 序列化并返回
	contextJSON, err := json.Marshal(userMessageContext)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("BuildUserPrompt 序列化失败")
		return content
	}

	return interactionPrefix + prompt + string(contextJSON)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleSkillsUseSlashCommand 解析 /skills use 斜杠命令。
//
// 对齐 Python: _handle_skills_use_slash_command(query)
func handleSkillsUseSlashCommand(query string) (skillsToUse []string, newQuery string) {
	stripped := strings.TrimSpace(query)
	if !strings.HasPrefix(stripped, "/skills use") {
		return nil, ""
	}

	matches := skillsUseRegex.FindStringSubmatch(stripped)
	if len(matches) > 0 {
		skillNamesIdx := skillsUseRegex.SubexpIndex("skill_names")
		queryIdx := skillsUseRegex.SubexpIndex("query")
		if skillNamesIdx >= 0 && queryIdx >= 0 {
			return []string{matches[skillNamesIdx]}, matches[queryIdx]
		}
	}

	logger.Warn(logComponent).Str("query", stripped).Msg("无法解析 /skills use 命令")
	return nil, ""
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -run TestBuildUserPrompt -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/build_user_prompt.go internal/swarm/server/runtime/build_user_prompt_test.go
git commit -m "feat(runtime): add BuildUserPrompt function aligned with Python build_user_prompt"
```

---

### Task 2: SessionHistory 写入实现

**Files:**
- Create: `internal/swarm/server/runtime/session_history.go`
- Create: `internal/swarm/server/runtime/session_history_test.go`

- [ ] **Step 1: 写 session_history_test.go 的失败测试**

```go
package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendHistoryRecord_基本写入(t *testing.T) {
	sessionID := "test-session-basic"
	requestID := "req-001"
	channelID := "web"

	// 使用临时目录覆盖 sessions 目录
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	AppendHistoryRecord(sessionID, requestID, channelID, "user", "你好", float64(time.Now().UnixMilli())/1000, "", nil, nil, "")

	// 等待异步写入完成（简短等待）
	time.Sleep(100 * time.Millisecond)

	records, err := ReadHistoryRecords(sessionID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "req-001:user", records[0]["id"])
	assert.Equal(t, "user", records[0]["role"])
	assert.Equal(t, "你好", records[0]["content"])
	assert.Equal(t, requestID, records[0]["request_id"])
	assert.Equal(t, channelID, records[0]["channel_id"])
}

func TestAppendHistoryRecord_role归一化(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	AppendHistoryRecord("sess-1", "r1", "web", "assistant", "回复", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	AppendHistoryRecord("sess-1", "r2", "web", "system", "系统消息", 2.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-1")
	require.Len(t, records, 2)
	assert.Equal(t, "assistant", records[0]["role"])  // assistant 保持
	assert.Equal(t, "user", records[1]["role"])       // system → user 归一化
}

func TestAppendHistoryRecord_eventType仅assistant写入(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	AppendHistoryRecord("sess-2", "r1", "web", "user", "问题", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	AppendHistoryRecord("sess-2", "r2", "web", "assistant", "回答", 2.0, "chat.final", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-2")
	require.Len(t, records, 2)
	// user 记录不应有 event_type
	_, hasEventType := records[0]["event_type"]
	assert.False(t, hasEventType)
	// assistant 记录应有 event_type
	assert.Equal(t, "chat.final", records[1]["event_type"])
}

func TestAppendHistoryRecord_extra字段展开(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	extra := map[string]any{"tool_result": map[string]any{"name": "search"}}
	AppendHistoryRecord("sess-3", "r1", "web", "assistant", "工具结果", 1.0, "chat.tool_result", extra, nil, "")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-3")
	require.Len(t, records, 1)
	assert.Equal(t, "chat.tool_result", records[0]["event_type"])
	// extra 展开到顶层
	_, hasToolResult := records[0]["tool_result"]
	assert.True(t, hasToolResult)
}

func TestAppendHistoryRecord_mode字段(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	AppendHistoryRecord("sess-4", "r1", "web", "assistant", "回答", 1.0, "chat.final", nil, nil, "team")
	time.Sleep(100 * time.Millisecond)

	records, _ := ReadHistoryRecords("sess-4")
	require.Len(t, records, 1)
	assert.Equal(t, "team", records[0]["mode"])
}

func TestTruncateHistoryRecords(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	AppendHistoryRecord("sess-5", "r1", "web", "user", "第一条", 1.0, "", nil, nil, "")
	time.Sleep(50 * time.Millisecond)
	AppendHistoryRecord("sess-5", "r2", "web", "user", "第二条", 2.0, "", nil, nil, "")
	time.Sleep(50 * time.Millisecond)
	AppendHistoryRecord("sess-5", "r3", "web", "user", "第三条", 3.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	// 截断到 r2
	err := TruncateHistoryRecords("sess-5", "r2")
	require.NoError(t, err)

	records, _ := ReadHistoryRecords("sess-5")
	assert.Len(t, records, 2)
	assert.Equal(t, "第一条", records[0]["content"])
	assert.Equal(t, "第二条", records[1]["content"])
}

func TestHistoryFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)

	AppendHistoryRecord("my-session", "r1", "web", "user", "test", 1.0, "", nil, nil, "")
	time.Sleep(100 * time.Millisecond)

	// 验证文件存在
	expectedPath := filepath.Join(tmpDir, "agent", "sessions", "my-session", "history.json")
	_, err := os.Stat(expectedPath)
	require.NoError(t, err, "history.json 应该存在于正确的路径")

	// 验证是合法 JSON 数组
	data, _ := os.ReadFile(expectedPath)
	var records []map[string]any
	err = json.Unmarshal(data, &records)
	require.NoError(t, err)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -run "TestAppend|TestTruncate|TestHistoryFile" -v -count=1 2>&1 | head -20`
Expected: 编译错误

- [ ] **Step 3: 写 session_history.go 实现**

```go
package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// historyWriteItem 写入队列项。
type historyWriteItem struct {
	// sessionID 会话标识
	sessionID string
	// record 待写入记录
	record map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// historyQueueSize 写入队列容量，对齐 Python _WRITE_QUEUE maxsize=20000
	historyQueueSize = 20000
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// historyWriteQueue 异步写入队列。
	historyWriteQueue chan historyWriteItem
	// historyFileMu 文件锁（read-modify-write 期间持锁）。
	historyFileMu sync.Mutex
	// historyWorkerOnce 保证 worker 只启动一次。
	historyWorkerOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AppendHistoryRecord 向指定 session 的 history.json 异步追加一条记录。
//
// 对齐 Python: append_history_record(session_id, request_id, channel_id, role, content, timestamp, event_type, extra, channel_metadata, mode)
func AppendHistoryRecord(sessionID, requestID, channelID, role, content string,
	timestamp float64, eventType string, extra map[string]any,
	channelMetadata map[string]any, mode string) {
	// 规范化
	sid := normalizeSessionID(sessionID)
	rid := requestID
	cid := channelID
	roleNorm := "assistant"
	if role != "assistant" {
		roleNorm = "user"
	}
	contentText := content

	// 构建记录项
	item := map[string]any{
		"id":         rid + ":" + roleNorm,
		"role":       roleNorm,
		"request_id": rid,
		"channel_id": cid,
		"timestamp":  timestamp,
		"content":    contentText,
	}

	// event_type：仅在 assistant 且非空时写入
	if roleNorm == "assistant" && eventType != "" {
		item["event_type"] = eventType
	}

	// extra 字段展开到顶层
	if len(extra) > 0 {
		for k, v := range extra {
			item[k] = v
		}
	}

	// mode：非空时写入
	if mode != "" {
		item["mode"] = mode
	}

	// 确保 worker 已启动
	ensureHistoryWorker()

	// 尝试入队
	select {
	case historyWriteQueue <- historyWriteItem{sessionID: sid, record: item}:
	default:
		// 队列满时退化为同步写，避免丢失记录
		writeHistoryItem(sid, item)
	}
}

// ReadHistoryRecords 读取指定 session 的全部 history 记录。
//
// 对齐 Python: read_history_records(session_id)
func ReadHistoryRecords(sessionID string) ([]map[string]any, error) {
	sid := normalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	return readHistoryFile(fpath)
}

// TruncateHistoryRecords 截断 history 到指定 request_id（rewind 使用）。
//
// 对齐 Python: truncate_history_records(session_id, request_id)
func TruncateHistoryRecords(sessionID string, requestID string) error {
	sid := normalizeSessionID(sessionID)
	fpath := historyFilePath(sid)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	records, err := readHistoryFile(fpath)
	if err != nil {
		return err
	}

	// 找到 request_id 对应的最后一个索引，保留到该位置
	truncateIdx := -1
	for i, r := range records {
		if rid, ok := r["request_id"].(string); ok && rid == requestID {
			truncateIdx = i
		}
	}

	if truncateIdx < 0 {
		// 未找到，不截断
		return nil
	}

	truncated := records[:truncateIdx+1]
	return writeHistoryFile(fpath, truncated)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureHistoryWorker 启动异步写入 worker（单 goroutine，sync.Once 保证）。
func ensureHistoryWorker() {
	historyWorkerOnce.Do(func() {
		historyWriteQueue = make(chan historyWriteItem, historyQueueSize)
		go historyWorker()
	})
}

// historyWorker 写入队列消费者。
func historyWorker() {
	for item := range historyWriteQueue {
		writeHistoryItem(item.sessionID, item.record)
	}
}

// writeHistoryItem 同步写入单条记录（持文件锁）。
func writeHistoryItem(sessionID string, record map[string]any) {
	fpath := historyFilePath(sessionID)

	historyFileMu.Lock()
	defer historyFileMu.Unlock()

	records, _ := readHistoryFile(fpath)
	records = append(records, record)

	if err := writeHistoryFile(fpath, records); err != nil {
		logger.Error(logComponent).Err(err).Str("session_id", sessionID).Msg("history 写入失败")
	}
}

// historyFilePath 返回 history.json 的完整路径。
func historyFilePath(sessionID string) string {
	dir := filepath.Join(workspace.AgentSessionsDir(), sessionID)
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "history.json")
}

// readHistoryFile 读取 history.json 全量记录。
func readHistoryFile(fpath string) ([]map[string]any, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return []map[string]any{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []map[string]any{}, nil
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// writeHistoryFile 写入 history.json 全量记录。
func writeHistoryFile(fpath string, records []map[string]any) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fpath, data, 0o644)
}

// normalizeSessionID 规范化 sessionID，空串→"default"。
func normalizeSessionID(sessionID string) string {
	sid := sessionID
	if sid == "" {
		return "default"
	}
	return sid
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -run "TestAppend|TestTruncate|TestHistoryFile" -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/session_history.go internal/swarm/server/runtime/session_history_test.go
git commit -m "feat(runtime): add session history persistence (AppendHistoryRecord/Read/Truncate)"
```

---

### Task 3: BuildInputs 方法实现

**Files:**
- Create: `internal/swarm/server/runtime/build_inputs.go`
- Create: `internal/swarm/server/runtime/build_inputs_test.go`

- [ ] **Step 1: 写 build_inputs_test.go 的失败测试**

```go
package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

func TestJiuWenClaw_BuildInputs_基本字段提取(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query": "你好世界",
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, paramsJSON)

	inputs, memoryMode, rawQuery := jw.BuildInputs(req)

	assert.Equal(t, "你好世界", rawQuery)
	_ = memoryMode // memoryMode 依赖 config，当前可能为空
	assert.NotNil(t, inputs["query"])
	assert.NotNil(t, inputs["conversation_id"])
	assert.NotNil(t, inputs["channel"])
	assert.NotNil(t, inputs["language"])
}

func TestJiuWenClaw_BuildInputs_projectDir优先级(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query":       "test",
		"project_dir": "/path/from/params",
	}
	paramsJSON, _ := json.Marshal(params)
	metadata := map[string]any{"project_dir": "/path/from/metadata"}
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentMetadata(metadata))

	inputs, _, _ := jw.BuildInputs(req)

	// params 优先
	assert.Equal(t, "/path/from/params", inputs["project_dir"])
}

func TestJiuWenClaw_BuildInputs_cron字段转换(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query": "定时任务",
		"cron":  map[string]any{"schedule": "0 9 * * *"},
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-3", "cron", schema.ReqMethodChatSend, paramsJSON)

	inputs, _, _ := jw.BuildInputs(req)

	runVal, hasRun := inputs["run"]
	assert.True(t, hasRun)
	runMap, ok := runVal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "cron", runMap["kind"])
}

func TestJiuWenClaw_BuildInputs_trustedDirs提取(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query":        "test",
		"trusted_dirs": []any{"/home/user/proj1", "/home/user/proj2"},
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-4", "web", schema.ReqMethodChatSend, paramsJSON)

	inputs, _, _ := jw.BuildInputs(req)

	dirs, ok := inputs["trusted_dirs"].([]string)
	assert.True(t, ok)
	assert.Len(t, dirs, 2)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -run "TestJiuWenClaw_BuildInputs" -v -count=1 2>&1 | head -20`
Expected: 编译错误

- [ ] **Step 3: 写 build_inputs.go 实现**

```go
package runtime

import (
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInputs 构建 adapter 所需的 inputs 字典。
//
// 对齐 Python: JiuWenClaw._build_inputs(request) -> (inputs, memoryMode, rawQuery)
//
// 返回: inputs 字典、memoryMode 字符串、原始 query。
func (jw *JiuWenClaw) BuildInputs(request *schema.AgentRequest) (map[string]any, string, string) {
	// 1. 获取配置
	var configBase map[string]any
	if cfg, err := config.New(); err == nil {
		if raw, err2 := cfg.Load(); err2 == nil {
			configBase = raw
		}
	}

	memoryMode := ""
	if configBase != nil {
		if mm, ok := configBase["memory_mode"]; ok {
			if mmStr, ok := mm.(string); ok {
				memoryMode = mmStr
			}
		}
	}

	// 2. 解析 params
	params := parseRequestParams(request)

	// 3. 提取基础字段
	query, _ := params["query"].(string)
	channel := extractChannelFromSessionID(request)
	language := "zh"
	if configBase != nil {
		if lang, ok := configBase["preferred_language"]; ok {
			if langStr, ok := lang.(string); ok && langStr != "" {
				language = langStr
			}
		}
	}

	// 4. 提取 trusted_dirs
	var trustedDirs []string
	if rawDirs, ok := params["trusted_dirs"]; ok {
		if dirsSlice, ok := rawDirs.([]any); ok {
			for _, d := range dirsSlice {
				if dirStr, ok := d.(string); ok && strings.TrimSpace(dirStr) != "" {
					trustedDirs = append(trustedDirs, strings.TrimSpace(dirStr))
				}
			}
		}
	}

	// 5. 提取 project_dir / cwd
	metadata := request.Metadata
	projectDir := extractStringWithFallback(params, "project_dir", metadata, "project_dir")
	cwd := extractStringWithFallback(params, "cwd", metadata, "cwd")

	// 6. 构建 finalQuery
	var finalQuery any
	// ⤵️ 10.3.2: InteractiveInput 类型判断（当前 query 为 string，直接走 BuildUserPrompt）
	// ⤵️ 10.3.2: answers 分支 → 构建 InteractiveInput（当前 stub，fallback 到 BuildUserPrompt）

	files, _ := params["files"].(map[string]any)
	finalQuery = BuildUserPrompt(query, files, channel, language, trustedDirs, metadata)

	// 7. 组装 inputs 字典
	sessionIDStr := ""
	if request.SessionID != nil {
		sessionIDStr = *request.SessionID
	}
	inputs := map[string]any{
		"conversation_id": sessionIDStr,
		"query":           finalQuery,
		"channel":         channel,
		"language":        language,
	}

	// enable_memory
	enableMemory := true
	if metadata != nil {
		if em, ok := metadata["enable_memory"]; ok {
			if emBool, ok := em.(bool); ok {
				enableMemory = emBool
			}
		}
	}
	inputs["enable_memory"] = enableMemory

	// 可选字段
	if len(trustedDirs) > 0 {
		inputs["trusted_dirs"] = trustedDirs
	}
	if projectDir != "" {
		inputs["project_dir"] = projectDir
	}
	if cwd != "" {
		inputs["cwd"] = cwd
	}

	// run 字段
	if run, ok := params["run"]; ok {
		inputs["run"] = run
	}

	// cron 字段转换
	if cron, ok := params["cron"]; ok {
		inputs["run"] = map[string]any{
			"kind":    "cron",
			"context": map[string]any{"extra": map[string]any{"cron": cron}},
		}
	}

	return inputs, memoryMode, query
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseRequestParams 解析 AgentRequest.Params（json.RawMessage）为 map。
func parseRequestParams(request *schema.AgentRequest) map[string]any {
	if request.Params == nil || len(request.Params) == 0 {
		return make(map[string]any)
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return make(map[string]any)
	}
	return params
}

// extractChannelFromSessionID 从 sessionID 提取 channel（第一个 _ 前部分）。
func extractChannelFromSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil && *request.SessionID != "" {
		parts := strings.SplitN(*request.SessionID, "_", 2)
		if parts[0] != "" {
			return parts[0]
		}
	}
	return "web"
}

// extractStringWithFallback 从 params 和 metadata 提取字符串，params 优先。
func extractStringWithFallback(params map[string]any, paramKey string, metadata map[string]any, metaKey string) string {
	// params 优先
	if val, ok := params[paramKey]; ok {
		if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
			return strings.TrimSpace(str)
		}
	}
	// metadata 兜底
	if metadata != nil {
		if val, ok := metadata[metaKey]; ok {
			if str, ok := val.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
		}
	}
	return ""
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -run "TestJiuWenClaw_BuildInputs" -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/build_inputs.go internal/swarm/server/runtime/build_inputs_test.go
git commit -m "feat(runtime): add BuildInputs method aligned with Python _build_inputs"
```

---

### Task 4: JiuWenClaw 结构体改造 + 核心方法实现

**Files:**
- Modify: `internal/swarm/server/runtime/jiowenclaw.go`
- Modify: `internal/swarm/server/runtime/jiowenclaw_test.go`

- [ ] **Step 1: 重写 jiowenclaw.go — 结构体 + 全部方法**

将 `JiuWenClaw struct{}` 替换为：

```go
package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JiuWenClaw Agent 统一门面。
//
// 提供：SDK 适配器路由、统一对外 API、公共编排
// （session 队列、Skills 路由、heartbeat、流式包装）。
//
// 对齐 Python: jiuwenswarm/server/runtime/agent_adapter/interface.py (JiuWenClaw)
type JiuWenClaw struct {
	// adapter SDK 适配器（延迟初始化，ensureAdapter 时创建）。
	adapter adapter.AgentAdapter

	// skillManager 技能管理器（server 层）。
	// ⤵️ 10.3.2: 替换为 swarm/server/runtime/skill.SkillManager 实例
	skillManager interface{}

	// sessionManager 会话任务队列管理器。
	sessionManager *SessionManager

	// skilldevService SkillDev 服务（懒初始化）。
	// ⤵️ 10.3.2: 替换为 SkillDevService 实例
	skilldevService interface{}

	// adapterMu 保护 adapter 字段的并发访问。
	adapterMu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJiuWenClaw 创建 JiuWenClaw 实例。
//
// 对齐 Python: JiuWenClaw.__init__()
func NewJiuWenClaw() *JiuWenClaw {
	return &JiuWenClaw{
		sessionManager: NewSessionManager(),
	}
}

// ProcessMessage 处理非流式 Agent 请求。
//
// 对齐 Python: JiuWenClaw.process_message(request)
func (jw *JiuWenClaw) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 1. CANCEL 分支 → 委托 ProcessInterrupt
	if request.ReqMethod == schema.ReqMethodChatCancel {
		return jw.ProcessInterrupt(ctx, request)
	}

	// 2. 确保 adapter
	mode := jw.adapterModeForRequest(request)
	a, err := jw.ensureAdapter(mode)
	if err != nil {
		return nil, err
	}

	// 3. ANSWER 分支
	if request.ReqMethod == schema.ReqMethodChatAnswer {
		return a.HandleUserAnswer(ctx, request)
	}

	// 4. heartbeat 分支
	if resp, herr := a.HandleHeartbeat(ctx, request); resp != nil {
		return resp, herr
	}

	// 5. Skills / SkillDev / Plugins 分支
	// ⤵️ 10.3.2: handleSkillsRequest(request) — 当前 stub，返回 nil 不拦截
	// ⤵️ 10.3.2: handleSkillDevRequest(request) — 当前 stub，返回 nil 不拦截
	// ⤵️ 10.3.2: handlePluginsRequest(request) — 当前 stub，返回 nil 不拦截

	// 6. 常规对话
	sessionID := normalizeSessionID(jw.extractSessionID(request))

	// 记录 user 历史
	AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", jw.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, nil, "")

	// 构建 inputs
	inputs, _, _ := jw.BuildInputs(request)

	// ⤵️ 10.3.2: cloud memory before-chat hook（ExtensionRegistry）

	// 提交到 session 队列并等待结果
	result, err := jw.sessionManager.SubmitAndWait(ctx, sessionID, func(taskCtx context.Context) (any, error) {
		return a.ProcessMessageImpl(taskCtx, request, inputs)
	})
	if err != nil {
		return nil, err
	}

	resp, ok := result.(*schema.AgentResponse)
	if !ok || resp == nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(true),
		), nil
	}

	// 记录 assistant 历史
	if resp.OK {
		content := jw.extractResponseContent(resp)
		AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
			"assistant", content, float64(time.Now().UnixMilli())/1000,
			"chat.final", nil, nil, "")
	}

	// ⤵️ 10.3.2: cloud memory after-chat hook

	return resp, nil
}

// ProcessMessageStream 处理流式 Agent 请求。
//
// 对齐 Python: JiuWenClaw.process_message_stream(request)
func (jw *JiuWenClaw) ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	// 1. SkillDev 流式分支
	// ⤵️ 10.3.2: handleSkillDevStreamRequest(request) — 当前 stub

	// 2. 确保 adapter
	mode := jw.adapterModeForRequest(request)
	a, err := jw.ensureAdapter(mode)
	if err != nil {
		return nil, err
	}

	// 3. 提取 sessionID
	sessionID := normalizeSessionID(jw.extractSessionID(request))

	// ⤵️ 10.3.2: Team 模式判断（isTeamMode / isAutoHarnessResume）
	// ⤵️ 10.3.2: Team 模式使用原始 query（不经过 BuildUserPrompt 包装）

	// 4. 记录 user 历史
	AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", jw.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, nil, "")

	// 5. 构建 inputs
	inputs, _, _ := jw.BuildInputs(request)

	// ⤵️ 10.3.2: cloud memory before-chat hook

	// 6. 创建中转 channel
	outCh := make(chan *schema.AgentResponseChunk, 64)
	streamDone := make(chan struct{})

	// 7. 生产者 goroutine
	go func() {
		defer close(streamDone)
		chunkCh, streamErr := a.ProcessMessageStreamImpl(ctx, request, inputs)
		if streamErr != nil {
			outCh <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID,
				map[string]any{"event_type": "chat.error", "error": streamErr.Error()},
			)
			return
		}
		for chunk := range chunkCh {
			outCh <- chunk
		}
	}()

	// 8. 消费者 goroutine
	resultCh := make(chan *schema.AgentResponseChunk, 64)
	go func() {
		defer close(resultCh)
		var finalAnswerContent string
		var finalAnswerChunks []string

		for {
			select {
			case chunk, ok := <-outCh:
				if !ok {
					goto streamComplete
				}
				if payload := chunk.Payload; payload != nil {
					if eventType, _ := payload["event_type"].(string); eventType != "" {
						if shouldRecordHistory(eventType) {
							AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
								"assistant", extractChunkContent(payload), float64(time.Now().UnixMilli())/1000,
								eventType, nil, nil, "")
						}
						if eventType == "chat.final" {
							if c, ok := payload["content"].(string); ok {
								finalAnswerContent = c
							}
						} else if eventType == "chat.delta" {
							if c, ok := payload["content"].(string); ok {
								finalAnswerChunks = append(finalAnswerChunks, c)
							}
						}
					}
				}
				resultCh <- chunk
			case <-streamDone:
				for len(outCh) > 0 {
					resultCh <- <-outCh
				}
				goto streamComplete
			}
		}

	streamComplete:
		// ⤵️ 10.3.2: cloud memory after-chat hook
		_ = finalAnswerContent
		_ = finalAnswerChunks
		resultCh <- schema.NewTerminalChunk(request.RequestID, request.ChannelID)
	}()

	// 9. 提交流式任务
	// ⤵️ 10.3.2: Team 后续请求 / Auto-Harness resume 绕过 Session 队列
	_ = jw.sessionManager.EnsureSessionProcessor(ctx, sessionID)

	return resultCh, nil
}

// ProcessInterrupt 处理中断请求。
//
// 对齐 Python: JiuWenClaw._process_interrupt(request)
func (jw *JiuWenClaw) ProcessInterrupt(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	intent := jw.extractIntent(request)
	sessionID := normalizeSessionID(jw.extractSessionID(request))

	// ⤵️ 10.3.2: Team 模式分流（_processTeamInterrupt）

	mode := jw.adapterModeForRequest(request)
	a, err := jw.ensureAdapter(mode)
	if err != nil {
		return nil, err
	}

	// pause / resume
	if intent == "pause" || intent == "resume" {
		return a.ProcessInterrupt(ctx, request)
	}

	// supplement
	if intent == "supplement" {
		resp, err := a.ProcessInterrupt(ctx, request)
		_ = jw.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(supplement)", nil)
		return resp, err
	}

	// cancel（默认）
	resp, err := a.ProcessInterrupt(ctx, request)
	// ⤵️ 10.3.2: cancelTeamWorkForSession(sessionID, channelID)
	waitTimeout := 5 * time.Second
	_ = jw.sessionManager.CancelSessionTask(ctx, sessionID, "interrupt(cancel)", &waitTimeout)
	return resp, err
}

// GetContextUsage 获取上下文使用量。
// ⤵️ 10.3.2: 需要从 adapter 获取 DeepAgent 实例后调用 GetContextUsage
func (jw *JiuWenClaw) GetContextUsage(_ string) (map[string]any, error) {
	return map[string]any{"usage": 0, "limit": 0}, nil
}

// CompressContext 压缩上下文。
// ⤵️ 10.3.2: 需要调用 DeepAgent 的压缩逻辑
func (jw *JiuWenClaw) CompressContext(_ string) (map[string]any, error) {
	return map[string]any{"ok": true, "compressed": false}, nil
}

// GenerateRecap 生成会话回顾。
// ⤵️ 10.3.2: 需要调用 DeepAgent 的回顾逻辑
func (jw *JiuWenClaw) GenerateRecap(_ string) (map[string]any, error) {
	return map[string]any{"recap": ""}, nil
}

// SwitchMode 切换运行模式。
// ⤵️ 10.3.2: 完整实现需要 DeepAdapter 支撑（session 持久化 + switch_mode + load_state）
func (jw *JiuWenClaw) SwitchMode(_, _ string) error { return nil }

// CreateInstance 创建 Agent 实例。
//
// 对齐 Python: JiuWenClaw.create_instance(config, mode, sub_mode)
func (jw *JiuWenClaw) CreateInstance(config map[string]any, mode string, subMode string) error {
	a, err := jw.ensureAdapter(mode)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := a.CreateInstance(ctx, config, mode, subMode); err != nil {
		return err
	}
	logger.Info(logComponent).
		Str("sdk", adapter.ResolveSDKChoice()).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Msg("JiuWenClaw Agent 实例已创建")
	// ⤵️ 10.3.2: 启动 dreaming 后台任务（adapter.TryStartDreaming）
	return nil
}

// ReloadAgentConfig 重载 Agent 配置。
//
// 对齐 Python: JiuWenClaw.reload_agent_config(config_base, env_overrides)
func (jw *JiuWenClaw) ReloadAgentConfig(configBase map[string]any, envOverrides map[string]any) error {
	jw.adapterMu.Lock()
	a := jw.adapter
	jw.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// ⤵️ 10.3.2: adapter.TryStopDreaming()
	if err := a.ReloadAgentConfig(context.Background(), configBase, envOverrides); err != nil {
		return err
	}
	// ⤵️ 10.3.2: adapter.TryStartDreaming()
	return nil
}

// CancelInflightWork 取消在途任务。
//
// 对齐 Python: JiuWenClaw.cancel_inflight_work()
func (jw *JiuWenClaw) CancelInflightWork() error {
	_ = jw.sessionManager.CancelAllSessionTasks(context.Background(), "[gateway disconnect]")
	jw.adapterMu.Lock()
	a := jw.adapter
	jw.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// ⤵️ 10.3.2: adapter.AbortOnGatewayDisconnect()
	return nil
}

// Cleanup 清理资源。
//
// 对齐 Python: JiuWenClaw.cleanup()
func (jw *JiuWenClaw) Cleanup() error {
	jw.adapterMu.Lock()
	a := jw.adapter
	jw.adapter = nil
	jw.adapterMu.Unlock()
	if a != nil {
		_ = a.Cleanup()
	}
	return nil
}

// GetInstance 获取底层 Agent 实例。
// ⤵️ 10.3.2: 需要返回 adapter 内部的 DeepAgent 实例
func (jw *JiuWenClaw) GetInstance() any { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureAdapter 确保 SDK adapter 已初始化，幂等。
func (jw *JiuWenClaw) ensureAdapter(mode string) (adapter.AgentAdapter, error) {
	jw.adapterMu.Lock()
	defer jw.adapterMu.Unlock()
	if jw.adapter != nil {
		return jw.adapter, nil
	}
	a, err := adapter.CreateAdapter("", mode)
	if err != nil {
		return nil, err
	}
	// ⤵️ 10.3.2: 若 adapter 有 SetSkillManager 方法，注入 skillManager
	// ⤵️ 10.3.2: 设置 skillManager 的 skillnet_install_complete_hook
	jw.adapter = a
	logger.Info(logComponent).
		Str("sdk", adapter.ResolveSDKChoice()).
		Str("mode", mode).
		Msg("JiuWenClaw adapter 已初始化")
	return a, nil
}

// adapterModeForRequest 从请求参数中提取 adapter mode。
func (jw *JiuWenClaw) adapterModeForRequest(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if modeVal, ok := params["mode"]; ok {
		if modeStr, ok := modeVal.(string); ok && modeStr != "" {
			parts := strings.SplitN(modeStr, ".", 2)
			return parts[0]
		}
	}
	return "agent"
}

// extractSessionID 从请求中提取 sessionID 字符串。
func (jw *JiuWenClaw) extractSessionID(request *schema.AgentRequest) string {
	if request.SessionID != nil {
		return *request.SessionID
	}
	return ""
}

// extractQuery 从请求参数中提取 query 字段。
func (jw *JiuWenClaw) extractQuery(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if q, ok := params["query"]; ok {
		if qStr, ok := q.(string); ok {
			return qStr
		}
	}
	return ""
}

// extractResponseContent 从响应中提取 content。
func (jw *JiuWenClaw) extractResponseContent(resp *schema.AgentResponse) string {
	if resp.Payload == nil {
		return ""
	}
	if content, ok := resp.Payload["content"]; ok {
		if cStr, ok := content.(string); ok {
			return cStr
		}
	}
	return ""
}

// extractIntent 从请求参数中提取 intent（默认 "cancel"）。
func (jw *JiuWenClaw) extractIntent(request *schema.AgentRequest) string {
	params := parseRequestParams(request)
	if intent, ok := params["intent"]; ok {
		if intentStr, ok := intent.(string); ok && intentStr != "" {
			return intentStr
		}
	}
	return "cancel"
}

// extractChunkContent 从 chunk payload 中提取 content。
func extractChunkContent(payload map[string]any) string {
	if content, ok := payload["content"]; ok {
		if cStr, ok := content.(string); ok {
			return cStr
		}
	}
	return ""
}

// shouldRecordHistory 判断 event_type 是否需要记录到 history。
func shouldRecordHistory(eventType string) bool {
	return strings.HasPrefix(eventType, "chat.")
}
```

- [ ] **Step 2: 运行编译确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/runtime/`
Expected: 编译成功

- [ ] **Step 3: 重写 jiowenclaw_test.go — 更新测试**

更新测试使用 fakeAdapter mock 来验证分流逻辑。保留原有基本测试并新增：

```go
package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// fakeAdapter AgentAdapter mock，用于 JiuWenClaw 测试。
type fakeAdapter struct {
	mu             sync.Mutex
	createErr      error
	processResp    *schema.AgentResponse
	processErr     error
	streamCh       <-chan *schema.AgentResponseChunk
	streamErr      error
	interruptResp  *schema.AgentResponse
	interruptErr   error
	heartbeatResp  *schema.AgentResponse
	heartbeatErr   error
	userAnswerResp *schema.AgentResponse
	userAnswerErr  error
	instanceCreated bool
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{
		processResp:   schema.NewAgentResponse("fake", "fake", schema.WithResponseOK(true), schema.WithPayload(map[string]any{"content": "mock response"})),
		interruptResp: schema.NewAgentResponse("fake", "fake", schema.WithResponseOK(true)),
	}
}

func (f *fakeAdapter) CreateInstance(_ context.Context, _ map[string]any, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instanceCreated = true
	return f.createErr
}
func (f *fakeAdapter) ReloadAgentConfig(_ context.Context, _, _ map[string]any) error { return nil }
func (f *fakeAdapter) ProcessMessageImpl(_ context.Context, req *schema.AgentRequest, _ map[string]any) (*schema.AgentResponse, error) {
	return f.processResp, f.processErr
}
func (f *fakeAdapter) ProcessMessageStreamImpl(_ context.Context, req *schema.AgentRequest, _ map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	return f.streamCh, f.streamErr
}
func (f *fakeAdapter) ProcessInterrupt(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.interruptResp, f.interruptErr
}
func (f *fakeAdapter) HandleUserAnswer(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.userAnswerResp, f.userAnswerErr
}
func (f *fakeAdapter) HandleHeartbeat(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.heartbeatResp, f.heartbeatErr
}
func (f *fakeAdapter) Cleanup() error { return nil }

// --- 基本测试 ---

func TestNewJiuWenClaw(t *testing.T) {
	jw := NewJiuWenClaw()
	require.NotNil(t, jw)
	require.NotNil(t, jw.sessionManager)
}

func TestJiuWenClaw_ensureAdapter_幂等(t *testing.T) {
	jw := NewJiuWenClaw()
	a1, err := jw.ensureAdapter("agent")
	require.NoError(t, err)
	require.NotNil(t, a1)
	a2, err := jw.ensureAdapter("agent")
	require.NoError(t, err)
	assert.Equal(t, a1, a2, "ensureAdapter 应幂等返回同一 adapter")
}

func TestJiuWenClaw_ProcessMessage_cancel分支(t *testing.T) {
	jw := NewJiuWenClaw()
	// 注入 fakeAdapter
	fa := newFakeAdapter()
	jw.adapter = fa

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatCancel, nil)
	resp, err := jw.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestJiuWenClaw_ProcessMessage_heartbeat分支(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	fa.heartbeatResp = schema.NewAgentResponse("req-1", "web", schema.WithResponseOK(true), schema.WithPayload(map[string]any{"event_type": "heartbeat"}))
	jw.adapter = fa

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, nil)
	resp, err := jw.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
	// 应短路返回 heartbeat 响应
	assert.Equal(t, "heartbeat", resp.Payload["event_type"])
}

func TestJiuWenClaw_ProcessInterrupt_intent分支(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa

	tests := []struct {
		name   string
		intent string
	}{
		{"pause", "pause"},
		{"resume", "resume"},
		{"supplement", "supplement"},
		{"cancel", "cancel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{"intent": tc.intent})
			req := schema.NewAgentRequest("req", "web", schema.ReqMethodChatCancel, params)
			resp, err := jw.ProcessInterrupt(context.Background(), req)
			require.NoError(t, err)
			assert.True(t, resp.OK)
		})
	}
}

func TestJiuWenClaw_Cleanup(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa
	err := jw.Cleanup()
	require.NoError(t, err)
	assert.Nil(t, jw.adapter)
}

func TestJiuWenClaw_CancelInflightWork(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa
	err := jw.CancelInflightWork()
	require.NoError(t, err)
}

func TestJiuWenClaw_GetContextUsage(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.GetContextUsage("sess-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result["usage"])
}

func TestJiuWenClaw_CompressContext(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.CompressContext("sess-1")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
}

func TestJiuWenClaw_GenerateRecap(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.GenerateRecap("sess-1")
	require.NoError(t, err)
	assert.Equal(t, "", result["recap"])
}
```

- [ ] **Step 4: 运行全部 runtime 测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/jiowenclaw.go internal/swarm/server/runtime/jiowenclaw_test.go
git commit -m "feat(runtime): implement JiuWenClaw facade with real logic (level 0+1)"
```

---

### Task 5: doc.go 更新 + 最终验证

**Files:**
- Modify: `internal/swarm/server/runtime/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

```go
// Package runtime 提供 AgentServer 运行时管理组件。
//
// 包含 JiuWenClaw（Agent 门面）、SessionManager（LIFO 会话任务队列）、
// AgentManager（Agent 实例管理器）等运行时组件，
// 负责 Agent 实例的并发执行控制、任务调度和请求路由。
//
// 文件目录：
//
//	runtime/
//	├── doc.go                # 包文档
//	├── jiowenclaw.go         # JiuWenClaw Agent 门面（层级 0+1 已实现，层级 2-4 ⤵️）
//	├── build_user_prompt.go  # BuildUserPrompt 用户 prompt 包装
//	├── build_inputs.go       # BuildInputs adapter 输入构建
//	├── session_history.go    # 会话历史持久化（history.json 读写）
//	├── session_manager.go    # SessionManager（LIFO 会话队列）
//	└── agent_manager.go      # AgentManager Agent 实例管理器（stub，10.3.12）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/
package runtime
```

- [ ] **Step 2: 运行完整包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -v -count=1`
Expected: PASS

- [ ] **Step 3: 运行编译检查（确认没有破坏其他包）**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/doc.go
git commit -m "docs(runtime): update doc.go with new file directory"
```

---

## 自查

### Spec 覆盖度

| Spec 章节 | 对应 Task |
|-----------|----------|
| 2. 层级 0：结构体改造 | Task 4 Step 1 |
| 3.1 buildUserPrompt | Task 1 |
| 3.2 buildInputs | Task 3 |
| 3.3 appendHistoryRecord | Task 2 |
| 4.1 ensureAdapter | Task 4 Step 1 (ensureAdapter) |
| 4.2 CreateInstance | Task 4 Step 1 (CreateInstance) |
| 4.3 ProcessMessage | Task 4 Step 1 (ProcessMessage) |
| 4.4 ProcessMessageStream | Task 4 Step 1 (ProcessMessageStream) |
| 4.5 ProcessInterrupt | Task 4 Step 1 (ProcessInterrupt) |
| 4.6 CancelInflightWork | Task 4 Step 1 (CancelInflightWork) |
| 4.7 ReloadAgentConfig | Task 4 Step 1 (ReloadAgentConfig) |
| 4.8 Cleanup | Task 4 Step 1 (Cleanup) |
| 4.9 辅助方法 stub | Task 4 Step 1 (GetContextUsage etc.) |
| 5. 层级 2-4 stub 标注 | Task 4 Step 1 (⤵️ 注释) |
| 6. 文件清单 | Task 5 (doc.go) |
| 9. 测试策略 | Task 1-4 tests |

### Placeholder 扫描

- 无 "TBD"/"TODO"/"implement later"/"fill in details" — 所有 ⤵️ 标注均为明确的回填标记
- 每个 Step 都有具体代码

### 类型一致性

- `BuildUserPrompt` 签名在 Task 1 定义，Task 3 引用一致
- `AppendHistoryRecord` 签名在 Task 2 定义，Task 4 引用一致
- `fakeAdapter` 满足 `AgentAdapter` 接口（8 个方法全部实现）
- `parseRequestParams` 在 Task 3 定义，Task 4 引用一致

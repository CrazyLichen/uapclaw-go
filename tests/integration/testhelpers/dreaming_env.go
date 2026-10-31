//go:build integration

package testhelpers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingEnv dreaming 集成测试的隔离环境。
//
// 在 AdapterTestEnv 之上补充 dreaming 专用的会话播种与断言能力：
//   - SessionsDir 指向 <DataDir>/agent/sessions（与 workspace.AgentSessionsDir 一致）
//   - PlantSession / AppendRounds / Touch 提供确定性的 history.json 与 mtime
//   - QueueJSON / QueueError 预置 extractViaLLM 的模型响应
//   - UserPrompts 抽取每次 Invoke 的 user 文本，用于增量窗口断言
type DreamingEnv struct {
	// AdapterTestEnv 隔离环境（数据目录 + mock provider 注册 + 路径缓存重置）
	*AdapterTestEnv
	// SessionsDir 会话根目录
	SessionsDir string
	// Mock 注册到 ClientRegistry 的 mock 客户端（响应队列读写）
	Mock *mockllm.MockModelClient
}

// historyEntry history.json 中的一条对话事件（测试造数用）。
//
// 不复用 swarmdreaming.HistoryEvent：后者无 omitempty，会把 user 消息写成
// "event_type":""，与真实 history.json 形状不符，反而掩盖后续变更。
type historyEntry struct {
	// Role 消息角色
	Role string `json:"role"`
	// Content 消息内容
	Content string `json:"content"`
	// EventType 事件类型（仅 assistant 终态消息携带）
	EventType string `json:"event_type,omitempty"`
}

// sessionMetadata metadata.json 的测试造数结构。
type sessionMetadata struct {
	// Mode 会话模式（如 agent.plan / code.normal）
	Mode string `json:"mode"`
	// SessionID 会话标识
	SessionID string `json:"session_id"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int64 `json:"created_at"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDreamingEnv 构建隔离环境并写入指向 mock provider 的 config.yaml。
//
// 必须先注册 mock provider 再写 config.yaml（YAML 中 client_provider 引用注册名），
// 该顺序由 NewAdapterTestEnv 保证。
func NewDreamingEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *DreamingEnv {
	t.Helper()

	base := NewAdapterTestEnv(t, mock, prefix)
	base.WriteConfigYAML(t, "")

	env := &DreamingEnv{
		AdapterTestEnv: base,
		SessionsDir:    filepath.Join(base.DataDir, "agent", "sessions"),
		Mock:           mock,
	}
	if err := os.MkdirAll(env.SessionsDir, 0o755); err != nil {
		t.Fatalf("创建 sessions 目录失败: %v", err)
	}
	return env
}

// OutputDir 返回 dreaming 输出目录（agent 模式为 memory，code 模式为 coding_memory）。
func (e *DreamingEnv) OutputDir(name string) string {
	return filepath.Join(e.DataDir, "agent", "workspace", name)
}

// Reset 清空 mock 的响应队列与调用计数。
//
// BaseIntegrationSuite 在 SetupSuite 创建 MockLLM，同一套件的多个用例共享该实例，
// 因此每个用例开头必须调用本方法，避免响应队列与计数串味。
func (e *DreamingEnv) Reset() {
	e.Mock.SetResponses()
}

// PlantSession 在 SessionsDir 下创建会话目录：history.json + metadata.json。
// rounds 为对话轮次数，第 i 轮的用户消息为「第i个问题」、助手消息为「这是第i个回答」。
func (e *DreamingEnv) PlantSession(t *testing.T, sid, mode string, rounds int) {
	t.Helper()

	dir := filepath.Join(e.SessionsDir, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建会话目录 %s 失败: %v", sid, err)
	}

	entries := make([]historyEntry, 0, rounds*2)
	for i := 1; i <= rounds; i++ {
		entries = append(entries, historyEntry{Role: "user", Content: fmt.Sprintf("第%d个问题", i)})
		entries = append(entries, historyEntry{
			Role:      "assistant",
			Content:   fmt.Sprintf("这是第%d个回答", i),
			EventType: "chat.final",
		})
	}
	e.writeHistory(t, filepath.Join(dir, "history.json"), entries)
	e.writeMetadata(t, filepath.Join(dir, "metadata.json"), sessionMetadata{
		Mode:      mode,
		SessionID: sid,
		CreatedAt: time.Now().Unix(),
	})
}

// AppendRounds 向已有会话追加 [from, to] 闭区间的新轮次并刷新 mtime。
func (e *DreamingEnv) AppendRounds(t *testing.T, sid string, from, to int) {
	t.Helper()

	path := filepath.Join(e.SessionsDir, sid, "history.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", sid, err)
	}
	var entries []historyEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("解析 %s 失败: %v", sid, err)
	}
	for i := from; i <= to; i++ {
		entries = append(entries, historyEntry{Role: "user", Content: fmt.Sprintf("第%d个问题", i)})
		entries = append(entries, historyEntry{
			Role:      "assistant",
			Content:   fmt.Sprintf("这是第%d个回答", i),
			EventType: "chat.final",
		})
	}
	e.writeHistory(t, path, entries)
}

// Touch 把会话的 history.json 与目录 mtime 设为 now+delta（delta 可为负）。
//
// 禁用 time.Sleep 造增量：文件系统 mtime 精度不稳定，显式 Chtimes 才能确定性。
func (e *DreamingEnv) Touch(t *testing.T, sid string, delta time.Duration) {
	t.Helper()

	ts := time.Now().Add(delta)
	dir := filepath.Join(e.SessionsDir, sid)
	if err := os.Chtimes(filepath.Join(dir, "history.json"), ts, ts); err != nil {
		t.Fatalf("设置 %s history.json mtime 失败: %v", sid, err)
	}
	if err := os.Chtimes(dir, ts, ts); err != nil {
		t.Fatalf("设置 %s 目录 mtime 失败: %v", sid, err)
	}
}

// QueueJSON 追加一条 ```json 围栏响应，内容为 [{title, content}, ...]。
//
// 用围栏形式命中 extractViaLLM 的 ```json 正则分支，顺带覆盖该分支。
func (e *DreamingEnv) QueueJSON(t *testing.T, items []map[string]string) {
	t.Helper()

	payload := make([]map[string]string, 0, len(items))
	for _, it := range items {
		payload = append(payload, map[string]string{"title": it["title"], "content": it["content"]})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化知识条目失败: %v", err)
	}
	e.Mock.AddTextResponse("```json\n" + string(body) + "\n```")
}

// QueueError 追加一条错误响应，用于验证 LLM 报错时的落盘语义。
func (e *DreamingEnv) QueueError(err error) {
	e.Mock.AddErrorResponse(err)
}

// UserPrompts 返回每次 Invoke 调用中 user 消息的文本（按调用顺序）。
func (e *DreamingEnv) UserPrompts() []string {
	var out []string
	total := e.Mock.InvokeCallCount()
	for i := 0; i < total; i++ {
		rec := e.Mock.GetInvokeCall(i)
		if rec == nil {
			continue
		}
		var parts []string
		for _, m := range rec.Messages.Messages() {
			if m.GetRole() == llmschema.RoleTypeUser {
				parts = append(parts, m.GetContent().Text())
			}
		}
		out = append(out, strings.Join(parts, "\n"))
	}
	return out
}

// ReadCheckpoint 读取 <outputDir>/.dreams/ingestion-checkpoint.json。
func (e *DreamingEnv) ReadCheckpoint(t *testing.T, outputDir string) swarmdreaming.CheckpointData {
	t.Helper()

	path := filepath.Join(outputDir, ".dreams", "ingestion-checkpoint.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 checkpoint 失败: %v", err)
	}
	var cp swarmdreaming.CheckpointData
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatalf("解析 checkpoint 失败: %v", err)
	}
	return cp
}

// ReadDreamingMD 读取 <outputDir>/DREAMING.md，不存在时返回空串。
func (e *DreamingEnv) ReadDreamingMD(outputDir string) string {
	raw, err := os.ReadFile(filepath.Join(outputDir, "DREAMING.md"))
	if err != nil {
		return ""
	}
	return string(raw)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeHistory 以 UTF-8 JSON 写入 history.json。
func (e *DreamingEnv) writeHistory(t *testing.T, path string, entries []historyEntry) {
	t.Helper()

	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("序列化 %s 失败: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}

// writeMetadata 以 UTF-8 JSON 写入 metadata.json。
func (e *DreamingEnv) writeMetadata(t *testing.T, path string, meta sessionMetadata) {
	t.Helper()

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("序列化 %s 失败: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}

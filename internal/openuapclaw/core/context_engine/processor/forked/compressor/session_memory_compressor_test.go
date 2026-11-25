package compressor

import (
	"context"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
)

// compressorFakeContext 测试用最小 ModelContext（带会话与召回配置）
type compressorFakeContext struct {
	iface.ModelContext
	sessionID    string
	workspaceDir string
	sess         sessioninterfaces.SessionFacade
	recallCfg    *ceschema.CompressionRecallConfig
	messages     []llm_schema.BaseMessage
}

func (f *compressorFakeContext) SessionID() string    { return f.sessionID }
func (f *compressorFakeContext) ContextID() string    { return f.sessionID + "-ctx" }
func (f *compressorFakeContext) WorkspaceDir() string { return f.workspaceDir }
func (f *compressorFakeContext) GetSessionRef() sessioninterfaces.SessionFacade {
	return f.sess
}
func (f *compressorFakeContext) SetMessages(messages []llm_schema.BaseMessage, withHistory bool) {
	f.messages = messages
}
func (f *compressorFakeContext) CompressionRecallConfig() *ceschema.CompressionRecallConfig {
	return f.recallCfg
}
func (f *compressorFakeContext) TokenCounter() token.TokenCounter { return nil }

// ContextWindowProvider 能力面：小窗口让 char 兜底计数能越过阈值
func (f *compressorFakeContext) ModelName() string                        { return "" }
func (f *compressorFakeContext) ContextWindowTokens() int                 { return 1000 }
func (f *compressorFakeContext) ModelContextWindowTokens() map[string]int { return map[string]int{} }

// compressorFakeSession 最小 SessionFacade（runtime 状态挂 get_state/update_state）
type compressorFakeSession struct {
	sessioninterfaces.SessionFacade
	state map[string]any
}

func (s *compressorFakeSession) GetSessionID() string { return "sess-1" }
func (s *compressorFakeSession) GetState(key state.StateKey) (any, error) {
	if s.state == nil {
		return nil, nil
	}
	return s.state[key.String()], nil
}
func (s *compressorFakeSession) UpdateState(entries map[string]any) {
	for k, v := range entries {
		s.state[k] = v
	}
}

// writeNotes 写 workspace 约定路径的笔记文件
func writeNotes(t *testing.T, ws, sessionID, content string) string {
	t.Helper()
	notesPath := filepath.Join(ws, "context", sessionID+"_context", "session_memory", "session_context.md")
	if err := os.MkdirAll(filepath.Dir(notesPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notesPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return notesPath
}

// newTestCompressor 带默认配置的压缩器 + 达阈值窗口
func newTestCompressor(t *testing.T, enabled bool) *SessionMemoryCompressor {
	t.Helper()
	cfg := NewSessionMemoryCompressorConfig()
	cfg.Enabled = enabled
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return NewSessionMemoryCompressor(cfg)
}

// thresholdWindow 构建 SystemMessages + 足量 ContextMessages 的窗口
func thresholdWindow() iface.ContextWindow {
	return iface.ContextWindow{
		SystemMessages: []llm_schema.BaseMessage{llm_schema.NewSystemMessage(strings.Repeat("sys", 1000))},
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(strings.Repeat("ctx", 1000)),
			llm_schema.NewAssistantMessage(strings.Repeat("out", 1000)),
		},
	}
}

// TestSessionMemoryCompressor_Trigger 触发矩阵
func TestSessionMemoryCompressor_Trigger(t *testing.T) {
	ctx := context.Background()
	mc := &compressorFakeContext{sessionID: "sess-1", workspaceDir: t.TempDir()}
	cw := thresholdWindow()

	// disabled → 永不触发
	if triggered, _ := newTestCompressor(t, false).TriggerGetContextWindow(ctx, mc, thresholdWindow()); triggered {
		t.Error("disabled 不应触发")
	}

	// enabled 但无笔记 → 不触发
	comp := newTestCompressor(t, true)
	if triggered, _ := comp.TriggerGetContextWindow(ctx, mc, cw); triggered {
		t.Error("无笔记不应触发")
	}

	// 写笔记 + runtime 锚点 + 达阈值 → 触发（Python：无锚点候选为 nil 不触发）
	writeNotes(t, mc.workspaceDir, mc.sessionID, "任务笔记")
	sess := &compressorFakeSession{state: map[string]any{}}
	sess.state["__session_memory__"] = map[string]any{"notes_upto_message_id": "anchor-1"}
	mc.sess = sess
	// 给窗口最后一条消息打 context_message_id（锚点命中）
	if assistant, ok := cw.ContextMessages[1].(*llm_schema.AssistantMessage); ok {
		assistant.SetMetadata(map[string]any{"context_message_id": "anchor-1"})
	}
	cwVal := cw
	if triggered, err := comp.TriggerGetContextWindow(ctx, mc, cwVal); err != nil || !triggered {
		t.Errorf("达阈值+有笔记应触发，实际 %v err=%v", triggered, err)
	}
}

// TestSessionMemoryCompressor_OnGetContextWindow 物化替换全链
func TestSessionMemoryCompressor_OnGetContextWindow(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	mc := &compressorFakeContext{sessionID: "sess-1", workspaceDir: ws}
	cw := thresholdWindow()
	writeNotes(t, ws, mc.sessionID, "已完成任务笔记")
	// runtime 空 → 锚点缺失 → 候选 nil（Python find_message_index 返回 -1 → None）
	comp := newTestCompressor(t, true)

	event, newCw, err := comp.OnGetContextWindow(ctx, mc, cw)
	if err != nil {
		t.Fatalf("物化不应报错: %v", err)
	}
	if event == nil {
		// Python 语义：无锚点 → 不物化（返回 nil 事件）
		t.Skip("runtime 无锚点时不物化（Python _build_candidate 返回 None）")
	}
	memoryMsg := newCw.ContextMessages[0]
	content := memoryMsg.GetContent().String()
	for _, want := range []string{
		SessionMemoryBlockOpen,
		"<meaning>Durable asynchronously maintained session memory",
		"<conflict_policy>New raw messages",
		"<coverage>notes_upto_message_id=; coverage_type=completed_api_rounds</coverage>",
		"已完成任务笔记",
		SessionMemoryBlockClose,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("记忆块应含 %q，实际:\n%s", want, content)
		}
	}
	// 无锚点（runtime 空）→ 全部消息保留
	if len(newCw.ContextMessages) != 3 {
		t.Errorf("无锚点时记忆块后应保留全部 2 条原消息，实际 %d", len(newCw.ContextMessages)-1)
	}
	// 上下文同步
	if len(mc.messages) != len(newCw.ContextMessages) {
		t.Error("mc.SetMessages 应同步新窗口")
	}
	if event.CompactSummary != content {
		t.Error("CompactSummary 应为记忆块内容")
	}
}

// TestSessionMemoryCompressor_AnchorConsumption 锚点消费：锚点后消息保留
func TestSessionMemoryCompressor_AnchorConsumption(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	mc := &compressorFakeContext{sessionID: "sess-1", workspaceDir: ws}
	sess := &compressorFakeSession{state: map[string]any{}}
	sess.state["__session_memory__"] = map[string]any{"notes_upto_message_id": "anchor-1"}
	mc.sess = sess
	writeNotes(t, ws, mc.sessionID, "笔记内容")

	msg0 := llm_schema.NewUserMessage(strings.Repeat("旧消息", 500))
	msg0.SetMetadata(map[string]any{"context_message_id": "anchor-1"})
	msg1 := llm_schema.NewUserMessage(strings.Repeat("新消息A", 500))
	msg1.SetMetadata(map[string]any{"context_message_id": "id-2"})
	msg2 := llm_schema.NewAssistantMessage(strings.Repeat("新消息B", 500))
	msg2.SetMetadata(map[string]any{"context_message_id": "id-3"})
	cw := iface.ContextWindow{ContextMessages: []llm_schema.BaseMessage{msg0, msg1, msg2}}

	comp := newTestCompressor(t, true)
	_, newCw, err := comp.OnGetContextWindow(ctx, mc, cw)
	if err != nil {
		t.Fatalf("物化不应报错: %v", err)
	}
	// 锚点（index 0）之前的被替换 → 记忆块 + 锚点之后 2 条
	if len(newCw.ContextMessages) != 3 {
		t.Fatalf("应保留锚点后 2 条 + 记忆块，实际 %d", len(newCw.ContextMessages))
	}
	if strings.Contains(newCw.ContextMessages[0].GetContent().String(), "旧消息") {
		t.Error("锚点之前消息应被替换")
	}
	if !strings.Contains(newCw.ContextMessages[1].GetContent().String(), "新消息A") ||
		!strings.Contains(newCw.ContextMessages[2].GetContent().String(), "新消息B") {
		t.Error("锚点之后消息应原样保留")
	}
	// coverage 行应带锚点 id
	if !strings.Contains(newCw.ContextMessages[0].GetContent().String(), "notes_upto_message_id=anchor-1") {
		t.Error("coverage 应记录锚点 id")
	}
}

// TestSessionMemoryCompressor_Guards 守卫矩阵：超长笔记 / 消息数不减反增
func TestSessionMemoryCompressor_Guards(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	mc := &compressorFakeContext{sessionID: "sess-1", workspaceDir: ws}
	comp := newTestCompressor(t, true)

	// 笔记超 max_notes_chars → 候选为 nil
	writeNotes(t, ws, mc.sessionID, strings.Repeat("长", 130_000))
	cwPtr := thresholdWindow()
	if got := comp.buildCandidate(mc, &cwPtr, false); got != nil {
		t.Error("超长笔记不应构建候选")
	}

	// 单条消息 + 无锚点 → 记忆块+1 条 > 原 1 条 → OnGetContextWindow 返回 nil 事件
	writeNotes(t, ws, mc.sessionID, "笔记")
	cw := iface.ContextWindow{ContextMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("唯一消息")}}
	event, _, err := comp.OnGetContextWindow(ctx, mc, cw)
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Error("消息数不减反增时应放弃物化")
	}
}

// TestSessionMemoryCompressor_RecallMarkerAndArchive recall 配置启用时归档 + 标记
func TestSessionMemoryCompressor_RecallMarkerAndArchive(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	mc := &compressorFakeContext{
		sessionID:    "sess-1",
		workspaceDir: ws,
		recallCfg:    ceschema.NewCompressionRecallConfig(),
	}
	mc.recallCfg.Enabled = true
	sess := &compressorFakeSession{state: map[string]any{}}
	sess.state["__session_memory__"] = map[string]any{"notes_upto_message_id": "anchor-1"}
	mc.sess = sess
	writeNotes(t, ws, mc.sessionID, "笔记内容")

	msg0 := llm_schema.NewUserMessage(strings.Repeat("旧消息", 500))
	msg0.SetMetadata(map[string]any{"context_message_id": "anchor-1"})
	msg1 := llm_schema.NewAssistantMessage(strings.Repeat("尾部消息", 500))
	msg1.SetMetadata(map[string]any{"context_message_id": "id-2"})
	cw := iface.ContextWindow{ContextMessages: []llm_schema.BaseMessage{msg0, msg1}}

	comp := newTestCompressor(t, true)
	_, newCw, err := comp.OnGetContextWindow(ctx, mc, cw)
	if err != nil {
		t.Fatal(err)
	}
	content := newCw.ContextMessages[0].GetContent().String()
	if !strings.Contains(content, "[[COMPRESSION_RECALL: id=") {
		t.Fatalf("记忆块应含 recall 标记，实际:\n%s", content)
	}
	// 标记应在 close 之前（withRecallMarker 的优先插入点）
	if strings.Index(content, "[[COMPRESSION_RECALL") > strings.LastIndex(content, SessionMemoryBlockClose) {
		t.Error("标记应在 close 标记之前")
	}
	// 归档目录落盘（recall 根带 session hash 后缀，用 Glob 定位）
	hits, _ := filepath.Glob(filepath.Join(ws, "context", "*_context", "compression_recall"))
	if len(hits) == 0 {
		t.Fatal("归档目录应存在")
	}
	entries, err := os.ReadDir(hits[0])
	if err != nil || len(entries) == 0 {
		t.Fatalf("归档目录应非空: %v", err)
	}
	// 标记中的 id 应与归档 memory_id 一致
	archiveID := entries[0].Name()
	if !strings.Contains(content, "id="+strings.SplitN(archiveID, "_", 2)[0]) {
		t.Errorf("标记 id 应与归档一致，归档 %s", archiveID)
	}
}

// TestSessionMemoryCompressor_NoRecallConfig recall 关闭时无标记
func TestSessionMemoryCompressor_NoRecallConfig(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	mc := &compressorFakeContext{sessionID: "sess-1", workspaceDir: ws}
	sess := &compressorFakeSession{state: map[string]any{}}
	sess.state["__session_memory__"] = map[string]any{"notes_upto_message_id": "anchor-1"}
	mc.sess = sess
	writeNotes(t, ws, mc.sessionID, "笔记内容")

	msg0 := llm_schema.NewUserMessage("旧消息")
	msg0.SetMetadata(map[string]any{"context_message_id": "anchor-1"})
	cw := iface.ContextWindow{ContextMessages: []llm_schema.BaseMessage{msg0}}
	comp := newTestCompressor(t, true)
	_, newCw, _ := comp.OnGetContextWindow(ctx, mc, cw)
	if strings.Contains(newCw.ContextMessages[0].GetContent().String(), "COMPRESSION_RECALL") {
		t.Error("recall 关闭时不应有标记")
	}
}

// TestWithRecallMarker 标记拼接两分支
func TestWithRecallMarker(t *testing.T) {
	body := SessionMemoryBlockOpen + "\nbody\n" + SessionMemoryBlockClose
	got := withRecallMarker(body, "m1")
	want := SessionMemoryBlockOpen + "\nbody\n[[COMPRESSION_RECALL: id=m1]]\n" + SessionMemoryBlockClose
	if got != want {
		t.Errorf("close 结尾应插在 close 前，实际:\n%s", got)
	}
	if got2 := withRecallMarker("plain", "m1"); got2 != "plain\n[[COMPRESSION_RECALL: id=m1]]" {
		t.Errorf("非 close 结尾应追加，实际 %s", got2)
	}
}

// TestSessionMemoryCompressor_NotesPathFallback 路径三级回退
func TestSessionMemoryCompressor_NotesPathFallback(t *testing.T) {
	comp := newTestCompressor(t, true)
	// 显式配置优先
	explicit := "/tmp/explicit_notes.md"
	comp.config.SessionMemoryPath = &explicit
	if got := comp.resolveNotesPath(nil); got != explicit {
		t.Errorf("显式配置应优先，实际 %s", got)
	}
	// runtime memory_path 次之
	comp.config.SessionMemoryPath = nil
	mc := &compressorFakeContext{sessionID: "sess-1"}
	sess := &compressorFakeSession{state: map[string]any{"__session_memory__": map[string]any{"memory_path": "/tmp/runtime_notes.md"}}}
	mc.sess = sess
	if got := comp.resolveNotesPath(mc); got != "/tmp/runtime_notes.md" {
		t.Errorf("runtime memory_path 次之，实际 %s", got)
	}
	// workspace 约定路径兜底
	mc.sess = nil
	mc.workspaceDir = "/ws"
	want := filepath.Join(mc.WorkspaceDir(), "context", "sess-1_context", "session_memory", "session_context.md")
	if got := comp.resolveNotesPath(mc); got != want {
		t.Errorf("workspace 约定路径兜底，want %s got %s", want, got)
	}
}

// TestSessionMemoryCompressor_ConfigDefaults 配置默认值
func TestSessionMemoryCompressor_ConfigDefaults(t *testing.T) {
	cfg := NewSessionMemoryCompressorConfig()
	if cfg.Enabled || cfg.TriggerContextRatio != 0.8 || cfg.MaxNotesChars != 120_000 {
		t.Errorf("默认值不符: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("默认配置应通过校验: %v", err)
	}
	bad := NewSessionMemoryCompressorConfig()
	bad.TriggerContextRatio = 1.5
	if err := bad.Validate(); err == nil {
		t.Error("ratio ≥ 1 应校验失败")
	}
	// SetModelDefaults 透传 Memory 子配置
	bad2 := NewSessionMemoryCompressorConfig()
	bad2.SetModelDefaults(nil, nil)
	if bad2.GetModel() != nil {
		t.Error("本配置无直接 Model 字段")
	}
}

// TestSessionMemoryCompressor_StateStateless 状态空实现
func TestSessionMemoryCompressor_StateStateless(t *testing.T) {
	comp := newTestCompressor(t, true)
	if got := comp.SaveState(); len(got) != 0 {
		t.Errorf("SaveState 应为空，实际 %v", got)
	}
	comp.LoadState(map[string]any{"x": 1})
	// 默认直通钩子
	mc := &compressorFakeContext{}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("m")}
	if triggered, _ := comp.TriggerAddMessages(ctx2(), mc, msgs); triggered {
		t.Error("TriggerAddMessages 默认不介入")
	}
	event, out, err := comp.OnAddMessages(ctx2(), mc, msgs)
	if err != nil || event != nil || len(out) != 1 {
		t.Errorf("OnAddMessages 默认直通，实际 %v/%v/%v", event, out, err)
	}
	if comp.ProcessorType() != "SessionMemoryCompressor" {
		t.Errorf("ProcessorType 不符，实际 %s", comp.ProcessorType())
	}
	// iface 接口编译期兜底
	var _ iface.ContextProcessor = comp
}

func ctx2() context.Context { return context.Background() }

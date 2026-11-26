package recall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// archiveFakeContext 测试用最小 ModelContext
type archiveFakeContext struct {
	iface.ModelContext
	sessionID    string
	contextID    string
	workspaceDir string
}

func (f *archiveFakeContext) SessionID() string    { return f.sessionID }
func (f *archiveFakeContext) ContextID() string    { return f.contextID }
func (f *archiveFakeContext) WorkspaceDir() string { return f.workspaceDir }

// TestSplitText_ASCII 单块内不切分
func TestSplitText_ASCII(t *testing.T) {
	got := splitText("hello world", 3000, 300)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("短文本应单块，实际 %v", got)
	}
	if got := splitText("", 3000, 300); got != nil {
		t.Errorf("空文本应返回 nil，实际 %v", got)
	}
}

// TestSplitText_MultiChunk 超窗口切分 + 重叠
func TestSplitText_MultiChunk(t *testing.T) {
	// chunk_size=10 token → 40 字节；内容 100 字节
	text := strings.Repeat("abcdefghij", 10) // 100 字节
	got := splitText(text, 10, 5)            // byteSize=40, overlap=20
	if len(got) < 3 {
		t.Errorf("100 字节/40 窗口应切成 ≥3 块，实际 %d", len(got))
	}
	// 相邻块应有重叠（第二块首 rune 出现在第一块尾部 overlap 区间内）
	overlapZone := got[0][max(0, len([]rune(got[0]))-10):]
	if !strings.Contains(overlapZone, string([]rune(got[1])[0])) {
		t.Errorf("相邻块应带重叠，chunk0 尾部=%q chunk2 首字符=%q", overlapZone, string([]rune(got[1])[0]))
	}
}

// TestSplitText_Multibyte 多字节字符不切碎
func TestSplitText_Multibyte(t *testing.T) {
	text := strings.Repeat("中文字符", 30) // 120 字节
	got := splitText(text, 10, 5)
	for _, chunk := range got {
		// Python str 按码点索引，切分不会产生碎字节；Go 侧 rune 等价实现须同样合法
		if !utf8.ValidString(chunk) {
			t.Errorf("多字节字符被切碎: %q", chunk[:12])
		}
	}
}

// TestQuerySlug_And_TextSlug slug 生成
func TestQuerySlug_And_TextSlug(t *testing.T) {
	if got := querySlug("hello world foo bar"); got != "hello_worl" {
		t.Errorf("查询 slug 应截断 10 字符，实际 %s", got)
	}
	if got := querySlug("   "); got != "no-query" {
		t.Errorf("空查询应回退 no-query，实际 %s", got)
	}
	if got := textSlug(`a/b:c*d`, 40, "x"); got != "a-b-c-d" {
		t.Errorf("非法字符应替换为 -，实际 %s", got)
	}
}

// TestChunkSlug 首个有效行 + 裸角色头跳过
func TestChunkSlug(t *testing.T) {
	if got := chunkSlug("## User\n\nhello content"); got != "hello_content" {
		t.Errorf("应跳过裸角色头取正文行（Python textSlug 空白折叠为 _），实际 %s", got)
	}
	if got := chunkSlug("## Tool Call: read_file"); got != "Tool_Call-_read_file" {
		t.Errorf("带载荷的头应保留（: 替换为 -），实际 %s", got)
	}
	if got := chunkSlug("   \n  "); got != "empty" {
		t.Errorf("空正文应回退 empty，实际 %s", got)
	}
}

// TestGroupTurns 轮分组：真实用户消息开新轮
func TestGroupTurns(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一问"),
		llm_schema.NewAssistantMessage("第一答"),
		llm_schema.NewUserMessage("<system-reminder>内部消息</system-reminder>"),
		llm_schema.NewAssistantMessage("内部回复"),
		llm_schema.NewUserMessage("第二问"),
		llm_schema.NewAssistantMessage("第二答"),
	}
	turns := groupTurns(messages, "fallback")
	if len(turns) != 2 {
		t.Fatalf("应分 2 轮（内部前缀消息不算真实用户轮），实际 %d", len(turns))
	}
	// 内部前缀消息不是真实用户轮 → 并入第一轮（Python _group_turns 仅真实用户消息开新轮）
	if turns[0].query != "第一问" || turns[0].answer != "第一答\n内部回复" {
		t.Errorf("第一轮内容不符: %q / %q", turns[0].query, turns[0].answer)
	}
	if turns[1].query != "第二问" || len(turns[1].messages) != 2 {
		t.Errorf("第二轮应含 2 条消息（第二问+第二答），实际 %d", len(turns[1].messages))
	}
}

// TestRenderTurn 渲染分节结构
func TestRenderTurn(t *testing.T) {
	assistant := llm_schema.NewAssistantMessage("回答内容")
	assistant.ReasoningContent = "思考过程"
	assistant.ToolCalls = []*llm_schema.ToolCall{{Name: "read_file", Arguments: `{"path":"a.txt"}`}}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		assistant,
		llm_schema.NewToolMessage("call-1", "工具结果"),
	}
	got := renderTurn(messages)
	for _, want := range []string{"## User\n问题", "## Assistant Reasoning\n思考过程", "## Assistant\n回答内容", "## Tool Call: read_file", "## Tool Result: call-1\n工具结果"} {
		if !strings.Contains(got, want) {
			t.Errorf("渲染应含 %q，实际:\n%s", want, got)
		}
	}
}

// TestArchiveCompressionMessages 端到端归档落盘与 manifest 断言
func TestArchiveCompressionMessages(t *testing.T) {
	ws := t.TempDir()
	mc := &archiveFakeContext{sessionID: "sess-1", contextID: "ctx-1", workspaceDir: ws}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询问题"),
		llm_schema.NewAssistantMessage("答案"),
	}
	archive, err := ArchiveCompressionMessages(mc, "SessionMemoryCompressor", messages, messages, nil, 3000, 300)
	if err != nil {
		t.Fatalf("归档不应报错: %v", err)
	}
	if archive.MemoryID == "" || archive.Path == "" {
		t.Fatal("MemoryID/Path 必填")
	}
	// 目录名带 query slug
	if !strings.HasSuffix(archive.Path, "_查询问题") && !strings.Contains(filepath.Base(archive.Path), "查询问题") {
		t.Errorf("目录名应带查询 slug，实际 %s", filepath.Base(archive.Path))
	}
	// manifest 字段
	data, err := os.ReadFile(filepath.Join(archive.Path, "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json 应存在: %v", err)
	}
	m := string(data)
	for _, want := range []string{`"schema_version": 1`, `"memory_id": "` + archive.MemoryID, `"processor_type": "SessionMemoryCompressor"`, `"source_message_count": 2`} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest 应含 %s，实际:\n%s", want, m)
		}
	}
	// chunk 落盘
	entries, err := os.ReadDir(filepath.Join(archive.Path, "chunks"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("chunks 目录应非空: %v", err)
	}
	// turns.jsonl
	turnsData, err := os.ReadFile(filepath.Join(archive.Path, "turns.jsonl"))
	if err != nil || !strings.Contains(string(turnsData), "查询问题") {
		t.Fatalf("turns.jsonl 应存在且含查询: %v", err)
	}
}

// TestArchiveCompressionMessages_Validation 参数与依赖校验
func TestArchiveCompressionMessages_Validation(t *testing.T) {
	mc := &archiveFakeContext{sessionID: "s", workspaceDir: t.TempDir()}
	if _, err := ArchiveCompressionMessages(mc, "x", nil, nil, nil, 300, 300); err == nil {
		t.Error("overlap >= size 应报错")
	}
	noWs := &archiveFakeContext{sessionID: "s"}
	if _, err := ArchiveCompressionMessages(noWs, "x", nil, []llm_schema.BaseMessage{llm_schema.NewUserMessage("q")}, nil, 3000, 300); err == nil {
		t.Error("无 workspace 应报错")
	}
	noSession := &archiveFakeContext{workspaceDir: t.TempDir()}
	if _, err := ArchiveCompressionMessages(noSession, "x", nil, []llm_schema.BaseMessage{llm_schema.NewUserMessage("q")}, nil, 3000, 300); err == nil {
		t.Error("无 session id 应报错")
	}
}

// TestValidateArchiveRoot_Symlink 符号链接拒绝
func TestValidateArchiveRoot_Symlink(t *testing.T) {
	ws := t.TempDir()
	sessionDir := filepath.Join(ws, "context", "link_session_context")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sessionDir, RecallDirName)
	if err := os.Symlink("/etc", link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if err := validateArchiveRoot(ws, link); err == nil {
		t.Error("符号链接路径应被拒绝")
	}
}

// TestDeleteCompressionArchive 删除校验 + 移除
func TestDeleteCompressionArchive(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, "20260101T000000Z00_memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := &CompressionArchive{MemoryID: "20260101T000000Z00", Path: dir}
	if err := DeleteCompressionArchive(archive); err != nil {
		t.Fatalf("合法删除不应报错: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("删除后目录应不存在")
	}

	// memory id 不匹配
	dir2 := filepath.Join(ws, "other_id_dir")
	os.MkdirAll(dir2, 0o755)
	if err := DeleteCompressionArchive(&CompressionArchive{MemoryID: "20260101T000000Z00", Path: dir2}); err == nil {
		t.Error("目录名与 memory_id 不匹配应报错")
	}

	// 逃逸路径
	if err := DeleteCompressionArchive(&CompressionArchive{MemoryID: "20260101T000000Z00", Path: "/etc"}); err == nil {
		t.Error("逃逸路径应报错")
	}
}

package recall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// retrieverFakeContext 测试用最小 ModelContext
type retrieverFakeContext struct {
	iface.ModelContext
	sessionID    string
	contextID    string
	workspaceDir string
}

func (f *retrieverFakeContext) SessionID() string    { return f.sessionID }
func (f *retrieverFakeContext) ContextID() string    { return f.contextID }
func (f *retrieverFakeContext) WorkspaceDir() string { return f.workspaceDir }

// buildTestArchive 构造一个真实归档（走 ArchiveCompressionMessages 全链）
func buildTestArchive(t *testing.T, ws, sessionID, query, answer string) *CompressionArchive {
	t.Helper()
	mc := &retrieverFakeContext{sessionID: sessionID, contextID: "ctx-1", workspaceDir: ws}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(query),
		llm_schema.NewAssistantMessage(answer),
	}
	archive, err := ArchiveCompressionMessages(mc, "SessionMemoryCompressor", messages, messages, nil, 3000, 300)
	if err != nil {
		t.Fatalf("构造归档失败: %v", err)
	}
	return archive
}

// TestRecallCompressedContext_SingleArchive 带 memory_id 的单归档召回（端到端）
func TestRecallCompressedContext_SingleArchive(t *testing.T) {
	ws := t.TempDir()
	archive := buildTestArchive(t, ws, "sess-1", "如何配置数据库连接", "使用 config.yaml 配置数据库连接串")

	got, err := RecallCompressedContext(ws, "sess-1", archive.MemoryID, "数据库连接")
	if err != nil {
		t.Fatalf("召回不应报错: %v", err)
	}
	if got["recall_root"] == nil {
		t.Error("结果应含 recall_root")
	}
	chunks := toChunkList(got["chunks"])
	if len(chunks) == 0 {
		t.Fatal("相关 chunk 应被召回")
	}
	if !strings.Contains(entryStr(chunks[0], "content"), "数据库连接") {
		t.Errorf("召回 chunk 应含查询相关内容，实际 %s", entryStr(chunks[0], "content"))
	}
	if got["truncated"] != false || got["returned_tokens"] == 0 {
		t.Errorf("小归档不应截断且应有 token 计数，实际 %v/%v", got["truncated"], got["returned_tokens"])
	}
	archives, _ := got["archives_in_session"].([]map[string]any)
	if len(archives) != 1 {
		t.Errorf("会话归档清单应含 1 条，实际 %v", archives)
	}
}

// TestRecallCompressedContext_AllArchives 无 memory_id 的全归档合并召回
func TestRecallCompressedContext_AllArchives(t *testing.T) {
	ws := t.TempDir()
	buildTestArchive(t, ws, "sess-1", "数据库连接配置", "config.yaml 中配置数据库")
	buildTestArchive(t, ws, "sess-1", "缓存策略讨论", "使用 Redis 做缓存")

	got, err := RecallCompressedContext(ws, "sess-1", "", "数据库")
	if err != nil {
		t.Fatalf("全归档召回不应报错: %v", err)
	}
	chunks := toChunkList(got["chunks"])
	if len(chunks) == 0 {
		t.Fatal("应召回相关 chunk")
	}
	// 归一化后主匹配归档的 chunk 排第一
	if entryStr(chunks[0], "memory_id") == "" {
		t.Error("单归档路径 chunk 应带 memory_id")
	}
	// raw_score 存在（keep_raw=True）
	if _, ok := chunks[0]["raw_score"]; !ok {
		t.Error("合并召回的 chunk 应带 raw_score")
	}
}

// TestRecallCompressedContext_Validation 入参校验矩阵
func TestRecallCompressedContext_Validation(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		ws, session, memory, query string
		want                       string
	}{
		{"", "s", "", "q", "requires a workspace directory"},
		{ws, "", "", "q", "requires a session"},
		{ws, "s", "../evil", "q", "memory_id is invalid"},
		{ws, "s", "", "  ", "query is required"},
		{ws, "s", "", "q", "archive was not found"},
		{ws, "s", "20260101T000000Z00_missing", "q", "archive was not found"},
	}
	for i, c := range cases {
		_, err := RecallCompressedContext(c.ws, c.session, c.memory, c.query)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("case %d 应报 %q，实际 %v", i, c.want, err)
		}
	}
}

// TestRecallCompressedContext_ForeignSession 跨会话归档：会话根隔离 → 未找到
func TestRecallCompressedContext_ForeignSession(t *testing.T) {
	ws := t.TempDir()
	foreign := buildTestArchive(t, ws, "other-session", "问题", "答案")
	// 会话根按 session 隔离（hash 目录），sess-1 的根下没有该归档 → 兜底"未找到"
	// （Python _raise_for_rejected_candidate 的 glob 在本会话根内为空）
	_, err := RecallCompressedContext(ws, "sess-1", foreign.MemoryID, "问题")
	if err == nil || !strings.Contains(err.Error(), "archive was not found in the current session") {
		t.Fatalf("跨会话请求应报未找到，实际 %v", err)
	}

	// 归属拒绝路径：本会话根内存在同名目录但 manifest 归属他者
	ownRoot := recallRoot(ws, "sess-1")
	foreignDir := filepath.Join(ownRoot, foreign.MemoryID+"_x")
	os.MkdirAll(foreignDir, 0o755)
	os.WriteFile(filepath.Join(foreignDir, "manifest.json"),
		[]byte(fmt.Sprintf(`{"memory_id":%q,"session_id":"other-session"}`, foreign.MemoryID)), 0o644)
	_, err = RecallCompressedContext(ws, "sess-1", foreign.MemoryID, "问题")
	if err == nil || !strings.Contains(err.Error(), "does not belong to the current session") {
		t.Errorf("manifest 归属他者应显式拒绝，实际 %v", err)
	}
}

// TestApplyBudget 预算截断
func TestApplyBudget(t *testing.T) {
	t.Setenv(MaxContentTokensEnv, "10")
	candidates := []map[string]any{
		{"chunk_id": "c1", "content": strings.Repeat("a", 40)},  // 10 token
		{"chunk_id": "c2", "content": strings.Repeat("b", 40)},  // 10 token
		{"chunk_id": "c3", "content": strings.Repeat("c", 100)}, // 25 token
	}
	chunks, truncated, returned := applyBudget(candidates)
	// 预算 10：第一块（10 token）收下后，后续块 10+10>10 全部丢弃
	if len(chunks) != 1 || truncated != true || returned != 10 {
		t.Errorf("预算 10 应收 1 块截断其余（token=10），实际 %d/%v/%d", len(chunks), truncated, returned)
	}
	// 无效环境变量回退默认
	t.Setenv(MaxContentTokensEnv, "not-a-number")
	if maxContentTokens() != DefaultMaxContentTokens {
		t.Error("非法环境变量应回退默认预算")
	}
	t.Setenv(MaxContentTokensEnv, "-5")
	if maxContentTokens() != DefaultMaxContentTokens {
		t.Error("非正数环境变量应回退默认预算")
	}
}

// TestResolveRelativeFile_PathSafety chunk 路径安全
func TestResolveRelativeFile_PathSafety(t *testing.T) {
	archivePath := t.TempDir()
	if _, err := resolveRelativeFile(archivePath, ""); err == nil {
		t.Error("空相对路径应报错")
	}
	if _, err := resolveRelativeFile(archivePath, "/etc/passwd"); err == nil {
		t.Error("绝对路径应报错")
	}
	if _, err := resolveRelativeFile(archivePath, "../../etc/passwd"); err == nil {
		t.Error("逃逸相对路径应报错")
	}
	if _, err := resolveRelativeFile(archivePath, "chunks/missing.md"); err == nil {
		t.Error("缺失文件应报错")
	}
	// 合法 chunk
	chunksDir := filepath.Join(archivePath, "chunks")
	os.MkdirAll(chunksDir, 0o755)
	os.WriteFile(filepath.Join(chunksDir, "c.md"), []byte("body"), 0o644)
	got, err := resolveRelativeFile(archivePath, "chunks/c.md")
	if err != nil {
		t.Errorf("合法 chunk 不应报错: %v", err)
	}
	if !strings.HasSuffix(got, "c.md") {
		t.Errorf("应解析到归档内文件，实际 %s", got)
	}
}

// TestEstimateTokens 字节/4 估算
func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens(strings.Repeat("a", 100)); got != 25 {
		t.Errorf("100 字节应估 25 token，实际 %d", got)
	}
	if got := estimateTokens(strings.Repeat("a", 101)); got != 26 {
		t.Errorf("101 字节应向上取整为 26，实际 %d", got)
	}
}

// TestRecallError_Message 错误类型消息
func TestRecallError_Message(t *testing.T) {
	err := newRecallError("boom")
	if err.Error() != "boom" {
		t.Errorf("错误消息不符，实际 %s", err.Error())
	}
	var _ error = err
	fmt.Sprint(err)
}

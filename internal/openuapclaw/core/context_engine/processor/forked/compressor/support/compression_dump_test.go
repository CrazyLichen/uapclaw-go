package support

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// fakeDumpProcessor 实现 DumpProcessorStats 窄接口
type fakeDumpProcessor struct{ tokensPerList int }

func (f *fakeDumpProcessor) ResolveContextMaxForDump(_ DumpContextIdentifiers, _ string) int {
	return 8000
}
func (f *fakeDumpProcessor) CountMessagesTokensForDump(_ DumpContextIdentifiers, _ []llm_schema.BaseMessage) int {
	return f.tokensPerList
}

// fakeDumpModelContext dump 用最小 ModelContext 桩
type fakeDumpModelContext struct {
	sessionID    string
	contextID    string
	workspaceDir string
}

func (f *fakeDumpModelContext) SessionID() string    { return f.sessionID }
func (f *fakeDumpModelContext) ContextID() string    { return f.contextID }
func (f *fakeDumpModelContext) WorkspaceDir() string { return f.workspaceDir }

// TestDumpCompressionArtifact_禁用时返回空 测试开关关闭直接返回
// Python: compression_dump.py dump_compression_artifact 首段
func TestDumpCompressionArtifact_禁用时返回空(t *testing.T) {
	proc := &fakeDumpProcessor{}
	input := &CompressionDumpInput{
		Processor:     proc,
		Context:       &fakeDumpModelContext{sessionID: "s1", contextID: "c1"},
		Config:        map[string]any{"enable_compression_dump": false},
		ProcessorType: "TestProcessor",
	}
	path := DumpCompressionArtifact(input)
	require.Empty(t, path)
}

// TestDumpCompressionArtifact_完整落盘 测试 payload 形状与文件命名
// Python: compression_dump.py dump_compression_artifact 主体 + _build_file_name
func TestDumpCompressionArtifact_完整落盘(t *testing.T) {
	dir := t.TempDir()
	proc := &fakeDumpProcessor{tokensPerList: 120}
	input := &CompressionDumpInput{
		Processor:        proc,
		Context:          &fakeDumpModelContext{sessionID: "s 1", contextID: "c/1", workspaceDir: dir},
		Config:           map[string]any{"enable_compression_dump": true},
		ProcessorType:    "TestProcessor",
		OriginalMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("old")},
		Span: CompressionSpanShape{
			PreservedPrefix:    []llm_schema.BaseMessage{llm_schema.NewUserMessage("keep")},
			MessagesToCompress: []llm_schema.BaseMessage{llm_schema.NewUserMessage("a"), llm_schema.NewUserMessage("b"), llm_schema.NewUserMessage("c")},
			ProtectedTail:      []llm_schema.BaseMessage{llm_schema.NewUserMessage("t1"), llm_schema.NewUserMessage("t2")},
		},
		Prompt:          "summarize",
		Request:         &CompressionRequest{Prompt: "summarize", ExcludeRecentMessages: 2},
		ResponseContent: "<state_snapshot>done</state_snapshot>",
		Summary:         "done",
		NewMessages:     []llm_schema.BaseMessage{llm_schema.NewUserMessage("<memory_block>done</memory_block>")},
		Usage:           map[string]any{"total_tokens": 42},
	}
	path := DumpCompressionArtifact(input)
	require.NotEmpty(t, path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	// 顶层键集合与 PY payload 一致
	for _, key := range []string{"processor_type", "session_id", "context_id", "trigger", "span",
		"compression_request", "compression_response", "context_after", "benefit", "timestamp"} {
		require.Contains(t, payload, key)
	}
	trigger := payload["trigger"].(map[string]any)
	require.Equal(t, float64(8000), trigger["context_max"])
	require.Equal(t, float64(120), trigger["target_tokens"])
	span := payload["span"].(map[string]any)
	require.Equal(t, float64(1), span["preserved_prefix_count"])
	require.Equal(t, float64(3), span["messages_to_compress_count"])
	require.Equal(t, float64(2), span["protected_tail_count"])
	benefit := payload["benefit"].(map[string]any)
	require.Equal(t, float64(120), benefit["tokens_before"])
	// 文件名：毫秒时间戳_处理器_会话.json
	base := filepath.Base(path)
	require.Regexp(t, regexp.MustCompile(`^\d+_TestProcessor_s-1\.json$`), base)
}

// TestResolveDumpDir_三级解析 测试配置 > 环境变量 > workspace 默认
// Python: compression_dump.py _resolve_dump_dir
func TestResolveDumpDir_三级解析(t *testing.T) {
	ws := t.TempDir()
	ctx := &fakeDumpModelContext{sessionID: "s1", workspaceDir: ws}
	// 1. 配置优先 + 模板展开
	dir := resolveDumpDir(ctx, map[string]any{
		"enable_compression_dump": true,
		"compression_dump_dir":    "/data/{session_id}_logs",
	})
	require.Equal(t, filepath.Join("/data", "s1_logs"), dir)
	// 2. 环境变量（模板同样展开）
	t.Setenv(CompressionDumpDirEnv, "/env/{context_id}/dumps")
	dir = resolveDumpDir(ctx, map[string]any{"enable_compression_dump": true})
	require.Equal(t, filepath.Join("/env", "unknown_context", "dumps"), dir)
	// 3. workspace 默认：{workspace}/context/{session_id}_context/compression_logs
	require.NoError(t, os.Unsetenv(CompressionDumpDirEnv))
	dir = resolveDumpDir(ctx, map[string]any{"enable_compression_dump": true})
	require.Equal(t, filepath.Join(ws, "context", "s1_context", "compression_logs"), dir)
	// workspace 为空 → 返回空串
	dir = resolveDumpDir(&fakeDumpModelContext{sessionID: "s1"}, map[string]any{"enable_compression_dump": true})
	require.Empty(t, dir)
}

// TestBuildBenefit 测试收益统计
// Python: compression_dump.py _build_benefit
func TestBuildBenefit(t *testing.T) {
	b := buildBenefit(100, 60)
	require.Equal(t, 100, b["tokens_before"])
	require.Equal(t, 60, b["tokens_after"])
	require.Equal(t, 40, b["reduction"])
	require.Equal(t, 0.4, b["reduction_ratio"])
	// tokens_before=0 → ratio 0
	b = buildBenefit(0, 0)
	require.Equal(t, 0.0, b["reduction_ratio"])
}

// TestSafeFilenamePart 测试文件名净化
// Python: compression_dump.py _safe_filename_part
func TestSafeFilenamePart(t *testing.T) {
	require.Equal(t, "a-b-c.d", safeFilenamePart("a b/c.d"))
	require.Equal(t, "unknown", safeFilenamePart("///"))
	require.Len(t, safeFilenamePart(strings.Repeat("x", 200)), 80)
}

var _ = context.Background

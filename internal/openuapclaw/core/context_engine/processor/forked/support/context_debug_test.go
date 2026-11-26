package support

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// debugFakeContext 测试用最小 ModelContext
type debugFakeContext struct {
	iface.ModelContext
	sessionID    string
	contextID    string
	workspaceDir string
}

func (f *debugFakeContext) SessionID() string    { return f.sessionID }
func (f *debugFakeContext) ContextID() string    { return f.contextID }
func (f *debugFakeContext) WorkspaceDir() string { return f.workspaceDir }

// TestWriteDebugRecord_Disabled 关闭时不落盘
func TestWriteDebugRecord_Disabled(t *testing.T) {
	if got := WriteDebugRecord(context.Background(), nil, "X", "threshold_check", false, t.TempDir(), nil); got != "" {
		t.Errorf("enabled=false 应返回空串，实际 %s", got)
	}
}

// TestWriteDebugRecord_ExplicitDir 显式目录落盘 + JSONL 字段断言
func TestWriteDebugRecord_ExplicitDir(t *testing.T) {
	dir := t.TempDir()
	mc := &debugFakeContext{sessionID: "s1", contextID: "c1"}
	got := WriteDebugRecord(context.Background(), mc, "SessionMemoryCompressor", "threshold_check", true, dir,
		map[string]any{"total_tokens": 100, "hit": false})
	if got == "" {
		t.Fatal("应返回落盘路径")
	}
	data, err := os.ReadFile(filepath.Join(dir, "SessionMemoryCompressor.jsonl"))
	if err != nil {
		t.Fatalf("应按处理器名落盘: %v", err)
	}
	line := string(data)
	for _, want := range []string{`"event":"threshold_check"`, `"processor":"SessionMemoryCompressor"`, `"session_id":"s1"`, `"context_id":"c1"`, `"total_tokens":100`, `"hit":false`} {
		if !strings.Contains(line, want) {
			t.Errorf("记录应含 %s，实际 %s", want, line)
		}
	}
	if !strings.HasSuffix(line, "\n") {
		t.Error("JSONL 应以换行结尾")
	}
}

// TestWriteDebugRecord_EnvFallback 环境变量兜底目录
func TestWriteDebugRecord_EnvFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ContextDebugDirEnv, dir)
	mc := &debugFakeContext{sessionID: "s1", contextID: "c1"}
	got := WriteDebugRecord(context.Background(), mc, "X", "e", true, "", nil)
	if got == "" {
		t.Fatal("环境变量兜底应落盘")
	}
	if filepath.Dir(got) != dir {
		t.Errorf("应落在环境变量目录，实际 %s", got)
	}
}

// TestWriteDebugRecord_WorkspaceFallback workspace 默认目录
func TestWriteDebugRecord_WorkspaceFallback(t *testing.T) {
	ws := t.TempDir()
	mc := &debugFakeContext{sessionID: "s1", contextID: "c1", workspaceDir: ws}
	got := WriteDebugRecord(context.Background(), mc, "X", "e", true, "", nil)
	want := filepath.Join(ws, "context", "s1_context", "context_debug")
	if filepath.Dir(got) != want {
		t.Errorf("应落在 workspace 默认目录，实际 %s", got)
	}
}

// TestDebugLogFileName_Sanitize 特殊字符替换
func TestDebugLogFileName_Sanitize(t *testing.T) {
	if got := debugLogFileName("a b/c"); got != "a-b-c.jsonl" {
		t.Errorf("应替换为 -，实际 %s", got)
	}
	if got := debugLogFileName("///"); got != "unknown.jsonl" {
		t.Errorf("全非法字符应回退 unknown，实际 %s", got)
	}
}

// TestExpandTemplate 模板占位展开
func TestExpandTemplate(t *testing.T) {
	mc := &debugFakeContext{sessionID: "s/1", contextID: "c 1"}
	got := expandTemplate("logs/{session_id}/{context_id}", mc)
	if got != filepath.Join("logs", "s-1", "c-1") {
		t.Errorf("模板应展开并消毒，实际 %s", got)
	}
	if expandTemplate("plain", mc) != "plain" {
		t.Error("无占位路径应原样返回")
	}
}

// TestWriteLLMRequestRecord 载荷记录固定文件名
func TestWriteLLMRequestRecord(t *testing.T) {
	dir := t.TempDir()
	mc := &debugFakeContext{sessionID: "s1", contextID: "c1"}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hi")}
	got := WriteLLMRequestRecord(context.Background(), mc, true, dir, "qwen", "OpenAI", "req-1", 1, msgs, nil, 1000, 1, 1, nil, nil)
	if filepath.Base(got) != "llm_request.jsonl" {
		t.Errorf("应固定 llm_request.jsonl 文件名，实际 %s", got)
	}
	data, _ := os.ReadFile(got)
	line := string(data)
	for _, want := range []string{`"model":"qwen"`, `"provider":"OpenAI"`, `"message_count":1`, `"context_window_tokens":1000`} {
		if !strings.Contains(line, want) {
			t.Errorf("载荷记录应含 %s，实际 %s", want, line)
		}
	}
}

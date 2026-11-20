package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
)

// TestInheritInvocationLogDir 继承语义（Python inherit_invocation_log_dir）
func TestInheritInvocationLogDir(t *testing.T) {
	d := NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("test-deep")))
	d.configMu.Lock()
	d.deepConfig = schema.NewDeepAgentConfig()
	d.configMu.Unlock()
	// 父目录为空：不覆盖
	d.InheritInvocationLogDir("")
	// 父目录非空且本 agent 未配置：继承
	d.InheritInvocationLogDir("/tmp/parent-logs")
	d.configMu.RLock()
	got := d.deepConfig.InvocationLogDir
	d.configMu.RUnlock()
	if got != "/tmp/parent-logs" {
		t.Fatalf("未继承父目录: %q", got)
	}
	// 本 agent 已有配置：不覆盖
	d.InheritInvocationLogDir("/tmp/other-logs")
	d.configMu.RLock()
	got = d.deepConfig.InvocationLogDir
	d.configMu.RUnlock()
	if got != "/tmp/parent-logs" {
		t.Fatalf("已有配置被覆盖: %q", got)
	}
}

// TestStartInvocationLogContext 作用域：配置后产生 JSONL、未配置零开销、构造失败告警不阻断
func TestStartInvocationLogContext(t *testing.T) {
	d := NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("test-deep")))
	// 未配置：ctx 原样、收尾 nil
	ctx, finish, err := d.startInvocationLogContext(context.Background(), nil)
	if err != nil || finish != nil {
		t.Fatalf("未配置应零开销: err=%v finish-nil=%v", err, finish == nil)
	}
	if ctx == nil {
		t.Fatal("ctx 不应为 nil")
	}
	// 配置目录：start 记录产生，finish 写 end 记录
	dir := t.TempDir()
	d.configMu.Lock()
	if d.deepConfig == nil {
		d.deepConfig = schema.NewDeepAgentConfig()
	}
	d.deepConfig.InvocationLogDir = dir
	d.configMu.Unlock()
	ctx2, finish2, err2 := d.startInvocationLogContext(context.Background(), nil)
	if err2 != nil {
		t.Fatalf("启动失败: %v", err2)
	}
	if finish2 == nil {
		t.Fatal("配置后收尾函数不应为 nil")
	}
	_ = ctx2
	finish2(nil)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("应产生 1 个 JSONL 文件, got %d", len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	content := string(data)
	if !strings.Contains(content, `"event_type":"invocation_start"`) ||
		!strings.Contains(content, `"event_type":"invocation_end"`) {
		t.Fatalf("start/end 记录缺失: %s", content)
	}
	if !strings.Contains(content, `"role":"test-deep"`) {
		t.Fatalf("role 应为 card.Name: %s", content)
	}
	// 构造失败：目录被文件占用 → 报错返回、finish nil（调用方仅告警）
	filePath := filepath.Join(dir, "occupied")
	os.WriteFile(filePath, []byte("x"), 0o600)
	d.configMu.Lock()
	if d.deepConfig == nil {
		d.deepConfig = schema.NewDeepAgentConfig()
	}
	d.deepConfig.InvocationLogDir = filePath
	d.configMu.Unlock()
	_, finish3, err3 := d.startInvocationLogContext(context.Background(), nil)
	if err3 == nil || finish3 != nil {
		t.Fatalf("非法目录应报错: err=%v finish-nil=%v", err3, finish3 == nil)
	}
}

// TestBuildSubagentKwargs_InvocationLogDir spec 值优先否则继承父（deep_agent.py:1684-1685）
func TestBuildSubagentKwargs_InvocationLogDir(t *testing.T) {
	d := NewDeepAgent(agentschema.NewAgentCard(agentschema.WithAgentName("test-deep")))
	d.configMu.Lock()
	d.deepConfig = schema.NewDeepAgentConfig()
	d.deepConfig.InvocationLogDir = "/parent/logs"
	d.configMu.Unlock()
	// spec 自带 → 优先
	subCfg := schema.NewSubAgentConfig()
	subCfg.InvocationLogDir = "/spec/logs"
	if got := d.buildSubagentCreateKwargs(subCfg, "s1").InvocationLogDir; got != "/spec/logs" {
		t.Fatalf("spec 值应优先: %q", got)
	}
	// spec 未配置 → 继承父
	subCfg.InvocationLogDir = ""
	if got := d.buildSubagentCreateKwargs(subCfg, "s1").InvocationLogDir; got != "/parent/logs" {
		t.Fatalf("应继承父目录: %q", got)
	}
}

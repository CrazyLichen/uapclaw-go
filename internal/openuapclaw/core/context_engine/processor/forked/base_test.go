package forked

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// fakeModelContext 测试用最小 ModelContext（仅 offload 相关路径用到）
type fakeModelContext struct {
	iface.ModelContext
	sessionID    string
	workspaceDir string
	offloaded    map[string][]llm_schema.BaseMessage
}

func (f *fakeModelContext) SessionID() string { return f.sessionID }
func (f *fakeModelContext) WorkspaceDir() string {
	return f.workspaceDir
}
func (f *fakeModelContext) OffloadMessages(handle string, messages []llm_schema.BaseMessage) {
	f.offloaded[handle] = messages
}

// TestContextProcessor_Usage记账 测试压缩用量的提取/累计/合并
func TestContextProcessor_Usage记账(t *testing.T) {
	p := NewContextProcessor(nil)
	if got := p.CurrentCompressionUsage(); got != nil {
		t.Errorf("初始应为 nil，实际 %v", got)
	}

	p.RecordCompressionUsage("not-a-response")
	if got := p.CurrentCompressionUsage(); got != nil {
		t.Errorf("非 AssistantMessage 输入应跳过，实际 %v", got)
	}

	resp := &llm_schema.AssistantMessage{
		UsageMetadata: &llm_schema.UsageMetadata{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
			CacheTokens:  5,
			ModelName:    "qwen-max",
			InputCost:    0.1,
			OutputCost:   0.2,
			TotalCost:    0.3,
		},
	}
	p.RecordCompressionUsage(resp)
	usage := p.CurrentCompressionUsage()
	if usage == nil {
		t.Fatal("记录后应非 nil")
	}
	if usage["total_tokens"] != 30 || usage["calls"] != 1 {
		t.Errorf("首次记录字段不符，实际 %v", usage)
	}

	// 第二次记录后 calls/total 累加、details 追加
	p.RecordCompressionUsage(resp)
	usage = p.CurrentCompressionUsage()
	if usage["calls"] != 2 || usage["total_tokens"] != 60 || usage["total_cost"] != 0.6 {
		t.Errorf("累计结果不符，实际 %v", usage)
	}
	details, _ := usage["details"].([]map[string]any)
	if len(details) != 2 {
		t.Errorf("details 应为 2 条，实际 %d", len(details))
	}

	// Reset 清空
	p.ResetCompressionUsage()
	if got := p.CurrentCompressionUsage(); got != nil {
		t.Errorf("重置后应为 nil，实际 %v", got)
	}
}

// TestGenerateOffloadPath 测试路径生成两分支
func TestGenerateOffloadPath(t *testing.T) {
	got := GenerateOffloadPath("/ws", "s1", "h1")
	want := filepath.Join("/ws", "context", "s1_context", "offload", "h1.json")
	if got != want {
		t.Errorf("workspace 分支不符，got %s want %s", got, want)
	}
	got2 := GenerateOffloadPath("", "s1", "h1")
	if !filepath.IsAbs(got2) || !strings.HasSuffix(got2, filepath.Join("memory", "offloads", "s1", "h1.json")) {
		t.Errorf("无 workspace 分支应为绝对路径，实际 %s", got2)
	}
}

// TestAPIRound 测试 API 轮收尾判定矩阵
func TestAPIRound(t *testing.T) {
	if APIRound(nil) {
		t.Error("空消息应为 false")
	}
	// user 开头未完成轮（user→assistant 才构成已完成轮）
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("hi"),
	}
	if APIRound(msgs) {
		t.Error("仅 user 消息（未完成轮）应为 false")
	}
	msgs = append(msgs, llm_schema.NewAssistantMessage("ok"))
	if !APIRound(msgs) {
		t.Error("user→assistant 已完成轮收尾应为 true")
	}
	// 完成轮后再追加 user → 未收尾
	msgs = append(msgs, llm_schema.NewUserMessage("next"))
	if APIRound(msgs) {
		t.Error("尾随 user 消息应打断收尾判定，应为 false")
	}
}

// TestOffloadMessages_Memory 测试内存卸载分支
func TestOffloadMessages_Memory(t *testing.T) {
	p := NewContextProcessor(nil)
	mc := &fakeModelContext{sessionID: "s1", offloaded: map[string][]llm_schema.BaseMessage{}}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("payload")}

	out, err := p.OffloadMessages(context.Background(), "user", "摘要内容", msgs, mc, "", "in_memory", "", nil)
	if err != nil {
		t.Fatalf("内存卸载不应报错: %v", err)
	}
	if out == nil {
		t.Fatal("应返回占位消息")
	}
	// 占位内容带 handle 标记
	text := textOf(out)
	if !strings.Contains(text, "[[OFFLOAD: handle=") || !strings.Contains(text, "type=in_memory") {
		t.Errorf("占位消息应含 OFFLOAD 标记，实际 %s", text)
	}
	if len(mc.offloaded) != 1 {
		t.Errorf("内存缓冲应收到 1 条，实际 %d", len(mc.offloaded))
	}
}

// TestOffloadMessages_Filesystem 测试文件系统卸载（os 直写 + WITH_PATH 标记）
func TestOffloadMessages_Filesystem(t *testing.T) {
	p := NewContextProcessor(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "offload", "h1.json")
	mc := &fakeModelContext{sessionID: "s1", workspaceDir: "", offloaded: map[string][]llm_schema.BaseMessage{}}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("payload")}

	out, err := p.OffloadMessages(context.Background(), "user", "摘要内容", msgs, mc, "h1", "filesystem", path, nil)
	if err != nil {
		t.Fatalf("文件卸载不应报错: %v", err)
	}
	if out == nil {
		t.Fatal("应返回占位消息")
	}
	text := textOf(out)
	if !strings.Contains(text, "path="+path) {
		t.Errorf("forked WITH_PATH 标记应含 path，实际 %s", text)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("归档文件应落盘: %v", err)
	}
	if !strings.Contains(string(data), `"offload_handle": "h1"`) {
		t.Errorf("归档 JSON 应含 handle，实际 %s", data)
	}
}

// TestOffloadMessages_WriteFail 返回 nil（Python 语义：写盘失败不 fallback）
func TestOffloadMessages_WriteFail(t *testing.T) {
	p := NewContextProcessor(nil)
	mc := &fakeModelContext{sessionID: "s1", workspaceDir: "", offloaded: map[string][]llm_schema.BaseMessage{}}
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("payload")}
	// 相对路径 + 无 SysOperation → 拒绝写
	out, err := p.OffloadMessages(context.Background(), "user", "x", msgs, mc, "h1", "filesystem", "relative/no-abs.json", nil)
	if err != nil {
		t.Fatalf("失败路径不应报错（静默 nil）: %v", err)
	}
	if out != nil {
		t.Errorf("写盘失败应返回 nil，实际 %v", out)
	}
}

// TestRebindModelReference_配置回退 测试无缓存模型时的配置回填
// 两条分支：实现 ModelBindingConfig 的配置走绑定回填；未实现的走"未绑定"分支
// （等价 Python hasattr 为假，偏差 D-24.2-7）。
func TestRebindModelReference_配置回退(t *testing.T) {
	// 分支 1：config 未实现 ModelBindingConfig → 断言失败，走未绑定分支
	bare := NewContextProcessor(&testBareConfig{})
	if bare.RebindModelReference(nil, nil, nil) {
		t.Error("未实现 ModelBindingConfig 且无模型配置时应返回 false")
	}

	// 分支 2：config 实现 ModelBindingConfig → 携带缓存模型重绑时写回配置
	// （不走 llm.NewModel 真实构造——provider 注册表依赖在单测中不铺开）
	reqCfg := &llm_schema.ModelRequestConfig{ModelName: "test-model"}
	clientCfg := &llm_schema.ModelClientConfig{ClientProvider: "test-provider"}
	bound := NewContextProcessor(&testBoundConfig{})
	if !bound.RebindModelReference(&llm.Model{ModelConfig: reqCfg, ClientConfig: clientCfg}, nil, nil) {
		t.Fatal("实现 ModelBindingConfig 且传入缓存模型时应返回 true")
	}
	if bound.Model() == nil {
		t.Error("缓存模型应被回填")
	}
}

// testBareConfig 未实现 ModelBindingConfig 的配置（等价 Python 无 model 属性）
type testBareConfig struct{}

// testBoundConfig 实现 ModelBindingConfig 的配置（15b forked 压缩器 config 形态预演）
type testBoundConfig struct {
	model       *llm_schema.ModelRequestConfig
	modelClient *llm_schema.ModelClientConfig
}

var _ ModelBindingConfig = (*testBoundConfig)(nil)

func (c *testBoundConfig) GetModel() *llm_schema.ModelRequestConfig       { return c.model }
func (c *testBoundConfig) GetModelClient() *llm_schema.ModelClientConfig  { return c.modelClient }
func (c *testBoundConfig) SetModel(v *llm_schema.ModelRequestConfig)      { c.model = v }
func (c *testBoundConfig) SetModelClient(v *llm_schema.ModelClientConfig) { c.modelClient = v }

// textOf 提取消息文本
func textOf(msg llm_schema.BaseMessage) string {
	return fmt.Sprintf("%v", msg.GetContent())
}

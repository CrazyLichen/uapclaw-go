package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubEvolveLLMClient 演进测试惰性 mock 客户端（stubQRClient 先例）。
// 按 messages 首条 UserMessage 内容分派：包含 "Skill 优化专家"
// （GenerateRecords cn 提示词标志）返回 generateResp；包含 "判断以下用户消息"
// （user feedback 提示词标志）返回 feedbackResp；否则返回 otherResp。
// invokeErr 非空时返回错误。所有字段并发安全（mu 保护）。
type stubEvolveLLMClient struct {
	mu            sync.Mutex
	generateResp  string
	feedbackResp  string
	otherResp     string
	invokeErr     error
	generateCalls int
}

// Invoke 按提示词标志分派响应。
func (s *stubEvolveLLMClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content := ""
	if msgs := messages.Messages(); len(msgs) > 0 {
		content = msgs[0].GetContent().Text()
	}
	if s.invokeErr != nil {
		return nil, s.invokeErr
	}
	if strings.Contains(content, "Skill 优化专家") {
		s.generateCalls++
		return llmschema.NewAssistantMessage(s.generateResp), nil
	}
	if strings.Contains(content, "判断以下用户消息") {
		return llmschema.NewAssistantMessage(s.feedbackResp), nil
	}
	return llmschema.NewAssistantMessage(s.otherResp), nil
}

// Stream 流式调用薄桩。
func (s *stubEvolveLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}

// GenerateImage 图像生成薄桩。
func (s *stubEvolveLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

// GenerateSpeech 语音生成薄桩。
func (s *stubEvolveLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

// GenerateVideo 视频生成薄桩。
func (s *stubEvolveLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

// TranscribeAudio 语音转写薄桩。
func (s *stubEvolveLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}

// Release KV 缓存释放薄桩。
func (s *stubEvolveLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// SupportsKVCacheRelease KV 缓存释放能力薄桩。
func (s *stubEvolveLLMClient) SupportsKVCacheRelease() bool { return false }

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newEvolveTestAdapter 构造带 SkillEvolutionRail 的测试 DeepAdapter。
// skillsDir 为技能根目录（调用方需先创建技能子目录）；stub 非 nil 时
// 用 stub 构造注入 mock LLM 的 Model，否则传 nil LLM（走降级路径）。
func newEvolveTestAdapter(t *testing.T, skillsDir string, stub *stubEvolveLLMClient) *DeepAdapter {
	t.Helper()
	var model *llm.Model
	if stub != nil {
		clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
		if err != nil {
			t.Fatalf("构造 clientCfg 失败: %v", err)
		}
		model, err = llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(stub))
		if err != nil {
			t.Fatalf("构造 Model 失败: %v", err)
		}
	}
	rail := coreevolution.NewSkillEvolutionRail(
		[]string{skillsDir}, model, "test-model", "cn",
		coreevolution.WithAutoScan(false), coreevolution.WithAutoSave(false),
	)
	d := NewDeepAdapter()
	d.skillEvolutionRail = rail
	d.defaultModelName = "test-model"
	return d
}

// makeEvolveSkillDir 在 skillsDir 下创建指定技能目录与 SKILL.md。
func makeEvolveSkillDir(t *testing.T, skillsDir, skillName string) string {
	t.Helper()
	skillDir := filepath.Join(skillsDir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %s 失败: %v", skillDir, err)
	}
	mdPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(mdPath, []byte("---\nname: "+skillName+"\ndescription: test\n---\n# "+skillName+"\n"), 0o644); err != nil {
		t.Fatalf("写 SKILL.md 失败: %v", err)
	}
	return skillDir
}

// appendEvolveRecord 写入一条演进记录（applied 控制是否 pending）。
func appendEvolveRecord(t *testing.T, rail *coreevolution.SkillEvolutionRail, skillName, recordID, content string, applied bool) {
	t.Helper()
	rec := checkpointing.EvolutionRecord{
		ID:     recordID,
		Source: "test",
		Change: checkpointing.EvolutionPatch{
			Section: "Troubleshooting",
			Action:  "append",
			Content: content,
			Target:  signal.EvolutionTargetBody,
		},
		Score:      0.8,
		Applied:    applied,
		UsageStats: &checkpointing.UsageStats{},
	}
	if err := rail.EvolutionStore().AppendRecord(t.Context(), skillName, rec); err != nil {
		t.Fatalf("AppendRecord 失败: %v", err)
	}
}

// newEvolveTestAgentCard 构造测试 AgentCard。
func newEvolveTestAgentCard(agentID string) *agentschema.AgentCard {
	return agentschema.NewAgentCard(
		agentschema.WithAgentID(agentID),
		agentschema.WithAgentName(agentID+"-name"),
	)
}

// newEvolveTestReActAgent 构造带 ContextEngine 的 ReActAgent
// （对齐 harness.createReactAgent 的引擎注入部分）。
func newEvolveTestReActAgent(t *testing.T, card *agentschema.AgentCard) *agents.ReActAgent {
	t.Helper()
	reactAgent := agents.NewReActAgent(card, saconfig.NewReActAgentConfig())
	reactAgent.SetContextEngine(context_engine.NewContextEngine(ceschema.NewContextEngineConfig()))
	return reactAgent
}

// newEvolveTestSession 构造隔离的测试 Session（新建 InMemoryCheckpointer，
// NewTestSession Checkpointer 隔离先例：全局单例导致跨测试状态污染）。
func newEvolveTestSession(t *testing.T, sessionID string) *session.Session {
	t.Helper()
	return session.NewSession(
		session.WithSessionID(sessionID),
		session.WithCheckpointer(checkpointer.NewInMemoryCheckpointer()),
	)
}

// setupEvolveTestWorkspace workspace 隔离（diff_service_test.go setupTestWorkspace 先例）。
// 返回 cleanup 函数恢复环境。
func setupEvolveTestWorkspace(t *testing.T) func() {
	t.Helper()
	tmpDir := t.TempDir()

	configDir := filepath.Join(tmpDir, ".uapclaw", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}

	oldHome := os.Getenv("UAPCLAW_HOME")
	oldDataDir := os.Getenv("UAPCLAW_DATA_DIR")
	os.Setenv("UAPCLAW_HOME", tmpDir)
	os.Unsetenv("UAPCLAW_DATA_DIR")
	workspace.SetUserHome(tmpDir)

	return func() {
		if oldHome != "" {
			os.Setenv("UAPCLAW_HOME", oldHome)
		} else {
			os.Unsetenv("UAPCLAW_HOME")
		}
		if oldDataDir != "" {
			os.Setenv("UAPCLAW_DATA_DIR", oldDataDir)
		} else {
			os.Unsetenv("UAPCLAW_DATA_DIR")
		}
		workspace.SetUserHome("")
	}
}

// ──────────── handleEvolveCommand 测试 ────────────

// TestHandleEvolveCommand_裸命令列表分支 测试 M-10 链 A。
//
// /evolve（无参数）→ 走 list_pending_summary 摘要分支（原先错误
// fallback 到 handleEvolveListCommand 导致 "请指定 Skill 名称" 错误）。
func TestHandleEvolveCommand_裸命令列表分支(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "demo-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	// 无 pending 记录 → ListPendingSummary 返回 "当前所有 Skill 暂无演进信息。"
	result, err := d.handleEvolveCommand(t.Context(), "/evolve", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand error: %v", err)
	}
	if result == nil {
		t.Fatal("裸 /evolve 应返回列表摘要，得到 nil")
	}
	if result["result_type"] != "answer" {
		t.Errorf("result_type = %v, want answer", result["result_type"])
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "**Skills 演进记录：**") {
		t.Errorf("裸 /evolve 输出应含演进记录标题，输出:\n%s", output)
	}
	// 关键断言：不能是 evolve_list 的参数错误（M-10 链 A 回归点）
	if strings.Contains(output, "请指定 Skill 名称") {
		t.Errorf("裸 /evolve 不应返回 evolve_list 参数错误（M-10 链 A），输出:\n%s", output)
	}

	// 追加 pending 记录后 → 摘要包含技能名与条目计数
	appendEvolveRecord(t, d.skillEvolutionRail, "demo-skill", "ev_rec_1", "遇到 X 错误时应检查 Y", false)
	result2, err := d.handleEvolveCommand(t.Context(), "/evolve", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand(有记录) error: %v", err)
	}
	output2, _ := result2["output"].(string)
	if !strings.Contains(output2, "demo-skill") {
		t.Errorf("有 pending 记录时摘要应含技能名，输出:\n%s", output2)
	}
	if !strings.Contains(output2, "1 条 pending 经验") {
		t.Errorf("摘要应含 pending 计数，输出:\n%s", output2)
	}

	// "list" 显式参数走同一分支
	result3, err := d.handleEvolveCommand(t.Context(), "/evolve list", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand(list) error: %v", err)
	}
	output3, _ := result3["output"].(string)
	if !strings.Contains(output3, "**Skills 演进记录：**") {
		t.Errorf("/evolve list 应与裸 /evolve 同分支，输出:\n%s", output3)
	}
}

// TestHandleEvolveCommand_空技能目录 测试无可见 Skill 时的提示。
func TestHandleEvolveCommand_空技能目录(t *testing.T) {
	skillsDir := t.TempDir()
	d := newEvolveTestAdapter(t, skillsDir, nil)

	result, err := d.handleEvolveCommand(t.Context(), "/evolve", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand error: %v", err)
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "未找到任何 Skill 目录") {
		t.Errorf("空技能目录应返回未找到提示，输出:\n%s", output)
	}
}

// TestHandleEvolveCommand_技能不存在 测试 skill_name 校验错误。
func TestHandleEvolveCommand_技能不存在(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "real-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	result, err := d.handleEvolveCommand(t.Context(), "/evolve no-such-skill 优化一下", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand error: %v", err)
	}
	if result["result_type"] != "error" {
		t.Errorf("result_type = %v, want error", result["result_type"])
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "no-such-skill") {
		t.Errorf("输出应含未找到的技能名，输出:\n%s", output)
	}
	if !strings.Contains(output, "real-skill") {
		t.Errorf("输出应列出可用 Skill，输出:\n%s", output)
	}
}

// TestHandleEvolveCommand_无信号无意图 测试无归属信号且无 user_query 时的提示。
func TestHandleEvolveCommand_无信号无意图(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "demo-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	// instance 为 nil → collectMessagesForEvolve 返回 nil → 无检测信号；
	// user_query 为空 → 提前返回 "未发现明确的演进信号"
	result, err := d.handleEvolveCommand(t.Context(), "/evolve demo-skill", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand error: %v", err)
	}
	if result["result_type"] != "answer" {
		t.Errorf("result_type = %v, want answer", result["result_type"])
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "未发现明确的演进信号") {
		t.Errorf("无信号无意图应返回提示，输出:\n%s", output)
	}
}

// TestHandleEvolveCommand_成功路径审批块形状 测试 M-14 修复的完整成功链。
//
// skill 存在 + user_query 非空 → RequestUserEvolution（user_intent 信号）→
// mock LLM 返回合法 JSON 草稿 → 生成 records + 审批事件 →
// 返回 approval_chunks: []map[string]any（非 []any{result.ApprovalEvent} 原指针形状）。
func TestHandleEvolveCommand_成功路径审批块形状(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "demo-skill")
	stub := &stubEvolveLLMClient{
		// GenerateRecords 提示词（cn 版以 "你是一个 Skill 优化专家" 开头）的 mock 响应：
		// 一条 append 草稿
		generateResp: `[{"action": "append", "section": "Troubleshooting", "target": "body", "content": "遇到超时错误时先检查网络代理配置"}]`,
	}
	d := newEvolveTestAdapter(t, skillsDir, stub)

	result, err := d.handleEvolveCommand(t.Context(), "/evolve demo-skill 把超时问题记录下来", "s1")
	if err != nil {
		t.Fatalf("handleEvolveCommand error: %v", err)
	}
	if result == nil {
		t.Fatal("成功路径应返回结果")
	}
	if result["result_type"] != "answer" {
		t.Errorf("result_type = %v, want answer, output=%v", result["result_type"], result["output"])
	}

	// M-14 核心：approval_chunks 必须是 []map[string]any 展平形状
	chunks, ok := result["approval_chunks"].([]map[string]any)
	if !ok {
		t.Fatalf("approval_chunks 应为 []map[string]any 展平形状（M-14），实际类型 %T", result["approval_chunks"])
	}
	if len(chunks) != 1 {
		t.Fatalf("approval_chunks 长度 = %d, want 1", len(chunks))
	}
	chunk := chunks[0]
	if chunk["event_type"] != "chat.ask_user_question" {
		t.Errorf("chunk event_type = %v, want chat.ask_user_question", chunk["event_type"])
	}
	requestID, _ := chunk["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		t.Errorf("chunk 应含非空 request_id（approvalChunkFromEvent 校验），chunk=%v", chunk)
	}
	// questions 双形状（M-14 残留修复）：进程内原生构造产出 []map[string]any，
	// 经 WS/JSON 序列化产出 []any；teamHelperQuestionsItems 统一识别
	questions := teamHelperQuestionsItems(chunk["questions"])
	if len(questions) != 1 {
		t.Fatalf("questions 长度 = %d, want 1", len(questions))
	}
	qm, _ := questions[0].(map[string]any)
	qText, _ := qm["question"].(string)
	if !strings.Contains(qText, "demo-skill") {
		t.Errorf("question 应含技能名，question=%q", qText)
	}

	// 输出摘要应含审批问题文本与条数
	output, _ := result["output"].(string)
	if !strings.Contains(output, "已为 Skill 'demo-skill' 生成 1 条演进经验") {
		t.Errorf("输出应含成功摘要，输出:\n%s", output)
	}
	if !strings.Contains(output, "1. ") {
		t.Errorf("输出应含问题编号列表，输出:\n%s", output)
	}
	// 顶层 request_id / skill_name 字段
	if result["skill_name"] != "demo-skill" {
		t.Errorf("skill_name = %v, want demo-skill", result["skill_name"])
	}
	if stub.generateCalls == 0 {
		t.Error("mock LLM 的 GenerateRecords 提示词未被调用")
	}
}

// TestHandleEvolveCommand_信号指纹去重 测试 S-11 决策点 A 的统一去重。
//
// slash 处理过的信号写入 rail.ProcessedSignalKeys()——同一指纹
// 第二次 slash 调用时被跳过。对话消息中 execution_failure 信号
// 归属目标 skill；去重键在信号检测后立即回写。
func TestHandleEvolveCommand_信号指纹去重(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "demo-skill")
	stub := &stubEvolveLLMClient{
		generateResp: `[{"action": "append", "section": "Troubleshooting", "target": "body", "content": "记录网络重试经验"}]`,
	}
	d := newEvolveTestAdapter(t, skillsDir, stub)

	// 构造带 execution_failure 工具消息的会话上下文：
	// DeepAgent + ReActAgent + ContextEngine + default_context 组件装配链
	card := newEvolveTestAgentCard("evolve-dedup-agent")
	instance := harness.NewDeepAgent(card)
	reactAgent := newEvolveTestReActAgent(t, card)
	instance.SetReactAgent(reactAgent, true)
	d.instance = instance

	ctx := t.Context()
	sess := newEvolveTestSession(t, "evolve-dedup-session")
	mc, err := reactAgent.ContextEngine().CreateContext(ctx, "default_context", sess)
	if err != nil {
		t.Fatalf("CreateContext 失败: %v", err)
	}
	// 消息链：assistant(skill_tool 读取 demo-skill) → tool(执行失败)
	assistantMsg := llmschema.NewAssistantMessage("正在读取技能",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("call_1", "skill_tool",
				`{"skill_name": "demo-skill", "relative_file_path": "SKILL.md"}`),
		}))
	toolMsg := llmschema.NewToolMessage("call_1", "执行失败: connection error timeout", llmschema.WithMessageName("skill_tool"))
	if _, err := mc.AddMessages(ctx, []llmschema.BaseMessage{assistantMsg, toolMsg}); err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}

	// 第一次调用：信号被检测并写入 processedSignalKeys
	result1, err := d.handleEvolveCommand(t.Context(), "/evolve demo-skill", "evolve-dedup-session")
	if err != nil {
		t.Fatalf("第一次 handleEvolveCommand error: %v", err)
	}
	if result1 == nil {
		t.Fatal("第一次调用应返回结果")
	}

	keys := d.skillEvolutionRail.ProcessedSignalKeys()
	if len(keys) == 0 {
		t.Fatal("首次调用后 processedSignalKeys 应非空（S-11 决策点 A 统一指纹回写）")
	}
	// execution_failure 信号指纹应存在（skill_name=demo-skill）
	found := false
	for fp := range keys {
		if fp[0] == "execution_failure" && fp[2] == "demo-skill" {
			found = true
		}
	}
	if !found {
		t.Errorf("processedSignalKeys 应含 execution_failure/demo-skill 指纹，实际 keys=%v", keys)
	}

	// 第二次调用：同一信号被去重跳过（user_query 为空 + attributed 为空 → 未发现信号提示）
	// （若去重失效，信号会再次进入演进链，mock LLM 的 generate 调用次数会增加）
	genCallsBefore := stub.generateCalls
	result2, err := d.handleEvolveCommand(t.Context(), "/evolve demo-skill", "evolve-dedup-session")
	if err != nil {
		t.Fatalf("第二次 handleEvolveCommand error: %v", err)
	}
	if result2 == nil {
		t.Fatal("第二次调用应返回结果")
	}
	output2, _ := result2["output"].(string)
	if !strings.Contains(output2, "未发现明确的演进信号") {
		t.Errorf("去重后第二次调用应提示未发现信号，输出:\n%s", output2)
	}
	if stub.generateCalls != genCallsBefore {
		t.Errorf("去重后不应再次调用 GenerateRecords（calls %d → %d）", genCallsBefore, stub.generateCalls)
	}
}

// ──────────── handleEvolveListCommand 测试 ────────────

// TestHandleEvolveListCommand_参数校验 测试空参数/`--` 前缀错误。
func TestHandleEvolveListCommand_参数校验(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "demo-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	tests := []struct {
		name  string
		query string
	}{
		{"无参数", "/evolve_list"},
		{"仅空白", "/evolve_list   "},
		{"--前缀", "/evolve_list --sort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := d.handleEvolveListCommand(t.Context(), tt.query, "s1")
			if err != nil {
				t.Fatalf("handleEvolveListCommand error: %v", err)
			}
			if result["result_type"] != "error" {
				t.Errorf("result_type = %v, want error", result["result_type"])
			}
			output, _ := result["output"].(string)
			if !strings.Contains(output, "请指定 Skill 名称") {
				t.Errorf("输出应为参数提示，输出:\n%s", output)
			}
		})
	}
}

// TestHandleEvolveListCommand_技能不存在 测试 skill 校验错误与可用列表。
func TestHandleEvolveListCommand_技能不存在(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "real-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	result, err := d.handleEvolveListCommand(t.Context(), "/evolve_list ghost-skill", "s1")
	if err != nil {
		t.Fatalf("handleEvolveListCommand error: %v", err)
	}
	if result["result_type"] != "error" {
		t.Errorf("result_type = %v, want error", result["result_type"])
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "未找到 Skill 'ghost-skill'") {
		t.Errorf("输出应含未找到提示，输出:\n%s", output)
	}
	if !strings.Contains(output, "real-skill") {
		t.Errorf("输出应列出可用 Skill，输出:\n%s", output)
	}
}

// TestHandleEvolveListCommand_空记录 测试无演进经验时的提示。
func TestHandleEvolveListCommand_空记录(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "empty-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	result, err := d.handleEvolveListCommand(t.Context(), "/evolve_list empty-skill", "s1")
	if err != nil {
		t.Fatalf("handleEvolveListCommand error: %v", err)
	}
	if result["result_type"] != "answer" {
		t.Errorf("result_type = %v, want answer", result["result_type"])
	}
	output, _ := result["output"].(string)
	if !strings.Contains(output, "Skill 'empty-skill' 暂无演进经验") {
		t.Errorf("空记录应返回暂无经验提示，输出:\n%s", output)
	}
}

// TestHandleEvolveListCommand_表格输出 测试 M-10 链 B 的文本表格格式。
//
// 有记录时返回 buildEvolveListSummary 表格（非旧的
// {"action": "evolve_list", "skills": [...]} 结构化 map）。
func TestHandleEvolveListCommand_表格输出(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "table-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)
	appendEvolveRecord(t, d.skillEvolutionRail, "table-skill", "ev_t1", "第一条经验内容", false)

	result, err := d.handleEvolveListCommand(t.Context(), "/evolve_list table-skill", "s1")
	if err != nil {
		t.Fatalf("handleEvolveListCommand error: %v", err)
	}
	if result["result_type"] != "answer" {
		t.Errorf("result_type = %v, want answer", result["result_type"])
	}
	output, _ := result["output"].(string)
	// 表格关键标志
	if !strings.Contains(output, "📊 Skill \"table-skill\" — 经验库摘要") {
		t.Errorf("输出应含表格标题（M-10 链 B），输出:\n%s", output)
	}
	if !strings.Contains(output, "共 1 条经验") {
		t.Errorf("输出应含经验计数，输出:\n%s", output)
	}
	if !strings.Contains(output, "| # | Score | Used | Effect |") {
		t.Errorf("输出应含表头，输出:\n%s", output)
	}
	if !strings.Contains(output, "第一条经验内容") {
		t.Errorf("输出应含记录内容预览，输出:\n%s", output)
	}
	if !strings.Contains(output, "/evolve_simplify table-skill") {
		t.Errorf("输出尾部应含 simplify 提示，输出:\n%s", output)
	}
	// 关键断言：不能是旧的结构化形状
	if _, hasAction := result["action"]; hasAction {
		t.Errorf("不应返回旧的 action 字段（M-10 链 B 回归点），result=%v", result)
	}
	if _, hasSkills := result["skills"]; hasSkills {
		t.Errorf("不应返回旧的 skills 字段（M-10 链 B 回归点），result=%v", result)
	}
}

// TestHandleEvolveListCommand_禁用技能过滤 测试 FilterVisibleSkillNames 接线。
//
// skills_state.json 中 enabled=false 的技能在"可用列表"中被过滤。
func TestHandleEvolveListCommand_禁用技能过滤(t *testing.T) {
	// workspace 隔离（skills_state.json 写入 AgentSkillsDir）
	cleanup := setupEvolveTestWorkspace(t)
	defer cleanup()

	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "visible-skill")
	makeEvolveSkillDir(t, skillsDir, "hidden-skill")
	d := newEvolveTestAdapter(t, skillsDir, nil)

	// 写 skills_state.json：hidden-skill 注册且禁用
	stateDir := workspace.AgentSkillsDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir skills 状态目录失败: %v", err)
	}
	state := map[string]any{
		"marketplaces":      []any{},
		"installed_plugins": []any{},
		"local_skills": []any{
			map[string]any{"name": "visible-skill"},
			map[string]any{"name": "hidden-skill"},
		},
		"skill_configs": map[string]any{
			"visible-skill": map[string]any{"enabled": true},
			"hidden-skill":  map[string]any{"enabled": false},
		},
	}
	stateData, _ := json.Marshal(state)
	if err := os.WriteFile(skillruntime.GetStateFile(), stateData, 0o644); err != nil {
		t.Fatalf("写 skills_state.json 失败: %v", err)
	}

	// /evolve_list hidden-skill：技能存在（store 层 SkillExists 不过滤）仍可查询
	result, err := d.handleEvolveListCommand(t.Context(), "/evolve_list hidden-skill", "s1")
	if err != nil {
		t.Fatalf("handleEvolveListCommand error: %v", err)
	}
	if result["result_type"] != "answer" {
		t.Errorf("result_type = %v, want answer（SkillExists 不过滤）", result["result_type"])
	}

	// 不存在技能的错误信息中可用列表应排除 hidden-skill
	result2, err := d.handleEvolveListCommand(t.Context(), "/evolve_list ghost", "s1")
	if err != nil {
		t.Fatalf("handleEvolveListCommand error: %v", err)
	}
	output2, _ := result2["output"].(string)
	if strings.Contains(output2, "hidden-skill") {
		t.Errorf("禁用技能不应出现在可用列表，输出:\n%s", output2)
	}
	if !strings.Contains(output2, "visible-skill") {
		t.Errorf("可见技能应出现在可用列表，输出:\n%s", output2)
	}
}

// ──────────── collectMessagesForEvolve / applyModelToReactAgent 测试 ────────────

// TestCollectMessagesForEvolve_nil守卫 测试各环节缺失时不 panic 且返回 nil。
func TestCollectMessagesForEvolve_nil守卫(t *testing.T) {
	d := NewDeepAdapter()

	// instance 为 nil
	if msgs := d.collectMessagesForEvolve("s1"); msgs != nil {
		t.Errorf("instance nil 时应返回 nil，got %v", msgs)
	}

	// instance 存在但 ReactAgent 为 nil（未 Configure 的 DeepAgent）
	card := newEvolveTestAgentCard("collect-nil-agent")
	instance := harness.NewDeepAgent(card)
	d.instance = instance
	if msgs := d.collectMessagesForEvolve("s1"); msgs != nil {
		t.Errorf("ReactAgent nil 时应返回 nil，got %v", msgs)
	}

	// ReactAgent 存在但 context 缺失（sessionID 无对应 context）
	reactAgent := newEvolveTestReActAgent(t, card)
	instance.SetReactAgent(reactAgent, true)
	if msgs := d.collectMessagesForEvolve("no-such-session"); msgs != nil {
		t.Errorf("context 缺失时应返回 nil，got %v", msgs)
	}
}

// TestCollectMessagesForEvolve_消息读取 测试正常链路的 tool_calls 归一化。
func TestCollectMessagesForEvolve_消息读取(t *testing.T) {
	d := NewDeepAdapter()
	card := newEvolveTestAgentCard("collect-read-agent")
	instance := harness.NewDeepAgent(card)
	reactAgent := newEvolveTestReActAgent(t, card)
	instance.SetReactAgent(reactAgent, true)
	d.instance = instance

	ctx := t.Context()
	sess := newEvolveTestSession(t, "collect-read-session")
	mc, err := reactAgent.ContextEngine().CreateContext(ctx, "default_context", sess)
	if err != nil {
		t.Fatalf("CreateContext 失败: %v", err)
	}
	assistantMsg := llmschema.NewAssistantMessage("调用工具",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("call_9", "read_file", `{"file_path": "/tmp/a.py"}`),
		}))
	toolMsg := llmschema.NewToolMessage("call_9", "file content here", llmschema.WithMessageName("read_file"))
	userMsg := llmschema.NewUserMessage("帮我读文件")
	if _, err := mc.AddMessages(ctx, []llmschema.BaseMessage{userMsg, assistantMsg, toolMsg}); err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}

	msgs := d.collectMessagesForEvolve("collect-read-session")
	if len(msgs) != 3 {
		t.Fatalf("消息数 = %d, want 3", len(msgs))
	}
	// 第二条 assistant 应带 tool_calls
	if msgs[1]["role"] != "assistant" {
		t.Errorf("msgs[1] role = %v, want assistant", msgs[1]["role"])
	}
	tcs, ok := msgs[1]["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatalf("msgs[1] tool_calls 类型 = %T, want []map[string]any", msgs[1]["tool_calls"])
	}
	if len(tcs) != 1 || tcs[0]["name"] != "read_file" {
		t.Errorf("tool_calls[0] 应为 read_file，got %v", tcs)
	}
	// 第三条 tool 消息
	if msgs[2]["role"] != "tool" {
		t.Errorf("msgs[2] role = %v, want tool", msgs[2]["role"])
	}
}

// TestApplyModelToReactAgent_nil与正常路径 测试 S-07 抽出的共用模型应用逻辑。
func TestApplyModelToReactAgent_nil与正常路径(t *testing.T) {
	d := NewDeepAdapter()

	// nil 模型 → 无操作不 panic
	d.applyModelToReactAgent(nil)

	// instance nil → 仅同步 adapter 字段
	clientCfg, err := llmschema.NewModelClientConfig("openai", "k1", "https://mock1.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造 clientCfg 失败: %v", err)
	}
	modelCfg := llmschema.NewModelRequestConfig()
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	d.applyModelToReactAgent(model)
	if d.model != model {
		t.Error("instance nil 时 adapter.model 也应被同步")
	}

	// 正常路径：instance + ReactAgent 存在 → SetLLM 注入
	card := newEvolveTestAgentCard("apply-model-agent")
	instance := harness.NewDeepAgent(card)
	reactAgent := newEvolveTestReActAgent(t, card)
	instance.SetReactAgent(reactAgent, true)
	d.instance = instance

	clientCfg2, err := llmschema.NewModelClientConfig("openai", "k2", "https://mock2.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造 clientCfg2 失败: %v", err)
	}
	model2, err := llm.NewModel(clientCfg2, modelCfg)
	if err != nil {
		t.Fatalf("构造 Model2 失败: %v", err)
	}
	d.applyModelToReactAgent(model2)

	gotLLM, err := reactAgent.GetLLM()
	if err != nil {
		t.Fatalf("GetLLM error: %v", err)
	}
	if gotLLM != model2 {
		t.Error("reactAgent 上应注入 model2（S-07 模型切换）")
	}
	if d.model != model2 || d.modelClientConfig != model2.ClientConfig || d.modelRequestConfig != model2.ModelConfig {
		t.Error("adapter 模型三字段应同步为 model2")
	}
}

// ──────────── 流式消费者双形状回归测试（M-14 扩展） ────────────

// TestStreamApprovalChunksEmission_双形状 直接测试流式发射逻辑。
//
// 复刻 ProcessMessageStreamImpl 审批块发射序列（M-14 双形状兼容块），
// 验证两种切片形状均产生 ask_user_question chunk + chat.done 终止 chunk。
// 使用与生产相同的 NewAgentResponseChunk 封装。
func TestStreamApprovalChunksEmission_双形状(t *testing.T) {
	emit := func(approvalChunks any) []*sschema.AgentResponseChunk {
		// 复刻 ProcessMessageStreamImpl 审批发射逻辑（L1074-1107）
		var out []*sschema.AgentResponseChunk
		if approvalChunks != nil {
			emitted := false
			if slice, ok := approvalChunks.([]map[string]any); ok {
				for _, chunkMap := range slice {
					out = append(out, sschema.NewAgentResponseChunk("r1", "c1", chunkMap))
					emitted = true
				}
			}
			if !emitted {
				if slice, ok := approvalChunks.([]any); ok {
					for _, item := range slice {
						if chunkMap, ok := item.(map[string]any); ok {
							out = append(out, sschema.NewAgentResponseChunk("r1", "c1", chunkMap))
						}
					}
				}
			}
			out = append(out, sschema.NewAgentResponseChunk("r1", "c1",
				map[string]any{"event_type": "chat.done"}, sschema.WithChunkIsComplete(true)))
		} else {
			out = append(out, sschema.NewAgentResponseChunk("r1", "c1",
				map[string]any{"event_type": "chat.final", "content": "x"}, sschema.WithChunkIsComplete(true)))
		}
		return out
	}

	// []map[string]any 形状（handleEvolveCommand 产出）
	mapShape := []map[string]any{
		{"event_type": "chat.ask_user_question", "request_id": "r_a", "questions": []any{map[string]any{"question": "q1"}}},
	}
	out1 := emit(mapShape)
	if len(out1) != 2 {
		t.Fatalf("map 形状应发射 2 个 chunk（1 审批 + 1 done），got %d", len(out1))
	}
	if out1[0].Payload["event_type"] != "chat.ask_user_question" {
		t.Errorf("chunk[0] event_type = %v, want chat.ask_user_question", out1[0].Payload["event_type"])
	}
	if out1[1].Payload["event_type"] != "chat.done" || !out1[1].IsComplete {
		t.Errorf("chunk[1] 应为 chat.done 终止，payload=%v isComplete=%v", out1[1].Payload, out1[1].IsComplete)
	}

	// []any 形状（/evolve_simplify 产出）
	anyShape := []any{
		map[string]any{"event_type": "chat.ask_user_question", "request_id": "r_b", "questions": []any{map[string]any{"question": "q2"}}},
	}
	out2 := emit(anyShape)
	if len(out2) != 2 {
		t.Fatalf("any 形状应发射 2 个 chunk，got %d", len(out2))
	}
	if out2[0].Payload["request_id"] != "r_b" {
		t.Errorf("chunk[0] request_id = %v, want r_b", out2[0].Payload["request_id"])
	}

	// nil → chat.final 分支
	out3 := emit(nil)
	if len(out3) != 1 || out3[0].Payload["event_type"] != "chat.final" {
		t.Errorf("nil 应走 chat.final 分支，got %v", out3)
	}
}

// TestProcessMessageStreamImpl_非流式审批块透传 测试非流式路径的
// approval_chunks 透传（M-14：非流式直接透传 JSON 数组，天然兼容）。
func TestProcessMessageStreamImpl_非流式审批块透传(t *testing.T) {
	skillsDir := t.TempDir()
	makeEvolveSkillDir(t, skillsDir, "ns-skill")
	stub := &stubEvolveLLMClient{
		generateResp: `[{"action": "append", "section": "Troubleshooting", "target": "body", "content": "非流式经验"}]`,
	}
	d := newEvolveTestAdapter(t, skillsDir, stub)
	card := newEvolveTestAgentCard("nonstream-agent")
	d.instance = harness.NewDeepAgent(card)

	params, _ := json.Marshal(map[string]any{"query": "/evolve ns-skill 记录经验", "mode": "agent.plan"})
	req := sschema.NewAgentRequest("req-ns-1", "ch-ns-1", sschema.ReqMethod("chat.send"), params,
		sschema.WithAgentSessionID("web_sess-ns-1"))

	// 非流式 agent.plan 模式：ensureEvolutionRailForSlash 需要 evolution.enabled
	d.configCache = map[string]any{"evolution": map[string]any{"enabled": true}}

	resp, err := d.ProcessMessageImpl(t.Context(), req, map[string]any{})
	if err != nil {
		t.Fatalf("ProcessMessageImpl error: %v", err)
	}
	if resp == nil {
		t.Fatal("应返回响应")
	}
	// 审批块透传：payload 含 approval_chunks
	if _, ok := resp.Payload["approval_chunks"]; !ok {
		// 无审批块时输出 content——检查是否走了 error 分支
		t.Fatalf("应含 approval_chunks payload，实际 payload=%v", resp.Payload)
	}
}

// ──────────── S-07 team 分支 6 步测试 ────────────

// TestProcessMessageStreamImpl_team分支6步 测试 S-07。
//
// team 模式请求进入 processTeamMessageStream 前完成 6 步运行时设置：
// 1-2 模型解析与应用（reactAgent.SetLLM）；3 语言解析；4 频道解析；
// 5 RuntimePromptRail SetModelName/SetMode；6 runtime_state.yaml 写入。
//
// 实现方式：预注册 stream task 使 HasStreamTask=true → 走后续请求分支
// （Interact 对无 active session 返回 false → chat.error 快速返回），
// 避免 first-request 分支的完整 TeamAgent 创建。
func TestProcessMessageStreamImpl_team分支6步(t *testing.T) {
	// workspace 隔离（runtime_state.yaml 写入 ConfigDir）
	cleanup := setupEvolveTestWorkspace(t)
	defer cleanup()

	channelID := "ch-team-sixstep-1"
	sessionID := "web_sess-team-sixstep-1"

	d := NewDeepAdapter()
	card := newEvolveTestAgentCard("team-six-step-agent")
	instance := harness.NewDeepAgent(card)
	reactAgent := newEvolveTestReActAgent(t, card)
	instance.SetReactAgent(reactAgent, true)
	d.instance = instance

	// 注入模型（modelRequestConfig 带 ModelName 供 resolveModelName）
	clientCfg, err := llmschema.NewModelClientConfig("openai", "k-team", "https://mock-team.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造 clientCfg 失败: %v", err)
	}
	modelCfg := llmschema.NewModelRequestConfig()
	modelCfg.ModelName = "team-sixstep-model"
	model, err := llm.NewModel(clientCfg, modelCfg)
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	d.model = model
	d.modelClientConfig = model.ClientConfig
	d.modelRequestConfig = model.ModelConfig

	// RuntimePromptRail（buildRuntimePromptRail 同款构造）
	d.runtimePromptRail = commrails.NewRuntimePromptRail("cn", "web")

	// 预注册 stream task → HasStreamTask=true → isFirstRequest=false
	// → 后续请求分支：Interact 对无 active session 返回 false → 快速返回
	tm := team.GetTeamManager(channelID)
	tm.RegisterStreamTask(sessionID, func() {})
	t.Cleanup(func() {
		if cancel := tm.PopStreamTask(sessionID); cancel != nil {
			cancel()
		}
	})

	params, _ := json.Marshal(map[string]any{"query": "hi", "mode": "team"})
	req := sschema.NewAgentRequest("req-team-1", channelID, sschema.ReqMethod("chat.send"), params,
		sschema.WithAgentSessionID(sessionID))

	ch, err := d.ProcessMessageStreamImpl(t.Context(), req, map[string]any{})
	if err != nil {
		t.Fatalf("ProcessMessageStreamImpl error: %v", err)
	}
	if ch == nil {
		t.Fatal("应返回 channel")
	}
	// 消费 channel 直到关闭（后续请求分支快速返回 chat.error + complete）
	for range ch {
	}

	// 步骤 1-2 断言：模型已应用到 react agent（SetLLM 注入）
	gotLLM, lerr := reactAgent.GetLLM()
	if lerr != nil {
		t.Fatalf("GetLLM error: %v", lerr)
	}
	if gotLLM != model {
		t.Error("team 分支应完成 applyModelToReactAgent（S-07 步骤 1-2）")
	}

	// 步骤 6 断言：runtime_state.yaml 已写入
	yamlPath := filepath.Join(workspace.ConfigDir(), "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("runtime_state.yaml 应已写入（S-07 步骤 6）: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "mode:") {
		t.Errorf("runtime_state.yaml 应含 mode 字段，内容:\n%s", content)
	}
	if !strings.Contains(content, "channel:") {
		t.Errorf("runtime_state.yaml 应含 channel 字段，内容:\n%s", content)
	}
	// team 模式显示名（cn 语言下 mode 显示 "集群模式"）
	if !strings.Contains(content, "集群模式") {
		t.Errorf("cn 语言下 team 模式应显示为 集群模式，内容:\n%s", content)
	}
	// channel：req.ChannelID 优先
	if !strings.Contains(content, channelID) {
		t.Errorf("runtime_state.yaml channel 应取 req.ChannelID，内容:\n%s", content)
	}
	// 模型名（步骤 5 的 SetModelName 参数与步骤 6 yaml 的 model 字段同源）
	if !strings.Contains(content, "team-sixstep-model") {
		t.Errorf("runtime_state.yaml 应含模型名，内容:\n%s", content)
	}
}

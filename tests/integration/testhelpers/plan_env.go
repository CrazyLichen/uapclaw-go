//go:build integration

package testhelpers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanAdapter 计划模式集成测试所需的最小适配器能力（DeepAdapter / CodeAdapter 均满足）。
type PlanAdapter interface {
	// ProcessMessageImpl 走完整请求链路
	ProcessMessageImpl(ctx context.Context, req *swarmschema.AgentRequest, inputs map[string]any) (*swarmschema.AgentResponse, error)
	// Instance 返回装配后的 DeepAgent
	Instance() *harness.DeepAgent
}

// PlanEnv 计划模式（S2/S3）集成测试环境。
//
// 与 AdapterTestEnv 的关系：复用隔离三件套 + MockLLM provider 注册，
// 额外提供「发一轮 chat → 读 plan 状态 / 控制事件 / 模型实收窗口」的断言助手。
type PlanEnv struct {
	// T 测试句柄
	T *testing.T
	// Env 底层隔离环境（DataDir / Provider）
	Env *AdapterTestEnv
	// Adapter 受测适配器
	Adapter PlanAdapter
	// Deep 仅 agent 族适配器可用（提供 plan 控制器与控制事件读取）
	Deep *adapter.DeepAdapter
	// Mock 模型桩（与 BaseIntegrationSuite.MockLLM 同一实例）
	Mock *mockllm.MockModelClient
	// ChannelID 请求渠道
	ChannelID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// planEnvChannel 集成测试渠道标识
	planEnvChannel = "itest-channel"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlanEnv 构建 agent 族（DeepAdapter）测试环境。
func NewPlanEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *PlanEnv {
	t.Helper()
	env := NewAdapterTestEnv(t, mock, prefix)
	env.WriteConfigYAML(t, "")
	d := adapter.NewDeepAdapter()
	if err := d.CreateInstance(context.Background(), map[string]any{}, "agent", ""); err != nil {
		t.Fatalf("创建 DeepAdapter 失败: %v", err)
	}
	t.Cleanup(func() { _ = d.Cleanup() })
	return &PlanEnv{T: t, Env: env, Adapter: d, Deep: d, Mock: mock, ChannelID: planEnvChannel}
}

// NewCodePlanEnv 构建 code 族（CodeAdapter）测试环境。
func NewCodePlanEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *PlanEnv {
	t.Helper()
	env := NewAdapterTestEnv(t, mock, prefix)
	env.WriteConfigYAML(t, "")
	c := adapter.NewCodeAdapter()
	if err := c.CreateInstance(context.Background(), map[string]any{
		"project_dir": filepath.Join(env.DataDir, "myproj"),
	}, "agent", ""); err != nil {
		t.Fatalf("创建 CodeAdapter 失败: %v", err)
	}
	t.Cleanup(func() { _ = c.Cleanup() })
	return &PlanEnv{T: t, Env: env, Adapter: c, Mock: mock, ChannelID: planEnvChannel}
}

// Send 发送一轮 chat.send 请求。
//
// extra 会合并进 params（如 plan_entry_source / sub_mode / resume）。
func (e *PlanEnv) Send(sessionID, mode, query string, extra map[string]any) *swarmschema.AgentResponse {
	e.T.Helper()
	params := map[string]any{
		"query":           query,
		"mode":            mode,
		"conversation_id": sessionID,
	}
	for k, v := range extra {
		params[k] = v
	}
	raw, err := json.Marshal(params)
	if err != nil {
		e.T.Fatalf("序列化 params 失败: %v", err)
	}
	req := swarmschema.NewAgentRequest(
		"req-"+sessionID, e.ChannelID, swarmschema.ReqMethodChatSend, raw,
		swarmschema.WithAgentSessionID(sessionID),
	)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           query,
		"channel":         e.ChannelID,
		"language":        "zh",
	}
	resp, err := e.Adapter.ProcessMessageImpl(context.Background(), req, inputs)
	if err != nil {
		e.T.Fatalf("ProcessMessageImpl 失败: %v", err)
	}
	return resp
}

// SendNonChat 以非 chat 方法发送请求（验证跳过矩阵）。
func (e *PlanEnv) SendNonChat(sessionID, method, mode string) {
	e.T.Helper()
	raw, err := json.Marshal(map[string]any{"query": "x", "mode": mode})
	if err != nil {
		e.T.Fatalf("序列化 params 失败: %v", err)
	}
	req := swarmschema.NewAgentRequest(
		"req-nonchat-"+sessionID, e.ChannelID, swarmschema.ReqMethod(method), raw,
		swarmschema.WithAgentSessionID(sessionID),
	)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           "x",
		"channel":         e.ChannelID,
		"language":        "zh",
	}
	if _, err := e.Adapter.ProcessMessageImpl(context.Background(), req, inputs); err != nil {
		e.T.Fatalf("非 chat 请求失败: %v", err)
	}
}

// PlanMode 读取会话持久化的 plan 模式（空串表示尚未写过状态）。
func (e *PlanEnv) PlanMode(sessionID string) string {
	e.T.Helper()
	mode, _ := e.planState(sessionID)
	return mode
}

// PlanSlug 读取会话持久化的 plan 文件短名。
func (e *PlanEnv) PlanSlug(sessionID string) string {
	e.T.Helper()
	_, slug := e.planState(sessionID)
	return slug
}

// Events 取出并清空会话控制事件。
func (e *PlanEnv) Events(sessionID string) []map[string]any {
	e.T.Helper()
	if e.Deep == nil {
		e.T.Fatal("只有 agent 族适配器提供控制事件")
	}
	raw := e.Deep.DrainControlEvents(sessionID)
	out := make([]map[string]any, 0, len(raw))
	for _, ev := range raw {
		out = append(out, map[string]any{"event_type": ev.EventType, "mode": ev.Mode})
	}
	return out
}

// HasEvent 判断会话事件里是否出现指定 event_type。
func (e *PlanEnv) HasEvent(sessionID, eventType string) bool {
	e.T.Helper()
	for _, ev := range e.Events(sessionID) {
		if ev["event_type"] == eventType {
			return true
		}
	}
	return false
}

// Controller 返回计划模式控制器（agent 族专用）。
func (e *PlanEnv) Controller() *adapter.DeepAdapter { return e.Deep }

// HasSection 判断系统提示词构建器里是否存在某节。
func (e *PlanEnv) HasSection(name string) bool {
	e.T.Helper()
	inst := e.Adapter.Instance()
	if inst == nil || inst.SystemPromptBuilder() == nil {
		return false
	}
	return inst.SystemPromptBuilder().HasSection(name)
}

// ToolVisible 判断工具是否已注册到 AbilityManager。
func (e *PlanEnv) ToolVisible(name string) bool {
	e.T.Helper()
	inst := e.Adapter.Instance()
	if inst == nil || inst.AbilityManager() == nil {
		return false
	}
	return inst.AbilityManager().Get(name) != nil
}

// LastMessages 返回最近一次模型调用的实收消息（优先流式记录）。
func (e *PlanEnv) LastMessages() []llmschema.BaseMessage {
	e.T.Helper()
	return e.lastMessagesParam().Messages()
}

// LastSystemText 返回最近一次模型调用实收的系统提示词。
func (e *PlanEnv) LastSystemText() string {
	e.T.Helper()
	for _, msg := range e.LastMessages() {
		if msg.GetRole() == llmschema.RoleTypeSystem {
			return msg.GetContent().Text()
		}
	}
	return ""
}

// LastUserText 返回最近一次模型调用实收的最后一条用户消息文本。
func (e *PlanEnv) LastUserText() string {
	e.T.Helper()
	messages := e.LastMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].GetRole() == llmschema.RoleTypeUser {
			return messages[i].GetContent().Text()
		}
	}
	return ""
}

// AttachmentMessages 返回最近一次模型窗口里带附件标记的消息。
func (e *PlanEnv) AttachmentMessages() []llmschema.BaseMessage {
	e.T.Helper()
	out := []llmschema.BaseMessage{}
	for _, msg := range e.LastMessages() {
		meta := msg.GetMetadata()
		if meta == nil {
			continue
		}
		if marked, _ := meta["_openjiuwen_prompt_attachment_history"].(bool); !marked {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// LastRealUserText 返回最近一次模型窗口里最后一条「真实用户」消息（跳过附件伪用户消息）。
func (e *PlanEnv) LastRealUserText() string {
	e.T.Helper()
	messages := e.LastMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.GetRole() != llmschema.RoleTypeUser {
			continue
		}
		if hprompts.IsPromptAttachmentUser(msg) {
			continue
		}
		return msg.GetContent().Text()
	}
	return ""
}

// HasAttachmentContaining 判断最近一次模型窗口里是否有附件消息包含指定文本。
func (e *PlanEnv) HasAttachmentContaining(substr string) bool {
	e.T.Helper()
	for _, msg := range e.AttachmentMessages() {
		if strings.Contains(msg.GetContent().Text(), substr) {
			return true
		}
	}
	return false
}

// CountAttachmentsContaining 统计窗口里包含指定文本的附件消息数。
func (e *PlanEnv) CountAttachmentsContaining(substr string) int {
	e.T.Helper()
	count := 0
	for _, msg := range e.AttachmentMessages() {
		if strings.Contains(msg.GetContent().Text(), substr) {
			count++
		}
	}
	return count
}

// SeedState 预置会话 plan 状态（模拟历史残留 plan_slug / 已退出等场景）。
func (e *PlanEnv) SeedState(sessionID, mode, planSlug string) {
	e.T.Helper()
	sess := e.sessionFacade(sessionID)
	inst := e.Adapter.Instance()
	state := inst.LoadState(sess)
	state.PlanMode.Mode = mode
	state.PlanMode.PlanSlug = planSlug
	inst.SaveState(sess, state)
	if err := sess.PostRun(context.Background()); err != nil {
		e.T.Fatalf("会话 PostRun 失败: %v", err)
	}
}

// SwitchMode 直接切会话状态（模拟 exit_plan_mode 工具的效果）。
func (e *PlanEnv) SwitchMode(sessionID, mode string) {
	e.T.Helper()
	if e.Deep == nil {
		e.T.Fatal("只有 agent 族适配器可切模式")
	}
	if err := e.Deep.SwitchMode(context.Background(), sessionID, mode); err != nil {
		e.T.Fatalf("SwitchMode 失败: %v", err)
	}
}

// MarkExitedByPostProcess 走 check_post_process_exit 记录退出记忆（一次性令牌）。
func (e *PlanEnv) MarkExitedByPostProcess(sessionID, mode string) []swruntime.ControlEvent {
	e.T.Helper()
	if e.Deep == nil {
		e.T.Fatal("只有 agent 族适配器有 plan 控制器")
	}
	events, err := e.Deep.PlanController().CheckPostProcessExit(context.Background(), swruntime.PlanRequest{
		SessionID: sessionID,
		Mode:      mode,
		ReqMethod: "chat.send",
	})
	if err != nil {
		e.T.Fatalf("CheckPostProcessExit 失败: %v", err)
	}
	return events
}

// RailCount 返回实例上指定类型的 rail 数量。
func (e *PlanEnv) RailCount(railType reflect.Type) int {
	e.T.Helper()
	inst := e.Adapter.Instance()
	if inst == nil {
		return 0
	}
	return len(inst.FindRailsByType(railType))
}

// AttachmentOf 返回管理器里指定 section 的附件（不存在返回 nil）。
func (e *PlanEnv) AttachmentOf(sessionID, section string) *hprompts.Attachment {
	e.T.Helper()
	inst := e.Adapter.Instance()
	if inst == nil || inst.PromptAttachments() == nil {
		return nil
	}
	items, err := inst.PromptAttachments().ListByFilter(hprompts.AttachmentFilter{
		SessionID: sessionID,
		Section:   section,
	})
	if err != nil {
		e.T.Fatalf("读取附件失败: %v", err)
	}
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

// AttachmentCounts 返回管理器里全部附件（按 section）。
func (e *PlanEnv) Attachments(sessionID string) []*hprompts.Attachment {
	e.T.Helper()
	inst := e.Adapter.Instance()
	if inst == nil || inst.PromptAttachments() == nil {
		return nil
	}
	items, err := inst.PromptAttachments().ListByFilter(hprompts.AttachmentFilter{SessionID: sessionID})
	if err != nil {
		e.T.Fatalf("读取附件失败: %v", err)
	}
	return items
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// planState 读取会话 plan 状态（模式 + 计划短名）。
func (e *PlanEnv) planState(sessionID string) (string, string) {
	e.T.Helper()
	sess := e.sessionFacade(sessionID)
	inst := e.Adapter.Instance()
	if inst == nil {
		e.T.Fatal("实例未就绪")
	}
	state := inst.LoadState(sess)
	return state.PlanMode.Mode, state.PlanMode.PlanSlug
}

// sessionFacade 预置/复用会话门面（LoadState 需要）。
func (e *PlanEnv) sessionFacade(sessionID string) *session.Session {
	e.T.Helper()
	inst := e.Adapter.Instance()
	if inst == nil {
		e.T.Fatal("实例未就绪")
	}
	card := inst.Card()
	if card == nil {
		e.T.Fatal("agent card 为空")
	}
	sess := session.CreateAgentSession(sessionID, card, nil)
	if err := sess.PreRun(context.Background()); err != nil {
		e.T.Fatalf("会话 PreRun 失败: %v", err)
	}
	return sess
}

// lastMessagesParam 取最近一次模型调用入参。
func (e *PlanEnv) lastMessagesParam() model_clients.MessagesParam {
	e.T.Helper()
	if rec := e.Mock.LastStreamCall(); rec != nil {
		return rec.Messages
	}
	if rec := e.Mock.LastInvokeCall(); rec != nil {
		return rec.Messages
	}
	return model_clients.MessagesParam{}
}

// WriteRawConfig 覆盖写入 config.yaml（需要自定义 react/modes 段时使用）。
func (e *PlanEnv) WriteRawConfig(content string) {
	e.T.Helper()
	if err := os.WriteFile(filepath.Join(e.Env.DataDir, "config", "config.yaml"), []byte(content), 0o644); err != nil {
		e.T.Fatalf("写 config.yaml 失败: %v", err)
	}
}

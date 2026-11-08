//go:build integration

package testhelpers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	srvruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanEnv 计划模式（S3）集成测试环境。
//
// 复用隔离三件套 + MockLLM provider 注册，从 **AgentServer 入口** 驱动请求
// （分发 → AgentRuntime → adapter），不再手工补 plan 同步与 query 回写；
// 断言助手覆盖「发一轮 chat → 读 plan 状态 / 控制事件 / 模型实收窗口」。
type PlanEnv struct {
	// T 测试句柄
	T *testing.T
	// Env 底层隔离环境（DataDir / Provider）
	Env *AdapterTestEnv
	// Server 进程内 AgentServer 环境（E2A 投递 + 帧收集）
	Server *ServerEnv
	// Agent 最近一次被工厂创建的 agent（供实例级断言）
	Agent *srvruntime.UapClawSwarm
	// Mock 模型桩（与 BaseIntegrationSuite.MockLLM 同一实例）
	Mock *mockllm.MockModelClient
	// ChannelID 请求渠道
	ChannelID string
	// ProjectDir 工程目录（非空时每轮请求都带上，保证 agent 缓存键稳定）
	ProjectDir string
	// WorkMode work / code（非空时每轮请求都带上，决定 agent 族）
	WorkMode string
	// FactoryMode 传给 CreateInstance 的模式（空=沿用 manager 解析值）。
	//
	// code 族的 IT 配置把 modes.code.tools 置空以精简用例，若沿用 "code" 会拿不到
	// todo 工具；基线用例一直用 "agent" 读 modes.agent，这里保持一致。
	FactoryMode string
	// agentMu 保护 Agent / 最近一轮 agent 标识的并发读写
	agentMu sync.Mutex
	// lastMode 最近一轮解析出的一级模式
	lastMode string
	// lastSessionID 最近一轮请求的会话标识（用于取该会话的 live instance）
	lastSessionID string
	// lastSubMode 最近一轮解析出的子模式
	lastSubMode string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// planEnvChannel 集成测试渠道标识
	planEnvChannel = "itest-channel"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlanEnv 构建 agent 族测试环境（AgentServer 入口驱动）。
func NewPlanEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *PlanEnv {
	t.Helper()
	env := NewAdapterTestEnv(t, mock, prefix)
	return newPlanEnvWithEnv(t, env, mock, "", "work")
}

// NewCodePlanEnv 构建 code 族测试环境（project_dir 指向临时工程）。
func NewCodePlanEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *PlanEnv {
	t.Helper()
	env := NewAdapterTestEnv(t, mock, prefix)
	return newPlanEnvWithEnv(t, env, mock, filepath.Join(env.DataDir, "myproj"), "code")
}

// Send 发送一轮 chat.send 请求（非流式）。
//
// extra 会合并进 params（如 plan_entry_source / sub_mode / resume）。
func (e *PlanEnv) Send(sessionID, mode, query string, extra map[string]any) *swarmschema.AgentResponse {
	e.T.Helper()
	requestID := e.send(sessionID, mode, query, extra, false)
	return e.Server.AwaitUnary(requestID)
}

// SendStream 以流式方式发送一轮 chat.send，返回收到的全部 chunk。
func (e *PlanEnv) SendStream(sessionID, mode, query string, extra map[string]any) []*swarmschema.AgentResponseChunk {
	e.T.Helper()
	requestID := e.send(sessionID, mode, query, extra, true)
	return e.Server.AwaitStreamEnd(requestID)
}

// SendNonChat 以非 chat 方法发送请求（验证跳过矩阵）。
func (e *PlanEnv) SendNonChat(sessionID, method, mode string) {
	e.T.Helper()
	params := map[string]any{"query": "x", "mode": mode, "conversation_id": sessionID}
	requestID := e.Server.Send(method, params, sessionID, false)
	e.Server.AwaitUnary(requestID)
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

// Events 返回 push 帧里的控制事件（event_type + mode）。
func (e *PlanEnv) Events(_ string) []map[string]any {
	e.T.Helper()
	out := []map[string]any{}
	for _, push := range e.Server.Pushes() {
		payload := PushPayload(push)
		if payload == nil {
			continue
		}
		out = append(out, map[string]any{
			"event_type": payload["event_type"],
			"mode":       payload["mode"],
		})
	}
	return out
}

// HasEvent 判断 push 帧里是否出现指定 event_type。
func (e *PlanEnv) HasEvent(_ string, eventType string) bool {
	e.T.Helper()
	for _, push := range e.Server.Pushes() {
		payload := PushPayload(push)
		if payload == nil {
			continue
		}
		if payload["event_type"] == eventType {
			return true
		}
	}
	return false
}

// Inst 返回装配后的 DeepAgent 实例（实例级断言用）。
func (e *PlanEnv) Inst() *harness.DeepAgent {
	e.T.Helper()
	return e.inst()
}

// PlanController 返回服务端的计划模式控制器（进程内唯一，对齐 Python _SERVER_PLAN_CONTROLLER）。
func (e *PlanEnv) PlanController() *swruntime.PlanModeController {
	e.T.Helper()
	return e.Server.Server.PlanController()
}

// HasSection 判断系统提示词构建器里是否存在某节。
func (e *PlanEnv) HasSection(name string) bool {
	e.T.Helper()
	inst := e.inst()
	if inst == nil || inst.SystemPromptBuilder() == nil {
		return false
	}
	return inst.SystemPromptBuilder().HasSection(name)
}

// ToolVisible 判断工具是否已注册到 AbilityManager。
func (e *PlanEnv) ToolVisible(name string) bool {
	e.T.Helper()
	inst := e.inst()
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
	inst := e.instForState()
	sess := e.sessionFacade(sessionID)
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
	if e.Agent == nil {
		e.T.Fatal("agent 尚未创建")
	}
	if err := e.Agent.SwitchMode(context.Background(), sessionID, mode); err != nil {
		e.T.Fatalf("SwitchMode 失败: %v", err)
	}
}

// MarkExitedByPostProcess 走 check_post_process_exit 记录退出记忆（一次性令牌）。
func (e *PlanEnv) MarkExitedByPostProcess(sessionID, mode string) []swruntime.ControlEvent {
	e.T.Helper()
	agent := e.agentRef()
	if agent == nil {
		e.T.Fatal("agent 尚未创建")
	}
	events, err := e.PlanController().CheckPostProcessExit(context.Background(), swruntime.PlanRequest{
		SessionID: sessionID,
		Mode:      mode,
		ReqMethod: "chat.send",
	}, agent.PlanStateAccessor())
	if err != nil {
		e.T.Fatalf("CheckPostProcessExit 失败: %v", err)
	}
	return events
}

// RailCount 返回实例上指定类型的 rail 数量。
func (e *PlanEnv) RailCount(railType reflect.Type) int {
	e.T.Helper()
	inst := e.inst()
	if inst == nil {
		return 0
	}
	return len(inst.FindRailsByType(railType))
}

// AttachmentOf 返回管理器里指定 section 的附件（不存在返回 nil）。
func (e *PlanEnv) AttachmentOf(sessionID, section string) *hprompts.Attachment {
	e.T.Helper()
	inst := e.inst()
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

// Attachments 返回管理器里全部附件（按 session）。
func (e *PlanEnv) Attachments(sessionID string) []*hprompts.Attachment {
	e.T.Helper()
	inst := e.inst()
	if inst == nil || inst.PromptAttachments() == nil {
		return nil
	}
	items, err := inst.PromptAttachments().ListByFilter(hprompts.AttachmentFilter{SessionID: sessionID})
	if err != nil {
		e.T.Fatalf("读取附件失败: %v", err)
	}
	return items
}

// WriteRawConfig 覆盖写入 config.yaml（需要自定义 react/modes 段时使用）。
func (e *PlanEnv) WriteRawConfig(content string) {
	e.T.Helper()
	if err := os.WriteFile(filepath.Join(e.Env.DataDir, "config", "config.yaml"), []byte(content), 0o644); err != nil {
		e.T.Fatalf("写 config.yaml 失败: %v", err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPlanEnvWithEnv 在既有隔离环境上启动 AgentServer 并接上捕获工厂。
func newPlanEnvWithEnv(t *testing.T, env *AdapterTestEnv, mock *mockllm.MockModelClient, projectDir, workMode string) *PlanEnv {
	t.Helper()
	env.WriteConfigYAML(t, "")
	cfg, err := config.New("")
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if _, err := cfg.Load(); err != nil {
		t.Fatalf("读取 config.yaml 失败: %v", err)
	}

	planEnv := &PlanEnv{
		T: t, Env: env, Mock: mock,
		ChannelID: planEnvChannel, ProjectDir: projectDir, WorkMode: workMode,
	}
	planEnv.Server = NewServerEnv(t, cfg, func(agentConfig map[string]any, mode, subMode string) (*srvruntime.UapClawSwarm, error) {
		merged := map[string]any{}
		for k, v := range agentConfig {
			merged[k] = v
		}
		agent := srvruntime.NewUapClaw()
		instanceMode := mode
		if override := planEnv.factoryMode(); override != "" {
			instanceMode = override
		}
		if err := agent.CreateInstance(
			srvruntime.WithCreateInstanceConfig(merged),
			srvruntime.WithCreateInstanceMode(instanceMode),
			srvruntime.WithCreateInstanceSubMode(subMode),
		); err != nil {
			return nil, fmt.Errorf("创建 agent 失败: %w", err)
		}
		planEnv.agentMu.Lock()
		planEnv.Agent = agent
		planEnv.agentMu.Unlock()
		return agent, nil
	})
	planEnv.Server.ChannelID = planEnvChannel

	// 预创建 agent：实例级断言（SeedState / Inst）在首轮请求之前也要可用。
	agentMode := "agent"
	if workMode == "code" {
		agentMode = "code"
	}
	planEnv.FactoryMode = agentMode
	if _, err := planEnv.Server.Server.AgentManager().GetAgent(
		context.Background(), planEnvChannel, agentMode, projectDir, ""); err != nil {
		t.Fatalf("预创建 agent 失败: %v", err)
	}
	return planEnv
}

// send 组装并投递一轮 chat.send。
func (e *PlanEnv) send(sessionID, mode, query string, extra map[string]any, isStream bool) string {
	e.T.Helper()
	params := map[string]any{
		"query":           query,
		"mode":            mode,
		"conversation_id": sessionID,
		"channel":         e.ChannelID,
		"language":        "zh",
	}
	for k, v := range extra {
		params[k] = v
	}
	if e.ProjectDir != "" {
		params["project_dir"] = e.ProjectDir
	}
	if e.WorkMode != "" {
		params["work_mode"] = e.WorkMode
	}
	e.trackMode(params)
	e.agentMu.Lock()
	e.lastSessionID = sessionID
	e.agentMu.Unlock()
	return e.Server.Send(string(swarmschema.ReqMethodChatSend), params, sessionID, isStream)
}

// factoryMode 返回 CreateInstance 应使用的模式覆盖值。
func (e *PlanEnv) factoryMode() string {
	e.agentMu.Lock()
	defer e.agentMu.Unlock()
	return e.FactoryMode
}

// trackMode 记录本轮解析出的 (mode, subMode)，供 Inst 定位实际承接的 agent。
func (e *PlanEnv) trackMode(params map[string]any) {
	raw, err := json.Marshal(params)
	if err != nil {
		return
	}
	req := &swarmschema.AgentRequest{ChannelID: e.ChannelID, Params: raw}
	resolved := swruntime.ResolveRequestRuntimeModeFromRequest(req)
	e.agentMu.Lock()
	e.lastMode, e.lastSubMode = resolved.ManagerMode, resolved.SubMode
	e.agentMu.Unlock()
}

// agentRef 返回最近一轮请求实际使用的 agent。
//
// 注意：Go 的 agent 缓存键含 sub_mode，plan 与 normal 轮会是不同实例
// （见计划「已知偏差」），因此实例级断言必须按本轮 mode/subMode 取。
func (e *PlanEnv) agentRef() *srvruntime.UapClawSwarm {
	e.agentMu.Lock()
	mode, subMode := e.lastMode, e.lastSubMode
	fallback := e.Agent
	e.agentMu.Unlock()
	if mode != "" {
		if agent := e.Server.Server.AgentManager().GetAgentNoWait(e.ChannelID, mode, e.ProjectDir, subMode); agent != nil {
			return agent
		}
	}
	return fallback
}

// instForState 返回可用于读写会话状态的 DeepAgent。
//
// 10.3.28f：首轮对话之前还没有 session child，此时按上游 plan.py:119 的
// 回退顺序（get_live_session_instance 取不到 → ensure_live_session_instance）
// 让根适配器惰建出自己的 DeepAgent。这与"根是纯 router"并不矛盾：
// 只有需要会话态句柄的路径才会触发惰建，chat 路径永远只建 child。
func (e *PlanEnv) instForState() *harness.DeepAgent {
	e.T.Helper()
	agent := e.agentRef()
	if agent == nil {
		e.T.Fatal("agent 未就绪")
	}
	e.agentMu.Lock()
	sid := e.lastSessionID
	e.agentMu.Unlock()
	if sid != "" {
		if live := agent.GetLiveSessionInstance(sid); live != nil {
			return live
		}
	}
	// 与上游同序：get_live_session_instance → ensure_live_session_instance
	// （建会话 child），而不是 EnsureInstance（建本适配器自己的 DeepAgent）。
	if sid != "" {
		inst, err := agent.EnsureLiveSessionInstance(context.Background(), sid)
		if err != nil {
			e.T.Fatalf("确保会话 DeepAgent 实例失败: %v", err)
		}
		if inst != nil {
			return inst
		}
	}
	inst, err := agent.EnsureInstance(context.Background())
	if err != nil {
		e.T.Fatalf("确保 DeepAgent 实例失败: %v", err)
	}
	if inst == nil {
		e.T.Fatal("实例未就绪")
	}
	return inst
}

// inst 返回装配后的 DeepAgent 实例。
//
// 10.3.28f：会话分身后根 adapter 是纯 router，不再持有 DeepAgent
// （对齐 Python _skip_own_instance_build :13109）；真正跑对话的
// DeepAgent 挂在各会话的 child 上。因此优先取"该会话的 live 实例"，
// 取不到才回退根的 GetInstance（根此时通常为 nil）。
func (e *PlanEnv) inst() *harness.DeepAgent {
	e.T.Helper()
	agent := e.agentRef()
	if agent == nil {
		return nil
	}
	e.agentMu.Lock()
	sid := e.lastSessionID
	e.agentMu.Unlock()
	if sid != "" {
		if live := agent.GetLiveSessionInstance(sid); live != nil {
			return live
		}
	}
	return agent.GetInstance()
}

// planState 读取会话 plan 状态（模式 + 计划短名）。
func (e *PlanEnv) planState(sessionID string) (string, string) {
	e.T.Helper()
	inst := e.inst()
	if inst == nil {
		e.T.Fatal("实例未就绪")
	}
	sess := e.sessionFacade(sessionID)
	state := inst.LoadState(sess)
	return state.PlanMode.Mode, state.PlanMode.PlanSlug
}

// sessionFacade 预置/复用会话门面（LoadState 需要）。
func (e *PlanEnv) sessionFacade(sessionID string) *session.Session {
	e.T.Helper()
	inst := e.instForState()
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

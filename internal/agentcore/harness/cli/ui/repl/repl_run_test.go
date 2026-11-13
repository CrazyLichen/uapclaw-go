package repl

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/agent"
	clirails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/storage"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// fakeBackend 可编程后端：RunStreaming 返回预设帧。
type fakeBackend struct {
	// mu 保护以下字段的并发访问（Abort/Stop 由后台 goroutine 调用，CI race 实证）
	mu sync.Mutex
	// responses 每次 RunStreaming 弹出的帧组
	responses [][]stream.OutputSchema
	// queries 收到的 query
	queries []any
	// aborted 是否被 Abort（atomic：Abort 由 Handle 异步 goroutine 调用，测试侧并发读取）
	aborted atomic.Int32
	// started / stopped 启停标记
	started, stopped bool
}

// abortedCount 读取 aborted 计数（atomic，无需加锁）。
func (f *fakeBackend) abortedCount() int {
	return int(f.aborted.Load())
}

// queriesSnapshot 持锁拷贝 queries。
func (f *fakeBackend) queriesSnapshot() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.queries...)
}

func (f *fakeBackend) Start(_ context.Context) error {
	f.started = true
	return nil
}

func (f *fakeBackend) Stop(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}

func (f *fakeBackend) RunStreaming(_ context.Context, query any, _ *string) (<-chan stream.OutputSchema, error) {
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.mu.Unlock()
	ch := make(chan stream.OutputSchema)
	var frames []stream.OutputSchema
	if len(f.responses) > 0 {
		frames = f.responses[0]
		f.responses = f.responses[1:]
	}
	go func() {
		for _, frame := range frames {
			ch <- frame
		}
		close(ch)
	}()
	return ch, nil
}

func (f *fakeBackend) Abort(_ context.Context) error {
	f.aborted.Add(1)
	return nil
}

// askRequest 构造 ask_user 中断请求。
func askRequest(question string) *saschema.ToolCallInterruptRequest {
	ask := &askUserRequestAlias{}
	ask.InterruptKind = saschema.InterruptKindAskUser
	ask.InterruptID = "int-1"
	ask.Questions = []map[string]any{{"question": question}}
	return saschema.NewToolCallInterruptRequest(ask, &llmschema.ToolCall{ID: "call-1", Name: "ask_user"})
}

// approvalRequest 构造 permission 审批请求。
func approvalRequest() *saschema.ToolCallInterruptRequest {
	req := &saschema.InterruptRequest{}
	req.InterruptKind = saschema.InterruptKindPermission
	req.InterruptID = "int-2"
	req.Message = "要写文件"
	return saschema.NewToolCallInterruptRequest(req, &llmschema.ToolCall{ID: "call-2", Name: "write_file"})
}

// TestInterruptManager_三层与窗口窗口期内 1→abort / 2→warn / 3→exit。
func TestInterruptManager_三层与窗口(t *testing.T) {
	backend := &fakeBackend{}
	clock := 0.0
	mgr := &InterruptManager{now: func() float64 { clock += 0.1; return clock }}

	assert.Equal(t, "abort", mgr.Handle(backend))
	assert.Eventually(t, func() bool { return backend.abortedCount() == 1 }, time.Second, 10*time.Millisecond,
		"第一次触发即中止后端（ensure_future 语义，异步执行）")
	assert.Equal(t, "warn", mgr.Handle(backend))
	assert.Equal(t, "exit", mgr.Handle(backend))

	// 窗口外重置
	clock += 10.0
	assert.Equal(t, "abort", mgr.Handle(backend))
}

// TestRenderInteraction_各kind ask_user / 审批 / 技能演化 / 自由形态。
func TestRenderInteraction_各kind(t *testing.T) {
	var out bytes.Buffer

	renderInteraction(askRequest("叫什么"), &out)
	assert.Contains(t, out.String(), "Agent needs your input")
	assert.Contains(t, out.String(), "叫什么")

	out.Reset()
	renderInteraction(approvalRequest(), &out)
	assert.Contains(t, out.String(), "⚠ Approve")
	assert.Contains(t, out.String(), "y/yes=approve")

	out.Reset()
	evolution := &saschema.InterruptRequest{}
	evolution.InterruptKind = saschema.InterruptKindSkillEvolutionApproval
	evolution.InterruptID = "int-3"
	evolution.Message = "要更新技能"
	renderInteraction(saschema.NewToolCallInterruptRequest(evolution, &llmschema.ToolCall{ID: "call-3", Name: "evolve_skill"}), &out)
	assert.Contains(t, out.String(), "Skill evolution approval")

	out.Reset()
	renderInteraction("纯文本问题", &out)
	assert.Contains(t, out.String(), "纯文本问题")
}

// TestAnswerForInteraction_审批回车默认同意 空输入按 y 处理。
func TestAnswerForInteraction_审批回车默认同意(t *testing.T) {
	r := &REPL{}
	lines := []string{""}
	idx := 0
	promptFn := func(string) (string, error) {
		line := lines[idx]
		idx++
		return line, nil
	}
	answer, err := r.answerForInteraction(approvalRequest(), promptFn)
	require.NoError(t, err)
	m, ok := answer.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, m["approved"])
}

// TestAnswerForInteraction_askUser 逐问题收集。
func TestAnswerForInteraction_askUser(t *testing.T) {
	r := &REPL{}
	answers := []string{"张三"}
	idx := 0
	promptFn := func(string) (string, error) {
		line := answers[idx]
		idx++
		return line, nil
	}
	answer, err := r.answerForInteraction(askRequest("叫什么"), promptFn)
	require.NoError(t, err)
	m := answer.(map[string]any)
	inner := m["answers"].(map[string]any)
	assert.Equal(t, "张三", inner["叫什么"])
}

// TestAnswerFreeForm_JSON优先 能解析成 JSON 就用 JSON。
func TestAnswerFreeForm_JSON优先(t *testing.T) {
	r := &REPL{}
	answer, err := r.answerFreeForm(func(string) (string, error) { return `{"a":1}`, nil })
	require.NoError(t, err)
	m, ok := answer.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(1), m["a"])

	answer, err = r.answerFreeForm(func(string) (string, error) { return "纯文本", nil })
	require.NoError(t, err)
	assert.Equal(t, "纯文本", answer)
}

// TestCmdHandlers_help_status_cost_sessions slash 处理器输出。
func TestCmdHandlers_help_status_cost_sessions(t *testing.T) {
	var out bytes.Buffer
	cmdHelp(&out)
	assert.Contains(t, out.String(), "/help")
	assert.Contains(t, out.String(), "! <cmd>")

	out.Reset()
	cmdStatus(&out, nil, agent.CLIConfig{Model: "gpt-4o", Provider: "OpenAI"})
	assert.Contains(t, out.String(), "gpt-4o (OpenAI)")

	out.Reset()
	cmdCost(&out, nil)
	assert.Contains(t, out.String(), "No token data available")

	out.Reset()
	cmdCompact(&out)
	assert.Contains(t, out.String(), "not yet implemented in MVP")

	out.Reset()
	cmdSessions(&out, nil)
	assert.Contains(t, out.String(), "No session store")
}

// TestCmdAutoHarness_子命令桩 四个子命令走桩提示；空走 usage。
func TestCmdAutoHarness_子命令桩(t *testing.T) {
	var out bytes.Buffer
	cmdAutoHarness(&out, "run --task x", agent.CLIConfig{})
	assert.Contains(t, out.String(), "领域十四")

	out.Reset()
	cmdAutoHarness(&out, "", agent.CLIConfig{})
	assert.Contains(t, out.String(), "Usage:")
}

// TestHandleSlash_内置与未知 内置命令分派；未知命令提示。
func TestHandleSlash_内置与未知(t *testing.T) {
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	r := &REPL{Cfg: agent.CLIConfig{Model: "m"}, Store: store}
	var out bytes.Buffer

	// /cost 无 tracker
	skill, err := r.handleSlash(context.Background(), "/cost", &out)
	require.NoError(t, err)
	assert.Empty(t, skill)
	assert.Contains(t, out.String(), "No token data")

	// /exit 哨兵
	_, err = r.handleSlash(context.Background(), "/exit", &out)
	assert.ErrorIs(t, err, errExitREPL)

	// /quit 别名
	_, err = r.handleSlash(context.Background(), "/quit", &out)
	assert.ErrorIs(t, err, errExitREPL)

	// 未知命令
	out.Reset()
	skill, err = r.handleSlash(context.Background(), "/nope", &out)
	require.NoError(t, err)
	assert.Empty(t, skill)
	assert.Contains(t, out.String(), "Unknown command: /nope")
}

// TestHandleSlash_技能命令 注册技能后返回命令名。
func TestHandleSlash_技能命令(t *testing.T) {
	ResetSkillCommands()
	defer ResetSkillCommands()
	skillMDPaths["/my-skill"] = "/tmp/SKILL.md"
	defer delete(skillMDPaths, "/my-skill")
	RegisterSkillCommand("/my-skill", "做某事")

	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	r := &REPL{Store: store}
	var out bytes.Buffer
	skill, err := r.handleSlash(context.Background(), "/my-skill 参数", &out)
	require.NoError(t, err)
	assert.Equal(t, "/my-skill", skill)
}

// TestRunREPL_主循环_slash与查询 假后端跑完整主循环：欢迎横幅、
// slash 分派、普通查询渲染、退出。
func TestRunREPL_主循环_slash与查询(t *testing.T) {
	ResetSkillCommands()
	defer ResetSkillCommands()
	backend := &fakeBackend{responses: [][]stream.OutputSchema{
		{
			{Type: "llm_output", Payload: map[string]any{"content": "你好"}},
			{Type: "llm_output", IsLastSchema: true, Payload: map[string]any{"content": "！"}},
		},
	}}
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	store.NewSession("sess-1", "m")

	input := strings.Join([]string{"/help", "查询一下", "/exit"}, "\n") + "\n"
	var out bytes.Buffer
	r := &REPL{
		Backend: backend,
		Cfg:     agent.CLIConfig{Model: "m", Provider: "p", Cwd: "/tmp"},
		Store:   store,
		In:      strings.NewReader(input),
		Out:     &out,
	}
	require.NoError(t, r.RunREPL(context.Background()))

	got := out.String()
	assert.Contains(t, got, "Welcome to OpenJiuWen")
	assert.Contains(t, got, "/help")
	assert.Contains(t, got, "你好！")
	assert.Contains(t, got, "Goodbye!")
	qs := backend.queriesSnapshot()
	require.Len(t, qs, 1)
	assert.Equal(t, "查询一下", qs[0])
	// 会话记录：user + assistant 各一条
	messages := store.ListSessions()
	require.Len(t, messages, 1)
}

// TestRunREPL_EOF退出 输入耗尽即退出。
func TestRunREPL_EOF退出(t *testing.T) {
	backend := &fakeBackend{}
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	var out bytes.Buffer
	r := &REPL{Backend: backend, Cfg: agent.CLIConfig{}, Store: store,
		In: strings.NewReader(""), Out: &out}
	require.NoError(t, r.RunREPL(context.Background()))
	assert.Contains(t, out.String(), "Welcome to OpenJiuWen")
	assert.Empty(t, backend.queriesSnapshot())
}

// TestRunREPL_中断恢复循环 第一轮挂起交互 → 应答 → 第二轮恢复。
func TestRunREPL_中断恢复循环(t *testing.T) {
	backend := &fakeBackend{responses: [][]stream.OutputSchema{
		{ // 第一轮：交互块挂起
			{Type: "__interaction__", Payload: map[string]any{
				"interaction_id": "int-1",
				"payload":        askRequest("叫什么"),
			}},
		},
		{ // 恢复轮：正常输出
			{Type: "llm_output", IsLastSchema: true, Payload: map[string]any{"content": "好的"}},
		},
	}}
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)

	input := strings.Join([]string{"开始", "张三"}, "\n") + "\n"
	var out bytes.Buffer
	r := &REPL{
		Backend: backend,
		Cfg:     agent.CLIConfig{},
		Store:   store,
		In:      strings.NewReader(input),
		Out:     &out,
	}
	require.NoError(t, r.RunREPL(context.Background()))

	got := out.String()
	assert.Contains(t, got, "Agent needs your input")
	assert.Contains(t, got, "好的")
	// 两次 RunStreaming：首轮 + InteractiveInput 恢复轮
	qs := backend.queriesSnapshot()
	require.Len(t, qs, 2)
	ii, ok := qs[1].(*interactiveInputAlias)
	require.True(t, ok, "恢复轮 query 应是 *InteractiveInput")
	assert.Equal(t, map[string]any{"answers": map[string]any{"叫什么": "张三"}}, ii.InterruptAnswers["int-1"])
}

// interactiveInputAlias interaction.InteractiveInput 的局部别名。
type interactiveInputAlias = interaction.InteractiveInput

// TestBuildSkillQuery_读写与缺失 文件缺失报错文案；存在则包 skill 标签。
func TestBuildSkillQuery_读写与缺失(t *testing.T) {
	assert.Contains(t, buildSkillQuery("/no/such/SKILL.md", ""), "Error reading skill file")

	path := t.TempDir() + "/SKILL.md"
	requireWrite(t, path, "# 技能\n步骤一")
	q := buildSkillQuery(path, "参数A")
	assert.Contains(t, q, "<skill-instructions>")
	assert.Contains(t, q, "步骤一")
	assert.Contains(t, q, "User request: 参数A")
}

// TestFormatInt_数值形态 int / int64 / float64 / 其他。
func TestFormatInt_数值形态(t *testing.T) {
	assert.Equal(t, "123", formatInt(123))
	assert.Equal(t, "456", formatInt(int64(456)))
	assert.Equal(t, "789", formatInt(float64(789)))
	assert.Equal(t, "raw", formatInt("raw"))
}

func requireWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 防止 interruptpkg 未使用
var _ = interruptpkg.IsApprovalKind
var _ = time.Now

// TestRunREPL_shell与技能命令 shell 透传 + 技能命令展开成 query 转发。
func TestRunREPL_shell与技能命令(t *testing.T) {
	ResetSkillCommands()
	defer ResetSkillCommands()
	ResetSkillMDPaths()
	defer ResetSkillMDPaths()

	skillMd := t.TempDir() + "/SKILL.md"
	requireWrite(t, skillMd, "# 技能\n做某事")
	RegisterSkillCommand("/my-skill", "做某事")
	RegisterSkillMDPath("/my-skill", skillMd)

	backend := &fakeBackend{responses: [][]stream.OutputSchema{
		{{Type: "llm_output", IsLastSchema: true, Payload: map[string]any{"content": "技能结果"}}},
	}}
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	store.NewSession("sess", "m")

	input := strings.Join([]string{"!true", "/my-skill 参数A", "/exit"}, "\n") + "\n"
	var out bytes.Buffer
	r := &REPL{Backend: backend, Cfg: agent.CLIConfig{}, Store: store,
		In: strings.NewReader(input), Out: &out}
	require.NoError(t, r.RunREPL(context.Background()))

	got := out.String()
	assert.Contains(t, got, "技能结果")
	require.Len(t, backend.queriesSnapshot(), 1)
	forwarded, _ := backend.queries[0].(string)
	assert.Contains(t, forwarded, "<skill-instructions>")
	assert.Contains(t, forwarded, "User request: 参数A")
}

// TestRunREPL_未启动后端报错 RunStreaming 报错时打印 ✗ Error。
func TestRunREPL_未启动后端报错(t *testing.T) {
	// fakeBackend.RunStreaming 总成功；用 errBackend 模拟 RunStreaming 失败
	be := &errBackend{}
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	var out bytes.Buffer
	r := &REPL{Backend: be, Cfg: agent.CLIConfig{}, Store: store,
		In: strings.NewReader("查询\n/exit\n"), Out: &out}
	require.NoError(t, r.RunREPL(context.Background()))
	assert.Contains(t, out.String(), "✗ Error:")
}

// errBackend RunStreaming 必失败的替身。
type errBackend struct {
	fakeBackend
}

func (e *errBackend) RunStreaming(context.Context, any, *string) (<-chan stream.OutputSchema, error) {
	return nil, errExitREPL
}

// TestCmdHandlers_带数据分支 tracker / store 有数据的展示分支 + 小助手。
func TestCmdHandlers_带数据分支(t *testing.T) {
	var out bytes.Buffer

	// /clear æ¸å±å½ä»¤åºæ¯
	cmdClear(&out)
	assert.Contains(t, out.String(), "\x1b[2J")

	// cmdStatus + cmdCost 带真实 tracker 数据
	tracker := clirails.NewTokenTrackingRail()
	cmdStatus(&out, tracker, agent.CLIConfig{})
	assert.Contains(t, out.String(), "Input tokens:")
	cmdCost(&out, tracker)
	assert.Contains(t, out.String(), "Calls:")

	// cmdSessions 带持久化会话
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	store.NewSession("s-1", "gpt-4o")
	store.AddMessage("user", "hi")
	cmdSessions(&out, store)
	assert.Contains(t, out.String(), "s-1")
	assert.Contains(t, out.String(), "gpt-4o")

	// cmdHelp 带技能命令
	ResetSkillCommands()
	defer ResetSkillCommands()
	RegisterSkillCommand("/zz-skill", "技能描述")
	cmdHelp(&out)
	assert.Contains(t, out.String(), "/zz-skill")

	// toString / printArgs / formatInt 边界
	assert.Equal(t, "", toString(nil))
	assert.Equal(t, "5", toString(5))
	printArgs(nil, &out)
	printArgs(`{"tool":"write_file","path":"`+strings.Repeat("x", 250)+`"}`, &out)
	assert.Contains(t, out.String(), "tool: write_file")
	assert.Contains(t, out.String(), "...")
	printArgs(map[string]any{"k": "v"}, &out)
	assert.Contains(t, out.String(), "k: v")

	// askQuestions 无 questions → 单个 Answer 提示
	r := &REPL{}
	promptFn := func(string) (string, error) { return "自由答案", nil }
	noQ := &askUserRequestAlias{}
	noQ.InterruptKind = saschema.InterruptKindAskUser
	noQ.InterruptID = "int-9"
	ans, err := r.answerQuestions(saschema.NewToolCallInterruptRequest(noQ, &llmschema.ToolCall{ID: "c9", Name: "ask_user"}), promptFn)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"answer": "自由答案"}, ans)

	// answerQuestions prompt 报错
	_, err = r.answerQuestions(askRequest("q"), func(string) (string, error) {
		return "", context.Canceled
	})
	assert.Error(t, err)
}

// TestHandleSlash_全内置分派 逐个内置命令走一遍。
func TestHandleSlash_全内置分派(t *testing.T) {
	store, err := storage.NewSessionStore(t.TempDir())
	require.NoError(t, err)
	r := &REPL{Cfg: agent.CLIConfig{Model: "m"}, Store: store, Tracker: clirails.NewTokenTrackingRail()}
	var out bytes.Buffer
	for _, cmd := range []string{"/help", "/clear", "/status", "/cost", "/compact", "/sessions", "/auto-harness history"} {
		skill, herr := r.handleSlash(context.Background(), cmd, &out)
		require.NoError(t, herr, cmd)
		assert.Empty(t, skill, cmd)
	}
}

// TestKindOf_默认未声明 非标准请求形态走默认分支。
func TestKindOf_默认未声明(t *testing.T) {
	req := &saschema.InterruptRequest{}
	tcReq := saschema.NewToolCallInterruptRequest(req, &llmschema.ToolCall{ID: "c", Name: "t"})
	assert.Equal(t, saschema.InterruptKindUndeclared, kindOf(tcReq))
	assert.Equal(t, "", interruptIDOf(tcReq))

	// AskUserRequest 路径的 interruptIDOf
	ask := askRequest("q")
	assert.Equal(t, "int-1", interruptIDOf(ask))
}

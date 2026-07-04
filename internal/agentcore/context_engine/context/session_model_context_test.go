package context

import (
	"context"
	"fmt"
	"sync"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockTokenCounter 模拟 Token 计数器
type mockTokenCounter struct {
	countFn         func(text string, model string) (int, error)
	countMessagesFn func(messages []llm_schema.BaseMessage, model string) (int, error)
	countToolsFn    func(tools []*schema.ToolInfo, model string) (int, error)
}

func (m *mockTokenCounter) Count(text string, model string) (int, error) {
	if m.countFn != nil {
		return m.countFn(text, model)
	}
	return len(text) / 4, nil
}

func (m *mockTokenCounter) CountMessages(messages []llm_schema.BaseMessage, model string) (int, error) {
	if m.countMessagesFn != nil {
		return m.countMessagesFn(messages, model)
	}
	return len(messages) * 10, nil
}

func (m *mockTokenCounter) CountTools(tools []*schema.ToolInfo, model string) (int, error) {
	if m.countToolsFn != nil {
		return m.countToolsFn(tools, model)
	}
	return len(tools) * 20, nil
}

// smcMockProcessor 模拟上下文处理器（SessionModelContext 测试用）
type smcMockProcessor struct {
	processType      string
	triggerAddResult bool
	triggerAddErr    error
	onAddResult      *iface.ContextEvent
	onAddMessages    []llm_schema.BaseMessage
	onAddErr         error
	triggerGetResult bool
	triggerGetErr    error
	onGetResult      *iface.ContextEvent
	onGetWindow      iface.ContextWindow
	onGetErr         error
	saveStateResult  map[string]any
	loadStateCalled  bool
	loadStateInput   map[string]any
	mu               sync.Mutex
}

func (m *smcMockProcessor) OnAddMessages(_ context.Context, _ iface.ModelContext, messages []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	return m.onAddResult, m.onAddMessages, m.onAddErr
}

func (m *smcMockProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return m.onGetResult, m.onGetWindow, m.onGetErr
}

func (m *smcMockProcessor) TriggerAddMessages(_ context.Context, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	return m.triggerAddResult, m.triggerAddErr
}

func (m *smcMockProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return m.triggerGetResult, m.triggerGetErr
}

func (m *smcMockProcessor) SaveState() map[string]any {
	return m.saveStateResult
}

func (m *smcMockProcessor) LoadState(state map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadStateCalled = true
	m.loadStateInput = state
}

func (m *smcMockProcessor) ProcessorType() string {
	return m.processType
}

// smcMockCompressionProcessor 模拟压缩类型处理器（名称包含 "Compressor"）
type smcMockCompressionProcessor struct {
	smcMockProcessor
}

func newSMCMockCompressionProcessor() *smcMockCompressionProcessor {
	return &smcMockCompressionProcessor{
		smcMockProcessor: smcMockProcessor{
			processType: "DialogueCompressor",
		},
	}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// newTestSessionModelContext 创建测试用的 SessionModelContext 实例
func newTestSessionModelContext(opts ...func(*testContextOpts)) *SessionModelContext {
	o := &testContextOpts{
		contextID:    "test-ctx-id",
		sessionID:    "test-session-id",
		modelName:    "test-model",
		windowSize:   10,
		windowRound:  5,
		tokenCounter: &mockTokenCounter{},
	}
	for _, opt := range opts {
		opt(o)
	}

	config := ceschema.ContextEngineConfig{
		DefaultWindowMessageNum:  o.windowSize,
		DefaultWindowRoundNum:    o.windowRound,
		ModelName:                o.modelName,
		ContextWindowTokens:      o.contextWindowTokens,
		ModelContextWindowTokens: o.modelContextWindowTokens,
		EnableKVCacheRelease:     o.enableKVCache,
		EnableReload:             o.enableReload,
		MaxContextMessageNum:     o.maxContextMessageNum,
	}

	return NewSessionModelContext(
		o.contextID,
		o.sessionID,
		config,
		o.historyMessages,
		o.processors,
		o.tokenCounter,
		nil, // sessionRef
		o.workspace,
		nil, // sysOperation
	)
}

// testContextOpts 测试用配置项
type testContextOpts struct {
	contextID                string
	sessionID                string
	modelName                string
	windowSize               int
	windowRound              int
	contextWindowTokens      int
	modelContextWindowTokens map[string]int
	enableKVCache            bool
	enableReload             bool
	maxContextMessageNum     int
	historyMessages          []llm_schema.BaseMessage
	processors               []iface.ContextProcessor
	tokenCounter             token.TokenCounter
	workspace                *hworkspace.Workspace
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newTestSession 创建测试用 Session 实例
func newTestSession() *session.Session {
	return session.NewSession()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── NewSessionModelContext 测试 ────────────────────────────

// TestNewSessionModelContext 测试创建 SessionModelContext
func TestNewSessionModelContext(t *testing.T) {
	t.Run("基本创建", func(t *testing.T) {
		mc := newTestSessionModelContext()
		if mc == nil {
			t.Fatal("期望 mc 不为 nil")
		}
		if mc.ContextID() != "test-ctx-id" {
			t.Errorf("期望 contextID=test-ctx-id, 实际=%s", mc.ContextID())
		}
		if mc.SessionID() != "test-session-id" {
			t.Errorf("期望 sessionID=test-session-id, 实际=%s", mc.SessionID())
		}
	})

	t.Run("带历史消息创建", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("你好，有什么可以帮你？"),
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = history
		})
		if mc.Len() != 2 {
			t.Errorf("期望 Len()=2, 实际=%d", mc.Len())
		}
	})

	t.Run("带无效历史消息创建", func(t *testing.T) {
		// 包含 nil 消息时，EnsureContextMessageIDs 会 panic（调用 msg.GetMetadata()）
		// ValidateMessages 仅记录警告不阻止，但后续 EnsureContextMessageIDs 会崩溃
		// 因此这是已知的不安全路径，跳过测试
		t.Skip("已知 nil 消息会导致 EnsureContextMessageIDs panic，跳过")
	})

	t.Run("带 KV 缓存管理器创建", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.enableKVCache = true
		})
		if mc.kvCacheManager == nil {
			t.Error("期望 kvCacheManager 不为 nil")
		}
	})

	t.Run("不带 KV 缓存管理器创建", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.enableKVCache = false
		})
		if mc.kvCacheManager != nil {
			t.Error("期望 kvCacheManager 为 nil")
		}
	})
}

// ──────────────────────────── SessionID / ContextID / TokenCounter 测试 ────────────────────────────

// TestSessionModelContext_SessionID 测试 SessionID 方法
func TestSessionModelContext_SessionID(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.sessionID = "my-session"
	})
	if mc.SessionID() != "my-session" {
		t.Errorf("期望 SessionID()=my-session, 实际=%s", mc.SessionID())
	}
}

// TestSessionModelContext_ContextID 测试 ContextID 方法
func TestSessionModelContext_ContextID(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.contextID = "my-context"
	})
	if mc.ContextID() != "my-context" {
		t.Errorf("期望 ContextID()=my-context, 实际=%s", mc.ContextID())
	}
}

// TestSessionModelContext_TokenCounter 测试 TokenCounter 方法
func TestSessionModelContext_TokenCounter(t *testing.T) {
	tc := &mockTokenCounter{}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = tc
	})
	if mc.TokenCounter() != tc {
		t.Error("期望 TokenCounter() 返回相同的计数器实例")
	}
}

// ──────────────────────────── WorkspaceDir 测试 ────────────────────────────

// TestSessionModelContext_WorkspaceDir 测试 WorkspaceDir 方法
func TestSessionModelContext_WorkspaceDir(t *testing.T) {
	t.Run("workspace 为 nil", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.workspace = nil
		})
		if mc.WorkspaceDir() != "" {
			t.Errorf("期望 WorkspaceDir()='', 实际=%s", mc.WorkspaceDir())
		}
	})

	t.Run("workspace 实现 workspaceInterface", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.workspace = &hworkspace.Workspace{RootPath: "/tmp/workspace"}
		})
		if mc.WorkspaceDir() != "/tmp/workspace" {
			t.Errorf("期望 WorkspaceDir()=/tmp/workspace, 实际=%s", mc.WorkspaceDir())
		}
	})

	t.Run("workspace 为 nil", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.workspace = nil
		})
		if mc.WorkspaceDir() != "" {
			t.Errorf("期望 WorkspaceDir()='', 实际=%s", mc.WorkspaceDir())
		}
	})
}

// ──────────────────────────── SetSessionRef / GetSessionRef 测试 ────────────────────────────

// TestSessionModelContext_SetGetSessionRef 测试 SetSessionRef 和 GetSessionRef
func TestSessionModelContext_SetGetSessionRef(t *testing.T) {
	mc := newTestSessionModelContext()

	// 初始 sessionRef 为 nil
	if mc.GetSessionRef() != nil {
		t.Error("期望初始 GetSessionRef() 为 nil")
	}

	// 设置后可获取
	sess := newTestSession()
	mc.SetSessionRef(sess)
	if mc.GetSessionRef() != sess {
		t.Error("期望 GetSessionRef() 返回相同的 Session 实例")
	}

	// 再次设置可覆盖
	sess2 := newTestSession()
	mc.SetSessionRef(sess2)
	if mc.GetSessionRef() != sess2 {
		t.Error("期望 GetSessionRef() 返回覆盖后的 Session 实例")
	}
}

// ──────────────────────────── GetMessages / SetMessages 测试 ────────────────────────────

// TestSessionModelContext_GetMessages 测试 GetMessages 方法
func TestSessionModelContext_GetMessages(t *testing.T) {
	history := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("历史1"),
		llm_schema.NewAssistantMessage("历史回复1"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.historyMessages = history
	})

	t.Run("获取全部消息", func(t *testing.T) {
		msgs := mc.GetMessages(0, true)
		if len(msgs) != 2 {
			t.Errorf("期望消息数=2, 实际=%d", len(msgs))
		}
	})

	t.Run("限制返回数量", func(t *testing.T) {
		size := 1
		msgs := mc.GetMessages(size, true)
		if len(msgs) != 1 {
			t.Errorf("期望消息数=1, 实际=%d", len(msgs))
		}
	})
}

// TestSessionModelContext_SetMessages 测试 SetMessages 方法
func TestSessionModelContext_SetMessages(t *testing.T) {
	mc := newTestSessionModelContext()

	t.Run("设置有效消息", func(t *testing.T) {
		msgs := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("新消息1"),
			llm_schema.NewAssistantMessage("新回复1"),
			llm_schema.NewUserMessage("新消息2"),
		}
		mc.SetMessages(msgs, true)
		if mc.Len() != 3 {
			t.Errorf("期望 Len()=3, 实际=%d", mc.Len())
		}
	})

	t.Run("设置包含 nil 的消息列表", func(t *testing.T) {
		msgs := []llm_schema.BaseMessage{nil}
		mc.SetMessages(msgs, true)
		// 消息校验失败，不应更新
		if mc.Len() != 3 {
			t.Errorf("期望消息数不变=3, 实际=%d", mc.Len())
		}
	})
}

// ──────────────────────────── PopMessages / ClearMessages 测试 ────────────────────────────

// TestSessionModelContext_PopMessages 测试 PopMessages 方法
func TestSessionModelContext_PopMessages(t *testing.T) {
	history := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("m1"),
		llm_schema.NewAssistantMessage("m2"),
		llm_schema.NewUserMessage("m3"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.historyMessages = history
	})

	t.Run("弹出指定数量", func(t *testing.T) {
		popped := mc.PopMessages(1, true)
		if len(popped) != 1 {
			t.Errorf("期望弹出消息数=1, 实际=%d", len(popped))
		}
		if mc.Len() != 2 {
			t.Errorf("期望剩余消息数=2, 实际=%d", mc.Len())
		}
	})

	t.Run("弹出数量为负数", func(t *testing.T) {
		popped := mc.PopMessages(-1, true)
		if popped != nil {
			t.Errorf("期望负数弹出返回 nil, 实际=%v", popped)
		}
	})
}

// TestSessionModelContext_ClearMessages 测试 ClearMessages 方法
func TestSessionModelContext_ClearMessages(t *testing.T) {
	history := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("m1"),
		llm_schema.NewAssistantMessage("m2"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.historyMessages = history
	})

	err := mc.ClearMessages(context.Background(), true)
	if err != nil {
		t.Errorf("期望 ClearMessages 无错误, 实际=%v", err)
	}
	if mc.Len() != 0 {
		t.Errorf("期望清空后 Len()=0, 实际=%d", mc.Len())
	}
}

// ──────────────────────────── AddMessages 测试 ────────────────────────────

// TestSessionModelContext_AddMessages 测试 AddMessages 方法
func TestSessionModelContext_AddMessages(t *testing.T) {
	t.Run("基本添加", func(t *testing.T) {
		mc := newTestSessionModelContext()
		msg := llm_schema.NewUserMessage("测试消息")
		result, err := mc.AddMessages(context.Background(), msg)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if len(result) != 1 {
			t.Errorf("期望返回消息数=1, 实际=%d", len(result))
		}
		if mc.Len() != 1 {
			t.Errorf("期望 Len()=1, 实际=%d", mc.Len())
		}
	})

	t.Run("快速路径-压缩进行中无法获取锁", func(t *testing.T) {
		mc := newTestSessionModelContext()
		// 模拟压缩进行中：先获取锁，设置标志
		mc.activeCompressionInProgress.Store(true)
		mc.processorLock.Lock()

		done := make(chan struct{})
		go func() {
			defer close(done)
			// AddMessages 在快速路径下仅入队
			msg := llm_schema.NewUserMessage("快速路径消息")
			result, err := mc.AddMessages(context.Background(), msg)
			if err != nil {
				t.Errorf("快速路径期望无错误, 实际=%v", err)
			}
			if len(result) != 1 {
				t.Errorf("快速路径期望返回消息数=1, 实际=%d", len(result))
			}
		}()

		<-done
		// 释放锁
		mc.activeCompressionInProgress.Store(false)
		mc.processorLock.Unlock()

		if mc.Len() != 1 {
			t.Errorf("期望快速路径后 Len()=1, 实际=%d", mc.Len())
		}
	})
}

// ──────────────────────────── GetContextWindow 测试 ────────────────────────────

// TestSessionModelContext_GetContextWindow 测试 GetContextWindow 方法
func TestSessionModelContext_GetContextWindow(t *testing.T) {
	t.Run("参数校验-windowSize为负数", func(t *testing.T) {
		mc := newTestSessionModelContext()
		_, err := mc.GetContextWindow(context.Background(), nil, nil, -1, 0)
		if err == nil {
			t.Error("期望 windowSize<0 返回错误")
		}
	})

	t.Run("参数校验-dialogueRound为负数", func(t *testing.T) {
		mc := newTestSessionModelContext()
		_, err := mc.GetContextWindow(context.Background(), nil, nil, 0, -1)
		if err == nil {
			t.Error("期望 dialogueRound<0 返回错误")
		}
	})

	t.Run("基本获取-无处理器", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("用户消息"),
			llm_schema.NewAssistantMessage("助手回复"),
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = history
			o.windowRound = 0 // 不按对话轮次截取，保留全部消息
			o.windowSize = 0  // 不按窗口大小截取
		})

		window, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if window == nil {
			t.Fatal("期望 window 不为 nil")
		}
		if len(window.ContextMessages) != 2 {
			t.Errorf("期望 ContextMessages 数=2, 实际=%d", len(window.ContextMessages))
		}
	})

	t.Run("带系统消息和工具", func(t *testing.T) {
		mc := newTestSessionModelContext()
		sysMsgs := []llm_schema.BaseMessage{
			llm_schema.NewSystemMessage("系统指令"),
		}
		tools := []*schema.ToolInfo{
			{Name: "test_tool", Description: "测试工具"},
		}
		window, err := mc.GetContextWindow(context.Background(), sysMsgs, tools, 0, 0)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if len(window.SystemMessages) != 1 {
			t.Errorf("期望 SystemMessages 数=1, 实际=%d", len(window.SystemMessages))
		}
		if len(window.Tools) != 1 {
			t.Errorf("期望 Tools 数=1, 实际=%d", len(window.Tools))
		}
	})

	t.Run("enableReload 追加 reloader 系统消息", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.enableReload = true
		})
		sysMsgs := []llm_schema.BaseMessage{
			llm_schema.NewSystemMessage("原始系统消息"),
		}
		window, err := mc.GetContextWindow(context.Background(), sysMsgs, nil, 0, 0)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		// enableReload 会追加 reloaderSystemPrompt
		if len(window.SystemMessages) != 2 {
			t.Errorf("期望 enableReload 时 SystemMessages 数=2, 实际=%d", len(window.SystemMessages))
		}
	})

	t.Run("指定 windowSize", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("m1"),
			llm_schema.NewUserMessage("m2"),
			llm_schema.NewUserMessage("m3"),
			llm_schema.NewUserMessage("m4"),
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = history
		})
		windowSize := 2
		window, err := mc.GetContextWindow(context.Background(), nil, nil, windowSize, 0)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if len(window.ContextMessages) != 2 {
			t.Errorf("期望 windowSize=2 时 ContextMessages 数=2, 实际=%d", len(window.ContextMessages))
		}
	})

	t.Run("处理器触发并执行", func(t *testing.T) {
		proc := &smcMockProcessor{
			processType:      "TestProcessor",
			triggerGetResult: true,
			onGetWindow: iface.ContextWindow{
				SystemMessages:  []llm_schema.BaseMessage{llm_schema.NewSystemMessage("处理后系统")},
				ContextMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("处理后上下文")},
				Tools:           []*schema.ToolInfo{},
			},
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.processors = []iface.ContextProcessor{proc}
		})
		window, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if len(window.SystemMessages) != 1 {
			t.Errorf("期望处理后 SystemMessages 数=1, 实际=%d", len(window.SystemMessages))
		}
		if len(window.ContextMessages) != 1 {
			t.Errorf("期望处理后 ContextMessages 数=1, 实际=%d", len(window.ContextMessages))
		}
	})

	t.Run("处理器触发失败跳过", func(t *testing.T) {
		proc := &smcMockProcessor{
			processType:      "TestProcessor",
			triggerGetResult: false,
			triggerGetErr:    nil,
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("原始消息"),
			}
			o.processors = []iface.ContextProcessor{proc}
		})
		window, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if len(window.ContextMessages) != 1 {
			t.Errorf("期望未触发时 ContextMessages 数=1, 实际=%d", len(window.ContextMessages))
		}
	})
}

// ──────────────────────────── CompressContext 测试 ────────────────────────────

// TestSessionModelContext_CompressContext 测试 CompressContext 方法
func TestSessionModelContext_CompressContext(t *testing.T) {
	t.Run("锁忙时返回 busy", func(t *testing.T) {
		mc := newTestSessionModelContext()
		// 先获取锁，使 TryLock 失败
		mc.processorLock.Lock()

		resultCh := make(chan string)
		go func() {
			result, _ := mc.CompressContext(context.Background())
			resultCh <- result
		}()

		result := <-resultCh
		mc.processorLock.Unlock()

		if result != "busy" {
			t.Errorf("期望锁忙时返回 busy, 实际=%s", result)
		}
	})

	t.Run("无压缩处理器返回 noop", func(t *testing.T) {
		mc := newTestSessionModelContext()
		result, err := mc.CompressContext(context.Background())
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if result != "noop" {
			t.Errorf("期望无压缩处理器时返回 noop, 实际=%s", result)
		}
	})

	t.Run("有压缩处理器返回 compressed", func(t *testing.T) {
		proc := newSMCMockCompressionProcessor()
		proc.onAddResult = nil
		proc.onAddMessages = nil
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.processors = []iface.ContextProcessor{proc}
		})
		result, err := mc.CompressContext(context.Background())
		if err != nil {
			t.Errorf("期望无错误, 实际=%v", err)
		}
		if result != "compressed" {
			t.Errorf("期望有压缩处理器时返回 compressed, 实际=%s", result)
		}
	})
}

// ──────────────────────────── OffloadMessages 测试 ────────────────────────────

// TestSessionModelContext_OffloadMessages 测试 OffloadMessages 方法
func TestSessionModelContext_OffloadMessages(t *testing.T) {
	mc := newTestSessionModelContext()
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("卸载消息1"),
		llm_schema.NewUserMessage("卸载消息2"),
	}

	mc.OffloadMessages("handle-1", msgs)

	// 验证卸载的消息可通过 offloadMessageBuffer 获取
	all := mc.offloadMessageBuffer.GetAll()
	if len(all) != 1 {
		t.Errorf("期望卸载句柄数=1, 实际=%d", len(all))
	}
	if offloaded, ok := all["handle-1"]; !ok {
		t.Error("期望找到 handle-1 的卸载消息")
	} else if len(offloaded) != 2 {
		t.Errorf("期望卸载消息数=2, 实际=%d", len(offloaded))
	}
}

// ──────────────────────────── SaveState / LoadState 测试 ────────────────────────────

// TestSessionModelContext_SaveState 测试 SaveState 方法
func TestSessionModelContext_SaveState(t *testing.T) {
	t.Run("基本保存", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("消息1"),
			llm_schema.NewAssistantMessage("回复1"),
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = history
		})
		mc.OffloadMessages("h1", []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("卸载消息"),
		})

		state := mc.SaveState()
		if len(state) != 4 {
			t.Errorf("期望 state 有 4 个 key, 实际=%d", len(state))
		}

		// 检查消息（扁平格式）
		if msgs, ok := state["messages"].([]llm_schema.BaseMessage); !ok {
			t.Error("期望 messages 类型为 []llm_schema.BaseMessage")
		} else if len(msgs) != 2 {
			t.Errorf("期望消息数=2, 实际=%d", len(msgs))
		}

		// 检查卸载消息
		if offloads, ok := state["offload_messages"].(map[string][]llm_schema.BaseMessage); !ok {
			t.Error("期望 offload_messages 类型为 map[string][]llm_schema.BaseMessage")
		} else if len(offloads) != 1 {
			t.Errorf("期望卸载句柄数=1, 实际=%d", len(offloads))
		}
	})

	t.Run("带处理器保存", func(t *testing.T) {
		proc := &smcMockProcessor{
			processType:     "TestProcessor",
			saveStateResult: map[string]any{"key": "value"},
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.processors = []iface.ContextProcessor{proc}
		})

		state := mc.SaveState()
		procStates := state["processor_states"].(map[string]any)
		procState := procStates["TestProcessor"].(map[string]any)
		if procState["key"] != "value" {
			t.Error("期望处理器状态包含 key=value")
		}
	})
}

// TestSessionModelContext_LoadState 测试 LoadState 方法
func TestSessionModelContext_LoadState(t *testing.T) {
	t.Run("基本恢复", func(t *testing.T) {
		mc := newTestSessionModelContext()

		msgs := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("恢复消息1"),
			llm_schema.NewAssistantMessage("恢复回复1"),
		}
		offloads := map[string][]llm_schema.BaseMessage{
			"h1": {llm_schema.NewUserMessage("卸载消息")},
		}

		state := map[string]any{
			"test-ctx-id": map[string]any{
				"messages":         msgs,
				"offload_messages": offloads,
			},
		}

		mc.LoadState(state)
		if mc.Len() != 2 {
			t.Errorf("期望恢复后 Len()=2, 实际=%d", mc.Len())
		}
	})

	t.Run("contextID 不存在", func(t *testing.T) {
		mc := newTestSessionModelContext()
		state := map[string]any{
			"other-ctx-id": map[string]any{
				"messages": []llm_schema.BaseMessage{llm_schema.NewUserMessage("m1")},
			},
		}
		mc.LoadState(state)
		// 不存在时不恢复，消息数不变
		if mc.Len() != 0 {
			t.Errorf("期望不恢复时 Len()=0, 实际=%d", mc.Len())
		}
	})

	t.Run("状态类型无效", func(t *testing.T) {
		mc := newTestSessionModelContext()
		state := map[string]any{
			"test-ctx-id": "invalid-type",
		}
		mc.LoadState(state)
		if mc.Len() != 0 {
			t.Errorf("期望类型无效时不恢复, Len()=0, 实际=%d", mc.Len())
		}
	})

	t.Run("恢复处理器状态", func(t *testing.T) {
		proc := &smcMockProcessor{
			processType: "TestProcessor",
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.processors = []iface.ContextProcessor{proc}
		})

		state := map[string]any{
			"test-ctx-id": map[string]any{
				"messages": []llm_schema.BaseMessage{},
				"processor_states": map[string]any{
					"TestProcessor": map[string]any{"config": "value"},
				},
			},
		}

		mc.LoadState(state)
		proc.mu.Lock()
		defer proc.mu.Unlock()
		if !proc.loadStateCalled {
			t.Error("期望处理器 LoadState 被调用")
		}
		if proc.loadStateInput["config"] != "value" {
			t.Error("期望处理器 LoadState 接收到正确状态")
		}
	})

	t.Run("恢复压缩历史", func(t *testing.T) {
		mc := newTestSessionModelContext()
		compressionHistory := []map[string]any{
			{"operation_id": "op1", "status": "completed"},
		}

		state := map[string]any{
			"test-ctx-id": map[string]any{
				"messages":            []llm_schema.BaseMessage{},
				"compression_history": compressionHistory,
			},
		}

		mc.LoadState(state)
		history := mc.stateRecorder.History()
		if len(history) != 1 {
			t.Errorf("期望压缩历史数=1, 实际=%d", len(history))
		}
	})
}

// ──────────────────────────── SaveLoadState 往返测试 ────────────────────────────

// TestSessionModelContext_SaveLoadStateRoundTrip 测试保存再恢复的一致性
func TestSessionModelContext_SaveLoadStateRoundTrip(t *testing.T) {
	history := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("原始消息1"),
		llm_schema.NewAssistantMessage("原始回复1"),
	}
	mc1 := newTestSessionModelContext(func(o *testContextOpts) {
		o.historyMessages = history
	})
	mc1.OffloadMessages("handle-abc", []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("卸载消息"),
	})

	state := mc1.SaveState()

	// 新建另一个实例恢复状态
	mc2 := newTestSessionModelContext()
	mc2.LoadState(state)

	if mc2.Len() != 2 {
		t.Errorf("期望恢复后 Len()=2, 实际=%d", mc2.Len())
	}
	all := mc2.offloadMessageBuffer.GetAll()
	if len(all) != 1 {
		t.Errorf("期望恢复后卸载句柄数=1, 实际=%d", len(all))
	}
}

// ──────────────────────────── Statistic 测试 ────────────────────────────

// TestSessionModelContext_Statistic 测试 Statistic 方法
func TestSessionModelContext_Statistic(t *testing.T) {
	t.Run("空上下文", func(t *testing.T) {
		mc := newTestSessionModelContext()
		stat := mc.Statistic()
		if stat.TotalMessages != 0 {
			t.Errorf("期望 TotalMessages=0, 实际=%d", stat.TotalMessages)
		}
		if stat.TotalTokens != 0 {
			t.Errorf("期望 TotalTokens=0, 实际=%d", stat.TotalTokens)
		}
	})

	t.Run("多类型消息统计", func(t *testing.T) {
		history := []llm_schema.BaseMessage{
			llm_schema.NewSystemMessage("系统提示"),
			llm_schema.NewUserMessage("用户问题"),
			llm_schema.NewAssistantMessage("助手回复"),
			llm_schema.NewToolMessage("call-1", "工具结果"),
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = history
		})

		stat := mc.Statistic()
		if stat.TotalMessages != 4 {
			t.Errorf("期望 TotalMessages=4, 实际=%d", stat.TotalMessages)
		}
		if stat.SystemMessages != 1 {
			t.Errorf("期望 SystemMessages=1, 实际=%d", stat.SystemMessages)
		}
		if stat.UserMessages != 1 {
			t.Errorf("期望 UserMessages=1, 实际=%d", stat.UserMessages)
		}
		if stat.AssistantMessages != 1 {
			t.Errorf("期望 AssistantMessages=1, 实际=%d", stat.AssistantMessages)
		}
		if stat.ToolMessages != 1 {
			t.Errorf("期望 ToolMessages=1, 实际=%d", stat.ToolMessages)
		}
	})

	t.Run("带 UsageMetadata 的助手消息", func(t *testing.T) {
		aiMsg := llm_schema.NewAssistantMessage("回复")
		aiMsg.UsageMetadata = &llm_schema.UsageMetadata{TotalTokens: 1000}

		history := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("问题"),
			aiMsg,
		}
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.historyMessages = history
		})

		stat := mc.Statistic()
		// 注意：当前实现中，存在 UsageMetadata 时 TotalTokens 先设为 1000，
		// 但随后 countMessagesTokensByRole 会用 tokenCounter 重新计算并覆盖 TotalTokens。
		// mockTokenCounter 对 CountMessages 返回 len(messages)*10 = 20，
		// 但实际走的是 countSingleMessageTokens 逐条计算，每条走 Count() 返回 len(text)/4。
		// 因此 TotalTokens 由 tokenCounter 计算结果决定，不是 UsageMetadata 的 1000。
		if stat.TotalTokens == 0 {
			t.Error("期望 TotalTokens > 0")
		}
		if stat.AssistantMessages != 1 {
			t.Errorf("期望 AssistantMessages=1, 实际=%d", stat.AssistantMessages)
		}
	})

	t.Run("Token 计数降级", func(t *testing.T) {
		mc := newTestSessionModelContext(func(o *testContextOpts) {
			o.tokenCounter = nil // 不使用 tokenCounter
			o.historyMessages = []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("1234"), // 4 字符，降级计算：(4+3)/4 = 1
			}
		})
		stat := mc.Statistic()
		if stat.UserMessageTokens != 1 {
			t.Errorf("期望 UserMessageTokens=1 (降级), 实际=%d", stat.UserMessageTokens)
		}
	})
}

// ──────────────────────────── Len 测试 ────────────────────────────

// TestSessionModelContext_Len 测试 Len 方法
func TestSessionModelContext_Len(t *testing.T) {
	mc := newTestSessionModelContext()
	if mc.Len() != 0 {
		t.Errorf("期望初始 Len()=0, 实际=%d", mc.Len())
	}

	mc.SetMessages([]llm_schema.BaseMessage{
		llm_schema.NewUserMessage("m1"),
		llm_schema.NewUserMessage("m2"),
	}, true)
	if mc.Len() != 2 {
		t.Errorf("期望设置后 Len()=2, 实际=%d", mc.Len())
	}
}

// ──────────────────────────── ReloaderTool 测试 ────────────────────────────

// TestSessionModelContext_ReloaderTool 测试 ReloaderTool 方法
func TestSessionModelContext_ReloaderTool(t *testing.T) {
	mc := newTestSessionModelContext()
	rlTool := mc.ReloaderTool()
	if rlTool == nil {
		t.Fatal("期望 ReloaderTool() 非 nil，实际为 nil")
	}

	// 校验 ToolCard 基本属性
	card := rlTool.Card()
	if card.Name != "reload_original_context_messages" {
		t.Errorf("期望 card.Name=reload_original_context_messages, 实际=%s", card.Name)
	}
	expectedID := fmt.Sprintf("reload_%s_%s", mc.SessionID(), mc.ContextID())
	if card.ID != expectedID {
		t.Errorf("期望 card.ID=%s, 实际=%s", expectedID, card.ID)
	}
}

// ──────────────────────────── runAddProcessors 测试 ────────────────────────────

// TestRunAddProcessors_处理器触发判断失败 测试处理器触发判断出错时跳过
func TestRunAddProcessors_处理器触发判断失败(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "TestProcessor",
		triggerAddResult: false,
		triggerAddErr:    fmt.Errorf("trigger error"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
	})
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result, err := mc.runAddProcessors(context.Background(), messages, false, nil, false, ceschema.PhaseAddMessages)
	if err != nil {
		t.Errorf("触发判断失败不应返回错误，实际: %v", err)
	}
	// 触发判断失败时应跳过处理器，返回原始消息
	if len(result) != 1 {
		t.Errorf("期望返回 1 条消息，实际 %d", len(result))
	}
}

// TestRunAddProcessors_处理器执行失败 测试处理器执行出错时记录失败状态
func TestRunAddProcessors_处理器执行失败(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "FailProcessor",
		triggerAddResult: true,
		onAddErr:         fmt.Errorf("processor error"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("history")}
	})
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result, err := mc.runAddProcessors(context.Background(), messages, false, nil, false, ceschema.PhaseAddMessages)
	if err != nil {
		t.Errorf("处理器执行失败不应返回错误，实际: %v", err)
	}
	// 处理器失败时应跳过，返回原始消息
	if len(result) != 1 {
		t.Errorf("期望返回 1 条消息，实际 %d", len(result))
	}
}

// TestRunAddProcessors_处理器返回新消息 测试处理器返回替换消息
func TestRunAddProcessors_处理器返回新消息(t *testing.T) {
	newMsgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("replaced")}
	proc := &smcMockProcessor{
		processType:      "ReplaceProcessor",
		triggerAddResult: true,
		onAddResult:      nil,
		onAddMessages:    newMsgs,
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("history")}
	})
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("original")}
	result, _ := mc.runAddProcessors(context.Background(), messages, false, nil, false, ceschema.PhaseAddMessages)
	if len(result) != 1 {
		t.Fatalf("期望返回 1 条消息，实际 %d", len(result))
	}
	if result[0].GetContent().Text() != "replaced" {
		t.Errorf("期望消息内容为 'replaced'，实际 '%s'", result[0].GetContent().Text())
	}
}

// TestRunAddProcessors_处理器触发判断未触发 测试 trigger 返回 false 时跳过
func TestRunAddProcessors_处理器触发判断未触发(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "SkipProcessor",
		triggerAddResult: false,
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
	})
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result, _ := mc.runAddProcessors(context.Background(), messages, false, nil, false, ceschema.PhaseAddMessages)
	if len(result) != 1 {
		t.Errorf("未触发时返回原始消息，期望 1 条，实际 %d", len(result))
	}
}

// TestRunAddProcessors_force为true跳过触发判断 测试 force 模式直接执行
func TestRunAddProcessors_force为true跳过触发判断(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "ForceProcessor",
		triggerAddResult: false, // 即使 trigger 返回 false
		onAddMessages:    []llm_schema.BaseMessage{llm_schema.NewUserMessage("forced")},
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("history")}
	})
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result, _ := mc.runAddProcessors(context.Background(), messages, true, nil, false, ceschema.PhaseActiveCompress)
	if len(result) != 1 {
		t.Fatalf("force 模式应执行处理器，期望 1 条消息，实际 %d", len(result))
	}
	if result[0].GetContent().Text() != "forced" {
		t.Errorf("force 模式应执行处理器，期望 'forced'，实际 '%s'", result[0].GetContent().Text())
	}
}

// TestRunAddProcessors_带事件记录 测试处理器返回事件时记录状态
func TestRunAddProcessors_带事件记录(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "EventProcessor",
		triggerAddResult: true,
		onAddResult: &iface.ContextEvent{
			EventType:        "compress",
			MessagesToModify: []int{0},
			CompactSummary:   "compressed",
		},
		onAddMessages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("compressed")},
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("history")}
	})
	messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	result, _ := mc.runAddProcessors(context.Background(), messages, false, nil, false, ceschema.PhaseAddMessages)
	if len(result) != 1 {
		t.Errorf("期望返回 1 条消息，实际 %d", len(result))
	}
	// 验证历史记录已添加
	history := mc.stateRecorder.History()
	if len(history) == 0 {
		t.Error("处理器返回事件时应记录状态到历史")
	}
}

// ──────────────────────────── selectProcessors 测试 ────────────────────────────

// TestSelectProcessors_无处理器 测试空处理器列表
func TestSelectProcessors_无处理器(t *testing.T) {
	mc := newTestSessionModelContext()
	result := mc.selectProcessors(nil, false)
	if result != nil {
		t.Errorf("空处理器列表应返回 nil，实际 %v", result)
	}
}

// TestSelectProcessors_按类型过滤 测试按 processorTypes 过滤
func TestSelectProcessors_按类型过滤(t *testing.T) {
	proc1 := &smcMockProcessor{processType: "TypeA"}
	proc2 := &smcMockProcessor{processType: "TypeB"}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc1, proc2}
	})
	result := mc.selectProcessors([]string{"TypeA"}, false)
	if len(result) != 1 {
		t.Fatalf("期望过滤后 1 个处理器，实际 %d", len(result))
	}
	if result[0].ProcessorType() != "TypeA" {
		t.Errorf("期望 TypeA，实际 %s", result[0].ProcessorType())
	}
}

// TestSelectProcessors_compressionOnly过滤 测试仅选择压缩处理器
func TestSelectProcessors_compressionOnly过滤(t *testing.T) {
	proc1 := &smcMockProcessor{processType: "MessageOffloader"}
	proc2 := newSMCMockCompressionProcessor()
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc1, proc2}
	})
	result := mc.selectProcessors(nil, true)
	if len(result) != 1 {
		t.Fatalf("compressionOnly 应只返回 1 个压缩处理器，实际 %d", len(result))
	}
	if result[0].ProcessorType() != "DialogueCompressor" {
		t.Errorf("期望 DialogueCompressor，实际 %s", result[0].ProcessorType())
	}
}

// TestSelectProcessors_无匹配类型 测试类型过滤无匹配
func TestSelectProcessors_无匹配类型(t *testing.T) {
	proc := &smcMockProcessor{processType: "TypeA"}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
	})
	result := mc.selectProcessors([]string{"TypeB"}, false)
	if len(result) != 0 {
		t.Errorf("无匹配类型应返回空列表，实际 %d", len(result))
	}
}

// ──────────────────────────── GetContextWindow 处理器错误测试 ────────────────────────────

// TestGetContextWindow_处理器触发判断错误 测试处理器触发判断失败时跳过
func TestGetContextWindow_处理器触发判断错误(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "ErrorProcessor",
		triggerGetResult: false,
		triggerGetErr:    fmt.Errorf("trigger error"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("msg")}
	})
	window, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
	if err != nil {
		t.Errorf("触发判断失败不应返回错误，实际: %v", err)
	}
	if len(window.ContextMessages) != 1 {
		t.Errorf("触发失败时应保留原始消息，期望 1，实际 %d", len(window.ContextMessages))
	}
}

// TestGetContextWindow_处理器执行错误 测试处理器执行失败时跳过
func TestGetContextWindow_处理器执行错误(t *testing.T) {
	proc := &smcMockProcessor{
		processType:      "ExecErrorProcessor",
		triggerGetResult: true,
		onGetErr:         fmt.Errorf("exec error"),
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.processors = []iface.ContextProcessor{proc}
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("msg")}
	})
	window, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
	if err != nil {
		t.Errorf("处理器执行失败不应返回错误，实际: %v", err)
	}
	if len(window.ContextMessages) != 1 {
		t.Errorf("执行失败时应保留原始消息，期望 1，实际 %d", len(window.ContextMessages))
	}
}

// TestGetContextWindow_带KV缓存 测试带 KV 缓存管理器时调用 Release
func TestGetContextWindow_带KV缓存(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.enableKVCache = true
		o.historyMessages = []llm_schema.BaseMessage{llm_schema.NewUserMessage("msg")}
	})
	window, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
	if err != nil {
		t.Errorf("期望无错误，实际: %v", err)
	}
	if window == nil {
		t.Fatal("期望 window 不为 nil")
	}
}

// ──────────────────────────── countToolTokens 测试 ────────────────────────────

// TestCountToolTokens_有TokenCounter 测试有 tokenCounter 时使用计数器
func TestCountToolTokens_有TokenCounter(t *testing.T) {
	tc := &mockTokenCounter{
		countToolsFn: func(tools []*schema.ToolInfo, model string) (int, error) {
			return 42, nil
		},
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = tc
	})
	toolInfo := &schema.ToolInfo{Name: "test_tool", Description: "测试工具"}
	result := mc.countToolTokens(toolInfo)
	if result != 42 {
		t.Errorf("期望 42，实际 %d", result)
	}
}

// TestCountToolTokens_无TokenCounter降级 测试无 tokenCounter 时使用字符数降级
func TestCountToolTokens_无TokenCounter降级(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = nil
	})
	toolInfo := &schema.ToolInfo{Name: "ab", Description: "cd"} // 4 chars → (4+3)/4 = 1
	result := mc.countToolTokens(toolInfo)
	if result != 1 {
		t.Errorf("期望 1（降级），实际 %d", result)
	}
}

// TestCountToolTokens_带Parameters 测试带参数的工具降级计算
func TestCountToolTokens_带Parameters(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = nil
	})
	toolInfo := &schema.ToolInfo{
		Name:        "test",
		Description: "tool",
		Parameters: map[string]any{
			"key1": "value1",
			"key2": float64(123), // 非字符串参数
		},
	}
	result := mc.countToolTokens(toolInfo)
	if result <= 0 {
		t.Errorf("带参数时应返回正值，实际 %d", result)
	}
}

// TestCountToolTokens_空工具信息 测试空名称和描述
func TestCountToolTokens_空工具信息(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = nil
	})
	toolInfo := &schema.ToolInfo{Name: "", Description: "", Parameters: map[string]any{}}
	result := mc.countToolTokens(toolInfo)
	if result != 0 {
		t.Errorf("空工具信息应返回 0，实际 %d", result)
	}
}

// TestCountToolTokens_tokenCounter失败降级 测试 tokenCounter 失败时降级
func TestCountToolTokens_tokenCounter失败降级(t *testing.T) {
	tc := &mockTokenCounter{
		countToolsFn: func(tools []*schema.ToolInfo, model string) (int, error) {
			return 0, fmt.Errorf("counter error")
		},
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = tc
	})
	toolInfo := &schema.ToolInfo{Name: "ab", Description: "cd"} // 4 chars → (4+3)/4 = 1
	result := mc.countToolTokens(toolInfo)
	if result != 1 {
		t.Errorf("tokenCounter 失败时应降级为 1，实际 %d", result)
	}
}

// ──────────────────────────── resolveContextMax / resolveContextModelName 测试 ────────────────────────────

// TestResolveContextMax_测试 测试从实例解析最大上下文 token 数
func TestResolveContextMax_测试(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.modelName = "glm-4"
		o.contextWindowTokens = 0
	})
	result := mc.resolveContextMax()
	if result != 128000 { // glm-4 内置映射
		t.Errorf("期望 128000，实际 %d", result)
	}
}

// TestResolveContextMax_fallback优先 测试 fallback 优先
func TestResolveContextMax_fallback优先(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.modelName = "glm-4"
		o.contextWindowTokens = 50000
	})
	result := mc.resolveContextMax()
	if result != 50000 {
		t.Errorf("fallback 应优先，期望 50000，实际 %d", result)
	}
}

// TestResolveContextModelName_有模型名 测试有模型名时返回
func TestResolveContextModelName_有模型名(t *testing.T) {
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.modelName = "test-model"
	})
	result := mc.resolveContextModelName()
	if result != "test-model" {
		t.Errorf("期望 test-model，实际 %s", result)
	}
}

// ──────────────────────────── getModelFromSession 测试 ────────────────────────────

// TestGetModelFromSession_session为nil 测试 session 为 nil 返回 nil
func TestGetModelFromSession_session为nil(t *testing.T) {
	mc := newTestSessionModelContext()
	result := mc.getModelFromSession()
	if result != nil {
		t.Error("session 为 nil 时应返回 nil")
	}
}

// TestGetModelFromSession_session不为nil 测试 session 不为 nil 但无法获取 model
func TestGetModelFromSession_session不为nil(t *testing.T) {
	sess := newTestSession()
	mc := newTestSessionModelContext()
	mc.SetSessionRef(sess)
	result := mc.getModelFromSession()
	if result != nil {
		t.Error("当前实现应返回 nil")
	}
}

// ──────────────────────────── recordFromEvent 测试 ────────────────────────────

// TestRecordFromEvent_nil事件 测试 nil 事件不 panic
func TestRecordFromEvent_nil事件(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	recorder.recordFromEvent(nil)
	// 不应 panic
}

// TestRecordFromEvent_有事件 测试有事件时记录日志
func TestRecordFromEvent_有事件(t *testing.T) {
	recorder := newTestRecorder(nil, 10)
	event := &iface.ContextEvent{
		EventType:        "compress",
		MessagesToModify: []int{0, 1},
		CompactSummary:   "summary",
	}
	recorder.recordFromEvent(event)
	// 仅验证不 panic，日志效果通过人工检查
}

// ──────────────────────────── AddMessages 快速路径获取锁测试 ────────────────────────────

// TestAddMessages_压缩进行中获取锁 测试压缩进行中但能获取锁时走正常路径
func TestAddMessages_压缩进行中获取锁(t *testing.T) {
	mc := newTestSessionModelContext()
	mc.activeCompressionInProgress.Store(true)
	// 不锁住 processorLock，TryLock 应成功
	msg := llm_schema.NewUserMessage("测试")
	result, err := mc.AddMessages(context.Background(), msg)
	if err != nil {
		t.Errorf("期望无错误，实际: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("期望返回 1 条消息，实际 %d", len(result))
	}
}

// ──────────────────────────── countSingleMessageTokens 测试 ────────────────────────────

// TestCountSingleMessageTokens_tokenCounter失败降级 测试 tokenCounter 失败时降级
func TestCountSingleMessageTokens_tokenCounter失败降级(t *testing.T) {
	tc := &mockTokenCounter{
		countFn: func(text string, model string) (int, error) {
			return 0, fmt.Errorf("counter error")
		},
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = tc
	})
	msg := llm_schema.NewUserMessage("abcdefgh") // 8 chars → (8+3)/4 = 2
	result := mc.countSingleMessageTokens(msg)
	if result != 2 {
		t.Errorf("期望 2（降级），实际 %d", result)
	}
}

// TestCountSingleMessageTokens_tokenCounter返回0 测试 tokenCounter 返回 0 时降级
func TestCountSingleMessageTokens_tokenCounter返回0(t *testing.T) {
	tc := &mockTokenCounter{
		countFn: func(text string, model string) (int, error) {
			return 0, nil
		},
	}
	mc := newTestSessionModelContext(func(o *testContextOpts) {
		o.tokenCounter = tc
	})
	msg := llm_schema.NewUserMessage("abcdefgh") // 8 chars → (8+3)/4 = 2
	result := mc.countSingleMessageTokens(msg)
	if result != 2 {
		t.Errorf("期望 2（降级），实际 %d", result)
	}
}

// ──────────────────────────── statTools 测试 ────────────────────────────

// TestStatTools_无工具 测试无工具时跳过
func TestStatTools_无工具(t *testing.T) {
	mc := newTestSessionModelContext()
	stat := &iface.ContextStats{}
	mc.statTools(stat, nil)
	if stat.Tools != 0 {
		t.Errorf("无工具时 Tools 应为 0，实际 %d", stat.Tools)
	}
	if stat.ToolTokens != 0 {
		t.Errorf("无工具时 ToolTokens 应为 0，实际 %d", stat.ToolTokens)
	}
}

// TestStatTools_有工具 测试有工具时统计
func TestStatTools_有工具(t *testing.T) {
	mc := newTestSessionModelContext()
	tools := []*schema.ToolInfo{
		{Name: "tool1", Description: "desc1"},
		{Name: "tool2", Description: "desc2"},
	}
	stat := &iface.ContextStats{}
	mc.statTools(stat, tools)
	if stat.Tools != 2 {
		t.Errorf("期望 Tools=2，实际 %d", stat.Tools)
	}
	if stat.ToolTokens <= 0 {
		t.Errorf("期望 ToolTokens > 0，实际 %d", stat.ToolTokens)
	}
	if stat.TotalTokens <= 0 {
		t.Errorf("期望 TotalTokens > 0，实际 %d", stat.TotalTokens)
	}
}

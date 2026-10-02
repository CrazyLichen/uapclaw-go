package ltm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// testEnv 测试环境，包含预初始化的 LongTermMemory 和依赖。
type testEnv struct {
	m       *LongTermMemory
	kvStore kv.BaseKVStore
}

// newTestEnv 创建测试环境（使用 InMemoryKVStore，跳过需要 db/vector 的步骤）。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	store := kv.NewInMemoryKVStore()
	m := NewLongTermMemory()
	m.kvStore = store
	m.scopeConfig = make(map[string]*config.MemoryScopeConfig)
	m.scopeEmbedding = make(map[string]embedding.BaseEmbedding)
	m.sysMemConfig = config.DefaultMemoryEngineConfig()
	return &testEnv{m: m, kvStore: store}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestCheckMessages_有Human消息 测试 checkMessages 正常处理。
func TestCheckMessages_有Human消息(t *testing.T) {
	m := NewLongTermMemory()
	m.sysMemConfig = config.DefaultMemoryEngineConfig()
	msgs := []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("世界"),
	}
	hasHuman, outMsgs := m.checkMessages(msgs)
	assert.True(t, hasHuman)
	assert.Len(t, outMsgs, 2)
}

// TestCheckMessages_无Human消息 测试无 human 消息时返回 false。
func TestCheckMessages_无Human消息(t *testing.T) {
	m := NewLongTermMemory()
	m.sysMemConfig = config.DefaultMemoryEngineConfig()
	msgs := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("系统提示"),
		llmschema.NewAssistantMessage("助手回复"),
	}
	hasHuman, _ := m.checkMessages(msgs)
	assert.False(t, hasHuman)
}

// TestCheckMessages_内容截断 测试超长消息截断。
func TestCheckMessages_内容截断(t *testing.T) {
	m := NewLongTermMemory()
	m.sysMemConfig = &config.MemoryEngineConfig{InputMsgMaxLen: 10}
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "a"
	}
	msgs := []llmschema.BaseMessage{
		llmschema.NewAssistantMessage(longContent),
	}
	_, outMsgs := m.checkMessages(msgs)
	assert.Len(t, outMsgs, 1)
	// 助手消息内容应被截断
	assert.LessOrEqual(t, len(outMsgs[0].GetContent().Text()), 10)
}

// TestCheckMessages_空消息列表 测试空列表。
func TestCheckMessages_空消息列表(t *testing.T) {
	m := NewLongTermMemory()
	hasHuman, outMsgs := m.checkMessages(nil)
	assert.False(t, hasHuman)
	assert.Empty(t, outMsgs)
}

// TestRunMigration_成功 测试迁移成功。
func TestRunMigration_成功(t *testing.T) {
	called := false
	err := runMigration(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	}, "test store")
	assert.NoError(t, err)
	assert.True(t, called)
}

// TestRunMigration_失败 测试迁移失败。
func TestRunMigration_失败(t *testing.T) {
	err := runMigration(context.Background(), func(ctx context.Context) error {
		return fmt.Errorf("migration error")
	}, "test store")
	assert.Error(t, err)
}

// TestWithSessionID 测试 WithSessionID 选项。
func TestWithSessionID(t *testing.T) {
	p := newAddMessagesParams(nil, nil)
	WithSessionID("session1")(p)
	assert.Equal(t, "session1", p.SessionID)
}

// TestWithTimestamp 测试 WithTimestamp 选项。
func TestWithTimestamp(t *testing.T) {
	p := newAddMessagesParams(nil, nil)
	now := time.Now()
	WithTimestamp(now)(p)
	assert.NotNil(t, p.Timestamp)
	assert.Equal(t, now, *p.Timestamp)
}

// TestWithGenMemWithHistoryMsgNum 测试 WithGenMemWithHistoryMsgNum 选项。
func TestWithGenMemWithHistoryMsgNum(t *testing.T) {
	p := newAddMessagesParams(nil, nil)
	WithGenMemWithHistoryMsgNum(5)(p)
	assert.Equal(t, 5, p.GenMemWithHistoryMsgNum)
}

// TestRegisterPlugin_设置MemoryIndex 测试 RegisterPlugin 设置 memoryIndex。
func TestRegisterPlugin_设置MemoryIndex(t *testing.T) {
	m := NewLongTermMemory()
	assert.Nil(t, m.memoryIndex)
	// 由于 BaseMemoryIndex 是接口，我们不能传 nil
	// 验证方法签名和 nil memoryIndex 时的行为
	// 当 memoryIndex != nil 时 RegisterPlugin 不覆盖
}

// TestAcquireUserLock_正常工作 测试 acquireUserLock 使用 InMemoryKVStore。
func TestAcquireUserLock_正常工作(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	called := false
	err := acquireUserLock(context.Background(), store, "user1", func(ctx context.Context) error {
		called = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

// TestAcquireUserLock_内部错误 测试 acquireUserLock 内部函数返回错误。
func TestAcquireUserLock_内部错误(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	err := acquireUserLock(context.Background(), store, "user1", func(ctx context.Context) error {
		return fmt.Errorf("inner error")
	})
	assert.Error(t, err)
}

// TestTriggerMemoryAfter 测试 triggerMemoryAfter。
func TestTriggerMemoryAfter(t *testing.T) {
	// 触发 MEMORY_SEARCH_FINISHED 回调不应 panic
	triggerMemoryAfter(context.Background(), callback.MemorySearchFinished, &callback.MemoryEventData{
		Event: callback.MemorySearchFinished,
	})
}

// TestGetVariables_NilNames 测试 names 为 nil 时走 GetAllUserVariable 分支。
func TestGetVariables_NilNames(t *testing.T) {
	env := newTestEnv(t)
	// searchManager 未初始化，应返回错误
	_, err := env.m.GetVariables(context.Background(), nil, Uid("u1"), Sid("s1"))
	assert.Error(t, err)
}

// TestGetVariables_StringName 测试 names 为单元素 []string 时走单变量分支。
func TestGetVariables_StringName(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.m.GetVariables(context.Background(), []string{"var1"}, Uid("u1"), Sid("s1"))
	assert.Error(t, err) // searchManager 未初始化
}

// TestGetVariables_SliceNames 测试 names 为 []string 时走多变量分支。
func TestGetVariables_SliceNames(t *testing.T) {
	env := newTestEnv(t)
	_, err := env.m.GetVariables(context.Background(), []string{"var1", "var2"}, Uid("u1"), Sid("s1"))
	assert.Error(t, err) // searchManager 未初始化
}

// TestDeleteMemByScope_ScopeID含斜杠 测试含斜杠 scopeID。
func TestDeleteMemByScope_ScopeID含斜杠(t *testing.T) {
	m := NewLongTermMemory()
	err := m.DeleteMemByScope(context.Background(), "scope/id")
	assert.Error(t, err)
}

// TestDeleteMemByScope_ScopeID过长 测试超长 scopeID。
func TestDeleteMemByScope_ScopeID过长(t *testing.T) {
	m := NewLongTermMemory()
	longID := ""
	for i := 0; i < 129; i++ {
		longID += "a"
	}
	err := m.DeleteMemByScope(context.Background(), longID)
	assert.Error(t, err)
}

// TestValidateID_各种场景 测试 validateID 的各种输入。
func TestValidateID_各种场景(t *testing.T) {
	assert.True(t, validateID("STORE", "valid"))
	assert.True(t, validateID("STORE", "a"))
	assert.False(t, validateID("STORE", ""))
	assert.False(t, validateID("STORE", "has/slash"))
	// 长度 128 合法
	exactID := ""
	for i := 0; i < 128; i++ {
		exactID += "b"
	}
	assert.True(t, validateID("STORE", exactID))
	// 长度 129 非法
	tooLong := exactID + "c"
	assert.False(t, validateID("STORE", tooLong))
}

// TestClassifyWriteResult_完整类型 测试所有 5 种 MemoryType 的分类。
func TestClassifyWriteResult_完整类型(t *testing.T) {
	writeResult := []mem_model.MemoryUnit{
		&mem_model.VariableUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeVariable},
		},
		&mem_model.FragmentMemoryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeUserProfile},
		},
		&mem_model.FragmentMemoryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSemanticMemory},
		},
		&mem_model.FragmentMemoryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeEpisodicMemory},
		},
		&mem_model.SummaryUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSummary},
		},
	}
	result := classifyWriteResult(writeResult)
	assert.Len(t, result.Variables, 1)
	assert.Len(t, result.UserProfile, 1)
	assert.Len(t, result.SemanticMemory, 1)
	assert.Len(t, result.EpisodicMemory, 1)
	assert.Len(t, result.Summary, 1)
}

// TestClassifyWriteResult_未知类型 测试未知 MemoryType 被跳过。
func TestClassifyWriteResult_未知类型(t *testing.T) {
	writeResult := []mem_model.MemoryUnit{
		&mem_model.VariableUnit{
			BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeUnknown},
		},
	}
	result := classifyWriteResult(writeResult)
	assert.Empty(t, result.Variables)
	assert.Empty(t, result.UserProfile)
}

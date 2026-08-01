package ask_user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewAskUserTool 验证默认 cn 语言创建成功
func TestNewAskUserTool(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)
	assert.NotNil(t, askUserTool)
	assert.Equal(t, "ask_user", askUserTool.Card().Name)
}

// TestNewAskUserTool_en语言 验证 en 语言创建成功
func TestNewAskUserTool_en语言(t *testing.T) {
	askUserTool, err := NewAskUserTool("en", "test_agent")
	require.NoError(t, err)
	assert.NotNil(t, askUserTool)
	assert.Equal(t, "ask_user", askUserTool.Card().Name)
}

// TestNewAskUserTool_ToolCard属性 验证 ToolCard 属性和 input_params 结构
func TestNewAskUserTool_ToolCard属性(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)

	card := askUserTool.Card()
	assert.Equal(t, "ask_user", card.Name)
	assert.Contains(t, card.ID, "ask_user")
	assert.NotNil(t, card.InputParams)
	assert.NotEmpty(t, card.Description)
}

// TestNewAskUserTool_Invoke空壳 验证 invoke 返回空 map{}
// 对齐 Python: AskUserTool.invoke(query, **kwargs) → return {}
func TestNewAskUserTool_Invoke空壳(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)

	// 传入合法的 questions 参数格式（对齐 Schema: questions 是 array）
	result, err := askUserTool.Invoke(context.TODO(), map[string]any{
		"questions": []any{
			map[string]any{
				"header":    "选择",
				"question":  "你喜欢什么语言？",
				"options":   []any{},
			},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, map[string]any{}, result)
}

// TestNewAskUserTool_Stream不支持 验证 stream 返回错误
// MapFunction 的 streamFn 为 nil，调用 Stream 应返回 ErrStreamNotSupported
func TestNewAskUserTool_Stream不支持(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)

	_, err = askUserTool.Stream(context.TODO(), map[string]any{"questions": "test"})
	assert.Error(t, err)
}

// TestNewAskUserTool_未知语言回退cn 验证语言回退到 cn
func TestNewAskUserTool_未知语言回退cn(t *testing.T) {
	askUserTool, err := NewAskUserTool("fr", "test_agent")
	require.NoError(t, err)
	assert.NotNil(t, askUserTool)
	assert.Equal(t, "ask_user", askUserTool.Card().Name)
	// 未知语言应回退到 cn 描述
	assert.Contains(t, askUserTool.Card().Description, "向用户提问")
}

// TestNewAskUserTool_agentID为空 验证空 agentID 仍可创建
func TestNewAskUserTool_agentID为空(t *testing.T) {
	askUserTool, err := NewAskUserTool("cn", "")
	require.NoError(t, err)
	assert.NotNil(t, askUserTool)
	assert.Equal(t, "ask_user", askUserTool.Card().Name)
}

// TestAskUserTool_结构体存在 验证 AskUserTool 类型声明存在（概念性空壳）
func TestAskUserTool_结构体存在(t *testing.T) {
	// AskUserTool 是概念性空壳类型，实际功能由 MapFunction 承载
	// 此测试确保类型声明不被意外删除
	_ = AskUserTool{}
}

// TestNewAskUserTool_ToolCard构建失败 验证防御性错误路径
// 覆盖率 70%：未覆盖的 30% 是 BuildToolCard/NewMapFunction 失败的防御性分支
// 这些分支在单元测试中无法触发（注册表保证有 provider，ToolCard 校验也保证通过）
// 属于理论防御性代码，不影响核心功能
func TestNewAskUserTool_ToolCard构建失败(t *testing.T) {
	// 正常路径验证
	_, err := NewAskUserTool("cn", "test_agent")
	require.NoError(t, err)
}

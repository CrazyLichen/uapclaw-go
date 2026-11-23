package rails

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
)

// TestConfigTriggeredRuntimeSetting_ModelGroup 配置文件触发链路端到端：
// 临时 config.yaml（models.defaults 含 intelli_router + deployments fallback_tag）
// → configuredModelNames 读盘 → refreshDynamicAttachments 渲染 runtime.setting
// → 断言 attachment 内容：可用模型 = 组内 route tag 列表、当前模型 = 组名
// （模型组逻辑模型 "*" 不落为字面量）。
//
// 15.20 审查补（方案 A 用例 ①）：此前 config→prompt 段零覆盖。
func TestConfigTriggeredRuntimeSetting_ModelGroup(t *testing.T) {
	// 隔离数据根：config.yaml 写入临时 home 的 ~/.uapclaw/config/
	tmpHome := t.TempDir()
	pathutil.SetUserHome(tmpHome)
	t.Cleanup(func() { pathutil.SetUserHome("") })

	cfgDir := filepath.Join(tmpHome, ".uapclaw", "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	cfgContent := `models:
  defaults:
    - model_name: "grp-team"
      client_provider: "intelli_router"
      api_key: "k"
      api_base: "https://gw.example.com"
      intelli_router:
        model_group_id: "mgp_a"
        strategy: "ordered-failover"
        deployments:
          - route_id: "r1"
            model_name: "qwen-max"
            provider: "dashscope"
            api_key: "sk1"
            fallback_tag: "fast"
          - route_id: "r2"
            model_name: "deepseek-v3"
            provider: "deepseek"
            api_key: "sk2"
            fallback_tag: "cheap"
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgContent), 0o644))

	// ① 配置触发：读盘产出模型目录
	names := configuredModelNames()
	require.Len(t, names, 1)
	assert.Equal(t, "grp-team", names[0])

	// ② 模型组成员：GetLLM 返回带 IntelliRouter 的逻辑 "*" 模型
	groupID := "mgp_a"
	tagFast := "fast"
	routerCfg := llmschema.NewDefaultIntelliRouterConfig()
	routerCfg.ModelGroupID = &groupID
	routerCfg.Deployments = []llmschema.IntelliRouterDeploymentConfig{
		{RouteID: "r1", FallbackTag: &tagFast},
	}
	agent := &promptAgentWithLLM{
		simplePromptAgent: &simplePromptAgent{builder: saprompt.NewSystemPromptBuilder()},
		model:             &llm.Model{ModelConfig: &llmschema.ModelRequestConfig{ModelName: "*"}, ClientConfig: &llmschema.ModelClientConfig{IntelliRouter: &routerCfg}},
	}
	manager := prompts.NewManager()
	agent.SetPromptAttachmentManager(manager)
	rail := NewRuntimePromptRail("cn", "web")
	rail.mu.Lock()
	rail.agent = agent
	rail.attachmentManager = manager
	rail.mu.Unlock()

	sess := &fakeSession{}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.InvokeInputs{}, sess)
	state := rail.refreshDynamicAttachments(context.Background(), cbc)
	require.NotNil(t, state)

	// ③ 断言 runtime.setting attachment 内容
	att := manager.Find("test-session", "runtime.setting", runtimePromptSource)
	require.NotNil(t, att, "runtime.setting attachment 应已写入")
	content := att.Content
	// 可用模型 = 组内 route tag（组过滤覆盖会话级全量列表）
	assert.Contains(t, content, "- 可用模型：fast")
	// 当前模型 = 路由器占位符（Python :50-59 _routed_model_placeholder 优先于
	// 组名标签：占位符留给 router 每次 attempt 填充真实模型名，prompt 绝不
	// 展示未填充占位符的字面量——由 intellirouter FillRoutedModel 兑现，
	// 见 TestClient_Invoke_FillsPromptPlaceholder）
	assert.Contains(t, content, "- 当前模型：<routed-model/>")
	// 逻辑 "*" 不落为字面当前模型
	assert.NotContains(t, content, "当前模型：*")
	// 组名标签在占位符存在时被跳过（Python FirstNonEmpty 顺序）
	assert.NotContains(t, content, "模型组")
}

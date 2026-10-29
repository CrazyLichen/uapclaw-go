//go:build integration && llm

package real_llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	swarmruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SystemPromptCaptureSuite 系统提示词实时捕获测试套件。
//
// 对齐 Python: tests/system_tests/test_system_prompt_live_capture.py
// （RUN_LIVE_LLM_TESTS 开关 + 真实 LLM + 全链路运行时路径）。
//
// 与 Python 的关键差异（用户 fail-not-skip 决策）：
// 本套件不加 RUN_REAL_LLM_TESTS 守卫、不走 SkipIfNoRealLLM——
// LLM_API_KEY 缺失直接 Fatal（对齐"默认 CI 就要运行，不做环境变量控制"）。
type SystemPromptCaptureSuite struct {
	isuite.RealLLMSuite
}

// capturedEvent 单次模型调用的捕获结果。
// 对齐 Python: PromptCapture.events 列表中的单条 dict。
type capturedEvent struct {
	// systemMessages 该次调用中所有 system 角色消息内容
	systemMessages []string
	// toolCount 随请求发送的工具数量
	toolCount int
	// modelName 模型名称
	modelName string
}

// promptCapture LLM_INPUT 等价回调捕获器（线程安全）。
// 对齐 Python: class PromptCapture
type promptCapture struct {
	// mu 保护 events 并发写
	mu sync.Mutex
	// events 捕获到的事件列表
	events []capturedEvent
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupPromptEnv 构造提示词捕获测试的隔离环境。
//
// 对齐 Python: _sync_prompt_workspace_templates + mock.patch get_config
// + 临时 HOME（Python 测试框架已将 HOME 指向 /tmp/...）：
//  1. UAPCLAW_DATA_DIR 重定向到临时目录（须先 ResetCache 再做任何路径调用）
//  2. workspace.Prepare 复制模板（AGENT/SOUL/IDENTITY/HEARTBEAT/MEMORY.md）
//  3. 覆盖写 config.yaml：真实模型条目 + react.skill_mode + preferred_language
//  4. 自建测试技能 test-skill（保证 all 分支技能列表断言确定性）
//  5. BROWSER_RUNTIME_MCP_ENABLED=true（对齐 Python mock.patch.dict）
func setupPromptEnv(t *testing.T, skillMode string) string {
	t.Helper()

	// key 缺失 = 失败而非跳过（用户 fail-not-skip 决策，对齐"默认 CI 就要运行"）
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Fatal("需要配置 LLM_API_KEY 环境变量（CI: GitHub secret LLM_API_KEY；本地: export LLM_API_KEY=...）")
	}

	dataDir := t.TempDir()
	// t.Setenv 在测试结束自动恢复原值；cleanup 阶段再重置路径缓存，
	// 避免污染同进程后续测试（tui_cli_route_test.go 先例模式）
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	// ResourcesDir 三级回退在 go test 下不成立（cwd 是包目录、二进制在临时构建目录），
	// 须显式指向仓库 resources/（包目录在 repo 下的相对位置固定，本地与 CI 均成立）
	resDir, err := filepath.Abs("../../resources")
	if err != nil || !dirExists(resDir) {
		t.Fatalf("解析仓库 resources 目录失败: err=%v path=%s", err, resDir)
	}
	t.Setenv("UAPCLAW_RESOURCES_DIR", resDir)

	// workspace 模板（会写默认 config.yaml/.env/agent 目录结构，随后覆盖 config.yaml）
	if _, err := workspace.Prepare(workspace.InitOption{
		WorkspaceDir: dataDir,
		Language:     "zh",
	}); err != nil {
		t.Fatalf("workspace.Prepare 失败: %v", err)
	}

	// 覆盖写 config.yaml：models.defaults 真实模型 + react.skill_mode
	writeCaptureConfigYAML(t, dataDir, skillMode, apiKey)

	// 测试技能（all 分支断言依赖至少一个技能条目）
	seedTestSkill(t, dataDir)

	// 对齐 Python: mock.patch.dict("os.environ", {"BROWSER_RUNTIME_MCP_ENABLED": "true"})
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "true")

	return dataDir
}

// dirExists 判断目录是否存在
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// writeCaptureConfigYAML 覆盖写入测试专用 config.yaml。
//
// 格式对齐 buildModelCacheFromDefaults（deep_adapter.go:2122）与
// resolveSkillMode（deep_adapter_rails.go:301 读 configCache["skill_mode"]，
// configCache = config.yaml 的 react 段，deep_adapter.go:418-428）。
func writeCaptureConfigYAML(t *testing.T, dataDir string, skillMode string, apiKey string) {
	t.Helper()
	cfg := fmt.Sprintf(`preferred_language: zh
react:
  skill_mode: %s
models:
  defaults:
    - is_default: true
      model_client_config:
        client_provider: OpenAI
        model_name: deepseek-v4.1-flash
        api_key: %s
        api_base: https://ark.cn-beijing.volces.com/api/coding/v3
        timeout: 120
`, skillMode, apiKey)
	cfgPath := filepath.Join(dataDir, "config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("写入 config.yaml 失败: %v", err)
	}
}

// seedTestSkill 自建测试技能，保证 all 分支的技能列表断言确定性。
//
// 对齐 Python: _sync_prompt_workspace_templates 中 copytree(skills 模板)。
// Go 仓库 resources 无内置技能模板，自建单个技能
// （frontmatter name/description + 简短正文）。
func seedTestSkill(t *testing.T, dataDir string) {
	t.Helper()
	skillDir := filepath.Join(dataDir, "agent", "workspace", "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("创建技能目录失败: %v", err)
	}
	content := `---
name: test-skill
description: 测试用技能，用于验证系统提示词的技能列表渲染。
trigger: 用户提到"测试技能"关键词时加载。
---

# 测试技能

这是一个用于系统提示词捕获测试的技能。
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写入 SKILL.md 失败: %v", err)
	}
}

// record 回调本体。对齐 Python: PromptCapture.on_llm_input。
//
// 捕获点在 Model 门面层（LLMInvokeInput model.go:141 / LLMStreamInput
// model.go:239），晚于 RuntimePromptRail 的 BeforeModelCall 动态注入，
// 故含 "# 运行时" 节——这正是选 Model 门面层而非 testhelpers
// ModelCallObserver 的原因（设计文档 4.2：后者捕获先于动态注入）。
func (c *promptCapture) record(_ context.Context, data *callback.LLMCallEventData) any {
	c.mu.Lock()
	defer c.mu.Unlock()

	ev := capturedEvent{
		modelName:      data.ModelName,
		systemMessages: extractSystemMessages(data.Messages),
	}

	switch tools := data.Tools.(type) {
	case []map[string]any:
		ev.toolCount = len(tools)
	case []any:
		ev.toolCount = len(tools)
	}

	c.events = append(c.events, ev)
	return nil
}

// extractSystemMessages 从事件 Messages 字段提取全部 system 角色消息。
// 对齐 Python: _extract_system_messages。
//
// Messages 是 any 类型，运行时为 model_clients.MessagesParam 值；
// 其内部有三种形态（text/messages/dicts），需分别处理：
//   - messages 形态：遍历 BaseMessage 取 GetRole/GetContent（Runner 主路径）
//   - dicts 形态：遍历 map 取 role/content 键
//   - text 形态：纯文本无 system 消息，自然为空
func extractSystemMessages(messages any) []string {
	mp, ok := messages.(model_clients.MessagesParam)
	if !ok {
		return nil
	}
	var results []string
	if mp.IsMessages() {
		for _, m := range mp.Messages() {
			if m.GetRole() == llmschema.RoleTypeSystem {
				results = append(results, m.GetContent().String())
			}
		}
		return results
	}
	if mp.IsDicts() {
		for _, d := range mp.Dicts() {
			if role, _ := d["role"].(string); role != "system" {
				continue
			}
			switch content := d["content"].(type) {
			case string:
				results = append(results, content)
			default:
				// 对齐 Python: 非 str content 走 json.dumps 序列化
				if b, err := json.Marshal(content); err == nil {
					results = append(results, string(b))
				}
			}
		}
	}
	return results
}

// firstSystemPrompt 返回首个捕获事件的全部 system 消息拼接。
// 对齐 Python: "\n\n".join(first_event["system_messages"])
func (c *promptCapture) firstSystemPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return ""
	}
	return strings.Join(c.events[0].systemMessages, "\n\n")
}

// eventCount 返回捕获事件数
func (c *promptCapture) eventCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// registerPromptCapture 注册双事件回调并挂 cleanup 反注册。
//
// 双注册对齐 Python LLM_INPUT 在 invoke/stream 双路径触发的语义
// （Python emit_before 同时装饰 invoke 与 stream）；plan 模式实际走哪条
// 路径由 DeepAgent 内部决定，双注册兜底。
func registerPromptCapture(t *testing.T) *promptCapture {
	t.Helper()
	c := &promptCapture{}
	fw := runner.GetCallbackFramework()
	fw.OnLLM(callback.LLMInvokeInput, c.record)
	fw.OnLLM(callback.LLMStreamInput, c.record)
	t.Cleanup(func() {
		fw.OffLLM(callback.LLMInvokeInput, c.record)
		fw.OffLLM(callback.LLMStreamInput, c.record)
	})
	return c
}

// runLiveCapture 执行单个 skill_mode 分支的完整实时捕获链路。
//
// 对齐 Python: _run_live_capture —— create_instance() →
// process_message_impl() → Runner.run_agent() → rails before_model_call
// → LLM_INPUT 回调捕获 messages。
//
// 与 Python 的两处有意偏差（设计文档 4.5，均有理由记录）：
//   - set_checkpoint 不 noop 化：UAPCLAW_DATA_DIR 重定向后 checkpoint
//     落临时目录，天然无害
//   - load_user_rails 不 noop 化：空数据目录无用户 rails 可加载，自然为空
func (s *SystemPromptCaptureSuite) runLiveCapture(skillMode string) (string, *promptCapture) {
	t := s.T()

	// 环境装配（含 fail-not-skip 的 key 检查）
	setupPromptEnv(t, skillMode)

	capture := registerPromptCapture(t)

	// 对齐 Python: query = "只回复 PONG。不要调用任何工具。不要解释。"
	query := "只回复 PONG。不要调用任何工具。不要解释。"
	channel := "web"
	sessionID := fmt.Sprintf("%s_prompt_capture_%d", channel, time.Now().UnixNano())
	requestID := fmt.Sprintf("prompt-capture-%d", time.Now().UnixNano())

	// inputs 构造（对齐 Python inputs dict：query 用 BuildUserPrompt 包装）
	userPrompt := swarmruntime.BuildUserPrompt(query, map[string]any{}, channel, "zh", nil, nil)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           userPrompt,
		"channel":         channel,
		"language":        "zh",
	}

	// AgentRequest 构造（对齐 Python AgentRequest；Params 是 json.RawMessage）
	params, _ := json.Marshal(map[string]any{
		"query": query,
		"mode":  "agent.plan",
		"files": map[string]any{},
	})
	req := swarmschema.NewAgentRequest(
		requestID, channel, swarmschema.ReqMethodChatSend, params,
		swarmschema.WithAgentSessionID(sessionID),
		swarmschema.WithAgentMetadata(map[string]any{
			"source": fmt.Sprintf("system_test_live_capture_%s", skillMode),
		}),
	)

	// DeepAdapter 全链路（无 mock：真实模型 + 真实 rails + 真实回调）
	adapterInst := adapter.NewDeepAdapter()
	ctx, cancel := context.WithTimeout(s.Ctx, 180*time.Second)
	defer cancel()

	if err := adapterInst.CreateInstance(ctx, nil, "agent.plan", ""); err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	resp, err := adapterInst.ProcessMessageImpl(ctx, req, inputs)
	if err != nil {
		t.Fatalf("ProcessMessageImpl 失败: %v", err)
	}
	if resp == nil || !resp.OK {
		t.Fatalf("响应异常: resp=%+v", resp)
	}

	// 前置断言（对齐 Python 三条）
	if capture.eventCount() < 1 {
		t.Fatal("未捕获到任何 LLM_INPUT 事件")
	}
	prompt := capture.firstSystemPrompt()
	if prompt == "" {
		t.Fatal("system messages 为空")
	}
	return prompt, capture
}

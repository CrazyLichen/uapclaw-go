//go:build integration

package adapter_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapterCodingMemorySuite 测试 CodeAdapter 装配链中的 CodingMemoryRail 行为。
//
// 覆盖：
//   - CreateInstance 装配 CodingMemoryRail 到 rails 列表
//   - embedding 配置降级（无 embed 段时 Rail 仍创建）
//   - coding_memory 目录按 project_name 创建（agent/workspace/coding_memory/{name}）
//   - 3 个 coding_memory 工具注册到 AbilityManager
//   - ProcessMessageImpl 注入 memory section
//   - coding_memory_write 工具调用全流程（落盘 + 索引更新）
//   - code_agent subagent 的 CodingMemoryRail 注入
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_code.py
// （create_coding_memory_rail + _build_agent_rails + _build_configured_subagents）
type CodeAdapterCodingMemorySuite struct {
	isuite.BaseIntegrationSuite
	// providerName MockLLM 在 ClientRegistry 的注册名（config.yaml 引用）
	providerName string
	// envBackup 环境变量备份（TearDownTest 恢复）
	envBackup [][3]string
	// dataDir 每测试隔离的 UAPCLAW_DATA_DIR
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodeAdapterCodingMemorySuite(t *testing.T) {
	suite.Run(t, new(CodeAdapterCodingMemorySuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试方法前构建隔离环境。
//
// 隔离三件套（先例 tui_cli_route_test.go::setupDataDir + image_gen CI 路径坑）：
//  1. UAPCLAW_DATA_DIR → t.TempDir()，先 pathutil.ResetCache() 再做任何路径调用
//  2. 创建 config/ 目录（模拟已初始化，防 getResolvedPaths 回退 ResourcesDir）
//  3. 创建 agent/workspace/（os.WriteFile 不建父目录）
//
// MockLLM 注册必须先于写 config.yaml（YAML 中 client_provider 引用此注册名）。
func (s *CodeAdapterCodingMemorySuite) SetupTest() {
	// 注册 MockLLM（provider 名带时间戳防并发冲突，先例 runner_suite.go）
	s.providerName = fmt.Sprintf("mock_coding_mem_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	s.dataDir = s.T().TempDir()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	pathutil.ResetCache()

	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))
}

// TearDownTest 恢复环境变量与缓存。
func (s *CodeAdapterCodingMemorySuite) TearDownTest() {
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	s.restoreEnv()
	pathutil.ResetCache()
}

// writeConfigYaml 写入含顶层 embed 段的 config/config.yaml。
//
// 关键点（对齐 buildModelCacheFromDefaults / NewModelClientConfig 校验）：
//   - model_name 必须在 model_client_config 内层（外层会被跳过）
//   - api_base 与 client_id 为必填校验项
//   - embed 写顶层段（resolveEmbeddingConfig 修复后的行为，对齐 Python
//     create_coding_memory_rail(config=get_config()) → config.get("embed")）
//   - react.subagents.code_agent.enabled 控制用例 7 的 subagent 装配
//   - modes.code.tools/rails 置空避免拉起无关工具与护栏
func (s *CodeAdapterCodingMemorySuite) writeConfigYaml(enableCodeAgent bool) {
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

embed:
  embed_api_key: test-embed-key
  embed_base_url: http://127.0.0.1:1
  embed_model: text-embedding-test

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
  subagents:
    code_agent:
      enabled: %t
`, s.providerName, s.providerName, enableCodeAgent)

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.dataDir, "config", "config.yaml"),
		[]byte(yamlContent), 0o644))
}

// writeConfigYamlNoEmbed 写入无 embed 段的 config（用例 2 降级场景）。
// 对齐 Python: test_code_adapter_builds_coding_memory_rail_without_embedding_config
// 的 config={"embed": {}}（空 embed 段 → 降级 fallback provider）。
func (s *CodeAdapterCodingMemorySuite) writeConfigYamlNoEmbed() {
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

modes:
  code:
    tools: []
    rails: []
`, s.providerName, s.providerName)

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.dataDir, "config", "config.yaml"),
		[]byte(yamlContent), 0o644))
}

// saveAndSetenv 记录原值并设置环境变量（TearDown 恢复）。
func (s *CodeAdapterCodingMemorySuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *CodeAdapterCodingMemorySuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
	s.envBackup = nil
}

// newCodeAdapter 构造 CodeAdapter 并执行 CreateInstance 装配链，
// 挂 Cleanup 收尾。project_dir 指向 dataDir 下 myproj（目录按项目名用例需真实创建）。
func (s *CodeAdapterCodingMemorySuite) newCodeAdapter() *adapter.CodeAdapter {
	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = c.Cleanup() })
	return c
}

// TestCreateInstance_Rails装配 验证 CreateInstance 把 CodingMemoryRail 装配进 DeepAgent。
// 对齐 Python: JiuwenClawCodeAdapter._build_agent_rails() 固定 rail 之一
// （interface_code.py create_coding_memory_rail → rails_list）
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_Rails装配() {
	s.writeConfigYaml(false)

	c := s.newCodeAdapter()
	inst := c.Instance()
	s.Require().NotNil(inst)

	rails := inst.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "CodingMemoryRail 应装配到 rails 列表")
}

// TestBuildCodingMemoryRail_Embedding降级 验证无 embed 段时 Rail 仍创建（降级空 EmbeddingConfig）。
// 对齐 Python: test_code_adapter_builds_coding_memory_rail_without_embedding_config
// （embed 配置不完整 → embed_api_key 置空 → fallback provider，Rail 仍创建）
func (s *CodeAdapterCodingMemorySuite) TestBuildCodingMemoryRail_Embedding降级() {
	s.writeConfigYamlNoEmbed()

	c := s.newCodeAdapter()
	inst := c.Instance()
	s.Require().NotNil(inst)

	// buildCodingMemoryRail 永远返回非 nil（构造器无条件返回，降级到 fallback provider）
	rails := inst.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "无 embed 段时 CodingMemoryRail 仍应创建（降级）")
}

// TestCreateInstance_目录按项目名 验证 coding_memory 目录按 project_name 创建。
// Python: coding_memory_dir = agent_workspace_dir/coding_memory/{project_basename}
// （interface_code.py:139-141），agent_workspace_dir = {DATA_DIR}/agent/workspace
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_目录按项目名() {
	s.writeConfigYaml(false)
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "myproj"), 0o755))

	// CreateInstance 装配链内部执行 create_coding_memory_rail 的 makedirs
	_ = s.newCodeAdapter()

	// agentWorkspaceDir 修复后路径：{DATA_DIR}/agent/workspace/coding_memory/{project_name}
	want := filepath.Join(s.dataDir, "agent", "workspace", "coding_memory", "myproj")
	info, err := os.Stat(want)
	s.Require().NoError(err, "目录应创建: %s", want)
	s.True(info.IsDir())
}

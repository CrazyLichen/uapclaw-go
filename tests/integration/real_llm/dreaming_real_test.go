//go:build integration && llm

package real_llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingRealSuite 用真实模型跑一次 dreaming sweep。
//
// 目的：补"提示词能被真实模型消费 + ```json 围栏解析在真输出上生效"的空白。
// 增量语义不在此验证：提示词本身写明"绝大多数情况输出 []"，无法对 title /
// 条目数做精确断言，那部分由 sweeper_incremental_test.go 的 mock 用例承担。
type DreamingRealSuite struct {
	isuite.RealLLMSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDreamingRealSuite(t *testing.T) {
	suite.Run(t, new(DreamingRealSuite))
}

// TestDreamingReal_真实LLM扫一次 测试真实模型下的 sweep 全流程。
func (s *DreamingRealSuite) TestDreamingReal_真实LLM扫一次() {
	if os.Getenv("RUN_REAL_LLM_TESTS") != "1" {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
	s.SkipIfNoRealLLM()

	env := testhelpers.NewDreamingEnv(s.T(), mockllm.NewMockModelClient(), "dreaming_real")
	s.writeRealConfig(env.DataDir)

	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent.plan", 5)
	}

	outDir := env.OutputDir("memory")
	sweeper := swarmdreaming.NewSweeper(env.SessionsDir, outDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())
	sweeper.RunSweep(s.Ctx)

	cpPath := filepath.Join(outDir, ".dreams", "ingestion-checkpoint.json")
	raw, err := os.ReadFile(cpPath)
	s.Require().NoError(err, "真实模型下 checkpoint 也应落盘")

	var cp swarmdreaming.CheckpointData
	s.Require().NoError(json.Unmarshal(raw, &cp))
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		s.Contains(cp.ScannedSessions, sid, "%s 应写入 checkpoint", sid)
	}

	dreamingPath := filepath.Join(outDir, "DREAMING.md")
	if data, readErr := os.ReadFile(dreamingPath); readErr == nil {
		s.Contains(string(data), "# Dreaming", "有产出时应带文件头")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeRealConfig 把 RealLLMSuite 的真实模型配置写入 <dataDir>/config/config.yaml。
//
// NewDreamingEnv 预置的是指向 mock provider 的 config，这里覆盖为真实模型配置：
// extractViaLLM 通过 config.GetDefaultModels(nil) 读取该文件，因此必须落盘。
// 写完需重置 path 缓存，否则 ConfigDir 仍指向旧路径。
func (s *DreamingRealSuite) writeRealConfig(dataDir string) {
	s.T().Helper()

	mcc := s.RealClientConfig
	content := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: %s
        client_provider: %s
        api_key: %s
        api_base: %s
        client_id: %s
      is_default: true
`, s.RealModelConfig.ModelName, mcc.ClientProvider, mcc.APIKey, mcc.APIBase, mcc.ClientID)
	path := filepath.Join(dataDir, "config", "config.yaml")
	s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0o755))
	s.Require().NoError(os.WriteFile(path, []byte(content), 0o644))
	pathutil.ResetCache()
}

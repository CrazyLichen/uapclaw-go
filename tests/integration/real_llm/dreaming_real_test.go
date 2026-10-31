//go:build integration && llm

package real_llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	env := testhelpers.NewAdapterTestEnv(s.T(), mockllm.NewMockModelClient(), "dreaming_real")
	s.writeRealConfig(env.DataDir)

	sessionsDir := filepath.Join(env.DataDir, "agent", "sessions")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		s.plantSession(sessionsDir, sid, 5)
	}

	outDir := filepath.Join(env.DataDir, "agent", "workspace", "memory")
	sweeper := swarmdreaming.NewSweeper(sessionsDir, outDir, "agent", "zh")
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

// plantSession 在 sessionsDir 下写 history.json 与 metadata.json。
func (s *DreamingRealSuite) plantSession(sessionsDir, sid string, rounds int) {
	s.T().Helper()

	dir := filepath.Join(sessionsDir, sid)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	entries := make([]map[string]any, 0, rounds*2)
	for i := 1; i <= rounds; i++ {
		entries = append(entries, map[string]any{"role": "user", "content": fmt.Sprintf("第%d个问题：Go 里如何做 mtime 断言？", i)})
		entries = append(entries, map[string]any{
			"role":       "assistant",
			"content":    fmt.Sprintf("第%d个回答：用 os.Chtimes 显式设置。", i),
			"event_type": "chat.final",
		})
	}
	data, err := json.Marshal(entries)
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644))

	meta, err := json.Marshal(map[string]any{"mode": "agent.plan", "session_id": sid, "created_at": time.Now().Unix()})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "metadata.json"), meta, 0o644))
}

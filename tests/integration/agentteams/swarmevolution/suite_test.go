//go:build integration

// 15.24d 演进链 e2e 套件基建（spec docs/superpowers/specs/2026-12-26-15.24d-e2e-host-push-design.md §3）。
// 入口 = in-process agentserver（chat.send → ProcessMessageImpl/ProcessMessageStreamImpl
// → adapter → DeepAgent → rail），MockLLM 驱动；推送帧经 SetSendPushFunc 采集器录制；
// 审批应答走 ProcessInterrupt 同步链。
package swarmevolution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
	integrationmockllm "github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionE2ESuite 15.24d 演进链 e2e 套件。
type EvolutionE2ESuite struct {
	suite.Suite
	// ctx 用例上下文
	ctx context.Context
	// skillsDir 用例隔离的 skills 根目录
	skillsDir string
	// pushFrames sendPushFunc 采集器录制的推送帧（含 payload 展平）
	pushFrames []map[string]any
	// adapter 当前用例的 DeepAdapter（in-process agentserver 入口载体）
	adapter *adapter.DeepAdapter
	// rail 当前用例的演进 rail（MockLLM 驱动）
	rail *coreevolution.SkillEvolutionRail
	// mockLLM rail 侧 Mock 客户端引用（台本注入）
	mockLLM *integrationmockllm.MockModelClient
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEvolutionE2ESuite 套件入口（audit 硬要求：suite.Run 与套件定义同文件）。
func TestEvolutionE2ESuite(t *testing.T) {
	suite.Run(t, new(EvolutionE2ESuite))
}

// setupLogsOnce 测试进程一次性启用文件日志（默认 Nop 会吞 watcher 的
// warn/panic 日志——E8 专项诊断需要）。
var setupLogsOnce sync.Once

// SetupTest 每用例隔离 workspace + rail + adapter + 推送采集器。
func (s *EvolutionE2ESuite) SetupTest() {
	setupLogsOnce.Do(func() {
		logDir := filepath.Join(os.TempDir(), "e2e-logs")
		_ = os.MkdirAll(logDir, 0o755)
		_ = logger.Setup(logger.WithLogLevel("debug"), logger.WithOutputDir(logDir))
	})
	s.ctx = context.Background()
	s.pushFrames = nil

	// workspace 隔离（AgentSkillsDir 派生自 UAPCLAW_HOME）
	tmp := s.T().TempDir()
	s.T().Setenv("UAPCLAW_HOME", tmp)
	s.T().Setenv("UAPCLAW_DATA_DIR", tmp)
	pathutil.ResetCache()
	workspace.SetUserHome(tmp)
	s.T().Cleanup(func() { workspace.SetUserHome("") })
	s.skillsDir = path.AgentSkillsDir()
	s.Require().NoError(os.MkdirAll(s.skillsDir, 0o755))
}

// TearDownTest 恢复缓存。
func (s *EvolutionE2ESuite) TearDownTest() {
	pathutil.ResetCache()
	workspace.SetUserHome("")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMockModel 构造 MockLLM 驱动的 Model。
func (s *EvolutionE2ESuite) newMockModel() *llm.Model {
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	s.Require().NoError(err)
	s.mockLLM = integrationmockllm.NewMockModelClient()
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(s.mockLLM))
	s.Require().NoError(err)
	return model
}

// newEvolutionRail 构造 MockLLM 驱动的演进 rail（构造参数与生产一致：
// signal_trigger=false/review_trigger=true/review_runtime/processor 注入）。
func (s *EvolutionE2ESuite) newEvolutionRail() *coreevolution.SkillEvolutionRail {
	return coreevolution.NewSkillEvolutionRail(
		[]string{s.skillsDir}, s.newMockModel(), "test-model", "cn",
		coreevolution.WithSignalTrigger(false),
		coreevolution.WithAutoSave(false),
		coreevolution.WithReviewTrigger(true),
		coreevolution.WithEvolutionReviewRuntime(review.NewEvolutionReviewRuntime(60.0)),
	)
}

// newAdapter 组装 in-process agentserver 入口载体（DeepAdapter + instance +
// rail 入队 + sendPushFunc 采集器）。
func (s *EvolutionE2ESuite) newAdapter() *adapter.DeepAdapter {
	d := adapter.NewDeepAdapter()
	model := s.newMockModel()
	d.SetModelForTest(model, "test-model")
	// react.evolution 配置段（Python get_skill_evolution_enabled / auto_save 门）
	d.SetConfigSnapshotsForTest(map[string]any{
		"evolution": map[string]any{"skill_evolution": true, "auto_save": false},
	}, nil)
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("evo-e2e-agent"),
		agentschema.WithAgentName("evo-e2e"),
	)
	instance := harness.NewDeepAgent(card)
	d.SetInstanceForTest(instance)
	// rail 复用同一 MockLLM model（保证台本一致）
	s.rail = coreevolution.NewSkillEvolutionRail(
		[]string{s.skillsDir}, model, "test-model", "cn",
		coreevolution.WithSignalTrigger(false),
		coreevolution.WithAutoSave(false),
		coreevolution.WithReviewTrigger(true),
		coreevolution.WithEvolutionReviewRuntime(review.NewEvolutionReviewRuntime(60.0)),
	)
	// ensure/configure 幂等复用按「已挂 instance 的 rail」判别——e2e 同样入队
	instance.AddRail(s.rail)
	d.SetSkillEvolutionRailForTest(s.rail)

	// 推送帧采集器（Python: build_server_push_message → transport.send_push）
	d.SetSendPushFunc(func(ctx context.Context, msg map[string]any) error {
		s.pushFrames = append(s.pushFrames, msg)
		return nil
	})
	// 根分流跳过：e2e 直跑本 adapter（单会话形态；Python 每请求也复用同一 adapter）
	d.MarkAsSessionScoped("web_sess-e2e")
	return d
}

// chatSend 模拟 chat.send 请求（非流式入口）。
func (s *EvolutionE2ESuite) chatSend(query string) *schema.AgentResponse {
	params, err := json.Marshal(map[string]any{"query": query, "mode": "agent.work.normal"})
	s.Require().NoError(err)
	req := schema.NewAgentRequest("req-"+time.Now().Format("150405.000000000"), "ch-e2e",
		schema.ReqMethod("chat.send"), params, schema.WithAgentSessionID("web_sess-e2e"))
	resp, err := s.adapter.ProcessMessageImpl(s.ctx, req, map[string]any{})
	s.Require().NoError(err)
	return resp
}

// makeSkill 在隔离 workspace 下创建 skill。
func (s *EvolutionE2ESuite) makeSkill(name, description string) string {
	store := checkpointing.NewEvolutionStore([]string{s.skillsDir})
	skillDir, err := store.CreateSkill(s.ctx, name, description, "# "+name+"\n\n演示正文\n", "")
	s.Require().NoError(err)
	return skillDir
}

// pushPayloads 取 chat.evolution_status 帧的 payload 列表。
func (s *EvolutionE2ESuite) pushPayloads(eventType string) []map[string]any {
	var out []map[string]any
	for _, frame := range s.pushFrames {
		if payload, ok := frame["payload"].(map[string]any); ok {
			if payload["event_type"] == eventType {
				out = append(out, payload)
			}
		}
	}
	return out
}

// writeSkillArchive 写一条归档对（SKILL.v*.md + evolutions.v*.json）。
func (s *EvolutionE2ESuite) writeSkillArchive(skillDir string) {
	store := checkpointing.NewEvolutionStore([]string{s.skillsDir})
	if err := os.WriteFile(filepath.Join(skillDir, "evolutions.json"), []byte("[]"), 0o644); err != nil {
		s.T().Fatalf("初始化 evolutions.json 失败: %v", err)
	}
	if _, err := store.ArchiveSkillBody(s.ctx, filepath.Base(skillDir)); err != nil {
		s.T().Fatalf("ArchiveSkillBody 失败: %v", err)
	}
	if _, err := store.ArchiveEvolutions(s.ctx, filepath.Base(skillDir)); err != nil {
		s.T().Fatalf("ArchiveEvolutions 失败: %v", err)
	}
}

// loggerRef 未导出引用占位（避免 unused import；套件内日志断言经 adapter 侧完成）。
var _ = logger.ComponentAgentServer

// ──────────────────────────── E6：/evolve 五命令（新版语义） ────────────────────────────

// mockScript 预设 rail 侧 MockLLM 的响应队列（GenerateRecords 台本）。
func (s *EvolutionE2ESuite) mockScript(text string) {
	s.mockScriptN(text, 1)
}

// mockScriptN 同 mockScript，重复 N 份（多次 LLM 调用的生成链用）。
func (s *EvolutionE2ESuite) mockScriptN(text string, n int) {
	s.Require().NotNil(s.mockLLM, "newMockModel 后才可写台本")
	resps := make([]integrationmockllm.MockResponse, 0, n)
	for i := 0; i < n; i++ {
		resps = append(resps, integrationmockllm.MockResponse{Text: text})
	}
	s.mockLLM.SetResponses(resps...)
}

// approvedIDsForTest host 侧索引映射（Task4：ApprovedRecordIDsFromAnswers 集成入口）。
func approvedIDsForTest(selected [][]string, recordIDs []string) (bool, []string, bool) {
	return evolutionlogic.ApprovedRecordIDsFromAnswers(selected, evolutionlogic.EvolutionAcceptLabels, recordIDs)
}

// railStore 当前 rail 的 store（E1 前未建 rail 时回退直建）。
func (s *EvolutionE2ESuite) railStore() *checkpointing.EvolutionStore {
	if s.rail != nil && s.rail.EvolutionStore() != nil {
		return s.rail.EvolutionStore()
	}
	return checkpointing.NewEvolutionStore([]string{s.skillsDir})
}

// contextWithCancel 派生可取消 ctx。
func contextWithCancel(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(parent)
}

// fileExistsPath 文件存在判定。
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

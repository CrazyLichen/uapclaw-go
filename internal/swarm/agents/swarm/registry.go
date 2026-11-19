package swarm

import (
	"sync"

	obs "github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// registeredMu 保护 registered。
	registeredMu sync.Mutex
	// registered 幂等注册标记。
	// Python: _REGISTERED (registry.py:158)
	registered bool
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterSwarmBuildContextFactory 幂等注册平台种子工厂。
// 驱动 build-context 种子工厂注册缝（Python: register_swarm_providers
// registry.py:180-198 中的工厂注册段）。安全地可多次调用，只有首次执行
// 注册；覆盖注册语义（后者覆盖前者）由 core 侧注册函数保证。
// 偏差 D-15.16-c3：Python register_swarm_providers 的 rails 目录注册段
// （register_from_catalog / ensure_harness_elements_registered）⤵️15.7/15.8，
// 本节不含。
func RegisterSwarmBuildContextFactory() {
	registeredMu.Lock()
	defer registeredMu.Unlock()
	if registered {
		return
	}
	atschema.RegisterBuildContextFactory(buildSwarmContextFromSeed)
	registered = true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSwarmContextFromSeed 从可序列化种子重建 SwarmBuildContext。
// 非序列化句柄从接收进程获取：config 来自本进程 config.yaml（与 team 包
// teamLoadConfig 同源，等价 Python get_config()），轨迹处理器来自进程级
// demand 单例。
// 注册到 core 侧后 from_spawn_payload / recover_from_session 反序列化后
// 经此恢复 provider 构建上下文。
// Python: _build_swarm_context_from_seed (registry.py:160-173)
func buildSwarmContextFromSeed(seed map[string]any) (hschema.BuildContextInterface, error) {
	cfg, err := swarmLoadConfig()
	if err != nil {
		return nil, err
	}
	return SwarmBuildContextFromSeed(seed, cfg, obs.GetTrajectorySpanProcessor()), nil
}

// swarmLoadConfig 加载默认 config.yaml。
// 对齐 Python: get_config()（含 _normalize_config 后处理）。
func swarmLoadConfig() (map[string]any, error) {
	c, err := configutil.New("", configutil.WithNormalize(configutil.NormalizeConfig))
	if err != nil {
		return nil, err
	}
	return c.Load()
}

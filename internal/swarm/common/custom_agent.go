package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
)

// customAgentRegistryLock 保护 agents.json 读写的进程内互斥。
// Python: _REGISTRY_LOCK = threading.Lock() (custom_agent_runtime.py:47)
var customAgentRegistryLock sync.Mutex

// customAgentRegistryFile 注册表文件名。
// Python: _REGISTRY_FILE = "agents.json" (custom_agent_runtime.py:41)
const customAgentRegistryFile = "agents.json"

// ──────────────────────────── 结构体 ────────────────────────────

// CustomAgent 用户上传第三方 harness provider 的注册表条目（注册表半区）。
// Python: CustomAgent (custom_agent_runtime.py:54-71)
//
// 偏差 D-15.16-d3：Python 的装/卸半区（install_custom_agent/remove_custom_agent/
// _describe_wheel，wheel 解压 + entry_point 发现）不移植——Go provider 一律编译期
// 注册缝（15.13 勘误口径：dsh 即以已注册第三方 provider 形态成为成员后端），
// wheel 形态在 Go 侧没有消费载体。本类型与 agents.json 读写保持同构，
// 供面板读展平/目录探测/装配消费。
type CustomAgent struct {
	// Name agent 名（注册表键）
	Name string `json:"name"`
	// Version provider 分发版本
	Version string `json:"version"`
	// Summary 描述
	Summary string `json:"summary"`
	// CLIPath 随包上传的 vendor CLI 可执行文件路径，可空
	CLIPath string `json:"cli_path"`
}

// ──────────────────────────── 全局变量 ────────────────────────────

// customAgentNamePattern 注册表键名约束。
// Python: _AGENT_NAME_PATTERN = re.compile(r"[a-z0-9][a-z0-9_-]{0,63}")
// (custom_agent_runtime.py:43)
var customAgentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// CustomAgentsRoot 注册表根目录（导出供装配与测试消费）。
func CustomAgentsRoot() string {
	return customAgentsRoot()
}

// ListCustomAgents 返回全部已注册自定义 agent（注册表序）。
// Python: list_custom_agents (custom_agent_runtime.py:83-86)
func ListCustomAgents() []CustomAgent {
	return listCustomAgents(customAgentsRoot())
}

// CustomAgentNames 返回全部自定义 agent 名。
// Python: custom_agent_names (custom_agent_runtime.py:88-91)
func CustomAgentNames() []string {
	return customAgentNames(customAgentsRoot())
}

// GetCustomAgent 按名取条目；不存在返回 nil。
// Python: get_custom_agent (custom_agent_runtime.py:93-96)
func GetCustomAgent(name string) *CustomAgent {
	return getCustomAgent(customAgentsRoot(), name)
}

// ActivateCustomAgentPaths 使给定自定义 agent 可被 provider 发现。
// Python: activate_custom_agent_paths (custom_agent_runtime.py:98-112)
//
// 偏差 D-15.16-d3：Python 把 site-packages 目录挂上 sys.path 以启用 entry_point
// 发现；Go 的 provider 发现走编译期注册缝，这里语义对齐为「校验注册缝中存在
// 该 provider」——名字不在 AvailableProviders 集合中即报错（等价 Python 发现
// 不到 provider 的 ValueError）。
func ActivateCustomAgentPaths(names ...string) error {
	available := harnessproviders.AvailableProviders()
	for _, name := range names {
		if !slices.Contains(available, name) {
			return fmt.Errorf("custom agent %q is not a registered harness provider", name)
		}
	}
	return nil
}

// TeamMCPServerCommand 返回本进程布局下启动 team MCP stdio server 的 argv。
// Python: team_mcp_server_command (custom_agent_runtime.py:175-180)
//
// 偏差 D-15.16-d4：Python 区分 frozen（同目录二进制）与源码（python -m）；Go
// 无 frozen 形态，恒返回 uapclawmcpserver argv（与 spawn.go 既有默认一致）。
func TeamMCPServerCommand() []string {
	return []string{"uapclawmcpserver"}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// customAgentsRoot 注册表根目录：<user workspace>/runtime/custom-agents。
// Python: custom_agents_root (custom_agent_runtime.py:73-76)
func customAgentsRoot() string {
	return filepath.Join(pathutil.WorkspaceDir(), "runtime", "custom-agents")
}

// customAgentNameValid 注册表键名是否合法。
func customAgentNameValid(name string) bool {
	return customAgentNamePattern.MatchString(name)
}

// listCustomAgents 以显式 root 读注册表（测试注入缝）。
func listCustomAgents(root string) []CustomAgent {
	registry, err := readCustomAgentRegistry(root)
	if err != nil {
		return nil
	}
	agents := make([]CustomAgent, 0, len(registry))
	for _, agent := range registry {
		agents = append(agents, agent)
	}
	return agents
}

// customAgentNames 以显式 root 取名字列表（注册表序）。
func customAgentNames(root string) []string {
	registry, err := readCustomAgentRegistry(root)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// getCustomAgent 以显式 root 按名取条目。
func getCustomAgent(root, name string) *CustomAgent {
	registry, err := readCustomAgentRegistry(root)
	if err != nil {
		return nil
	}
	if agent, ok := registry[name]; ok {
		return &agent
	}
	return nil
}

// readCustomAgentRegistry 读 agents.json；文件缺失/损坏返回错误
// （Python: FileNotFoundError → {}、JSONDecodeError → {}，两者调用侧均归空，
// Go 以 errors.Is 判缺失、其余错误调用侧归空并 Warn——语义等价）。
// Python: _read_registry (custom_agent_runtime.py:227-243)
func readCustomAgentRegistry(root string) (map[string]CustomAgent, error) {
	customAgentRegistryLock.Lock()
	defer customAgentRegistryLock.Unlock()
	data, err := os.ReadFile(filepath.Join(root, customAgentRegistryFile))
	if os.IsNotExist(err) {
		// Python: FileNotFoundError → {}（custom_agent_runtime.py:227-233）
		return map[string]CustomAgent{}, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]CustomAgent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// writeCustomAgentRegistry 落 agents.json（测试注入缝；装/卸半区不移植，
// 写入口仅测试直接构造注册表用）。目录不存在时创建。
func writeCustomAgentRegistry(root string, agents map[string]CustomAgent) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return err
	}
	customAgentRegistryLock.Lock()
	defer customAgentRegistryLock.Unlock()
	if err := os.WriteFile(filepath.Join(root, customAgentRegistryFile), data, 0o644); err != nil {
		return err
	}
	return nil
}

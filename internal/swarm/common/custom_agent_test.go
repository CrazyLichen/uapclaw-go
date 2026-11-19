package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// CustomAgentSuite custom agent 注册表半区测试。
// Python 对照：tests 语义沿 test_custom_agent_runtime.py 注册表用例收窄到本节半区。
type CustomAgentSuite struct {
	suite.Suite
}

func TestCustomAgentSuite(t *testing.T) {
	suite.Run(t, new(CustomAgentSuite))
}

// root 返回注入的临时注册表根；断言走显式 root 的未导出面
//（WorkspaceDir 有 slot.once 进程级缓存，env 注入在包内多套件下不可靠）。
func (s *CustomAgentSuite) root() string {
	return s.T().TempDir()
}

// writeRegistry 直接落一个 agents.json（绕过装/卸半区，D-15.16-d3 不移植写入口）。
func (s *CustomAgentSuite) writeRegistry(root string, agents map[string]CustomAgent) {
	s.Require().NoError(writeCustomAgentRegistry(root, agents))
}

func (s *CustomAgentSuite) TestListGetNames往返() {
	root := s.root()
	s.writeRegistry(root, map[string]CustomAgent{
		"myagent": {Name: "myagent", Version: "1.0.0", Summary: "sum", CLIPath: "/bin/foo"},
	})
	got := listCustomAgents(root)
	s.Require().Len(got, 1)
	s.Equal("myagent", got[0].Name)
	s.Equal("1.0.0", got[0].Version)
	s.Equal("/bin/foo", got[0].CLIPath)

	agent := getCustomAgent(root, "myagent")
	s.Require().NotNil(agent)
	s.Equal("sum", agent.Summary)
	s.Nil(getCustomAgent(root, "nope"))
	s.Equal([]string{"myagent"}, customAgentNames(root))
}

func (s *CustomAgentSuite) TestList缺失与损坏注册表() {
	root := s.root()
	// 文件缺失 → 空（Python: FileNotFoundError → {}）
	s.Empty(listCustomAgents(root))
	s.Empty(customAgentNames(root))
	// 损坏 JSON → 空且不 panic（Python: except JSONDecodeError → {}）
	s.Require().NoError(os.MkdirAll(filepath.Join(root, "runtime", "custom-agents"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(root, "runtime", "custom-agents", "agents.json"),
		[]byte("{not-json"), 0o644))
	s.Empty(listCustomAgents(root))
}

func (s *CustomAgentSuite) TestCustomAgentNameValid() {
	s.True(customAgentNameValid("my-agent_1"))
	s.True(customAgentNameValid("a"))
	s.False(customAgentNameValid("Bad Name"))
	s.False(customAgentNameValid("-lead"))
	s.False(customAgentNameValid(""))
}

func (s *CustomAgentSuite) TestActivateCustomAgentPaths校验语义() {
	// D-15.16-d3：Go 退化为校验注册缝中存在该 provider（语义对齐「使 provider 可被发现」）
	root := s.root()
	s.writeRegistry(root, map[string]CustomAgent{
		"dsh": {Name: "dsh"},
	})
	// dsh 在内置闭集 ProviderNames 中 → 通过
	s.NoError(ActivateCustomAgentPaths("dsh"))
	// 未注册名 → 报错（Python: entry_point 发现不到 → ValueError 语义）
	err := ActivateCustomAgentPaths("ghost-agent")
	s.Require().Error(err)
	s.Contains(err.Error(), "ghost-agent")
}

func (s *CustomAgentSuite) TestTeamMCPServerCommand() {
	// D-15.16-d4：Go 无 frozen 形态，恒返回 uapclawmcpserver argv
	cmd := TeamMCPServerCommand()
	s.Require().Len(cmd, 1)
	s.Equal("uapclawmcpserver", cmd[0])
}

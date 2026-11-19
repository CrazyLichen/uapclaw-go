package swarm

import (
	"testing"

	"github.com/stretchr/testify/suite"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// AssemblyTransportSuite 15.16d transport 装配段测试。
// Python 对照：assembly.py:85-112（_external_team_publish_url /
// _ensure_external_team_transport）。
type AssemblyTransportSuite struct{ suite.Suite }

func TestAssemblyTransportSuite(t *testing.T) {
	suite.Run(t, new(AssemblyTransportSuite))
}

func (s *AssemblyTransportSuite) TestExternalTeamPublishURLTui() {
	s.T().Setenv("GATEWAY_PORT", "19001")
	s.Equal("ws://127.0.0.1:19001/tui", externalTeamPublishURL("tui"))
}

func (s *AssemblyTransportSuite) TestExternalTeamPublishURLWeb默认() {
	s.T().Setenv("WEB_PORT", "")
	s.T().Setenv("WEB_PATH", "")
	s.Equal("ws://127.0.0.1:19000/ws", externalTeamPublishURL("web"))
	s.Equal("ws://127.0.0.1:19000/ws", externalTeamPublishURL("")) // 空渠道归 web（Python :86）
}

func (s *AssemblyTransportSuite) TestExternalTeamPublishURL自定义路径补斜杠() {
	s.T().Setenv("WEB_PORT", "19000")
	s.T().Setenv("WEB_PATH", "customws")
	s.Equal("ws://127.0.0.1:19000/customws", externalTeamPublishURL("web"))
}

func (s *AssemblyTransportSuite) TestEnsureExternalTeamTransport幂等() {
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "t1"
	ensureExternalTeamTransport(&spec, "web")
	s.Require().NotNil(spec.ExternalTransport)
	s.Equal("hybrid", spec.ExternalTransport.Type)
	s.Equal("t1", spec.ExternalTransport.Params["team_name"])
	s.Equal("ws://127.0.0.1:19000/ws", spec.ExternalTransport.Params["external_publish_url"])
	// 幂等：已配置直接返回（Python :99-100）
	ensureExternalTeamTransport(&spec, "tui")
	s.Equal("ws://127.0.0.1:19000/ws", spec.ExternalTransport.Params["external_publish_url"])
}

func (s *AssemblyTransportSuite) TestEnrich挂载Transport() {
	// enrich 主链路调用位对齐 Python :425
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "t-enrich"
	s.Require().NoError(EnrichTeamSpecForSwarmSeed(&spec, EnrichSeedParams{
		SessionID: "s1",
		Mode:      "team",
		ChannelID: "web",
	}))
	s.Require().NotNil(spec.ExternalTransport)
	s.Equal("hybrid", spec.ExternalTransport.Type)
	s.Equal("t-enrich", spec.ExternalTransport.Params["team_name"])
}

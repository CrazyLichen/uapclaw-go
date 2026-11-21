// uapclawmcpserver team-mcp stdio server 入口。
//
// 把团队协作（真团队工具 / operator 控制面）经 stdio MCP 暴露给外部 agent
// （被 spawn 的第三方 CLI 或外部操作者界面）。入团凭据从
// OPENJIWEN_TEAM_JOIN 环境变量读取；server 由 CLI 子进程拉起、随 stdio
// 断开退出。
//
// 默认启动命令名已由 openjiuwen-team-mcp 改为 uapclawmcpserver（用户拍板，
// D-15.16-b6）；OPENJIWEN_TEAM_JOIN 变量名属协议字符串不改。
package main

import (
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/mcp"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/spawn"
)

func main() {
	// 共享 DB 工厂接线：ExternalTeamClient.Connect 依赖它打开团队库；
	// 独立二进制不走 runtime 包的装配（external_cli_spawn_wiring.go），
	// 必须在此自接（复用 spawn.GetSharedDB 的按连接串缓存与 memory 分支）。
	external.SetSharedDBFactory(spawn.GetSharedDB)
	mcp.MainFromEnv()
}

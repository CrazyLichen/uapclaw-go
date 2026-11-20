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
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/mcp"
)

func main() {
	mcp.MainFromEnv()
}

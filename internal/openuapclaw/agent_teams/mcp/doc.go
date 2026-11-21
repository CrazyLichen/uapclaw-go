// Package mcp 提供 team-mcp stdio server：把团队协作（真团队工具 / operator
// 控制面）经 stdio MCP 暴露给外部 agent。入口可执行为 cmd/uapclawmcpserver。
//
// member scope 暴露真 teammate TeamTool 实例（外部成员与进程内成员同一批
// 工具面）；operator scope 经 ExternalTeamClient 操作团队（任务板/消息/名册）。
// 入团凭据从 OPENUAPCLAW_TEAM_JOIN 环境变量读取；server 由 CLI 子进程拉起、
// 随 stdio 断开退出，宿主零监控负担。
//
// 文件目录：
//
//	mcp/
//	└── server.go         # stdio MCP server（BuildServer / operator 工具集 / member TeamTool 投影）
//
// 对应 Python 代码：openjiuwen/agent_teams/mcp/server.py（308 行）
package mcp

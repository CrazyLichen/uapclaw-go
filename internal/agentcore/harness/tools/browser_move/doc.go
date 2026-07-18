// Package browser_move 提供浏览器运行时工具集，支持 Playwright MCP 浏览器自动化。
//
// 本包实现 BrowserAgent 的运行时内核、进度追踪 Rail、
// 紧凑探测工具、自定义动作控制器和 Worker Agent 构建器。
//
// 文件目录：
//
//	browser_move/
//	├── doc.go              # 包文档
//	├── env.go              # 环境变量解析工具
//	├── config.go           # RuntimeSettings + BrowserRunGuardrails + MCP 配置工厂
//	├── parsing.go          # JSON 解析工具
//	├── progress.go         # BrowserTaskProgressState 进度状态
//	├── service.go          # BrowserService 后端服务
//	├── runtime.go          # BrowserAgentRuntime 运行时内核
//	├── browser_rail.go     # BrowserRuntimeRail 进度追踪 Rail
//	├── runtime_tools.go    # 7 个运行时辅助工具
//	├── probes.go           # JavaScript 探测代码生成（BuildInteractiveProbeJS / BuildCardProbeJS）
//	├── controllers.go      # BaseController 接口 + ActionController 注册/调度 + 内置动作
//	├── agents.go           # Browser Worker Agent 构建器 + 系统提示词
//	├── profiles.go         # BrowserProfile + BrowserProfileStore JSON 配置存储
//	└── site_profiles.go    # 站点配置文件和选择器缓存（⤵️ 9.38-49 回填）
//
// 对应 Python 代码：openjiuwen/harness/tools/browser_move/
package browser_move

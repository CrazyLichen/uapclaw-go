// Package web_tools 提供联网搜索和网页抓取工具集。
//
// 本包完整复刻 Python openjiuwen/harness/tools/web_tools.py，
// 包含三个核心工具：WebFreeSearchTool（免费搜索）、WebPaidSearchTool（付费搜索）、
// WebFetchWebpageTool（网页抓取），以及 HTTP 客户端、HTML 解析、调试输出等辅助功能。
//
// 工具在 Agent 会话中位于「LLM 决策 → 工具执行」层，提供「搜索→抓取→阅读」链路。
// 注意：search_tools（工具发现）和 load_tool（工具加载）是不同的概念——它们搜索的是
// Agent 自身可用的工具，不涉及联网。
//
// 文件目录：
//
//	web_tools/
//	├── doc.go              # 包文档
//	├── env.go              # 环境变量常量与读取函数
//	├── http_client.go      # HTTP 客户端封装（代理、重试、超时、SSL）
//	├── html_parser.go      # HTML 解析封装（goquery 选择器、正文提取、标签清理）
//	├── helpers.go          # 共享辅助函数（CJK 检测、URL 解码、评分、查询词提取等）
//	├── debug.go            # 调试输出逻辑（环境变量控制、JSON 转储）
//	├── free_search.go      # WebFreeSearchTool + DDG/Bing 搜索逻辑
//	├── paid_search.go      # WebPaidSearchTool + 4 个付费 provider
//	├── fetch_webpage.go    # WebFetchWebpageTool + HTML 正文提取
//	└── create.go           # CreateWebTools 工厂函数
//
// 对应 Python 代码：openjiuwen/harness/tools/web_tools.py
package web_tools

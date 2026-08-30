// Package search 提供搜索操作统一路由器。
//
// 本包实现 SearchManager，按记忆类型分发语义搜索请求，
// 聚合结果并按 score 排序截断。同时提供分页列表、用户画像、
// 摘要和变量查询等便利方法。
//
// 文件目录：
//
//	search/
//	├── doc.go               # 包文档
//	└── search_manager.go    # SearchManager 搜索路由器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/search/
package search

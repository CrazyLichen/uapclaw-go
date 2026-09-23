// Package ace 提供 ACE（Adaptive Context Evolution）算法的检索操作。
//
// ACE 检索不使用语义搜索，而是全量加载 playbook bullets，
// 通过 metadata 过滤获取指定用户的所有 ACE 记忆。
//
// 文件目录：
//
//	ace/
//	├── doc.go           # 包文档
//	├── run.go           # ACERecallMemoryOp 检索操作
//	└── run_test.go      # 测试
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/retrieve/task/ace/
package ace

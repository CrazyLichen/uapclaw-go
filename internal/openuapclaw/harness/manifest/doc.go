// Package manifest 提供 harness 内置元素的注册名常量与清单面。
//
// 当前内容：builtin_elements.py 1:1——常量段（:64-81，19 个 provider 注册名）
// + 构造注册段（:84-420 各默认 rail/tool/subagent provider +
// ensure_builtin_elements_registered 等价幂等注册）。构造
// 注册段 ⤴️ rail 注册表章节。
//
// 文件目录：
//
//	manifest/
//	├── doc.go                # 包文档
//	└── builtin_elements.go  # 内置元素常量 + 构造注册段（builtin_elements.py 1:1）
//
// 对应 Python 代码：openjiuwen/harness/manifest/builtin_elements.py
package manifest

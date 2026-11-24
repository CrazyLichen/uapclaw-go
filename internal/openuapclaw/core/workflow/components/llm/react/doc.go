// Package react 8.26 ReAct 工作流组件薄壳——把整个 ReAct Agent（领域 6）作为
// 一个 workflow 图节点嵌入，是 workflow 确定性图与 single_agent 自主多轮循环
// 两个世界的桥接节点。
//
// Python: openjiuwen/core/workflow/components/llm/react/
//
//	├── react_comp.go        # ReActAgentComp 组合面（react_component.py）
//	├── react_config.go      # ReActAgentCompConfig 配置（react_config.py）
//	├── react_executable.go  # ReActExecutable 执行体 + stream 解包（react_executable.py）
//	└── doc.go               # 包文档
//
// 对应 Python 代码：openjiuwen/core/workflow/components/llm/react/（3 文件 136 行）
package react

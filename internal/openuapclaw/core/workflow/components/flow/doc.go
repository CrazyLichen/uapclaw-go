// Package flow 提供工作流流程控制组件：Start/End/Branch/SubWorkflow。
//
// 文件目录：
//
//	flow/
//	├── doc.go                # 包文档
//	├── start_comp.go         # Start 组件（透传）
//	├── end_comp.go           # End 组件（模板渲染/流叶转发）
//	├── branch_router.go      # Branch + BranchRouter（排他分支路由，8.19）
//	├── branch_comp.go        # BranchComponent（排他网关组件，8.19）
//	├── sub_workflow_comp.go  # SubWorkflowComponent（子工作流嵌套，8.14b）
//	└── testutil_test.go     # 测试桩（仅 _test.go）
//
// 对应 Python 代码：openjiuwen/core/workflow/components/flow/
package flow

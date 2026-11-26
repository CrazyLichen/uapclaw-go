// Package workflow_drawable 8.13 Graph Visualization 全链集成测试。
// 真实跨子系统链路：workflow（门面 + 组图 drawable 钩子五段 + Draw 三分支）→
// components/flow+loop（BranchRouter 分支投影 + LoopGroup 子图 + break 登记，
// D-19-1/D-20-2 回填件）→ visualization（Drawable 镜像 + Mermaid 拼接 + 渲染）。
// 对齐 Python: openjiuwen/core/graph/visualization/ + _workflow.py drawable 段。
package workflow_drawable

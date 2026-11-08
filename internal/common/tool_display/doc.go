// Package tool_display 提供工具调用展示辅助：call_goal schema 注入与参数剥离。
//
// 优先使用主模型在 tool_call 参数里一并给出的 call_goal（一句目标说明，
// 如「调研 openJiuwen 官网信息」）；执行前会从 arguments 剥掉该字段以免
// schema 校验失败。可读工具标题由前端根据 name 做 i18n，后端不再生成
// display_name。
//
// 注意：绝不能用 display_name 作为 UI 字段名——team 的 spawn_teammate /
// build_team 等工具本身就有必填的 display_name（成员展示名），撞名会导致
// 参数被误剥、持续报错。
//
// 文件目录：
//
//	tool_display/
//	├── doc.go           # 包文档
//	└── tool_display.go  # call_goal schema 注入 + 参数剥离
//
// 对应 Python 代码：jiuwenswarm/common/tool_display.py
package tool_display

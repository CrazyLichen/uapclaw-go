// Package ctx 提供请求级的跨层上下文携带能力（RunContext 与请求级权限覆盖）。
//
// 本包存在的原因是一条真实的循环依赖：rails/permissions 需要
// WithRunPermissions / OverlayRunPermissions（一次请求的临时工具权限选择），
// 而 runtime 的 permission_catalog 又需要 permissions.CapturePermissionLayers
// （读三层权限 YAML）。两边互相 import，Go 编译期直接禁止。
//
// Python 侧靠"函数内 import"绕过（permission_interrupt_rail.py:242/249/259、
// policy_eval.py:225、permission_catalog.py:207 全部写在函数体内），把依赖边
// 推迟到运行期。Go 没有这个手段，因此把**双方共用的这一层**下沉到本包：
//
//	runtime/ctx  ←─ runtime           （RunContext / 请求级权限覆盖）
//	runtime/ctx  ←─ rails/permissions （同上）
//
// 两包从此单向依赖本包，runtime 可正常 import permissions，无需类型镜像。
//
// 文件目录：
//
//	ctx/
//	├── doc.go               # 包文档
//	├── run_context.go       # RunContext 一次调用选项 + CURRENT_RUN_CONTEXT ctx 绑定
//	└── run_permissions.go   # 请求级权限覆盖层（overlay_run_permissions）
//
// 对应 Python 代码：jiuwenswarm/runtime/run_context.py（20 行）、
// jiuwenswarm/runtime/run_permissions.py（54 行）
package ctx

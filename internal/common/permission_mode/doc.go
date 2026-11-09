// Package permission_mode 提供任务级权限模式判定。
//
// 只读 `permissions.mode` 字符串（manual / auto），不做归一化改写——
// 运行时的配置归一化在 permissions 包的 auto_config 里完成。
//
// 文件目录：
//
//	permission_mode/
//	├── doc.go              # 包文档
//	└── permission_mode.go  # 模式常量与四个判定函数
//
// 对应 Python 代码：jiuwenswarm/common/permission_mode.py
package permission_mode

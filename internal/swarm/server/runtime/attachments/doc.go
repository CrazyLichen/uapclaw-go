// Package attachments 提供 server 侧媒体/文档附件落盘的中立工具。
//
// Python: jiuwenswarm/server/runtime/attachments/（upload_storage /
// media_attachments / document_attachments / steer_images）。
// 全部围绕当前会话 uploads 目录（agent/sessions/<sid>/uploads/）读写：
// 命名规则三方共用（gateway/common/attachment_safety.py → Go attachmentsafety 包），
// 落盘原子去重（O_EXCL）、分块读、steer 引用校验均在此实现。
// 企业版 OBS/租户分支不移植（spec D-27.8-4）。
//
// 文件目录：
//
//	attachments/
//	├── doc.go                    # 包文档
//	├── upload_storage.go         # 落盘原语：原子去重写/去重命名/分块读
//	├── stat_unix.go              # 硬链接数/nlink（Unix Stat_t）
//	├── stat_windows.go           # 硬链接数/nlink（Windows 占位恒 1）
//	├── upload_storage_test.go
//	├── media_attachments.go      # 图片 normalize/discard
//	├── media_attachments_test.go
//	├── document_attachments.go   # 文档校验落盘与黑名单
//	├── document_attachments_test.go
//	├── steer_images.go           # steer 引用校验与真实格式探测
//	└── steer_images_test.go
//
// 对应 Python 代码：jiuwenswarm/server/runtime/attachments/
package attachments

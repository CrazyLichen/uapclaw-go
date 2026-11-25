// Package attachmentsafety 提供上传文件名安全与扩展名黑名单（纯函数）。
//
// Python: gateway/common/attachment_safety.py（197 行，gateway/runtime/server 三方共用）。
// 消毒、截断、黑名单判定、data-uri 前缀剥离与图片 MIME→后缀映射在此统一定义，
// 供 Gateway web 通道与 server 侧 attachments 包共用，避免各处重复推导。
//
// 文件目录：
//
//	attachmentsafety/
//	├── doc.go                  # 包文档
//	├── attachment_safety.go    # 消毒/截断/黑名单/data-uri/图片 MIME 助手
//	└── attachment_safety_test.go # 边界用例
//
// 对应 Python 代码：gateway/common/attachment_safety.py
package attachmentsafety

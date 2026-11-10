// Package usageattr 定义用量归属（usage attribution）的载荷类型。
//
// 归属回答"这次模型调用的用量属于谁"：发起方 agent、所属团队/成员、父会话、
// 委派链、调用链路径与深度。Python 用 dict 承载并原地更新，Go 用具名 map
// 保持同构，只把签名上的裸 map 收成可读的类型。
//
// 文件目录：
//
//	usageattr/
//	├── doc.go         # 包文档
//	└── attribution.go # Attribution 类型
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py（context_usage_attribution 载荷）
package usageattr

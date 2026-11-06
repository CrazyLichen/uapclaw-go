//go:build integration

// Package model_anomaly 提供模型异常检测护栏（ModelAnomalyDetectionRail）的集成测试。
//
// 覆盖三条只有端到端才成立的链路：
//   - 流式分片检查器 → 重复输出检测 → on_model_exception 重试 → 第二轮成功
//   - Model.Stream 首帧超时 → StreamError 哨兵 → rail 重试 → 第二轮成功
//   - 工具循环压缩配置透传到真实 DeepAgent（装配可达性）
//
// 对齐上游：openjiuwen/harness/rails/model_anomaly_detection_rail.py
//
// 文件目录：
//
//	model_anomaly/
//	├── doc.go          # 包文档
//	└── anomaly_test.go # 三个端到端闭环
package model_anomaly

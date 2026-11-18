// Package swarmseed_test 平台 seed 生产工厂集成测试（15.16c）。
//
// 覆盖 enrich seed 生产段 → spec JSON 往返 → MaterializeBuildContext 经
// 平台工厂重建 → externalcli 消费端（ResolveExternalPaths cwd 选择链）
// 的端到端链路；以及基线空壳（无 seed）行为不回归。
//
// 文件目录：
//
//	swarmseed/
//	├── doc.go               # 包文档
//	└── seed_e2e_test.go    # SwarmSeedSuite：seed 端到端往返与消费端接线
//
// 对应 Python 代码：jiuwenswarm/agents/swarm/{assembly,context,registry}.py
package swarmseed

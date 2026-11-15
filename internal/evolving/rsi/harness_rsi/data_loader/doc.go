// Package dataloader 提供评估数据集的载入、校验、批次规划与剖析。
//
// DataLoader 把一个数据集目录读成批次化的 case 映射；BatchPlanner 按均衡键分组，
// grading_contract 归一评分契约，profiler 输出数据集统计画像，plan_store 持久化
// 批次计划，reference_adapter 适配引用答案格式。
//
// 文件目录：
//
//	data_loader/
//	├── doc.go               # 包文档
//	├── loader.go            # DataLoader 主入口（目录 → 批次）
//	├── batch_planner.go     # 批次均衡规划（BatchPlanner）
//	├── case_files.go        # 数据集文件校验（validate_dataset_files）
//	├── grading_contract.go  # 评分契约归一（normalize_grading_case）
//	├── plan_store.go        # 批次计划持久化（BatchPlanStore）
//	├── profiler.go          # 数据集画像（DatasetProfiler）
//	└── reference_adapter.go# 引用适配（adapt_reference）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/data_loader/
package dataloader

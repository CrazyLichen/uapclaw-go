// Package operator 提供自演化的参数句柄抽象。
//
// Operator 是自演化框架的参数句柄，不是执行单元。执行由消费者（Agent/Rail）
// 使用 Operator 管理的参数来完成。
//
// Operator 提供统一接口让演化框架能够：
//   - 通过 operator_id 标识参数（用于轨迹归因）
//   - 通过 GetTunables 描述可调参数
//   - 通过 GetState 读取当前值（用于检查点/回滚）
//   - 通过 SetParameter 更新参数（演化更新入口，检查冻结标记）
//   - 通过 LoadState 从检查点恢复（不检查冻结标记）
//
// 参数变更通过 onParameterUpdated 回调推送给消费者，确保即时同步。
//
// 文件目录：
//
//	operator/
//	├── doc.go            # 包文档
//	├── base.go           # Operator/PreviewableOperator 接口 + TunableSpec + DefaultApplyUpdate
//	├── llm_call/
//	│   ├── doc.go        # 子包文档
//	│   └── llm_call_operator.go  # LLMCallOperator
//	├── tool_call/
//	│   ├── doc.go        # 子包文档
//	│   └── tool_call_operator.go # ToolCallOperator
//	├── memory_call/
//	│   ├── doc.go        # 子包文档
//	│   └── memory_call_operator.go # MemoryCallOperator
//	└── skill_call/
//	    ├── doc.go        # 子包文档
//	    └── skill_experience_operator.go # SkillExperienceOperator
//
// 对应 Python 代码：openjiuwen/core/operator/
package operator

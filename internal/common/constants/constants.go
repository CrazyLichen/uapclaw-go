package constants

// ──────────────────────────── 常量 ────────────────────────────

// MaxCollectionSize 表达式集合尺寸上限（constant.py:25）
const MaxCollectionSize = 100000

// MaxExpressionLength 表达式长度上限（constant.py:26）
const MaxExpressionLength = 5000

// MaxASTDepth 表达式 AST 嵌套深度上限（constant.py:27）
const MaxASTDepth = 50

// LoopID 循环节点标识 state 键（constant.py:20，Python 值逐字）
const LoopID = "__sys_loop_id"

// Index 循环轮次 state 键（constant.py:21，Python 值逐字）
const Index = "index"

// FinishIndex 已完成轮次 state 键（constant.py:22，Python 值逐字）
const FinishIndex = "finish_index"

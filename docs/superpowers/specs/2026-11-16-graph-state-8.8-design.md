# 8.8 GraphState（节点状态片段）设计

- 日期：2026-11-16
- 状态：已批准（方案 A，2026-11-16 会话拍板）
- Python 基线：`openjiuwen/core/graph/graph_state.py`（全文 5 行有效代码）
- 上游计划：`IMPLEMENTATION_PLAN.md` 领域八 8.8 行（⤵️ 后移拍板 2026-11-15，本节提前落地收口）

## 1. Python 事实基线

```python
import operator
from typing import TypedDict, Annotated

class GraphState(TypedDict):
    source_node_id: Annotated[list, operator.add]
```

全 Python 侧消费者仅一处：`vertex.py:364` 的 `BatchVertex`（实为 `class Vertex`，日志文案 "batch-in node"）`__call__(self, state: GraphState, config)` 的**类型标注**；该节点成功后 `return {"source_node_id": [self._node_id]}`（vertex.py:385），但 `task.py:152` 丢弃节点返回值，且 `operator.add` reducer 在全仓（graph/workflow/pregel）**无任何归并执行点**。

结论：**零运行时效果的声明**。作用是对齐 LangGraph 惯例、给未来 Vertex 的 state 形参提供类型落点。

## 2. 定性与同名辨析

| | 8.8 GraphState（本节） | 8.9 GraphState（已实现，`pregel/state.go`） |
|---|---|---|
| 本质 | TypedDict 类型标注 | struct 实例（检查点快照） |
| 内容 | 一个键：执行过的来源节点列表 | ns/step/channel_values/pending_nodes |
| 消费者 | vertex.py 类型提示（唯一） | engine.go Init/RunStep/保存恢复 |
| 运行时效果 | 零 | 断点续跑核心 |

Python 靠模块路径（`graph.graph_state` vs `graph.store`）区分；Go 靠包隔离（`internal/agentcore/graph/` vs `internal/agentcore/graph/pregel/`）区分。

## 3. 文件映射（规则 9 一比一）

| Python | Go |
|---|---|
| `openjiuwen/core/graph/graph_state.py` | `internal/agentcore/graph/graph_state.go`（新包 `graph` 的唯一实现文件） |
| —（Python 无包文档） | `internal/agentcore/graph/doc.go`（规则 1） |
| —（Python 无测试） | `internal/agentcore/graph/graph_state_test.go`（规则 3） |

Python 一个模块文件 → Go 一个实现文件；模块级 TypedDict → 包级 struct；无方法、无函数（Python 侧就没有，禁止发明 Merge 等助手）。

## 4. Go 类型设计

```go
// GraphState 节点输出聚合 schema，对齐 openjiuwen/core/graph/graph_state.py 的 TypedDict。
// Python 声明为 source_node_id: Annotated[list, operator.add]——语义为
// "执行过的来源节点 ID 以列表拼接（operator.add）追加式归并，不覆盖"。
// 全仓零实例化死类型：唯一 Python 消费者是 vertex.py:364 对 __call__ state 形参的类型标注，
// 节点返回值在 task.py:152 被丢弃，reducer 无归并执行点。
// 与 pregel/state.go 的检查点 GraphState 同名不同义，靠包隔离区分。
type GraphState struct {
    // SourceNodeID 执行过的来源节点 ID 列表（operator.add 追加语义）
    SourceNodeID []string
}
```

要点：
- 字段 `SourceNodeID []string`（Python `list` 按元素为节点 ID 收窄为 `[]string`，vertex.py:385 写入的就是 `[self._node_id]`）；
- 无 JSON tag、无序列化、无构造函数——Python 均无；
- 归并语义只存在于注释，不落代码（见偏差 D1）。

## 5. 已知偏差

| 编号 | Python | Go | 性质 | 回收条件 |
|---|---|---|---|---|
| D1 | `Annotated[list, operator.add]`（graph_state.py:8） | 结构体字段 + doc 注释承载语义，不发明归并方法 | Go 类型系统无 Annotated reducer 注解对应物；且该 reducer 在 Python 全仓从未被执行，实现归并逻辑属于为死代码发明行为 | 未来 8.11 Vertex 落地且真实消费该类型时，按消费形态补归并语义并回填本偏差 |

## 6. 测试设计

`graph_state_test.go`：
- 零值构造与字段读写；
- 追加语义验证（两次 append 拼接，仅字段级验证，对应 `operator.add` 语义的注释锚点）；
- 包覆盖率 ≥ 85%（单 struct 包，测试保证语句覆盖）。

## 7. 删除清单

无旧实现被替代：`SourceNodeID`/`source_node_id` 全仓 Go 0 命中，8.8 为全新落点，无残留清理项。

## 8. 计划表回填

- 8.8 行 ☐ → ✅，补 Go 事实，保留 ⚠️ 同名不同义警告；⤵️ 后移注记改「✅ 已提前落地（2026-11-16）」；
- 顺序链追加「九次记录」：8.8 于 8.9b 悬挂注销后落地，Pregel 子域（8.3/8.3a/8.3b/8.3c/8.3d/8.4/8.5/8.6/8.7/8.9）全部收口，领域八剩余 8.1/8.2/8.10-8.26。

## 9. 验收门槛

- `go build ./...` / `go vet ./...` 通过；
- `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/graph/...` 零 FAIL；
- 包覆盖率 ≥ 85%；
- 推送远端即交付（规则 11，不追踪 CI）。


## 回收核对（2026-11-17，8.11 Vertex 实施时）

`Vertex.__call__`（vertex.py:385）返回 `{"source_node_id": [node_id]}` 已在 Go `Vertex.Call` 兑现（internal/agentcore/graph/vertex.go），GraphState.SourceNodeID 迎来首个真实消费；reducer（Annotated[list, operator.add]）在上游仍无归并执行点（task.py:152 丢弃节点返回值），偏差 D1 结论维持：类型承载语义、注释承载 reducer、不发明归并方法。

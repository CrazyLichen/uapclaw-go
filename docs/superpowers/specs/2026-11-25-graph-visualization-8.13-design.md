# 8.13 Graph Visualization 设计（spec）

- 日期：2026-11-25
- 状态：已批准（brainstorming 四拍板见 §2）
- Python 真相源：`openjiuwen/core/graph/visualization/`（6 文件 419 行）
- 消费方基线：Go 侧领域八 8.1-8.26 全部完成，8.13 为领域八唯一缺口，三处预埋回填缝（§6）

## 1. 背景与流程位置

8.13 属领域八「工作流与图引擎」的图引擎子域收尾节。Python 侧它不是运行时执行链路的一环，而是**组图期副作用件 + 可视化出口**：`_workflow.py` 组图时同步喂入 `Drawable`（add_node/set_start_node/set_end_node/add_edge，_workflow.py:160-294），把工作流图镜像为 `DrawableGraph` 数据结构，最终产出 Mermaid 文本 / PNG / SVG（`workflow.py:494-499` Draw 三分支对外暴露）。

关键事实：Python 全程纯文本——`to_mermaid()` 只做字符串拼接；png/svg 由第三方包 `mermaid-py` 将脚本 base64 后 HTTP 请求外部服务 `mermaid.ink`（`MERMAID_INK_SERVER` env 可换自建实例）取回渲染结果（`img_response.content` = PNG 字节，`svg_response.text` = SVG 文本）。Python 本地不渲染。

## 2. 已批准拍板

| # | 拍板 | 内容 |
|---|---|---|
| ① | 落点方案 β | 落 `internal/openuapclaw/core/graph/visualization/`（对齐 Python 目录层级）；依赖方向 visualization → workflow/components 单向，与 Python 同向，已核验不成环（components 只 import `core/graph` 根包与 `core/graph/executable`，根包不 import visualization） |
| ② | 渲染方案 ① | `to_mermaid_png/svg` 一比一复刻：net/http 调 mermaid.ink（env `MERMAID_INK_SERVER` 可配），单测用 httptest 起本地 mock 服务 |
| ③ | `_get_targets` | 保留函数壳复刻（死代码复刻惯例，Python 侧该分支在 Go 侧无可达调用形态，见 D-13-1），**不删除** |
| ④ | 设计整体 | 按 §3-§7 写 spec |
| ⑤ | 环破除（D-13-5） | 写计划时依赖核验发现 workflow→visualization→flow/loop→workflow 成环（Python 靠 add_node 函数内延迟导入破环，Go 无此能力）。批准改为**能力接口断言分派**（见 §4.2/§5 D-13-5）：visualization 只依赖 common/exception；flow/loop/llm → visualization 实现接口；workflow → visualization 构造与 Draw。连带：`BaseWorkflow.drawable` 具体类型化 `*visualization.Drawable`；`Workflow.Draw` 签名 `expandSubgraph bool → any`（D-14-7 回收标注） |

## 3. 文件结构（六文件一比一映射）

| Go 文件 | ≙ Python | 内容 |
|---|---|---|
| `drawable_node.go` | drawable_node.py (10 行) | `DrawableNode{ID string; Name string; Metadata map[string]any}` |
| `drawable_edge.go` | drawable_edge.py (26 行) | `DrawableEdge{Source, Target string; Data string; Conditional, Streaming bool}` + `DrawableBranchRouter{Targets, Datas []string}` |
| `drawable_graph.go` | drawable_graph.py (15 行) | `DrawableGraph{Nodes map[string]*DrawableNode; Edges []*DrawableEdge; StartNodes, EndNodes, BreakNodes []*DrawableNode}` |
| `drawable_subgraph_node.go` | drawable_subgraph_node.py (12 行) | `DrawableSubgraphNode`（嵌 `DrawableNode` + `Subgraph *DrawableGraph`，Python 继承 → Go 嵌入） |
| `drawable.go` | drawable.py (356 行) | `Drawable`（构造/add_node/set_start_node/set_end_node/set_break_node/add_edge/to_mermaid/to_mermaid_png/to_mermaid_svg/get_graph）+ `_MermaidDiagram` 同文件（Python 同文件：NodeIdGenerator/LinkIdGenerator/_SubGraphNode/_ExtendLink/to_mermaid/_gen_mermaid_node/_gen_mermaid_links） |
| `doc.go` | — | 包文档（规则 1） |

另：Python `__init__.py` 为空文件，Go 包天然无对应物，不落。

## 4. 核心设计

### 4.1 数据结构

- 四个 dataclass 一比一；`DrawableEdge.Data` 类型偏差见 D-13-2（Python `Stringifiable` 协议 → Go `string`）
- `DrawableGraph.BreakNodes` Python 为 `Optional` → Go nil 切片承载（nil ≙ None，非空即切片）

### 4.2 Drawable.add_node 类型分派（drawable.py:51-81）

Python isinstance 链 → Go **能力接口断言**（偏差 D-13-5，Python 函数内延迟导入在 Go 不可用的唯一出口；断言顺序=Python isinstance 分支序）。visualization 包定义四个接口：

```go
// 循环体子图源（Python isinstance(LoopComponent/AdvancedLoopComponent) 分支）
type LoopSubgraphSource interface {
    LoopDrawableSubgraph() *DrawableGraph
}
// 子工作流子图源（Python isinstance(SubWorkflowComponent) 分支）
type SubgraphSource interface {
    SubWorkflowDrawableSubgraph() *DrawableGraph
}
// 分支路由源（Python isinstance(BranchComponent/IntentDetectionComponent) 分支；
// 实现：BranchComponent.DrawableRouter() / IntentDetectionComponent.DrawableRouter()
// 返回 component.Router() 供条件边展开）
type BranchRouterSource interface {
    DrawableRouter() any
}
```

分派步骤一比一：

1. `LoopSubgraphSource` → 取 `LoopDrawableSubgraph()`；子图 `len(EndNodes)==0` 时**出度=0 自动发现 end_nodes**（drawable.py:62-70 逐行复刻）→ 包 `DrawableSubgraphNode` + 记入 loopNodes 集合 + `addEdge(nodeID, nodeID)` 自环边
2. `SubgraphSource` → 取 `SubWorkflowDrawableSubgraph()` → `DrawableSubgraphNode`
3. `BranchRouterSource` → `DrawableNode` + `addEdge(source=nodeID, conditional=true, data=DrawableRouter())`
4. 默认 → 普通 `DrawableNode`

实现方（flow/loop/llm 包，import visualization 实现接口）：

- `LoopComponent.LoopDrawableSubgraph()`：`g.loopGroup.Drawable().GetGraph()`（LoopGroup 嵌入 BaseWorkflow 继承 Drawable 访问器）
- `AdvancedLoopComponent.LoopDrawableSubgraph()`：`a.body` 为 `*LoopGroup` 时同上断言取图（Python `component.body.drawable.get_graph()`）
- `SubWorkflowComponent.SubWorkflowDrawableSubgraph()`：`c.subWorkflow.Internal.Drawable().GetGraph()`（Python `sub_workflow._internal.drawable.get_graph()`；需 Workflow 补 `Internal()` 访问器）
- `BranchComponent.DrawableRouter()` / `IntentDetectionComponent.DrawableRouter()`：返回 `c.router`

### 4.3 add_edge 条件边展开（drawable.py:104-124）

- source 在 loopNodes → 强制 conditional 边直接落（drawable.py:107-109）
- 非 conditional → 直落
- conditional：data 实现 `DrawableBranchTargetsSource` 接口（`GetDrawableBranchTargets() (targets, datas []string)`，实现在 `*flow.BranchRouter`，即 D-19-1 回填件 `GetDrawableBranchRouter` 的接口化形态）→ 取 targets+datas；否则走 `_get_targets(data)`（见 D-13-1）；逐 target 展开 DrawableEdge，datas 缺省 None → Go 空串

### 4.4 `_get_targets`（drawable.py:29-43）

Python 逻辑：callable/class → `get_type_hints(func)["return"]` 为 `Literal[...]` 时取 args 作 targets。Go 无类型注解反射，且 Go 侧 `BranchComponent.Router()` 与 `IntentDetectionComponent.Router()` 恒返回 `*flow.BranchRouter` 具体类型（4.2 分支 3 恒走 4.3 的 BranchRouter 路径），该函数在 Go 侧**无可达调用形态**。按拍板 ③：保留函数壳一比一复刻（签名 `getTargets(data any) []string`，非函数/函数无 Literal 等价物时返回 nil），注释登记 D-13-1。

### 4.5 _MermaidDiagram 文本拼接（drawable.py:143-356）

自实现 mermaid 脚本拼接，**不引第三方 mermaid 库**（Go 生态无等价物）：

- `NodeIdGenerator`/`LinkIdGenerator`：`node_1`/`link_1` 自增前缀
- `_ExtendLink.__str__`（drawable.py:210-228）输出格式逐字对齐：`origin.id_ + " " + [id_@] + head_left + shape + head_right + [|"message"|] + " " + end.id_ + [\nid@{k: v, ...}]`
- 节点形状：start/end 节点 round-edge，其余 normal；条件边 DOTTED、流式边 THICK，流式+animation 时挂 `id_`/`{animate: true}` properties
- `_gen_mermaid_node`：子图递归展开，`expand_subgraph` 层数控制；形参一比一用 `expandSubgraph any` 承载 bool|int（偏差 D-13-3）
- 子图边界连接四象限展开（drawable.py:334-352）：外→子图用 subgraph_start_nodes、子图→外用 subgraph_end_nodes+subgraph_break_nodes、子图→子图笛卡尔积
- 参数校验三分支（title 非串 / expand_subgraph 非 bool 且 int<0 / enable_animation 非串）→ `StatusDrawableGraphToMermaidInvalid` 错误码（**核对：Go codes_graph.go 已有 112020-112022 三件，to_mermaid 校验码需新增**）

### 4.6 png/svg 渲染（拍板 ②）

- `to_mermaid_png/svg`：先 `to_mermaid` 产脚本 → base64（URL-safe，对齐 mermaid-py `_process_diagram`）→ GET `{MERMAID_INK_SERVER}/img/{b64}`（png）/`/svg/{b64}`（svg），读响应体返回 `([]byte, error)`
- 服务地址：env `MERMAID_INK_SERVER` 默认 `https://mermaid.ink`（常量与 Python 同名 env 对齐）
- 单测：httptest 起本地服务 mock 端点（规则 3.3：可 mock 的必须 mock，httptest 即真实边界）

## 5. 偏差登记（D-13-x）

| 编号 | Python 事实 | Go 等价 | 性质/理由 | 回收条件 |
|---|---|---|---|---|
| D-13-1 | `_get_targets` 靠 `get_type_hints`+`Literal` 反射 callable 返回类型（drawable.py:29-43） | 保留函数壳 `getTargets(data any) []string`，无可达调用形态（两条 Router() 恒 `*BranchRouter`） | 语言层缺类型注解反射 + Go 具体类型化使分支不可达 | 上游若把 router 泛化为任意 callable 且出现非 BranchRouter 形态时重新评估 |
| D-13-2 | `DrawableEdge.data: Optional[Stringifiable]`（Protocol，duck-typed） | `Data string` | Go 无 Protocol；调用方实传均为字符串标签（BranchRouter datas 即 []string） | 出现非字符串 data 消费方时改 `any` |
| D-13-3 | `expand_subgraph: int \| bool`（Python union 动态分派） | 形参 `expandSubgraph any`，运行时 bool/int 分派复刻；**修订（审查拍板 2026-11-26）：`enableAnimation` 维持 `bool` 收窄不收 any**——Python `isinstance(enable_animation, bool)` 校验分支在 Go 静态类型下不可达（全仓消费方 `Draw()` 恒传 bool），校验不落，回收条件=出现非 bool 消费方 | 保持签名一比一（规则 12） | enableAnimation：出现非 bool 消费方时改 any 并补校验 |
| D-13-4 | mermaid-py 依赖（soft-import，缺包抛 ImportError，drawable.py:11-18/143-160） | 文本拼接自实现 + png/svg 直连 HTTP；无"缺包"形态（Go 静态编译） | 依赖差异；soft-import 分支不复刻 | 无 |
| D-13-5 | `Drawable.add_node` 内函数级延迟导入组件类型做 isinstance 分派（drawable.py:53-57），借此破 workflow↔visualization 环 | 能力接口断言分派（§4.2 四接口，断言序=Python isinstance 序；实现方 flow/loop/llm 反向 import visualization，依赖全单向零环） | Go 无函数内延迟导入，语言层缺能力唯一出口（规则 12.3，2026-11-25 用户批准） | Go 若引入延迟加载/插件化机制则回收为 type switch |

## 6. 回填清单（三处缝，同改动回收，规则 8）

| 缝 | 位置 | 回收内容 |
|---|---|---|
| **D-d** | `internal/openuapclaw/core/workflow/base_workflow.go:62-92,181` | `drawable any` 恒 nil → 具体类型 `*visualization.Drawable`（workflow → visualization 单向，已核验无环）；env 读取骨架（`WORKFLOW_DRAWABLE`=="true"）真接线（构造期非空创建，Python _workflow.py:157-160）+ 补 `Drawable()` 访问器供 LoopGroup/SubWorkflow 分支取子图；_workflow.py:200-294 组图钩子全链填充（add_node/set_start_node/set_end_node/add_edge 静态边/流式边/条件边五段）；workflow.go:241 `Draw()`（workflow.py:471-499 output_format 三分支）回收为真实现：`expandSubgraph bool → any`（偏差 D-14-7 同改动回收标注，层级语义兑现）；`_drawable is None` 分支（env 未开启时可达）保留同语义 |
| **D-19-1** | `internal/openuapclaw/core/workflow/components/flow/branch_router.go:60,76` | 三处：① 构造期 `WORKFLOW_DRAWABLE` env 判定 → drawable 字段 ② `GetDrawableBranchRouter()` 方法（branch_router.py:71，返回 DrawableBranchRouter）③ `AddBranch` 填充段（branch_router.py:58-64，drawable 非空时 append targets/datas） |
| **D-20-2** | `internal/openuapclaw/core/workflow/components/flow/loop/loop_comp.go:682,710-711` | `set_break_node` 段（loop_comp.py:83-84）：drawable 非空且组件类型匹配时登记 break node |

删除清单（规则 8）：
- base_workflow.go D-d 注释块与 `_ = drawableEnabled` 留档段
- branch_router.go D-19-1 注释块
- loop_comp.go D-20-2 注释块
- workflow.go Draw 空分支注释（保留 None 语义分支本体）
- grep 锚点：`D-d`（workflow 包内）、`D-19-1`、`D-20-2` 在 internal/ 中 0 命中（spec/plan 台账除外）

## 7. 测试策略

- 包覆盖率 ≥85%（规则 3）
- mermaid 文本输出：黄金串断言（node/link 自增序、round-edge/DOTTED/THICK 形状、`|"msg"|`、`{animate: true}` properties、子图嵌套缩进结构）
- 子图四象限边界连接：构造嵌套 loop + branch 混合图逐边断言
- `expand_subgraph` 层数递归：bool / int=0 / int=N / 非法值（-1、string）四分支
- png/svg：httptest mock mermaid.ink 端点，断言 URL 路径含 base64 脚本、响应体透传
- 参数校验三分支错误码断言
- 回填三处各配用例：env 开启时 BranchRouter.GetDrawableBranchRouter 有值 / AddBranch 填充 / loop break node 登记 / env 关闭时全链路与现状等价（回归锁）
- 集成：真实组图（start→branch→end + loop 子图）→ env 开启 → `Draw()` 输出完整 mermaid 脚本端到端

## 8. 错误码盘点

已有：112020 StartNodeInvalid / 112021 EndNodeInvalid / 112022 BreakNodeInvalid（codes_graph.go）。
需核对新增：`DRAWABLE_GRAPH_TO_MERMAID_INVALID`（Python StatusCode.DRAWABLE_GRAPH_TO_MERMAID_INVALID，实施时到 Python codes.py 取实际数值与文案登记）。

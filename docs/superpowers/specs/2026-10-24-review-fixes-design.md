# 2026-10-24 审查修复设计（S/M/T 全批次）

- **来源**：`docs/review/2026-10-24-48h-logic-review.md`（6S/17M/26T）
- **验证状态**：全部 S/M 级问题已对照 Python（`agent-core/openjiuwen` + `jiuwenswarm-develop/jiuwenswarm`）逐项核实存在，无冲突；代码基线 `d621d6db`（已 fetch 对齐 origin/main）
- **决策方式**：brainstorming 逐批确认，全部采用推荐方案（方案A，M-11 为方案B 实装 orderedmap）
- **关键探针结论**：
  - S-05/M-11 合并方案（Token 流 + wk8 orderedmap + UseNumber）与 Python `json.dumps(loads(x), indent=2, ensure_ascii=False)` 逐字节一致（大整数/1.0 字面/插入序三保真）
  - S-03 桥接探针：`Marshal(&ControllerOutputPayload) → Unmarshal(map)` 形状与 Python `model_dump(mode="json")` 逐键一致
  - M-14 探针：Python `content[:remaining]` 是**字符切片**（remaining 为字节差值但按字符切），Go 需按 rune 切片对齐
  - M-04 探针：sessionops 无循环依赖，gateway 可直接 import

## 修复清单

### S 级（6 项全修）

| # | 问题 | 修复方案 |
|---|---|---|
| S-01 | `/mode code` 映射错误 | `modeChangeCancelAndNotice` ActionModeOK 分支加一级映射（agent→AgentPlan / code→CodeNormal / team→Team），其余二级值走 ParseChannelMode |
| S-02 | hooks 并发写共享 map + defer 晚注册 | 4 个 On* 方法 `defer recoverHookPanic` 前移到第一行（治标）+ applyGatewayTimeout 构造新 slice 不就地写（治本），同步改 handler_test.go 3 处断言为"RunAll 收到的配置带 timeout" |
| S-03 | chunkToMap 丢弃结构体 payload | 三个 typed 分支加 `*cschema.ControllerOutputPayload` 桥接（Marshal→Unmarshal 产 map，形状=Python model_dump）；detectTaskFailed 加结构体直读分支（cop.Type/cop.Data[0].Text）；补结构体测试用例 |
| S-04 | 英文 header 复制粘贴错 | manager.go:308 改 `"\n\n## Your relevant memories\n\n"`，补英文分支断言用例 |
| S-05 | JSON 大整数 float64 失真 | 与 M-11 合并：json_parser.go 改 Token 流 + orderedmap + UseNumber 解码器（探针代码 ~60 行直接移植） |
| S-06 | rewind `.(int)` 断言永不命中 | slash_cmd.go 2 处 + team_completion.go 4 处（N-02 连带）改 float64/int/json.Number 宽容转换 |

### M 级（15 修 + 2 归档）

| # | 问题 | 修复方案 |
|---|---|---|
| M-01 | /mode cancel 加了守卫 | 去掉 hasActiveStreamTaskForSession 守卫 |
| M-02 | /branch new_sid 用 ChannelID | 先 resolveControlChannelType 再生成；E2A channel_id 字段同改 channel_type |
| M-03 | rewind 确认文案缺四行模板 | 逐字复制 Python L1040-1044（含不可逆警告/删除范围/`/rewind confirm {turn}` 格式/bash 文件影响说明） |
| M-04 | rewind 无本地 fallback | E2A 失败/错误 → sessionops.RewindSession 本地截断 → 通知带「（注意：上下文未同步截断）」+ Warning 日志 |
| M-05 | cancel→notice 顺序丢失 | newSessionCancelAndNotice/modeChange 两处 `go` 异步改同步调用 |
| M-06 | fallback 前缀推断过度受控 | resolveControlChannelType fallback 原样返回 `ChannelType(msg.ChannelID)`；删 inferChannelTypeFromID（唯一调用方） |
| M-07 | rewind preview 字节截断 | rune 口径截断（`[]rune(p)[:50]`） |
| M-08 | reason 兜底语义偏差 | 去掉 `&& v != ""`；非字符串 `fmt.Sprintf("%v", ...)` |
| M-09 | UserPromptSubmit 缺 str() 强转 | 类型 switch：string 直接用；float64/int/bool 走 Sprintf（仅 truthy），按 Python or 链回退 |
| M-10 | timeout 字符串静默默认 | runCommandHook/runPromptHook 两处 switch 加 `case string:`，Atoi 成功用、失败返回 NON_BLOCKING_ERROR |
| M-11 | JSON key 字母序 vs 插入序 | 与 S-05 合并实装：Token 流递归构造 `*orderedmap.OrderedMap[string, any]`（值 json.Number），wk8 MarshalJSON 保序输出 |
| M-12 | 编码检测次生问题 | 查表失败补 Warn 日志；UTF-32LE/BE 手工映射 `unicode.UTF32`；短文本检测器差异注释归档 |
| M-13 | U+FFFD vs 丢弃非法字节 | txt_md 解码后 `ReplaceAll("\uFFFD", "")`；json 先 utf-8 容错清洗（对齐 Python errors=ignore 读取） |
| M-14 | 个人记忆预算字节截断 | 按字符切片对齐 Python（`[]rune(content)[:remaining]`） |
| M-15 | lite closed 裸读 | L551 改调 `mgr.IsClosed()`（L550 已断言具体类型） |
| M-16 | list 工具直读本地 | 改走 `sysOp.Fs().ListFiles`，异常返回 `{"files":[]}` |
| M-17 | RunStreaming 空壳 vs 计划✅矛盾 | 实装分支1（`runner.ByAgent(h.deepAgent.(BaseAgent))` + `runner.BySessionID(sessionID)` → RunAgentStreaming，~15 行）；分支2 保持 TODO 但注释改为"待 plan-mode 种子化" |
| M-18 | SpawnConfig 缺字段 | RunnerConfig 用现成 `SerializeRunnerConfig(runner.GetConfig())` 补齐；LoggingConfig 记 TODO 归档（Go 无 sinks 结构无法复刻 per-member 目录） |

### T 级（修 5 + 归档其余）

| # | 修复方案 |
|---|---|
| T-01 | session_map.go save 改 Encoder+SetEscapeHTML(false)（同包 interaction_context 写法） |
| T-02 | load 对非 dict JSON 静默跳过（Unmarshal 到 any 后断言 map，非 map return） |
| T-03 | scope 非字符串值 `fmt.Sprintf("%v", v)` 后进同一 switch |
| T-08 | skills req_id 接 `generateRandomHex(3)` 随机后缀 |
| T-12 | HookMatcher.Matches 入口 `strings.TrimSpace(m.Matcher)` |

归档（不动代码，注释/文档记录）：T-04（Unix 秒粒度）、T-05/T-06/T-07/T-09/T-11（边界一致化/ACP 回填点）、T-10（12 前缀 ACP 回填时补）、T-13~T-17（可接受偏差）、T-18~T-20、N-03（登记备查）、T-25（保留 Python 缺陷防误修）。

## 验证计划

1. 每项修复配套测试：S-01（mode 映射表用例）、S-02（并发触发断言）、S-03（结构体 payload 用例）、S-04（英文 header）、S-05/M-11（大整数/浮点字面/key 序用例）、S-06（float64 payload 用例）、M-03（文案断言）、M-07/M-14（CJK 截断用例）、M-17（分支1 流式用例）
2. `go build ./...` + 相关包 `go test ./...` 全绿
3. 遵循规范：注释中文、声明顺序、日志同步 Python

## 风险与回滚

- S-02 治本改测试断言语义（"就地修改可观察"→"生效值可观察"）：Python 的就地修改语义在 Go 并发下是 bug 根源，测试改断言 target 行为非实现细节
- M-05 同步化：CancelAgentWorkForSession 内部 SendRequest 有超时（unaryRequestTimeoutSeconds）不会永久阻塞 forwardLoop；rewindSlashNotice L414 已有同步调用先例
- M-17 分支1：DeepAgent→BaseAgent 断言失败时（理论不可能，编译期已断言）回退空通道不 panic

# 2026-10-27 48h 逻辑审查报告

**审查窗口**：2026-10-25 04:02 ~ 2026-10-27 04:02（98 个提交）
**审查方法**：逐章对照 Python 参考源码（jiuwenswarm-develop / agent-core），核对方法签名、执行步骤、分支结构、模板/文案字面、异常→error 映射、日志对位；已归档偏差（各 spec §8）不重复报；关键指控均已机器验证（逐行读源+库源码核对）。

## 一、窗口内完成的实施计划章节

| 章节 | 内容 | 完成时间 | 历史审查 | 本轮处置 |
|------|------|---------|---------|---------|
| 13.11 | AutoParser 三件套 | 10-25 08:18 | 未审 | **全面审查** |
| 14.2 | devtools 枚举层 | 10-25 09:05 | 未审（14.3 时机器核对过计数） | 全面审查（零问题） |
| 11.11 | 心跳服务 | 10-25 11:29 | 实施后审查 3 项（ad90f4ce）+ Start/Stop 竞态（e21ebec9） | 独立复核 |
| 13.17 | TripleExtractor | 10-25 13:42 | 未审 | **全面审查** |
| 11.12 | IM Pipeline | 10-25 17:26 | 偏差 8 项已修（7ab4148f） | 独立复核+修复质量抽查 |
| 13.19+13.20 | Retriever 接口+实现+工厂 | 10-25 18:22 | 未审 | **全面审查** |
| 14.3 | 进度上报 | 10-25 18:54 | 时区已修（052bbfaa） | 独立复核 |

窗口内另有：cron 6 项修复（a07f3f68，已审）、13.11 exists 修复（6ef6a7a1）、风格修复（4ef51e3f/89e96c03/750f0e2b）等属已审事项收尾，不再重复。

**问题总览：严重 6 / 一般 11 / 提示 14，共 31 项。**

| 编号 | 级别 | 位置 | 标题 |
|------|------|------|------|
| S-01 | 严重 | 13.11 | linkRoute 注入链路断裂：导出构造参数不可构造 + nil 路由 panic |
| S-02 | 严重 | 13.17 | pyStr 超 int64 整数字面被 e 记法化（Python int 无界原样输出） |
| S-03 | 严重 | 13.17 | json-repair 库深修复路径 float32 截断（上游传导） |
| S-04 | 严重 | 13.17 | json-repair 库深修复路径超 int64 截断为 MaxInt64（上游传导） |
| S-05 | 严重 | 11.11 | config `target: ""` 显式禁用 relay 被反转为启用 "web" |
| S-06 | 严重 | 11.11 | active_hours `{}` 清除语义失效（注释声称清除、实现 no-op） |
| M-01 | 一般 | 13.11 | regexpMatcher 用 MatchString（search 语义）≠ Python re.match（头部锚定） |
| M-02 | 一般 | 13.11 | filepath.Ext 前导点文件名语义 ≠ os.path.splitext |
| M-03 | 一般 | 14.3 | StartStage details 深克隆偏离 Python 引用共享且未归档 |
| M-04 | 一般 | 14.3 | message/error 空串赋值：Go nil 判断 ≠ Python truthy 判断（4 处） |
| M-05 | 一般 | 11.11 | runLoop 因父 ctx 取消退出后 running 未复位（僵尸态） |
| M-06 | 一般 | 11.11 | session.list 缺 last_message_at 排序 + 负 offset 切片 panic（顺带发现，非 11.11 引入） |
| M-07 | 一般 | 11.12 | extractContent dict 分支无匹配键提前返回空，Python 落 str(content) 继续管线 |
| M-08 | 一般 | 11.12 | 出站步骤8 reply_scope 用 `.(string)` 断言，非字符串 truthy 值行为反转（R1 遗漏点位） |
| M-09 | 一般 | 11.12 | model_client_config/react 非 dict 时：Python 异常回退（LLM 不激活）vs Go 静默 env 回退（可能激活） |
| M-10 | 一般 | 13.20 | 工厂 default 分支错误文案多一个空格，与 Python 逐字符不一致 |
| M-11 | 一般 | 13.20 | BatchRetrieve 首错返回时机：Python gather 立即传播 vs Go errgroup.Wait 等全完 |
| T-01 | 提示 | 13.11 | factory() 第三方返回 nil 时 Parse panic |
| T-02 | 提示 | 14.3 | 回调失败日志 callback 字段固定串，无法定位故障回调 |
| T-03 | 提示 | 14.3 | 日志与回调通知先后顺序颠倒（锁外日志所致） |
| T-04 | 提示 | 14.3 | 5 个非导出 config 结构体置于导出方法之后（违反规范 2） |
| T-05 | 提示 | 14.3 | ParseProgressStage 错误消息未列合法值清单（其余 3 个 Parse 均列出） |
| T-06 | 提示 | 11.11 | env 非法值：Python fail-fast 崩启动 vs Go 静默回退无日志 |
| T-07 | 提示 | 11.11 | normalizeActiveHours 对 bool 的语义分叉（Python True→1 分钟） |
| T-08 | 提示 | 11.11 | get_path 用 filepath.Rel 总成功 vs Python relative_to 越界 raise |
| T-09 | 提示 | 13.17 | repair 失败日志 content 截断追加了 "..." 后缀（Python 纯截断） |
| T-10 | 提示 | 13.17 | WithTripleMaxConcurrent 负数 make chan panic vs Python ValueError |
| T-11 | 提示 | 11.12 | web connection.ack 未接出站管线（装配缺口，spec 声称与实现不符） |
| T-12 | 提示 | 11.12 | resolveTimestampMs 超长数字串 Atoi 溢出回退未声明容差 |
| T-13 | 提示 | 11.12 | 入站步骤10 sender 三键读取引用而非 Python 入口快照 |
| T-14 | 提示 | 13.20 | 工厂经路下显式 nil vector_field 不可达 155109（options 翻译边界） |

---

## 二、13.11 AutoParser 三件套（未审章节，全面审查）

已核对通过：三件套全部方法签名与步骤、切片参数二分（nil→默认/`[]`→恒空）、docMatcher 双形态路由、RegisterNewParser last-wins、四键 metadata 增强、URL/文件 fall-through、LLMClient 三层传递（透传/剔除/透传）、exists 语义（#11 修复后全仓无 IsNotExist 残留同类）、155103/155104 错误码与消息、七组扩展名注册清单、5 条日志对位、doc.go 三条目。

### [严重] S-01 NewAutoLinkParser 包外无法注入自定义路由，且存在零值路由 panic 路径

**Python 样例**（`auto_link_parser.py:45-51`）——routes 为公开构造参数，可传任意 `[(pattern_or_callable, parser), ...]`：
```python
def __init__(self, routes: List[Tuple[Union[re.Pattern, Callable[[str], bool]], Parser]] | None = None, **kwargs):
    self.routes = routes if routes is not None else _default_routes()
```

**Go 问题**（`auto_link_parser.go:43-48/87/173-175`）：
- `NewAutoLinkParser(routes []linkRoute)` 已导出，但 `linkRoute` 的字段 `matcher`/`parser` 与接口 `docMatcher` 均非导出——**包外无法编写 `linkRoute{...}` 复合字面量（非导出字段编译报错），只能传 nil/空切片**。spec 决策 2 声称"导出 NewRegexpMatcher/NewFuncMatcher 作为外部注入自定义路由入口"——外部能拿到 matcher 却造不出 route，注入链路断裂。
- 包外可写 `[]linkRoute{{}}`（零值字面量合法），Parse 遍历到 `matchDoc(nil, doc)` 即 nil 接口调用 **panic**；Python 传垃圾路由抛 AttributeError 可被上层 except 捕获，Go panic 不可恢复——与决策 6（nil Metadata 防御）同一立论。
- 现有 37 个用例全为同包测试，掩盖了此断裂。

**修复方案**：
1. 新增导出构造 `func NewLinkRoute(m docMatcher, p Parser) linkRoute`（或字段导出 `Matcher/Parser` + `docMatcher` 导出为 `DocMatcher`）打通注入链路；
2. `matchDoc` 加 nil 防御：`if m == nil { return false }`（对齐决策 6 防御优先，nil 路由视作不命中并 Warn）；
3. 补包外注入测试用例。

### [一般] M-01 regexpMatcher 用 MatchString（search 语义）对齐 Python re.match 应为头部锚定

**Python 样例**（`auto_link_parser.py:36`）：`pattern_or_callable.match(doc.strip())`——`re.Pattern.match` 要求**从串首匹配**。

**Go 问题**（`auto_link_parser.go:175-177`）：`m.re.MatchString(strings.TrimSpace(doc))`——Go `MatchString` 等价 Python `re.search`（任意位置）。

**流程示例**（注入未锚定正则 `weixin\.qq\.com`，doc=`https://mp.weixin.qq.com/s/x`）：
```
Python: match 从位置 0 匹配，"https" 开头不中 → False → 路由不命中
Go:     MatchString 全文搜索命中（位置 11）→ True → 委托微信解析器
```
默认两条路由（WECHAT_MP_URL_PATTERN/HTTP_URL_PATTERN）均带 `^` 锚定，**默认链路零差异**；仅 `NewRegexpMatcher` 自定义注入未锚定正则时行为反转。

**修复方案**：`loc := m.re.FindStringIndex(strings.TrimSpace(doc)); return loc != nil && loc[0] == 0`。web_page_parser.go:202/391 同款 MatchString（正则自带 `^` 实际等价）可一并修正。

### [一般] M-02 filepath.Ext 对前导点文件名的语义与 os.path.splitext 不同

**Python 样例**（`auto_file_parser.py:99/147`）：`os.path.splitext(doc)[-1].lower()`——CPython 规则：basename 最后一个点之前若全为点（或无字符）则**无扩展名**。

**Go 问题**（`auto_file_parser.go:81/146`）：`strings.ToLower(filepath.Ext(doc))`——最后一个点起的尾段照收。

**实测对照**：`.txt`→Python `""`/Go `".txt"`；`a/.json`→`""`/`".json"`；`..md`→`""`/`".md"`。隐藏文件名恰为注册扩展名时，Python 走 155104 / supports=False，Go 命中 registry 实际解析成功——分支反转，与偏差 #11（ENOTDIR）同为"标准库语义窄化"。

**修复方案**：包内加 `splitExtLower` 辅助复刻 CPython 尾段规则（basename 最后点且点前存在非点字符才算扩展名），替换两处；`excel_parser.go:78/163`（13.13）同款建议同步。

### [提示] T-01 factory() 第三方返回 nil 时 Parse 将 panic

`auto_file_parser.go:95-103`：第三方工厂返回 nil 后 `parser.Parse(...)` 为 nil 接口调用直接 panic；Python `None.parse` 抛 AttributeError 可捕获。包内七个 init 工厂不可失败，仅第三方路径暴露。一行 `if parser == nil` 守卫（或归档为已知边界）。

---

## 三、14.2 枚举层 + 14.3 progress 复核

**14.2 结论：零问题。** 四枚举 25 常量值与 Python 逐字符一致；Python 无 from_string/get_all 方法（仅 str-Enum 构造语义，非法值抛 ValueError），Go Parse×4 大小写敏感+非法返回 error 行为等价；All×4 声明序对齐迭代序；`.value` 消费点全部由 `string()` 转换覆盖。

**14.3 复核通过项**：时区修复 8 处 `.UTC()` 全部到位（ToDict 输出 `Z` 偏移）；start_stage 自动收尾（仅前序非 INITIALIZING）、计时删键、update/warn 不建新 step、fail 双写 error、complete_stage/fail_stage 不通知回调（隐蔽特性忠实保留）、双映射表 15 键值、11 个 With 构造器默认值、reporter 无过期清理（Python 亦无）均核对一致。

### [一般] M-03 StartStage 细节字典克隆，偏离 Python 引用共享语义且未归档

**Python 样例**（`progress.py:213-218`）：
```python
step = ProgressStep(stage=stage, status=ProgressStatus.RUNNING, message=message, details=details or {})
```
非空 dict 时 step.details **与调用方共享同一对象**，后续 `steps[-1].details.update()`（L248/L308/L340）写穿到调用方 dict；空/None 才换新对象。

**Go 问题**（`progress.go:224 + 754-765`）：`WithStepDetails(cloneStageDetails(cfg.details))`——非空 map 深位克隆，调用方别名断开；且 L754 注释自称"对齐 Python 'details or {}'"与实际行为不符；spec §4 六条偏差均未登记此克隆（§4-5 只归档了 mergeDetails 逐键合并）。与 WithStepDetails（L479-483 整体替换共享引用）内部不一致。

**修复方案**（二选一）：①严格对齐去掉 clone（并发下调用方再改 map 有 race 风险，不推荐）；②保留 clone 作为语言驱动防御，修正 L754 注释 + spec §4 补录第 7 条偏差（推荐）。

### [一般] M-04 Python truthy 判断 vs Go nil 判断，空串行为全系偏差（4 处）

**Python 样例**（`progress.py:244/301/336/342`）：
```python
if message:                       # 空串与 None 同为 falsy，跳过赋值
    self.progress.current_message = message
```

**Go 问题**（`progress.go:248/299/659/667`）：`if cfg.message != nil`——空串**会赋值**。`WithCompleteStageMessage("")` 将 step.Message 与 CurrentMessage 置空；`FailStage("")` 将 step.Error 置 ""；Python 中 `complete_stage(message="")` 后 step.message 保持不变、`fail_stage(error="")` 后 step.error 保持 None。

**修复方案**：四处判断补 `&& *x != ""`（endCurrentStageLocked L667 errMsg 同理；FailStage L285 的 progress.Error 无条件赋值保持，对齐 Python L284）。

### 提示级（4 项）

- **T-02**：`progress.go:695` 回调失败日志固定串 `"progress_callback"`，Python `str(callback)`（L173）可辨识回调——建议 `reflect.TypeOf(cb).String()`。
- **T-03**：StartStage/Complete 的日志与 notify 顺序颠倒（Python L221 logger→L228 notify；Go L229 notify→L232 logger，日志移出锁外的副作用）——建议注释注明或接受现状。
- **T-04**：`progress.go:362-396` 五个非导出 config 结构体在导出方法区块之后，违反规范 2——上移至 ProgressManager 定义之后。
- **T-05**：`enums.go:140` ParseProgressStage 错误消息 `"valid: 15 values"` 未列清单，L113/L124/L152 均列出具体值——补全或统一。

---

## 四、11.11 心跳服务复核

复核通过项：isActiveNow 用 `time.Unix(nowFn,0).Local()` 对齐 Python `time.localtime()`（**无 14.3 类时区问题**）；tick 六步、停机取消豁免、timeout 归类、Start/Stop CAS 互斥、config round-trip、清缓存跳过语义均核对无误。已归档 9+2 项偏差不重复。

### [严重] S-05 config `target: ""` 时 Python 禁用 relay、Go 反向回退 "web" 启用 relay

**Python 样例**（`app_gateway.py:898-900`）：
```python
heartbeat_relay_channel = os.getenv("HEARTBEAT_RELAY_CHANNEL_ID") or (
    str(cfg_target) if cfg_target is not None else "web"
)
```
config.yaml 写 `heartbeat: {target: ""}` 时：`cfg_target=""` → `str("")=""` → `relay_channel_id=""` → tick 中 falsy **不回传**（heartbeat.py:229）——用户显式置空即禁用 relay。

**Go 问题**（`app_gateway.go:214-217`）：
```go
} else if cfgTarget != nil {
    if s := fmt.Sprint(cfgTarget); s != "" {   // 空串被拦下
        relayChannel = s
    }
}
```
空串被 `s != ""` 拦截，relayChannel 保持默认 `"web"`——**显式禁用 relay 的合法配置被反转为向 web 通道回传心跳消息**，用户会看到不该出现的心跳消息。

**修复方案**：去掉 `s != ""` 判断，`cfgTarget != nil` 即赋值（含空串），对齐 Python or-chain 取值。

### [严重] S-06 active_hours 空对象 `{}` 清除语义失效（Go 静默 no-op）

**Python 样例**（`app_web_handlers.py:1545-1550` + `heartbeat.py:327-330`）：
```python
if active_hours is not None:
    if not isinstance(active_hours, dict):
        active_hours = None
    elif active_hours and ("start" not in active_hours or "end" not in active_hours):
        active_hours = None
await hb.set_heartbeat_conf(every=every, target=target, active_hours=active_hours)
```
`{}` 为 falsy，**elif 不命中**，空 dict 原样穿透 → `set_heartbeat_conf` 中 `is not None` 成立 → 更新为 `{}`、重启、写回 `active_hours: {}`——**清除生效（始终活跃）**。注意 Python 的微妙三分：`{}` → 清除；`{"foo":1}`（truthy 缺 start/end）→ 置 None → 不更新保持旧值。

**Go 问题**（`web_handlers.go:2116-2128` + `heartbeat.go:293-298`）：
```go
if ahMap, isMap := v.(map[string]any); isMap {
    if _, hasStart := ahMap["start"]; hasStart { ... }  // 空 map 无 start → 全部穿透
}
// 非 map 或缺 start/end → nil（清除语义，前端传 {} 走此分支）← 注释如此声称
```
空 map 缺 start → activeHours 保持 nil → `SetHeartbeatConf` 中 `activeHours != nil` 不成立 → `updated=false` 早退：**旧时段保留、不重启、不 reset——注释声称的"清除语义"实际是 no-op**。

**流程对比**：
```
前端传 {}（用户点"清除时段"）：
Python: {} 穿透 → set_heartbeat_conf(active_hours={}) → 更新+重启+写回 {} → 始终活跃 ✓
Go:     {} → nil → updated=false 早退 → 旧时段保留，配置无任何变化 ✗
```

**修复方案**：`web_handlers.go` 区分三分支——`isMap && len(ahMap)==0` 时传**非 nil 空 map**（触发 SetHeartbeatConf 更新为空，isActiveNow 对空配置返回始终活跃）；非空但缺 start/end 保持 nil（对齐 Python 置 None 不更新）。

### [一般] M-05 runLoop 因父 ctx 取消退出后 running 未复位（僵尸态）

**Python 样例**（`heartbeat.py:139-149`）：任务取消唯一路径是 `stop()`，且先置 `_running=False`，不存在"循环死了但 is_running()=true"。

**Go 问题**（`heartbeat.go:344-360`）：`case <-ctx.Done(): return` 直接退出，不复位 `running`；若父 ctx（AppGatewayServer.Start 传入）被外部取消而未走 Stop()，心跳静默死亡但 `IsRunning()=true`、再次 `Start()` 因 CAS 基于 stale true 而拒绝重启（假幂等）。

**修复方案**：runLoop 的 ctx.Done 分支（或 defer）加 `s.running.Store(false)`（Stop 已置 false，重复 Store 无害）。

### [一般] M-06 session.list 缺排序 + 负 offset 未 clamp 可 panic（顺带发现，非 11.11 引入）

**Python 样例**（`app_web_handlers.py:1243` + `session_metadata.py:437-440`）：
```python
offset = max(0, offset)                                        # 负值 clamp
sessions.sort(key=lambda x: x.get("last_message_at", 0), reverse=True)  # 按最后消息时间倒序
return sessions[offset: offset + limit], total
```

**Go 问题**（`web_handlers.go:1077-1114`）：无 last_message_at 排序（ReadDir 词法序，正常调用每次都偏离 Python 展示序）；offset 只 clamp 上界（L1111），负值直通 `sessions[offset:]`（L1114）**切片 panic**。

**修复方案**：`if offset < 0 { offset = 0 }`；读 metadata 后按 last_message_at 降序排序再分页。

### 提示级（3 项）

- **T-06**：env 非法值（如 `HEARTBEAT_INTERVAL=abc`）Python `float()` ValueError 直接炸装配；Go ParseFloat 失败静默保持默认且**无日志**（`app_gateway.go:189-209`）——建议至少 Warn 一条。
- **T-07**：`normalizeActiveHours` 对 bool：Python `isinstance(v, (int,float))` 中 True 是 int 子类 → 1 → "00:01"；Go switch 无 bool 分支 → "true" → parseHM 失败回退始终活跃（`heartbeat.go:156-181`）——建议补 `case bool`。
- **T-08**：`get_path` 用 `filepath.Rel` 总成功可返回 `../..` 形态；Python `relative_to` 越界 raise → INTERNAL_ERROR（`web_handlers.go:2155` vs `app_web_handlers.py:1573`）——实际路径固定派生不触发，防御性差异。

---

## 五、13.17 TripleExtractor（未审章节，全面审查）

已核对通过：接口双方法、构造器默认值（0.0/50）、buildPrompt 模板 3480 字节逐字节 diff 一致+渲染等价（含 Untitled 回落、花括号注入安全双向验证）、parseTriples 七例语义、gather 全跑完+按 chunk 序首错、155507 三路径、Triple 4 字段无 omitempty、4 条日志对位、23+8 测试用例。

### [严重] S-02 pyStr 超 int64 整数字面被 e 记法化（短路路径，移植代码自身缺陷）

**Python 样例**：
```python
>>> json.loads('[["a","b",12345678901234567890]]')  # int 无界
12345678901234567890 → str() → "12345678901234567890"   # 原样输出
```

**Go 问题**（`triple_extractor.go:408-417`）：
```go
case json.Number:
    if i, err := strconv.Atoi(val.String()); err == nil { return val.String() }
    if f, err := val.Float64(); err == nil { return pyFloatStr(f) }  // 大整数漏到此处
```
`json.Number("12345678901234567890")`（超 int64 界 9.22e18）Atoi 失败 → Float64 成功 → pyFloatStr → **`"1.2345678901234568e+29"`** ≠ Python 原样输出。且测试 `triple_extractor_test.go:368` 注释宣称"int 超界仍整数字面"但用例 `10000000000000000`（1e16）未超 int64 界——**注释宣称与实际能力不符，测试未覆盖真实超界**。

**修复方案**：Atoi 失败后先判纯整数字面（可选符号+全数字）则原样返回字面量，仅小数/指数记法走 Float64；测试补 `json.Number("12345678901234567890")` 用例。

### [严重] S-03 深修复路径数字经 float32 解析精度截断（json-repair v0.0.18 库级传导）

**Python 样例**（json_repair 0.64.2 双精度）：
```python
>>> repair_json("{'t': [1e-4]}", return_objects=True)
['0.0001']        # str() 输出与字面一致
```

**Go 问题**：库源码 `/root/go/pkg/mod/github.com/!real!alexandre!a!i/json-repair@v0.0.18/jsonrepair.go:1081`：
```go
r, _ := strconv.ParseFloat(string(rst), 32)   // ← float32 精度
```
深修复路径（单引号/尾逗号等非法 JSON——**LLM 输出不合规 JSON 时正是设计内高频路径**）数字以 float32 精度解析后转 float64，pyFloatStr 忠实还原的输入已失真：

| 输入（需深修复） | Python str() | Go 实际 |
|---|---|---|
| `1e-4` | `"0.0001"` | `"9.9999997e-05"` |
| `1e15` | `"1000000000000000.0"` | `"999999986991104.0"` |
| `1e16` | `"1e16"` | `"1.0000000272564224e+16"` |

数值误差经心算复核与 float32 尾数 24 位精度吻合（1e15 相对误差 ~1.3e-8 → 绝对 ~1.3e7）。

**修复方案**：①上游修复（`ParseFloat(…, 64)`）——登记 issue/fork；②短期在 spec §8 归档此库级偏差，如需立即消除可在包装层对深修复结果改用 `RepairJSON` 取修复后字符串自行 `UseNumber` 解码。合法 JSON 短路路径（`json.Valid` 预检）现状已无损。

### [严重] S-04 深修复路径超 int64 大整数截断为 MaxInt64（库级，与 S-03 同根因）

**Python 样例**：`repair_json("{'t': [12345678901234567890]}")` → int 12345678901234567890。

**Go 问题**：库 `jsonrepair.go:1088`：`r, _ := strconv.Atoi(string(rst))` 忽略 ErrRange → 深修复返回 `9223372036854775807` → 输出 `"9223372036854775807"` ≠ Python `"12345678901234567890"`。与 S-03 合并登记上游 issue + spec 归档。

### [一般] M-01（13.17）metadata title 非字符串值行为分叉

**Python 样例**（`triple_extractor.py:87→263`）：`chunk.metadata.get("title", "")` 得 42 → `title or "Untitled"` 42 truthy → 模板渲染 `"Title:\n42\n"`。

**Go 问题**（`triple_extractor.go:454-461`）：metadataTitle 类型断言失败返回 `""` → 回落 `"Untitled"`。metadata={"title": 42} 时 Python 渲染 "42"、Go 渲染 "Untitled"。

**修复方案**：非字符串 title 用 `fmt.Sprint(v)` 字符串化（对齐 Python truthy 直传的 str 化效果），或 spec 归档为有意防御。

### 提示级（2 项）

- **T-09**：repair 失败日志 `truncateContent` 追加 `"..."`（203 字符）；Python `content[:200]` 纯截断（`triple_extractor.py:289` vs Go L464-469）——对齐去掉后缀或归档。
- **T-10**：`WithTripleMaxConcurrent(-1)` → `make(chan struct{}, -1)` panic；Python `asyncio.Semaphore(-1)` 构造期 ValueError（`triple_extractor.go:184`）——建议 n≤0 改 error 或钳制，或归档。

---

## 六、11.12 IM Pipeline 复核

复核通过项：十三步/十四步顺序、七层分支（reason 字面值）、三模板（SYSTEM_PROMPT/peekPending/classifyPrompt）机器比对逐字一致；RE2 追问正则 8 组边界与 Python re 等价；PendingInteraction 消费链一致；出站单点接入成立。已修 8 项（R1-R8）抽查质量良好，唯 R1 漏 reply_scope（M-08）、R3 漏 dict fallthrough（M-07）。

### [一般] M-07 extractContent dict 分支无匹配键时提前返回空，Python 落 str(dict) 继续管线（R3 修复遗漏延伸）

**Python 样例**（`im_outbound.py:366-371`）：
```python
if isinstance(content, dict):
    for key in ("output", "text", "message"):
        val = content.get(key)
        if isinstance(val, str) and val.strip():
            return val.strip()
return str(content or "").strip()    # dict 无匹配键不提前退出，落到此行
```
非空 dict（truthy）→ 产生 `"{'a': None}"` repr 串 → **继续走 pending 清除/前缀解析/路由决策全链**。

**Go 问题**（`im_outbound.go:290-296`）：`case map[string]any:` 循环后 `return ""` 提前退出 → 步骤5短路 `return nil`，**整条后续链不执行**（消息静默不发）。R3 修复只改了 default 分支，未对齐 dict 分支的 fallthrough。

**修复方案**：dict 分支循环后删去 `return ""`，落入 default 分支 `strings.TrimSpace(pythonStr(content))`。

### [一般] M-08 出站步骤8 reply_scope 用 `.(string)` 断言，非字符串 truthy 值行为反转（R1 遗漏点位）

**Python 样例**（`im_outbound.py:178`）：
```python
if str(meta.get("reply_scope") or "").strip():
    return                                    # True/123 → str 化 "True"/"123" 非空 → 短路返回
```

**Go 问题**（`im_outbound.go:192-194`）：
```go
replyScope, _ := meta["reply_scope"].(string)   // 非字符串 → "" → 继续走 candidate 提取 + LLM 分类
```
reply_scope 为 bool True/数字时 Python 短路不路由，Go **继续执行可能升级 DM**。R1 声明"IM 元数据取值 15 处全部替换 stripOrStr"，此点位漏改。

**修复方案**：改 `replyScope := stripOrStr(meta["reply_scope"]); if strings.TrimSpace(replyScope) != "" { return nil }`。（同文件 event_type/chat_type 两处同模式但消费语义保证等价，无需改。）

### [一般] M-09 model_client_config/react 非 dict 时方向反转：Python 异常回退（LLM 不激活）vs Go 静默 env 回退（可能激活）

**Python 样例**（`im_inbound.py:162` + `im_outbound.py:81-87`）：
```python
react = cfg.get("react") or {}                          # react 为 list/str 时保留原值
mcc = react.get("model_client_config") or {}            # 后续 mcc.get("api_key") → AttributeError
# → _ensure_llm except → llm=None（入站改写恒回退 / 出站 apply 抛出→publish 降级 "queued without routing"）
```

**Go 问题**（`im_inbound.go:466-467/788-791` + `im_outbound.go:339-343`）：
```go
react, _ := cfgBase["react"].(map[string]any)      // 断言失败 → nil
mcc, _ := react["model_client_config"].(map[string]any)
if mcc == nil { mcc = map[string]any{} }           // 空配置 → api_key 走 env 回退链
```
config 写错类型时，Python 异常路径**LLM 不激活**；Go 静默空 map 走 env 回退，**若环境变量存在则 LLM 构造成功激活**——方向相反。

**修复方案**：两处 ensureLLM 前置类型守卫——key 存在但非 map 时记 Warn 并按 Python 异常路径处理（入站 return nil；出站 return false 不激活）。

### 提示级（3 项）

- **T-11**：web connection.ack Python 走 `publish_robot_messages` 过出站管线（`app_web_handlers.py:594`）；Go `WebHandlersBindParams` 未注入 MessageHandler（`app_gateway.go:274-283`）→ `ch.Send` 直发（`web_handlers.go:526`）。行为终效等价（ack 非数字分身必短路），属 11.9 装配既有缺口，建议补注入或修正 spec 声称。
- **T-12**：`resolveTimestampMs` 超长数字串 Atoi 溢出回退 `timestamp*1000`（`im_inbound.go:628-629`）；Python `int()` 任意精度照常采用（`im_inbound.py:275-276`）——建议偏差表补一行声明容差。
- **T-13**：入站步骤10 sender 三键读取 `msg.Metadata` 引用（步骤7 patch 合并后值）；Python 读 L478 入口快照（`im_inbound.go:287+378` vs `im_inbound.py:478+554-558`）。现有适配器 patch 不含三键不触发，建议复制快照或加注释声明。

---

## 七、13.19+13.20 Retriever（未审章节，全面审查）

已核对通过：四方法接口签名、RetrievalMode 三成员、kwargs→Extra 收而不用、155200/155202 文案逐字符、Search/SparseSearch 不传 filters（字面复刻）、chunk_id 双来源（决策 7）、rsr 不对称（§8-8）、pgvector 155402（决策 6）、Python 四文件零 logger→Go 零日志、49 测试例。

### [一般] M-10 工厂 default 分支错误文案多一个空格，与 Python 逐字符不一致

**Python 样例**（`vector_store/store.py:38-39`，相邻 f-string 隐式拼接，第一段以逗号结尾、第二段以 "and" 开头，**中间无空格**）：
```python
error_msg=f"unavailable vector store provider: {config.store_provider},"
f"and available providers are: {', '.join([m.value for m in StoreType])}",
```
实测渲染：`unavailable vector store provider: xxx,and available providers are: milvus, chroma, pgvector`（**`,and` 无空格**）。

**Go 问题**（`vector_store/store.go:181-183`）：
```go
"unavailable vector store provider: %s, and available providers are: milvus, chroma, pgvector"
```
`%s,` 后多了空格。且 L177-178 注释自称"逗号+空格拼接"、设计文档 §5.6/§6 错误转录为含空格并断言"逐字符对齐"、`store_test.go:98` 用错误文本钉死断言——错误源头是设计文档转录笔误，实现与测试照抄。

**修复方案**：格式串删空格（`%s,and`），同步修正注释、spec、测试断言。

### [一般] M-11 BatchRetrieve 首错返回时机不等价：Python gather 立即传播，Go errgroup.Wait 阻塞至全部结束

**Python 样例**（`vector_retriever.py:114-120`，sparse 同构）：
```python
tasks = [self.retrieve(query, top_k=top_k, **kwargs) for query in queries]
results = await asyncio.gather(*tasks)     # 裸 gather：首异常立即向 awaiter 传播
```

**Go 问题**（`vector_retriever.go:107-125`，sparse 同构）：`errgroup.Wait()` 阻塞至所有 goroutine 返回后才返回首错。错误负载一致（时间上首个错误），但**错误返回延迟不同**；`base.go:23-24` 注释"首错即返回"表述不准确（实际是"等全完→返首错"）。

**流程对比**：
```
Python: query[0] err(1ms) ──→ gather 立即抛出 ──→ 调用方 1ms 收到异常（query[1] 孤儿跑完丢弃）
Go:     query[0] err(1ms) ──→ errgroup 记录首错
        query[1] ..............30s..............→ Wait() 返回 ──→ 调用方 30s 后收到同一异常
```

**修复方案**（二选一）：①保真复刻——`sync.WaitGroup` + 带缓冲 `errs chan error` + `select` 首错即返（注意 done 关闭与末 goroutine 出错的竞态需非阻塞 drain 兜底）；②维持 errgroup，修正 base.go 与两处注释及设计文档表述，明确"阻塞等全完、返时间首个错误"。

### [提示] T-14 工厂经路下 Python 155109（vector_field 显式 None 拒绝）不可达

Python `milvus_store.py:82-90` 显式传 None → 干净 155109；Go options 模式下"未设置"与"显式 nil"不可区分（`store.go:134-141/152-160`），一律兜底 `NewMilvusAUTO("embedding")`；另有 typed-nil 陷阱（`WithStoreMilvusVectorField((*MilvusAUTO)(nil))` 接口非 nil 直传构造器解引用 panic）。属 kwargs→options 翻译的结构性边界——可加反射双 nil 判定返回 155109，或在 StoreCreateOptions 注释标注翻译边界归档。

---

## 八、统计与处置建议

| 章节 | S | M | T | 小计 |
|------|---|---|---|------|
| 13.11 AutoParser | 1 | 2 | 1 | 4 |
| 14.2 枚举 | 0 | 0 | 0 | 0 |
| 14.3 progress 复核 | 0 | 2 | 4 | 6 |
| 11.11 心跳复核 | 2 | 2 | 3 | 7 |
| 13.17 TripleExtractor | 3 | 1 | 2 | 6 |
| 11.12 IM Pipeline 复核 | 0 | 3 | 3 | 6 |
| 13.19+13.20 Retriever | 0 | 2 | 1 | 3 |
| **合计** | **6** | **12** | **14** | **32** |

（注：11.11 M-06 为顺带发现非本轮引入；表格按问题归属章节计 12 M，含 M-06。）

**优先处置建议**：
1. **S-05/S-06（心跳）**：正常配置写法即可触发的行为反转/功能失效，修复成本低（两处判断），建议即修；
2. **S-02（pyStr）**：移植代码自身唯一可控严重缺陷，修复+补超界测试用例；
3. **S-01（linkRoute）**：导出 API 契约断裂，补导出构造+nil 防御+包外测试；
4. **S-03/S-04（json-repair）**：根因在第三方库——登记上游 issue（`ParseFloat(…,32)`→64、`Atoi` 忽略 ErrRange），spec §8 归档库级偏差，视上游响应决定是否包装层绕过；
5. **M-10（文案空格）**：逐字对齐是项目硬约束，且测试/文档三处连锁错误需一并修正；
6. 其余 M/T 按批次处置，T 级多数可归档（尤其标明"声明容差"类）。

**方法论备注**（本轮系统性风险信号）：
- "设计文档转录笔误 → 实现与测试照抄"（M-10）：spec 中声称"逐字符对齐"的文案，实现时未与 Python 原文逐字符 diff；
- "修复本身有遗漏点位"（M-07/M-08）：R1/R3 声称"15 处全部替换""default 分支对齐"，实际各漏一处——批量修复后应按声明范围机器复核；
- "注释宣称与实际能力不符"（S-02 测试注释）：测试断言写"int 超界仍整数字面"但用例未真正超界，需边界值实际计算验证（1e16 < int64 界 9.22e18）。

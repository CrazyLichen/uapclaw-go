# jiuwenswarm — 完整提交清单

- 仓库：https://gitcode.com/openJiuwen/jiuwenswarm
- 基线提交：`1ea114cf1446a8ef32eac98ae51a41db68cdc789`
- 目标提交：`a2abceccfba89ab25a518568916152865fa28acd`
- 提交总数：**2760**（不含合并提交）

## 类别统计

| 类别 | 提交数 | 占比 |
| --- | --- | --- |
| 新功能 | 453 | 16.4% |
| 缺陷修复 | 1904 | 69.0% |
| 优化改进 | 32 | 1.2% |
| 重构 | 101 | 3.7% |
| 文档 | 90 | 3.3% |
| 测试 | 38 | 1.4% |
| 工程/依赖/构建 | 33 | 1.2% |
| 安全加固 | 1 | 0.0% |
| 回滚 | 8 | 0.3% |
| 其他 | 100 | 3.6% |

> 提交按类别分组、组内按时间正序排列。`commit` 列为完整 40 位 SHA，可 `git show <sha>` 查看详情。

## 新功能（453 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `6f5441946d34b496711332aa19a03a76736da34f` | 2026-06-01 | xiaoyao42 | feat：去掉活动面板的loading，它和发送按钮的状态是一致的；去掉历史加载蒙版 |
| 2 | `780d867d15d7eff5bee6dbbe7f6d6a1a3703b849` | 2026-06-01 | zhengkailan | feat(websocket): enhance shutdown member handling and cleanup logic |
| 3 | `a09f3085cc83398a200e9ea0f90738761e216cf8` | 2026-06-01 | openJiuwen-bot | feature:/config支持交互面板 |
| 4 | `2102dff2f8ff2bb2ee3c68fa819ebe0c9543e0e6` | 2026-06-02 | huanggawa | feat: update v0.2.1 |
| 5 | `dd0964bb599de921080f7abf8c1b279aab1f48ff` | 2026-06-02 | huanggawa | feat: release v0.2.1 |
| 6 | `46ce28fc720116724fe4fe291bbae48f4a0b3bf0` | 2026-06-03 | ICEORY | feat(rails): Add detection and active interruption for cyclic calls of four types of tools to avoid infinite loops caused by repeated tool calls |
| 7 | `c172ca182ebf2e19b63133c3661e73c4862244f5` | 2026-06-03 | openJiuwen-bot | feat(tui): /config reset supports parameter-free invocation |
| 8 | `441c2186c5f50a9bc9164a02c17609d00783a37f` | 2026-06-05 | jack_ricky | feat: improve modular A2UI integration |
| 9 | `a63f06017a59e25f9d2b820f65751b046cb9edfc` | 2026-06-05 | ldstyle8 | feat(diff): display git and file_ops info both |
| 10 | `b8cb3ca5dab0ad24e9883f610b4b8d13b9f901f2` | 2026-06-05 | ldstyle8 | feat: rails interrupt helpers, stream event rail improvements, TUI command builtins refactor, agent adapter interface simplification |
| 11 | `22bce9377eb175de249972959037bf0f78f05f17` | 2026-06-06 | alan_cheng | feat(swarmflow): 支持swarmflow能力 |
| 12 | `298332a538bc24f75ca83433a1c96655202ef299` | 2026-06-07 | chenchunzhou | feat(swarmflow): Added output for the workflow.update event and handling for the command.workflows command |
| 13 | `0645b62df61dbce8f62cee9af82f26b79b634656` | 2026-06-08 | ldstyle8 | feat(plan_mode): refactor the plan mode process |
| 14 | `bb9cb9affc41b703f6ce12eb3811cf923b49f160` | 2026-06-08 | min_gitcode | feat(swarm): add enable_swarmflow config option with default true |
| 15 | `def8093e36dd681d7147ef498234a5ec0cec9ce6` | 2026-06-08 | min_gitcode | feat(swarm): enable skill discovery for chat-team members |
| 16 | `f7867b1b4214d77c96fd855155d316c9ec10fb5e` | 2026-06-08 | min_gitcode | feat(swarm): add skill storage policy and shared link refresh rails for team members |
| 17 | `1e63db1eadf95422485331214badf0905053d3fb` | 2026-06-09 | huanggawa | feat: support using tui when using dmg |
| 18 | `3a4d95f5139b326c11a16f16f6eeefb1a5ad1a08` | 2026-06-09 | cycong_1 | feat: inject DeepSeek V4 thinking params from reasoning_level at runtime |
| 19 | `634098fc19a4011fd55885575bffa9e08c172705` | 2026-06-09 | ldstyle8 | feat(recap): bilingual recap output + auto-recap config toggle |
| 20 | `6e39ff605dc05b3c6f44161362f4851a88ac8e74` | 2026-06-09 | lin-xiaoyu | feat(symphony-orchestration): 增加交响乐编排核心组件 |
| 21 | `a89c994c1d9782ca4e1f0686bd3a57979997f7e3` | 2026-06-09 | xutuo | feat (symphony-retrieval): 引入本地skill的Agentic检索能力 |
| 22 | `cac1c7eec82077e613738da1a782043a94a470d1` | 2026-06-09 | lin-xiaoyu | feat(symphony-orchestration): Added integration of Symphony orchestration into Jiuwenswarm |
| 23 | `d31df0288fde243d5978d01ab3bfd16dd030bbf9` | 2026-06-09 | openJiuwen-bot | feat(acp): harden E2A envelope path for headless clients |
| 24 | `e5e95a9801b7d5ee8b6f92b342df021b3084896a` | 2026-06-09 | chensheng12345 | feat(TUI): tui  log and fold |
| 25 | `f01df480131d7ed393b19af6835afcc670ec53fa` | 2026-06-09 | chenchunzhou | feat(swarmflow): pipeline stream event telemetry to frontend and refactor phase types |
| 26 | `8b1b21db9d3817b0853aca5cc537d663a69bc138` | 2026-06-11 | xiaoyao42 | feat(web-frontend): enhance markdown, persistent history, and member panel layout |
| 27 | `bfb996eb431781104d359989248229adf22d0359` | 2026-06-11 | openJiuwen-bot | feat(symphony): visualize agentic search skill-tree path in chat |
| 28 | `dea8f4b4b3e4018859f46e688eb23adcdd7c8e54` | 2026-06-11 | openJiuwen-bot | feat(teams): distributed teams support dismiss temporary team |
| 29 | `3026ece6794a96207fc8225af24962e4396056ec` | 2026-06-12 | layShang264 | feat:支持 Symphony 总谱增量构建、指纹/关系缓存、断点续构建 |
| 30 | `502dee76e68e1d283b90166adfbad42381a6760b` | 2026-06-12 | chunbs_cbs | feat:skill能力树支持增量构建 |
| 31 | `b592e6e82bb5d4d9f1f3f16690fe7de9bdcf8ea6` | 2026-06-12 | openJiuwen-bot | /skills支持skillnet和clawhub源 |
| 32 | `c817e1e7813ff1c1d2d17b447d81def7d7137b55` | 2026-06-12 | zhengkailan | feat(TeamEventGroupDisplay): add member status handling and filter active team members |
| 33 | `c839a028953dfa767edfa766e028712060f3c44e` | 2026-06-12 | zhengkailan | feat(TUI): enhance tool rendering with firstNonEmptyLines utility and formatLeafValue improvements |
| 34 | `5024bfcdede6f6561cce482c7965846688d36972` | 2026-06-13 | openJiuwen-bot | feat(auto-harness): Tui support GitCode issue fix workflow |
| 35 | `5cf6c560f4fc97cafaab80b7103e1182acc72212` | 2026-06-13 | layShang264 | feat(Symphony): Added incremental and full builds, plus a pause button |
| 36 | `a6a6333c7161a9d0007e388b633b566c72632d8c` | 2026-06-13 | chensheng12345 | feat: Preserve swarmflow agent selection |
| 37 | `b8d1272e7ed275c407fc9b79536022b83febb99a` | 2026-06-13 | layShang264 | feat(skill): Skills list automatically adjusts to the view after refreshing |
| 38 | `6a61e4b2e55f8f37d52375be392b7797bc8501a4` | 2026-06-15 | lin-xiaoyu | feat(symphony): 增加先检索，后编排，分别调用工具的功能 |
| 39 | `b815ca0b51e21733f220a3b9d6483445a66fda9f` | 2026-06-15 | lin-xiaoyu | feat(symphony): 优化工具返回结果和执行图显示，增加swarm模式支持 |
| 40 | `bc760050473604c4d823a2cbb588fda236a6b57b` | 2026-06-15 | lin-xiaoyu | feat(symphony)：删除symphony tui命令 |
| 41 | `e803f062e3e9d1f14cfa7345115975228045a037` | 2026-06-15 | hwxiajing | feat(observability): 集成team观测采集 |
| 42 | `d7c2ab2f299cd2c73201afb41ebb8b9c00cc7015` | 2026-06-16 | huanggawa | feat: update 0.2.2 |
| 43 | `291a83631b49d86d47468a507cd80b89455f3371` | 2026-06-17 | ldstyle8 | feat(tui): add /simplify command for code review and cleanup |
| 44 | `3fd61661eb69765d2df353c4b7fea6419cc6f10c` | 2026-06-17 | xiaotian077 | feat(skill): 默认安装 skill-creator 和 swarmskill-creator 内置技能 |
| 45 | `7eea4a2575c3e4c8fac9532ca929bc4294e61d88` | 2026-06-17 | kdgmpw | feat(coding): 添加/keybindings指令，支持快捷键编辑 |
| 46 | `8b5f2e72ebe71628651469e23aff7d4df6be3db2` | 2026-06-17 | kdgmpw | feat(tui): support multiple independent TUI windows per backend |
| 47 | `da6da5f5fb276ca540f4957e50602b40768f9170` | 2026-06-17 | ldstyle8 | feat(btw): add /btw command |
| 48 | `eee0949acc38f49b038754d98a317ca0fc1d255b` | 2026-06-17 | ldstyle8 | feat(review): add /review slash command for local PR review and add /security-review slash command for branch security audit |
| 49 | `5343fb312940e3026e0f8ab21fd0491bad374e90` | 2026-06-22 | SushiNinja | feat: 新增 gitcode-api SKILL |
| 50 | `c0372411e646f9bd8546df0792e38a27d58a9fa3` | 2026-06-22 | kwangyyinc | feat(browser): headless mode support in UI + backend configuration settings |
| 51 | `c98d3b943f496f1dab7b2db2a160c71ddbfe320e` | 2026-06-22 | haowenzhong1 | [feat](tui) add terminal CLI chat entry point jiuwenswarm chat with /cli route, CC-inspired spinner, keepalive handling, and 125 unit tests Refs:#1360 |
| 52 | `d7b5639396a12be146d067dd68bfdd65ee046e30` | 2026-06-22 | sapeurs | feat(jiuwenbox): 增加 remote MCP 接入能力 |
| 53 | `a73ba6e837e38e5e4cc0359cbff84a25b110c7b2` | 2026-06-23 | ldstyle8 | feat(workspace): add path auto-completion to /workspace add\|set\|remove |
| 54 | `93c4c192cfa431b4c8a5a20c25b72541edf6fd82` | 2026-06-24 | zhengkailan | feat: Enhance usage tracking with token direction and total tokens display |
| 55 | `d89bf68abb016f25b397d82e377b290d7621a980` | 2026-06-24 | zhengkailan | feat(model): enhance model management with add, update, and delete functionalities; improve model configuration handling |
| 56 | `080477cc9dba1eeaab5c6180bf4b26aa3c472d9e` | 2026-06-25 | chunbs_cbs | feat(skill): Experience-based enhancement of skill distribution |
| 57 | `4c42b7445661fdf9eaea2512d55ff8a53091436d` | 2026-06-26 | Evan_Yunkai | feat(skills): add skill-omni-creation skill |
| 58 | `1d5b94293066b7560ad1fc2eddec28cf70f923aa` | 2026-06-27 | ShowMeYourCodePrompt | feat(memory): add lakebase memory provider |
| 59 | `3c215fa65a7553be782482111df8814dd47810c7` | 2026-06-27 | Haowei_Du | feat(skill dispatch experience): trace collect, correctness evaluate, embed and store |
| 60 | `ea1ae686b421e92d64741be173a90ee889f09dfe` | 2026-06-29 | kwangyyinc | feat(teams): agent swarm teammates now can use browser subagents, with isolated browser / teammate |
| 61 | `0e2e6d27934f289b28a9f8b58de13c6290bc8778` | 2026-07-01 | alan_cheng | feat(team): convert swarmflow worker/phase events to web team events |
| 62 | `5eb456343007660b21353d32f8f8ed5c954e7005` | 2026-07-02 | ldstyle8 | feat(tui): /btw history navigation, delete, and stale-interrupt fix |
| 63 | `7d3b6497f7483c25c2e931351211d9f0cf81f2e4` | 2026-07-03 | ldstyle8 | feat(gateway/tui): graceful disconnect with delayed cancel and session rebind， and bound tui unary agent requests |
| 64 | `4deb7cdef96fc466c45d8ead644e574ecdaa3d08` | 2026-07-06 | huanggawa | feat: update 2.3.beta1 release |
| 65 | `45c6630e96fca2923b3f3d44c0bbb1dee4127da2` | 2026-07-07 | ldstyle8 | feat: optimize agent config hot-reload mechanism |
| 66 | `4b2afc168eb747ece769f3f082608a94bc88d7fe` | 2026-07-07 | tianmengyue | feat: 支持一键进程部署基于yuanrong的gateway+jiuwenswarm函数 |
| 67 | `a7946086f8de81cd2964a97f0db7b23bb6353626` | 2026-07-07 | ldstyle8 | feat(server): scope model-switch agent reload to target channel/session |
| 68 | `b9bfa4fc49c33e6490dfcaeff49c64e6ef2c75dd` | 2026-07-07 | Haowei_Du | feat(proactive recommendation)：add proactive recommendation ability to  remind user of the schedule, recommend skills to help user solve problems, or explore implicit needs. |
| 69 | `e0927e00017d8732c3c2117dc2d59e3e02c592c8` | 2026-07-07 | alan_cheng | feat(debug): add SIGUSR1 async-state dump to service entrypoints |
| 70 | `30553466056231f29f15d6d617d920aa70c1dc20` | 2026-07-08 | chunbs_cbs | feat(skill): Experience-based skill distribution enhancement. |
| 71 | `8f54b26a7ac825d7ed6c3078f95a4cdf3f183c68` | 2026-07-08 | chensheng12345 | feat(team): implement same-session online play |
| 72 | `d87e428ca9e4ff29b8cd4663140ea825400abedd` | 2026-07-08 | fyfugoyfa | feat(web): 支持图片附件与多模态对话 |
| 73 | `a494cb417709e10a3f59cd602f7bbaa12adcae4f` | 2026-07-10 | douran | feat(project-maintainer): upload project-maintainer skill and corresponding artifacts |
| 74 | `9edd6ec914bd7dc599b09401f30630b3a0da132a` | 2026-07-13 | ldstyle8 | feat(ask-user): add preview field for single-select question options, and improve ask_user_question UX |
| 75 | `ddddc0d6c45c00920b47aa3cfeb77d42d6bcf8b5` | 2026-07-13 | feitiandada | feat(sandbox): 同步全部业务修改，使用合规CLA邮箱 |
| 76 | `d37620b02a4635df01834bccd01d398c57a2430f` | 2026-07-14 | haowenzhong1 | feat: support passing --user-id from jiuwenswarm-tui to http header |
| 77 | `d79cdc36d1bceddd61ebc8cd60651573e287ae62` | 2026-07-14 | yechenzhen | feat(gateway): passthrough Web Channel user_id via WS query to Yuanrong |
| 78 | `d99a9f8e669f21b01bdd3021d738070c66c564e3` | 2026-07-14 | yechenzhen | feat(gateway): passthrough TUI user_id via WS handshake to Yuanrong |
| 79 | `504fb78efad51cb7e7efb696d8a97f71a7f676a1` | 2026-07-15 | openJiuwen-bot | feat(web): 模型 API Key 最大长度由 500 提升至 2048 字符 |
| 80 | `522d5ab15c4663fa4ec7b480f88d5a1fd752edba` | 2026-07-15 | sapeurs | feat(jiuwenbox): 支持 Java 程序在沙箱内运行 |
| 81 | `ea105309e6ae3b87b8b627ef86db17482828a8b1` | 2026-07-15 | cycong_1 | feat: plan模式和fast模式合并 |
| 82 | `0856e694de7e48717941182b82c744cb05701564` | 2026-07-16 | yuhaiquan | feat: 定时任务改造 |
| 83 | `57d3b7715a3c699020d3e5eaa351f6d1a4abb542` | 2026-07-17 | LasVie | [mirror] openJiuwen-ai/jiuwenswarm#201: feat(openai-account): add OAuth login support to swarm config |
| 84 | `9689f75a2b4d9077b6909ca8c29ea7b1f69ac72c` | 2026-07-17 | kdgmpw | feat(aipc): 新增work mode功能，区分work和code。code模式添加git监控功能。 |
| 85 | `a83f4361e1ce4ed3402fb1d1b525d28b88b3f82a` | 2026-07-17 | huanggawa | feat: support debug and trace |
| 86 | `bf3a7220d6f265277c18a24782e0846cebfe26f1` | 2026-07-17 | lin-xiaoyu | feat(Symphony): 优化 Symphony 编排，清理死代码，精简prompt |
| 87 | `cfe1a46f122cd04affcdd0cefd9d43f805904370` | 2026-07-18 | kdgmpw | feat(aipc): 新增编辑回退接口，优化旧数据兼容与git状态监控的代码实现 |
| 88 | `168b8096ef332431a260bf34e0567f40e9173dfc` | 2026-07-20 | wncbdj | feat(harness): adapt backend goal interaction flow |
| 89 | `b65b3258d5ab57c76ad6b24e9e0baed67bc000a6` | 2026-07-20 | yechenzhen | feat(gateway): add AgentOS Router extension |
| 90 | `380ab4d61739e5534eadc37ec19b91510090083a` | 2026-07-21 | kdgmpw | feat(aipc): 新增git状态按照轮次索引 |
| 91 | `4501d0b4b1e1995c51bd4eb653803c7c0c329501` | 2026-07-21 | cjh_jinduoxia | feat: 技能演进审批弹窗改走 AuthorizationPrompt 并修复长文案溢出 |
| 92 | `47d7ecf20a8ce8a9d957e2bf9a21582300b2a7be` | 2026-07-21 | yechenzhen | feat(agentos): implement registry HTTP client for gateway |
| 93 | `5fc795de2cf42a030d1b2ddf9a0791511d53fca2` | 2026-07-21 | chensheng12345 | feat(web): add code mode frontend MVP |
| 94 | `60d73ac53944413a5d6ca41e14a0329deee4ce30` | 2026-07-21 | xutuo | feat (symphony): 支持依赖感知检索 |
| 95 | `636cf73db48283d2c270ba9a84e71b2de97911b7` | 2026-07-21 | kdgmpw | feat(aipc): web页面变更统计工作区的修改行 |
| 96 | `74e44dd397c079a6822b297be3808c995cdc1838` | 2026-07-21 | lin-xiaoyu | feat(symphony): Symphony 能力编排支持双向束搜索 |
| 97 | `98484844cedbe8767bb6a7339cb334aa99027e6e` | 2026-07-21 | kdgmpw | feat(aipc): code模式的文件监控添加无git兜底逻辑 |
| 98 | `a862878299a0358ebe138f362d271a7228f1ad7c` | 2026-07-21 | yechenzhen | feat(agentos): wire YuanRong create/delete sandbox to /api/agent |
| 99 | `aa6e3f035def487471e7a8c841061e02f9d7352e` | 2026-07-21 | yechenzhen | feat(a2a): separate reasoning from answer in A2A responses |
| 100 | `d2496da8e2cf617d4c3fa254fb8b5672a9ffe380` | 2026-07-21 | lin-xiaoyu | feat(symphony): Symphony增加能力评估功能 |
| 101 | `e879c014292721f52165ded22372cd9b1bd663e5` | 2026-07-21 | yechenzhen | feat(gateway): add third-party agent switch RPC |
| 102 | `ebb680bafe075c928c87ee1be7b9645c734b2121` | 2026-07-21 | huanggawa | feat: support trace & dump subagent data flow |
| 103 | `0a7f65bd0c737dd73a7eca64da1dd799fe52c2b0` | 2026-07-22 | haowenzhong1 | feat(tui): implement /switch-cc command and supervision ports for agentos-tui launcher handoff |
| 104 | `10845b440bd5090f7f324f5ec6f9352d2da33e5e` | 2026-07-22 | kdgmpw | feat(aipc): 补充code模式和git管理的接口文档 |
| 105 | `1ba6c22814b522d4fc5ecd6f85aea5fdcdee0051` | 2026-07-22 | kdgmpw | feat(aipc): Ignore JiuwenSwarm agent history in git diffs |
| 106 | `6485276ee90cc53b515aed956398d84dfb30d2e9` | 2026-07-22 | wncbdj | feat(goal): Web Goal streaming (is_stream), failure error text, and set history write. |
| 107 | `6a4fda346c06ef5807c76331c8b0ab2adb6af02d` | 2026-07-22 | yechenzhen | feat(gateway): route SSH channel southbound to YuanRong via AgentOS Router |
| 108 | `6afd105997b875eb8734f4ee4007603cab3b2936` | 2026-07-22 | xutuo | feat: 优化skill在线搜索，增加多源聚合 |
| 109 | `a6e080f6416474df6d3e062aa58a8c5684e68cb1` | 2026-07-22 | chensheng12345 | feat(web): support per-turn code diff history |
| 110 | `c6d6cf07069ffcf717ef24c144048983cfc07769` | 2026-07-22 | chenchunzhou | feat(swarmflow): add HITL reply path and TUI workflow UX for stateful operators |
| 111 | `da53a9211dba9b66cd8b40d34adee4d8e80fa9d9` | 2026-07-22 | kdgmpw | feat(aipc): code模式适配team.agent |
| 112 | `e97cd756c7d63d622cb48e0f53a44789023b5a18` | 2026-07-22 | cjh_jinduoxia | feat(goal): 目标管理前端实现 |
| 113 | `f17a6645ec9cf02c09dd59dec0e7979d6b369e34` | 2026-07-22 | layShang264 | feat(symphony): learn dynamic graph from session history |
| 114 | `106e738a52e4db76bf524e9e54eb8f8941fa7f5a` | 2026-07-23 | yuhaiquan | feat：新增定时任务触发session未读功能 |
| 115 | `6f347094e0b9bf457b07fae09b890312f2656fcc` | 2026-07-23 | cjh_jinduoxia | feat(goal): 目标管理模块优化更新 |
| 116 | `e381a3f08a2d1beba4dd4b73fd88c7225e615b2d` | 2026-07-23 | kdgmpw | feat(aipc): 修复code工作模型下team.agent的文件追踪 |
| 117 | `62aed632976027066cd7da37944e712f23bef2d9` | 2026-07-24 | yechenzhen | feat(gateway): add team.mq.publish WS method |
| 118 | `98b248ab9d18ffc06b652d4f2403052c9bac61f9` | 2026-07-24 | steven_sun | 支持 Java 程序在沙箱内运行端到端测试脚本路径修改 |
| 119 | `3d998198f7cbc4f820bd639e24de486d4b406a94` | 2026-07-25 | openJiuwen-bot | feat(agent-server): relay team.mq.publish to active team runtime |
| 120 | `c2dc116b4a2ca15b635471da50513ceaf9b59dbe` | 2026-07-25 | kdgmpw | feat(aipc): 新增git权限错误提示 |
| 121 | `c73eab2fdb8423ed9766fd4f7dbc69f600510385` | 2026-07-25 | xgg1227 | [mirror] openJiuwen-ai/jiuwenswarm#1307: feat: add Slack channel support |
| 122 | `646e9857757b0e30ebf1807be3d406ac6e92e303` | 2026-07-26 | yechenzhen | feat(agentos): return channels.ssh listen endpoint from 3rdagent.switch |
| 123 | `69c026a6613279964146d317e164cf32c6900285` | 2026-07-27 | yechenzhen | feature(deploy): sync yuanrong gateway template with AgentOS SSH defaults |
| 124 | `ab241e3ec75663e8e56dc48f4ed123727e76adf6` | 2026-07-27 | kdgmpw | feat(aipc): 优化 git 监控与撤销功能 |
| 125 | `2e5198e48aa07ec0ed7e9020a74a59dd71224e1b` | 2026-07-28 | cjh_jinduoxia | feat: 适配 Goal 后端耗时字段与 chat.final 气泡收尾 |
| 126 | `464ff99c6278086ec6baf90b493f41c084c58685` | 2026-07-28 | yechenzhen | feat(agentos): reclaim idle YuanRong sandboxes via activity accounting |
| 127 | `c1cd98b7d46946eea3c7ebf0a3c87d400c99a282` | 2026-07-28 | ldstyle8 | feat(tui-frontend): page btw overlay with ctrl+p/ctrl+n, close btw overlay with Enter/Space/ctrl+c |
| 128 | `fbbeeafac854c31b5f1c00fbf2f330256ba3ff22` | 2026-07-28 | cjh_jinduoxia | feat: 定时任务抽屉支持模式/模型选择，并修复手动创建任务丢 project_id、集群模式模型显示恒为默认值的问题 |
| 129 | `10c0cb4dcf46e5b92de73687c4da6379190f8a33` | 2026-07-29 | openJiuwen-bot | feat: team_name支持多session复用，后端能力支持 |
| 130 | `459bc6c2972c5b08afe3049b1fb7c2a30ee735a2` | 2026-07-29 | zhengkailan | feat: add project path tooltip to ProjectEntityRow component |
| 131 | `10ac6742afa601b4bfd286bb9fd1239a865afef0` | 2026-07-30 | yechenzhen | feat(agentos): select SSH agent from remote command prefix without switch |
| 132 | `376b70799233ca70d5e66fb503cc933e8dc87381` | 2026-07-30 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1776: feat(ppt-creation): add slide content generation skill |
| 133 | `3c96d98a66c828ac7a668f0bcf914f9eae69cf4c` | 2026-07-30 | kdgmpw | feat(aipc): 新增git提交和推送功能 |
| 134 | `839b25638d682d9835dc9af33f556a709fc5203a` | 2026-07-30 | openJiuwen-bot | feat(agent-server): support team.mq.publish over TUI |
| 135 | `988da8c58666f2b6e3a6add121d5244cace13d75` | 2026-07-30 | kdgmpw | feat(aipc): git状态监控性能优化 |
| 136 | `af8d9670a129183f36aa51fea2ab6b982a77cac7` | 2026-07-30 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1858: feat(ppt-creation): add OOXML packaging toolchain |
| 137 | `d330f5f1580bf460b4221d5e6d0d3d49d02768d0` | 2026-07-30 | kdgmpw | feat(aipc): 新增last-turn重新加载接口 |
| 138 | `4c638d9f1e7bd48e8d5c6fa85489a115b3869c3b` | 2026-07-31 | chensheng12345 | feat(web): add current branch diff review |
| 139 | `a9944051c3f1f4e3535e511f2e6e76c6b6d49505` | 2026-07-31 | Haowei_Du | feat(experience bank): add retry for exception and add batch-wise embedding |
| 140 | `c9d8db9c7331c9b397c67a44bf2adf632c3d3e10` | 2026-07-31 | kdgmpw | feat(aipc): web端分支选择器适配实时监控 |
| 141 | `13a9550d48f2cea359d1165745456d6ec9b084f0` | 2026-08-01 | alan_cheng | feat(server): instrument the chat message pipeline |
| 142 | `5036b30973fa9968e58d0714ebe121be73f7a5f6` | 2026-08-01 | alan_cheng | feat(server): add a bounded, opt-outable log preview helper |
| 143 | `7f7c842c4cd0b20c83cc425486c5e3e6f8000af1` | 2026-08-01 | alan_cheng | feat(server): break down rail construction timing |
| 144 | `c0029109d569212bece7e7b8c5065a029f3c31a2` | 2026-08-01 | alan_cheng | feat(server): break down per-request runtime config timing |
| 145 | `f5d805ed13fb4cd3597cf47f7577b347d7e2cce7` | 2026-08-01 | alan_cheng | feat(server): add a per-stage timer for hot-path breakdowns |
| 146 | `4ae4a8ae08d9cf65435046d5487d6ddc17cf4497` | 2026-08-03 | xiaoyao42 | feat(web): expand artifact preview and download support |
| 147 | `9d4c707aae6875938e61ba8fc646d76e351896e5` | 2026-08-03 | openJiuwen-bot | feat(team): Add reliability configuration items |
| 148 | `3270b5f3a80b441b9b321bab55eef95bb18939fc` | 2026-08-04 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#2096: feat(pdf): implement PDF file input and content analysis |
| 149 | `5bd38c031338eda02763e8e965d7e668fb68a21b` | 2026-08-04 | wncbdj | feat: 单agent支持plan模式 |
| 150 | `b3653b1c7eb45c7a47fe9c92dd2c8ecdf1de7fc0` | 2026-08-04 | XinchenZzz | feat: update v0.2.4.beta3 release |
| 151 | `1812fd622821507b5e52b6a2390f95feaa49c801` | 2026-08-05 | alan_cheng | feat(cli): add debug --skip-build and echo the resolved ports |
| 152 | `18ef20cd3b4cbe3a7e7e49d9583c32d81b47b6dc` | 2026-08-05 | yechenzhen | feat(agentos): unregister registry instance on idle agent delete |
| 153 | `8aa24766f8fdd678b5e716c93a9d595c443f0b2b` | 2026-08-05 | xiaoyao42 | feat(markdown): add SVG rendering and diagram exports |
| 154 | `c35e8a12c7cece4ed50a31af85d56e74d8e69fda` | 2026-08-05 | chensheng12345 | feat(swarmflow): nested workflow phase tree and swarmflow budget CLI |
| 155 | `dd94e91f4736554b11eaa1636870669e853c24fd` | 2026-08-05 | alan_cheng | feat(observability): stamp the run's final answer on the single-agent root span |
| 156 | `e76376708b84fbef4f990b53802f2ace554e16b7` | 2026-08-05 | alan_cheng | feat(swarm): end the round for clients on a team-idle marker |
| 157 | `faa0a05459ae937ae5baef717f298d3e58c444c9` | 2026-08-05 | alan_cheng | feat(cli): add debug mode and jiuwenswarm-stop |
| 158 | `06280e7b83e87e3977a1874e55fb9018a1c73e90` | 2026-08-06 | lin-xiaoyu | feat(symphony): symphony编排组件迁移至core |
| 159 | `21ac85b8c96567031b4fc9824faacf2977fbd68d` | 2026-08-06 | alan_cheng | feat(swarm): make team members visible and identifiable before they start |
| 160 | `4a337ca69efb6b3dc6e15eb7fc01dd9e3ac05e3f` | 2026-08-06 | alan_cheng | feat(swarm): mount the explore subagent in code mode again |
| 161 | `5d2224237ecdeac8f692394b5b7595e953f61026` | 2026-08-06 | Huang, Zeyu | feat(config): add support for video generation model and update related configurations |
| 162 | `89d2f2d3176efac25780e3d138ec2cc28aaad9fb` | 2026-08-06 | chensheng12345 | feat(web): add undo and redo for turn changes add commit and push controls |
| 163 | `8d75086e5c662eaf0df8534fd6bea85fccb39341` | 2026-08-06 | Huang, Zeyu | feat(media): add generate_video and auto-deliver image/video via chat.file |
| 164 | `ec15d1b06ee12a2535dd55966787ea3592379d24` | 2026-08-06 | ZhuYajun_AI | feat(agentos): add approval authentication function |
| 165 | `117ba9eeb81bb5ef164f75de14f06a71394d2854` | 2026-08-07 | yechenzhen | feat(agentos): route post-create traffic via YuanRong instance WS |
| 166 | `8ee2feb7c3814023b20433d00e5e0aa34ddb7166` | 2026-08-07 | QDylan | feat: 增强鸿蒙开发环境和工程初始化工作流 |
| 167 | `96ece95f3a571760d71483ef25c9361ca0ef388d` | 2026-08-07 | XinchenZzz | feat: update v0.2.4.beta4 release |
| 168 | `a35d6a44d09a0cb43364092f5fa9f67e62451120` | 2026-08-08 | openJiuwen-bot | feat(team): split team plan into normal and code profiles |
| 169 | `12e63eb4d5f9ce139f291ec7527700bf2675db7d` | 2026-08-10 | yechenzhen | feat(ssh): issue ephemeral keys on 3rdagent.switch for TUI auth |
| 170 | `362ffe956870ab322ffe7f96a94b01b91deb1dfd` | 2026-08-10 | cjh_jinduoxia | feat: 计划与目标模式互斥、进行中禁用与标签防误关 |
| 171 | `4ae2d9915c385777682e4de046eb8b84c9d222dc` | 2026-08-10 | taoliankuan123 | add template |
| 172 | `949fb49e017ecc73e13c63d85da387cbfd0e7ea3` | 2026-08-11 | d-0771 | feat(gateway):Team模式添加注入外部CLI agent参数 |
| 173 | `700cb7db3b249a8ff83a10df9960738e97a3cce2` | 2026-08-12 | d-0771 | [Feature]: Gateway支持systemd部署，无systemd场景回退到nohup启动 |
| 174 | `b1d6c67caf0387cc3ff41a85a768a35a849a51d3` | 2026-08-12 | alan_cheng | feat(swarm): drop team Skill link views for visibility declarations |
| 175 | `d100253af993133b3bc97981313804d3e5dcdd59` | 2026-08-12 | alan_cheng | feat(swarm): seed the team Skill declaration from the workspace only |
| 176 | `06946651964f4fd2bb3a99a80f6593ca300b7483` | 2026-08-13 | zhanglongmin | feat(agent-team): package external cli agent dependencies |
| 177 | `3ba45491f36dfbacaa16176b583721de85949b7a` | 2026-08-13 | zhanglongmin | feat(agent-team): add external cli agent config panel |
| 178 | `3e092a4259f7e8db8f27dbcb4b100165179f2a40` | 2026-08-13 | yechenzhen | feat(web): host WebChannel on uvicorn for same-port HTTP readiness |
| 179 | `5380ca2d3257da5a06767226d369d6f6c84304c7` | 2026-08-13 | zhanglongmin | feat(agent-team): add external cli agent web handlers |
| 180 | `648e770ed80f8b927d28495a48f503b3f58f74ae` | 2026-08-13 | zhanglongmin | feat(agent-team): normalize external cli agent config |
| 181 | `e69feefac2200f91f276b4995a39a93b187b180e` | 2026-08-13 | sapeurs | [Feature]: jiuwenbox沙箱Proxy支持Basic认证 |
| 182 | `f7c6f8e859239a0c6fd4a6630f994290a98e1b67` | 2026-08-13 | lzzzzzzasd | 【merge】fix #2909 发送“你好”，显示“已完成”折叠条，同session再次发送“你好”，2.回复后“已完成”折叠条消失 文件发送之后对话中显示太小 3.添加文件或者图片后，不写描述发送按钮不可点击 #3143 |
| 183 | `fca8e3902a3ac2519e40b0db3939772caff62bf4` | 2026-08-13 | Tiffany_F | 前端页面CronPanel增加data-testid |
| 184 | `030bebeec0c155299dd4c310d483809cd268c70f` | 2026-08-14 | yechenzhen | feat(agentos): add WebChannel file.raw.read/write via YuanRong files API |
| 185 | `4446a9e926675e3af0048ff958df0eb2515d8916` | 2026-08-14 | steven_sun | JiuwenBox Proxy Basic 认证支持端到端测试 |
| 186 | `25ff0f043590337205a51f5df481d1606d118689` | 2026-08-15 | Tiffany_F | 通用组件、表单弹窗、配置扩展、内容产物、团队协作、 会话导航 、主对话增加testid |
| 187 | `80184852c2ec17ee9823a68b11140dd65ae3fba7` | 2026-08-15 | slichengsong | feat(deploy): 支持分布式多机部署，删除函数注册逻辑 |
| 188 | `becf7c07eaa3a02c94e1e8b140c67383b7b3a6a1` | 2026-08-15 | alan_cheng | feat(build): add make stop target for the debug service |
| 189 | `c70eb64c7e1dc4869817e9b2a5c279bfc91fb0f7` | 2026-08-15 | alan_cheng | feat(swarm): gate explore/plan subagents on react.subagents.enabled |
| 190 | `ea943db0870aaa99f54e94f0eb92e00eb43ee2f7` | 2026-08-15 | alan_cheng | feat(swarm): enable scheduled dispatch mode for jiuwen_team |
| 191 | `0c15cb12da61d3b880c545d07f59efb780279d61` | 2026-08-17 | xiaoyao42 | feat(web): update WorkSwarm welcome and cron copy |
| 192 | `5bd1a5d617edbbdbdabaf987bb7c123e36b747fe` | 2026-08-17 | huanggawa | feat: rename workswarm |
| 193 | `a7f21fad03dc948fcc80f8b1de3db4d89236d0a1` | 2026-08-17 | yyuse | add permission modes (Full access/Auto/Strict) |
| 194 | `b41dba0cb97554aa3212215e2fddb8824636bb1f` | 2026-08-17 | min_gitcode | feat(agent-teams): install external cli deps on demand |
| 195 | `578fb2826fbb86bc26c11ef50508c569e4c369e1` | 2026-08-18 | zhangyaomaggie | feat(swarm): support the creation, loading, and execution of templates and plugins |
| 196 | `99f1f51c15360d754d39bc64bbcf710b47e5a168` | 2026-08-18 | ray_le | feat(MCP): MCP 特性（动态加载预置与自定义 MCP 服务）会话级生效 |
| 197 | `a5fd996dcd63897cc781dcbc063594821d54ec4b` | 2026-08-18 | d-0771 | feat(agentos): Gateway支持根据配置只在管理节点启动 |
| 198 | `c3f04124074eecb3977c7a63b4e83c83176130ff` | 2026-08-18 | cjh_jinduoxia | feat: 隐藏 GoalBar 悬浮框暂停/恢复按钮 |
| 199 | `d14d8f9cbfe625f4eec1a56a3ed9eed2be667cb1` | 2026-08-18 | wncbdj | feat(llm): 适配core的llm协议优化 |
| 200 | `8ff610e1b1f479027853e0bd0991ecbef61490ed` | 2026-08-19 | douran | feat: add pluggable eternal conversation rail |
| 201 | `36c7959cccc4e721240007c1211aa4ecbd34c22d` | 2026-08-20 | openJiuwen-bot | feat(team): support AgentGroup packages in hybrid team mode |
| 202 | `99c4b9d2a0a8c5889ca586480f2b33e2c255494a` | 2026-08-20 | zhaominjun1997 | feat(context_engine): integrate proactive context |
| 203 | `41b411963abca21dcd6c32f06ac1f67e8b6df6aa` | 2026-08-21 | yuhaiquan | feat(skill): 技能-我的技能相关功能需求实现 |
| 204 | `6afd291dfa11325e771abb5b36888d40474b6b84` | 2026-08-21 | yuhaiquan | feat：技能广场ucd前端重构、联调 |
| 205 | `8d1b4c14297f75914d130e19cf5cb096c98983ab` | 2026-08-21 | 2401_87256159 | feat(evolution): connect reviewer feedback to core skill evolution rails |
| 206 | `e2b76a3ece56a0a14ad9bb235ee027731c523a8d` | 2026-08-21 | alan_cheng | feat(swarm): add realtime trajectory archive and replay |
| 207 | `4ef68d6fa7d172a74777778016b54d7330fb892c` | 2026-08-22 | cycong_1 | feat(subagent): integrate subagent_spawn/wait/list/send_input/close/resume into swarm |
| 208 | `0c54930a8bb442812a49656eaf4f672c77ee532a` | 2026-08-24 | zhengkailan | feat: enhance artifact panel handling and update context compression display in ToolPanel |
| 209 | `120030c5d16f1943f9fa339101391d7cc7b8266a` | 2026-08-24 | stevenchenhanwen2006 | # feat(autofix-pr): 新增 /autofix-pr 自动修复 PR 命令（GitHub/GitCode 双平台 + --watch 持续盯守）#2099 |
| 210 | `4c2122c6247d28b262ccdb2d426f3bc21a504ed7` | 2026-08-24 | cjh_jinduoxia | feat(web): web端新增插件/MCP扩展市场 |
| 211 | `5b83ff2ab75eff246693b26b510fb9d5c41d1193` | 2026-08-24 | ldstyle8 | feat(code): accept caller-supplied DeepAgentSpec |
| 212 | `674811c79e32caff36f45a9d7ed41f04f22306b1` | 2026-08-24 | zhengkailan | feat: implement merging of history tool replay items and enhance subagent tool status handling |
| 213 | `6b8cdf08b9fb2217d822c48247e4359bd70a60ad` | 2026-08-24 | sapeurs | feat(jiuwenbox): Python ForkServer FastPath（预热 worker fork 加速 Python /exec 冷启动） |
| 214 | `3a416d4e554c0e2954c8391c61a0fa389e370968` | 2026-08-25 | zhangyaomaggie | feat(swarm): support the creation, loading, and execution of templates and plugins |
| 215 | `a232e4ed661b24a83e5d1a352de810e2c7412b27` | 2026-08-25 | yuhaiquan | feat(web)：我的技能ucd前端重构、联调 |
| 216 | `ea3b740173c74e4cd4e8939ae546cfec3ebb7d80` | 2026-08-25 | huanggawa | feat: support slash command on web |
| 217 | `24144b33d4e16b54b7d35ea966c3d815791d4a3d` | 2026-08-26 | kyrie_irving11 | feat(skills): forward SkillHub recommend via swarmskillshub RPC |
| 218 | `280a267fde188efd60e164bea856c98ca4b101ce` | 2026-08-26 | XinchenZzz | feat: implement Rail-based Heartbeat managed by AgentServer |
| 219 | `c4058d3cde0ad768c88c79243633a02ffb6ec7ef` | 2026-08-26 | lzzzzzzasd | - 重构响应式系统：删除 useResponsiveLayout，新增 useResponsive + breakpoints + useFullscreenPanel - 统一断点管理，替换散落的 window.matchMedia 调用为 useMaxWidth/useMediaQuery - 面板全屏机制改为 useFullscreenPanel（fixed定位），替代旧的 toolPanelMaximized 命令式控制 - 图标主题化：多个 img 图标改为 SVGR ?react 组件，符合 currentColor 规范 - 欢迎页 banner 由 PNG 替换为 SVG，布局调整为左对齐大标题 - CompactTaskList/TaskPlanningPanel 新增 emptyIllustration 空状态插图 - ToolPanel skillTasks 补充从 toolExecutions 识别 skill_tool 调用" |
| 220 | `ee904b9ba0584d3559b51b2cb20f7e5542b54578` | 2026-08-26 | cjh_jinduoxia | feat: 统一我的技能过滤逻辑、聊天创建插件预填、修复resume丢失plugin_names/mcp及扩展弹窗层级问题 |
| 221 | `144ea014c353ff1e335f0229b252636028e79ce7` | 2026-08-27 | xiaoyao42 | feat(web): 重构设置页，移除旧版频道和“更多”页面 |
| 222 | `3ec4bfbceb359fcb9c571c5317d29c3204e36b73` | 2026-08-27 | huanggawa | feat(web): improve compact pending and slash picker dismissal |
| 223 | `4b9bcbbdcb3b9889ee13656d755e413d9ca9e144` | 2026-08-27 | paradiseyph | [feature] JiuwenSwarm Code模式SDD需求分析与设计特性 |
| 224 | `50d0dd850defdf4b25ca4421f75cebabcd1261bb` | 2026-08-27 | yechenzhen | feat(agentos): add locatable Gateway logs without leaking [AgentOS] on shared paths |
| 225 | `b5a0def29f484d07335462a8121643940041de1e` | 2026-08-27 | yechenzhen | feat(agentos): 内置 create 固定 18092 并注入 JIUWENSWARM_USER_DIRECTORY |
| 226 | `ca0b12b072d40cf71be63e4a4c0c97c0636869a3` | 2026-08-27 | zhengkailan | feat: add agent management frontend |
| 227 | `cfb4b855b37048bb65f3ade4ca43a682c93b8c2f` | 2026-08-27 | william9200000 | feat(skill): 技能创建和技能发布功能后端实现 |
| 228 | `dbb15ba691cb43607fdea281bf3ab213fc86241d` | 2026-08-27 | openJiuwen-bot | feat(team): support agent group selection and details in team mode |
| 229 | `fc9ba5fff9a97f297642ed680027c4523b43b44e` | 2026-08-27 | cjh_jinduoxia | feat(heartbeat): web端增加heartbeat心跳功能 |
| 230 | `396d3eb9a09e76af5de5cebaaadede10be147936` | 2026-08-28 | cjh_jinduoxia | feat: 优化 web 端心跳任务面板交互 |
| 231 | `3f3dd75cec492041384de57c7c6fb1b2ecd2a8b9` | 2026-08-28 | zhaominjun1997 | feat(context_engine): 增加采集管理、进度查询和上下文浏览能力 |
| 232 | `a6e8635b358c389ea6f92e0432b571c0f8bcb368` | 2026-08-28 | huanggawa | feat(web): execute plan slash command immediately |
| 233 | `785999334d29edd5790ffdb04ed8dac4798a81f4` | 2026-08-29 | yechenzhen | feat(agentos): add container directory mkdir via YuanRong files API |
| 234 | `13c3f5ed1e38a0ee5ef96a2e8a9788c0e0177e4e` | 2026-08-31 | alan_cheng | feat(trajectory): add token-cost timeline view |
| 235 | `4069b4585bf77915b388300f1ba91273ec493bac` | 2026-08-31 | xiaoyao42 | feat(settings): 多模态能力增加开关、模型配置优化 |
| 236 | `afa0234bdeb3ea4e8bd21bb3301b12cf979a8ef4` | 2026-08-31 | openJiuwen-bot | feat(team): add frontend lifecycle management APIs |
| 237 | `c4dbc8b93b8f68284786fec6c025158b6757842d` | 2026-08-31 | alan_cheng | feat(trajectory): render team member lanes with team-mode store path |
| 238 | `4c2e47c5b9240ede6198e984b10719ae8c460ab5` | 2026-09-01 | chenchunzhou | feat(swarmflow): add workflow pause/resume/stop with workflow budget and run_id cache isolation |
| 239 | `534959a0e85d12b59795e3b13eb439683601d4aa` | 2026-09-01 | wncbdj | feat(llm): 模型适配思考开关和推理强度 |
| 240 | `765ae20fe57d3212421dc3fe9a94cbf12c3604ae` | 2026-09-01 | zhengkailan | feat: Implement agent catalog store and enhance agent identity handling |
| 241 | `a1241d0c191974f658b6076dc1054fc9b86f0df6` | 2026-09-01 | z2tng | feat(swarmskill-creator): support workflow token limits |
| 242 | `a8b54e03053603b3ea23d89e5bbdfab5c16904c5` | 2026-09-01 | lin-xiaoyu | feat(symphony): 技能编排接口适配core修改，使用标准 JGF 格式，在symphony_compose_graph工具调用结果中增加流程图 |
| 243 | `f89781af9d6218a7762c81fc8b9b9b30a9a92683` | 2026-09-01 | yangzequ | feat(rsi): deliver yangzequ AgentServer service domain, dispatch and gateway registration |
| 244 | `ff1fe3e161fef82aad207c7354748a3f29175511` | 2026-09-01 | xiaoyao42 | feat(web): 增加上下文显示 |
| 245 | `a6b1d97ea81f93055ae5b8a4f52aaac31501b5d9` | 2026-09-02 | l_x_d | feat(rsi): wire artifact optimization service flow |
| 246 | `ee1b9f2d79083423fd33cab85a5324c5bb962bbc` | 2026-09-02 | Haowei_Du | feat(feedback ) : user feedback enhanced proactive recommendation |
| 247 | `f9172b2d44e067a3e97918cb2da9917850b06a63` | 2026-09-02 | ray_le | feat(rsi-web): rsi web frontend initial commit with mock data support |
| 248 | `3201cf027e0a7b3aec9300d5d48af8654d57c3ae` | 2026-09-03 | alan_cheng | feat(trajectory): recover real error causes in the projector |
| 249 | `6a59dfc701e90d78d0beb8ffe51140846eeb97e3` | 2026-09-03 | maxmadmax | feat(permissions): 建立智能审批策略基础 |
| 250 | `a24ea71f7097c71cc99caa4692c21f488fa9350a` | 2026-09-03 | maxmadmax | feat(permissions): 建立隔离语义审查与安全审计基础 |
| 251 | `a7cc2978d9e2ad9ef762e95ce32eaf6d1ab4cddc` | 2026-09-03 | alan_cheng | feat(trajectory): align viewer with GenAI semconv |
| 252 | `c41af502c070629aabcd60afd436ac1c77310c76` | 2026-09-03 | zhengkailan | feat(agentManagement): implement category scrolling and tag summary components |
| 253 | `df5f89646227b05c6cbd90e1a0debe729ab2b26e` | 2026-09-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#2813: feat(extensions): 支持插件化多模态音视频全双工能力 |
| 254 | `02c31aa5808bd445dce75fe6a38362056fca3c6e` | 2026-09-04 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#4095: feat(browser): add browser extension as a new channel |
| 255 | `220452a61e48f2cbe8c0848e40ee01bb0e420595` | 2026-09-04 | maxmadmax | feat: 完善智能审批的工具执行与文件交付安全边界 |
| 256 | `7b0f42f6619565f740d092d86f3e7ff1da4ea5ec` | 2026-09-04 | ray_le | feat(rsi): add artifact file browsing and UI polish |
| 257 | `d4185ebd528fce4b5e06b975cba9b9c421311fb8` | 2026-09-04 | yechenzhen | feat(agentos): Web/TUI 连接时后台预热内置沙箱 |
| 258 | `dd5f2a8e8d0dd6442f8183891785bea78eaf656f` | 2026-09-04 | ray_le | feat(rsi): add feature toggle and polish artifact preview web frontend |
| 259 | `1ae189ca7f52ef6791203f01d71e2929f506e3f3` | 2026-09-05 | yangzequ | feat(rsi): integrate published Harness with AgentServer |
| 260 | `0cfdaa40c6ec0b91b7feb1e6f9c91709fe7cc596` | 2026-09-07 | maxmadmax | feat: 实现主agent审批队列与智能审批事务 |
| 261 | `2704523aa6db1d5697c4f31e6750ed2b89ef48c7` | 2026-09-07 | huanghongyun | feat(marketplace): 支持 SkillHub 专家与插件市场 |
| 262 | `31e7318abd1960e604725eaf0e6ac1071a691cee` | 2026-09-07 | yyuse | feat(permission): split and persist Global, User, and Session permission layers |
| 263 | `676e71d37271a0f2927d3ed6f046c328148cdc3b` | 2026-09-07 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#4142: feat(browser): bundle Playwright MCP 0.0.78 |
| 264 | `897d93f1a598301cbbddfdc791096dc007e1124b` | 2026-09-07 | yangzequ | feat(rsi): consolidate task-local optimization lifecycle |
| 265 | `687cde1957ba756df6d3ab8bb00280b537db21d6` | 2026-09-08 | ray_le | feat(context): proactive context frontend init version |
| 266 | `a230df410312ae5eda2b62d5e05eede47f141797` | 2026-09-08 | Huang, Zeyu | add back missing setting page after rebase |
| 267 | `c032ca2d12aa9d19cab55b791417c8d61dd1f7ed` | 2026-09-08 | alan_cheng | feat(trajectory): attribute trajectory UI components to DeepSeek Harness |
| 268 | `e330dfc963ec6c4b390349ceef4b395be7ae9d79` | 2026-09-08 | Huang, Zeyu | add seedance and minimax (#1) |
| 269 | `444d19b2a9f23244091d0b88126bf3d38d39e5c2` | 2026-09-09 | alan_cheng | feat(swarm): serve stream frames and carry their watermark to the browser |
| 270 | `51876a286e71e493654c2a205190662b7f344ed2` | 2026-09-09 | birfied | feat(skills): 程序演进技能改名为 rsi-program-dataset-creator，并补齐环境与错误信息两条规则 |
| 271 | `6d00e76c65f60f20534049ef3bfcca6dc0b1b1b8` | 2026-09-09 | alan_cheng | feat(swarm): persist model stream frames as an append-only chain |
| 272 | `8206622b555ada415f2e0707d1b8bce186364176` | 2026-09-09 | alan_cheng | feat(swarm): render live model output from the stream frame chain |
| 273 | `ae6b232b06a533ff977ee8e0808966105d3fff7d` | 2026-09-09 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#4106: feat(ide): add ide plugins as a new channel |
| 274 | `c293392e0e26da09bea49e0282e414c3f01f746e` | 2026-09-09 | alan_cheng | feat(swarm): rebuild context windows from the delta chain |
| 275 | `c7503e5447375cac7bd9654439c746daf9578387` | 2026-09-09 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#4923: feat(clip_gen): setup clip gen tool |
| 276 | `e36161745fb4b931e18f7b7715c77dc86e6c9f18` | 2026-09-09 | maxmadmax | feat(permissions): 完善智能审批权限快照、工作目录对齐与审批交互 |
| 277 | `0d04699caaf62f72ec47bc70a0f5737dc9a3aeab` | 2026-09-10 | yechenzhen | feat(agentos): drop Gateway 50MB file upload cap |
| 278 | `418623e7831502e435d1f89f4d58613355ba1743` | 2026-09-10 | chunbs_cbs | [Feature]: WorkSwarm新增预置高质高频办公类Skill |
| 279 | `63231150893bb141441c795e0fc0d92e8f231393` | 2026-09-10 | chunbs_cbs | [Feature]: WorkSwarm新增预置高质高频办公类Skill |
| 280 | `b83923ae1f8cf0a315ce1f580e016edf48a02c2d` | 2026-09-10 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#5301: feat: add multimodal full-duplex agent support |
| 281 | `dca2b2e3d71fe279994b4739a2d9d1be77980293` | 2026-09-10 | hw_liuyu | feat(process-cli): 复用 Runtime 补齐模式与会话管理 |
| 282 | `dca819b2fc35ee1516cb6ce5e28d61dbc846ab8c` | 2026-09-10 | zhaominjun1997 | feat(context): 主动上下文支持GitCode服务，完善基于规则的上下文整理 |
| 283 | `0b0490a7065404b736464d06df94620738d65513` | 2026-09-11 | maxmadmax | feat(permission): Agent会话接入与权限配置切换 |
| 284 | `103f6bcb33c78d2fb1b7cdf1ec16d167e4be42c0` | 2026-09-11 | Haowei_Du | feat(symphony): add skill pack in skill graph based on experienced traces |
| 285 | `2bc576c3de43332e0a9a874e365f14353464e13d` | 2026-09-11 | alan_cheng | feat(swarm): rebuild restated attributes from a hash-addressed cache |
| 286 | `9d42acb6affb953b007d488d44cf3a44830c8837` | 2026-09-11 | alan_cheng | feat(swarm): tell expired content apart from a model that said nothing |
| 287 | `a0c29cb21522436c625e2e5f6c2d14766a184237` | 2026-09-11 | alan_cheng | feat(swarm): resolve addressed sequences incrementally on read |
| 288 | `b3bb93816f896fa1ceef39f83757d2fdb9754f35` | 2026-09-11 | alan_cheng | feat(swarm): show every attempt of a numbered context compaction |
| 289 | `dcd2bd5f6c9e43c372f521e5dba549572c7dd31e` | 2026-09-11 | alan_cheng | feat(swarm): export trajectories as plain OTLP or addressed references |
| 290 | `e314482c512e57d0ff742f1cc603e8a17e42053b` | 2026-09-11 | alan_cheng | feat(swarm): store addressed sequences once per distinct prefix |
| 291 | `cbc2dc7949478c6f0be346d009ff15a4faf399d0` | 2026-09-14 | hw_liuyu | feat(process-cli): 新增单 Agent 一次性执行协议基础 |
| 292 | `f29c060cee90aef10e46b2a3646fe3628f60ea7c` | 2026-09-14 | hw_liuyu | feat(runtime): 补齐单 Agent SDK 必需 Runtime Public API |
| 293 | `154f1f1af5773dcea82d43067dcac6a5ce76a7f6` | 2026-09-15 | kdgmpw | feat(session): 优化session归档、恢复、删除功能 |
| 294 | `4858ab217ef0dae2cca4a98c41d8513350f0316e` | 2026-09-15 | zhengkailan | feat: Enhance AgentGroup functionality with improved error handling and identity management |
| 295 | `72bf75d1ce4b47c39c2f52b7162a48d466bb4fc6` | 2026-09-15 | kdgmpw | feat(code): 优化last-turn审核卡片 |
| 296 | `8773691e6add6fc2de99ac548d0e95701ac03905` | 2026-09-15 | hw_liuyu | feat(process-cli): 实现单 Agent 一次性机器执行入口 |
| 297 | `bab69e65adadb39cd3fa1133b5219d9c5c6dec6e` | 2026-09-15 | ldstyle8 | feat: add cross-session agent messaging |
| 298 | `e6ea2206c430f49586f92337efeb8879de9f62a3` | 2026-09-15 | hw_liuyu | feat(process-cli): 支持单 Agent 一次性执行的 stdio 双工交互 |
| 299 | `ea7ad2012529f3326034342cb9e2a5bd860e865e` | 2026-09-15 | huanghongyun | feat(marketplace): unify listing for four asset types, hub caching, and card interactions |
| 300 | `14c39401fc01d35dd3045e8cef287c03de494218` | 2026-09-16 | kusanagima | feat(web):add archived setting by project |
| 301 | `2181ea3d544113a341ee75eddd1a531c96247f83` | 2026-09-16 | BarakeelFanseuKamhoua | Add Designer audio locks, slim continuity handoff, and soft dep scheduling. |
| 302 | `28b1dec4b949ba17be72dac7bf534e054d4fb72e` | 2026-09-16 | alan_cheng | feat(trajectory)!: archive v2 is content-addressed only |
| 303 | `373ad320140d5cd967293fff4389022bc4637fa5` | 2026-09-16 | alan_cheng | feat(image-modality): honor per-model supports_vision declaration |
| 304 | `382f053190faeeb767b0b602757ba78b90e23961` | 2026-09-16 | ldstyle8 | feat(web): add ephemeral side & new & fork conversation and /goal commands |
| 305 | `480849e750b168581f08b92db89aa71fb82b1f8d` | 2026-09-16 | lin-xiaoyu | feat(symphony): 接入 Symphony 经验沉淀与组合技能安装 |
| 306 | `76a9d08d474207b742e5e3a37a2257d9966a1fae` | 2026-09-16 | maxmadmax | feat(permissions): 完善智能审批 WebUI 交互 |
| 307 | `861ce2e99768db46dbd5383e9d8f13a4791be002` | 2026-09-16 | hw_liuyu | feat(process-cli): 新增一次性 Runtime 查询与 Python/TypeScript SDK |
| 308 | `d45e728130354678bb8849d9b5751c212e0e8d18` | 2026-09-16 | alan_cheng | feat(trajectory): generate the OpenJiuwen trajectory vocabulary from agent-core |
| 309 | `f60820e7899ac732446a92307439d011ab8ecfce` | 2026-09-16 | alan_cheng | feat(trajectory): add a script that clears pre-contract trajectory data |
| 310 | `fbb901b8b47473137972a9e150f91b99d8b7f902` | 2026-09-16 | alan_cheng | feat(trajectory)!: export trajectory archives as version 2 |
| 311 | `11704010ee24f7f0117075a2ee84f26ca7189c68` | 2026-09-17 | alan_cheng | feat(history): restore tool messages from rendered_result |
| 312 | `40c9687e1b81defb0f75c6c48fab7bfbd2ad1f5c` | 2026-09-17 | alan_cheng | feat(rails): stream the model-facing tool result as rendered_result |
| 313 | `4c705d0633f98446d3f90dd3238a23732cc435c7` | 2026-09-17 | alan_cheng | feat(trajectory)!: turn-page retention with checkpoints and JSONL zip archives |
| 314 | `5872fdb1d52d33dfd34e25cb116159d6c7c50419` | 2026-09-17 | zhengkailan | feat: Enhance agent management with improved navigation and UI components |
| 315 | `63f4369a04650dccb95ef54e8a3e99da65d3c323` | 2026-09-17 | alan_cheng | feat(tui): display rendered_result for tool results |
| 316 | `6f4eaa1b6fdf46df0b5f3b660afb6bee105e37ee` | 2026-09-17 | lin-xiaoyu | feat(symphony): 实现技能包 |
| 317 | `746dfba9660179c88556a1428fe519f201b41966` | 2026-09-17 | zhangfuzhi | feat(auth): 华为账号登录与限时免费模型 |
| 318 | `8241944cff38c6cb4ee215dd404b94532fc6831b` | 2026-09-17 | alan_cheng | feat(web): display rendered_result for tool results |
| 319 | `8aaa411f9263d15303dce98080a926ece8eea3e6` | 2026-09-17 | xinyang_bali | feat(evolve): Mount jiuwenclaw translation via TTSE Rail |
| 320 | `b1924f5c44b8bfb978faf5d1cd84000b8beab173` | 2026-09-17 | alan_cheng | feat(trajectory): explain context compactions and link offloaded tool results |
| 321 | `b9bd54256e92be8eb83be7bfa44e21bc8b4f0089` | 2026-09-17 | yukuanj | feat(a4p): integrate A4P intent authorization |
| 322 | `cbdff8d42ff46f0c254b3393502fa4ef79082e44` | 2026-09-17 | hw_liuyu | feat(runtime): 接入 Session steering，支持向运行中的 Agent 补充输入 |
| 323 | `d0daf01017be6c6fd2c4c5f9064da6807379a9f5` | 2026-09-17 | lin-xiaoyu | feat(symphony): 实验功能新增“发现技能包”开关 |
| 324 | `dbc642af8d45b517b858e11b30420562e50bf589` | 2026-09-17 | alan_cheng | feat(stream): forward rendered_result in tool result events |
| 325 | `f185748852bd4d809d90fb400c2cc274d83906cb` | 2026-09-17 | alan_cheng | feat(trajectory): show a tool's model-facing result beside its raw return |
| 326 | `071ce9d80e073182230a642d8aa2fe3c5a8d646a` | 2026-09-20 | alan_cheng | feat(config): keep the external CLI built-in model catalog |
| 327 | `9dd4bb2076a1452700e7bed3f2761cd3e53a238a` | 2026-09-20 | zhengkailan | feat: Enhance agent management with skill and MCP installation flows |
| 328 | `27ed0f988e98337a1728c1ce7405003cb53288f1` | 2026-09-21 | gcw_db2yUMYp | 增加polyfill兼容低版本内核浏览器（chrome v90版本） |
| 329 | `3c03990177566079bcf5b833f19ebde3ea0d1df8` | 2026-09-21 | hw_liuyu | feat(web): 复用 chat.send 接入当前任务 steering，保持执行流与补充消息归属 |
| 330 | `43e192e30c139c086f86ff5eb5ae60983aedaf35` | 2026-09-21 | sapeurs | feat(jiuwenbox): /health 支持通过环境变量配置健康成功响应报文 |
| 331 | `4a8e45135a74f93412087819b97d79531886e36f` | 2026-09-21 | yechenzhen | feat(agentos): raise wait-running timeout to 90s |
| 332 | `52ece24aa9ef3d626334279706ecbbe038249632` | 2026-09-22 | xgg1227 | [mirror] openJiuwen-ai/jiuwenswarm#6432: feat(video-duplex): 增加语音–Agent 任务管理模块 |
| 333 | `5892cb8330256b1b8f8f3cded4bad25924262a4a` | 2026-09-22 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#6846: feat: 修改全双工配置界面入口与UI |
| 334 | `791458d34d161a4f516f1d77f9cbcfcaf1060bc1` | 2026-09-22 | hw_liuyu | feat(process-cli): 交互执行期间接收绑定当前执行的 steer |
| 335 | `c2507039a0ff7893b30c6b85b2d98f92559685a7` | 2026-09-22 | alan_cheng | feat(trajectory): drop stream frames a terminal record supersedes |
| 336 | `ed68046cff6014ea281311b7e9c7d7884d75f172` | 2026-09-22 | kyrie_irving11 | feat(ttse): wire consult retrieve mode onto the live rail |
| 337 | `f86c29263b3847a4591315ed3c6b6a23b97ffa0c` | 2026-09-22 | alan_cheng | feat(trajectory): keep the chat composer usable on the trajectory view |
| 338 | `1e3b90fdadf0f33fa9c931bbb38df287dc6c6e33` | 2026-09-23 | hw_liuyu | feat(session): 支持跨 Session steering 消息投递 |
| 339 | `40d8b3c3ecba2fb75d8b92c4d97ce11d73d5ae17` | 2026-09-23 | hw_liuyu | feat(im): 公共输入接入共享IM入站管线 复用SDK steer |
| 340 | `645679511df7e025da2f79f4887fceb2a1f18e0c` | 2026-09-23 | xinyang_bali | feature:增加ttse前端开关 |
| 341 | `7613f3d0c09d7db22bddae6079cb917986e395b1` | 2026-09-23 | yechenzhen | feat(agentserver): extract Front so listen happens before Runtime load |
| 342 | `cda5235ed4152e6cdb85d5a2c4f37d196608b63c` | 2026-09-23 | huanghongyun | feat(marketplace): unify five asset searches through Hub |
| 343 | `4ec875f02b743faec10158db1a6b49203e4f282c` | 2026-09-24 | Huang, Zeyu | add vllm omni support (#15) |
| 344 | `741a2615f07601e8b32496c52a7930235f6ec098` | 2026-09-24 | openJiuwen-bot | feat(agent-teams): The expert team has connected with the third-party agent. |
| 345 | `8a126399a1d53fb5a114a71ce8d8b21984745fe7` | 2026-09-25 | Huang, Zeyu | add sidebar menu to design project (#18) |
| 346 | `c15dd703664793f679fa6036aa12e3a8cba86b76` | 2026-09-28 | hw_liuyu | feat(process-cli): 增加会话、模型、规划、状态和权限命令 |
| 347 | `0b748dda2c8d0995837142487b15f2bbc154f86b` | 2026-09-29 | Huang, Zeyu | feat(designer): capture exact trajectory call inputs (#67) |
| 348 | `53ca3a4677c4f787608102a63b42fc703173c9d6` | 2026-09-29 | hwxiajing | feat(swarmflow): verify和fork算子可视化 |
| 349 | `d0b233552aff703dafd14d52ba303afdd4dd9657` | 2026-09-29 | hw_liuyu | feat(process-cli): 支持会话搜索选择与无参恢复 |
| 350 | `232996209b82cf4c6505b1e9a40138690548e3cf` | 2026-09-30 | Huang, Zeyu | feat(designer): route attached stills by role (design-0.2.7) (#82) |
| 351 | `f0a69728c96b5961d993449f1a901cbd2f4dac5b` | 2026-09-30 | hw_liuyu | feat(process-cli): 绑定跨进程续聊的 Agent 定义并防止同会话并发执行 |
| 352 | `3648933bdab64b9b6f4a30f41a4f2aa48ef13661` | 2026-10-07 | ZezeChang | feat(swarm): integrate team organization behind experimental UI flag |
| 353 | `661710be338a73cff2141cf826a25697f623a248` | 2026-10-08 | hw_liuyu | feat(process-cli): 支持运行级 Agent 能力与权限控制 |
| 354 | `d8a9eb79df41995f58a14534298a5f7adaf69822` | 2026-10-08 | hw_liuyu | feat(process-cli): 精简非交互式输出并保存诊断日志 |
| 355 | `de9fb179022136a059a184584123b4f3d1be7c77` | 2026-10-08 | alan_cheng | feat(trajectory): prefer time to first token and show TTFB in request timing |
| 356 | `4d6b38b1fdd3de87ce0653fd364cc10d414e3e83` | 2026-10-09 | weichenhao | feat(agent-teams): Support the swarmchat message type |
| 357 | `872a2fb1e6c8710700064def82f6c3882ca30fbb` | 2026-10-09 | alan_cheng | feat(trajectory): flag inferences whose request was not observed |
| 358 | `a27e3d13e98bb213e1bd73b8f480de8bb2ef0a12` | 2026-10-09 | hh0205 | feat(team): support team model group routing |
| 359 | `dff72c219bd2bf3feecbb1e2c8acfca8bc31d3f4` | 2026-10-09 | ldstyle8 | feat(code): enable default multimodal tools |
| 360 | `1f345884de2af62edbb902d9312f253970f073d5` | 2026-10-10 | kdgmpw | feat(agentos): 适配 AgentOS 多用户场景，冻结客户端契约兼容与数据面能力移植 |
| 361 | `25ad1fdb1a980bba0b8cd5a498aedd0f89299c68` | 2026-10-10 | hw_liuyu | feat(process-cli): 完善 SDK 运行控制、宿主工具回调与 JSON 协议契约 |
| 362 | `a228f937b4bdbbb40932446f1d0d078a560dc896` | 2026-10-10 | d-0771 | feat(gateway): Gateway解耦工程，修改E2A和部分接口依赖为外部gateway-protocol包 |
| 363 | `b0433f822456c321440dc064f456d1f5d1445047` | 2026-10-10 | yechenzhen | feat(agentserver): run Agent Runtime in a resident Worker process |
| 364 | `e82407e3c4b660efdaf2aa8807f4c6950f9cfafb` | 2026-10-10 | gcw_3GcDk5mt | feat(agentos): 同步gateway侧etcd配置下发到develop |
| 365 | `f5de110b39c6b79c253e9065292ddd2b6c16c492` | 2026-10-10 | zhangqingguo1 | 修复旧版本存在配置文件残留导致解析字段bug，触发前端添加模型按钮置灰，页面报错，Bug https://gitcode.com/openJiuwen/jiuwenswarm/issues/4967 |
| 366 | `1c881b34cfe4955414b6b2ac0ee63957d311291f` | 2026-10-12 | chenchunzhou | feat(swarmflow): Carry agent_type to the Web tree and render third-party node avatars |
| 367 | `27d545e1bca219a407fc0e8363625b85e2979c3a` | 2026-10-12 | alan_cheng | feat(external-cli): install third-party harness providers from the runtime manifest |
| 368 | `2917879826f543ea94b1fe9a1c236af2c3cbec8c` | 2026-10-12 | alan_cheng | feat(external-cli): upload custom harness provider agents in settings |
| 369 | `405d31f7e16f007adc8f5a7c949caf2e0496cf6c` | 2026-10-12 | chenchunzhou | feat(swarmflow): Refresh node model from completion and failure events |
| 370 | `4d2135fc2df91a38cda6702ec7449c2e6af9f237` | 2026-10-12 | yukuanj | feat(a4p): 引入A4P授权协议，实现session和定时任务的权限预授权功能 |
| 371 | `90fe58182c3079b0749f1d162ea2beb74fe0e976` | 2026-10-12 | XinchenZzz | feat(windows): sign packaged executables when certificate is available |
| 372 | `9f1e6d2b67dccf8f3d52f9d4202c4dad546bd6aa` | 2026-10-12 | chenchunzhou | feat(swarmflow): Add swarmflow_progressive_tool config key for worker tool exposure |
| 373 | `b892f7f9371d0dca14ff95c9d78ef4e5daa36edf` | 2026-10-12 | chenchunzhou | feat(swarmflow): Surface worker tool activity as live call cards with run-grouped views |
| 374 | `d6c67ccf5e7fcef28e1e2a55a624900603030508` | 2026-10-12 | alan_cheng | feat(external-cli): discover custom agent models and keep catalog facts out of config |
| 375 | `134fe19793f7046d31bb9ead1ee5502dfe2cb7e1` | 2026-10-13 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#7846: feat(GenTools): Allow task chat to create/edit images and video clips |
| 376 | `1fd4c422a31fbcc9b3159bb5b4508c5673cba955` | 2026-10-13 | Henry TIAN | feat(designer): add a video generation option on the home composer (#15) |
| 377 | `320dca013a3f9eab187b1294b06b6c8be05f7d27` | 2026-10-13 | ZezeChang | feature(swarm):修复集群org模式下切换session导致的异常关闭 |
| 378 | `32b93ca11fe91881085ed05edb6066b0580301d8` | 2026-10-13 | kwangyyinc | [mirror] openJiuwen-ai/jiuwenswarm#8083: feat(browser): port Jev integration and settings to develop |
| 379 | `73a535e67498186371c1d66ebfb5a6021b63d6fb` | 2026-10-13 | hw_liuyu | feat(process-cli): 支持非交互命令配置模型、API 地址和密钥来源 |
| 380 | `9384bc2c74b218432d221ec13a5eadb2b5347cdc` | 2026-10-13 | hw_liuyu | feat(process-cli): 增加终端会话 ID 查询并完善跨进程续接落盘 |
| 381 | `fbd8d6f41a194b94e7a1361f2c43e4d15e6103f8` | 2026-10-13 | hh0205 | feat(settings): 实验功能新增调度模式与验证者开关 |
| 382 | `1c0c75f13197e89937fbbe418dc7d28431c03024` | 2026-10-14 | maxmadmax | feat(permission): 启用智能审批入口 |
| 383 | `1e36180c99849e91dd4529c791eca423fc5ba086` | 2026-10-14 | xiajing | feat(team.design): adopt the SDD-0030 expert table for agent_template workers and uniform options style |
| 384 | `28383f2724709eb880f19103ee806f92aea40331` | 2026-10-14 | alan_cheng | feat(web): add a per-session scheduled dispatch toggle for team mode |
| 385 | `2a5aa01ab42de939a9fcd5b90a73c430837d217f` | 2026-10-14 | chenchunzhou | feat(swarmflow): Surface agent_template expert workers on the canvas |
| 386 | `3810e60566f44e448ab3d5dbe90913394f8acfba` | 2026-10-14 | lzzzzzzasd | feat(web): RSI 实验栏收起态 peek 悬浮预览与详情头部交互重构 |
| 387 | `3a80e946116f1fc5342c1a3d93b1f80c133175ea` | 2026-10-14 | ZezeChang | feature(swarm)：修复专家团广场的team接入问题并优化组织界面 |
| 388 | `99012b31d9aa23314a317c22b32db98330f21cdf` | 2026-10-14 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7676: feat(clouddoc): provider contract, text map and shared format helpers (co-scr... |
| 389 | `ac838251bc30f8fd2f3eb88f6c93db280295819a` | 2026-10-14 | chunbs_cbs | [Feature]: tool渐进式披露优化 |
| 390 | `cdc76d7ca0d32a7fe5d23aff1f623b5bd57edf5d` | 2026-10-14 | xiajing | feat(team.design): designer.media.capabilities RPC and media settings patch |
| 391 | `d4592dddfecd5c5e1da655ee1c5cd762b4012213` | 2026-10-14 | xiajing | feat(team.design): design execution on the new architecture |
| 392 | `d48931ccdf593b13d2f1e134c9208bbee3a522bf` | 2026-10-14 | alan_cheng | feat(swarm): assemble a read-only reviewer spec for scheduled-dispatch reviewers |
| 393 | `f466408480a8b27fbbc8c333c53647688fda9bad` | 2026-10-14 | xiajing | feat(designer): standard chat panel and session wiring on colleague UI |
| 394 | `04b95b08757da1d0bb24428ae2574ed9f82b892b` | 2026-10-15 | Rossoneri_1007 | Feature：模型组 |
| 395 | `131c18483e1375cc4dd814861664a20d957e16f5` | 2026-10-15 | weichenhao | feat(group-chat): Supports expert joining and expert group leader agents to execute on demand |
| 396 | `15b0bf0d2c86fe1d518bec75da9e094e6103b575` | 2026-10-15 | xutuo | feat: 新增 Computer Use 通用引擎 |
| 397 | `2bdda11df624eead355715d9fbdc4144cbd909d4` | 2026-10-15 | zhangyaomaggie | feat(swarm): Single-Agent Mode Supprts Third-Party Harness Integration |
| 398 | `33ab41ed0fdedfc7eda2c7140a34af0c23356805` | 2026-10-15 | xutuo | feat: 新增 computer-use Windows 驱动：界面读取与输入控制 |
| 399 | `54a9f040fa08f2569e264de5e9d61b39f02352ae` | 2026-10-15 | Rossoneri_1007 | Feature：模型组 |
| 400 | `5d2600b484030dfaaa5f2b2224bcccdcd74af52e` | 2026-10-15 | chenchunzhou | feat(swarmflow): Mark expert workers on every surface and keep them across restore |
| 401 | `6db955871770ecdd0e12868124809f5706c345a6` | 2026-10-15 | xutuo | feat: 新增 computer-use macOS 驱动：输入控制与视觉反馈 |
| 402 | `7baa3216437c1d002fe500a207df02d92d45864e` | 2026-10-15 | xutuo | feat: 新增 Computer Use 无障碍观察与展示 |
| 403 | `7f1b346983fed661d9b2b57eaa2623cb4d60c328` | 2026-10-15 | alan_cheng | feat(team): mount tool_search on team members via progressive_tool_enabled |
| 404 | `8b2f32b1a463ff01ad38fcca5ecac94fbf26e803` | 2026-10-15 | xutuo | feat: 新增 computer-use macOS 驱动：协议运行时与界面读取 |
| 405 | `8eff19800951a772331f0b4d8731feb39ea9a704` | 2026-10-15 | kwangyyinc | [mirror] openJiuwen-ai/jiuwenswarm#8317: feat(subagents): wire the cua (desktop automation) sub-agent into jiuwenswarm |
| 406 | `987a40ea7136d665b531b3bb3dd46b1aee8eabe2` | 2026-10-15 | min_gitcode | feat(agent_teams): integrate team sessions into unified runtime with cross-session messaging |
| 407 | `a534c116e2806d058b057eba30dcf5e99f288f53` | 2026-10-15 | xutuo | feat: 新增 computer-use macOS 驱动：基础对象与窗口边界 |
| 408 | `bb8535704775a1205c86ce15e16505f4ae05954f` | 2026-10-15 | maxmadmax | feat(permissions): 支持Code模式智能审批 |
| 409 | `d03231f0749e49dd2cadf8bb88de2d8419d4bcbb` | 2026-10-15 | xutuo | feat: 新增 Computer Use 原生协议与 MCP 运行时 |
| 410 | `e487e9a3048fb84c9fbddb346e1200bd108a2e03` | 2026-10-15 | lamsaan_yau | feat(moa): add MoA with JEV and LLM verifier routing |
| 411 | `ea793d9c4d6b14df9a948e8e79db1c782a313c76` | 2026-10-15 | For_YL | feat(mcp): 新增百度网盘连接器包（官方 SSE 端点）+ sse 远程连接预检修复 |
| 412 | `3f353fa45853c75b591af2b29ea2a5ed3a3344d0` | 2026-10-16 | xutuo | feat: 新增 computer-use macOS 驱动单元测试：输入与视觉反馈 |
| 413 | `4522cd2095e13e388f9a1c6fa48f62017e48f31c` | 2026-10-16 | yechenzhen | feat(agentos): proxy 3rd-agent Web UIs on registry-assigned ports |
| 414 | `78f92632fc9e5e1a27171c46f0b86ec390de5439` | 2026-10-16 | alan_cheng | feat(team): add session-level framework bootstrap switch under scheduled dispatch |
| 415 | `d923e8ee2bdbbf5290350748eb84f2d4d55b7ef7` | 2026-10-16 | alan_cheng | feat(team): carry scheduler handoff on team message events |
| 416 | `dc00c999c808e86190abef00d2daa62375b8f812` | 2026-10-16 | alan_cheng | feat(web): render scheduler handoffs and task flow in group chat |
| 417 | `55ae18b14da6371d11fab6800f9a1184c4a82d4a` | 2026-10-19 | lzzzzzzasd | [Feature]: 上下文模块优化页面视觉，体验用户提升 |
| 418 | `8740f1edde957cb7d1e76ee0a897d28750bb5118` | 2026-10-19 | william9200000 | feat(skills): 支持skill中英展示与内置技能白名单 |
| 419 | `de269e10b81bb7f4b9a18991f50a764009005a8e` | 2026-10-19 | yukuanj | feat(a4p): 优化会话A4P预授权，过滤无需新增授权的申请 |
| 420 | `1756e5b4dfe8b2e09b95ff3237f231ed4ff9a1f6` | 2026-10-20 | XinchenZzz | feat(build): expose packaged source revisions |
| 421 | `38b56cf87c864d7b592dccda7719efe24b2346f8` | 2026-10-20 | gcw_3GcDk5mt | feat(agentos): manage agent and tool sandbox settings via etcd data plane |
| 422 | `a54fd726abb733006ebaff4e52169e2125c7b3a5` | 2026-10-20 | alan_cheng | feat(team): locate installed experts for the leader and parse packages in openjiuwen |
| 423 | `1ad240f9b0ace7760125373a3eab13ad15101e0b` | 2026-10-21 | gl_zhou | feat(evolution): 轨迹归因：对轨迹做诊断与归因，指导 Agent 进行任务修复与演进 |
| 424 | `4da8853a1ba7de2a0348728941b07ab88472053b` | 2026-10-22 | huangyuzhe | feat(memory): 新增 jiuwen 内置记忆变体挂载 |
| 425 | `9ed959016ea2ea6a7f4ce0b1ab93eab438bfc3c5` | 2026-10-22 | alan_cheng | feat(team): add member messaging sub-switch under scheduled dispatch |
| 426 | `5e691040748b0a56551b1eef1160a54047fe273d` | 2026-10-23 | Haowei_Du | feat: 话题推荐和提示词推荐 |
| 427 | `b688478f1c6aa8194759e41787bf81e3e19e7e79` | 2026-10-24 | alan_cheng | feat(team): start scheduled AgentGroup teams with an empty roster |
| 428 | `f8e95546a93536a269013a7218750743117671a1` | 2026-10-24 | alan_cheng | feat(team): add member subtask sub-switch under scheduled dispatch |
| 429 | `0b31c70d0fbffe105f1e348927c3747e5a283393` | 2026-10-26 | alan_cheng | feat(skills): sync decision and behavior evidence workflows |
| 430 | `71648a3914dec5718bf253264fbe456a441937a0` | 2026-10-26 | kingdomhit | feat(build): 移除 requires-python 上界，放宽至 >=3.11,<3.15 |
| 431 | `8dbce35396b3e0cdc88b38a5b33d5b6c9426515d` | 2026-10-26 | neatline | feat(gateway): add ADX ingress adapter for appliance deployments |
| 432 | `94eb17a88e0ac7f983bc7f5e39228d42a2a6e268` | 2026-10-26 | Haowei_Du | feat: 话题推荐放在对话框下方 |
| 433 | `e49cc90200776f3a8d7a5a269dad5387db172eb7` | 2026-10-26 | alan_cheng | feat(skills): sync causal flow and context confidence guidance |
| 434 | `5f0232ddb46222d9c7dba50b4baead93965cd534` | 2026-10-27 | neatline | feat(gateway): route ADX agents through Environment APIs |
| 435 | `14c26f5a46c47fe2beed0b4da705ad0f2115ca83` | 2026-10-28 | gl_zhou | feat(diagnosis): refine live process, findings, and suggestions |
| 436 | `579c5f561058906f7302b366aa4ba9103a50d51b` | 2026-10-28 | alan_cheng | feat(retention): support indefinite session and trajectory retention |
| 437 | `e02280a1f4d774082aa6debfda057a0d2eb8508a` | 2026-10-29 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#2362: feat(chat): emit llm_call_start / llm_call_end stream events for LLM activity... |
| 438 | `0722806d2a45b8fadbfc7db5588b5d646e278537` | 2026-10-30 | meizi6 | feat: 企业版特性握手 |
| 439 | `0187d6d00538eb9fc6bf87f919ccd48d1a4fdd46` | 2026-10-31 | jack_ricky | feat(mcp-apps): complete App-to-Agent interaction |
| 440 | `3c235295a42dac9feb63fedec8cb031d5b1d43b2` | 2026-10-31 | robbluo | feat(gateway): integrate ADX AgentServer and tool environments |
| 441 | `eabe143dbefcae6508beb8acdb3d03eec6f12644` | 2026-10-31 | kdgmpw | feat(web): 定时任务执行与聊天会话统一 |
| 442 | `65a3fcb46f077af2b4fe1b05ac958000e08beae5` | 2026-11-02 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#10035: feat(interrupts): tell interrupts apart by kind and count answers addressed b... |
| 443 | `a83d39bbd048818da156cdf1664dfa170b497d18` | 2026-11-02 | wangmingqi | feat(enterprise): 企业版 web 放开连接器广场导航 |
| 444 | `c44cdfca9fba9e466c5e0c92abb0d30d77d3ee16` | 2026-11-02 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#10347: feat(interrupts): clients echo the interrupt_id they answer, and the server a... |
| 445 | `c9f2cc0723b1dd9303638aea504bf8efb338c4d7` | 2026-11-02 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#10361: feat(client-tools): run tools in the connected client and resume with its res... |
| 446 | `094276a39743c15861585351b3037d689d68b094` | 2026-11-03 | CUIZIBO | feat(enterprise): add team quota and route binding |
| 447 | `341dfdddd4a6dd2b7f8f8fde5a27eabebe05a706` | 2026-11-03 | xinyang_bali | feat(ttse): support code agent integration as a native plugin |
| 448 | `44bc50f9ef88726d6d22989c6b166a22050f6e71` | 2026-11-03 | hh0205 | feat(team): support identifying the routed model in model group |
| 449 | `5f6e642a5a7acbaa09223b38733d86874ac92adb` | 2026-11-03 | hh0205 | feat(trajectory): optimize scheduled dispatch trace analysis |
| 450 | `9251bbd29996472ff02a55bff4aada0aded23b4d` | 2026-11-03 | Rossoneri_1007 | Feature：在会话开始前推荐话题 |
| 451 | `a2abceccfba89ab25a518568916152865fa28acd` | 2026-11-03 | wangcan103 | feat(swarmflow): group workflow traces and show operator details |
| 452 | `d4048ca9016d058e6ce3bbe2950b15c67f5855ba` | 2026-11-03 | CUIZIBO | feat(enterprise): add team control store and membership |
| 453 | `dee257a66cba61c9d7b61236b2e6804aee9ee222` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#10382: feat(client-attachments): attach each client to one conversation and let conv... |

## 缺陷修复（1904 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `184ae75c4b5c0736e3b526000d931e727bac660a` | 2026-06-01 | iamcandiceguo | fix(team): hitt error message fix |
| 2 | `51b3db03f7699c30d857344da81c0edafa425d44` | 2026-06-01 | openJiuwen-bot | fix：agent下拉框技能面板只能选择已安装技能 |
| 3 | `58af273598dbdc503773cbaf1447cc6d80dd4f36` | 2026-06-01 | slichengsong | fix(yuanrong): Fix the failure to parse SSE streaming response which causes the web page to become unresponsive |
| 4 | `64de1b98833c42189e2f9934928fa1b702bd9886` | 2026-06-01 | xiaoyao42 | fix：删除前端调用snapshot的逻辑，通过将status_change事件数据持久化来解决刷新页面，成员状态不恢复的问题；修复刷新页面task状态偶尔会变回等待中的问题 |
| 5 | `67247644faa8c5e6c702d30272f4655e354f4850` | 2026-06-01 | kdgmpw | fix: Task splitting capability aligned with Claude code |
| 6 | `6d470c2d5694f9710e1d8610720cca890214172c` | 2026-06-01 | openJiuwen-bot | fix:集群模式右侧面板展开可以同时展开左边栏 |
| 7 | `a47ab7c959419ac9e6fdf9b5a1a6450fa0561d42` | 2026-06-01 | ray_le | fix(auto_harness): cancel support & bug fixes |
| 8 | `a704e66d8cc9b939c930a7ad1f348769afc76985` | 2026-06-01 | openJiuwen-bot | fix：1.删除冗余的安全配置-Embed配置；2.下掉集群模式-规划面板-已花费时间 |
| 9 | `bb79921064c3c6c7cdc6681b85bde5168bc9ac7e` | 2026-06-01 | yan_zj | fix(context):Optimize the method for obtaining the maximum value of the model context window |
| 10 | `cecdf00f57b68ddaf3c06a545251f5322c9391e9` | 2026-06-01 | jingjinduo | fix: tui 隐藏/help中部分命令，取消禁用 |
| 11 | `dcd24799c955d5cd73af49617ad99ba0e62e7409` | 2026-06-01 | chensheng12345 | fix(TUI): Support English TUI status text |
| 12 | `e5da16707a49e066bfccf6321a84ba676ee404c8` | 2026-06-01 | iamcandiceguo | fix: include message_id in team event dedup key to prevent duplicate message loss |
| 13 | `f7d8ad5939770a56f9b502eb11bff6be8e2339f7` | 2026-06-01 | wncbdj | fix(heartbeat): Add heartbeat content to the user prompt |
| 14 | `2b7b887603b7dead6bc2b1173c1bc483633653c1` | 2026-06-02 | ray_le | fix(auto-harness): fix auto harness config edit |
| 15 | `46b95477656b8917cf140c2734ea2f82ee78f0cf` | 2026-06-02 | weichenhao | fix(tools): windows bash tool |
| 16 | `4f6a4a475bdaeb2d8e2965a1a8948be3a7e618d7` | 2026-06-02 | ray_le | fix(auto-harness): refine pipeline final stage status |
| 17 | `7cdc9357ce1adb9448bb349f3720dc11ac778be4` | 2026-06-02 | jingjinduo | fix: tui 隐藏/help中部分命令 |
| 18 | `a6b94284c9d7e7c4272c0f0a0bff49af42c2d4dc` | 2026-06-02 | ray_le | fix(auto-harness): 取消默认刷新解决加载生效问题 |
| 19 | `c216b9d19d8eb60dbca35dae5b8583e809367bdf` | 2026-06-02 | huanggawa | fix(desk-app): remove wkwebview system cache on Macos |
| 20 | `c8146ad1498fcd0139afc8e7760d24d5d971ac22` | 2026-06-02 | ray_le | fix(auto-harness): extend pipeline progress percentage calc fix |
| 21 | `d7d2d393a5d6c7d7774bafecb19c91457d2c3957` | 2026-06-02 | douran | fix(TUI): hide hidden commands from tui suggestions |
| 22 | `df3a8d9ac583ec134c3b876417b7332ce2b46f70` | 2026-06-02 | openJiuwen-bot | fix;删除被agent引用的模型后，agent自动配置主模型且适配参数 |
| 23 | `fab51e6bf419e2928c1c7e7f0f07b1c77de1274e` | 2026-06-02 | wangxiaolong100 | fix:team默认配置项删除恢复问题 |
| 24 | `017654ecdfc0286c01e7fbf2ed00304b20d1ee7a` | 2026-06-03 | openJiuwen-bot | fix(skill): Improve error messages for local skill import |
| 25 | `0da940f52eea4478d026e79b89a3ca2c900b5c03` | 2026-06-03 | cycong_1 | fix: language switch stuck loading |
| 26 | `4bf08d65bbf8898dcb4c21cfc2c3a9aeca48de15` | 2026-06-03 | yan_zj | fix(swarm):Clears the context window usage ratio when creating a new conversation on the frontend. |
| 27 | `89b9fe7f6e7e4a15ceffdbc0cbc05f9058b53bf7` | 2026-06-03 | ldstyle8 | fix: unregistry /plan command for current version |
| 28 | `9371319aa3ba8fafd715b409c3658c3e46dd6a7d` | 2026-06-03 | openJiuwen-bot | fix(tui): After /resume, /recap can be called immediately |
| 29 | `a11e75e79ee5c1f08b1b26343d886ce938c2dc74` | 2026-06-03 | openJiuwen-bot | fix(tui): Support interruption via Ctrl+C during manual execution of /recap |
| 30 | `a1c642326cc3fd4af5aeb3716729abedd50658b9` | 2026-06-03 | l_x_d | fix(context): Adjust the message_summary_offloader processor parameters |
| 31 | `c2fc658f4249c94f820bb75a5686bd02b26d4e93` | 2026-06-03 | openJiuwen-bot | fix(web-frontend): Agent Configuration, No Prompt for Skill Installation |
| 32 | `d1e3d316afc368793c67b638cfc6c89d2afe6ac1` | 2026-06-03 | haowenzhong1 | fix(tools): cluster mode restore and token expiry |
| 33 | `e268e103cf1d885b0a788262e9b60b28f7738938` | 2026-06-03 | yangzequ | fix(llm): add app attribution headers for OpenRouter provider |
| 34 | `e325af11cbd8022b13b9118c366ad9070fce4d60` | 2026-06-03 | openJiuwen-bot | fix(Web-frontend): Show/Hide plaintext for Cli Token in skill source management (ClawHub) |
| 35 | `e3b59acba9b9eb56d60400c845e4e96293509273` | 2026-06-03 | ldstyle8 | fix(plan-mode): refactor plan mode with stabilized KV cache, state management fixes |
| 36 | `ebaa9ab8407f485d03ca441b94680a4a6c9d294d` | 2026-06-03 | zhengkailan | fix: simplify button disable logic in ChannelsPanel component |
| 37 | `041f74d45a75e3c1cde3f934eef6ec6f82fef981` | 2026-06-04 | z2tng | fix(skill-evolution): align skill-evolution flow with latest openjiuwen |
| 38 | `071fa2bc25ec3fee53cf699c144fdc14c50930c6` | 2026-06-04 | openJiuwen-bot | fix(TUI): Fix the error reported when executing workspace add on directories without access permissions |
| 39 | `0c5f3d01393e8d1346693e0def0df6e354ea159b` | 2026-06-04 | openJiuwen-bot | fix(TUI): /branch my-feature-branch yields /branch my-feature-branch. |
| 40 | `0f8d83098f04c40597f64cbdebfe3c71fa684703` | 2026-06-04 | ldstyle8 | fix(/memory): remember list pointer location |
| 41 | `18cbaa520e58c2780a516bfd28b0946b70a929e6` | 2026-06-04 | zhengkailan | fix(team): Enhance member shutdown logic in collectTeamState function |
| 42 | `21e7bab9daffc3cc3ecdef39075087ab8d246ef3` | 2026-06-04 | zhaozixuan | fix: delete unuseful info from sandbox |
| 43 | `2de1aa2c535db296f0b51778b286dea81518c585` | 2026-06-04 | wncbdj | fix(subagent): Modify the subagent's workspace |
| 44 | `36638120f42d73bc16c570344960618f470bbf1c` | 2026-06-04 | openJiuwen-bot | fix：技能管理-技能广场-刷新效果优化 |
| 45 | `37aad02a96d88404ee7a250317cc0dd6f2d921a4` | 2026-06-04 | openJiuwen-bot | fix(team):The issue of Chinese and English language coverage in the team mode |
| 46 | `6510e6f9792d7e9806fcddc7062aae96d29c79ea` | 2026-06-04 | openJiuwen-bot | fix(TUI):/recap supports automatic triggering |
| 47 | `697aacba1101a1be4a545fdd7eff3e16fc7a00e9` | 2026-06-04 | ldstyle8 | fix(tui): keep streaming state and live thinking display in sync |
| 48 | `739fd8d2cea6e723612af0bb2eacb6c99564499d` | 2026-06-04 | openJiuwen-bot | fix(TUI): Inconsistent error messages for /branch |
| 49 | `74b51fef8629b176d16b9498ccb681d13346cd08` | 2026-06-04 | jingjinduo | fix: 修复tui /recap 输出错误 |
| 50 | `7720f45d6cac16675fa92eb1d3e23f068b967cf6` | 2026-06-04 | min_gitcode | fix(agent-teams): manage runtime skills with shared links |
| 51 | `910af57942da73d8cafd88c08a6371d5c1c5a913` | 2026-06-04 | xiaotian077 | fix(skill): using slug to uninstall skill with unsafe name |
| 52 | `97a7cdc768f7d8709769dc225a0c2f60fd2c33a1` | 2026-06-04 | openJiuwen-bot | fix：1.完成超时时间大于0；2.下掉告警提示 |
| 53 | `9875bec21e3ca959dc18116203bed2997a7dadb5` | 2026-06-04 | yan_zj | fix(swarm):Team mode shows only leader context window. |
| 54 | `9a45a2ccb7d3f70e538d326bb178b860eca45239` | 2026-06-04 | openJiuwen-bot | fix(web-frontend): Session - Cluster Mode - Modify icons for task overview/team member status |
| 55 | `9e31366b0a7eaeb8fe633abf7f45856be8ea9c95` | 2026-06-04 | openJiuwen-bot | fix：集群模式-暂停后会话窗口placeholder修改 |
| 56 | `b5d4a6cedbaa02d573bfa565eafc5b2131286c9d` | 2026-06-04 | openJiuwen-bot | fix(security rail): add alert and retract |
| 57 | `badbe793ada3538d5f8f30b2bb1c66c84a6d84ec` | 2026-06-04 | ray_le | fix(auto_harness): package加载skill不生效问题 |
| 58 | `bdcda1035b1bb1500f1c91612687db276e59c3ba` | 2026-06-04 | GeorgeGcs | fix(skill): safely extract downloaded skill archives |
| 59 | `c10f6d77fae8bc989f015d6f09049eac8a82027a` | 2026-06-04 | openJiuwen-bot | fix(web-frontend): Change the limit parameter for skillNet search to 20 |
| 60 | `d4abb8098e8b0e066dda354d8ab1b28d43096de0` | 2026-06-04 | jingjinduo | fix(TUI): Fix the color configuration when resuming the tui |
| 61 | `f0055682700de6290e92b8d1e39eb236db81e59c` | 2026-06-04 | wncbdj | fix(prompts): Fixed the issue where the final result was not displayed on the front end |
| 62 | `fbf59d75427baa9951115a672397d1758532f4e5` | 2026-06-04 | xiaoyao42 | fix：增加对话分享功能 |
| 63 | `2ea237f1cc62e792780a1aeb171efc4ffcf57430` | 2026-06-05 | xiaoyao42 | fix：将p2p里面touser的数据在主会话区域显示，将导出放大倍数从2改成3 |
| 64 | `41aac3d4979814533c66948026129c8243c1014f` | 2026-06-05 | openJiuwen-bot | fix(memory):Write "memory.db" to the agent workspace. |
| 65 | `4e175988dd3e27adf35bcf85f9202fb34d25c980` | 2026-06-05 | cycong_1 | fix: language switch stuck loading |
| 66 | `73d02d3cf68dee6f7d5c64b29688e666632363fd` | 2026-06-05 | wncbdj | fix(rails): The task was not completed, abnormal return |
| 67 | `7afc49c8077cc91207fbb436e6f119157f0b9b54` | 2026-06-05 | ray_le | fix(auto-harness)： 删除package同时取消激活问题修复 |
| 68 | `93032dc44b2e060cd2a5ec3227071f89380ca9f8` | 2026-06-05 | ray_le | fix(auto_harness): 取消激活技能时性能模型未生效问题修复 |
| 69 | `93b219d8a59005ae969128e075985f67cbe9f7e2` | 2026-06-05 | openJiuwen-bot | fix：agents/team未配置不给保存 |
| 70 | `e76760f305cb68e9e7867825d4ae63b7b6200e74` | 2026-06-05 | openJiuwen-bot | fix:/export粘贴到剪贴板时间接性报错 |
| 71 | `5bf200998f076880e7b0c87132ab6ff14d0d65d1` | 2026-06-06 | ldstyle8 | fix(rewind): truncate session-specific file_ops logs on rewind_session |
| 72 | `916cac604ce95452ca0bb363eeefdacfd9966197` | 2026-06-07 | openJiuwen-bot | fix：1.下掉agents-完成超时时间配置；2.保存后删除agents/team本地缓存数据 |
| 73 | `a35d7488bf004867635f574a73a0f39d1cb60851` | 2026-06-07 | openJiuwen-bot | fix：刚加载时不触发agents配置校验 |
| 74 | `137a641add21e9668d29bb86cc62689deb1983db` | 2026-06-08 | xiaotian077 | fix(model): make alias string to avoid transforming to bool |
| 75 | `33d3ee5d8337db7ea2ab66dca194ed4cf9d7369b` | 2026-06-08 | tangfei0306 | fix:add default value for role_type |
| 76 | `46c1f4c497a303180b21c3463bd6a9db722a497f` | 2026-06-08 | wncbdj | fix(heartbeat): Optimize heartbeat frontend display |
| 77 | `48e996d2fee96b9f67868ac420ac7341e3664765` | 2026-06-08 | chensheng12345 | fix(TUI): Fix swarm workflow phase selection error |
| 78 | `5b0b773bd55560d9be782b06696a8bf2931f3673` | 2026-06-08 | ray_le | fix(auto_harness): extend pipeline -> auto package to web |
| 79 | `8913b1212b57c210a109922f22e8d82197e9dd38` | 2026-06-08 | openJiuwen-bot | fix:/export支持绝对路径 |
| 80 | `8f3686b45f7fe19d6dd8959e651c7bd811818cab` | 2026-06-08 | openJiuwen-bot | fix:/model超时错误 |
| 81 | `a11192f9849e154ed0fc4b5ad24b1017d3bde961` | 2026-06-08 | openJiuwen-bot | fix:/cron一次性任务过期未触发后显示 |
| 82 | `a4609fa8d2953162f9d2e4b2b043b3a4a4e6bec8` | 2026-06-08 | chensheng12345 | fix(TUI): Improve swarm workflows TUI copy display |
| 83 | `af1544870833b54fc62de80efa9984fa371cf8ed` | 2026-06-08 | openJiuwen-bot | fix:/memory绝对路径展示不完全 |
| 84 | `b1a5250024c206965ebfdf9138787ad82340bd0a` | 2026-06-08 | ldstyle8 | fix(diff): fix --detail has no effect and add docs for /agents & /hooks |
| 85 | `c2840818d3f87d14cbdc4e8cf65cc5ab029be89c` | 2026-06-08 | xiaotian077 | fix(session): recycle one-shot (heartbeat/cron) session processors to fix memory leak |
| 86 | `d05c9fca1b669084c6a38f38dc3373a803e02a61` | 2026-06-08 | ldstyle8 | fix(rewind): display diff info for rewind conversation |
| 87 | `f4cddf092248dd3e3a1829774a067ebe6521384d` | 2026-06-08 | openJiuwen-bot | fix:/status config支持搜索 |
| 88 | `faee36405c44b41070a10457095d22a7ef924174` | 2026-06-08 | openJiuwen-bot | fix:/status config中修改了model名，但是/status不更新 |
| 89 | `14e5f2d90f7574cd03a8ab6ecdd59a65a08c399f` | 2026-06-09 | openJiuwen-bot | fix：1.预置成员删除后，保存按钮依旧置灰问题；2.删除agents/team配置后保存恢复问题优化；3.下掉技能token明文可以看到的问题 |
| 90 | `161939af4cebe927ad9d08518d00730b599a4cee` | 2026-06-09 | z2tng | fix(swarmskill-creator): swarmskill-creator new version for swarmflow |
| 91 | `21fa3c85bd87d47bf1e47292d4d73a9becb491c6` | 2026-06-09 | MengHan18 | fix(PromptAttachment): adapt to PromptAttachmentManager and migrate dynamic prompt context out of system prompt |
| 92 | `25641c2e5ca2cd33eb42119838f1ceb1e3e2f3d5` | 2026-06-09 | min_gitcode | fix(swarm): skip stream cancel for team chat-send and emit processing_done for follow-up |
| 93 | `283ae94663dde8d4313b2494733a20f008b1420e` | 2026-06-09 | zhaozixuan | fix: support internet connection on isolated network mode |
| 94 | `40ee1563aedeaf7a5d4368fd37cf9d2ab1583d17` | 2026-06-09 | jack_ricky | fix: keep A2UI web-only while preserving web prompts |
| 95 | `5e89f15effdc1800b51b8d59480597d14a58e260` | 2026-06-09 | ray_le | fix(auto-harness): tui修复logs补全及error显示问题 |
| 96 | `63d22fc222c81757dfebb51abf6bb2988d47a84f` | 2026-06-09 | xiaoyao42 | fix：去掉外链字体加载，统一字体设置 |
| 97 | `6bdc0cfeb9af9902278f3db9a96be72d58a3ecb6` | 2026-06-09 | xiaotian077 | fix(session): recycle one-shot (heartbeat/cron) session processors to fix memory leak |
| 98 | `774cfd534f7d8c3682a3c1f5d0c6c56c1f509e2b` | 2026-06-09 | openJiuwen-bot | fix: 修改 JiuwenClaw 为 JiuwenSwarm |
| 99 | `996cf61ba69896d36da2278a84ea0cd9564eb69c` | 2026-06-09 | wangxiaolong100 | fix(session): multi-session status optimize. |
| 100 | `a4a4e625ed182de11b0d87cd48f7378795f77639` | 2026-06-09 | z2tng | fix(evolution): block evolve commands in unsupported modes and on non-swarm skills |
| 101 | `a5280ad421f7691b6e9d36d2b9a30950bf58dbf5` | 2026-06-09 | openJiuwen-bot | fix(web-frontend): Issue where predefined members do not display upon loading |
| 102 | `aa553f3eb9f59cb4eeb9472d4a47001a1513459b` | 2026-06-09 | openJiuwen-bot | fix：移除技能-禁用按钮的悬浮文字 |
| 103 | `bffae994de15ea617cc1942bff30100ae2fc6646` | 2026-06-09 | xiaotian077 | fix(skill): resolve skills.get not finding skill when SKILL.md lacks frontmatter |
| 104 | `cddd2324f66adb05c5527f09a106495410ebdedc` | 2026-06-09 | cycong_1 | fix: client_provider placeholder not materialized on first model save |
| 105 | `d557c976c1285500bcecb99a0737ea25bea283cf` | 2026-06-09 | jingjinduo | fix(TUI): When running /compact, pressing Esc, the process cannot be interrupted |
| 106 | `e89a51df9f7a38c7b7d4b1e00f5932e7f411407f` | 2026-06-09 | ldstyle8 | fix(resume): add session_id info, fix error of switch sessions |
| 107 | `e95a6db93ae7cad169ff580402c01d93801c1483` | 2026-06-09 | huanggawa | fix: support switch same name model |
| 108 | `ead899a14b0409971e9717e539fa608555666bd8` | 2026-06-09 | haowenzhong1 | [fix] /resume add title substring matching and typeahead completion aligning with CC frontend design refs:#1186 |
| 109 | `f2b59f2fc912e333cfb0d740a12581cfbc32507a` | 2026-06-09 | jack_ricky | fix: disable A2UI by default and reject nested templates |
| 110 | `f70ade07065fe59af710e4367f2288b2085f8ef9` | 2026-06-09 | haowenzhong1 | [fix] 修复linux系统无法使用鼠标选中文本 |
| 111 | `6623b335d381f94e1ecc2d64602591737f340244` | 2026-06-10 | openJiuwen-bot | fix:/cron add和update 字段校验 |
| 112 | `80e25b363df178bcf37a2cc266bee2c959e1f239` | 2026-06-10 | tangfei0306 | fix(agent_team): llm can change successfully in team mode |
| 113 | `d1be6aa49d67a326fab3f4e64e41fe8f23bed891` | 2026-06-10 | openJiuwen-bot | fix(memory): 修复 /memory 列表显示"1 lines"的单复数问题 |
| 114 | `f26e89b69a0b0a21e85ac6c3b4e5e04941e91d6d` | 2026-06-10 | wncbdj | fix(prompt): Optimize agent compliance |
| 115 | `29ea72f7012a5c2d0215b8d519ad45370af3254c` | 2026-06-11 | cycong_1 | fix: store raw user input instead of wrapped supplement prompt in session history |
| 116 | `2a61cc8f83711fd22c1d2eb0e06e2b040bd775f1` | 2026-06-11 | MengHan18 | fix(PromptAttachment): align prompt attachment loader with session sections |
| 117 | `4388a6f0d7c3c5a54cd5c0cf98c7a9a7d35bf880` | 2026-06-11 | xiaotian077 | fix(skill): register unmanaged skills as local skill. |
| 118 | `56c62882ad09125f4f52d31a2862781c54ca39db` | 2026-06-11 | openJiuwen-bot | fix:新建文件夹中/memory检查不到JIUWENSWARM.md |
| 119 | `5f756c0f9dc2bff58e5a0003da7548075dc63252` | 2026-06-11 | chenchunzhou | fix(swarmflow): optimize workflow state refresh and session stop fallback |
| 120 | `8c6ebfbf431c78ef27679d06f4e900b591c3863d` | 2026-06-11 | lin-xiaoyu | fix(symphony): whl包增加symphony内置插件 |
| 121 | `91e57548c1372b4afdb9b24a0f6c3c1c7dc403d5` | 2026-06-11 | cycong_1 | fix: reset history loading when sending a message to fix stuck stop button |
| 122 | `91e66f7debac33efabb125e7955d51a15e4afbc3` | 2026-06-11 | openJiuwen-bot | fix:创建多个同时触发的/cron任务后退出直接删除cron任务持久化文件，清空定时任务列表 |
| 123 | `daf8bec4183461ce7c2a0c031f3c8ead431ec781` | 2026-06-11 | openJiuwen-bot | fix:/export ../abc导出到上级目录 |
| 124 | `18478dd098ada1ab4393087fd40f4b73648c3ac2` | 2026-06-12 | MengHan18 | fix(PromptAttachment): split prompt attachment sections by stability |
| 125 | `26fa6f656ed03280a32c16fa9de648cd42533663` | 2026-06-12 | jack_ricky | fix(a2ui): enable prompts in team web mode |
| 126 | `287ca1cc888edba6c7e67bbca162ef26cbf496cb` | 2026-06-12 | zhaozixuan | fix: /sandbox delete permission parameter |
| 127 | `2de7d57c524ddd602488bffe9d2fc279a74c8b23` | 2026-06-12 | kdgmpw | fix(TUI): Fix the TUI frontend's null value issue when handling workflow data |
| 128 | `473302fdb027c053236b0296b01109140acca099` | 2026-06-12 | zhengkailan | fix(MultiModelSection): enhance model validation with unique keys |
| 129 | `47f78ad19759417083b78d82e012593c2a68df10` | 2026-06-12 | Haowei_Du | fix(security): /permission command supports CRUD for tool rule and parameter rule |
| 130 | `48892055290a610fb86aadd2b19a4f15097e07e3` | 2026-06-12 | openJiuwen-bot | fix：1.修改team配置图标；2.心跳间隔提示修改;3.技能-加载中优化展示 |
| 131 | `5e05461a59df069f871fbab6faf3474a95cc5c96` | 2026-06-12 | z2tng | fix(swarmskill-creator): fix the problems caused by complex f-strings |
| 132 | `6cddcca9bc80a9af84748e9e31b6791e90bc1fca` | 2026-06-12 | ldstyle8 | fix: Interrupt task, prohibit zombie output |
| 133 | `92e4359337fc95b7a3ae976cb872295331f8b45f` | 2026-06-12 | openJiuwen-bot | fix:前端svg图标属性修改 |
| 134 | `979197e34f13907b8cfc59186661a4ba349f5a83` | 2026-06-12 | openJiuwen-bot | fix(tui): Show ellipsis when the content of /rewind is truncated |
| 135 | `a2f6a98d2e8ae991dfe228f9f45d52e82401b16d` | 2026-06-12 | chensheng12345 | fix(tui): remove unnecessary summary |
| 136 | `abe7aaee4595d4cfaea57b9d4d845c57d704c20e` | 2026-06-12 | openJiuwen-bot | fix：1.删除多agents和team配置的缓存；2.切换技能/我的技能页签调用list接口 |
| 137 | `b684bdac801725af73b7889e18cab461a95ce94d` | 2026-06-12 | haowenzhong1 | [fix] add "Summarize from here" / "Summarize up to here" partial conversation compaction refs: #540 |
| 138 | `c6f2ab05cf683df5a39a2d6c87c81fcb8086b841` | 2026-06-12 | openJiuwen-bot | fix：1.修改agents配置图标；2.team配置-团队名称长度限制32；3.去除成员名称的前后空格 |
| 139 | `e2901630df7b82b44d9ffaa367e6a1635affce6c` | 2026-06-12 | lin-xiaoyu | fix(symphony): Fix the issue of floating cards generated during orchestration |
| 140 | `e2d05038f92ea66060f263ef3411707cf5b4009a` | 2026-06-12 | xiaotian077 | fix(skill): skillnet skill search bug fix |
| 141 | `f746e569a87daff9af5e2874508dd86038a6c4fc` | 2026-06-12 | openJiuwen-bot | fix(tui): Fix the issue where pasted text containing file paths gets truncated |
| 142 | `f96e5391dc43e4792fc06f24ea2e029411100c58` | 2026-06-12 | xiaoyao42 | fix：修复技能页面安装按钮样式 |
| 143 | `204887fb32131f282cdcc73cb548d8e68efaae1c` | 2026-06-13 | ldstyle8 | fix: change the review info when exit plam mode |
| 144 | `2d3cab9ca3b814c3934981e7ab795158efd39f9e` | 2026-06-13 | openJiuwen-bot | fix(tui):修复vscode终端/rename 后，tab 标题没有修改 |
| 145 | `508188be9665890c7808b26b3241c6e65c1097a1` | 2026-06-13 | openJiuwen-bot | fix：清理agents配置-技能下拉框中已选中的卸载的技能 |
| 146 | `53903610f15b5c271150f4da7f61c32bd3dce938` | 2026-06-13 | openJiuwen-bot | fix：1.clawHub搜索显示slug字段；2下掉源管理clawHub显示/隐藏明文按钮 |
| 147 | `59d3ab294e2137dcfa6879dfc0ce4e0aaf71af46` | 2026-06-13 | kdgmpw | fix(tui): /resume adds shortcuts and functions like Ctrl+A, Ctrl+B, Space, Ctrl+R, etc. |
| 148 | `679920df41c39117414f24aa60b70f2acca4aa68` | 2026-06-13 | kdgmpw | fix(TUI): Fix null value issues in TUI frontend |
| 149 | `6835f69da3319c5af441df6ff66a7d56d2099601` | 2026-06-13 | chenchunzhou | fix(TUI): refine request logic and add workflow time metrics |
| 150 | `8ca6cc4a9813f65c29d1019caf2cfea1f7eda12a` | 2026-06-13 | yangzequ | fix(rail): fix miss match of tool messages and toolcalls in assistant messages |
| 151 | `90f5add79c9c676af029704cc3800d8fc7338248` | 2026-06-13 | yan_zj | fix(swarm):Optimize the dynamic memory content concatenated into the system prompt |
| 152 | `ada4c82e2de228ac5897355efdecf59184739ac6` | 2026-06-13 | xutuo | fix(symphony): Optimise the skill retrieval toolchain |
| 153 | `c359d07cbaac1cf5db6e9437c2f87a44847c801d` | 2026-06-13 | openJiuwen-bot | fix(tui): 启动时当解析config.json 报错时展示信息 |
| 154 | `da0948a9276aa04b817ac7ec0180e6d73369749d` | 2026-06-13 | xiaoyao42 | fix：调整team成员右侧面板样式以及mermaid样式；加上chat.final兜底作为停止条件 |
| 155 | `f09fde22be55260324be87daf0a7e0a7a6cf4480` | 2026-06-13 | openJiuwen-bot | fix：导入本地技能失败错误提示改为显示8秒 |
| 156 | `f2025b2a7063392a01e312efc3c2392ce37f2c7a` | 2026-06-13 | zhanglongmin | fix(agent-teams): add team permission rail and policy rail with leader-mediated ASK resolution |
| 157 | `005e42f500680d9b44ea20158793286521c56a3a` | 2026-06-15 | openJiuwen-bot | fix(web-frontend): Front-end feature changes |
| 158 | `015f2a3b575f98649b400bbe751fcc214fe7bb92` | 2026-06-15 | openJiuwen-bot | fix(jiuwenswarm): Rename jiuwenclaw to jiuwenswarm in the newly added code |
| 159 | `14215a1c6142528eee337177902f5508f834ba3e` | 2026-06-15 | haowenzhong1 | fix(tui): Add periodic background cleanup for sessions and snapshots, and disable mouse tracking after rewind |
| 160 | `23a5c6770f22c3b5476f3b5ccc3e30c98ed1b2ca` | 2026-06-15 | openJiuwen-bot | fix:/statusline支持agent自动写脚本并生效 |
| 161 | `256742edb2fc5421ecfb27ff174b14013bfc32db` | 2026-06-15 | chensheng12345 | fix(TUI): Detect offline network during TUI streams |
| 162 | `295371f6bb43e6161c125ecc3dff36ea99a114dd` | 2026-06-15 | ray_le | fix(auto-harness): /auto-harness issue指令文档补充 & Tui 显示修复调整 |
| 163 | `3126805eb94009ff63e7a74c628e1f6866f6fd3d` | 2026-06-15 | ICEORY | fix(circuit_breaker): Fix missed detection of ping-pong stall and incorrect result parsing for Windows tools |
| 164 | `4c938ef557173c65b50dc605d4910f2d021c0aa4` | 2026-06-15 | xutuo | fix(symphony): Fix the inconsistency of skill indexes between the front end and the back end |
| 165 | `60a88dc5e8da16cc59e520682ecc5ece140958f1` | 2026-06-15 | z2tng | fix(a2ui): resolve A2UI rendering and interaction issues |
| 166 | `6593e0222ad0d491b7315cae705fe737f250cfc7` | 2026-06-15 | openJiuwen-bot | fix:vscode终端中/status config 支持英文搜索 |
| 167 | `75587e63bcb7e45c76fae6e98f9f2d181fec0690` | 2026-06-15 | xiaoxiawang | fix: 修复AgentTeam.md 文档检查问题 |
| 168 | `8011940fec81c314b7ad529cf4e4bfd9403007a7` | 2026-06-15 | openJiuwen-bot | fix(tui): 修复jiuwenswarm.md 过大后，告警字符过长报错 |
| 169 | `82a17d3d32283533bf5128fcc6b77fd0b0fdcf16` | 2026-06-15 | chensheng12345 | fix: Style TUI todo statuses |
| 170 | `8e59b8ed081631f9a0ee42a6d00ae856f56bb34c` | 2026-06-15 | xiaoyao42 | fix：优化滚动条样式 |
| 171 | `ab2f1b93aa50db6b0576b1ff2d57f9d2ddc2960d` | 2026-06-15 | openJiuwen-bot | fix(web-frontend): SwarmSkills online search displays the 'name' field |
| 172 | `ad4010a63cb68fcff044366c7e85ffccddeba1a8` | 2026-06-15 | cycong_1 | fix(session): restore parallel session startup in performance mode by removing the erroneous MessageHandler init check and resolving the summary agent by channel |
| 173 | `b1e076967ad5d15b0f87aa7af22ae3e982a02aaf` | 2026-06-15 | z2tng | fix(evolution): adapt skill evolution commands and triggers |
| 174 | `b78c14567e231e18158bc391534727ec83eca13b` | 2026-06-15 | xiaoxiawang | fix: 修复配置信息文档 |
| 175 | `c776c29b6924d4e8cb00b654a643bc7c1c853b1a` | 2026-06-15 | huanggawa | fix: empty item when resume |
| 176 | `cf15d5d93da0397ae4508cdafc0747bc61fd1464` | 2026-06-15 | chensheng12345 | Fix:swarmflow phase selection icon |
| 177 | `cf289e471a89c361236d778d4b7879b0bc5105c7` | 2026-06-15 | openJiuwen-bot | fix:/cron建立多个定时任务后删除cron_jobs.json，定时任务取消 |
| 178 | `d2499d780edcd919f820fbcac96bebf5d9972d5b` | 2026-06-15 | ray_le | fix(auto-harness): auto-harness 特性文档 & tui 子指令提示 |
| 179 | `d42b14297bc48fb724258b061521a9caaa00491b` | 2026-06-15 | layShang264 | 修复 Symphony 总谱构建取消后重试时的孤立 ToolMessage 上下文异常；进入技能总谱即触发适配视图 |
| 180 | `d8da27b2c9dbf797cd7dda19c28d5f2f40a16cf0` | 2026-06-15 | lin-xiaoyu | fix(symphony): Fixed a compatibility issue caused by changes to the default extension_dirs |
| 181 | `d8e1e0537aed269969b39927409a85f64b15ae63` | 2026-06-15 | openJiuwen-bot | fix(TUI): Display the full absolute path of the md file in the /memory parent directory |
| 182 | `f5be96788953a918673f7e9aefcb73f7f99c3f73` | 2026-06-15 | xiaoxiawang | fix: 修复频道文档的易用性问题 |
| 183 | `13244bb87f170116f8a98861459c0c6bbf14ec6c` | 2026-06-16 | haowenzhong1 | [fix](tui) Add quit altName to TUI slash command autocomplete suggestions Refs:#1557 |
| 184 | `248462eee56670dcaf89ea31fe201c3093460cac` | 2026-06-16 | xutuo | fix(symphony): 修复前后端一致性问题 & 优化技能树的前端展示效果 |
| 185 | `2e0397dd80d0656fc490159ad9cfc6ab221c99fa` | 2026-06-16 | z2tng | fix(evolution): harden active evolution slash flow |
| 186 | `3b349ba868c7436f2c2dbf6e5d860e339d181445` | 2026-06-16 | lin-xiaoyu | fix(symphony)：修复图谱构建暂停或更新进度时，时间重置的问题 |
| 187 | `4404acf763c8e9623958f4cc59c79e0eeee0a085` | 2026-06-16 | haowenzhong1 | [fix](tui) Interactive diff dialog with file list and scrolling detail view, remove static detail view Refs:#1554 |
| 188 | `4800a9a901ae43a68351233c35b364a806eb146e` | 2026-06-16 | kdgmpw | fix(tui): Show past conversations when previewing with the /resume command |
| 189 | `53a2961aa97041bd10f28ea3a86eb087f0c4ab18` | 2026-06-16 | ldstyle8 | fix: await cancelled tasks to prevent zombie calls after Ctrl+C |
| 190 | `723c2ac86355f9c67b7204a2dd025c94948ed126` | 2026-06-16 | xutuo | fix(symphony): 撤销config.py中的修改 |
| 191 | `7b34a44f5f4bf8bfb3212846df94a4c67161cc13` | 2026-06-16 | ray_le | fix(auto-harness): tui auto-harness 去除 issue 指令提示补全 |
| 192 | `af6d700d782be28a292d2d08bd7ae07f0448f434` | 2026-06-16 | lamsaan_yau | fix(harness): doc for harness |
| 193 | `b58d4da5ef4d955d52a2867d0ca116df3b7cdcb0` | 2026-06-16 | layShang264 | fix: 修复模型配置不可用时， Symphony 总谱构建失败或暂停后前端仍显示刷新中的问题 |
| 194 | `bf3c9b747b375eed1c7ff156c841fe260c56e882` | 2026-06-16 | ldstyle8 | fix(tui): temporarily disable /plan and /diff, expose /permissions and /recap |
| 195 | `bf864b4759b945b9588b3a80629924723ea9b3ad` | 2026-06-16 | chensheng12345 | Fix TUI workflow log accumulation |
| 196 | `c8f08bd21b0c16cd7895423fcab5c11676caf7af` | 2026-06-16 | min_gitcode | fix(agent-teams): improve team stream failure events |
| 197 | `cda164066ba4edf11ac2ca53a730a308b4aafff8` | 2026-06-16 | lin-xiaoyu | fix(symphony): 修复点击技能管理刷新按钮，技能总谱无法刷新的问题 |
| 198 | `d6bb19faf3111f08f4096be942bfbfe125aa275b` | 2026-06-16 | haowenzhong1 | fix (tui): Add readdirSync fallback for TUI @ file autocomplete Refs:#1510 |
| 199 | `ddd4440844580d29a43ecb84b144977431435958` | 2026-06-16 | weichenhao | fix(team): Default write position for coding_memory_write has been changed |
| 200 | `e4d867fcb5cf4df066d873d3fb43693e6e8a057d` | 2026-06-16 | lin-xiaoyu | fix(symphony): 技能总谱增加英文翻译 |
| 201 | `f812e5190fbb9e159db777c6e6a3a54784c444c7` | 2026-06-16 | ray_le | fix(web): 支持 TUI 会话前缀并改进时间戳解析，修复 #1479 |
| 202 | `f9a9cab61d7da41eadcf0495b52e5518a824e076` | 2026-06-16 | kdgmpw | fix(tui): Filter the data needed for the resume command on the backend to prevent None from being sent to the TUI frontend |
| 203 | `150451a8349d00d027b25936775c0bb079a44988` | 2026-06-17 | yangzequ | fix(deepagent): isolate DeepAgent state per session without rewriting adapter internals |
| 204 | `4cfec60f60ba0f02f5d92d85d4b35998676fb771` | 2026-06-17 | openJiuwen-bot | fix(team):Team plan mode reuse the code.plan approval interaction link |
| 205 | `567ecb719076e969d060832fec1d9c2996fa13dc` | 2026-06-17 | openJiuwen-bot | fix(code-adapter): completion_timeout 改为从配置读取 |
| 206 | `6e220c6dfe69a894864407b24c2c964a1b8f9404` | 2026-06-17 | zhengkailan | fix(pasted-text): implement functions for handling pasted text and integrate into AppScreen |
| 207 | `7ea29c236580ef7e794ebc5991b1b40eab9a7a5d` | 2026-06-17 | xiaoyao42 | fix：解决history序列化问题 |
| 208 | `8e7b540f30ce69bf70c9899af33c8d09e0ed7ade` | 2026-06-17 | openJiuwen-bot | fix(tui)：tui-/theme light导致字体颜色看不清 |
| 209 | `a4716ac3d68428efacb56d3afc2be3eec53db375` | 2026-06-17 | m056838700 | fix (audio tools): fix hot reloading issues for audio tools based on configs |
| 210 | `b33d50c25385b52613c73bbab3d374278df47f2a` | 2026-06-17 | ldstyle8 | fix(tui): hide slash command aliases from suggestions |
| 211 | `b7520acd12877bdad07ac8fdf27d2ec8c5e0c874` | 2026-06-17 | openJiuwen-bot | fix:/memory显示相对路径，加载rule目录下的md规则文件，git目录提示'Checked in at' |
| 212 | `b9b91cd04dd7f99101fc73703a624ecfac013b65` | 2026-06-17 | cycong_1 | fix: store raw user input instead of wrapped supplement prompt in session history |
| 213 | `bc4e63ebf42f95927647ac32eb6f6537295980c7` | 2026-06-17 | zhengkailan | fix(context): enhance token count formatting in context usage display |
| 214 | `df871d97bc892195b0d81182097c9a74af00767e` | 2026-06-17 | jingjinduo | fix(coding memory): auto memory |
| 215 | `e12d1952ce23213124b7c66a64306d5964dd11ef` | 2026-06-17 | openJiuwen-bot | fix(web-frontend): 下掉定时任务-推送频道下拉框中的企业微信/微信 |
| 216 | `f538993e22ad5bc163b9a2a5451f9c3cf89c817b` | 2026-06-17 | openJiuwen-bot | fix(web-frontend)：team配置删除-保存后恢复问题 |
| 217 | `f64b509be2f1b146e6cdcf208863c92ee1d96df1` | 2026-06-17 | chensheng12345 | fix(swarmflow):Preserve swarmflow workflow selection |
| 218 | `07f6abcfa93bca110b37a82fb9ecfb6545a27356` | 2026-06-18 | zhanglongmin | fix(agent-teams): switch openjiuwen dependency to git source (develop branch) |
| 219 | `0aaf7f26de6adc0282f2ab2f8873366c0ff60587` | 2026-06-18 | zhanglongmin | fix(agent-teams): filter non-leader __interaction__ from frontend stream |
| 220 | `14a0ed700499b2694bf75bcc6ac0a5b1c32717ab` | 2026-06-18 | zhanglongmin | fix(tests): adapt tests for PERMISSION_INTERRUPT exclusion from code-profile rails |
| 221 | `61ebe74d775ab99e8ca0be3f31b893e90a442ead` | 2026-06-18 | ldstyle8 | fix(tui): recover /plan and /diff commands |
| 222 | `6f7c855bc1fb2dce38d2df72e4ef5d255aea437c` | 2026-06-18 | openJiuwen-bot | fix(team): restore post-round request routing |
| 223 | `7e8f47030178eabf8b1e987d43b220157e530637` | 2026-06-18 | ray_le | fix(auto-harness): expert pipeline执行完成 web 自动更新 |
| 224 | `cd21caba3a240159dd66ad22076f10e7874e8bd1` | 2026-06-18 | openJiuwen-bot | fix:1.◯ disabled->o disabled;2.channel上架微信（定时任务也可选） |
| 225 | `0648b1fa4a5838ac242e2e5a3a9b4ac5a4f9b360` | 2026-06-22 | yangzequ | fix(session): make history jsonl durable, compatible, and reversible |
| 226 | `07e36593c31f3488d2ac5008ef6c1657d053f09f` | 2026-06-22 | xiaotian077 | fix(deps): 为 httpx 启用 socks extra，修复 SOCKS 代理环境下网络请求失败 |
| 227 | `09cd90e012f7873a9df2957e6a22d5a67e469198` | 2026-06-22 | ldstyle8 | fix(mode): re-enable /mode command and always show mode in status bar |
| 228 | `2a17e2c23da21d96f241db0948f9b7cb29261677` | 2026-06-22 | ICEORY | fix(Rails): 修复断路器历史队列长度边界条件问题 |
| 229 | `40ce444acc22318a673016b04873a0036afa5d48` | 2026-06-22 | xiaotian077 | fix(mcp): 添加 reload 串行锁防止并发叠加 OOM + 配置无变化跳过无效 reload |
| 230 | `456a17ca6ed2cf480a030f6e73ca8a8980b358a2` | 2026-06-22 | tangfei0306 | fix(agent-teams): reversed is_processing flicker on cluster-mode follow-up requests |
| 231 | `6a46e0e1f9e77fe71582821a775fafd6b026d9cb` | 2026-06-22 | openJiuwen-bot | fix：技能-源管理存储在线搜索状态 |
| 232 | `75510476e57032310ce034911f2e58b559492ab5` | 2026-06-22 | kdgmpw | fix(tui): resume预览session时支持翻页 |
| 233 | `75804fde5ba7f57aac0bf341773c9b53f26d116b` | 2026-06-22 | min_gitcode | fix(agent-teams): forward non-leader member outputs |
| 234 | `82bdabb0818d15180d893bf78dff32933a13bc5c` | 2026-06-22 | zhaozixuan | fix: disable user namespace isolation in code agent |
| 235 | `8a195bfb9cc9fb33f5a6e5904494bafeaa4af3d2` | 2026-06-22 | min_gitcode | fix(agent-teams): expose enable_permissions toggle in team config panel |
| 236 | `a4616cb307b15bd8deb3ba8abc23d78fa4797f37` | 2026-06-22 | kdgmpw | fix(tui): /keybindings中防护快捷键冲突 |
| 237 | `b6c519a0b205e682f2c03bc5649a9273dadc557f` | 2026-06-22 | openJiuwen-bot | fix:/memory两个窗口不一致 |
| 238 | `bc56fd2f9d1d3224e7be54478b2edbc5d8bf9f44` | 2026-06-22 | openJiuwen-bot | fix(team): resolve project workspace prompt in code.team |
| 239 | `d256552627d57face4a9f32d084c8f1faa8824f2` | 2026-06-22 | openJiuwen-bot | bugfix(toolrail):fix tool rule removal not effective |
| 240 | `d96e693ec50a07d8242ee44b32855d911cd36f13` | 2026-06-22 | xutuo | fix(Symphony): 清理Symphony中未使用的文件、配置等冗余代码 |
| 241 | `fee2301ca1a8c3e89f90f86385b042d479a41ff9` | 2026-06-22 | kdgmpw | fix(tui): 定时任务支持 team 模式及 SwarmFlow |
| 242 | `0402bfa0053b03ed2a59baff43eb394d52a68946` | 2026-06-23 | ray_le | fix(auto-harness): 热生效 activate 广播到 session 级 DeepAgent & web 中英文提示适配 |
| 243 | `052f287c381fe666a1c8ac066956b08a1c5d129f` | 2026-06-23 | jack_ricky | fix(a2ui): stabilize interactions and text rendering |
| 244 | `230d5995aae16c4d85cddcae228a476fff93b557` | 2026-06-23 | kdgmpw | fix(tui): help支持esc退出 |
| 245 | `69907263483be9932708f9c16b2c72f5f3176f4c` | 2026-06-23 | xiaotian077 | fix(skill-install): 同名技能覆盖安装前弹窗确认 |
| 246 | `70077f1908cb4176ae19ec115390b41a4da195fe` | 2026-06-23 | openJiuwen-bot | fix(code): anchor worktree workspace to project dir |
| 247 | `7b0082db6c02b44de80756017f0db676d65cce63` | 2026-06-23 | kdgmpw | fix(tui): resume预览功能适配jsonl的session记录 |
| 248 | `9a409a2915c5f847a5513a5df4f7c0aee394923c` | 2026-06-23 | openJiuwen-bot | fix(team): mount config MCP servers |
| 249 | `aab6e33539717af424da721bda8632e29b8b485b` | 2026-06-23 | hwxiajing | fix(observability): modify Langfuse password to jiuwenswarm |
| 250 | `c68f2097d51c14ae2ca6ba288b68bbd331e26f93` | 2026-06-23 | openJiuwen-bot | fix:/model中添加同名model，并切换时，model后面的序号区分 |
| 251 | `c6962044e2badbc77fdd8e05a78f24b83feb591e` | 2026-06-23 | zhaozixuan | fix: add fallback_on_failure for sandbox |
| 252 | `e04d44acfe2eda0b7bde3373480d06f81f9fb2cf` | 2026-06-23 | jingjinduo | fix(tui): 修复auto memory报错 |
| 253 | `f09555db73b8955f5b7aa9384d3b0e71974b208c` | 2026-06-23 | openJiuwen-bot | fix:/cron建立多个定时任务后删除cron_jobs.json，后端停止运行 |
| 254 | `14758d3275e62d178f4277f8813636d7cb7976b4` | 2026-06-24 | jingjinduo | fix(tui): 无消息时不自动recap |
| 255 | `53dfd881510afd90b5d459b93e97a95acd405a38` | 2026-06-24 | openJiuwen-bot | bugfix(toolrail):fix tool rule removal not effective |
| 256 | `af36c629435ec4adaa3d71f417d8618429d1cf7c` | 2026-06-24 | ldstyle8 | fix(task_todo): add task reminder into prompt |
| 257 | `c86a2183687f21b304516d626ff51f28e72b2f1b` | 2026-06-24 | ldstyle8 | fix: session not found after /branch |
| 258 | `397b6c89d9001510644fe9d1b0400ccd58346014` | 2026-06-25 | openJiuwen-bot | fix(team): default swarm worktree members to project workspace |
| 259 | `3a78f36cdf7a2d7c456525498977225ea5940e9f` | 2026-06-25 | z2tng | fix(evolution): stabilize skill self-evolution approval flow |
| 260 | `72d034fc8d39a05f987b707ee8aa43a604f106b0` | 2026-06-25 | m056838700 | fix(A2UI, browser subagent): add A2UI to browser subagent, improve performance for A2UI and browser use |
| 261 | `96c07097b554db277a530c94c9897103be096413` | 2026-06-25 | xutuo | fix(symphony): Fixed an issue where switching models required the index to be rebuilt |
| 262 | `97ce2fda40de3066dd82b80d6ffc4b770a676e49` | 2026-06-25 | iamcandiceguo | fix(team): drain request_queue after has_stream_task loop to prevent team.completed event loss |
| 263 | `99b9672381e2f9cbf53ccdcd35d6405395468ba8` | 2026-06-25 | ldstyle8 | fix(btw): overlay UI isolation and session-scoped context lookup |
| 264 | `a95a21cc6a1ae30fa304b5d265bfeeaf36f90d06` | 2026-06-25 | openJiuwen-bot | fix(TUI): Change the default theme colour for tui to dark |
| 265 | `b38150edf920ac1484dac93d0588bded4bc52ad3` | 2026-06-25 | zhaozixuan | fix: restart sandbox when error occured |
| 266 | `e9d1560a3ca99c12d7163dce53132e10579521ff` | 2026-06-25 | hwxiajing | fix(observability): correct LANGFUSE_INIT_USER_NAME from jiuwensarm to jiuwenswarm |
| 267 | `fe6e4dca3582c50f699ae64a6b615afffa9d25bf` | 2026-06-25 | openJiuwen-bot | fix(web-frontend): Issue with repeated reloading when switching tabs after saving the deletion of default team configurations |
| 268 | `1918ff7489338b97949b2f55cc22e84921661d4e` | 2026-06-26 | kdgmpw | fix(tui): 定时任务结果通知删除cron标签 |
| 269 | `2ca20d2bd0eb8c701e03904398771ec698a1980b` | 2026-06-26 | xiaotian077 | fix(llm): non-stream invoke 兼容 SSE-only 网关 |
| 270 | `71ec753d21870c098d6710b9480d08d24dc22e7b` | 2026-06-26 | haowenzhong1 | [fix] chat CLI Windows signal handling, REPL exit, and total timeout |
| 271 | `a101304c819e62aece11aa44899dd5b9e1e4877f` | 2026-06-26 | ldstyle8 | fix(diff): file info display for /diff command |
| 272 | `bf58e7e2fef21392641857290d5d3366fe1d6b88` | 2026-06-26 | ldstyle8 | fix: tui start failed |
| 273 | `c435f7b89e50275936b6c329737c86934286e5e3` | 2026-06-26 | openJiuwen-bot | fix(cron): Fixed an issue where cron jobs were triggered 5 minutes early |
| 274 | `d36e7467038f7bfa84eee3c3b411c5443334b3b4` | 2026-06-26 | openJiuwen-bot | fix(team): constrain manual git worktree creation to project .worktrees dir |
| 275 | `d91b11fb8404667026eff6ab66dd72545964d9b9` | 2026-06-26 | wangxiaolong100 | fix: CodeCheck清理 |
| 276 | `f313cae99fe896ebe66b3c86906d2f025d3fa50b` | 2026-06-26 | haowenzhong1 | fix(chat): resolve chat CLI team mode premature exit and route through /tui |
| 277 | `00a69edd703b3dcb21db0275246f9ac760d60c8b` | 2026-06-27 | jingjinduo | fix: 修复auto-memory获取消息问题 |
| 278 | `1c97656605ebbf668ba858c8d65eefad5ae87f4e` | 2026-06-27 | haowenzhong1 | fix(chat): chat CLI plan mode interactive question and approval flow |
| 279 | `1d59a62a40d1596071afca159a0c8302b4b93a1e` | 2026-06-27 | openJiuwen-bot | fix:/status中config面板进入编辑后，esc返回两次即可回到原页面 |
| 280 | `28cc6b6d94512838fa686411bcd1dbb7f5b5f119` | 2026-06-27 | zhanglongmin | fix(team): filter teammate reasoning events |
| 281 | `2dd2bd5ffdeae84ba58fa80675615387b8be0d3d` | 2026-06-27 | kdgmpw | fix(tui): Distributed teams do not support multiple TUI windows; add this constraint to the documentation |
| 282 | `4a86c895a02c661ae879cc84e5b6f82367ecb6ea` | 2026-06-27 | openJiuwen-bot | fix(TUI): When the /status panel is opened, it displays thoughts from previous conversations, which disappear after a short while |
| 283 | `5ce9b87a0053041d6292bb9f469088782a5f33f1` | 2026-06-27 | wangxiaolong100 | fix(jiuwenswarm): CodeCheck清理 |
| 284 | `61cb1088a612308d3b3740dd4570a00b08721007` | 2026-06-27 | huanggawa | fix: support http mcp server |
| 285 | `65a7d9df170d655818936e904e37bb6d35b057c7` | 2026-06-27 | ldstyle8 | fix(tui): stabilize plan approval reject flow |
| 286 | `6c5b38048fbfce4f149bcc7924c304ccdd6a5077` | 2026-06-27 | openJiuwen-bot | fix(team): seed project_dir into team member runtime prompt rail |
| 287 | `736a09133dd7f223564561eb0de6fd34b56966b2` | 2026-06-27 | ldstyle8 | fix(TUI): display changed files aligned to git diff |
| 288 | `92ab155fc319eef0576be52e740c468441f37b8f` | 2026-06-27 | xiaotian077 | fix(skill): 修复 teamskillshub.init path 路径穿越漏洞 |
| 289 | `a6f8d6155cecdde975af5053a90bd298c05d3287` | 2026-06-27 | z2tng | fix(evolution): add skill auto approval config toggle |
| 290 | `beeb4064744c3662b5f011df38e095c9ab5c74c8` | 2026-06-27 | openJiuwen-bot | fix(heartbeat): 配置飞书心跳时检查是否有过对话 |
| 291 | `c5a95d5fb703857d081c100c8fa27fd3524e3c10` | 2026-06-27 | zhanglongmin | fix(team): truncate tool result events |
| 292 | `cef62cb1d7853a604acfeaa5fbae3538d88974e0` | 2026-06-27 | zhengkailan | fix(web): avoid blocking config saves on agent reload |
| 293 | `f2591c9c21b1d715c91492b4f0d574aa8df7c5b8` | 2026-06-27 | kdgmpw | fix(tui): Fixed an issue with the keybindings feature |
| 294 | `086bb35048f4d65121db628eb69de22309459f82` | 2026-06-29 | z2tng | fix(evolution): avoid config reads for stream chunks |
| 295 | `247440371cf7f858d08260c47dcf4e3bebd56b01` | 2026-06-29 | openJiuwen-bot | fix:/status中的config面板进入编辑后，esc返回直接回到原本的页面 |
| 296 | `28da7443c2dce78f0801bb94719e7dbef6fc0f88` | 2026-06-29 | kdgmpw | fix(coding): coding memory按照项目隔离 |
| 297 | `2e43c8cd8178ebf61d920485dc96794d4fa4c415` | 2026-06-29 | kdgmpw | fix(tui): 优化定时任务结果推送显示 |
| 298 | `328531f1183072024497ce82af58fcb31b3c158e` | 2026-06-29 | openJiuwen-bot | fix:/memory找不到有git仓库的父目录里面的md文件 |
| 299 | `4257e5cd2eafa28cd464c4a11fb8b5533a7f055e` | 2026-06-29 | ldstyle8 | fix(btw): pass tools schema to preserve cache key, discard tool_use |
| 300 | `6833296959883f581b39b61eb1036b8eb31ee0d2` | 2026-06-29 | wangxiaolong100 | fix: CodeCheck清理 |
| 301 | `795f72021cf87bb69a3ce7046a5701bc22c9c9b7` | 2026-06-29 | tangfei0306 | fix(agent-teams): first request should valid no_existed member |
| 302 | `a788070532ae5215da1d9e16aac482667aaf1cbb` | 2026-06-29 | ICEORY | fix: 修复工具死循环检测断路器pingpong偶发失效 |
| 303 | `b5abd9f14ffb4efc2e028575f23caf9284d2c105` | 2026-06-29 | ldstyle8 | fix: support review in plan mode |
| 304 | `c0c08712788eeb65b44f541a916088bf68cb068a` | 2026-06-29 | ldstyle8 | fix(tui): show untracked files in diff viewer |
| 305 | `c864a274c38b2f1aeb0fe9292bf1e0c34c7c2117` | 2026-06-29 | iamcandiceguo | fix(web): handle Windows WSAEWOULDBLOCK in SPA WebSocket proxy |
| 306 | `cad7b35dcdf1a8dc3ad9e8b69d6ebcd514b76e1d` | 2026-06-29 | hwxiajing | fix(observability): 默认输出到文件，提供手动上传脚本 |
| 307 | `cb2be7df72f0323901b00d6b6de248abebb753bd` | 2026-06-29 | zhengkailan | fix(tui): display token usage for team mode |
| 308 | `db7c44f8bdbf059ff4d46b517775b4e3279ab74a` | 2026-06-29 | lin-xiaoyu | fix(symphony): 修复技能总谱构建过程中报错提示没有适配显示位置 |
| 309 | `e9355681586c2da81cfead4648a13117d589211f` | 2026-06-29 | xutuo | fix(symphony): 修复技能索引显示与集群检索问题 |
| 310 | `026ecf2e8e3e40a56c4aa28ea8455a3ed92c14fb` | 2026-06-30 | ldstyle8 | fix(simplify): exclude security vulnerabilities from /simplify scope, and report-only |
| 311 | `0b892a74e715f5db464b5d6901dbc21ff0889403` | 2026-06-30 | min_gitcode | fix(team): add env switch to filter non-leader frames in team streaming |
| 312 | `370eda48dd3bc39c932fb68c5cf1f0a1fe29e6c1` | 2026-06-30 | openJiuwen-bot | bugfix(toolrail): remember option optimize |
| 313 | `442f47464539cc7022f5e8c2314b6d7b3575705e` | 2026-06-30 | xutuo | fix (symphony): 修复技能索引页面展示的技能名与SKILL.md中不一致的问题 |
| 314 | `679611c4c09ad2555f94dd739e01eea8863494b3` | 2026-06-30 | openJiuwen-bot | fix:/model中添加同名model，用key的尾号4位做区分 |
| 315 | `81d1c71509afb16deb21af1de4fe5135af895dad` | 2026-06-30 | ldstyle8 | fix(security-review): pre-run git and abort on failure to avoid mis-review and branch switching |
| 316 | `8c8576d39d3b21fc78ce2c0a75ebc10050bfff9e` | 2026-06-30 | Haowei_Du | fix(/permissions): fix bug 263 264 280 |
| 317 | `ac090792d268531a3a7a0610b767db8d58debc9f` | 2026-06-30 | haowenzhong1 | fix: avoid duplicate display when final content differs from streamed delta in CLI chat |
| 318 | `acf9cb000099293b8828c3b39a35cdc88a9547d8` | 2026-06-30 | lin-xiaoyu | fix(symphony): 技能编排支持技能禁用 |
| 319 | `dff8865c395c9ded9745c6a215c284f729c766b3` | 2026-06-30 | xutuo | fix(symphony): 构建页面优化&恢复集群模式下Agentic检索开启时，不注入全量skills |
| 320 | `e0d3d34490aafa60f65abdcfb3d98f85d388cd80` | 2026-06-30 | openJiuwen-bot | fix(code): restore ask_user rail in agent.plan |
| 321 | `e1594f3923b4fe21569c2c9b3053a3d238918e23` | 2026-06-30 | alan_cheng | fix(startup): silence gRPC fork noise in tool subprocess output |
| 322 | `e9babd6e4c7e203d83cf64dbd19133d8f25d9ba4` | 2026-06-30 | chensheng12345 | Fix: btw overlay scrolling |
| 323 | `3b6965eb71abc01d1e303a51428b0aca19509aff` | 2026-07-01 | openJiuwen-bot | fix(evolving): add evolve rollback Refs: #1754 |
| 324 | `4c74014e94d39a31fc1ace297dcb493ea2538818` | 2026-07-01 | huanggawa | fix: fix mcp http issue |
| 325 | `531314983a283bfbd305b4c2625f58904fc50276` | 2026-07-01 | xutuo | fix(symphony): 开启Agentic技能检索时，集群模式下不再attach全量skills到user message |
| 326 | `5576d049dfb790c94982f0fb90b3b321da9e0c8d` | 2026-07-01 | xiaoyao42 | fix：team成员增加上下文压缩；修复历史恢复被空闲超时误判为空的问题 |
| 327 | `677c3a02fbb47ee918fab4d6540ecf7e7f4e6d72` | 2026-07-01 | kdgmpw | fix(tui): 多tui窗口resume同一session防护 |
| 328 | `7bc9655349fcc737bd56d8038c9e90ff74204ffb` | 2026-07-01 | cycong_1 | fix: isolate runtime_state.yaml per session |
| 329 | `b1d176ffa5c0bf553e6d95cd02c0de542c079a6d` | 2026-07-01 | min_gitcode | fix(team): bind session_id before creating TeamMonitor to avoid empty-hash table lookup |
| 330 | `b643a92cc0a02fd0ee365150925fe1ba69f1ca67` | 2026-07-01 | zhaozixuan | fix: code agent sandbox diretory bugs |
| 331 | `e50e05851330cc7e2f24c7ccfdb574757c6d6477` | 2026-07-01 | ldstyle8 | fix: handle websocket close during gateway restart |
| 332 | `eb05ed23e026b0e9e52a0c1219defe6f3b3287e5` | 2026-07-01 | ldstyle8 | fix(diff): normalize rename numstat path so hunks align |
| 333 | `245beb332d036b2326c0e3a406917271dad73abb` | 2026-07-02 | ldstyle8 | fix: harden oversized websocket history/snapshot payload budget guards |
| 334 | `344fe24f53b0b9b9d5885726cdc3d08ea9a1a6f2` | 2026-07-02 | openJiuwen-bot | fix:/btw支持摁c复制会话 |
| 335 | `7f7171512fd3c1139fa66c2c9816b7112fc1a5fa` | 2026-07-02 | l_x_d | fix: update round level compressor config |
| 336 | `9a2d785a7fb18270017d08fec04f6f85d8e95df9` | 2026-07-02 | ldstyle8 | fix: improve websocket diagnostics |
| 337 | `a6467634a561c3f0295f6c05ae861e0ee707b3f3` | 2026-07-02 | xiaoyao42 | fix：exe包对话中的分享功能不可用 |
| 338 | `ed3e70cba8fa333964003c295207fb49621bbbbf` | 2026-07-02 | ldstyle8 | fix: harden agent websocket recovery |
| 339 | `211b89b824bc5db953f18744732d858b13c8c5db` | 2026-07-03 | openJiuwen-bot | fix:在 IM（微信） 输入 /skills list，无返回。/skills 可以有返回 |
| 340 | `3603a6059ec4180c4539cd4944456dc9878bcf8e` | 2026-07-03 | xiaoyao42 | fix：undo a646763；修复对话中文件下载文件名路径的安全问题；修复exe、dmg包对话中的分享功能不可用；增加分享下载选择保存路径操作 |
| 341 | `541e5137b6df0d9fa2c9f1c198116b9a5487862c` | 2026-07-03 | z2tng | fix(agent_adapter): handle missing session id in tool refresh |
| 342 | `7b620bef660ee1b5abc5071e38d7a6e52ce82836` | 2026-07-03 | haowenzhong1 | fix(TUI): jiuwenswarm chat CLI garbled characters on Windows and Ctrl+C interrupt handling |
| 343 | `98b19784bb2182fa606e91c8285da9d8b9cb1ba4` | 2026-07-03 | lairuofei | fix(skillnet): 友好化 GitHub 安装报错并修复错误提示 i18n 冻结 |
| 344 | `ab8a80af911d45ebad3516c5a09b664ef5ea2c04` | 2026-07-03 | ldstyle8 | fix: display changed files for diff command |
| 345 | `b46a0a890ebd4ff1faae7d34ac99cc9d561d60ba` | 2026-07-03 | min_gitcode | fix(team): handle team follow-up delivery races |
| 346 | `d228026f1b96ac7cfba6cf5e4ac30edc25c7a9b1` | 2026-07-03 | haowenzhong1 | fix(cli): persist trusted directories and merge into request params |
| 347 | `df735c2e350e26660a2e6b9955f4360496c14b35` | 2026-07-03 | MengHan18 | fix(Prompt Attachment): Move the trusted_dirs_policy section back into the system prompt. |
| 348 | `fa4a4613fd944471568f39e8a7b68ddbf5940d8b` | 2026-07-03 | haowenzhong1 | fix(tui): allow ctrl+o to toggle transcript folding while status/overlay is open |
| 349 | `0f3488a8fb43c917772fccb39b1d5f0d4d5cc9f0` | 2026-07-06 | yuhaiquan | fix:api_base字符长度改为256 |
| 350 | `1023a47bb5098917a7adc7be901eb60a8639ad3d` | 2026-07-06 | haowenzhong1 | fix(tui): align --mode parsing with TUI /mode command by removing unused aliases (fast, normal, team.normal) Refs:#1888 |
| 351 | `5084467df1951fd71a9747d3ed8fd9625de914ac` | 2026-07-06 | kdgmpw | fix(server): skip agent init and code-mode management for stateless skills/plugins/symphony requests |
| 352 | `5673d871dcaa43bb0e6466bba4a91377851a5531` | 2026-07-06 | openJiuwen-bot | fix:/skills安装重复skill后提示覆盖或取消 |
| 353 | `693081bbbcc5a81026010d7bbce7152b65a3c759` | 2026-07-06 | kdgmpw | fix(tui): fix keybindings watcher dropping rename events on Linux |
| 354 | `71e078b091065134f00ce50b370e8183fa805692` | 2026-07-06 | openJiuwen-bot | fix(team): The team mode lacks the "ask_user" tool. |
| 355 | `830df01ad84d5bc9e38fb039aabf7f1dfc1a8762` | 2026-07-06 | chensheng12345 | Fix:TUI team panel toggle |
| 356 | `c2b2c6302eafc294dde23d362754cdcd32a78ae9` | 2026-07-06 | openJiuwen-bot | fix:tui增加文本输入框摁两次esc清空的功能 |
| 357 | `cf882b45285c6f304ca9ac55d5d7af8c554b0aee` | 2026-07-06 | iamcandiceguo | fix: resolve session lifecycle issues in team mode |
| 358 | `d0e7bcdc8ce1710cb039a91bb9ac8d1cc05157ae` | 2026-07-06 | kdgmpw | fix(tui): /keybindings编辑器退出时，误判为tui退出 |
| 359 | `d4e048a226dc1e81a5693c4b7a47f586f1a648f3` | 2026-07-06 | tangfei0306 | fix(agent-teams): 集群多模型池补充 reasoning_level 适配清洗 |
| 360 | `df3e30b97d10ad1481d502fcc52b486e63436843` | 2026-07-06 | yan_zj | fix(swarm):update context config |
| 361 | `28c19e468708784a836b2f207f35ab05b484fa96` | 2026-07-07 | yechenzhen | fix(gateway): migrate A2AChannel to a2a-sdk 1.0.0 server API |
| 362 | `370164b97986e5b3814307d93b4e286153940e7c` | 2026-07-07 | openJiuwen-bot | fix(diff): 修复windows 下 /diff 不显示删除以及修改文件 |
| 363 | `516de5ce6b46fe674dd1b64e012524e37824965a` | 2026-07-07 | haowenzhong1 | fix(tui): parallelize diff computation and skip agent history for new sessions Refs:#1917 |
| 364 | `a62485e327be42c8473b7ce2332d91370b78bc68` | 2026-07-07 | openJiuwen-bot | fix:/skills skillnet search 无结果时提示无结果 |
| 365 | `af779aa6742969e46005a2a94f49d42d7a3b443a` | 2026-07-07 | min_gitcode | fix(team): wait for team stream exit on pause |
| 366 | `b38e14e63be860a1840dc7bea665813626391365` | 2026-07-07 | min_gitcode | fix(team): remove default swarmflow enablement |
| 367 | `c3e7d3d78145ccf80fb9e8741c7d310f42ac665d` | 2026-07-07 | iamcandiceguo | fix(session): modify appgateway not cancel when disconnect |
| 368 | `da7c13dcc4c5b1856057e9a38d6fc389c28deda4` | 2026-07-07 | kdgmpw | fix(updater): 优化桌面端的自动更新判断 |
| 369 | `dd226d9243f781ddf53f8deb236704387d057108` | 2026-07-07 | wncbdj | fix(deepagent): Configure the llm_retry_rail parameter |
| 370 | `fb5eee8d14be1bac914dd8c85ac6dcf2d23e26a6` | 2026-07-07 | haowenzhong1 | fix(tui): exclude untracked files from /diff and update empty state |
| 371 | `ad2b0e35e780bfd3634285fbab00a19d35f16851` | 2026-07-08 | xiaoyao42 | fix：前端增加swarmflow开关 |
| 372 | `e822c5a2fa5aeb4a1543add0db0fa06b433b585a` | 2026-07-08 | min_gitcode | fix(team): enable team observability by default |
| 373 | `fd552fdfa8d6625a45111c61181207dd63109104` | 2026-07-08 | haowenzhong1 | fix(tui): relocate CLI trusted-dir state file to agent workspace and clarify trusted directory prompts |
| 374 | `032f808a5c3fe01e98f234a0c949b802b935e7ad` | 2026-07-09 | openJiuwen-bot | fix(cron):  修复飞书，web侧定时任务推送异常 |
| 375 | `0aa18a456c7c5f488ab1fd45dba24bf498c8f5e7` | 2026-07-09 | xiaotian077 | fix(xiaoyi): 补全 CHAT_ERROR 出站分支，异常消息回传小艺 |
| 376 | `1678afabbcc4c0ce72fb2971ec8b622639e8f9ed` | 2026-07-09 | ray_le | fix(exe): 修复 windowed exe 主进程 stdio 为 None 导致 auto-harness decode_stdout 崩溃 |
| 377 | `24336420ffe42862ac159f5071b8b49f82ebf4d8` | 2026-07-09 | kdgmpw | fix(updater): 巩固windows和mac桌面端自动更新流程，支持预览版自动更新 |
| 378 | `256a3ac4763797e3c5c89b2f7ad568784c2ff21b` | 2026-07-09 | xiaoyao42 | fix：删除对话框中添加会话按钮 |
| 379 | `318f12da7c81a407e969113821e5233897a15d0d` | 2026-07-09 | chensheng12345 | fix(session): Fix human invite card history restore |
| 380 | `37e134a208ba0b5ed11bbaee54d9bd04eee1e284` | 2026-07-09 | Haowei_Du | fix(permissions/security protection): Optimized slow permission modification and resolved issue where rule details persisted after exiting. |
| 381 | `652406483e19000a9e111bcdb6bf4a86cd8b049c` | 2026-07-09 | openJiuwen-bot | fix(runtime): 暴露已配置的模型列表到系统提示词 |
| 382 | `6c94f3e9f2ce7b6f8644f85fb1a0a5a7587013bd` | 2026-07-09 | Haowei_Du | fix(proactive recommendation): refresh colour, restrict recommendation in one session. |
| 383 | `7e5e0a5bfae2af627b3e916c76c85b2c108842dd` | 2026-07-09 | iamcandiceguo | fix(session): modify session cannot be stopped bug |
| 384 | `9c25fec13bec58f347eaec219faa8883ce1c0bec` | 2026-07-09 | ldstyle8 | fix(adapter): guard placeholder model config and skip unchanged reloads， and clean up in-memory session runtime on client disconnect |
| 385 | `a0f0fb708e94afcdd21968dbbbc03f6c0c11ff48` | 2026-07-09 | haowenzhong1 | fix(tui): unify trusted directory confirmation and path normalization |
| 386 | `a6470241b7f999b4857fe3bbccfb38a619abec97` | 2026-07-09 | ldstyle8 | fix(agentserver): skip agent reload after add_dir command |
| 387 | `f6bd05dbbcc63839a87a268483686de0182ebba6` | 2026-07-09 | fyfugoyfa | fix: 修复图片历史恢复与非多模态模型读图降级 |
| 388 | `1c722b8a00743f48dfa73e3a3b98be3b1b80839f` | 2026-07-10 | haowenzhong1 | fix(tui): enable interactive team conversation with idle-watch round detection |
| 389 | `22eaab42933328954bc515edd9cdf84ec27152e5` | 2026-07-10 | hzx99 | fix(memory): 解决embedding配置更新时，记忆索引无法重构，无法热更新的问题 |
| 390 | `3a49d495d9bee290c12d66cd42efd8d7d66fec58` | 2026-07-10 | lin-xiaoyu | fix(symphony): 优化集群模式下symphony技能编排工具调用 |
| 391 | `3c546bd6eefba598d0e9c3135bdb6a8769aaa33f` | 2026-07-10 | chensheng12345 | fix(TUI): Fix TUI transcript scrolling after skills list |
| 392 | `3f8387d5abe4f66ccb8c67103370a49a20b31c16` | 2026-07-10 | openJiuwen-bot | fix(cron/feishu): 修复飞书/web/tui 多应用场景下心跳/定时任务异常 |
| 393 | `405f158c22444dcb66677023656dabfb19190aaa` | 2026-07-10 | hwxiajing | fix(session): team模式3种mode统一处理 |
| 394 | `59cf6de704a606a64d502c8f3612428deaa4829f` | 2026-07-10 | Haowei_Du | fix(proactive recommendation): format check for max recommendation per day and max dialogue turn used |
| 395 | `62e24b69cb1112756b2a5145614b48de9ce1eb54` | 2026-07-10 | jack_ricky | fix(deps): exclude protobuf-incompatible opentelemetry versions |
| 396 | `75721f1d0188744bbc04ad5eae1ac74ee0964f9b` | 2026-07-10 | openJiuwen-bot | fix:在 IM（微信） 输入 /skills list，会返回所有的内置skill，且看不出是否已安装。 |
| 397 | `9a4e051b547038b9c6857b6a008ea7943f17af50` | 2026-07-10 | lin-xiaoyu | fix(symphony): 修复WebChannel 精确路由改动影响了无 session 的 forwarded RPC的问题 |
| 398 | `9cb230f68b0ded0d2badd5573484f8796e2681a8` | 2026-07-10 | kdgmpw | fix(updater): 优化更新页面文案提示和按钮状态 |
| 399 | `a28f138d06c8a6d69e831f30380581795ca61585` | 2026-07-10 | MengHan18 | fix(gateway): improve web channel response reliability |
| 400 | `d022b540d32c137c2a651f6fed1d0991a07023a2` | 2026-07-10 | kdgmpw | fix(claw): skill删除时实现增量更新 |
| 401 | `01554109cfd1c069f74b5fdd9f3da7f4447853bf` | 2026-07-13 | chunbs_cbs | fix(skill): 基于经验增强的skill分发针对同一个query可能推送重复skill |
| 402 | `095a784f75edbf61cb2f08bad2a3ba4590d2a2c2` | 2026-07-13 | Haowei_Du | fix(proactive recommendation): forbid natural language to delete proactive cron |
| 403 | `0aef0014f35738cdc1a772e53eb3d21f8a5b81e2` | 2026-07-13 | chensheng12345 | fix(web-frontend):Simplify Xiaoyi channel app config |
| 404 | `0ba510217a54937a282d6febcb03691b57dc33bf` | 2026-07-13 | zhaozixuan | fix: add auth for jiuwenbox |
| 405 | `2c3a2f8739f9521744f4219a6906ddd5a20d9394` | 2026-07-13 | lin-xiaoyu | fix(symphony): 技能编排补充HITL，追问确认执行 |
| 406 | `2f04f53acc52158ac4768458ec2f108f0d9fc41b` | 2026-07-13 | Haowei_Du | fix(proactive recommendation): notify user when proactive recommendation  reaches the limit. Set proactive recommendation to close in default。 |
| 407 | `42e8a0a9d7520f17847979c12b7004068f995247` | 2026-07-13 | openJiuwen-bot | fix:config修改model时加锁，确保高并发条件下修改无误 |
| 408 | `4656d46f53e2124dbcdb560fd6b28712849e0788` | 2026-07-13 | stevenchenhanwen2006 | fix(ask_user): 选择 Other 时展开自定义输入框，避免输入丢失 (#1983) |
| 409 | `47894322d918578ca5b11fce394108a3fc078898` | 2026-07-13 | stevenchenhanwen2006 | fix(rails): stop ask_user option cards re-popping forever (#1976) |
| 410 | `4bec9837e7e6acc2763c13bc7cfc8acaf7e06562` | 2026-07-13 | chunbs_cbs | fix:基于经验增强的skill分发不应该轨迹评估无法正常召回 |
| 411 | `51ac4cb15938115d74e3d373f83d136f1533a82d` | 2026-07-13 | hwxiajing | fix(session): /join成员查询校验不依赖Monitor |
| 412 | `569d4ca91e03528bf19950bb0bdc0d2692492797` | 2026-07-13 | iamcandiceguo | fix(team): silence chat.error on gate_closed during shutdown race |
| 413 | `6d5cdb5d68624bee1126c7d0b8bbb9c618088e26` | 2026-07-13 | iamcandiceguo | fix(team): gate chat.final on seen_team_events to prevent premature isProcessing reset |
| 414 | `73385e67a7093c42c71e6177b0de02b58d004822` | 2026-07-13 | kdgmpw | fix(tui): 修复/keybindings在老linux系统中不能热加载的问题 |
| 415 | `8f828cf07529ac7f043f807a4918efc8330447fb` | 2026-07-13 | xiaoyao42 | fix：调整api_base长度至512 |
| 416 | `a98d7adc26b09c0861a93622de499196b05d71ae` | 2026-07-13 | openJiuwen-bot | fix(config): 任务运行时禁用配置， 应用配置后多窗口自动更新 |
| 417 | `aa5bf9548aa912820a9a0c0fd7a9fff371f906f1` | 2026-07-13 | iamcandiceguo | fix(mcp): fix CPU 100% caused by StdioClient cross-task context exit and ToolMgr resource cleanup bugs |
| 418 | `ca4658a0fc1e155be7ce5d3d9c86447d96b82fe2` | 2026-07-13 | jingjinduo | fix(tui):修改/memory交互 |
| 419 | `d57567e4ab624f059e297db04a5a54add7529aad` | 2026-07-13 | yuhaiquan | fix:1.下掉微信channel以及相关下拉选项；2.已安装技能agent配置不显示问题 |
| 420 | `e33ef03af1001c688eb9ca6b5ea5be76d9887150` | 2026-07-13 | yuhaiquan | fix(web-frontend)：下掉日志页签 |
| 421 | `e4b18e0c20d0ea4b716959655cf0fe166ec7a21a` | 2026-07-13 | m056838700 | fix (desktop): package node to exe desktop app |
| 422 | `e6d5e4faa2d266424347499e1df81303c679528e` | 2026-07-13 | haowenzhong1 | fix(tui): update /diff command docs with usage guide, data source details, and effect boundaries |
| 423 | `fb4a17151d14f99de3ea7a681be05819b029453c` | 2026-07-13 | openJiuwen-bot | fix(tui): 修复/resume显示问题 |
| 424 | `ff2f5fa9a34cb4a646f69a5a56691b2073f5356e` | 2026-07-13 | stevenchenhanwen2006 | fix(config): 校验微信通道数值配置，拒绝非法输入 (#2022) |
| 425 | `048945398cc10d10626f6593085c33413af5421e` | 2026-07-14 | huanggawa | fix: v0.2.3 upgrade |
| 426 | `3643312f89c74a99ee25030697543e9b7c61995d` | 2026-07-14 | Haowei_Du | fix(proactive recommendation): change the location of notify of proactive recommendation (from session to pop-up window. |
| 427 | `3ad7003c5888dd6c075a7388effcf5d13341766d` | 2026-07-14 | zhaozixuan | fix: return if agents don't exist when recreating agents |
| 428 | `515e32e204fb957be82a630cf40cf07fdb9214fa` | 2026-07-14 | tianmengyue | fix(deploy): gateway部署默认在本机节点执行 |
| 429 | `7b4f5055ff35bb21a69aa3244abefec6fa7c85af` | 2026-07-14 | huanggawa | fix: packing nodejs |
| 430 | `87d1e4e21611f07524b4221507efbe4a924a2c0e` | 2026-07-14 | openJiuwen-bot | fix(team):Press Ctrl+C, but remain on the approval interface and do not exit. |
| 431 | `a047dcce25483e57da5dfaa0fe81825ca0cab8e2` | 2026-07-14 | xiaotian077 | fix(scripts): build-exe use relative path, UTF-8, skip redundant npm install; exe run without admin |
| 432 | `b06ff06d0fd5114aa93a7c3bb331219219cf0637` | 2026-07-14 | wncbdj | fix(harness): The model keeps looping the same reasoning contents and tool calls |
| 433 | `b5e87c9464ae6ccba4755e5f66ce1e35869a63ca` | 2026-07-14 | stevenchenhanwen2006 | fix(web): 发送新 query 时清掉未回答的内联提问卡片 (#2091) |
| 434 | `cc0fe1237e289d0bf2ebd140463b91bfe949720d` | 2026-07-14 | kdgmpw | fix(updater): 新增恢复默认更新源按钮，适配老旧配置 |
| 435 | `db84f06bf04377bfba56d9eabb58b2a1482433a3` | 2026-07-14 | huanggawa | fix: add node.js when packing dmg |
| 436 | `e69770a70bf1cde6808c716b6a9b8d2f151906ef` | 2026-07-14 | ray_le | fix(channel): 同session 多飞书应用发送文件问题 & 飞书上传文件找不到问题修复 |
| 437 | `e9c4c6dd29d815c232a88585b178fb233b7837b6` | 2026-07-14 | openJiuwen-bot | fix(log):增加日志以便定位 |
| 438 | `ea480aba1af4eb05c1f47b2056f8c9ebc713d904` | 2026-07-14 | jingjinduo | fix:屏蔽tui中mcp、sandbox、diff、memory命令 |
| 439 | `f90f4d04b52464fbabaab22cf14714d4d7e7323d` | 2026-07-14 | openJiuwen-bot | fix(config): 取消任务后广播全局运行态并排除 team 任务 |
| 440 | `ff5e8ef102c3cdecf5b0a35c5e1a5ceca9e41ba7` | 2026-07-14 | chunbs_cbs | fix(symphony): 异常未抛出 |
| 441 | `01d9a37d711db82271273a8d57280ce9a4e45359` | 2026-07-15 | iamcandiceguo | fix:chat.final not receive bug fix |
| 442 | `557a8cbcbf2ea82491fa795807e9361400bf4214` | 2026-07-15 | tangfei0306 | fix(agent-teams): put models.defaults in runtime |
| 443 | `816ae71df17cc65a5a848ce9ed41d4de67c689d5` | 2026-07-15 | jack_ricky | fix(a2ui): 修复安装包缺失 Schema 资源 |
| 444 | `ae8c236d83be94ffa584f48e2c3f78f45a832ba3` | 2026-07-15 | ldstyle8 | fix(agentserver): reclaim terminal session processors |
| 445 | `c96540bef81411431c0ca98962ef9d9b70dabd8b` | 2026-07-15 | ldstyle8 | fix(tui): single-press Ctrl+C/Esc interrupts askUserQuestion approval box; drop Ctrl+D |
| 446 | `da072e3babdcf0778afb84a6731cadecbb0910d3` | 2026-07-15 | openJiuwen-bot | fix(start):  修复jiuwenswarm-start   --status/list 命令无输出 |
| 447 | `5faad049865b702ac329e2ce2762272b8ae6f563` | 2026-07-16 | haowenzhong1 | fix(tui): enable /memory command and fix open tab on headless Linux |
| 448 | `70ef24827b81b3faf524fb1acf4f8d69e6a61fdc` | 2026-07-16 | hwxiajing | fix(session): 修改报错提示 |
| 449 | `893ec5adfea056774d24621e8564d98f3cdf074c` | 2026-07-16 | haowenzhong1 | fix(tui): swallow Ctrl+C traceback during CLI startup sync phase |
| 450 | `8aa41f95d63d555237f29c26d96fbc1aebb434cb` | 2026-07-16 | ray_le | fix(web): 导出对话历史图片添加AIGC隐性标识 |
| 451 | `a26885b6b89709e51df791177cb2a7aa1d308421` | 2026-07-16 | openJiuwen-bot | fix(team):In team mode, it is impossible to determine what mode the current situation is. |
| 452 | `ac858bd035469f21ddf5acbba5bdbaa673232131` | 2026-07-16 | douran | fix:屏蔽tui中mcp、sandbox、diff、memory命令 |
| 453 | `c5f4b354a3eb38737d233718df4c4478ae5a0b4a` | 2026-07-16 | min_gitcode | fix(installer): launch app through explorer after setup |
| 454 | `1c5f3f8edf9b49efe2ba4379c451f9d6e90802a1` | 2026-07-17 | steven_sun | fix(jiuwenbox): 支持 Java 程序在沙箱内运行端到端测试 |
| 455 | `300de826b6d2743d87984297286c456fa113663c` | 2026-07-17 | cjh_jinduoxia | fix(cron): 修复定时任务面板一批交互与状态显示问题 |
| 456 | `427e938beb85325da6ca12733f6e625f6afc179b` | 2026-07-17 | feitiandada | 合并远端主干+修复clencode的问题 |
| 457 | `62f7bf3c3dea8e29b2dbea294f1bdb2a921f7e19` | 2026-07-17 | Haowei_Du | fix(experience): revise fallback for model failed |
| 458 | `7b6b4e570c4742020f31f4daa157f6ee4b2b1610` | 2026-07-17 | openJiuwen-bot | fix(yuanrong):Fix the issue where the resume list is empty when using yuanrong |
| 459 | `a18f73bdcae266e8279b109e1e30a90ee53e1634` | 2026-07-17 | liangfuqi | fix(gateway): 修复非对话流退出导致配置保存不可用 |
| 460 | `b5244b94731c71196cff319a72907c882f8add88` | 2026-07-17 | xiaoyao42 | fix(web-frontend)：修改样式实现，兼容107版本的chrome |
| 461 | `e76ef92fd57207a0d5fd2991ffde49fa92adbf50` | 2026-07-17 | yuhaiquan | fix：定时任务面板点击“立即执行”后刷新左侧定时任务下session |
| 462 | `778219b0ea3eade58aea4a06691829a2be3f5d84` | 2026-07-18 | cjh_jinduoxia | fix: 聊天消息中代码块/表格字号过小的问题；对话框复制消息后弹出已复制提示；过期定时任务不再反复更新时间； |
| 463 | `8b2307a1ded0468b0933fddcdecfc59104eb7a0d` | 2026-07-18 | kdgmpw | fix(skill): 修复前端过滤内置skill遗漏的问题 |
| 464 | `bd5d2e7c6e67055d8a532a139b33a0fc457c6fce` | 2026-07-18 | cjh_jinduoxia | fix(cron): 修复左侧栏工作态未同步高亮 + 编辑面板长下拉框未做高度限制 + 排班的日期选项按所选月份动态限制，避免选中不存在的日期 |
| 465 | `bd6dc29582e5af91ef112893b73adc0d3af44845` | 2026-07-18 | xiaoyao42 | fix：样式调整 |
| 466 | `0b4039a2336670c2e7c6bcd773bc8a457c587a6c` | 2026-07-20 | zhaozixuan | fix: jiuwenbox bg-exec and exec process use the same pidns |
| 467 | `0e11c8b1fc70ed6739fb6391858363fad79fbe53` | 2026-07-20 | yangzequ | fix(harmony): sync HarmonyOS adaptation from develop_harmony_on_win_0716 |
| 468 | `0ee6ee3c7865cfa9fbc17180426a4b3bd8f38e29` | 2026-07-20 | cjh_jinduoxia | fix: 国际化定时任务默认项目名与 Harness 扩展导入报错信息 |
| 469 | `18756aa58e834f74863592e9ef066f04611d314b` | 2026-07-20 | zhengkailan | fix(dialogs): enhance delete dialog with improved structure and localization |
| 470 | `1b8b3eb31d4bc364e6a39a2854d1783561dc69b1` | 2026-07-20 | yuhaiquan | fix：广播消息不会发往当前会话 |
| 471 | `35c5fef883086a00a51e109c8c70aa62505d5776` | 2026-07-20 | Ziliang_Zhou | fix: 修复openjiuwen记忆插件scope_config没设置的问题 |
| 472 | `4309fa2e2eb5e4d7aa3af202133dca714971a37d` | 2026-07-20 | kdgmpw | fix(tui): 修复多窗口 /resume 冲突导致窗口异常退出 |
| 473 | `54a2e3e29fce54f7dee514620fcb1dfacd3c5b29` | 2026-07-20 | zhengkailan | fix: refine project filtering to exclude project_dir search |
| 474 | `5a81dab3eb0b968db8cd9182f393fec4dccef701` | 2026-07-20 | z2tng | fix(evolving): optimize skill capture and evolution triggers |
| 475 | `b698f77f127a014051c0476274988a469bb9344e` | 2026-07-20 | kdgmpw | bugfix(cron): 定时任务默认不提前启动 |
| 476 | `bd4f95db2d13eaa4dbc5b33483901adf123ecc36` | 2026-07-20 | huanggawa | fix: fix gatekeeper problem and packing skills on mac |
| 477 | `d1af886db38a8e5ce36f35fed31b897287af6003` | 2026-07-20 | Ziliang_Zhou | fix: complete PyInstaller bundling of chromadb submodules and Rust bindings |
| 478 | `2361c59060187b09938ea179704aeb0b780b97b9` | 2026-07-21 | hwxiajing | fix(team): remove unused top-level team config (superseded by modes.team) |
| 479 | `49020f9ea4231fc4ac1846b056eb4e9b06f6608e` | 2026-07-21 | chunbs_cbs | fix:memory/manager.py 中 6 处裸 except: 吞掉 CancelledError/KeyboardInterrupt，导致进程无法正常关闭 |
| 480 | `522164db0fba59f8ceb813a946e5df1160154fb0` | 2026-07-21 | yan_zj | fix(swarm):update default project workspace and workspace prompt |
| 481 | `6a2a953b14bdeba4aa25ebd8e8cec67df7b66734` | 2026-07-21 | yuhaiquan | fix：1.url接受“cron_”前缀的路由；2.心跳-频道下拉框同步频道启用逻辑 |
| 482 | `94310a3ad04690ff1012b34035344fa0eedab36f` | 2026-07-21 | fyfugoyfa | fix(web): restore persisted chat images in desktop history |
| 483 | `971376dc8b7b6ce5095c5824b319f73d85fdfb78` | 2026-07-21 | xiaoxiawang | fix（docs）: 修复SwarmSkills文档结构 |
| 484 | `a4701db8c625c31bf72ee863ed9cda8c0c974002` | 2026-07-21 | kdgmpw | fix(aipc): 变更统计untracked文件行 |
| 485 | `b073b1fe579afc455d8cc63651a66712fcdf62ca` | 2026-07-21 | weichenhao | fix(team): In the team mode, directly invoking the "chat" command yields no response. |
| 486 | `baf35abadf32d5c5f71433551ba1c75b7a9890a8` | 2026-07-21 | Haowei_Du | fix(proactive recommendation): fix abnormal page when click recommendation auto-run |
| 487 | `bd3c50361758e26bf65d3ad62b0282fec97fc5c2` | 2026-07-21 | openJiuwen-bot | fix:/config set 的 agent.reload_config 改为后台执行 |
| 488 | `be115f6ce1ec1e09de908254eefbd2f705ad0faa` | 2026-07-21 | kdgmpw | fix(gateway): Web 流事件优先按 metadata.ws_id 寻址，避免跨会话泄漏 |
| 489 | `caec89ca36b1ec1b007896a53f95ef078a57f694` | 2026-07-21 | l_x_d | fix(harness): use plan prompt attachment |
| 490 | `d4155ed706a549e939af834f480f704a822cf1b8` | 2026-07-21 | xiaoyao42 | fix：回复时间戳是第一个文字生成的时间，而不是结束时间 |
| 491 | `d9efec8897468bf785f9098ad747ba4080a182a7` | 2026-07-21 | fyfugoyfa | fix(feishu): 使用 CardKit 实现流式卡片回复 |
| 492 | `dd7c6ef46a90053de26b426e423a0be0cde86a1e` | 2026-07-21 | z2tng | fix(swarmskill-creator): support human and session operators |
| 493 | `e68a5a15aad51e1c4138a32450d7adf819c0d327` | 2026-07-21 | xiaotian077 | fix(server): 模型配置变更时广播 reload 到所有 channel |
| 494 | `ee6c874d1191295f7b6241cea7174f9105e31c69` | 2026-07-21 | openJiuwen-bot | fix:修复tui会话正在进行时esc无法中断任务 |
| 495 | `f42fe4e8e031f65f0e99fada2e8909b6354dea82` | 2026-07-21 | slichengsong | fix: <asyncio.locks.Lock object> is bound to a different event loop 问题修复 |
| 496 | `fc6ef277e6053d833d25f356f90e732af75b53a6` | 2026-07-21 | xiaoxiawang | fix:优化频道结构 |
| 497 | `0819dc9dfa5c7280ccba0f07aaad95ba763f4275` | 2026-07-22 | openJiuwen-bot | fix:删除所有模型默认配置 |
| 498 | `0baa582389e0a3ffa8ce766e935636073777572e` | 2026-07-22 | zhaozixuan | fix: don't save backgroud command output |
| 499 | `0c8fe16ef2896eb17573d04b04d5045c0f95b05f` | 2026-07-22 | xiaoyao42 | fix：修复显示问题；去掉拖拽文件进对话框的能力；去掉前端根据前缀过滤会话的判断，过滤由后端处理 |
| 500 | `253c6ed564dd4f849611d70fff30e5a05e480a4e` | 2026-07-22 | cjh_jinduoxia | fix(web): carry question text back in ask_user answers |
| 501 | `2d369f1b617c5cb3f9a7b988f2aa116a3f18a208` | 2026-07-22 | NickFuryXXX | fix(xiaoyi): CLI notice 下发 is_complete 终止信号 |
| 502 | `6819a5f726b633be552fc91b587355edab3da676` | 2026-07-22 | kdgmpw | fix(cron): web run-now push routing |
| 503 | `6bc36113b3ffe62f17087de4014148e28dfa034d` | 2026-07-22 | yuhaiquan | fix：广播结束消息不回显 |
| 504 | `6ef18a9e97d0066fa498c0f6bd58cf0c96e09058` | 2026-07-22 | haowenzhong1 | [fix] add JIUWENSWARM_SKIP_NETWORK_CHECK env var to skip external network detection Refs:#1667 |
| 505 | `842300b8f4de80e1bc555dbb0c1cacf4f5a4b271` | 2026-07-22 | kdgmpw | fix(cron): web端按照channel_id过滤cron_task |
| 506 | `85e48583d46ee6bd4b5bfc2da5399426351b20db` | 2026-07-22 | zhengkailan | fix(web):Remove unused clear label from project selection in InputArea |
| 507 | `95bd2a08016be84063f48af4ac50216329f1bc2d` | 2026-07-22 | kdgmpw | fix(cron): 修复定时任务创建时 _Event 不可比较导致首次提交失败 |
| 508 | `a4e716b9708303338063e62dba94fae19539e0e4` | 2026-07-22 | openJiuwen-bot | fix:jiuwenswarm端口冲突自动辨别 |
| 509 | `d7c977b236d4dc19e15d1b2c5b728778164a1c54` | 2026-07-22 | haowenzhong1 | fix(tui): gracefully handle Ctrl+C during CLI startup to avoid raw KeyboardInterrupt traceback |
| 510 | `dff4afe9edd4ac75af904d548e8ed5d470248b7d` | 2026-07-22 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#512: fix (A2UI): support EN for A2UI hints |
| 511 | `06ae138c25a3c009913e2d73bbafb114fed69e14` | 2026-07-23 | xiaoyao42 | fix：前端去掉心跳页签、去掉心跳通知处理；集群模式规定选择默认模型，暂不允许切换 |
| 512 | `14c706c2ba900acbcadda42f5433e8759a1a4f91` | 2026-07-23 | liangfuqi | fix(gateway): web 侧返回的会话列表仅返回 web 渠道会话 |
| 513 | `16486b1596751f4baf73b3ce6ad2139fa9f3c198` | 2026-07-23 | yuhaiquan | fix：队列消息立即发送不会同时发送两条消息 |
| 514 | `18692b1720bb03b90eae88bdf6e79af4720d1e70` | 2026-07-23 | yechenzhen | fix(agentos): switch YuanRong agent create to inline runtime_spec |
| 515 | `368c75bc779b0263a3facfe9597437d4ad02950b` | 2026-07-23 | hwxiajing | fix(gateway): /join /exit session_ref 正则支持中文 team name |
| 516 | `4dd4458ee2ec2ce7810539bee0fdf2d7fb4a5cb7` | 2026-07-23 | huanggawa | fix: stop spawning a subprocess in stdio pre-check (anyio cancel-scope hang) |
| 517 | `61c00a8b2ddb750550d713ca07b52a21e8dcae12` | 2026-07-23 | NickFuryXXX | fix(start): print access URLs after jiuwenswarm-start is ready |
| 518 | `6bfbfafaffc2ff80d3fbdc42c0a9725bce6eb925` | 2026-07-23 | xiaoyao42 | fix：产物数量为 0 时，团队模式和单智能体模式均不显示“产物”页签 |
| 519 | `75f63d088509b32fe81c7e76c2b2e0fc70622afc` | 2026-07-23 | xiaoyao42 | fix：修复prettier配置；修复团队清单不显示数据的问题；团队文件显示增加拖动能力 |
| 520 | `9b559904bc4c96f5eef62cf5b271566647b259f5` | 2026-07-23 | SushiNinja | [mirror] openJiuwen-ai/jiuwenswarm#1084: fix: logger does not support file=sys.stderror |
| 521 | `b291fa64a11a4ba9825874a50ddfa0811d5a0461` | 2026-07-23 | chensheng12345 | fix(web): sync code mode branch and turn diff state |
| 522 | `bbe3e4a02b0d73142aa12a899bcf42c006f111c1` | 2026-07-23 | slichengsong | fix(agentos): 函数入口处统一loop，解决Queue is bound to a different event loop |
| 523 | `de6933ad3d4f1e80e63018298a3255ffe907b209` | 2026-07-23 | tangfei0306 | fix(agent-teams): emit artifact file events for team workspace deliverables |
| 524 | `e2f0f2b7cceb2917fdfb67794073372c5c078e4e` | 2026-07-23 | kdgmpw | fix(cron):project context for agent-created jobs |
| 525 | `ed1220b75bdb66590002bc19a043ac683d3946fa` | 2026-07-23 | MengHan18 | fix(ascend affinity): Integrate AscendAffinity KV Cache affinity management and lifecycle optimizations |
| 526 | `ef3d0a1a390a3ce972d79030f44c3b0cea7ec5e5` | 2026-07-23 | min_gitcode | fix(agent-teams): handle team follow-up runtime boundary |
| 527 | `fb126a5fbb57dcdbebc3eda74a87b66ea36731f8` | 2026-07-23 | openJiuwen-bot | fix:/model删除模型命令文档补充 |
| 528 | `13735a1ba5dd74bb88d60095a033b60dca2bfffd` | 2026-07-24 | jingjinduo | fix:修复输入/memory不存在的子命令时逻辑 |
| 529 | `25e5cf0ea3ab1ee06fcbc4137050062668a8e499` | 2026-07-24 | openJiuwen-bot | fix:_config_set_on_saved_background()改成后台运行以防/config get超时 |
| 530 | `28955323cdd6bae4c38f28fb25ca293e19f9c458` | 2026-07-24 | yuhaiquan | fix：执行session重命名后刷新列表 |
| 531 | `398ca0759db8803225edfe838562851ee8f7650b` | 2026-07-24 | chensheng12345 | fix(web): position mention remove action |
| 532 | `4c6e2c789e53e7edb107eb1a922edefc8c91df27` | 2026-07-24 | haowenzhong1 | fix(agentos): align yuanrong faas handler with ws agent_server processing path |
| 533 | `74dcc23d3636cef8a19645838e1da11fde5fa847` | 2026-07-24 | cjh_jinduoxia | fix: 定时任务列表飞书/钉钉等非web渠道任务不显示，且无主动刷新机制等bug问题 |
| 534 | `9539d360573b769ad90f0d239b4a02764e2f1341` | 2026-07-24 | kdgmpw | fix(updater): 修复被删除的恢复默认源按钮 |
| 535 | `95a21d2ab1c16657563a9118a6643c10b4870960` | 2026-07-24 | chenchunzhou | fix(team): dedupe task cards by DB task_id and backfill title/content |
| 536 | `9b05c8adf666f7e76d2998e66b2a7bfed1ac8a7a` | 2026-07-24 | chunbs_cbs | fix:TraceRecord(trace_id="t1") 该入喉非必填项 query 没有默认值 |
| 537 | `a0d9d499e8e58e2af66f721f74b2a2b09e7f5585` | 2026-07-24 | xiaoxiawang | fix:快速上手同步英文及重构 |
| 538 | `a4ad6413eea33540636ece2877456b74ec61e68a` | 2026-07-24 | chunbs_cbs | fix:显示浏览器后关闭出现僵尸进程 |
| 539 | `b07b23f407ea3d6aacaf91fde74a0796f073a05d` | 2026-07-24 | jingjinduo | fix:修复askuser取消后再问吞消息 |
| 540 | `bd19121b53ae05ad580f39ccd95bf573600500d9` | 2026-07-24 | jingjinduo | fix:修复/memory编辑文件过程中交互逻辑 |
| 541 | `d105dea8159eea18045501f5f9af46ab370155bc` | 2026-07-24 | xiaoyao42 | fix：修复桌面应用端若选择相同的项目，错误提示一直存在于对话列表中的问题 |
| 542 | `d2f1ae839f068171a8ec05cce95e50362201277b` | 2026-07-24 | cjh_jinduoxia | fix: 去掉定时任务执行计划"按间隔"分钟/小时步长必须整除60/24的多余前端校验 |
| 543 | `d6b9daac694eab5c57511a97a2ed2ade946f5722` | 2026-07-24 | wncbdj | fix(goal): goal task exception handling |
| 544 | `ddb78de0ffa66098bf46bef8fae5ddc2a413d2c5` | 2026-07-24 | douran | fix(web): remove typo |
| 545 | `e68d8722a48aa2bc5966c13765de073e7c86eebf` | 2026-07-24 | kdgmpw | fix(aipc): team last-turn file tracking and remove file-ops filtering |
| 546 | `1367fee0d3ef3af62ba7db5a6241d8b23fc055ad` | 2026-07-25 | lin-xiaoyu | fix(symphony): 修复能力指纹新增type字段导致的兼容性问题 |
| 547 | `1443c54b551c9a3710513bc411cf6bedc2255828` | 2026-07-25 | yechenzhen | fix(agentos): route jiuwenswarm via YuanRong URN invoke |
| 548 | `14d62be49e583593ab9f3db26c1591b65effd3d3` | 2026-07-25 | layShang264 | fix(symphony): recover interrupted graph builds |
| 549 | `1d5c54bdf9cd9de5836d4a94148b5d5848a15244` | 2026-07-25 | CUIZIBO | [mirror] openJiuwen-ai/jiuwenswarm#803: fix:cronJobStore的持久化文件的跨进程互斥锁 |
| 550 | `212eb22e0d132a2464298da7cbabdc5819ebc855` | 2026-07-25 | openJiuwen-bot | fix:日志中打码api_key等敏感配置 |
| 551 | `217c0400888a107daa59466993e7a55bf864ef56` | 2026-07-25 | GYHHelloworld | dev:锁定依赖版本下限以修复已知 CVE（!1506） |
| 552 | `233c94456738adde368787018e8ba56661ed6515` | 2026-07-25 | zhengkailan | fix(code): handle user cancellation in askQuestions without reporting failure |
| 553 | `25d660ebaf67ab76647fdd8b143ea0fa869c7047` | 2026-07-25 | gcw_6ZTnJdNN | [mirror] openJiuwen-ai/jiuwenswarm#1302: fix(LLM connectivity test): The test probe is hard-coded with temperature=0, ... |
| 554 | `4ce15d40474f4eaeef6eb520a964a8c2cf625126` | 2026-07-25 | yangzequ | fix(git): retry prune/detach in switch_branch when branch held by worktree |
| 555 | `4d6f33dd58c5becb2d3384b38fbb4c6b16de5ab6` | 2026-07-25 | ray_le | fix(exe): 提升jiuwenswarm & exe 启动速度 |
| 556 | `5ac6fa98f09297eb559e2ad669735ccff3ac1cae` | 2026-07-25 | gcw_6ZTnJdNN | [mirror] openJiuwen-ai/jiuwenswarm#978: fix(memory): When all the folders in the workspace path don't have a … |
| 557 | `634c4c8fc98dbf32128eb93cc0f65d321e214166` | 2026-07-25 | NickFuryXXX | fix(skills): keep SkillNet install jobs across stateless agents |
| 558 | `699c497dc6346e183b0b424faffd994e4e27abc6` | 2026-07-25 | NickFuryXXX | fix(gateway): stop tracking temporary WebSocket session ids |
| 559 | `8d147cf522bead198a63f05b4bb2aa2539e8202c` | 2026-07-25 | chunbs_cbs | fix:降低多次join相应错误提示消息严重滞后显示概率 |
| 560 | `a7bcbf785448e190ac2774a0a3656c753990260a` | 2026-07-25 | renquanjia | [mirror] openJiuwen-ai/jiuwenswarm#1345: fix: 模型回复语言跟随界面语言设置，不再被用户输入语言带偏 |
| 561 | `aa32ded9f24259282413b2f6ee09a8a9fd610420` | 2026-07-25 | NickFuryXXX | fix(xiaoyi): persist push_id for cron notifications |
| 562 | `ac161881be0f4d94faa0eb5f3240a504317eff04` | 2026-07-25 | xiaoyao42 | fix：隐藏开关和选项 |
| 563 | `c254a6233e6fd259e0e6f29d67bc818d74110e8e` | 2026-07-25 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1085: fix:生成多余-p文件夹 |
| 564 | `c9a2ce83bc5aa857cf5ac8b4cc077d2ff9289730` | 2026-07-25 | cjh_jinduoxia | fix: 定时任务模块多项问题修复（Cron 表达式文案/校验、项目下拉框、列表分页、名称长度限制） |
| 565 | `d1c19455a48ca82fc51ac2e2dfb9ca09f79e4cb9` | 2026-07-25 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#1259: fix(browser subagent): Simplified browser startup logic and fixed bro… |
| 566 | `d561955f855e6ebbae91aa3ee1f95ee13e0159f2` | 2026-07-25 | gcw_6ZTnJdNN | [mirror] openJiuwen-ai/jiuwenswarm#1377: fix(LLM test): Fix the issue where the base_url mistakenly included the /chat... |
| 567 | `ed4e0ad986568e6e0159b98ad772b2405cd548e2` | 2026-07-25 | NickFuryXXX | fix(session): harden session.create against timeout and cancel queue blocking |
| 568 | `f57879ceb621cafe6cee13fc9a49a44396189ab3` | 2026-07-25 | z2tng | fix(web): improve skill experience display |
| 569 | `ff17738e24bd09eff90189ce5be18488a07de7f1` | 2026-07-25 | jack_ricky | fix(a2ui): 增加桌面安装包冻结资源校验 |
| 570 | `2bc04cec97461c60b078988d4a83d448ff6dcac9` | 2026-07-26 | yuhaiquan | fix：Harness Package 管理文件弹窗被遮挡 |
| 571 | `4e9b98d19f846c6b655309ee79c5a4b593d18c53` | 2026-07-26 | NickFuryXXX | fix(cron): disable Xiaoyi push target without api_id |
| 572 | `de623dd9ef9871159a5287a57b97db1c6f19390a` | 2026-07-26 | cjh_jinduoxia | fix: 定时任务/集群模式4项问题修复 |
| 573 | `f76b2c46fe58fcd1d330744b88f36a06ee142957` | 2026-07-26 | wncbdj | fix(tui): disable newline translation when restoring files |
| 574 | `0305a2f56264c3012234b6848952bd7758252b9c` | 2026-07-27 | yechenzhen | fix(gateway): tear down SSH north/south when either side disconnects |
| 575 | `05e206d890c6f90509c4a6beacf65f7762a443f0` | 2026-07-27 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1399: fix:agent主动推荐内置技能后搜索不到导致无法安装 |
| 576 | `1effa29f3c33f71a49dad76087356f44d797da4e` | 2026-07-27 | GYHHelloworld | fix: 子 agent 未挂载上下文压缩轨导致上下文膨胀 (!1147) |
| 577 | `216f8316806a031bca01d41dd0a10c8b92a45f3e` | 2026-07-27 | kdgmpw | fix(project): 新建项目选择现有目录时添加校验 |
| 578 | `23fc8625f50ef3b55a8e6763c95ca35f0b43a758` | 2026-07-27 | chunbs_cbs | fix:飞书配置权限缺失，但报错不明确，不方便定位 |
| 579 | `2b55f666b17734eaa8b5b85a605fefc788714dfe` | 2026-07-27 | hwxiajing | fix(observability): skip root span when agent_observability not enabled |
| 580 | `32e077613e97a84bb12289af9c35c787f0d946ee` | 2026-07-27 | ldstyle8 | fix(stream): coalesce deltas and preserve whitespace across layers |
| 581 | `3e528ebb53f03168e0a77712fa32601a0b3cfd9b` | 2026-07-27 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1034: fix(clawhub): pass ownerHandle to resolve ambiguous slug 409 Conflict |
| 582 | `3fae43e95a148ea0406adaf8798e2f32e4f4b83e` | 2026-07-27 | Lexie-7 | fix:tui --session命令启动后端调用/resume以及对session_id的限制 |
| 583 | `4c4dcb90709348d49c68e03d6f1a2c67559921e0` | 2026-07-27 | chenchunzhou | fix(team): remove harness todo from leaders, keep team task board |
| 584 | `564ed861a3bb6940828c0e0bb344687a698a948d` | 2026-07-27 | xiaoyao42 | fix：解决randomUUID的兼容性问题；去掉会话运行中不允许切换code和work的限制；增加初次进入系统，渐进式引导配置模型 |
| 585 | `614fbdd47b26d92c049701f270d317bd8a5a0fc7` | 2026-07-27 | yechenzhen | fix(gateway): guard None metadata so A2A ingress does not crash |
| 586 | `6b75f478985f39f628f4ea0d070d8a46cfb32427` | 2026-07-27 | wncbdj | fix(llm): Fallback configuration for adaptation exceptions |
| 587 | `73c9abc20a47cc55bb3b00310893f1602ca1df9c` | 2026-07-27 | yechenzhen | fix(agentos): load southbound SSH keys from /home/{user_id}/.ssh |
| 588 | `759ef7b29c1b4295a9c5d3529c1939a3863de971` | 2026-07-27 | lin-xiaoyu | fix(symphony): 优化图构建时间 |
| 589 | `7d27b49e27ba28b6a6bf9908f8aa4ee9292937d5` | 2026-07-27 | ldstyle8 | fix(ask_user): validate structured question fields and allow free-text options |
| 590 | `86d7f6aabd414a64d28fc710e9407fbb3537ac7e` | 2026-07-27 | RyanYang51 | fix：修复deepsearch skill任务执行异常问题 |
| 591 | `92cafacd5a435fd3bf875fba427364003220c715` | 2026-07-27 | ldstyle8 | fix(agent-server): reclaim idle TUI root agents and propagate cleanup failures |
| 592 | `a90254f8d74fd83400e2d7fabf016faa713abd04` | 2026-07-27 | liangfuqi | fix(tui): 对 Ctrl+D 显示正确的退出信息 |
| 593 | `b4318421c43e2c31e4275a92f654fba03b8e28ab` | 2026-07-27 | haowenzhong1 | fix(agentos): emit handoff JSON to stdout for launcher-driven 3rdagent.switch |
| 594 | `b96626a37d55f63add739ccbb4ef684c40c37b81` | 2026-07-27 | wangxiaolong100 | fix: 【代码回退】子 agent 未挂载上下文压缩轨导致上下文膨胀，并补齐日志目录与 chat.final trace (!1147) |
| 595 | `cbcf545ab3fb3e62619ce3511643bfb660b13623` | 2026-07-27 | yuzengjie01 | fix(cancel): close cancelled tools live after fire-and-forget interrupt |
| 596 | `d17af285dc10c2f22b7edad70c55fd4a79db4d87` | 2026-07-27 | GYHHelloworld | fix: 子 agent 未挂载上下文压缩轨导致上下文膨胀，并补齐日志目录与 chat.final trace (!1147) |
| 597 | `d238c6aa713f21bc44205d7a61b838dbf417d847` | 2026-07-27 | Lexie-7 | fix:第三方命名空间logger打印的api_key加密 |
| 598 | `ee2ed499b80bfaeaa04ef2086f6d51c9e3bb4ae3` | 2026-07-27 | NickFuryXXX | fix(cron): allow configuring wake_offset in schedule editor |
| 599 | `eec0d014e998bac8d68314cb5aaa93553a98b14c` | 2026-07-27 | chunbs_cbs | fix:MiniMax-M3模型在配置界面测试不通过，但是聊天界面可以用 |
| 600 | `04c658864074e76dfb74121a4e6d47b747d96342` | 2026-07-28 | zhaozixuan | fix: add default config for tool sandbox |
| 601 | `2536a8fe90a3c3a75b0db1c2c402a8a7b970a81f` | 2026-07-28 | tangfei0306 | fix(agent-teams):  In team mode, messages from IM channels (e.g., Feishu) are sent by teammates, not the main user, and won't show in the main chat panel. |
| 602 | `2d740196ace48f6fa103de8b647a88aa16f60816` | 2026-07-28 | openJiuwen-bot | fix:解决桌面版jiuwenswarm端口冲突 |
| 603 | `51ed46d3c43d1a07ccaf602acf2968bb53cec8fb` | 2026-07-28 | NickFuryXXX | fix(start-services): 修复启动端口展示/绑定不一致与 IPv6 预检漏检 |
| 604 | `5593b121f861db6fd9e9cd50eceb165820bef2b1` | 2026-07-28 | zhaozixuan | fix: sandbox support yuanrong type |
| 605 | `5baad226d36f873b88e64e474fab8c6110d8a87f` | 2026-07-28 | xutuo | fix (harmony): 修复鸿蒙pc上，集群模式下Agentic技能检索技能可见性问题 |
| 606 | `5ff68960132645a823ff1e0c7fe02b8f443dc449` | 2026-07-28 | gcw_6ZTnJdNN | [mirror] openJiuwen-ai/jiuwenswarm#1624: fix(/rewind) conversation-only mode deleting file snapshots and a metadata.js... |
| 607 | `61a7976bff143636424702faff2978f5140aac2f` | 2026-07-28 | yechenzhen | fix(gateway): propagate TUI user_id into all outbound E2A envelopes |
| 608 | `6478cc2a060c25f89eb0607a563d2ea1e5762325` | 2026-07-28 | alan_cheng | fix(swarm): report and seed the member's real working directory |
| 609 | `7460fdfaf1a50e9cd065ec0a10038a7182b8841e` | 2026-07-28 | GYHHelloworld | fix: send_file_to_user 增加 session 级去重，防止压缩后重复投递交接文档 |
| 610 | `769563f2f4c251cbabc3ca52b69482e7d890a8f4` | 2026-07-28 | wncbdj | fix(goal): keep user-round chat.final; demote only in-flight goal attempts |
| 611 | `7707530a839a0e0729658547d31f939cabcf478b` | 2026-07-28 | chunbs_cbs | fix:选择项目目录右侧的下拉箭头方向与其他有下拉菜单的箭头相反 |
| 612 | `927d3ef621643dc84a101c9dcd84cde937ce8495` | 2026-07-28 | NickFuryXXX | fix(cron-project-field): unify unset project display for manual vs chat cron jobs |
| 613 | `9348287b5e1f9f5db3a8de356b96734b6305fac0` | 2026-07-28 | NickFuryXXX | fix(web): dedupe cluster-mode artifact file downloads |
| 614 | `a9ad25a23e3b43a0bea5b8caeb3272baae4a5a1e` | 2026-07-28 | lin-xiaoyu | fix(symphony): 删除 json-repair 依赖 |
| 615 | `baf2070fcdb9566fcc3367d07a6a563e69de87ae` | 2026-07-28 | jack_ricky | fix(a2ui): 修复提示词作用域、流式渲染与表单交互问题 |
| 616 | `c04a63cc16b3e5b18032f97ac9801477750d6794` | 2026-07-28 | GYHHelloworld | fix: 过滤历史空白消息与心跳 HEARTBEAT_OK，避免压缩后会话被空行污染 |
| 617 | `c7295e00d50fd69409ca984f9e455933687adaf8` | 2026-07-28 | l_x_d | fix(context): migrate context processor configs to forked ratio-based fields |
| 618 | `cd546aba18e40ac73ab9674402eaad66a8b6430b` | 2026-07-28 | w1101627533 | fix: 配置信息按全局语言路由统一管理 |
| 619 | `cf475dd43490ee286a4bf07e337ecb6bb6fb109e` | 2026-07-28 | NickFuryXXX | fix(tui): seed session path so /resume finds new sessions |
| 620 | `f8998290274a2be0c550680df0b207b683e5065f` | 2026-07-28 | chensheng12345 | fix(tui-frontend): swarmflows running icon |
| 621 | `11382dba0f1627f4e13017f7d2587d498a9e561e` | 2026-07-29 | jingjinduo | fix:修复/memory编辑文件过程中交互逻辑 |
| 622 | `12ad6f651956a72be53d9710a97b2c86d138f6de` | 2026-07-29 | huanggawa | fix: capture subagent Observability |
| 623 | `3cf1f72ab435cf1ef4e1fa664e4aef5aeed045ba` | 2026-07-29 | hwxiajing | fix(cron): propagate LLM errors to frontend and persist in history |
| 624 | `52e90a95716319636905ead11df92891816aa4a9` | 2026-07-29 | cjh_jinduoxia | fix: 修复模型配置报错重复弹出、模型改名会话跌回默认、前端构建 TS 类型报错三处问题 |
| 625 | `5d110ef95069d2d44310d9bea0f9d264ecd5e1f9` | 2026-07-29 | ldstyle8 | fix(tui-frontend): preserve btw history navigation |
| 626 | `6020f1c7ff4bd68f092a0cfee280922438038f3d` | 2026-07-29 | zhengkailan | fix: 无项目新建会话误显示上一个项目后缀 |
| 627 | `6eb801f70ca009cbab94f65bb014dfd18e5ca04c` | 2026-07-29 | NickFuryXXX | fix(skill): ClawHub owner/slug origin, display_name uninstall, online source filter |
| 628 | `91df254722800ab03f6516421213e243340c43b0` | 2026-07-29 | zhengkailan | fix: enhance mouse tracking logic for terminal interactions |
| 629 | `98186f7a880d0f04cc02575e8c21a94908742985` | 2026-07-29 | NickFuryXXX | fix(web): allow clearing configured ClawHub token |
| 630 | `a42e6ddcba477ad012a37a1efe6cc903af9a2d84` | 2026-07-29 | XinchenZzz | fix(cli): swallow Ctrl+C traceback during dotenv load in CLI startup |
| 631 | `aff134bad32bdf5643f1bf88f6c9a7d6ee195bce` | 2026-07-29 | zhengshangyi | fix(web): 主对话展示优化：对话时间线交错展示调整&增加思考过程打印&调整工具调用展示逻辑 |
| 632 | `b722ee449f66eaa674ee06f1bf4615be6d509e08` | 2026-07-29 | zhengshangyi | fix(web): 修复主对话时间统计膨胀&会话消息折叠展开闪动问题 |
| 633 | `bfa4e789dc5ee241cc979f1dae478944a472af0f` | 2026-07-29 | chunbs_cbs | fix:TUI对模型判断与web逻辑不一致 |
| 634 | `ee793d017846a40562381da74d50f90f0ed814d0` | 2026-07-29 | chunbs_cbs | fix:接了飞书channel后日志里一直打印“消息内容为空，跳过发送” |
| 635 | `f812f60d1924397600016a1e8c30d817f6328385` | 2026-07-29 | openJiuwen-bot | fix(memory): "记忆系统敏感信息过滤"配置信息删除无关内容 |
| 636 | `10ef10ddf2edb2f8f9f30d38c7b60492a4e50a91` | 2026-07-30 | renquanjia | fix: correct typos found by typos scan |
| 637 | `1c2964272e562d75e9f2f01a74b6e4b278a8ff23` | 2026-07-30 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1827: fix(tui): 修复面板选择模型切换报错 - index索引与_raw_defaults不一致 (#2665) |
| 638 | `1c72867ec23f2fbeb6b3c5e47a03e7db90138070` | 2026-07-30 | renquanjia | [mirror] openJiuwen-ai/jiuwenswarm#1651: fix(memory): filter today/yesterday daily_memory from search results … |
| 639 | `4258eaccd9dedf50af17bcf3995e34b7662e8250` | 2026-07-30 | openJiuwen-bot | fix: 安全问题整改，1、ssl证书校验开关通过配置文件读取，不做硬编码；2、gateway通过配置文件加载三方服务的访问地址，取代URL硬编码的方式 |
| 640 | `4d36a08889f5fd54ee5e4f10671d6ab9050f5ef4` | 2026-07-30 | ldstyle8 | fix(tui): fully mask sensitive config values as ****** |
| 641 | `4da1a7fb451ff6bb97685d883358b49cb568edbd` | 2026-07-30 | CodehubTTG | fix: 修改top_p参数默认值为空 |
| 642 | `5d872ccbd71bb20c08991cace10adecbb5fb9570` | 2026-07-30 | wncbdj | fix(tui): clear stale context after session rewind |
| 643 | `745182afb26b7fefe7ea52c965a4384bb113c5b9` | 2026-07-30 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1870: fix(ppt-creation): repair two doc references that point at nothing |
| 644 | `795c4a9f506240003d530bc9fd473cb82a3bf5b1` | 2026-07-30 | openJiuwen-bot | fix: session.delete 路径遍历导致任意目录删除问题修改 |
| 645 | `7d87e0035d92d6b0f8943e9ad39f78d2e9bee95a` | 2026-07-30 | yechenzhen | fix(deploy): restore agentos_router and SSH config in yuanrong gateway template |
| 646 | `909aa43528d2bdf2b1c9f44245cf889002eb1eb4` | 2026-07-30 | ldstyle8 | fix: clear pending ask_user interrupt on supplement instead of answering it |
| 647 | `942757b7b4bb78092bf78aefbcaac5906091a097` | 2026-07-30 | wncbdj | fix(goal): 历史记录里goal任务不显示徽章 |
| 648 | `9b643f586ec2e6eb8e247700692011fcc4d1cc06` | 2026-07-30 | chunbs_cbs | fix:mcp disconnect逻辑存在问题，导致进程残留，cpu100%自旋 |
| 649 | `9c3fe3fc283c15d9f9ea5ebe42ca4be29fb9deb5` | 2026-07-30 | chenchunzhou | fix(gateway): /join falsely rejects with team_name mismatch |
| 650 | `b35507c182058102b167575ef2e57afd71f51c96` | 2026-07-30 | xiaotian077 | fix(tui): 移植行内 /skill 补全与提取逻辑回 develop |
| 651 | `b7111b57118590f45be4bfdb6af61106ad753520` | 2026-07-30 | cjh_jinduoxia | fix: 修复OpenAI账号未连接时模型配置报错文案误导问题 |
| 652 | `bd1db668a85525a9944e415c58ae458046b80703` | 2026-07-30 | min_gitcode | fix(agent_teams): include external cli agent dependencies |
| 653 | `cc5a09055a4985571f27f75246e92d20c8e6882e` | 2026-07-30 | hwxiajing | fix(frontend): don't remove team member on failed shutdown_member |
| 654 | `cffc12001d688eb2c96de9f8234a3031bf36a58f` | 2026-07-30 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1717: fix(diff): include sub-agent file changes in parent session diff stats |
| 655 | `ecbe9b149e514ddca508578c4437989e0a74bac4` | 2026-07-30 | l_x_d | fix(agent): sync runtime attachment with live mode |
| 656 | `edee3ee6b03b422316188fd24a172121ecf1c833` | 2026-07-30 | huanggawa | fix: add debug docs |
| 657 | `fde87e399623bf429b5d0951e8caf5f7577800ef` | 2026-07-30 | zhaozixuan | fix: Add back accidentally deleted code |
| 658 | `ff92c6a8c7aa00b301f7988707cafc88e23f51cd` | 2026-07-30 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1857: fix: persist chat.tool_result to history so tool status restores corr… |
| 659 | `06c4ed48c132cd44ca9807e26e7f83c49e8d6fe8` | 2026-07-31 | d30029696 | [mirror] openJiuwen-ai/jiuwenswarm#1721: fix(cron): fix(cron): one-shot job missed trigger within 10s window schedules... |
| 660 | `1659d6f4993f51391e0c63e232815bade168a44b` | 2026-07-31 | wncbdj | fix(tui): /swarmskill-creator命令失效 |
| 661 | `59b87ca3becd733ff96c4aadf2b80a8f1246d1fc` | 2026-07-31 | w1101627533 | fix: 增加文档上传和解析功能 |
| 662 | `771f6cec0f48f54d5ba95c099d6ec3cce6643c12` | 2026-07-31 | kdgmpw | fix(cron): 增强执行的可靠性，以应对崩溃恢复和空结果 |
| 663 | `7b69fdeb9e9f47bc32f390ace19f655beab865c6` | 2026-07-31 | openJiuwen-bot | fix(team): enforce generated team name format |
| 664 | `a0d34e34cd58a812bea44e28712767fa69fd7192` | 2026-07-31 | openJiuwen-bot | fix(tui): 统一 /memory 显示路径盘符为大写 |
| 665 | `a4b8d5fc93b857f9b6dc8cc9113e12cf04c83b95` | 2026-07-31 | liangfuqi | fix(cron): 修复飞书定时任务超时失败但是任务继续执行 |
| 666 | `b911067f3152cd59d4e5aae68314570f17d9c719` | 2026-07-31 | xiaoyao42 | fix：文件上传错误提示3s自动消失 |
| 667 | `d3047e1db8d2ebf24b12b50bd08526b300eb98b5` | 2026-07-31 | openJiuwen-bot | fix：/memory edit命令可以在记忆目录下，随便创建文件。功能冗余 |
| 668 | `147433c0c3171cb97d3f1882441a3fb46d851c62` | 2026-08-01 | alan_cheng | fix(server): release adapter-held SysOperations on cleanup |
| 669 | `1928ca5c488ca20bc1f823bea49087b3e5e45045` | 2026-08-01 | NickFuryXXX | fix(ask_user): block empty Other and reject non-array options |
| 670 | `2c1c387cc78bb92df8bd20a0a80d06371feccdc8` | 2026-08-01 | XinchenZzz | fix(cli): move parse_dotenv_early into main() try block to guard Ctrl+C |
| 671 | `5e2b56d01c0d08d15e391025209ed6afef387d3d` | 2026-08-01 | douran | fix(agent): prewarm sessions and centralize allocation |
| 672 | `5fa710897f15cd3920f607b07264174d13bb6358` | 2026-08-01 | alan_cheng | fix(server): disable session prewarming by default |
| 673 | `a7faf106a2350a75c3d247d27ed9183349ec5c93` | 2026-08-01 | douran | fix(agent): prioritize foreground chat over prewarming |
| 674 | `ae6e1dac16f32bab481d1d141b1381dcee566e5b` | 2026-08-01 | alan_cheng | fix(server): bind StageTimer's clock as a module attribute |
| 675 | `b595235fd4d80320b3ad70c911914230a02b1a13` | 2026-08-01 | amelafejza | [mirror] openJiuwen-ai/jiuwenswarm#1614: fix(web): recover from malformed team panel localStorage state / 修复团队面板因畸形 lo... |
| 676 | `bcc368d4449e4f34b854cd9ce4e7e5b8f01d169d` | 2026-08-01 | z2tng | fix(swarmskill-creator): support workflow and budget operators |
| 677 | `e4962e18b10da2feb7ffdb1a4f22a393b147edff` | 2026-08-01 | alan_cheng | fix(server): scope tool registrations to the owning adapter |
| 678 | `e907fa4b04333159ed7a3c90b8cda9e07a83b8e2` | 2026-08-01 | alan_cheng | fix(server): re-take the git snapshot when the checkout moves |
| 679 | `e9c83e729bf04106f205e367385b7e5728ecc0d0` | 2026-08-01 | alan_cheng | fix(server): make stage breakdowns visible below the slow bar |
| 680 | `f6d7f652f4316a4974c2bfbfb25eaa5453111827` | 2026-08-01 | alan_cheng | fix(server): make the injected git status a real conversation snapshot |
| 681 | `fb5ac03b58966d8a1402871f2df6b9585b2602de` | 2026-08-01 | NickFuryXXX | fix(cron): bind DingTalk scheduled push to originating session |
| 682 | `0222ed4f3e7d6f082ff7d8e2b945d74a831ff869` | 2026-08-03 | douran | fix(tui): route startup sessions through agentserver |
| 683 | `1158899f8da34cdc59b0c08ed0a1224ff29e1587` | 2026-08-03 | tangfei0306 | fix(agent-teams): we can retry when humanagentinbox request is failed as leader request |
| 684 | `68f0b374d096bdd4048ea6352de1296f59a85c7c` | 2026-08-03 | alan_cheng | fix(server): enable session prewarming by default |
| 685 | `7597f91794522accabd47cbde948728530ec52e4` | 2026-08-03 | zhengshangyi | fix(web): 统一对话耗时展示并修正 team 头像混乱问题 |
| 686 | `7fa1a8ee4fb3cb95b747ceaf281b012631dd09ef` | 2026-08-03 | yechenzhen | fix(deploy/yuanrong): force gateway up under /root when JIUWENSWARM_HOME mismatches |
| 687 | `ac05890261b06402d5fe76fccd88fd4276c63f9c` | 2026-08-03 | MengHan18 | fix(ascend affinity): prevent reentrant session switch races and isolate async KVC lifecycle signals |
| 688 | `af3bebf1e55502c839675a1cec5737916b3bf3c3` | 2026-08-03 | xiaoyao42 | fix：修复iframe导致拖动时焦点丢失导致卡顿的问题；修复md文件内部跳转打开新浏览器页签的问题 |
| 689 | `df41a41abef6323e9d7dba8cd1eb9761d1d0fc05` | 2026-08-03 | zhengshangyi | fix(web): align live/history chat UI for tools, files, and turn duration |
| 690 | `f3ba948a2c39d249add361709efcedb44cf6611c` | 2026-08-03 | openJiuwen-bot | fix:发现父级目录中的md文件时，未展示出路径，存在歧义 |
| 691 | `f5c34c9f7b6c4fc5c5a4269da6bc7a79b3dd7613` | 2026-08-03 | openJiuwen-bot | fix(tui):Agent模式下在toggle和status面板移除/memory_proactive相关显示 |
| 692 | `1a4f146845deec121858244358841bd580856c9e` | 2026-08-04 | openJiuwen-bot | fix：过滤敏感信息开关功能实现 |
| 693 | `2ad172a7e2af4f9999cd95885faea5a96351439d` | 2026-08-04 | alan_cheng | fix(swarm): render one user-turn envelope for single agent and team |
| 694 | `2d7682b1e37729b340012ef898faaf5e3b787b56` | 2026-08-04 | wncbdj | fix: 配置中间流式帧超时时间为120秒 |
| 695 | `35ac94e6aa2bc8d92ffe63a6b7b69f78b36fd2d0` | 2026-08-04 | ray_le | fix(auto_harness): 同步修复auto_harness 代码引入路径 |
| 696 | `5806431007289e760f338e15192928c4378535ac` | 2026-08-04 | MengHan18 | fix(session): remove stale prompt attachment loader dependency |
| 697 | `5a7319a4d6bc28a120bd703a67eaf8037453af16` | 2026-08-04 | MengHan18 | fix(PromptAttachment): remove unused file hot-loading support and stop creating unused workspace directory |
| 698 | `6f2b80ae50af187563b422b3bf9d618dd0ca5933` | 2026-08-04 | l_x_d | fix(context): repair compression events and honor context window config |
| 699 | `77c0b3b640674d99f86ddefab009638ab7b1c462` | 2026-08-04 | openJiuwen-bot | fix:memory edit 面板存在的默认占位文件被允许创建 |
| 700 | `a46b9e04e508b56dab413ceed778fffd685d1646` | 2026-08-04 | openJiuwen-bot | fix(deps): 升级 5 个开源依赖至漏洞扫描所需版本 |
| 701 | `b097f5373bd93d780fc0ee5527dbfe3b48b1650b` | 2026-08-04 | iamcandiceguo | fix(team): team.error is map to chat.error |
| 702 | `be1f650e6a45950ddc89c7ad7e358f576f999f41` | 2026-08-04 | alan_cheng | fix(swarm): deliver uploaded attachment paths to the team leader |
| 703 | `ccdf2647aa3f2cb90ada5081b5efd0fa05bdf15e` | 2026-08-04 | openJiuwen-bot | fix(team): make team name generation resilient |
| 704 | `dc45102b7dfd2807a05a25cd5fe25519c382c47e` | 2026-08-04 | alan_cheng | fix(gateway): keep non-ASCII names for uploaded files |
| 705 | `e05df1262fdf730d45ca82ba7d6c750e9096b1fd` | 2026-08-04 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#2163: fix(browser subagent): fix bugs of browser subagent tools |
| 706 | `e7600032d0a70857fb0f81277beb0af3f137c999` | 2026-08-04 | ldstyle8 | fix: bound TUI session memory usage |
| 707 | `08d7615e30a7181eaa720d38e8b8a74e7dcd4113` | 2026-08-05 | lzf2025 | fix(memory): inline non-blocking coding memory rail |
| 708 | `155c7cdef1ae62d3d398fc1420e922bfa2a40f82` | 2026-08-05 | alan_cheng | fix(instance): stop lingering sockets from shifting the port group |
| 709 | `39242c1dcb811806c90263f3489f000a4e8483e7` | 2026-08-05 | wncbdj | fix(goal): 历史消息缺失、徽章折叠 |
| 710 | `50d2ee6c34afeb98321355e5a3de76d8435f2fda` | 2026-08-05 | Ziliang_Zhou | fix: support jiuwen_memory(mem2.0) provider |
| 711 | `5bd10fb15045503cbd043fc90aa3d417ee45bc84` | 2026-08-05 | alan_cheng | fix(swarm): keep routing tokens outside the rendered envelope |
| 712 | `6807daf8736971910831d10a7d0fbce9e09b45fe` | 2026-08-05 | alan_cheng | fix(swarm): envelope only team-wide input, never member deliveries |
| 713 | `881716ea84f154b4fda15fd00c76354404ea8d1e` | 2026-08-05 | jack_ricky | fix(a2ui): 隔离 Team 成员级 block finalization |
| 714 | `9aa5b2311d55fbc4d8395a005d7416c5747ae721` | 2026-08-05 | alan_cheng | fix(swarm): refuse team rounds on non-streaming channels |
| 715 | `9e93caa450e4b67a3fb8da7ef570d5e2c68c2a31` | 2026-08-05 | alan_cheng | fix(observability): keep single-agent llm.call spans findable across tasks |
| 716 | `ae598fc14404f3faa46bda503e96568df1e43bc8` | 2026-08-05 | openJiuwen-bot | fix:给.jiuwen文件夹不可写权限，创建 User memory时，tui并未报错，只有编辑器报错了。应该tui直接报错 |
| 717 | `b2219706efd141a4267895b2c1c9bbf5254e0d4a` | 2026-08-05 | alan_cheng | fix(observability): end the single-agent root span before the child flush |
| 718 | `b5c95ec7768526aaa477d84f73b6eddb881802f1` | 2026-08-05 | min_gitcode | fix(team): reset paused team runtimes on lifecycle changes |
| 719 | `b8e6b8aedc26044003c2ec2dd6c8c1214df8fea3` | 2026-08-05 | alan_cheng | fix(subagent): inherit the parent agent's sys_operation |
| 720 | `b98b6e7cd2ae7d50fcb951e1c109b0fdc7f4daa8` | 2026-08-05 | alan_cheng | fix(app): terminate child services on SIGTERM |
| 721 | `c62d9097c9babd31ea89f603379de7b5592832cb` | 2026-08-05 | openJiuwen-bot | fix:上次选择了某内存文件后，再次打开memory面板，应记住上次选择的选项 |
| 722 | `df5ca426f7d4a26e717ac1aeaeadc22bc78f048a` | 2026-08-05 | alan_cheng | fix(swarm): give team members the request permission context |
| 723 | `e49f10a6dc196c58f6cf7391788a37aecec10628` | 2026-08-05 | xiaoyao42 | fix：解决右屏闪烁的问题；修改工作菜单名字 |
| 724 | `0da6728d80047dce7f5cfabb0aecd377e3519331` | 2026-08-06 | xiaoyao42 | fix(web): restrict artifact image previews to supported formats |
| 725 | `11bdd02805ed0c1a0a6e8dc65ff140bbe6861d68` | 2026-08-06 | chensheng12345 | fix(tui): align swarmflow agents back navigation |
| 726 | `29d3d95a185a7fb9b3b472c8714f7a111c22d8f9` | 2026-08-06 | xgg1227 | [mirror] openJiuwen-ai/jiuwenswarm#906: fix(agent-adapter): persist cancelled todos on interrupt |
| 727 | `371ce4397f0d5f76a2c346efe1fd32f74817ec44` | 2026-08-06 | alan_cheng | fix(agentserver): warm the image modality probe cache |
| 728 | `4a4533237e4d53c933ffb736ecb0b3e6f6a6f3ef` | 2026-08-06 | alan_cheng | fix(observability): attach the subagent rail at creation, not only in debug runs |
| 729 | `5cbf0be2c2ccebf61d51d66dcddb47911c350123` | 2026-08-06 | alan_cheng | fix(observability): nest a task-tool subagent under the dispatching agent |
| 730 | `607fddd1d435fd2d8f77d9722d261fd0d5fbb8c3` | 2026-08-06 | yechenzhen | fix(agentos): repair Web/TUI token auth switch and connect hooks |
| 731 | `66db66ebaa3b28dc0341ee4a3c8f4e3f944f04f8` | 2026-08-06 | kdgmpw | fix(web): allow branch switching with untracked files |
| 732 | `6708c66891d550c4b4102ad8473b12221a25c143` | 2026-08-06 | chenchunzhou | fix(swarmflow): command completion, phase agents view, budget status and --budget none |
| 733 | `81552d7a19ad87e5447bb843fffb3212ab304235` | 2026-08-06 | xiaoyao42 | fix(markdown): render LaTeX with KaTeX |
| 734 | `8682c885781f5544130f91f3f5e8afda5727ba96` | 2026-08-06 | alan_cheng | fix(observability): key the root-span fallback by session, not one global slot |
| 735 | `87cdbaf3e90003c1ffb289f62c950e982cdd944d` | 2026-08-06 | CUIZIBO | [mirror] openJiuwen-ai/jiuwenswarm#1886: fix:浏览器/whl实现通过弹窗选择目录的绝对路径 |
| 736 | `bd86b2160a476df0866a825e53a212e6ac6b63d6` | 2026-08-06 | liangfuqi | fix(prompt): 提示词增加约束，优先使用svg 作为输出 |
| 737 | `cba421f19d49a21affb1fcf1af3f438cf3831433` | 2026-08-06 | xiaoyao42 | fix: 为 Office 预览补充跨平台字体设置兜底渲染逻辑 |
| 738 | `d1dd6296003e2f46bc160ff9e96c91efa5b9af96` | 2026-08-06 | Huang, Zeyu | fix(media): return early when no SendFileToolkit is bound |
| 739 | `e4fae3061523f458dccfd3f28a48853c832928d2` | 2026-08-06 | yan_zj | fix(prompt): Apply prompt improvements |
| 740 | `eb912aa81ae21e680c510eea34df6396dd989e42` | 2026-08-06 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1081: fix(session): prevent metadata clobber that erases session identity |
| 741 | `fbb1d0431f71e2a34b31b1716e7e439b2eabc58f` | 2026-08-06 | Huang, Zeyu | fix(media): session-scope SendFileToolkit bind to avoid misdelivery |
| 742 | `fd9876151ba958e51db6cd3ff3eededaa9200444` | 2026-08-06 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#373: fix(web): Model validation falsely fails for local Ollama models |
| 743 | `09e27088a74c7fa1353f92815954f7f60065df2c` | 2026-08-07 | xiaoyao42 | fix(svg): preserve incomplete SVG previews after streaming |
| 744 | `1a27c097645783a0e33ce874cab0b291686fc2c9` | 2026-08-07 | haowenzhong1 | fix(agentos): propagate user_id for faas sandbox and harden history reload |
| 745 | `2c5b43ff44fc56ec9b84bdf19f1353ea3dee478c` | 2026-08-07 | Haowei_Du | fix(proactive recommendation): fix the bug of cover the latest agent output |
| 746 | `2f916612d7ab74892380ab625c6037d0541a5ab0` | 2026-08-07 | openJiuwen-bot | fix(tui): prevent Vim hang in standalone Git Bash |
| 747 | `37b330822ca1972d32e1bc68ed74187cdd4cbe1e` | 2026-08-07 | openJiuwen-bot | fix:auto-coding-memory |
| 748 | `4d60d7931dfac0cb1fd949abc28bea4341e6b4d6` | 2026-08-07 | haowenzhong1 | fix(agentos): disable session prewarm by default |
| 749 | `68e55293293ba540d49f803e389ee6b210e0146e` | 2026-08-07 | alan_cheng | fix(tools): honor blank endpoint overrides for web and video tools |
| 750 | `690e3197822136dcc17a66810ab47e612ddf8c21` | 2026-08-07 | openJiuwen-bot | fix:/memory选中记忆文件打开编辑器，编辑后保存。memory列表依然打开，没有任何编辑成功的提示 |
| 751 | `6bae51a6f4ec87296f7953dcd4369dc089ac009d` | 2026-08-07 | min_gitcode | fix(team): report paused runtime stop result |
| 752 | `6cb03dba3fe4645b10264ac72005acaded7593dd` | 2026-08-07 | wangxiaolong100 | fix: introduction.mp4 update2 |
| 753 | `7062a05a514d6ef261100ece5a51ae5d307fdf9d` | 2026-08-07 | wangxiaolong100 | fix: introduction.mp4 update |
| 754 | `73671e897a4c4a04abf16bd1f36a538164f2190e` | 2026-08-07 | cjh_jinduoxia | fix(goal): 新会话设目标时用户气泡立即落地，不再被推迟到回复下方 |
| 755 | `79c3b41f791cb791b2847885408e23181b4d13ce` | 2026-08-07 | openJiuwen-bot | fix：coding_memory 同步实际记忆文件保存地址与索引文件一致 |
| 756 | `8311f26d2c4882bf62606a4d6a7a4687aff928f1` | 2026-08-07 | xiaoxiawang | fix:修复浏览器文档 |
| 757 | `85062dd5e8190d8c30fb225238ca4094cd1977e3` | 2026-08-07 | d-0771 | fix(agentos): fall back to X-User-Id when auth disabled and tune builtin agent resources |
| 758 | `8c4fdfb0ba8985237e11a4fac9680cb78a2c0299` | 2026-08-07 | ldstyle8 | fix(agentos): stop auto-writing .agent_history to project .gitignore |
| 759 | `9c3fec02ac82d5361c19f0cb3e0f591e50ea8ba9` | 2026-08-07 | lzf2025 | fix(memory): keep coding memory in agent workspace |
| 760 | `b95e42651143c5ed7e0e061e8567ea16bf8aab65` | 2026-08-07 | yechenzhen | fix(agentos): run jiuwenswarm-init before agentserver in YuanRong cmds |
| 761 | `f6deb641ee5365e0229e60a28af3f4105c786576` | 2026-08-07 | z2tng | fix(evolution): unify skill self-evolution controls |
| 762 | `c563f35687f4a2f906f1d20a5525fa7f1650e0df` | 2026-08-08 | chenchunzhou | fix(swarmflow): count terminal agents (failed/stopped) in completed_agent_count |
| 763 | `129d13aa0ce8a60aacde46bdb049dd110a998fa6` | 2026-08-10 | zhengkailan | fix(web): enhance session state management to include paused status |
| 764 | `348bce63b9db06c033739809befe611c296ef5b9` | 2026-08-10 | haowenzhong1 | fix(agentos): remove hardcoded third-party agent fields from /switch command |
| 765 | `366e3f8fe7416a472e65f8bef5d682d6db9550c2` | 2026-08-10 | xiaotian077 | fix(server): 针对xiaoyi_claw的环境 进行patch |
| 766 | `64cf8aafac279dc8f2bd70c80f6e5f842b4e4cb0` | 2026-08-10 | w1101627533 | fix: 优化文件上传功能 |
| 767 | `7cd46dea8bdc2b7fc3989c76f23351d127fd8858` | 2026-08-10 | chensheng12345 | fix(tui): auto-exit swarmflow pending list and wrap file-viewer content |
| 768 | `7d4a8fb7177d56510a6902d8ab4b6d5f611e47cb` | 2026-08-10 | zhengkailan | fix(multi-session): enhance delete conversation logic to prevent action during processing |
| 769 | `801873920d748eb176bd6c6f60a4716cd9c3b19c` | 2026-08-10 | lin-xiaoyu | fix(symphony): 修复模型不可用时swarm图谱构建仍然显示成功的问题 |
| 770 | `a4f3d84794c8945150455eccd34d5994df736e5a` | 2026-08-10 | haowenzhong1 | fix(agentos): allow down flow to continue when yuanrong is not running and uninstall both jiuwenswarm and openjiuwen packages |
| 771 | `b57e69b860ffe682bbcb3e11b369a3a75b4b8934` | 2026-08-10 | xiaoxiawang | fix:技能文档修复 |
| 772 | `be378ddb6e7c1f74bf272301e2b0511b801b99cd` | 2026-08-10 | liangfuqi | fix(web): 删除用户目录下的dist路径作为web前端的启动路径 |
| 773 | `c7d970c5fe8b261553f6a8635071b8e60a9b026a` | 2026-08-10 | yuhaiquan | fix：立即执行后广播消息不自动显示问题 |
| 774 | `e99884eae0ab9290659fdbce0c67749ac6eb4a9e` | 2026-08-10 | zhengkailan | fix(InputArea): display project directory error message in the input area |
| 775 | `eab50afec6c3adcfc553d85cc1fe7c0322ea0209` | 2026-08-10 | huanggawa | fix: remove agentteam metadata info for single agent mode |
| 776 | `fb43da6cf282b1d513be80957f3a49dca557b8d5` | 2026-08-10 | zhaozixuan | fix: return ip address when create sandbox |
| 777 | `200682b374da8bb517bb44b7d68a1e2872d4c304` | 2026-08-11 | renquanjia | fix(agent): 自定义 Agent 名称首尾空格导致保存和查找不一致 (#1279) |
| 778 | `3f3cdbb7f45fdd29e7d03deafa5bca10e363434e` | 2026-08-11 | ray_le | fix(tui): auto-harness 补全及超时问题修复 |
| 779 | `7e7063a1a98b53cc0e6d67265a97eb3b2336a912` | 2026-08-11 | xiaoxiawang | fix:修复agent teams及记忆日志相关文档 |
| 780 | `819a66e8720243ff3ee5d8a3da9b6398c0d0dc9a` | 2026-08-11 | openJiuwen-bot | fix(gateway): clean up stale websocket team subscriptions |
| 781 | `91c9f367a8308ac95f9b688905fa76f3eabd55e7` | 2026-08-11 | chensheng12345 | fix(web): sync discarded turn review state |
| 782 | `ac6e16f887db217e29cf094db4fc4af0efe29b0e` | 2026-08-11 | haowenzhong1 | fix(agentos): remove pip uninstall from down flow to avoid unnecessary package removal |
| 783 | `c3fdda8c1a920bc98ad3abb89d47abdcf62e1d4a` | 2026-08-11 | liangfuqi | fix(gateway): 飞书普通通道纳入 session_map 复用，修复每条消息新建 session 丢上下文 |
| 784 | `d7a2cd0ebe7c1ae36b6cf4abe2a2aa340c02275e` | 2026-08-11 | yechenzhen | fix(agentos): 3rdagent.switch 等 sandbox sshd 就绪再返回 |
| 785 | `dbaed1529d2d68fdf2b9d775212e3bfc2157ee98` | 2026-08-11 | yechenzhen | fix(agentos): yuanrong template 默认开 TUI 鉴权，Web 不挂 hook |
| 786 | `e2aca579824c99579f206a967abe3a56fbd46dd5` | 2026-08-11 | chensheng12345 | fix(tui): leave session-detail when its workflow run finishes |
| 787 | `fa545d128c9e98f8a90d55ecc2d1f3238f17a4a6` | 2026-08-11 | SushiNinja | [mirror] openJiuwen-ai/jiuwenswarm#2558: fix(ws): E2A通信未传入work_mode时使用默认值 |
| 788 | `0420d214f4fecdadac53fd3640c73cb9b5390a54` | 2026-08-12 | XinchenZzz | fix(web): handle unavailable work mode storage |
| 789 | `12fb54520ceda34bcc48a81ed98d56ea297ea4af` | 2026-08-12 | liangfuqi | fix(feishu):修复音视频下载报错 |
| 790 | `2781229e4142170e5982dbb12e09d6f8bb3e8c32` | 2026-08-12 | iamcandiceguo | fix(team): team_helpers modify team.completed |
| 791 | `2860abb2ec5834403d701c48d278226bb33bb370` | 2026-08-12 | l_x_d | fix(context_engine): 补充上下文配置注释 |
| 792 | `29499243136ab6e90285f71f00370c02169da118` | 2026-08-12 | zhaozixuan | fix: excluded_commands support hybrid command |
| 793 | `2d726f688cd25b9abe6cf32778841e4b9b458492` | 2026-08-12 | xiaoyao42 | fix: refine inline SVG output, preview, and export |
| 794 | `44cf890cc129242929cc188ee8d664fa3eb30302` | 2026-08-12 | wncbdj | fix(todo): 页面刷新，todo任务列表为空 |
| 795 | `4a462489d318fe1f9759826692ad2e630e1d1918` | 2026-08-12 | xiaoyao42 | fix(web): render SVG attachments as files and hide Agent config tab |
| 796 | `4ea044a6c4a232d8e014caab0c4ccab9b1fdfa77` | 2026-08-12 | layShang264 | fix(symphony): persist session feedback across Skill reuse |
| 797 | `5f05ee6c132c0435672a42af2cdf996b612729e9` | 2026-08-12 | chensheng12345 | Fix code environment status display |
| 798 | `68a5f49198971bf84614f3611c61230329bd8038` | 2026-08-12 | zhengkailan | fix: clear stale project for ordinary new conversations |
| 799 | `961740443825c674707dfa33d46bd420c87727de` | 2026-08-12 | yechenzhen | fix(cron): propagate user_id for AgentOS sandbox routing |
| 800 | `9f6fc1681dc5f69176f42dfafe58d6fd7b7869f3` | 2026-08-12 | cjh_jinduoxia | fix: 修复启动2个会话后目标的编辑/暂停按钮卡死不可点的问题 |
| 801 | `b5eca0c3b1563476ca51de6f833b93ed9618217c` | 2026-08-12 | haowenzhong1 | fix(agentos): switch TUI to remote mode and replace new session placeholder with unique UUID |
| 802 | `ba7a51dc3694ed45a960053cead9db047f8d9cae` | 2026-08-12 | openJiuwen-bot | fix(team): relocate bindings store and migrate legacy data |
| 803 | `f721f7cb394d0303f623f74f10efe89934cabd94` | 2026-08-12 | wncbdj | fix(rails): adapt model anomaly detection rail for rectification |
| 804 | `ff9227caabc4bf6b998732a81b2f47331a2290ca` | 2026-08-12 | ray_le | fix(mcp): 修复 MCP 连接失败导致 agent 无法对话的问题 |
| 805 | `0cd99b2ccc08154f5760ce7fb4b53b175ed15258` | 2026-08-13 | kdgmpw | fix(tui): session自动更新project |
| 806 | `1dfabf57a2f10886a88dc5c0088c2bea2c50de33` | 2026-08-13 | chenchunzhou | fix(swarmflow): clear stale waiting task card for unrun workflow phases |
| 807 | `2edc46815aebbaacbc6f3a9ed8e2a566efc16887` | 2026-08-13 | yechenzhen | fix(agentos): default IAM auth URL to master host:8090 |
| 808 | `4b91f648c4af5d9007afd63a7c44819fe5ffc27c` | 2026-08-13 | Lexie-7 | fix:TUI看门狗以 ws 连接状态替代公网探针判离线，修复 air-gapped 内网误报 |
| 809 | `667217ff943a541751f1a02dfbdc73d7cd9e30f3` | 2026-08-13 | xiaoyao42 | fix：统一系统中文件图标显示 |
| 810 | `6820b81905d91658ad45257485ef7fedf28c215d` | 2026-08-13 | xiaoxiawang | fix:修复定时任务文档 |
| 811 | `7204fc8a8af21d2bcfcbc6d5042ee60231ad9e83` | 2026-08-13 | Lexie-7 | fix:模型配置增加agentos字段，支持用户自定义输入上下文长度max_tokens |
| 812 | `84eb83ce1ca22c4862229d9ceb105904c29c6161` | 2026-08-13 | liangfuqi | fix(web): reasoning 每帧记 updatedAt，修复 LLM 思考非正常结束时任务用时异常 |
| 813 | `88a25a8598667d4695a1dfc730fa9b980667f865` | 2026-08-13 | wangcan103 | fix(tui): expose /new as standalone slash command Refs #3116 |
| 814 | `8ded3cd530f2a3f967e410599b80453ac50375ae` | 2026-08-13 | z2tng | fix(jiuwenswarm): restore evolution trajectory capture with current agent-core |
| 815 | `9137e555a8d2cdf790909cfad19b877257afae34` | 2026-08-13 | openJiuwen-bot | fix :rewind恢复会话，功能不可用 |
| 816 | `aee2b0a2fb7c980860b468ec4d30c43cb0ad8730` | 2026-08-13 | wncbdj | fix(plan): plan执行下一步没有触发审核 |
| 817 | `bdf2773557503c097a06fa9f8d960fb478f6dfd4` | 2026-08-13 | openJiuwen-bot | fix tui一直在working，esc无法暂停，提示：Request Interrupted，却一直无法暂停 |
| 818 | `c2f5970c4847eb3c5802a1bf8a3369d7a4e15cd8` | 2026-08-13 | lin-xiaoyu | fix(symphony): 总谱改为图谱，迁移编排配置到技能图谱标签页，便捷用户操作 |
| 819 | `c45f145bb61bce9111cbd4ab993b232082f39a1d` | 2026-08-13 | z2tng | fix(observability): fix observability import |
| 820 | `c8d8df1acb49198c6d3eed2e330ba863600bf33a` | 2026-08-13 | chensheng12345 | fix(tui): return to chat after swarmflow reply |
| 821 | `d6468791a1997a77be551eeae4bffb8c7b5ce45c` | 2026-08-13 | ldstyle8 | fix(hooks): propagate session id to user hooks |
| 822 | `e0f63cdba8ff0ccec471c90af66a59c851ea19e0` | 2026-08-13 | openJiuwen-bot | fix(team): materialize delivered files in project directory |
| 823 | `eb21acf958985a1cbe95925f50b68b1bbffbfe67` | 2026-08-13 | openJiuwen-bot | fix(swarm): relay external CLI team events through active channel |
| 824 | `f5dc352c472dc4c57368bd00fd94beb837f5ae83` | 2026-08-13 | openJiuwen-bot | fix 648 /statusline，未配置时，无参数执行直接报错。而cc会调用setup agent，引导用户配置 |
| 825 | `fc110aafb954aa6d99c886dc8f4e4fdf71973885` | 2026-08-13 | ldstyle8 | fix(btw): route queries through session adapter |
| 826 | `32691916b2edccd04b0761d3d0dfb4786d9ff513` | 2026-08-14 | ldstyle8 | fix(agent): Prevent silent termination due to task-loop timeout |
| 827 | `3e134ba39dfce15461369284619978ff246cf3e9` | 2026-08-14 | xiaoyao42 | fix：修改文件卡片大小，修复历史数据竞态关系 |
| 828 | `5b51c13af4a113f6c7bf17be897ea3f8af68c7e5` | 2026-08-14 | MengHan18 | fix(session): allow internal session IDs with underscore prefixes |
| 829 | `74128efb279f664e62fb7abc9ef261aace1180f6` | 2026-08-14 | Tiffany_F | fix(skill): Upload the test ID related to the skill |
| 830 | `87c2d620e58c132543752354b37215c3a90797bb` | 2026-08-14 | z2tng | fix(swarmskill-creator): enforce portable child workflow paths |
| 831 | `8e7d7c8dda2107ef0ae09854659e67525fa6093f` | 2026-08-14 | xiaoyao42 | fix：remove guide button, and add skip button to initial interactive guide |
| 832 | `a31168964af4cc2450700e296b99317b2dd0b689` | 2026-08-14 | ldstyle8 | fix(debug-trace): preserve agent plan dump directory |
| 833 | `c41206f923b74ad496613cebd9ceaf3f19c4b6f4` | 2026-08-14 | Lexie-7 | fix:支持models.agentos配置多个模型 |
| 834 | `de431f1f4df6f44f39ecb204a47a6fce87fd2c86` | 2026-08-14 | xiaoxiawang | fix:修复频道文档 |
| 835 | `e1d0123af269d0592d0b8d69dc049888cba385a7` | 2026-08-14 | wangcan103 | fix(tui): preserve budget dict in workflow list summary Refs #3115 |
| 836 | `e509f7696173eee823c7af5bcfeb6200d0a49fbf` | 2026-08-14 | liangfuqi | fix(tui): 下掉 /mode 的agent.fast 命令 |
| 837 | `fe616de23d9261f3abf28b161c82751a8926fd81` | 2026-08-14 | xiaoyao42 | fix(share): export complete long conversation images |
| 838 | `4a1f64ffb73b1e81665a25fdb448383e817a36df` | 2026-08-15 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1661: fix(gui): Glyph Descenders Clips / 字形下伸部裁剪 |
| 839 | `9d6e73cab5864aa8577010ce1c9c1c36a265ebe1` | 2026-08-15 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1664: fix(GUI): fixing model provider icon matching for exact model names / 修复模型厂商图... |
| 840 | `a95b0ce3125c9c119ee72680e36e5cd4cc92faec` | 2026-08-15 | SushiNinja | [mirror] openJiuwen-ai/jiuwenswarm#3053: fix: reload env variables when reloading agent config |
| 841 | `ab3780f5bc85b25c2320ff75664e370c4aa5cd0c` | 2026-08-15 | zhengkailan | fix：Update config save logic to block during processing or global tasks |
| 842 | `ac46349ca2421593bcca687d52bef7ed48cb90fd` | 2026-08-15 | lin-xiaoyu | fix(symphony): 修复symphony_compose_graph工具调用超时的问题 |
| 843 | `c36194dcd087ebfd245efb14619ed89b49a8a4b0` | 2026-08-15 | xiaoyao42 | fix: 统一使用全局安全护栏开关控制Team |
| 844 | `e654d9c3d46e5cd1db8d597d0867b995f3903163` | 2026-08-15 | wangxiaolong100 | fix:  linux系统容器化拉起服务报错 |
| 845 | `058e4b35bf0ae31d8925be2618d1e9b2e45a6ee5` | 2026-08-17 | XinchenZzz | fix(installer): prevent Windows uninstall while the app is running |
| 846 | `05f67034077f1546d8aeb4e19eafd425c9864660` | 2026-08-17 | lin-xiaoyu | fix(symphony): 编排工具调用加固 |
| 847 | `1b070a20637d97b3e1cac2ea458c2a3f44c33222` | 2026-08-17 | ldstyle8 | Fix: permission interrupt classification for query tools |
| 848 | `1e34384de7d66efe00685253b78c9609e53387b3` | 2026-08-17 | kdgmpw | fix(cron): 修正 cron 工具描述的 dow 语义，消除 Quartz/croniter 编号错位 |
| 849 | `267c940fb120aea595106d41ec9babe69082873d` | 2026-08-17 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#3116: fix(docker): make standalone web image runnable |
| 850 | `2af4b56640111aa943b4576a97adca8272bf5ee4` | 2026-08-17 | min_gitcode | fix(team): tolerate team directory cleanup failures |
| 851 | `4005d05fea1cc26923b7b3072620a8067e326c43` | 2026-08-17 | yan_zj | fix(tool):Introduce `tool_search` and set infrequently used tools to `deferred`, so they can be discovered through `tool_search`. |
| 852 | `4815c3cffa56f35217ec85f58adcec382a4780a0` | 2026-08-17 | lzf2025 | fix(team-code): isolate coding memory from workspace tree |
| 853 | `63493236fd4d906072c049b211df036708eb32c6` | 2026-08-17 | huanggawa | fix: fix debug dump's modename |
| 854 | `66d499b94329e69e9bcefee729d6dcc1d17f2a4b` | 2026-08-17 | lin-xiaoyu | fix(symphony): 切换主对话模型，导致使用旧模型构建的图谱失效 |
| 855 | `6702322a574c7d3c404c4a48e630be080a6cb9ca` | 2026-08-17 | Haowei_Du | fix(proactive recommendation): fix the format of proactive message |
| 856 | `6ba9e9c190f82bbd0ad4ba8c9f930899473164cf` | 2026-08-17 | lin-xiaoyu | fix(symphony): 优化图谱操作与新手说明 |
| 857 | `74543da0a0a6f01230d507aa199ea349888fb144` | 2026-08-17 | openJiuwen-bot | fix:已经通过/mode切换成了team模式，auto-recap显示模式还是code.team |
| 858 | `771fb8c3d1285d898bf21b7fb6beb65cae0b4df1` | 2026-08-17 | ray_le | fix(harness): 修复harness package加载后工具偶现不存在问题 |
| 859 | `773dd67ba08d5c961c36f3003458aae54251942a` | 2026-08-17 | xiaoyao42 | fix：调整文字展示 |
| 860 | `80fde75e6603aad60f211183dae692d2d8c9b7ed` | 2026-08-17 | openJiuwen-bot | fix:code模式中，merge 冲突场景下 transient 状态检测失效导致分支切换/创建/提交/推送守卫绕过 |
| 861 | `853b336dc8764ee883f13b5a1d292cda2f9a84f0` | 2026-08-17 | william9200000 | fix(tui): refresh installed skills before slash skill match |
| 862 | `952019543f1e55fe412622b20fde393625372a23` | 2026-08-17 | liangfuqi | fix(history): web 前端长回复消息被 collapse 截断——改切片流+前端重组 |
| 863 | `ad6b4a2127530edd6dd5649b864e57ed11a82f5f` | 2026-08-17 | openJiuwen-bot | fix(web): route team code mode to code.team runtime |
| 864 | `ae8e95c94673d957206eb8c6d205265703936687` | 2026-08-17 | haowenzhong1 | fix(agentos): integrate jiuwenswarm web server into one-click deploy |
| 865 | `cecde41335b2376e696eaab242435ff0a0e2d6b1` | 2026-08-17 | cycong_1 | fix: 热更新后无法使用general-purpose subagent |
| 866 | `da0c9ce9b1849c3b19cbd2a4cb49450b8b21074a` | 2026-08-17 | XinchenZzz | fix(build): 统一 WorkSwarm 包名与版本号配置 |
| 867 | `de79f5b81236b60aa022a49d9df55fd3a193d2bb` | 2026-08-17 | yechenzhen | fix: unwrap FaaS envelope in YuanRong files/list parser |
| 868 | `0115e798d5c2f10747d1b73e76693f2455666f72` | 2026-08-18 | zhengkailan | fix: remove shouldRecoverProcessingFromReasoning function and adjust processing recovery logic |
| 869 | `2bbc103a3e9e10813e4e48279929a01531152bd5` | 2026-08-18 | d-0771 | fix(agentos): Gateway在TUI断连清理沙箱前添加任务状态检查，超时时长改为可配置 |
| 870 | `2c884d6f7546e03bb35cd6bfc5e8261a21188b6d` | 2026-08-18 | zhengkailan | fix: simplify configSaveBlocked logic by removing mode dependency |
| 871 | `4868f0d311a8cf83c4a449ac6273b0a850f575fa` | 2026-08-18 | renquanjia | fix: 新增默认模型、华为maas模型开通购买引导skill |
| 872 | `6849dfe19675e51bcb80e938c72d9f167b541e88` | 2026-08-18 | XinchenZzz | fix(bug): 为运行时依赖加上界约束以支持可重现构建 |
| 873 | `a2418265db7d144c37b7780a7bd22da55b31a4e0` | 2026-08-18 | kdgmpw | fix(session): 修复重启后，历史session丢失history的问题 |
| 874 | `c037f908d01498e002fe2470d18f5c3abf4e20ff` | 2026-08-18 | xiaoyao42 | fix(web): hide OpenAIAccount and unsupported config sections |
| 875 | `c986da8c5915842c107a627c13ac69ae731fb275` | 2026-08-18 | openJiuwen-bot | fix recap |
| 876 | `0da7b2ce37c6ecd1c9f65d30c1159f12ab07e173` | 2026-08-19 | z2tng | fix(skills): harden local skill import source validation against arbitrary file read |
| 877 | `51dfb3679349240743ee785bb7035502e054cccb` | 2026-08-19 | ray_le | fix(harness): 修复自定义 harness package 配置白名单及路径控制 |
| 878 | `57f675f673782a7dd510a24329a47c17a1a73da3` | 2026-08-19 | renquanjia | fix(skill): 补全 huawei-cloud-maas-setup 缺失的 check_account.py 脚本，优化开通失败检测 |
| 879 | `5a0ab337dabf897a63c78f0e09435792ea0ee4b1` | 2026-08-19 | lin-xiaoyu | fix(symphony): 优化技能图谱字体布局，更新symphony文档 |
| 880 | `6820761dabe669196ab0d20da04b01edf54423d7` | 2026-08-19 | cjh_jinduoxia | fix(web): web端的集群模式支持plan |
| 881 | `6cae1b14b1202c982bffa47df75b3e58195ce36d` | 2026-08-19 | MengHan18 | fix(ascend-affinity): harden session KV-cache lifecycle and runtime policy propagation |
| 882 | `95c25360d042a46b835be70cca2cf3932881e548` | 2026-08-19 | XinchenZzz | fix(bug): 安装包全局配置命名规则修改 |
| 883 | `a823253316ae6958ec378022c0cede5750688ac6` | 2026-08-19 | renquanjia | fix: 删除首页配置引导按钮 |
| 884 | `bddf3359060303d9f8d96796119e350792390d34` | 2026-08-19 | chenchunzhou | fix(swarmflow): keep round running until workflow reaches terminal |
| 885 | `c32a4cdbe2dc9ab71c9a68ac4f05f102fc821b02` | 2026-08-19 | wncbdj | fix(plan): 修复plan模式首次无法触发审批 |
| 886 | `d04d629ee958bd9e822dabdb8039b1410b1b1fc1` | 2026-08-19 | yan_zj | fix(tool):Turn off the tool_search switch |
| 887 | `e27fc98a2215dc390333d057badeccdbcb9ab4d6` | 2026-08-19 | kdgmpw | fix(code): 修复team模式下，审核卡片不能撤销的问题 |
| 888 | `133eac4edf9b59915b5cf09ccd24f5afa538f612` | 2026-08-20 | ray_le | fix(mcp): surface friendly CLI connect errors |
| 889 | `152583aa305836e87481e6de8a5f34e8c7d0928b` | 2026-08-20 | liangfuqi | fix(session): 修复team 任务结束后，人类成员继续发送飞书消息，导致会话列表中该任务消失 |
| 890 | `20097e86f35e8b11be21e3bf76edaf97737301ed` | 2026-08-20 | ray_le | fix(mcp): prewarm connected MCP cache on restart so first chat sees tools |
| 891 | `56da762dc6d8c49a422abd666038b37588c2e4c5` | 2026-08-20 | yangzequ | fix(harmony): add Harmony frontend platform profile routing |
| 892 | `85e1947aa951a0ab6e7a251d6316bbb23def3841` | 2026-08-20 | kdgmpw | fix(cron): 1、添加文件发送工具 2、透传api报错 3、任务超时时间默认1小时 |
| 893 | `91c913726cedabb89cc6b538d9369e0ef1070578` | 2026-08-20 | tangfei0306 | fix(ci): fix uv sync index |
| 894 | `b7378eb48cdc8ffa363778cb918a891ceb96fcfd` | 2026-08-20 | kdgmpw | fix(cron): cron适配内置免费模型 |
| 895 | `bab81faf058877b022e9343839fb7ec7ea2bd052` | 2026-08-20 | huanggawa | fix: dump trace and title |
| 896 | `1d45d2b4a08423365eae7c37b2afdae6614a97ad` | 2026-08-21 | kdgmpw | fix(cron):1、cron subagent继承主agent模型配置  2、适配401报错透传 |
| 897 | `2f1a46ac097b7255235062e1453a5c030c2edf72` | 2026-08-21 | wncbdj | fix(goal): goal会话使用默认模型 |
| 898 | `42c33bdf33cbe48efb71150df1b112d2deb06d06` | 2026-08-21 | xiaoyao42 | fix: workswarm分支修改的内容双合至develop |
| 899 | `4b188314cc82615dd0df84bd3b777d0f6a61c677` | 2026-08-21 | kdgmpw | fix(cron): cron的超时配置覆盖gateway的超时配置，避免误杀 |
| 900 | `601e9a3202598e010cad8c9733fd865c0eb2f9ce` | 2026-08-21 | xiaoyao42 | fix: 恢复技能广场重构误删的国际化内容 |
| 901 | `76835bc165c1d6dba5097d0b4e0164db03909922` | 2026-08-21 | min_gitcode | fix(team): resolve page-selected zen free model as team fallback |
| 902 | `962f0a4426041d54cef60e980a10491df84546ef` | 2026-08-21 | liangfuqi | fix(mode): 模式系统迁移到三段命名 canonical |
| 903 | `a1981719a623b0c85e28650695f99d0359090226` | 2026-08-21 | douran | fix: enable persist sessions via slash command |
| 904 | `bd8663ec4f36c457ece415d0b20d3bdd3ab8f21b` | 2026-08-21 | openJiuwen-bot | fix(agent-group): align manifests with agent template schema |
| 905 | `d986447603852a2401a2c02138e817d245689d75` | 2026-08-21 | wangxiaolong100 | fix:  增加分支选择模块 |
| 906 | `754c111bffd6a3af904c45ff8bb90f3646636374` | 2026-08-22 | yan_zj | fix(attachment): optimize attachment logic and inject as system messages |
| 907 | `e90d9ea80cdeccb84a1f92f296a85aa23e84133d` | 2026-08-22 | openJiuwen-bot | fix(team): recognize canonical Team Plan modes during capability assembly |
| 908 | `fc35e76e191372369a3ec154ea8f92bdd2c08e08` | 2026-08-22 | ldstyle8 | fix(tui): prevent session cleanup races on restart; create missing memory files on edit; avoid initializing Git for empty workspaces |
| 909 | `59e42de913cf837e04fea1aa79d555d82dc10316` | 2026-08-24 | lzzzzzzasd | fix: 前端优化与响应式适配 |
| 910 | `82401efcb3fb728c7571eb135c2003c136c62da8` | 2026-08-24 | kdgmpw | fix(cron): 修复多个一次性cron同事执行时的调度冲突 |
| 911 | `8c6b3d5ebe6714f4ef8beaa7af05ebbc86df47d6` | 2026-08-24 | XinchenZzz | fix(bug): set macOS bundle main executable |
| 912 | `cc2b1efc86d510ccdfb5d50f43cf655ce60933f7` | 2026-08-24 | liangfuqi | fix(feishu): 修复team 模式下， llm的回复会多个空框 |
| 913 | `ea98a45207f8e543d0e8817b82a097959e865116` | 2026-08-24 | ray_le | fix(mcp): forward mcp.* methods to AgentServer in web route |
| 914 | `2d626773206e73349008347106d3dda34f4efdc3` | 2026-08-25 | ldstyle8 | fix(code-prompt): Move git snapshot into stable system context |
| 915 | `74b745c210c3884b80f6163d6dba3ea71f9f5a52` | 2026-08-25 | kdgmpw | fix(gateway): 禁止同一 workspace 多 Gateway 并存，根除定时任务重复执行 |
| 916 | `8a942a3444ff094e79a0b308cc98b822a8e1e500` | 2026-08-25 | kdgmpw | fix(agentos): 修复AgentOS场景重构引入的回归问题 |
| 917 | `9ac2fa5e7d60142146448bd1395ec2165292beaa` | 2026-08-25 | kdgmpw | fix(browser): E2A 重启前先获取旧浏览器配置以正确匹配运行时身份 |
| 918 | `e99393616946aef6d3fb655b5735cb52bff7809f` | 2026-08-25 | zhangyaomaggie | fix(swarm): support the creation, loading, and execution of templates and plugins |
| 919 | `efe23c8d32cf01ecc424d2249ff261a4f47a7118` | 2026-08-25 | alan_cheng | fix(observability): stabilize realtime subagent trajectories |
| 920 | `f56fe73749b27d5775a6b9e09ee3de1b46159a9e` | 2026-08-25 | joyhou_cn | fix(jiuwenbox): 测试代码安全整改 |
| 921 | `143730b938fc5d7676bc04f1e5b09b4f9c779eb3` | 2026-08-26 | xutuo | fix (symphony): 清理 jiuwenswarm 中冗余的 symphony 代码 & 优化 symphony 接入 jiuwenswarm 的使用逻辑 |
| 922 | `8562fa4dd5ceb0af5df692c42149a1bc278a12de` | 2026-08-26 | d-0771 | fix(gateway): Gateway添加对AgentServer请求网络层错误的处理 |
| 923 | `92ff0969207dc6276299a9ee1483e45c1e69b670` | 2026-08-26 | XinchenZzz | fix(bug): stop orphaned setsid process groups |
| 924 | `b2c8c0ca55cd82974a05d5eba0fde5f13e9f5adc` | 2026-08-26 | ray_le | fix(mcp): isolate per-MCP failures in reload + drop dead stdio-entry code + mcp-prewarm server startup |
| 925 | `dca173a939183d061b60f9d06027e4f7ac057a47` | 2026-08-26 | haowenzhong1 | fix(agentos): add web authentication with HttpOnly cookie gateway integration |
| 926 | `02ed78db4f07b93686aedde0402b536629c5f58d` | 2026-08-27 | yuhaiquan | fix:会话框技能迁移到“+”中 |
| 927 | `1a9b3b6aaa90f3b7f5f0ec70b518c000e493436f` | 2026-08-27 | xiaoyao42 | fix(web): repair Mermaid share image layout |
| 928 | `2b8a97954cfd20fe6a84bd6570252174be89c43b` | 2026-08-27 | haowenzhong1 | fix(agentos): redirect agentcore logs to user home .jiuwenswarm/logs directory |
| 929 | `30c6a131bdb0c2e0d6b1e0ac9bfd9de8a4aeedaf` | 2026-08-27 | cjh_jinduoxia | fix: bug001 集群模式下隐藏插件/MCP扩展入口 |
| 930 | `349796ad171ec1dcfca2a343f8ea9f6ebb084fa2` | 2026-08-27 | XinchenZzz | fix(bug): stabilize Team round lifecycle on heartbeat rail |
| 931 | `5eb82d14bbbb061713be4543d28a985944d0f71a` | 2026-08-27 | ldstyle8 | fix(code): remove stale browser rail channel update |
| 932 | `6078172c91f84842144b14b2a6fa62c05ef12517` | 2026-08-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#2429: fix(tests): repair tests/ui_e2e Web UI smoke harness |
| 933 | `7ebebe3fe116754f1162731cc1c958abc860611c` | 2026-08-27 | ray_le | fix(mcp): isolate per-MCP failures in team assembly preflight |
| 934 | `8a99c57d7d33c5e4d9864fbf69065210c134a150` | 2026-08-27 | yuhaiquan | fix：技能页面左右铺满 |
| 935 | `8c80adde08dd02b3771dc7094ff992e670e7ec46` | 2026-08-27 | Lexie-7 | fix:新增厂商预设注册表与 vendors.list/fetch_models RPC |
| 936 | `8f34291906abf7c4e1a3a94d1a819e5a94c0ff3b` | 2026-08-27 | yuhaiquan | fix：发布鉴权逻辑优化 |
| 937 | `914babb21805e1603de5681dd2ab8e28f5975324` | 2026-08-27 | haowenzhong1 | fix(agentos): use WEB_HOST for web proxy target instead of hardcoded 127.0.0.1 |
| 938 | `9f8fbf833c643adf7885c7427adde178c1e1ea81` | 2026-08-27 | yechenzhen | fix(agentos): hook Web WS onto the same token auth as TUI |
| 939 | `bdfdeaf35f157bd051dd038bfa29a10e34b90505` | 2026-08-27 | cycong_1 | fix: 启动服务自动合并 config 模板新增项 |
| 940 | `bf08c49a450af73744047607db0a8b4a7d9cffd9` | 2026-08-27 | lzzzzzzasd | fix 对话区域ui 1600px 以下点击折叠面板外的区域折叠 团队区域ui还原 |
| 941 | `e048a5a91fe71a81bda71b2e4b05854318b4cbe7` | 2026-08-27 | yuhaiquan | fix:1.技能搜索框弹性布局；2.修改图标 |
| 942 | `e27c9deb1ca2331f0b61accc120a873e1af287d0` | 2026-08-27 | xutuo | fix: 禁用 SkillNet 源 |
| 943 | `ec729b01e61dc1d1b36d836662bf6d8cd6970d24` | 2026-08-27 | xiaoyao42 | fix：单agent模式允许已有会话进行模型切换 |
| 944 | `f0034059fc674bf50a9eda8ee15e69655114d684` | 2026-08-27 | ldstyle8 | fix: avoid duplicate user message during session warmup |
| 945 | `f2d6613844a1253690ad61aecffc1ad59e36e552` | 2026-08-27 | l_x_d | fix(context): context-window support |
| 946 | `f48121b00325cf1d6f67d802e25202d98a08f7e0` | 2026-08-27 | yan_zj | fix(tool_search): add tool_call for unified deferred-tool execution |
| 947 | `02957a531381ab4384ee67170c40f97e156f33b1` | 2026-08-28 | douran | fix: harden persist session continuity |
| 948 | `a8937118ecc72d82620efb99f0d8c1cbd1dbc5f1` | 2026-08-28 | joyhou_cn | fix(jiuwenbox): 整改测试代码硬编码凭证、API密钥和IP地址 |
| 949 | `dc6a523981fead44397ec87b4d3219dcf7977989` | 2026-08-28 | openJiuwen-bot | fix(team): Turn on the reliability switch |
| 950 | `5d196d18bc478f448f2acf0c4d12bcab72204e52` | 2026-08-29 | cycong_1 | fix: subagent_runtime.enabled 未传入 BrowserTaskPromptRail |
| 951 | `64a004c1d8d43e16eb56e47b3e65125cec6c7372` | 2026-08-29 | kdgmpw | fix(server): 图像探针与 Zen 免费模型预热改为 listen 后异步，不阻塞端口 |
| 952 | `715e370cd6bf4c6f3f7bccd33d15cce929bde5d6` | 2026-08-29 | zhangyaomaggie | fix(swarm): support the creation, loading, and execution of templates and plugins |
| 953 | `9c77ec4313a7369b9ebae0c9f9cea632214d0c75` | 2026-08-29 | william9200000 | fix(skill): 修复知识转技能成功后因旧刷新方法报错 |
| 954 | `aa7e7d9d91aca081c05d796b63f27a4d9196b142` | 2026-08-29 | Lexie-7 | fix(agentos): 新增models.agentos模型配置和context_window字段 |
| 955 | `cfe09ccf1c04f4abb978ec84dc5403650a41f553` | 2026-08-29 | zhengkailan | fix(web): enhance file preview support for Python and improve markdown rendering |
| 956 | `deedbe25371e8228e6b979989c31ccf4330a8818` | 2026-08-29 | haowenzhong1 | fix(agentos): auto-detect sandbox local IP for agentserver bind host |
| 957 | `ec0f2ed5f52424b3a194e89c766050a61f325515` | 2026-08-29 | haowenzhong1 | fix(agentos) create logs directory during workspace initialization |
| 958 | `f405ddec32b2db08167f16191b78e9b2108a8d6f` | 2026-08-29 | xiaoyao42 | fix：样式优化、小艺频道增加app_id为空时提示 |
| 959 | `1dcefaee3b805889ee3643af884d0b36353af4db` | 2026-08-31 | alan_cheng | fix(trajectory): avoid replaying context moves |
| 960 | `29677071fa63b69cfe08ad9614dcffeb10897177` | 2026-08-31 | yuhaiquan | fix：1.技能广场搜索出来的技能没有安装按钮；2.技能广场初始加载不显示已安装图标 |
| 961 | `65f12c0b80fbd4d5434f961a65bd18a6feedfa82` | 2026-08-31 | alan_cheng | fix(trajectory): restore ask-user resume facts |
| 962 | `6b81c8b495daba6cffb2639ff69c51c1aa9d44b3` | 2026-08-31 | alan_cheng | fix(trajectory): consume standard GenAI usage fields |
| 963 | `884dcd5642c46bfdd43a6953cd03dcd000464431` | 2026-08-31 | alan_cheng | fix(trajectory): stabilize timeline navigation |
| 964 | `920a63f6f2d74a667568650cc2aad0cb80caa727` | 2026-08-31 | XinchenZzz | fix(bug):preserve immediate steer semantics with heartbeat admission |
| 965 | `c3b89902a3a2717acd2bc45d6ff26b76ff9a3b1c` | 2026-08-31 | lzzzzzzasd | fix: 聊天时间线耗时上移与 subagent 面板总览/详情交互优化 |
| 966 | `da6a319b5571def8308753105ca1633bbd52ded6` | 2026-08-31 | XinchenZzz | fix(bug): align desktop packaging and installer identity |
| 967 | `fe6024570f3deac0216c666a659425ff7a666da3` | 2026-08-31 | XinchenZzz | fix(test): validate optional CLI packaging exclusions |
| 968 | `050d54dcaf17652c50b88700a2b038bbb2c549b2` | 2026-09-01 | XinchenZzz | fix(bug): clean stale descriptions during Windows upgrade |
| 969 | `099487e568f6a368bf44be0a64c09d60ff7b0cdb` | 2026-09-01 | xiaoyao42 | fix(web): 更新模型配置引导以匹配新版设置路径；优化模型名提示；统一对话与定时任务模型选择器 |
| 970 | `0bfd03a32c25401522d8169213bf447fc14e189e` | 2026-09-01 | lin-xiaoyu | fix(symphony): 修复模型工具调用参数格式错误，candidate_skill_ids 入参为字符串时校验失败 |
| 971 | `287931a4de301871ed040dc7a071ce497f002d1f` | 2026-09-01 | kdgmpw | fix(installer): per-user install and migrate legacy admin version |
| 972 | `2d99f9d126fb2c34966a2e2521a71ad8016c1a1e` | 2026-09-01 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#4363: fix: align native image policy with model probing |
| 973 | `4698a194f15060aae553c7d71cf487234e312b97` | 2026-09-01 | cjh_jinduoxia | fix(web): adjust connector market UI per feedback |
| 974 | `493efaf82651387efc108f24d2cb78d819ad15c8` | 2026-09-01 | layShang264 | fix(symphony): 图谱构建前校验主模型并透传失败详情 |
| 975 | `54d002e9355eb0ae3323ed22f25d29a76faeb86e` | 2026-09-01 | lin-xiaoyu | fix:(symphony): gitcode ISSUE模板新增 “编排与分发” 模块 |
| 976 | `5ac394cee32c860230ba9d8e4260ff9461fcf286` | 2026-09-01 | l_x_d | fix(context): prewarm tokenizers with local context fallback |
| 977 | `7bd47fcc11e1fda362b9e7809979305bfc2e0a22` | 2026-09-01 | yangzequ | fix(harness): adapt imports to openjiuwen RSI harness_rsi restructure |
| 978 | `8955caedb6b603f4092a5692d9ed93e951c18cdc` | 2026-09-01 | chensheng12345 | fix(web): disable slash commands in team mode |
| 979 | `8e5392e7b0d480dc209f39bb508d2cfe79184c20` | 2026-09-01 | lin-xiaoyu | fix(symphony): 删除技能图谱页面中的JiuwenSwarm文案 |
| 980 | `987e233df20768180ad4756852b6c5ba23a525a8` | 2026-09-01 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#2095: fix(search): keep explicit free-search env opt-in across process start |
| 981 | `b136e50af12dd544b9a5cca2aa78b73941a280b8` | 2026-09-01 | yan_zj | fix(attachment): keep request mode authoritative and inject as user messages |
| 982 | `b734f0451fd8d48730380069b570985b9e3ae205` | 2026-09-01 | liangfuqi | fix(config): 新增 config_version 字段，版本更新时迁移config.yaml模板 |
| 983 | `b7eca2383da90aa0e35de79b47a265d676b68da6` | 2026-09-01 | yuhaiquan | fix:1.团队技能“去试试'跳转到集群模式；2.禁用技能”编辑“和”去试试“置灰 |
| 984 | `bbe440d3f6075b06c19948b8fbd1cd3ae22c5868` | 2026-09-01 | cjh_jinduoxia | fix(web): 创建自定义插件/MCP 必填项前端校验高亮 + 隐藏头像上传入口 |
| 985 | `c2d0d1b4acd979897aa14df982866119a59e1bd8` | 2026-09-01 | XinchenZzz | fix(bug): prevent Windows EXE from hanging on Loading after startup failure |
| 986 | `cf9c5560f023ee220f149db2a4333a2f9094231a` | 2026-09-01 | douran | fix(feishu): fall back to Open ID suffix for user names |
| 987 | `d701ba2e3a9e2b9f0c807102b983d72449fb4bdb` | 2026-09-01 | kdgmpw | fix(code): 优化diff预览 |
| 988 | `d9536977de96258f56e04f2d2d11adb0b51b9b55` | 2026-09-01 | openJiuwen-bot | fix：修复Edge密码框双眼睛问题 |
| 989 | `df4d42c36beff9deb95ce3989849e816aa924ef5` | 2026-09-01 | l_x_d | fix: pin agent-core revision for tokenizer registry |
| 990 | `e9e2ac1df505e7b12ec73c849ed5b17a8ee335cc` | 2026-09-01 | xiaoyao42 | fix(web): 刷新免费模型开关后的模型列表 |
| 991 | `eda379e8364b4636dd83e6836f97671a41bbb4bc` | 2026-09-01 | XinchenZzz | fix(bug): Fix Windows startup failures when WebView2 is missing or console encoding is limited |
| 992 | `f9b8bed8a7ae7f60e2c87b256a44888be9e767b6` | 2026-09-01 | Lexie-7 | fix(tui):多窗口/model删除模型不冲突 |
| 993 | `fd50c1d459beb28f49fd6b7bfc3dbaa68e8baa52` | 2026-09-01 | kdgmpw | fix(cron):修复cron部分星期错误 |
| 994 | `06369a7a09f079d7a4f2b8bf32c7a39764ccb975` | 2026-09-02 | wncbdj | fix(llm): 集群模式关闭思考会报错 |
| 995 | `264276354530b3e92774afd78f90cc720d534d7a` | 2026-09-02 | cjh_jinduoxia | fix: 从扩展页入口跳转会话时强制切回单 agent 模式 |
| 996 | `2867ec74e282bea34ad2fc853c711c2a7ecff207` | 2026-09-02 | openJiuwen-bot | fix(team): When the execution window of the plan pops up, it is prohibited to input "query" in the chat window any longer. |
| 997 | `3f75c40d4d7922af055e2f4f2f0db3a4f16ae516` | 2026-09-02 | rivervale | fix(web): update team mode skill info |
| 998 | `474a014b206c8d47b23b7d374384c3c0a49fd26c` | 2026-09-02 | william9200000 | fix(tools): send_file_to_user 交付文件下载令牌过期问题修复 |
| 999 | `71f7333220854ef04bbb0ee210dba3a88efee4a0` | 2026-09-02 | kyrie_irving11 | fix(skills): drop N+1 recommend enrich after SkillHub hydrate |
| 1000 | `72be83d6997a10df8291f87c26f05875aa15a18d` | 2026-09-02 | l_x_d | fix(session): persist usage summaries in history |
| 1001 | `80c3afc4e20107f9f989347a8f3807d20a43d0b5` | 2026-09-02 | rivervale | fix(tui): prevent human shortcut from intercepting h |
| 1002 | `8465de8d2e01b4e59e1fddc2feea37e25ff7935d` | 2026-09-02 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#4511: fix(browser): remove obsolete bundled runtime |
| 1003 | `a0b1466fc9225db10a318eac226e33ee5cc8225a` | 2026-09-02 | layShang264 | fix(symphony): 修复UT用例 |
| 1004 | `b509da6aca6f194ea7a316431aa1ecb84411a200` | 2026-09-02 | min_gitcode | fix(agent_teams): improve managed runtime installation |
| 1005 | `edc0d6e942c338834db9a9091a3564ecd61f3ff3` | 2026-09-02 | xiaoyao42 | fix(settings): 恢复多模态配置国际化文案 |
| 1006 | `166386ff0031d25c67ba2249577b2ee8d923e716` | 2026-09-03 | wangcan103 | fix(web): 修复 Agent 详情弹窗点击「原始」后格式化/原始切换按钮消失Refs：3504 |
| 1007 | `2b52ed67a4c6873b51d92c11dc0c41f5102fa146` | 2026-09-03 | XinchenZzz | fix(bug): allow resuming after increasing max runs |
| 1008 | `492d1c4dfbf26dd91e693051593c5290ee3c22f5` | 2026-09-03 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#2377: fix(cron): follow the store when the workspace migration relocates it |
| 1009 | `505b6822046a4a965489a6b7668deb8c5c2144e9` | 2026-09-03 | wncbdj | fix(plan): 修复plan执行中，页面刷新后前端plan标签消失 |
| 1010 | `5663d42343eb67f44d7ba7a4c9c2eae840bb2c8b` | 2026-09-03 | alan_cheng | fix(observability): align session usage fixture with the GenAI operation filter |
| 1011 | `5ffc9901266bdf6d0c7a506c4e214b155d26eeef` | 2026-09-03 | kusanagima | fix(web):修复推理下拉框选项并没有被国际化 |
| 1012 | `661868bc0329e0cfa072de2aed900c34726ef602` | 2026-09-03 | openJiuwen-bot | fix：前端部分修复子任务跳过后进程视图不展示已取消任务问题 |
| 1013 | `6a01713daa0369efc4afe201218b161136f38665` | 2026-09-03 | xiaoyao42 | fix(web): 集群模式增加leader上下文显示；kv cache返回null的时候，不显示 |
| 1014 | `6eaf659687ec9e2f0dc8f66de89c42440bfd2437` | 2026-09-03 | lin-xiaoyu | fix(symphony): preserve unicode skill names |
| 1015 | `71d4a84c979c0d2c1b773307bd5bab7d168d93c8` | 2026-09-03 | lzzzzzzasd | fix(web): 技能面板组件化与主题 token 化；耗时计算修复；页面通用组件抽取 |
| 1016 | `74917c17e8bf39e20bdccfe9dc8bcd2a7ec9d82b` | 2026-09-03 | xutuo | fix(symphony): 修复技能索引提示和配置开关 |
| 1017 | `77ecf2ed7bdf91bbb1d0b57c7b8be18c139d4bce` | 2026-09-03 | cjh_jinduoxia | fix: 前端 MCP 显示统一改名为「连接器/Connectors」，扩展页 tab 增加「广场」 |
| 1018 | `7e76738f27fbd8fc219c44527c12f86dad3eead3` | 2026-09-03 | zhangyaomaggie | fix(swarm): fix empty generated files for pre‑built plugin packages |
| 1019 | `8574643c1a05fc10499f412a5b4447b9e4ac4d6e` | 2026-09-03 | l_x_d | fix(rsi): render experiment page once |
| 1020 | `896664ce0de93d6b718c4d262500ca8ba3392aa1` | 2026-09-03 | xiaoyao42 | fix(settings): 将免费模型移至对话模型下方 |
| 1021 | `aafe0e78f991cc68d4f896edda0a4278a393e45b` | 2026-09-03 | cjh_jinduoxia | fix: 计划模式执行中刷新页面后恢复输入框「计划」标签 |
| 1022 | `ab9fa14d03f3db3bbdfbf8ee40ed05c0842e064b` | 2026-09-03 | min_gitcode | fix(agent_teams): include selected model in team pool |
| 1023 | `c985faf49dbd790e300402e04bdf28a5fbcdb7da` | 2026-09-03 | yan_zj | fix(workspace): When the project directory is not passed, the agent's runtime working directory is located under the user's directory |
| 1024 | `e4708824e95c08f57e4baee4150fa2e07267a046` | 2026-09-03 | hwxiajing | fix(workspace): team 产物落 team-workspace/artifacts outputs、去 .team 路由、引入 per-member work |
| 1025 | `e4e781f0e7b0bb90137ec9b1fa83732f2f7c47e0` | 2026-09-03 | haowenzhong1 | fix(tui): catch unknown CLI args and print concise error instead of stack trace |
| 1026 | `f5f83f690305a73fa409d18ed2b557439b1bbec3` | 2026-09-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#4746: fix(web): handle macOS IME confirmation Enter |
| 1027 | `fa563a95be78eaac5a1f4d20812eb5821190d7a4` | 2026-09-03 | xiaoxiawang | fix: 更新 /ctx引用图片 |
| 1028 | `08831312610eb3a0813a83c06e11afb207d70f79` | 2026-09-04 | lzzzzzzasd | fix 对话区域宽度 全屏集群模式群聊宽度 专家tabs 产物卡片宽度 |
| 1029 | `0f803042f40954cab4a369296e135a9a2946e16e` | 2026-09-04 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4300: fix(ask_user): guard the question read, state a remedy in every rejection |
| 1030 | `11a32efbe1fc1d532ea0e5c05adebf31159f8f8f` | 2026-09-04 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4294: fix(rails): recognise plain-string tool failures in ToolResultErrorDetector |
| 1031 | `253f1780aaf4ebe8e698ac2f183009377872162e` | 2026-09-04 | XinchenZzz | fix(build): use display name for package artifacts |
| 1032 | `419563d53e26aa36638ea596c35d72fbd7813f72` | 2026-09-04 | kdgmpw | fix(cron): use real execution sessions in web |
| 1033 | `63293d12d0c44b372fa1812dedb4541b7ee9ce2e` | 2026-09-04 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#2720: fix(pdf): disclose partial reads in read_pdf, and make the whole-document rea... |
| 1034 | `6d98ae7e1e00301de1e7bcee212a57775becb7bb` | 2026-09-04 | zhengkailan | fix(agentManagement): enhance tag scrolling functionality and improve UI responsiveness |
| 1035 | `7f394441176da32dc7c478005b66eff9c871caf9` | 2026-09-04 | rivervale | fix(web): enforce plan and goal mode exclusivity |
| 1036 | `8b35ae1054595f1116c33c5a1002f05bb4edcd75` | 2026-09-04 | kdgmpw | fix(desktop): 优化桌面端启动流程 |
| 1037 | `902509c4f9b4d28e28d294bbc246c73d4cce3b9b` | 2026-09-04 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4301: fix(permission): decode approval answers through one shared option vocabulary |
| 1038 | `90bd1db306633f30a717df03671596017a7fb586` | 2026-09-04 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#4303: Fix paths in bundled skill instructions |
| 1039 | `91c168cbe38c2932f342938eba82344cf0340244` | 2026-09-04 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4288: fix(skills): stop building shipped skill temp paths from fixed shared names |
| 1040 | `c1e615feb5111c83c12ef5b2a319c90ddd9ef403` | 2026-09-04 | wncbdj | fix(todo): code模式页面刷新后，前端不显示todo进度 |
| 1041 | `c5b25bd0d2045443d38422d0e7f3ae0df3640ebc` | 2026-09-04 | alan_cheng | fix(observability): anchor trajectory turns to the ReAct loop |
| 1042 | `d255b19cf6d102296fbe0af41b9faf6f49deedbe` | 2026-09-04 | ldstyle8 | fix(tui): keep resume session titles readable |
| 1043 | `d902806d58a9a342fe189dea2911ce8ec065c5dd` | 2026-09-04 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4283: fix(tool-display): stop instructing the model to write call_goal in Chinese |
| 1044 | `fe26d47ce2523923762cd495be62a6066d28a726` | 2026-09-04 | ldstyle8 | fix(code): prevent duplicate task reminder attachments |
| 1045 | `4c738d9a6e36cc16660b821eb57a0ee7f8e2094d` | 2026-09-05 | Tiffany_F | fix(web): 增加设置页面、对话页面等testid |
| 1046 | `aa58c4b5a3979dc1b06debe14be4ca327fffb41b` | 2026-09-05 | ray_le | fix(rsi)：rsi web restore harness plugin install flow |
| 1047 | `bdc337ca9262a53865dbf96f135376fbe327a7f6` | 2026-09-05 | kdgmpw | fix(web): forward websocket diagnostic parts |
| 1048 | `e8aa1b433e8b5ff1875cdd4cfd63155ad2a2a862` | 2026-09-05 | XinchenZzz | fix(heartbeat): remove conflicting delete-after-run flag |
| 1049 | `04cd319034c457ff8e6103bc894375aa3b5edfe6` | 2026-09-07 | ldstyle8 | fix(todo): keep cancelled subtasks visible in the progress view |
| 1050 | `0822971b153080ab3cce1f8b3b53f8a6164c6dad` | 2026-09-07 | cjh_jinduoxia | fix: 计划模式切换收敛到 planModeGate 决策层，ask_user 待回答时不可切换 |
| 1051 | `0d7b5641b1dd1defa9b8f2fa9c1289ca693dad11` | 2026-09-07 | rivervale | fix(web): align slash skill suggestions with picker |
| 1052 | `1a71968f46205802c0c7ff68afc028aef61f9756` | 2026-09-07 | XinchenZzz | fix(heartbeat): preserve once expiration after cancellation |
| 1053 | `2404d43d706682f71bd24f5af9a53b3dd9b74e35` | 2026-09-07 | Rossoneri_1007 | fix：截图不能粘贴到文本框 |
| 1054 | `28dee0a40ea8a0df2eb053d3bb4560ccc621d8b3` | 2026-09-07 | AFSDFASD | fix(rsi): 接通插件选择、优化包安装与会话恢复 |
| 1055 | `3524107fde223b3463e72b2e45749f565e69f77f` | 2026-09-07 | xiaoyao42 | fix(web): 设置页搜索密钥改为可清空保存，工具模型配置支持删除并联动关闭开关；移除浏览器类型设置项；修复蜜蜂图片结尾帧变亮、蜜蜂动画时长不全的问题 |
| 1056 | `37c29ae103ef4b90ad61c5dabd40095eeed2099b` | 2026-09-07 | kusanagima | fix:(i18n)：添加测试模型时的错误文本 |
| 1057 | `38db2dca5e0497890163b32b59dca5af653bf1ae` | 2026-09-07 | XinchenZzz | fix(heartbeat): add active task constraints to list response |
| 1058 | `3f548663c63376558da138245c0c17f32c38316e` | 2026-09-07 | yuhaiquan | fix:1.技能仅上传.zip包；2.技能下掉“发布”按钮 |
| 1059 | `4b2251d7097173a6484cf7f0e12613ea7dc15702` | 2026-09-07 | ldstyle8 | fix(adapter): surface 0-token empty LLM runs instead of completing silently |
| 1060 | `4b3aabee308245f571f4879469bcc3b39ec2a00b` | 2026-09-07 | ray_le | fix(rsi): localize node stage progress and deprecate search_width |
| 1061 | `4d95ecb0ffc366a4c1568714f088f979d80c4eee` | 2026-09-07 | min_gitcode | fix(agent_teams): auto-save external CLI agent config after dependency install |
| 1062 | `4eb1786098adbe620e29364eb614d9026ca2adb6` | 2026-09-07 | lin-xiaoyu | fix(symphony): restore compatible thinking disable fields |
| 1063 | `4ef3c6f1453188da507d6bebb65a7a5552a08400` | 2026-09-07 | XinchenZzz | fix(heartbeat): constrain automated task scope |
| 1064 | `56c504cf43a6396e66d19cb09d96f88380b12aa2` | 2026-09-07 | openJiuwen-bot | fix：修复我的智能体中进入某个智能体卡片详情页，文件预览-点击下载，下载不可用 |
| 1065 | `58127359f8e2f6c6c2dbd2fb49ce2e04a0460d51` | 2026-09-07 | Lexie-7 | fix:coding plan中kimi模型限制条件和对ask_user工具schema校验 |
| 1066 | `63c50573c798825aa2d4a5350975ba1372fd6875` | 2026-09-07 | l_x_d | fix(rsi): wire paper provider and artifact folder flow |
| 1067 | `75a7b8ea1f4476239e4f6b4f4ed4814862cf195f` | 2026-09-07 | cjh_jinduoxia | fix: bug001 单次心跳任务隐藏「最大触发次数」+ 不限次数开关 |
| 1068 | `766cba092fbb3a2201d9cb47b5c3e09504e3aadf` | 2026-09-07 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#4937: fix(subagent): keep browser delegation synchronous |
| 1069 | `8339afa2b5a51576f1fa356887b83f844ac4d675` | 2026-09-07 | Lexie-7 | fix:百炼模型列表添加模型白名单 |
| 1070 | `83a1d85aa1561b61e153717323acdee820cce55a` | 2026-09-07 | XinchenZzz | fix(heartbeat): avoid ask_user resume admission deadlock |
| 1071 | `86ae3c1677e2ea5aa82870fa3bc01fa93cd0370d` | 2026-09-07 | lzzzzzzasd | fix(web): clean debug code, standardize icons, and fix tooltip positioning |
| 1072 | `88f3da59192942d4978455e1e18a789cb5ffab6d` | 2026-09-07 | Rossoneri_1007 | fix(web): 优化工具调用文字显示 |
| 1073 | `a0917c93aee90e846be20d3338403abe06f8da55` | 2026-09-07 | ldstyle8 | fix(subagent): report resumed agents as idle |
| 1074 | `a831a97af1a6638af8f6efdce2be3e52b7c5d6c5` | 2026-09-07 | zhengkailan | fix(AgentEditor): add character count for name and description fields |
| 1075 | `a9636a88a91a24caf0435bb0f7abd69f2f517a3f` | 2026-09-07 | kusanagima | fix(web)：修复样式，折叠时文字被挤压 |
| 1076 | `cd1f35b4c968b9307bfd6dcb6e1f6f47800c6656` | 2026-09-07 | ldstyle8 | fix(tui): handle statusline stdin pipe errors |
| 1077 | `d5898d3912f70ce3c56183da9927a0ade955e2fd` | 2026-09-07 | XinchenZzz | fix(heartbeat): hide session deletion policy on heartbeat create |
| 1078 | `e957c058746d3b83fbc5137bcefcd15b868ef7fb` | 2026-09-07 | Lexie-7 | fix(model): token plan/coding plan中拉取模型厂商url修改 |
| 1079 | `f6d93c964da58acfb6bfea59beee2b6816761c2a` | 2026-09-07 | ldstyle8 | fix(runtime): keep idle session adapters with live subagents from TTL eviction |
| 1080 | `fc01a75b9532007880c1b04818fa7031e8662103` | 2026-09-07 | hw_liuyu | fix(heartbeat): 增加执行超时和用户请求抢占机制 |
| 1081 | `0c6e3f29fc4ae67da7e2a3e88c726626d33c8543` | 2026-09-08 | min_gitcode | fix(agent_teams): preserve external CLI install state |
| 1082 | `0cdcce5c53743b7d9ca7dfadc606d5e1b619c4a0` | 2026-09-08 | ray_le | fix(rsi): track token usage and polish experiment UI |
| 1083 | `11cb28a4b52ab2cea4aabd5070c1309cb5de0fed` | 2026-09-08 | kusanagima | fix(web)：修复切换页签时没有清理数据的问题 |
| 1084 | `11e0c9688c0e012b957c75de99855e5a07638f21` | 2026-09-08 | openJiuwen-bot | fix:【同步】修复定时任务工具权限审批超时 |
| 1085 | `17de2149479ec0b7c2e38a0d35e23f74d568f1b6` | 2026-09-08 | xiaoyao42 | fix(web): 修复未认领任务头像和运行中横向滚动条；修复桌面安装包技能上传接口缺失 |
| 1086 | `1a71a348e508eef97e9806064cb6ddf7570a031f` | 2026-09-08 | AFSDFASD | fix(rsi): 数据集必要适配与默认三轮修复 |
| 1087 | `3b94192de9b810260c5547c0596b62661b41e8ca` | 2026-09-08 | cjh_jinduoxia | fix: 恢复扩展页顶部 tab「插件广场/连接器广场/我的扩展」文案 |
| 1088 | `3ff669191b54c305fbc81ed2d5e48e0fbe20ab6a` | 2026-09-08 | ray_le | fix(rsi): cancel running harness task and add evaluation method selector in create dialog |
| 1089 | `41be2ba78753ff6f196b7fd9d19e3a1764f43948` | 2026-09-08 | XinchenZzz | fix(heartbeat): completed heartbeat missions no longer can get changed to disabled |
| 1090 | `43728b2d096840f786961748d9eb1030aecd46ec` | 2026-09-08 | kusanagima | fix(web):修复input无法控制最大最小值的问题 |
| 1091 | `48287a034d03e1202c213202b9e06ae95a61585b` | 2026-09-08 | AFSDFASD | fix(rsi): reconcile epoch trees and consume native plugin artifacts |
| 1092 | `485f63016bc63af1fc5cd69515cba9579e6723ec` | 2026-09-08 | openJiuwen-bot | fix(security): 集群模式工具护栏不生效 |
| 1093 | `63b796bbe6f1f51ee86efc2fe420869f3ef0f222` | 2026-09-08 | XinchenZzz | fix(heartbeat): reject expired one-time schedules |
| 1094 | `7621d4f0870c7818e918955d2dadbcf860c92582` | 2026-09-08 | lzzzzzzasd | fix(web): 技能/专家/扩展 布局 对话区meta字体样式 tts tips 任务栏 悬浮tips问题 compact样式更新 |
| 1095 | `7e5dee0d632390b556c795d6f551300b8e432534` | 2026-09-08 | huanghongyun | fix(mcp): 修复企业微信授权页面无法弹出 |
| 1096 | `862d04cccefdfc549358094cc705b9051e2813ca` | 2026-09-08 | alan_cheng | fix(observability): keep trajectory runtime joins off the event loop |
| 1097 | `8e6fe36932848304a8818aee29b9d82c1d7c3182` | 2026-09-08 | layShang264 | fix(code): expose Symphony tools in single-agent mode |
| 1098 | `8e7002bc3e3aa69920e60f6a9ae1513f3fbd1f3f` | 2026-09-08 | XinchenZzz | fix(heartbeat): refresh task card when run starts |
| 1099 | `9614b21fb046482e19fa2059b1955a59ca398600` | 2026-09-08 | z2tng | fix(skill-evolution): pin agent-core tool interrupt fix for skill-evolution |
| 1100 | `9708dac664c150cdbaae5821d2ad5cd0bde46476` | 2026-09-08 | XinchenZzz | fix(agentserver): bound stream keepalive cleanup |
| 1101 | `98ff514c8a62a46e1d132c21915ad203588a7a0d` | 2026-09-08 | XinchenZzz | fix(heartbeat): allow resuming edited expired once jobs |
| 1102 | `9901644af3a60a105b69e4fe10bcbce7db8c1f44` | 2026-09-08 | l_x_d | fix(rsi): remove duplicate open artifact action |
| 1103 | `9cfb32696bbccf6295338936aa812d7d417cbb88` | 2026-09-08 | XinchenZzz | fix(heartbeat): preserve parent delivery channel |
| 1104 | `aecdefca1162fef44a2e9d287e425b46e3a06b8f` | 2026-09-08 | zhangyaomaggie | fix(swarm): fix the issue where agent creation via file upload could succeed even when the persona directory file or README.md file is missing |
| 1105 | `aeff563080c3559374ec39af3ad483808f491513` | 2026-09-08 | xiaoyao42 | fix(web): clarify added and built-in skill group labels |
| 1106 | `b20afd2b0ce13f9febfa48c223e44a6eb9029899` | 2026-09-08 | huanghongyun | fix(mcp): 修复华为云 MCP 兼容性并隐藏连接失败敏感信息 |
| 1107 | `b6d3272a2636fddee8803bfbc739844d8626d29a` | 2026-09-08 | william9200000 | fix(skills): 修复广场详情内容为空的问题 |
| 1108 | `b7ae0ebdc6a0f0f8181ab16d2eb4c992e1e3ca31` | 2026-09-08 | yuhaiquan | fix:“我的技能”区分内置技能 |
| 1109 | `c1cc70fa0f7b4062760ca7a2bb153286df09e1dd` | 2026-09-08 | l_x_d | fix(rsi): align paper proxy handoff and UI hints |
| 1110 | `c7bf529a15dfdf422b854ee03f6ef1eb80f6fe24` | 2026-09-08 | openJiuwen-bot | fix(team): prevent duplicate replies during concurrent session startup |
| 1111 | `cb4c1999ad1359778c8a04d69320db901e166b68` | 2026-09-08 | cycong_1 | fix(subagent): commit subagent state on close so resume works after adapter eviction |
| 1112 | `d037d0f9b9523fd8e3f86148f3bba50abaf651ec` | 2026-09-08 | Ziliang_Zhou | fix: jiuwen_memory log_file配置补充 |
| 1113 | `d8df43b074a8b618bcec79a1b9d20e771dfbc275` | 2026-09-08 | alan_cheng | fix: persist trajectory turn numbers across restarts |
| 1114 | `dbd17932cceec6bc51c7ef324e887ea2dbb9aa6a` | 2026-09-08 | l_x_d | fix(skills): bundle program evolution design skill |
| 1115 | `e2e92237173cb446039965aa4f2fd9dd2692e012` | 2026-09-08 | xiaoyao42 | fix(web): update custom model icon with lighter strokes |
| 1116 | `e3fcd90e12695e3bc18e6d613a73206b6e5cd17c` | 2026-09-08 | birfied | fix(rsi): 暂停即让出执行位；程序优化的分数按百分制、隐藏 harness 专有指标 |
| 1117 | `e755d19c0a774df3cf4b466d2d18a73470f1311b` | 2026-09-08 | openJiuwen-bot | fix(team): Fix the issue where tasks remain in the running state after skipping the plan approval process. |
| 1118 | `e77ce11ea9b0fb1971d9ceaf3564df531821a631` | 2026-09-08 | william9200000 | fix(skills): 广场安装走统一 online_search.install 接口 |
| 1119 | `edaf4d863be84e012a4445a70500f0ae97c9176c` | 2026-09-08 | lin-xiaoyu | fix(symphony): 避免图谱构建期间重复调用编排工具 |
| 1120 | `fee0da8107d0521faf37fba521063e49aeaeb631` | 2026-09-08 | yechenzhen | fix(gateway): TUI cron.job.update 按 dict 读取归属以免创建者改描述失败 |
| 1121 | `08b3fac519e1621a3b0b4ed0b5981e38a13c8319` | 2026-09-09 | ray_le | fix(mcp): TUI MCP connection creation failure &  mask credentials in error strings, encrypt token store at rest |
| 1122 | `14e412871f2da9f2d84f311b9891582621933285` | 2026-09-09 | cjh_jinduoxia | fix(desktop): exe 端拖动队列排序卡顿——拦截判据修正 + 拖拽源改用 span |
| 1123 | `16f1f19b7ac873b30d4349f607003036e8bccfe3` | 2026-09-09 | Lexie-7 | fix:暂下智谱anthropic端口，待模型服务厂商调整后重新上架 |
| 1124 | `1b538e25d89cf1e23b67122cda0bad5dad6d8b48` | 2026-09-09 | kdgmpw | fix(code): 优化git嵌套的提示信息 |
| 1125 | `3b00611e8fdb93bbf286ddcffc608f9276a6a5e1` | 2026-09-09 | openJiuwen-bot | fix(security): 修复定时任务工具权限审批超时 |
| 1126 | `48f11d72eefd45326a1075ee1ad5fc662f14a286` | 2026-09-09 | william9200000 | fix(skills): 知识转技能目录无效报错修复 |
| 1127 | `4e96603bc0bece97bc4e083c85f7fcb7a4b89749` | 2026-09-09 | alan_cheng | fix(swarm): restore the session archive read and move HTTP tests to subjects |
| 1128 | `6264de338af30aa989290fa6d5a492628578209f` | 2026-09-09 | lzzzzzzasd | fix (web): 卡片统一 PageCard 组件化与主题化样式重构 |
| 1129 | `639bc1cde382d554f11be5b6e377c76bc9019104` | 2026-09-09 | kusanagima | fix(web)：增强在技能市场的搜索体验 |
| 1130 | `7acefea24c7e17dc2cc6dcfe42199db09989a20d` | 2026-09-09 | ray_le | fix(rsi): correct harness artifact handling and update dataset format tip |
| 1131 | `7bb28f64bffde41d3e83bfe8277cb32e3aa58b35` | 2026-09-09 | XinchenZzz | fix(heartbeat): align edit form with create fields |
| 1132 | `7f0c038364e860c6e69d741010103a3711e01342` | 2026-09-09 | layShang264 | fix(symphony): 使用当前会话模型执行技能图谱工具 |
| 1133 | `8193c954dd22519e432439b3ce01abd6bb9d0767` | 2026-09-09 | ldstyle8 | fix(rails): treat a serialised interrupt envelope as a tool placeholder |
| 1134 | `9616d87de21135fbb11b95f536aacca0b679e83c` | 2026-09-09 | lzf2025 | fix(memory): commit external memory sessions on cleanup |
| 1135 | `ae440da993311374a39c55c375add43f227fdf99` | 2026-09-09 | alan_cheng | fix(swarm): return the subject id from the chain read |
| 1136 | `bb88019c636eda727eb335901fe49cae3e0c2506` | 2026-09-09 | renquanjia | fix: 回退edge相关改动 |
| 1137 | `c4b86d22dece86527908df33ab68734b946e3029` | 2026-09-09 | hwxiajing | fix(swarmflow): 修复 team 卡片状态显示——session 状态聚合 + pause 在途反馈 |
| 1138 | `ccf9fd04701818de7c560932bb4681e07278f555` | 2026-09-09 | openJiuwen-bot | fix(team): Open the worktree in the team code mode. |
| 1139 | `ceaefb33955eab6c0f327cc33c24a1d86d7ce356` | 2026-09-09 | XinchenZzz | fix(heartbeat): support shared seven-field cron expressions |
| 1140 | `d74d43d2fae7b88ce8c1f44f9ea4b6a0bcf610a1` | 2026-09-09 | chenchunzhou | fix(swarmflow): bind swarmflow run lifecycle to the team runtime |
| 1141 | `dea9bb8b29acf4277c4ff1c28ed9656601bab0ff` | 2026-09-09 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#5446: fix(search): keep configured paid providers in sync across agents |
| 1142 | `e27354fbb270716af1ccf6acfe49ad73f0f54272` | 2026-09-09 | xiaoyao42 | fix(web): 团队成员退出后再新增现实问题修改，添加集群模式消息回复时间 |
| 1143 | `e716af112228e3748431091f8c3644443bd103e5` | 2026-09-09 | lzzzzzzasd | fix 1.右侧悬浮面板 折叠 展开xx个 恢复问题 2.隐藏展开代码 |
| 1144 | `f2498ee6e57d82e15f4304f69278b1cfdbd0882f` | 2026-09-09 | haowenzhong1 | fix(agentos): suppress prewarm sync requests and logs when prewarming is disabled |
| 1145 | `fba41452180a1daecfa1ac0634d28134a66433f3` | 2026-09-09 | AFSDFASD | fix(rsi): wire evaluation selector to LLM judger profile |
| 1146 | `029c76a64d4b2c754ae29aa5e4e8985f80d5673f` | 2026-09-10 | zhangyaomaggie | fix(swarm): delete built-in plugin-packages and move to hub |
| 1147 | `06cb2062d676402968872f3c8cf8cb94e42d45b7` | 2026-09-10 | kdgmpw | fix(claw): 删除旧版本的兼容迁移代码 |
| 1148 | `2e0827f107c69113f36ede17b8ed876212553aa3` | 2026-09-10 | cjh_jinduoxia | fix: 扩展页确认弹窗遮罩用 Portal 渲染到 body，修复灰底不铺满视口 |
| 1149 | `3486b2170eb649e373f75a9a566616012266bd2b` | 2026-09-10 | renquanjia | fix: 【同步分支】关闭免费模型 |
| 1150 | `3e3b66c7cdd6926c663166fa3417456452d8921e` | 2026-09-10 | Lexie-7 | fix:盘古anthropic端口修复 |
| 1151 | `412d2c109b9dcdc88ac77590c95dd52bac853620` | 2026-09-10 | openJiuwen-bot | fix(security): tool rails permission updates not taking effect in session |
| 1152 | `443e7e26d61b2f58abbdaa9c9a81ce4cd26f1732` | 2026-09-10 | XinchenZzz | fix(agentserver): preserve active gateway on stale disconnect |
| 1153 | `4f3d30beffc4d299773674b0ed59eeb45ef63e91` | 2026-09-10 | william9200000 | fix(web): 聊天文件卡片区分技能 zip 与普通产物 |
| 1154 | `640ec2e7fb63c747370f740715e07c512b00a46c` | 2026-09-10 | william9200000 | fix(skills): 修复重建技能报错问题 |
| 1155 | `716b30d70f662ed4fc2646a06496dbf2f54762b1` | 2026-09-10 | ldstyle8 | fix(history): correct history.jsonl records around HITL interrupts |
| 1156 | `74b9d6fb8e75ddeae2532e4ba5c61076b4e5fc4a` | 2026-09-10 | min_gitcode | fix(agent_teams): stabilize install and team state restoration |
| 1157 | `7c85e15d863f611b3513e764d3333029a42af6e6` | 2026-09-10 | min_gitcode | fix(agent_teams): improve external CLI settings and install dialog |
| 1158 | `8271d8ac8340f23b60326c8c3687a663c2fdef31` | 2026-09-10 | ldstyle8 | fix(permission): propagate project directory to team agents |
| 1159 | `83e5e122d3ba931636755db284744f435d4be1a6` | 2026-09-10 | ray_le | fix(mcp): suppress wecom CLI auto-opened auth browser to avoid duplicate tabs |
| 1160 | `88c6a6ac1a913b1caefc1717eff324afb340f678` | 2026-09-10 | kdgmpw | fix(desktop):防止web访问桌面端 |
| 1161 | `a39ca6e9e05366a4f775dbb184a1d9fc05a739f1` | 2026-09-10 | openJiuwen-bot | fix(security): Revert "Cluster mode toolbar not working" |
| 1162 | `a608347f3025f286a632fc68ee7e82aa3d2def12` | 2026-09-10 | ray_le | fix(personal-context): add context settings, services, content and graph UI |
| 1163 | `a7aadc59295f78661b6d3cb1b523959b0d1286a0` | 2026-09-10 | chenchunzhou | fix(swarmflow): team.task dedup survives the consumer restart on resume |
| 1164 | `abbcfa0f3b1ba6a1f76fb82134341e9c985b2a41` | 2026-09-10 | xiaoyao42 | fix(web): 统一子智能体面板样式并隐藏集群模式权限选择；修改插件描述字段为512；修复子智能体切换对话显示数据慢；修复某页数据无主对话区域内容时，页数推进受阻的问题 |
| 1165 | `b26adb1b71cfd645bcca47e1bb07becf3cf14070` | 2026-09-10 | zhangyaomaggie | fix(swarm): delete built-in agent-templates and move to hub |
| 1166 | `bcacc937482c6d9c87b6c1f8b3dfb65ee02a19aa` | 2026-09-10 | Lexie-7 | fix:删除模型默认temperature和top_p值设置 |
| 1167 | `d0c4ce2c82e1f642585effbe9e37f7d98af71472` | 2026-09-10 | xiaoyao42 | fix(web): 修复产物面板word显示不全问题；修复产物面板缺少滚动条问题；修复工具调用多的会话渲染慢的问题；群聊成员超长增加tooltip；屏蔽a2ui和whatsapp频道配置；移除免费模型及配置引导首页 MaaS 入口 |
| 1168 | `dcc49a9264d2ed73c0eb061fee4715b40566b429` | 2026-09-10 | kdgmpw | fix(cron): 禁止cron嵌套 |
| 1169 | `18731607d624e6bc62b7294879a8555d19379fc7` | 2026-09-11 | Lexie-7 | fix:修复盘古qwen3-32b模型工具列表传参 |
| 1170 | `27e4d376e19a1470cf5a2be3fc11aa4b9a0e6e19` | 2026-09-11 | ray_le | fix(personal-context): Improve personal context runtime and graph UX |
| 1171 | `5813977c204c32b7b95f9456b3fa05668c04edde` | 2026-09-11 | yechenzhen | fix(agentos): verify /file-api tokens with IAM instead of forwarding them |
| 1172 | `67a0d3c9d2aa5cb2de1456f261120d44240c6734` | 2026-09-11 | Lexie-7 | fix:kimi模型设置temperature和top_p |
| 1173 | `8ea63b1153c602a22b691648a268fe2fe927f844` | 2026-09-11 | william9200000 | fix(skills): 广场安装时持久化 display_name，使「我的技能」与广场展示名一致 |
| 1174 | `90b7fdd938a507b604a8814ab5eb10585ebcd349` | 2026-09-11 | alan_cheng | fix(swarm): fetch addressed content a reader turned out not to hold |
| 1175 | `b7da9ef98f357cbe152bd26017548d784ad8cbe1` | 2026-09-11 | Lexie-7 | fix:修复盘古qwen3-30b-a3b模型调用报错 |
| 1176 | `c15402d218143b41038c948eead7cd401688fdc9` | 2026-09-11 | Lexie-7 | fix:模型主模型占位符在首次设置时移除 |
| 1177 | `c94364d3ba276ec0d1aa7b39f1a0cd019dca80e1` | 2026-09-11 | alan_cheng | fix(swarm): rebuild an addressed chain as the array it came from |
| 1178 | `c982b1e52ea8c3b9dd98c9d165416b18e3ff8215` | 2026-09-11 | xiaoyao42 | fix：产物下载后打开文件的提示框强提醒 |
| 1179 | `ca8a1c0b50734491953f4d6b203d750a275d7b93` | 2026-09-11 | alan_cheng | fix(swarm): stop a resumed run from restating old user messages as new |
| 1180 | `8c7bfecdf0cc7607b07763fe687e08bf24f6ab83` | 2026-09-12 | lzzzzzzasd | fix(web): 统一详情页共享组件体系，拆分 SkillPanel 巨型组件，优化任务栏 |
| 1181 | `c0aacf206d0f8ed8ac13ed8565ec1055bdaaf669` | 2026-09-12 | zhaominjun1997 | fix(personal-context): fix context-link validation and collection liveness |
| 1182 | `84a64d1ba7a70ab2d4f744b7bcc9f09532488da0` | 2026-09-13 | yangzequ | fix(deps): adapt RSI runtime to openjiuwen 0.1.18 |
| 1183 | `1564fdccfdbf1c449185f5c9dd53df16bc367798` | 2026-09-14 | jack_ricky | fix(a2ui): 修复 Code 单 Agent 模式提示词注入失效 |
| 1184 | `28faf679514145cb0237e8a8ac011441a3aa547f` | 2026-09-14 | ray_le | fix(mcp): combine Hub install with connect and prefer builtin packages |
| 1185 | `41567e3a3ea74a130df1e440a92ec0355e462c0a` | 2026-09-14 | MengHan18 | fix(ascend-affinity): 适配 Session Runtime 生命周期并统一 KVC 清理 |
| 1186 | `43e796e1d9f4dac38b5a393a417410f8ad93b261` | 2026-09-14 | alan_cheng | fix(swarm): carry proxy and domain limits into trusted free search |
| 1187 | `4b031a4463243255c68757cacc8354daf0716974` | 2026-09-14 | kdgmpw | fix(cron): 修复一次性执行标志位没有联动编辑的问题 |
| 1188 | `64d6cec6a722218749565e0269d204120de1f8b3` | 2026-09-14 | kdgmpw | fix(cron): cron支持mcp |
| 1189 | `98f78a0c1c5bd7a6ce705d6af827e2e7c1df7041` | 2026-09-14 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#5490: fix(cron): resolve environment model names before storing |
| 1190 | `a165ad0c6aa1d3de14ba31ddf3756946a1256a62` | 2026-09-14 | liangfuqi | fix(mode): 修复web 场景未升级到标准三段式传mode |
| 1191 | `be2ce1750a49195ac7e3744c3dd303138a95d0d1` | 2026-09-14 | jack_ricky | fix(a2ui): 允许已验证的 Wikimedia 图片 URL |
| 1192 | `d9a71ca24cffec49024da21863b564f4d0f11415` | 2026-09-14 | wangcan103 | fix(swarmflow): 修复中断链路状态丢失与后台任务感知缺失 |
| 1193 | `fdd940a8a4ca6cc39e7aaba11158537855d800ac` | 2026-09-14 | kdgmpw | fix(web): 完善session、project的归档、恢复、删除的生命周期接口 |
| 1194 | `0165ea72c530f00d0f6b39341981ecc2a66cf8b9` | 2026-09-15 | birfied | fix(skills): 多文件种子的 artifact_path 必须是 seed 目录 |
| 1195 | `523d806d289eea5750b99369321d0071b9a4b9c9` | 2026-09-15 | zhangyaomaggie | fix(swarm): fix expert switch failure on first conversation |
| 1196 | `61de4533d86f0adb4aa355d201df99b2c6122b5d` | 2026-09-15 | hw_liuyu | fix(process-cli): 完善单 Agent one-shot 退出契约并修复交互续答边界 |
| 1197 | `75d98881f738ff59f8364202942f936e7108bb8d` | 2026-09-15 | ray_le | fix(mcp): bundle gitcode CLI into the frozen executable |
| 1198 | `7a291e78c45ec2dea0d0b32c5304dec0a54d8ce6` | 2026-09-15 | Lexie-7 | fix:web模型列表未填写key时没有默认模型 |
| 1199 | `7bb12fa4f8de3b32f49b411fc9dfcda78472ea96` | 2026-09-15 | AFSDFASD | fix(rsi): install harness refs through RSI endpoint |
| 1200 | `7f99fb2b782d41e0512fdd6875228cd8bbdf69f6` | 2026-09-15 | XinchenZzz | fix(heartbeat): manage interaction lifecycle in session runtime |
| 1201 | `97475178ff77cacf276d5c062525c85eb3222259` | 2026-09-15 | xiaoyao42 | fix: 修复大数据场景下会话、操作卡顿等问题 |
| 1202 | `998974bad5a418682e2bd718ab888f72c18a7602` | 2026-09-15 | zhongqinghua2 | fix:将分支选择和模块选择合并并改为平铺形式 |
| 1203 | `a9626dc4d103881bab27f334542adf874657d2ff` | 2026-09-15 | yyuse | fix(guardrail):权限引擎统一收编命令安全检查、更新权限配置文件 |
| 1204 | `af21766fdf6c3fd2af78bc8b4683a9a9f95d443d` | 2026-09-15 | zhaominjun1997 | fix(personal-context): complete reauthorization |
| 1205 | `dc4f6e9114fc07c8321523dd210d23e3f4cf11c5` | 2026-09-15 | yan_zj | fix(system priority): 1.Update section priorities and maintain the static registry. 2. Warn and error on conflicting priorities; sort ties by section name. |
| 1206 | `e12bcb31cf6b85a2f6d9ae9c192e75ae50752ed5` | 2026-09-15 | yangzequ | fix(rsi): keep task deletion resilient when cleanup is blocked |
| 1207 | `f0ca68a82b2d83c64222639a7e8d2f70c0d1991b` | 2026-09-15 | qq_42846434 | fix(gateway): AgentServer TCP 探测改用 URI 真实 host |
| 1208 | `f0d0bde84ab48fec40288d6dc3f3bbd2c8a855f7` | 2026-09-15 | openJiuwen-bot | fix(chat): keep assistant messages visible outside collapsed reasoning |
| 1209 | `f6ac7b70d0552a514625268740c01fc68d2f8df9` | 2026-09-15 | Lexie-7 | fix:model.context_window删除默认值 |
| 1210 | `029f3caf4a7100fba668d17d3953f3d87d683a67` | 2026-09-16 | xiaoyao42 | fix(web)：chat.final 字符串正文按原文展示，删除猜测性协议解包 |
| 1211 | `1b95fa0f625f2429f400061e7eb60e05dffa7012` | 2026-09-16 | alan_cheng | fix(trajectory): show streaming text when the panel opens mid-answer |
| 1212 | `21aa1104e18e736de7625d5caf7ab0dc41e126ef` | 2026-09-16 | l_x_d | fix(context): make model context windows explicit and configurable |
| 1213 | `36f387de3c5b239ebdf789a88b118d21cc00aac7` | 2026-09-16 | alan_cheng | fix(trajectory): stamp manual compaction as its own turn with a canonical mode |
| 1214 | `3c09f783cd1e68871875d75df497cbf8a73e420d` | 2026-09-16 | alan_cheng | fix(trajectory): render tool payload as the model's arguments |
| 1215 | `5403d33b2a58b8f7e5cf06bc4c97a5d18181613c` | 2026-09-16 | alan_cheng | fix(trajectory): delete Team and dormant sessions' trajectory databases |
| 1216 | `6afab97fa0adaf811e1556f5463ec572c21e092b` | 2026-09-16 | xiaoyao42 | fix：修复对话中有预览视频导致会话导出报错的问题；catalogpage删除重复classname |
| 1217 | `709fbdf99fc61c8530c84db8d64de32bebd361c8` | 2026-09-16 | alan_cheng | fix(trajectory): mark compaction requests without assistant rows |
| 1218 | `8a5695b8a625c5619ee081e001f0b7df1e00a989` | 2026-09-16 | ray_le | fix(mcp): resolve bundled gitcode CLI without runtime pip install |
| 1219 | `9aa7b89faa0d5c1da223b4d73b072eefe08cae2e` | 2026-09-16 | lzzzzzzasd | fix(web): 移除任务列表空状态逻辑，压缩条移出任务栏条件，清理冗余i18n   定时任务 table固定列宽 拖动列 |
| 1220 | `a2865d125817ed801d82f3b9f08af543fb5e6b65` | 2026-09-16 | layShang264 | fix(symphony): 修复取消图谱构建后普通任务误恢复构建的问题 |
| 1221 | `acf087c2ef1dca2e52a8532980834db858c20ea2` | 2026-09-16 | alan_cheng | fix(trajectory): show manual compaction between turns |
| 1222 | `ad874341427d9f02d89424522c3c011b319de1e8` | 2026-09-16 | alan_cheng | fix(trajectory): number manual compaction turns from the session tracker |
| 1223 | `b9012686233b8a3cc77176d6e8d2d111b0ee652c` | 2026-09-16 | alan_cheng | fix(observability): stop Team observability from overriding the configured exporter |
| 1224 | `c3968484d63f5ac8457f8416254fb7a23bd7155f` | 2026-09-16 | zhengkailan | fix: refactor AgentGroupDetailPage and GroupCard components for improved UI and functionality |
| 1225 | `c9f750b8d08312a9c081dc03d53fadc3ed08781e` | 2026-09-16 | alan_cheng | fix(trajectory): sweep session databases that outlived retention |
| 1226 | `cbb1c6ce4c13d080aa94f5ed70dc259c8eca5e0f` | 2026-09-16 | gp1000 | fix: scope media_items/files to current turn in history extra (#3407) |
| 1227 | `da07a90146cd2a437d604b0d202d7ce8bbd3fca2` | 2026-09-16 | alan_cheng | fix(trajectory): identify v2 events by their event kind |
| 1228 | `dff3aac6a66213e88fc5e6dab3c841743f6f6f26` | 2026-09-16 | alan_cheng | fix(trajectory): keep panel state bounded and follow the session's mode |
| 1229 | `0c19be5f1740343ccde138ef075aa84f1c19b5f7` | 2026-09-17 | alan_cheng | fix(trajectory): hide the actual time switch again |
| 1230 | `3f08df33f0f74c4fb208a1cbdc5681fca84ab023` | 2026-09-17 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#6108: fix (browser agent): Fix redundant verification of sourced answers in browser |
| 1231 | `454b983f0a4ca094b801872bc4c40d64644ba36e` | 2026-09-17 | JustinBieber1223 | fix(gateway): before_chat 钩子改写 query/model_name 实时回推前端 |
| 1232 | `47721e66fcd8f292cf5ee1b706557b1d5a525bbd` | 2026-09-17 | cjh_jinduoxia | fix(web): translate permission approval prompts under English UI |
| 1233 | `489298f9a69bdd947e3d45592e5ecd38478a6d71` | 2026-09-17 | yangzequ | fix(mcp): resolve bare HNP executables on HarmonyOS |
| 1234 | `4f1cd68e31799d0a0a568cc5b0f29243b42d4246` | 2026-09-17 | openJiuwen-bot | fix(heartbeat): 按 event_type 判定终端错误事件，修复失败轮误记成功 |
| 1235 | `530c25ea126931d85c5b5fbcc74864edd0ed72b1` | 2026-09-17 | gcw_db2yUMYp | bugfix:在会话中添加日期 |
| 1236 | `5518794546573e4a1d304689059397fdf44c4cbe` | 2026-09-17 | lzzzzzzasd | fix：优化detail-body样式间距 2.tips border优化 统一 PageCard 头像体系与滚动区间距3. 欢迎页蜜蜂气泡离开3秒后淡出隐藏，模型图标清理无效样式并修复抖动 4.输入区建议列表滚动条改4px并贴右缘，上下margin避开弹层圆角裁剪 |
| 1237 | `5714c0368ffc3a26847ce0ca4b14a5acbab79b52` | 2026-09-17 | william9200000 | fix(skilldev): 用 timezone-aware UTC 替换已弃用的 datetime.utcnow() |
| 1238 | `64571479c81c2821fbc59ee2b5ad40ab3aa084fc` | 2026-09-17 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#4426: fix: reject unverified agent bridge URLs |
| 1239 | `71dcea87c6cf480c11149635bea8a676d39b8098` | 2026-09-17 | alan_cheng | fix(test): align unknown rail test and drop SQLite RETURNING in change seq |
| 1240 | `76719997c0a826e0bd6bc3e22f3a4ecde1d32f93` | 2026-09-17 | ray_le | fix(personal_context): consolidate polling, add batchRefresh and background restart |
| 1241 | `7fdde42d290784636aae490629761a2a940828ac` | 2026-09-17 | chunbs_cbs | fix(skill): 内置技能添加来源标识 |
| 1242 | `8506bd28fbfca77c5afe0d2a48cdc4724c3c79bf` | 2026-09-17 | liangfuqi | fix(upload): fix end-to-end browser file upload workflow after Linux deployment |
| 1243 | `8cdd85a1a5a1a748572f5a2a825c20bd73adf4ad` | 2026-09-17 | lzzzzzzasd | fix 修复设计师检视问题 |
| 1244 | `9992f74d922c9531dbd889b0dcb089aa52226dcd` | 2026-09-17 | alan_cheng | fix(debug): keep enabled external CLI SDK extras on uv sync |
| 1245 | `c03409cf8037dad195dad9f9c628598229ebf27f` | 2026-09-17 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#6113: fix(llm): Fix using jiuwenswarm with Gemini failing due to wrong configuration |
| 1246 | `c07c57b13d45bba3c6f8453ea55b5126286ea682` | 2026-09-17 | kdgmpw | fix(session): 优化批量归档删除时的性能问题，修复部分bug |
| 1247 | `c3e98ce6798a42c23ff1a41049bfc86cabd24fc8` | 2026-09-17 | blueChild | fix(telegram): remove unbounded chat session cache |
| 1248 | `d7aa70217c6011af6e1e147a069cc118f2ecb28c` | 2026-09-17 | alan_cheng | fix(hooks): deliver PostToolUse additional context to the model |
| 1249 | `dd7defda417e4d841e8bd609bb72307185385cb9` | 2026-09-17 | maoxiangyang | fix: 新增【dev-stable专属】 企业级，删除【dev-stable专属】 稳定性 【dev-stable专属】 AI提效（开发质量、代码检视） |
| 1250 | `f7f499b9fd6c94d83c3afc989d52722ed3312108` | 2026-09-17 | yangzequ | fix(rsi): keep RSI Harness creation focused on the active Harness default |
| 1251 | `fcc1a28a7a2a07e414d6840d1b760facb6f8c9ca` | 2026-09-17 | kdgmpw | fix(code-mode,cron): 修复审核卡片重复、变更栏恒为 +0 -0 与 cron 失败原因丢失 |
| 1252 | `0253991c6ec7ad4c6e8acf6bcd0346f8057bacc5` | 2026-09-18 | Rossoneri_1007 | fix: 对话框的鼠标右键弹出菜单没有复制等 |
| 1253 | `1a5ae44138bd7d9706bd1efb57c5345231a473e3` | 2026-09-18 | yuhaiquan | fix：内置非自研技能详情新增标注 |
| 1254 | `1cf546f1d8bc9e0b2c7e573e14c8455cef49d801` | 2026-09-18 | kdgmpw | fix(session): 历史写卸载出事件循环，批量扫描降争用，迁移日志分类降噪 |
| 1255 | `23ce8c4a1b8a2c4eb65f442b32b05161674303e0` | 2026-09-18 | xiaoyao42 | fix：修复对话区域折叠频繁重绘的问题 |
| 1256 | `4b0708c8fb34ef1f2a3b7b50610a00d6b825665b` | 2026-09-18 | l_x_d | fix(context): restrict manual compact processors |
| 1257 | `4c2ffa2e4ed71311adaad4862e8124bade338781` | 2026-09-18 | l_x_d | fix(model): make context window configuration editable |
| 1258 | `962200208574140a0d82779ef86a904418b35114` | 2026-09-18 | kdgmpw | fix(cron): 执行会话以任务名作为标题，修复前端显示「未命名对话」 |
| 1259 | `b6cd34c2b1ea068b57cb6f9a91a0d15a0c9457f9` | 2026-09-18 | maxmadmax | fix(permissions): disable auto permission mode |
| 1260 | `d49626f58c05bed83fa6760df7d5d621749e6aad` | 2026-09-18 | lzzzzzzasd | fix(build): Electron 打包，调用内置浏览器 |
| 1261 | `da420e8045b5e6b5b18ba590b92041efec4668f7` | 2026-09-18 | yuhaiquan | fix：技能包前端适配 |
| 1262 | `da49aa1252a62fe0273f948d2246c149b66de60c` | 2026-09-18 | openJiuwen-bot | fix(team):stop sessions when the bound group is deleted |
| 1263 | `01c4b751659a4f6a6646711b1c287804cbb58ac5` | 2026-09-20 | haowenzhong1 | fix(timezone): embed timezone into the user-turn envelope timestamp |
| 1264 | `0244dc2b2d3c4e1343524b272608c15a20c900f6` | 2026-09-20 | haowenzhong1 | fix(agentos): increase iteration limits |
| 1265 | `0ad8067eab8dd887d4adc5eac68acb1702dcaa7c` | 2026-09-20 | huanghongyun | fix(marketplace): complete expert group release and integrate SkillHub OAuth |
| 1266 | `0e1d0d43bda01da06aa40f70fa1161cc41169d76` | 2026-09-20 | haowenzhong1 | fix(agentos): reconfigure stdio streams to utf-8 to fix unicode encode error on windows |
| 1267 | `2a7a670035d58c22593ad567adc12feed7244693` | 2026-09-20 | maoxiangyang | fix: dev-stable专属排序靠后调整 |
| 1268 | `2f086d1d40a46315a49fc602c3a6f093feb62b43` | 2026-09-20 | ray_le | fix(personal_context): apply fetch-service patch/delete via hot update to avoid timeout |
| 1269 | `48c7627fe5eaa24fae3435bbcc2ea24a65f94db9` | 2026-09-20 | zhangfuzhi | fix(auth): 回调落鉴权服务时减少无效认领、活动未开启时显示公告 |
| 1270 | `76ea608fff5c5378bf3cb32b7cc2419c34b0b9dd` | 2026-09-20 | kdgmpw | fix(archive): 修复team-session常驻流阻止归档的问题 |
| 1271 | `7b492c93c9d0730225ddf6bd22e66a249654848d` | 2026-09-20 | xiaoyao42 | fix：调整设置页面样式 |
| 1272 | `80766e7b90f979a4e0387cabe183acdcc908799f` | 2026-09-20 | openJiuwen-bot | fix(team): support complete file tree and PDF previews |
| 1273 | `8a183ab3f70bd8795f02f5a9cbd8ca097851d2b2` | 2026-09-20 | rivervale | fix: add English command desc |
| 1274 | `93ca984f48b2b1bdcdebdf21f0068a840ca5449d` | 2026-09-20 | kdgmpw | fix: improve proactive cron actions and execution feedback |
| 1275 | `949fd62f5618acc0b4a39ce3eb1e2a4c1066c5ea` | 2026-09-20 | haowenzhong1 | fix(agentos): pass through optional cmd field in TUI switch handoff |
| 1276 | `af423add4e008d3c884f7ecd617c1eef0b52e285` | 2026-09-20 | kusanagima | fix(web):在没有可以归档的情况下禁用按钮 |
| 1277 | `af58d979d190d095b88df738e51ad6b7816e4da4` | 2026-09-20 | gp1000 | fix(build): bundle sqlite-vec extension (vec0.dll) in frozen package (#4319) |
| 1278 | `cdf1d0fd4aa6b93f95565383e6e61913981af844` | 2026-09-20 | Rossoneri_1007 | fix：设置中的开关不明显，看着像是不可用等 |
| 1279 | `02b37f47fbefe80a58e4ed88606120818ca44ac3` | 2026-09-21 | yuhaiquan | fix(web): 子技能禁用联动技能包禁用 |
| 1280 | `03779644959d04e2d4dfcb6a954135cb384d8012` | 2026-09-21 | wangcan103 | fix(swarmflow): 看板暂停单个 run 后按钮自我锁死，改用 isPaused 判定禁用态Refs:4435 |
| 1281 | `08db5bf34b537a466de9dde0e511c6dbcd80595c` | 2026-09-21 | amelafejza | [mirror] openJiuwen-ai/jiuwenswarm#6055: fix(web): copy pretty-printed JSON from file preview |
| 1282 | `152fa2bf7d8cff29502c681c0cf00fab21cb20d6` | 2026-09-21 | kusanagima | fix:修复导出长图时的错误，给无头浏览器添加cookit |
| 1283 | `38eb6a0f369b7d7e972ba115e539f3c67b0e454d` | 2026-09-21 | ray_le | fix(rsi): refine personal-context and RSI model/action UX |
| 1284 | `397892769da788868c20b2eed7eb37d3654a77b5` | 2026-09-21 | zhangfuzhi | fix(auth): 免费模型报错只给用户看指引，额度查询失败显示"额度未知" |
| 1285 | `3bd94a87e5bf1fbfcd5e3e49879f7aecc1875b8a` | 2026-09-21 | ding_chi | fix(rsi): project persisted usage in harness provider |
| 1286 | `4990729f370059610fb13de5ecbd1fff05cce695` | 2026-09-21 | openJiuwen-bot | fix(agent-group): Clarify the incorrect information reported by the expert team |
| 1287 | `4a04a1b624ffa781878879b8b6e7ee90de28d1d5` | 2026-09-21 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#6774: fix(browser): preserve deadlines and focused delegation |
| 1288 | `4b8232dea523266c68f881677a1f6cbe0b351071` | 2026-09-21 | Rossoneri_1007 | fix：专家团：选择上传文件创建，使用win自带的winRAR打开专家团压缩包，直接将专家团文件拖到上传框后前端页面崩溃 |
| 1289 | `57bc3eca21ff48d6ac4c6196ea6b9e55429500ba` | 2026-09-21 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4365: fix(config): stop deleting user config keys absent from the template |
| 1290 | `5b0efbedffedf4f6018869699f56fb0288561b0d` | 2026-09-21 | harenome_ | [mirror] openJiuwen-ai/jiuwenswarm#4391: fix(config): ship the options the code reads in the config template |
| 1291 | `6b39f169963d6363358b546621f1f9209310dffa` | 2026-09-21 | CharlesWang2030 | fix(web): 超长项目名展示截断，修复下拉撑破控件与欢迎页撑破输入容器 |
| 1292 | `6f5d92f0c732fac38d4ba5a958b9fa9f0d4c2831` | 2026-09-21 | kusanagima | fix: add i18n features on tools_title and tool_category |
| 1293 | `70bdd1b5dbc22fc485f8d1e10e2864d0639160a8` | 2026-09-21 | zhaozixuan | fix(jiuwenbox): enable token authentication between jiuwenswarm and jiuwenbox |
| 1294 | `71819ef1f540c3937e537bae3eaea898d26a7f19` | 2026-09-21 | XinchenZzz | fix(deps): bound resolver backtracking candidates |
| 1295 | `7a13190db4253e66e69ca14483bde9e07cf4e65c` | 2026-09-21 | kdgmpw | fix(session): 修复归档列表偶现加载失败：扫描竞态逐项容错、归档时间解析移出请求路径 |
| 1296 | `7be7794929804cba3960d3211a6d0ec828af2b5c` | 2026-09-21 | XinchenZzz | fix(desktop): Modify the behavioral logic of the red close button for mac & windows app |
| 1297 | `7f4523501eec304735606496717dc564a6148eff` | 2026-09-21 | l_x_d | fix(rsi): expose task failure reasons |
| 1298 | `94ee634c9f367bc2d39efcb781791e5d1d691096` | 2026-09-21 | yan_zj | fix(systemprompt): Add the paths for agents, agent groups, plugins, and MCPs to the system prompt. |
| 1299 | `964af301e6edfaa7712df467bd702b9d7efa51bb` | 2026-09-21 | JustinBieber1223 | fix(mode): 补齐两段模式名归一，修复 chat.send mode=team.work 无法走到会话结束 (#4168) |
| 1300 | `9db403b7e306e2de8858874fbda1e06c4fef3bac` | 2026-09-21 | lzzzzzzasd | fix 设计稿以及问题单检视问题 |
| 1301 | `9dcad79a2d4b8e1e8236bb756aca56f633355bae` | 2026-09-21 | xutuo | fix(symphony): 静默构建技能索引，优化前端选项展示 (cherry-pick) |
| 1302 | `a06c5db02b65b2d15051cd2fe417e5d57401cc48` | 2026-09-21 | XinchenZzz | fix(desktop): persist managed child process logs |
| 1303 | `aa03fb98d7004f7aeb18b624b6c9a356fc659041` | 2026-09-21 | yangzequ | fix(rsi): cherry-pick bugfix to develop |
| 1304 | `af73f666d4539aef55b5a85e6d32d7e2bec17d4d` | 2026-09-21 | wncbdj | fix(session): Session exception triggered during plan mode execution |
| 1305 | `b453667dbec3fac6f37d0a6cc00bb5a9037009f5` | 2026-09-21 | yechenzhen | fix(agentos): CAS unregister stale sandboxes by instance_id |
| 1306 | `bb29b3d7a8581ae1829b0f74e427af454b0b0c81` | 2026-09-21 | zhangfuzhi | fix(auth): 桌面端一键切换华为账号，重开应用保持登录 |
| 1307 | `bf5b6d88fca9fda46e3d897c93cb9fda97087e55` | 2026-09-21 | kdgmpw | fix(cron): cron执行会话全面下架cron与心跳工具 |
| 1308 | `c2a2466d047857d1e838ff85a5aee14bc5feae4e` | 2026-09-21 | ZHANgPP_ | fix(a2ui): Modal 弹窗透明及点击打开后表单消失 |
| 1309 | `c361a7110a0bef2c20abd046d38aa82ac21f7adc` | 2026-09-21 | xiaoyao42 | fix(chat): 修复虚拟列表滚动时的内容丢失与位置跳变 |
| 1310 | `c763694802e1672a7388487a8b72acd22c4e8e23` | 2026-09-21 | zhangyaomaggie | fix(swarm): Uninstalled Hub connectors require installation prior to connection |
| 1311 | `d88ef45aa54da11489e1598a74f7c81afb20b0e8` | 2026-09-21 | yechenzhen | fix(agentos): keep SSH network-failure cleanup off the cancelled relay |
| 1312 | `dd49b7d90a106ecad62ec90fa664aa0e09cd76b6` | 2026-09-21 | amelafejza | [mirror] openJiuwen-ai/jiuwenswarm#6157: fix(web): stop chat copy from showing Copied on clipboard failure |
| 1313 | `e0d1affee910217ea9716193b8f831fccb017d14` | 2026-09-21 | CharlesWang2030 | fix(web): 定时任务模式选择下拉菜单随滚动/缩放重定位，不再脱离触发器 |
| 1314 | `03858674b655f21630d246de5fdfd649a125b511` | 2026-09-22 | kdgmpw | fix(project): 项目删除改为移除语义,会话归档保留置顶 |
| 1315 | `0a79c25f3434093c82e1159d1d7d1d9ddd97647d` | 2026-09-22 | alan_cheng | fix(trajectory): draw a turn that failed without leaving a record |
| 1316 | `0d25fda804304b6b187d99176849b1a97fb0cffd` | 2026-09-22 | l_x_d | fix(context): publish usage after manual compact |
| 1317 | `14ab349751cf6db759c0771523478ede69329213` | 2026-09-22 | yangzequ | fix(rsi): align openjiuwen with the cherry-picked fix baseline |
| 1318 | `1b057b61c7771366db29cae14e3f189ed7b9dd13` | 2026-09-22 | lzzzzzzasd | fix(desktop): remove default menu to prevent Alt from revealing menu bar; adjust cron session list padding |
| 1319 | `1b2221adf1800399f94b46c09ca3dbd6f4ecdf53` | 2026-09-22 | zhangfuzhi | fix(auth): 登录未完成时给出提示，免费模型支持自定义上下文窗口 |
| 1320 | `31caa3314e01f1586b5bb2e2facc3cb60369ccb3` | 2026-09-22 | william9200000 | fix(skills): 修复技能广场搜索结果重复问题 |
| 1321 | `3722dd1c70b3780502778ba682ffec6aad087815` | 2026-09-22 | kusanagima | fix(web): 增加在批量归档的时候交互信息 |
| 1322 | `493c38e4784e945b3394a3b6df58a1728a94f635` | 2026-09-22 | ray_le | fix(mcp): add cancel_connect to abort in-flight auth and filter stale marketplace records |
| 1323 | `5445077f6e4de6ebb4c72536e5c84667b36be0cb` | 2026-09-22 | ray_le | fix(personal_context): stabilize collection status, refine Feishu auth flow and drawer UX |
| 1324 | `5a6f6f861f055eb4c8a246d9defb1046a795f832` | 2026-09-22 | wncbdj | fix(agent): remove general mode and the configuration for max_iterations of react |
| 1325 | `63b4f7be93e945991e2125ef76f587f82dc7a958` | 2026-09-22 | openJiuwen-bot | fix(agent-teams): The newly established expert group has not been ranked. |
| 1326 | `876e537af7f99d3eef37bc38f11bb034381278da` | 2026-09-22 | kdgmpw | fix(session): 取消不得遗留生命周期栅栏，废弃归档操作按目录位置收尾 |
| 1327 | `8bdc904ff23cda7bf070d1680067ade92612619f` | 2026-09-22 | zhangyaomaggie | fix(swarm): hide the edit button on the expert detail page |
| 1328 | `91bf75144c01cfa4cf4f963c0cdb9c72f42744c8` | 2026-09-22 | zhangfuzhi | fix(auth): 免费模型上下文选256K在集群模式会沿用provider的真实上下文 |
| 1329 | `aa456fc139d4588cc06452a2aa408a8290c14beb` | 2026-09-22 | hw_liuyu | fix(desktop): 修复 exe 端 webp 上传失败且无法重试 |
| 1330 | `ac4edc4886ee9c13a60605b5a4e2dcb4dcd64ecd` | 2026-09-22 | xiaoyao42 | fix：修复下拉框样式和delta显示问题 |
| 1331 | `e32e242c2b1cd2e81f0f1f0be96c14c2ab4fac91` | 2026-09-22 | yuhaiquan | fix:技能包子技能删除后不支持安装 |
| 1332 | `e5b4013dd68e68c68a9051727dbcdc2419153b9f` | 2026-09-22 | kdgmpw | fix: avoid blocking event loop during session pin and log timings |
| 1333 | `e829cd8bf86bbe7a3b0a4004709677f272c2e5e1` | 2026-09-22 | openJiuwen-bot | fix(agent-teams): only check local groups before creation |
| 1334 | `eefb003c6747f59d4e760cdb9f04e7e82109843a` | 2026-09-22 | kdgmpw | fix(gateway): 修复归档会话时 request_id 撞号导致请求被拒 |
| 1335 | `f74d88bbf6e24d88f829449a36c5702edf2c6467` | 2026-09-22 | xiaoyao42 | fix：调整技能图谱内容遮挡问题，统一详情页加载中文字高度 |
| 1336 | `fb456f7ac522c754a501787df898a2438d13dfc5` | 2026-09-22 | yuhaiquan | fix：技能包“去试试”跳转到单agent模式 |
| 1337 | `24d1474be1585d2ce7b1d7cd7b26adf8df71f651` | 2026-09-23 | kdgmpw | fix(session): team 会话常驻流不再阻塞 session.delete |
| 1338 | `2760cd3434457fce6d5ba7e39f7ff5b9aeb46e4e` | 2026-09-23 | wncbdj | fix(tool): image_tools delete loaded environment variables |
| 1339 | `2d2d6eb909fe8bcbf890264b131b337f4720bd28` | 2026-09-23 | kdgmpw | fix(agentserver): pause latch 无超时等待导致会话挂死，fail-open 并修复 resume 竞态 |
| 1340 | `307d3b3a574e8524d718799b15a5b28078cddf4f` | 2026-09-23 | liangfuqi | fix(session): 支持恢复会话显示报错信息 |
| 1341 | `5e856d8ce6214950c0e7bf50964ce9519a8e2548` | 2026-09-23 | zhengkailan | fix: stabilize expert team management and chat flows |
| 1342 | `6a06e2a11663fc46439f1ffd6e25ce1c4be6ec17` | 2026-09-23 | alan_cheng | fix(trajectory): end the trajectory view at the docked composer's edge |
| 1343 | `71a84190583479a7cc334d6bec952842175c4918` | 2026-09-23 | hw_liuyu | fix(web): 删除未发送图片时清理会话 uploads 副本 |
| 1344 | `829e5f4da84beb6260882fba9351ac049a8f5ecb` | 2026-09-23 | kdgmpw | fix(cron): ask_user/审批中断在无人值守会话中快速失败 |
| 1345 | `8ea9059f504df9dd53d3256446eabe42d67da3ce` | 2026-09-23 | alan_cheng | fix(trajectory): replay an imported archive in the mode it was exported from |
| 1346 | `9459c283ef4e0f6ac5e39dc093a56203f4ebeb54` | 2026-09-23 | yuhaiquan | fix：手动创建专家/专家团确认按钮样式loading冲突 |
| 1347 | `969120d4679eea1112f6101052a2ec2e41cc072a` | 2026-09-23 | liangfuqi | fix(session): 问题澄清问答落盘与刷新后历史恢复，会话生命周期错误码端到端透传与前端 i18n 翻译 |
| 1348 | `98e4f8b4550a11e741c96a820df4923a9999b354` | 2026-09-23 | kdgmpw | fix(cron): preserve team mode and session visibility |
| 1349 | `9e3620618b249d27fdb591d0f7e9a332801808d7` | 2026-09-23 | huanghongyun | fix(web): improve marketplace loading and publish guards |
| 1350 | `b5fddb35c249846ca6cba546636d980f4762038d` | 2026-09-23 | xiaoyao42 | fix(desktop): unify native clipboard paste and plain-text input |
| 1351 | `bc27f03fff59605ff32e78fc6de75a3ecbdd4ab1` | 2026-09-23 | cjh_jinduoxia | fix: 目标覆盖弹窗改自定义组件、统一goal与plan的命令联想及互斥逻辑、修复目标执行中被误关闭及忙态判断滞后问题 |
| 1352 | `c30c24a28690346d743b286c663cabba631d89eb` | 2026-09-23 | hw_liuyu | fix(attachments): 上传 .jpeg/.jfif 时保留原文件名 |
| 1353 | `d9a281c75a18e83ba510a9fb5a864974dd5a2eb7` | 2026-09-23 | alan_cheng | fix(trajectory): show a visible error when an imported archive is invalid |
| 1354 | `e7871c410c5e9e098c4800159b943a2a086e5805` | 2026-09-23 | kdgmpw | fix(cron): reload 丢弃已到点未消费的 wake/push 导致循环任务整轮静默丢失 |
| 1355 | `e8f383339a88a8ce932f5e00d0dbdbec947ff06b` | 2026-09-23 | kdgmpw | fix(project): 项目移除之前添加 session busy 的判断 |
| 1356 | `e9effea98402ea729987ca6764fb0a8b14313426` | 2026-09-23 | openJiuwen-bot | fix(agent-teams): The expert group for creating natural language cannot have the same name as any existing expert group. |
| 1357 | `ec2cbba05a5a4fcc837877514bb8cc74e1811961` | 2026-09-23 | XinchenZzz | fix(desktop):  adapt close dialog to scaled text |
| 1358 | `f09af5df116170940f0c5b721c9dd3d74020feb7` | 2026-09-23 | alan_cheng | fix(trajectory): tell which turns ran before recording was turned on |
| 1359 | `f4017e15db48df6f11d38b24d03f712c3ef75212` | 2026-09-23 | yechenzhen | fix(agentos): delete sandbox and registry row when create never reaches running |
| 1360 | `f948096fd685d3a72e4d29b9343750978beaf604` | 2026-09-23 | alan_cheng | fix(trajectory): resolve static check findings and stream frame tests |
| 1361 | `0ec524c8504edbdf931f11ffb8994cdfb7e66827` | 2026-09-24 | JustinBieber1223 | fix(gateway): reject oversized IDs and prevent log sanitizer backtracking |
| 1362 | `193a1487bd045d56b414812648638dbac7a5afae` | 2026-09-24 | weichenhao | fix(agent-teams): Stop generating team names from the first user query via TinyAgent. |
| 1363 | `1b9b03f6330e60bbf09f7b1d5d64569c7f8c1525` | 2026-09-24 | lin-xiaoyu | fix(symphony): 优化技能编排触发时机 |
| 1364 | `29ea7eed940fe4cdbd13dc7bd811beed0730e5f3` | 2026-09-24 | min_gitcode | fix(agent_teams): redetect reselected CLI path |
| 1365 | `349a82416ffc445a7e1ffa7e16ec6338900a9d53` | 2026-09-24 | yuhaiquan | fix：1.技能广场更多前端传参最大50；2.技能广场不调skillpack |
| 1366 | `35b39b0812f179e6eb3eb4d2b1c7b1596f2c34e3` | 2026-09-24 | gp1000 | fix(audio-tools): avoid IndexError when artists list is empty |
| 1367 | `394ed6b8de4863e63c1628e088888a093f6c57c7` | 2026-09-24 | haowenzhong1 | fix(web): remove IAM login page and cookie-based auth bridge |
| 1368 | `3bf0ccada801b9e3bd03b79d5ce5eec7f95b9026` | 2026-09-24 | XinchenZzz | fix(multimodal): distinguish unknown image capability from unsupported |
| 1369 | `4706d97d5952e43beb94f5cb9267d7033ba3472a` | 2026-09-24 | Lexie-7 | fix:模型与模型组唯一ID迁移 |
| 1370 | `4aede0766cea9a14e6cd2566f8a074c6b8fd2695` | 2026-09-24 | ldstyle8 | fix(rails): scope RailManager registration state per agent so custom rails load in every session |
| 1371 | `5e275ea7ac502a3b5a8142edd06c494770ac5761` | 2026-09-24 | Lexie-7 | fix:修复盘古dsv4未带thinking问题 |
| 1372 | `6051a88327d32797e98d76bc850e055680a8ac2d` | 2026-09-24 | yangzequ | fix(rsi): cherry pick rsi bugfix to develop |
| 1373 | `63594ecbc7b1308e2e772ec02852a441f54af303` | 2026-09-24 | XinchenZzz | fix(updater): preserve downloaded desktop installers |
| 1374 | `6ab1ed59a8c455cdb9dfe45db34a3a8d42c10d47` | 2026-09-24 | zhengkailan | fix(web): preserve dialog confirm contrast on hover |
| 1375 | `7ee0009a76de11a229735e06b688cbec3e1b16be` | 2026-09-24 | kdgmpw | fix: fence archive execution and complete session busy checks |
| 1376 | `8536fe3f6b3f204bd609f9ada8e4bf73bcd65b11` | 2026-09-24 | kdgmpw | fix(archive): swarm flow 结束后的收尾窗口给出可区分的 busy 提示 |
| 1377 | `9ddedd61bb0ca8ff0aad9ec337b363301d913fb2` | 2026-09-24 | XinchenZzz | fix(cron): surface empty execution failures |
| 1378 | `b23eae8946e55937b25301b2577feb8ed038bd34` | 2026-09-24 | ldstyle8 | fix(cross-session): Fix cross-session message bugs, and remove /new & /side commands |
| 1379 | `b2c8fc54cb9997e74e0247d0f64a50aa73fb62a8` | 2026-09-24 | layShang264 | fix(Symphony): 修复 Symphony 指纹提取被固定 Token 上限截断 |
| 1380 | `b7a7c32564a98eb2033271ed93b7ad0124fc92c8` | 2026-09-24 | kdgmpw | fix(cron): 兜底回写补齐 project_id，防止存量会话二次查询后从列表消失 |
| 1381 | `bb3df76c49d523e333d35d6fe356e7dd880b26c0` | 2026-09-24 | lin-xiaoyu | fix(symphony): 修复技能包生成配置实效的问题 |
| 1382 | `bb736b8e2d684b18691284b8b49240990cc28d69` | 2026-09-24 | kdgmpw | fix(project): 项目下没有定时任务时，移除项目不再提示"其定时任务已停止" |
| 1383 | `c96c747b984d3e275b86079eab06397fe27c34e5` | 2026-09-24 | XinchenZzz | fix(branding): refresh browser and IDE icons |
| 1384 | `e0f8f17c149ecb1e3b84dd00342df1ff3e96d443` | 2026-09-24 | zhaominjun1997 | fix(personal-context): parse Edge bookmark folder filters |
| 1385 | `ee7693f2b2b8b12d12d19589fe00b55fa1517499` | 2026-09-24 | XinchenZzz | fix(gateway): recover chats after stream failures |
| 1386 | `02df635548791d155ce591bb6e0badb2333681ad` | 2026-09-25 | Huang, Zeyu | fix missing Chinese translation (#38) |
| 1387 | `52abe68db2dd167485f6bd79d6e36e193d608e64` | 2026-09-25 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#7316: fix(visual_video_gen): change free input text to dropdown list |
| 1388 | `54fb290786710b278154f6768ecd9e7fa0ab0ea1` | 2026-09-28 | kdgmpw | fix(runtime): 仅执行中的会话阻塞删除归档，清理常驻 Team 请求 |
| 1389 | `669f8d7af3d56f4342f07aafc3691e13b35c6188` | 2026-09-28 | Huang, Zeyu | Bugfix: generation error ends up in infinite loop (#55) |
| 1390 | `880adf23deccb96bccf9a1254253ea8a89bc8b48` | 2026-09-28 | zhangfuzhi | fix(auth): first message after being idle for some time prompts not logged‑in when using free model |
| 1391 | `8a24f8d8403b98f4de80bfe55062542fa729a170` | 2026-09-28 | william9200000 | fix(skills): 修复技能广场先展示“暂无匹配的技能”的问题 |
| 1392 | `94d76cdb9d9e1ba72d1275b7ae0c47897af12540` | 2026-09-28 | zhangfuzhi | fix(auth): first message after being idle for some time prompts not logged‑in when using free model |
| 1393 | `b119633856586ae0ff19deb7a0de29957af5611e` | 2026-09-28 | kusanagima | fix(web): 增加删除按钮以及重构代码结构 |
| 1394 | `bdc4f1c2ead7f5157696193a4ac79f354e2804c9` | 2026-09-28 | ldstyle8 | fix(stability): survive slow web scrapes without tearing down the service |
| 1395 | `dc447fa055cf5a736422fc42888114a7495e7202` | 2026-09-28 | kyrie_irving11 | fix(ttse): isolate TTSE cleanup from skill-evolution watcher cancel |
| 1396 | `e3b7039ad1f51393c3ddf676bb93dd6160fcdab3` | 2026-09-28 | cjh_jinduoxia | fix: 任务执行中禁用追求目标模式（含 /goal 与 /goal set 入口） |
| 1397 | `e7c4e7f72ad24bcdbb27c01e630fcca1b603549f` | 2026-09-28 | hw_liuyu | fix(process-cli): route slash commands locally during REPL turns |
| 1398 | `ef9a1db42264798f99f35a0c5476ab9cabeb69e2` | 2026-09-28 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7352: fix(全双工): 全双工适配权限管理卡片以及输出语言配置 |
| 1399 | `fc18e5c572a6b3b62bb42ea843cce674140e4266` | 2026-09-28 | kdgmpw | fix: improve free cron handling |
| 1400 | `21d335484d7921ea917588d4e5fd4f997fc1eac8` | 2026-09-29 | Huang, Zeyu | fix(designer): retry failed nodes when continuing a failed run (#79) |
| 1401 | `6a60cacce7969a5953f927b2750e4b5ac644212a` | 2026-09-29 | Huang, Zeyu | fix(designer): remove too many negative/global guide in prompt (#77) |
| 1402 | `8ff2e7c749992e6f0b8cd8cfae9a77662789644a` | 2026-09-29 | hw_liuyu | fix(agentserver): persist team ask_user answers in session history (#4475) |
| 1403 | `9b3706f3a59dd0ecb66626693206e7eeaacba005` | 2026-09-29 | Rossoneri_1007 | fix: 解决专家团选择上传文件创建的问题 |
| 1404 | `a070f4a22fcca2a6134557cc4b3ffd39b90adf1d` | 2026-09-29 | maoxiangyang | fix： 新增【dev-stable专属】 其他类型 |
| 1405 | `a487b88772e2d07924007d180a28f2dc2b979a13` | 2026-09-29 | kdgmpw | fix(cron): 定时任务列表首开提速——读盘下线程、闸门计时日志、挂载请求并行化 |
| 1406 | `c49ef2f7c9dff0daf9e3e36505a2e5ec95f3f435` | 2026-09-29 | Huang, Zeyu | fix(designer): preserve authored visual style end to end (#63) |
| 1407 | `cc429b42da55ebd1a2c11c31090b61944333b8bd` | 2026-09-29 | zhaominjun1997 | fix(personal-context): cover six collection audit fixes |
| 1408 | `e628d626ef4d8d8e50ff66124227d2750544de63` | 2026-09-29 | Huang, Zeyu | fix(designer): disable generated audio nodes (#64) |
| 1409 | `ebdf0bfcad4663c02dc22a43b08e745b483016fc` | 2026-09-29 | openJiuwen-bot | fix(rail): stop executing previous command by default on reboot |
| 1410 | `1e3bcf85c3556f5d29f4e37e5d645a05474b051e` | 2026-09-30 | lzzzzzzasd | fix 本地样式调整与 UI 组件更新 |
| 1411 | `3194e595deeb4d810faad7bfc8e00be934ec7824` | 2026-09-30 | ldstyle8 | fix(hooks): stop execution after PreToolUse blocks a tool |
| 1412 | `8904825d095ff12188874aacb6e7bbe53e53b59a` | 2026-09-30 | yyuse | 修复整工具会话内记住不落盘问题 |
| 1413 | `99de0473e9647a8ddfcae869245b359556f7ef34` | 2026-09-30 | Huang, Zeyu | fix(session): scope user history event types (#101) |
| 1414 | `a28ddd035746f5907cdbc3a891afa02acb15fb94` | 2026-09-30 | Alicia | fix(designer): complete chat edits and document synchronization (#87) |
| 1415 | `f2c364376c61aa4dd80a6bd752878c3fce364652` | 2026-09-30 | Huang, Zeyu | fix(web): restore TypeScript project references (#100) |
| 1416 | `f3c157980e9b2b9e1449655e46e288e6ba0e3b18` | 2026-09-30 | gp1000 | fix(web): return 400 for malformed file-api requests |
| 1417 | `43bab7f9e42f121a33d56c277fd4a00dc9592af7` | 2026-10-02 | BarakeelFanseuKamhoua | fix(designer): clear billing latch on next chat/Play entry |
| 1418 | `ac4fb2a4c57f1e83407829269dd27ab11b3ff9c1` | 2026-10-02 | Henry TIAN | fix(designer): default clips to spoken dialogue |
| 1419 | `007df59e142ff26330abe3b1148569ba2726535c` | 2026-10-05 | Henry TIAN | fix(designer): stamp type when chat inserts a shot |
| 1420 | `9b0c1465f924b97009eb55c4ca102e495fda86e2` | 2026-10-05 | BarakeelFanseuKamhoua | fix(designer): companions from analysis-covered only; multi-still cap 5 |
| 1421 | `a2f444d1d71af3a759fe95155b5204c0ff67d9ad` | 2026-10-05 | BarakeelFanseuKamhoua | fix(designer): video_binding drives R2V; pack all refs under Wan cap |
| 1422 | `a53fd8dbb4662879807d8df57781a1b065cdd448` | 2026-10-05 | BarakeelFanseuKamhoua | fix(designer): reference-led use-as-is pipeline with companion cast/set |
| 1423 | `0b0a957fd0f92da0ac4f055397e48bea643651b8` | 2026-10-06 | BarakeelFanseuKamhoua | fix(designer): UI string for ref img limit change |
| 1424 | `0cf680291381a0380f2881bddde45f1ad1c13f73` | 2026-10-06 | Henry TIAN | fix(designer): keep color edits on the named object |
| 1425 | `23b96c624d71544c4b6ddbd7a67b862b28709a85` | 2026-10-06 | BarakeelFanseuKamhoua | fix(designer): cover companions by identity, not shared match_terms |
| 1426 | `2b622b89de8a28bc2e4e5d08e905a5071676db55` | 2026-10-06 | BarakeelFanseuKamhoua | fix(design): honor user prompt authority and remove stale shot refs |
| 1427 | `2bbea4c50e9c4c3455222d34399080c13579b992` | 2026-10-06 | BarakeelFanseuKamhoua | fix(designer): snap clip duration per video model capacity |
| 1428 | `a886c0ca4e67901f958674cd8c0c255ba6dfedf5` | 2026-10-06 | Henry TIAN | fix(designer): roll back chat edits outside the named substitution |
| 1429 | `f4431356a86b1bd17b5aa2271057f040b55ce846` | 2026-10-06 | Henry TIAN | fix(designer): keep original qualified phrases when rolling back a chat edit |
| 1430 | `6a68e78cd7f035623956560d9254bb296d995c5b` | 2026-10-07 | Huang, Zeyu | fix(designer): resolve AI review on GitCode 7667 |
| 1431 | `a97b2f06156b6abaa1d732ecc84175f275c11be6` | 2026-10-07 | Huang, Zeyu | fix(designer): key trajectory files by required project_id |
| 1432 | `f7b6e3884b33b2054460841f367d3ccc0e318d35` | 2026-10-07 | Huang, Zeyu | fix(designer): canvas mouse and textarea fix |
| 1433 | `1b576c8381db5cdf6e7a788de7f4216077f7c5a9` | 2026-10-08 | ZezeChang | fix(swarm): guard organization descriptions and persist expert errors |
| 1434 | `46a1aafdb749ea1ec482fc2fa0d6d07e9904e23c` | 2026-10-08 | hw_liuyu | fix(process-cli): 确保单次调用退出前完成会话落盘 |
| 1435 | `6f09530627ae8a40c4150457b192125289cfd475` | 2026-10-08 | XinchenZzz | fix(build): bundle gateway adapter submodules |
| 1436 | `c4fc68471d53354d9505381866a32e5563c98383` | 2026-10-08 | zhangfuzhi | fix(llm): 修复流式工具调用被拆成两个导致会话持续 400 |
| 1437 | `d2bd6cb2108d68200004c5059a8c1512b3198d0f` | 2026-10-08 | liangfuqi | fix(upload):修复远程linux测试环境文件上传弹不出文件选择框 |
| 1438 | `ff3071dda2dbe0b42afc706985d59f02c4a2d345` | 2026-10-08 | Henry TIAN | fix(designer): stop a timed-out clip agent from submitting a second video |
| 1439 | `18be8cbe92e0dfbab8e228af1d510509a44c5908` | 2026-10-09 | lzzzzzzasd | fix 统一提示/空态/表单抽屉样式组件，连接提示改造为常驻 Toast |
| 1440 | `4522e8a219c1f526aaedc41f70f4b457ddba54dd` | 2026-10-09 | MengHan18 | fix(ascend-affinity): propagate turn number to affinity requests |
| 1441 | `4cad4bc518dc1abff5305956015d5bf65ec35a29` | 2026-10-09 | lzzzzzzasd | fix(chat): 1.收敛 MCP/技能选择抽屉，修复连接器错误双 Toast 与停用技能过滤丢 2.失斜杠命令建议的 "/" 文本占位换成 command 图标 |
| 1442 | `5022f4826178bd8df33b77d3be697ae6f577be6d` | 2026-10-09 | GeorgeGcs | fix(web): show full team member names on hover |
| 1443 | `648259bea6381d6e7edbfe65e99d29cd0189307a` | 2026-10-09 | kusanagima | fix(web):添加斜杠命令筛选，在欢迎界面不允许选择压缩和fork命令 |
| 1444 | `655bbbedbede0aed09a48589aa0ce47bc88b433f` | 2026-10-09 | GeorgeGcs | fix(test): verify delayed blob URL revocation |
| 1445 | `893101f103bb7729f55ac1e880fccf7fee21c3cb` | 2026-10-09 | ray_le | fix(personal-context): restore graph canvas styles to match 0.2.7 release |
| 1446 | `899fd636e83dfb84778cd317e4d67757bbe3d2c2` | 2026-10-09 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7803: fix: 调整全双工配置默认值与完善全双工错误信息显示的细节 |
| 1447 | `8c75ae0c463b29636c79bf81e288d100c9ebd03a` | 2026-10-09 | yangzequ | fix(frontend): keep unsupported sidebar entries hidden on HarmonyOS |
| 1448 | `9e20e56daef0f4d4b81c5becd235c5847159b340` | 2026-10-09 | openJiuwen-bot | fix(rail): 修复重启后默认继续执行老命令 |
| 1449 | `ab8f1210f8ebd7723476533843d5ce61f655a903` | 2026-10-09 | ZezeChang | fix(team-org): stabilize organization UI and summary execution |
| 1450 | `b38a00227e9add4612c5aed3c66f69c98f970d71` | 2026-10-09 | yechenzhen | fix(agentos): leave address empty on first registry registration |
| 1451 | `eb5a2cf9d40290e993e3952a9e63f16e7a2c8019` | 2026-10-09 | ray_le | fix(mcp): validate env-token CLI credentials at connect |
| 1452 | `eb9d3dea31930a00d25a5e7048aba77cc1b70a55` | 2026-10-09 | GeorgeGcs | fix(web): show full slash skill descriptions on hover |
| 1453 | `0cfb6b05366a19d9e4d9fc1ff999d6b807db1066` | 2026-10-10 | ZezeChang | fix: address team organization codecheck findings |
| 1454 | `386de7f9d4178b7b3a6ae02ed088a7831b1c1057` | 2026-10-10 | alan_cheng | fix(trajectory): distinguish reasoning previews while preserving assistant labels |
| 1455 | `6b4ab42c0ca804f612c695b26676c214affeefb3` | 2026-10-10 | ray_le | fix(mcp): detect missing PowerShell during CLI install and surface friendly error |
| 1456 | `6fd403c42fbdd93cf0ee2f383128e653ffc816cf` | 2026-10-10 | alan_cheng | fix(agentserver): encode Front pushes and preserve delivery results |
| 1457 | `8107bf8ec23e1bd79bf69a9fcc42ddff0eda6735` | 2026-10-10 | douran | fix(code): 注入项目级 agent.md/claude.md 为 coding 模式 prompt attachment |
| 1458 | `a39311cb212a2effca3c132822216e25cd7fb9a9` | 2026-10-10 | yuhaiquan | fix:1.发现技能包的开关误删；2.结束会话仍然显示“思考中” |
| 1459 | `ac8b00de80cdad08a398929959a10e2917987e32` | 2026-10-10 | yuhaiquan | fix:“问题澄清”导致技能保存卡片渲染丢失 |
| 1460 | `b0019db1f41bca55d502fdbdac6402051e47abcf` | 2026-10-10 | ZezeChang | fix: preserve organization wire adapters after rebase |
| 1461 | `4623441c64536da8d9a0be7d6f7b445db7765f67` | 2026-10-11 | Huang, Zeyu | fix(designer): correct rename & delete internal ai docs (#6) |
| 1462 | `072244280a3c26ba7370eeda1cd3c2e439baafb7` | 2026-10-12 | hw_liuyu | fix(process-cli): 修复终端 EOF、嵌套追问展示及 Team 输出串字 |
| 1463 | `1620ae60dcd8950e3a9e49a19a82dd5283b44318` | 2026-10-12 | BarakeelFanseuKamhoua | fix(designer): do not call the image model again for a node's own image (#8) |
| 1464 | `2dae26d4df7c4c65ed39c661fbfc8ea660717098` | 2026-10-12 | xiaoyao42 | fix(attachments): unify upload limits and preserve attachment batches |
| 1465 | `33a46c4de1da9641b849235e56146bb3e83aa98b` | 2026-10-12 | kdgmpw | fix(agentos): skip template migration for deployed YuanRong config |
| 1466 | `520d40a01e35fc5b96b1b08f3047fde84bffaa76` | 2026-10-12 | Huang, Zeyu | fix(design): UI gap between sidebars |
| 1467 | `55fb54fe545f9cfb25410ed19f04b0f59984cfc9` | 2026-10-12 | ray_le | fix(rsi): restore RSI experimental feature toggle to match 0.2.7 |
| 1468 | `8d7fd8631534df5e44713f682a67cc25335f994a` | 2026-10-12 | maoxiangyang | fix: 提交飞书任务通知配置信息 |
| 1469 | `a3d05d9756edea2c7d5bc94767a55ee372b10e6c` | 2026-10-12 | alan_cheng | fix(team): remove the iteration cap on the team leader |
| 1470 | `cfd577c5d332da8c2479238c10e9f81171539381` | 2026-10-12 | Huang, Zeyu | fix(designer): align leftover prompts with clip_action (#7) |
| 1471 | `f92e2fbfb83d6d1bb5b2deffd24e1aae910f7e90` | 2026-10-12 | gp1000 | fix(web): return 400 for malformed static paths instead of dropping the connection |
| 1472 | `1ba578d922f6bbe4af06157e0b5eeb37678e9e5e` | 2026-10-13 | wangcan103 | fix(swarmflow): 动态工作流新增 Worktree 隔离会话级开关Refs:5171 |
| 1473 | `258501900f0be4152b5f3f5bc81975f67d0b66bb` | 2026-10-13 | XinchenZzz | fix(packaging): add installable a4p wheel dependency address |
| 1474 | `366c42810be01560a90f47d145704cb8269431a5` | 2026-10-13 | alan_cheng | fix(debug): stop the resident runtime worker on jiuwenswarm-stop |
| 1475 | `493789542e42aab1d475867d37e41019fb967d4d` | 2026-10-13 | hw_liuyu | fix(process-cli): 移除 --work-mode，非交互命令仅支持单 Agent 普通模式 |
| 1476 | `5118f982482dc8c6d1275b523ef69ffe3e1e0eb7` | 2026-10-13 | chenchunzhou | fix(web): Carry runId on swarmflow session graph nodes |
| 1477 | `6148a8e82275e794cbf543f9a42b9d0292b2455c` | 2026-10-13 | maoxiangyang | fix: 解决issue分支和模块不联动问题 |
| 1478 | `62a32c0684673465bb630375c497ad6bcd232611` | 2026-10-13 | maxmadmax | fix(permission): 修改自动审批UI显示效果 |
| 1479 | `6592d91ae57e2bd874433ecf80e222abc02fac62` | 2026-10-13 | kdgmpw | fix(agentos): 补齐SANDBOX_IP快照注入契约，对齐agent_os分支实现 |
| 1480 | `6711a5c5634f7742c96a5cffef504793b87374b1` | 2026-10-13 | chenchunzhou | fix(swarmflow): Mirror a tool result that pairs back after its call row was seen |
| 1481 | `6ec1689745fc308751eb4bf46e8b1118b132dfe5` | 2026-10-13 | hw_liuyu | fix(process-cli): 修复流式思考输出重复分段 |
| 1482 | `73919d8286f4390645a02e618923ac3b09752f48` | 2026-10-13 | xiaoyao42 | fix(desktop): use Electron renderer for share image export |
| 1483 | `7963a4c91bc156e894a5c05c6d047c46f2ea627b` | 2026-10-13 | chenchunzhou | fix(swarmflow): Push the buffered activity merged into the status delta that flushes it |
| 1484 | `880c0b7ef2f77a2948558e260993538e51c3a197` | 2026-10-13 | yyuse | fix(permissions)：修复安全护栏热更新不生效问题 |
| 1485 | `957f1093b779dcfbda6e13a6ffa8b5a08682d895` | 2026-10-13 | XinchenZzz | fix(agentserver): dispatch frozen worker module arguments |
| 1486 | `a6b9ea2e0d4e9b36a0ebc75543020688f0e9cc35` | 2026-10-13 | Rossoneri_1007 | fix(models): complete model group backend & frontend integration |
| 1487 | `b19a9e659e1dd52e73412935cc3fbdb663645cc5` | 2026-10-13 | XinchenZzz | fix(packaging): fix the electron shell packaging script of dmg & exe |
| 1488 | `cb3390af33220b7f9eb655a62fe21f4273f220c5` | 2026-10-13 | kusanagima | fix(web)：修复前后端时间不一致时顺序出错的问题 |
| 1489 | `d525558d0069aa0130f9c99e98109c3a455f96bc` | 2026-10-13 | chenchunzhou | fix(web): Restate openjiuwen git requirements when installing a CLI agent SDK |
| 1490 | `d798d82dbe98839ac5af6da53d82c0ff7d04e3ed` | 2026-10-13 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7614: fix(video-duplex): hand tasks to Core Agent and sync tool-result language |
| 1491 | `ec230c6b43f26ad3aa6a3a1571f7dc98e40fa1d5` | 2026-10-13 | haowenzhong1 | fix(skill_sync): add skill sync API for split server/client deployment |
| 1492 | `f693cac23f3c4bc2e62881200bae3785ef958146` | 2026-10-13 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8166: 修复 JoyAI 语音与全双工任务委派，并隐藏集群模式入口 |
| 1493 | `00a21a6c6378bf1565b6155fb3e3a1c8f1e2fe6d` | 2026-10-14 | zvby | fix(swarm): gate org tool mounting behind spec-level enable_organization flag |
| 1494 | `19a40598774b886fa65512676821f21b3272f36b` | 2026-10-14 | XinchenZzz | fix(session): accept cron IDs in model selection |
| 1495 | `426a334162ec22f5058ede472257e796163af7da` | 2026-10-14 | xiajing | fix(designer): disable virtual timeline in dock; remove custom history restore |
| 1496 | `519fe2d94acbdd88b1ac38f8f61ed4a0eee630be` | 2026-10-14 | xiaotian077 | fix(xiaoyi): 小艺渠道修复与能力收敛（会话复用 / 心跳下线 / push 截断 / 关闭文件回传） |
| 1497 | `673c498459fe58289bad1e1bf6a181159b53bd45` | 2026-10-14 | alan_cheng | fix(web): show the full member lane name on hover in the team trajectory |
| 1498 | `7bf3448b1629be12d6fbdabb5974affa9d69703d` | 2026-10-14 | xiajing | fix(team.design): sidebar design default node queries and default-project check |
| 1499 | `8b9dc9d6e0eb9c3b4ac874cbc904fba20d0550ee` | 2026-10-14 | birfied | fix(cron): log stream_gen.aclose failures instead of silent swallow |
| 1500 | `8e55d11b9b56d482976b1baf76852fe3854eb328` | 2026-10-14 | xiajing | fix(team.design): canvas page RPC breakages, title stamping and colleague layout |
| 1501 | `99e5bba2d8bb37e641e377a24d27263ea144a82e` | 2026-10-14 | Henry TIAN | fix(designer): stamp identity refs from character spec nodes (#24) |
| 1502 | `9e372ee387812e88f898cf82c82cc8b1aaa7af47` | 2026-10-14 | xiajing | fix(team.design): frontend fixes after the design-develop merge |
| 1503 | `af09fb6cb0d86316d7a1192e8f5fcdbb185036a5` | 2026-10-14 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7638: fix(hooks): apply user hooks to subagents |
| 1504 | `bd8b48763a0166ff9e09aeb8ff449b80a1bd3aa4` | 2026-10-14 | yyuse | fix(permissions)：修复工具总是允许后无法撤销的问题 |
| 1505 | `c637be1c2f64a4100f8c76736b31a89b3758fcce` | 2026-10-14 | alan_cheng | fix(web): default the scheduled dispatch toggle to off |
| 1506 | `d323b67efb92d37b8a6c29770e71548f6ccb9b6e` | 2026-10-14 | huanghongyun | fix: 修复手动添加专家团使用 Hub ID 导致启动失败 |
| 1507 | `e6e44b4ecfe5758ff4a44c2c5f690f84ba48b160` | 2026-10-14 | kdgmpw | fix(cron): 文件版 update_job 原子化，修复并发写回互覆 (#4964) |
| 1508 | `f4e8efee30dea53a77ecfb2e6d64562405bbadd9` | 2026-10-14 | huanghongyun | fix: 统一我的资产名称搜索并修复澄清回答误脱敏 (#4473, #4470) |
| 1509 | `ffa6c1e5b2d0e04bba5037f74bd1489fbe070a91` | 2026-10-14 | xiajing | fix(team.design): re-adopt colleague media settings, prompt-as-is, and save_dir after pick replay |
| 1510 | `00344b9927014e25e7ef4d4412708b6ef722e88a` | 2026-10-15 | yechenzhen | fix(gateway): 修复web会话复用跨请求事件串扰与chat.final内容为空 (RC-3) |
| 1511 | `0d769e9e82ce68454538e0ec889bb3559f9f26d8` | 2026-10-15 | hh0205 | fix(team): Update the default model group routing strategy |
| 1512 | `0e9a73045cd3a8f92b2eb70c008514b9ae0aa6aa` | 2026-10-15 | XinchenZzz | fix(desktop): align Electron file drops and downloads |
| 1513 | `173fd30e3bc0d379c4e8da085a1cb663625a3599` | 2026-10-15 | alan_cheng | fix(team): rebind monitors when the runtime pool replaces the leader |
| 1514 | `184089a5584bed74afe8ce06774afa91cd988474` | 2026-10-15 | xiajing | fix(team.design): surface real media errors and upstream-failure attribution |
| 1515 | `1d4c73ae0cc37af3d59eda5c36d585e29118f8c4` | 2026-10-15 | min_gitcode | fix(team): persist unattended team round events to session history |
| 1516 | `1da30a0197e993d188e7b62961f666acb1183e32` | 2026-10-15 | xiajing | fix(gateway): route designer canvas pushes by session, not the request ws |
| 1517 | `1e2c510492eda3e3cb4751d7c4bda2c09b593228` | 2026-10-15 | alan_cheng | fix(observability): back off trajectory hint retries without a Gateway |
| 1518 | `2783d61c7bb910907142ae2c505ee1807c3131de` | 2026-10-15 | Huang, Zeyu | fix(team.design): bind reference-node literals in the compiled script (#34) |
| 1519 | `2c564596a2c6d66835aaaeac6cf96db1f32f97b6` | 2026-10-15 | alan_cheng | fix(swarm): persist team member stream output under its own identity |
| 1520 | `32bca274bbf31ef15341efe318ec30a25cdcaace` | 2026-10-15 | yuhaiquan | fix：前端适配优化三方Agent能力 |
| 1521 | `3c550e26fff246328c830cd6f6786038322456d1` | 2026-10-15 | birfied | fix: stop sibling candidates after adopted node reaches threshold and show skipped state |
| 1522 | `435039e8b74c6c1979f2255d50ee9666ac3a2d35` | 2026-10-15 | XinchenZzz | fix(electron): set WorkSwarm Windows file properties |
| 1523 | `4467ccaec2c6941cd2ddefdc749bfeb6d89e40ea` | 2026-10-15 | xiajing | fix(design): keep the minted run id across client graph saves |
| 1524 | `571bec10ff8be5287b876a56f96ff86cc4e0d604` | 2026-10-15 | yukuanj | fix(a4p): 开启A4P时为定时任务接入权限护栏，补充四个文件工具的预授权、更新A4P文档 |
| 1525 | `577ec94852421f475fb1af04dd7eac848ca5ee6a` | 2026-10-15 | GeorgeGcs | fix(version): show installed wheel build version |
| 1526 | `6634505a1100cbcc2bb52e7936074e8c72c6a590` | 2026-10-15 | Huang, Zeyu | fix(designer): remove the cycling leader status bar above the canvas (#35) |
| 1527 | `6e9d9cbbed50cb080ba342f0019825f1aad1f85b` | 2026-10-15 | Henry TIAN | fix(designer): move node labels into common and bound the companion pipeline test (#42) |
| 1528 | `700d4f8f159f08594238172d29318e93d4eb3f09` | 2026-10-15 | Huang, Zeyu | fix(designer): resolve logic conflicts with develop |
| 1529 | `75afcd1f7ea57e54850613f70dae8c846d5e5ba1` | 2026-10-15 | kdgmpw | fix(gateway): 修复 CLOSE-WAIT 泄漏与 SIGKILL 连带全栈停机 (issue #4946) |
| 1530 | `859110f7fdd952fcb85f55ea48b17ee3319363b6` | 2026-10-15 | zvby | fix(swarm): mark the assembling team as owner to avoid blank web conversation flash |
| 1531 | `8c0ab716fa56994a7fb94c9e690508677888fa8b` | 2026-10-15 | hw_liuyu | fix(agentserver): Worker 心跳与健康检测完整修复 |
| 1532 | `8f20c7b6a6a199ae64d944299146555e8538dfb8` | 2026-10-15 | kdgmpw | fix(cron): 流完成无文本也回写 last_session_id (#4980) |
| 1533 | `98e053877638ae58af6fb23a4efc9a4f97f212fc` | 2026-10-15 | Huang, Zeyu | fix(server): import develop fixes for team roster, streams, and gateway push |
| 1534 | `9a8e4c5ba2f4e4a61e8e76025f06e87c2fb03f72` | 2026-10-15 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#7848: fix(GenTools): Allow Internal and International models for Image and Video ge... |
| 1535 | `9c99e20e63af584ee1e09cd5a876d4e830e80846` | 2026-10-15 | Henry TIAN | fix(designer): report the real member failure and retry text generation once (#39) |
| 1536 | `9dcf8fe660d1ab8e252bfd4276eb6acb0adbb667` | 2026-10-15 | BarakeelFanseuKamhoua | fix(designer): scene plate prompt keeps the authored setting description |
| 1537 | `a76f96e445c384844de40ad508c2a4219d950d53` | 2026-10-15 | kdgmpw | fix(server): forward sync subagent activity and heartbeat to parent stream |
| 1538 | `a81d16926c470739016e09db28d1836a4c0978a3` | 2026-10-15 | xutuo | fix: 修复 Worker 诊断夹具的异步连接接口 |
| 1539 | `adfd92c5b9d2ed871cacaec8b20630bd62280792` | 2026-10-15 | Huang, Zeyu | fix ci error |
| 1540 | `be4dcd74dd30e93fe1756a0c5f1c8ff79720463d` | 2026-10-15 | BarakeelFanseuKamhoua | fix(designer-ui): toolbar prompt shows run-stamped prompts; lock filter only matches banners |
| 1541 | `c478cd99806024f61003844602e37f4b54d8ae02` | 2026-10-15 | wangcan103 | fix(swarmflow): 会话预算耗尽时向 Web 端推送终态错误提示Refs:5114 |
| 1542 | `d5de7dafa793034b0c4753adbc04ed3ca62cb22a` | 2026-10-15 | alan_cheng | fix(web): keep dormant team members on the roster |
| 1543 | `d815242f2ae3f6dec7bb5107330761fa14e5a68e` | 2026-10-15 | Henry TIAN | fix(designer): clear the severe code-check findings that still apply (#41) |
| 1544 | `db3199999c6e1073107680e2ded991dc833db2b2` | 2026-10-15 | Huang, Zeyu | fix(designer): overlay run controls on the canvas in dev mode only (#36) |
| 1545 | `df23e8d1680634111c43fca34e52b9433a736cd1` | 2026-10-15 | alan_cheng | fix(front): keep the push channel on the Gateway connection |
| 1546 | `e38384e8d71cdec96e8caa4bb638041a661d12b8` | 2026-10-15 | kdgmpw | fix(gateway): reject non-string mode before web routing |
| 1547 | `fe0b8aba98940bb41c23dac0c361739b32cea144` | 2026-10-15 | Henry TIAN | fix(designer): resolve general-level code scan issues (#44) |
| 1548 | `057e86125e47c455bbaa167607432b593bad3a64` | 2026-10-16 | XinchenZzz | fix(models): remove reference checks blocking model and model group deletion |
| 1549 | `0f11f8e701a1192fb787f215548e688b55dbbac6` | 2026-10-16 | Rossoneri_1007 | fix：模型推理配置选择开启或关闭，保存成功后仍显示使用默认值，且后续新建会话模型不可用等 |
| 1550 | `1c31cfa69689e6a18425cfcd511aff98cf794e8f` | 2026-10-16 | yechenzhen | fix(server): AgentServer 主进程（Front）注入点装配——B1' 遗留 P1 |
| 1551 | `3548d192d8211a2b15480f519752239a6e26478c` | 2026-10-16 | xiaoyao42 | fix: 优化对话/实验二级列表展开收起逻辑；对于服务端部署不存在浏览器的情况，支持通过用户自己的浏览器进行会话导出下载 |
| 1552 | `50806b11b1f823334bffe2acf9c2e814dd61d312` | 2026-10-16 | XinchenZzz | fix(designer): include designer pipeline in wheel |
| 1553 | `5a0fd9ca6a286775d70b5d986d727d9c2d04081d` | 2026-10-16 | yuhaiquan | fix：1.三方Agent前端优化；2.会话模式切换清空技能 |
| 1554 | `8fc076a70b39a8e45d583028bc573089aeb21677` | 2026-10-16 | XinchenZzz | fix(models): restore saved API key when editing models (develop) |
| 1555 | `9e0569f4fd047e510f4ee4ff28c26c54aaec5066` | 2026-10-16 | hwxiajing | fix(swarm): keep team.design members out of progressive tool exposure |
| 1556 | `aea8e10a7722e814d2dfd25dd9fa32b16dc4a3c2` | 2026-10-16 | zhaozixuan | fix: sync code from agent_os branch |
| 1557 | `e72f68673558fb570106e3339d97fa35bafb59a6` | 2026-10-16 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8201: fix(rails): keep paused tool calls out of the circuit breaker |
| 1558 | `660d28b26c70390ac1ac8f48dc1513dd149983ed` | 2026-10-17 | min_gitcode | fix(team): treat team attach subscriptions as passive observers |
| 1559 | `682b5b82fbf71dc122d5fab4c6e113cbe07191c1` | 2026-10-17 | yukuanj | fix(a4p): 授权确认等待超时由 5 分钟缩短为 3 分钟、补充英文文档 |
| 1560 | `9a734b440cb89ae1891010b8160d2ad1c75e9089` | 2026-10-17 | kusanagima | fix(web):给dialog组件添加portal功能避免z-index出错 |
| 1561 | `028d236b756f1f825853521fdcd74c2723dc2c75` | 2026-10-19 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#8351: fix(browser): configure Jev credentials independently |
| 1562 | `040037f1c6b05261621ac4c2b128ed909dbda3dc` | 2026-10-19 | kdgmpw | fix(swarm): block nested cron creation in cluster team cron runs (#5116) |
| 1563 | `10ebaf1dec4e0f90c8bc1dec13ba8467a3d142cb` | 2026-10-19 | hwxiajing | fix(design): refuse a single-node rerun whose upstream produced nothing |
| 1564 | `1372eda94970bf91d5dc0cb03d784e971f39b991` | 2026-10-19 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8495: fix(media): keep a submitted video job pending through transient provider errors |
| 1565 | `1409b174220be5ff46f1556292f305da49d05146` | 2026-10-19 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8490: fix(designer): preserve fields in partial node patches |
| 1566 | `225bb71387d4199247c1396322101b289022ec89` | 2026-10-19 | lzzzzzzasd | fix 修复专家团广场外边框 |
| 1567 | `2fb4b73654cd21953f83dc992256f614be7ba6bd` | 2026-10-19 | birfied | fix(feishu): 解析 post 富文本正文，避免 Agent 只收到 [post] |
| 1568 | `36e831481d7c005b4137a5d502c3373dca98852c` | 2026-10-19 | hw_liuyu | fix(process-cli): align session list headers and columns |
| 1569 | `3d95d617b285e2b15a1b757ba2fa7da8e1265f99` | 2026-10-19 | MengHan18 | fix(ascend-affinity): stabilize KVC target and offload lifecycle |
| 1570 | `4d789502c34ccbde97152194365f3ad75813099c` | 2026-10-19 | z2tng | fix(swarmskill-creator): support verify and session forks |
| 1571 | `62e5b7cd46c00c7a3668d6a3d7ef16083c0293f2` | 2026-10-19 | yukuanj | fix(a4p): 修复智能审批模式误启用 A4P 预授权 |
| 1572 | `8a4073832c9d47efedeedde0f7d4e778f2c2a5f2` | 2026-10-19 | huben | fix(runtime): stream question and confirmation replies before turn completion |
| 1573 | `8e0f173b46e00b1ae19bd76748f7f6e7af9516e1` | 2026-10-19 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8208: fix(rails): keep tool arguments an approval rewrote |
| 1574 | `9c5be6de7270ba912fe556e8217dd056ab24419b` | 2026-10-19 | kdgmpw | fix(gateway): allow unary request_id reuse after completion (#4940) |
| 1575 | `9d379d58cef0aa1cc6086dac6e47e7fdabb23c4b` | 2026-10-19 | grandocean | fix(attachments): cap images, not the whole mixed media batch |
| 1576 | `b2e668d17303867a1ae75325c1f0f29b196172ed` | 2026-10-19 | zhaozixuan | fix: add conch to valid sandbox type |
| 1577 | `b465f45c8cb059117ffd64c92178493239b613d1` | 2026-10-19 | XinchenZzz | fix(moa): allow single-member model groups in agent chats |
| 1578 | `bf2738e5cf6ec89e2be6e672028317f24f493001` | 2026-10-19 | yechenzhen | fix(gateway): 补齐 gateway/extensions/__init__.py——B2 合入版 wheel 打包修复（P1） |
| 1579 | `bfec97839d7575ee79842184f2296f37f6fb6fe0` | 2026-10-19 | ZezeChang | fix(swarm):修复任务结束后组织后台turn的结束通知失效问题 |
| 1580 | `c7b2a78f78862df08a8996ab620db161ae0b3b89` | 2026-10-19 | kdgmpw | fix(session): ride out transient PermissionError on metadata reads |
| 1581 | `d11719cd4ab14a3ac29a50dbb2fed87119b3a50e` | 2026-10-19 | maoxiangyang | fix: 更新飞书通知人信息 |
| 1582 | `d7ec1156e50cd6115e64eb4ec0a5d53505cd5789` | 2026-10-19 | xiajing | fix(design): write node outputs directly on every trigger type |
| 1583 | `e8a3538e2f79aaf8fb31ede5602922a360714f40` | 2026-10-19 | birfied | fix(team): team.task.updated 帧携带任务当前 status |
| 1584 | `ee2e3ef25da2b43d9552cbca01a5daa0c4669e8c` | 2026-10-19 | ding_chi | fix(marketplace): drop locally derived publication status |
| 1585 | `eedd064b06ac1d08b7a18234d5b85f8f950a729f` | 2026-10-19 | chenchunzhou | fix(swarmflow): stop firing the invite-human card for mirrored human nodes |
| 1586 | `f219cdbcdfd020331934f842f86515768dc4f632` | 2026-10-19 | hw_liuyu | fix(process-cli): 修复 Shell 命令绕过文件访问授权 |
| 1587 | `0b0d09c4c4883c3eddb1ef0b905d844df65a82e9` | 2026-10-20 | gp1000 | fix(build): bundle CLI channel modules in frozen package |
| 1588 | `1aa4d739635c0987e458823683e3d73c253af197` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8494: fix(designer): record why a member tool refused the call |
| 1589 | `23a6cea0ee29031c64ebb424fdce57a62767eefa` | 2026-10-20 | GeorgeGcs | fix(web): preserve expert team display identity |
| 1590 | `277ff5ed3c3e96201c18f319c7c2e62549d59a0d` | 2026-10-20 | alan_cheng | fix(settings): preserve external CLI choices and PATH candidates |
| 1591 | `28a547dae01542fd15044697fc4a9f037134f70a` | 2026-10-20 | xiajing | fix(design): rebind the worker spec for canvas reruns after a restart |
| 1592 | `2d1d044052013cef46a51e138ca4e70736f45619` | 2026-10-20 | tangfei0306 | fix(ask_user): 修复飞书渠道唤起ask_user卡片时一直卡住的问题 |
| 1593 | `3a7ca5f48fa127fea44611b4572f85832e53c289` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7922: fix(stream): keep model retries out of assistant replies |
| 1594 | `42bceb22ba627c50d4156fa60c3e788a2f7f88c8` | 2026-10-20 | xiaoyao42 | fix(export): reclaim lost share-image jobs and bound idle uploads |
| 1595 | `489ef05f67aed40c2943600609b04e5c38273db9` | 2026-10-20 | hw_liuyu | fix(worker): 修复同步热路径阻塞导致的 runtime worker closed |
| 1596 | `4e8a29c7e537b1951f5e8eb552729fd43709c2a9` | 2026-10-20 | yuhaiquan | fix：模型hover图标不显示tips |
| 1597 | `5417281cef325cc817bc9424fcc698a738dd9d64` | 2026-10-20 | grandocean | fix(designer): ship the Designer skill corpus in the wheel |
| 1598 | `5b97d9b88f26aca4d7c443f1fe7e6b435b10408f` | 2026-10-20 | XinchenZzz | fix(chat): remove empty model group prompt |
| 1599 | `5fc068e702c25dd9b485fcd46287ab0cae0c6dcc` | 2026-10-20 | GeorgeGcs | fix(web): prevent duplicate models in group editor |
| 1600 | `61f52e58ce03d2593c2ddbde0b5b479c824ff805` | 2026-10-20 | maxmadmax | fix(permission): 修复自动审批人工弹窗的全局&会话内规则落盘 |
| 1601 | `68990db04e99510452ec58659afa640afb6cf85c` | 2026-10-20 | kdgmpw | fix(cron): 手动创建定时任务放行"起点/步长"写法的cron表达式校验 (#5138) |
| 1602 | `6956a3e8015ace2ab06dedc7dc35feef3965b6c5` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8482: fix(designer): gate the Design landing prompt when chat/video/image models ar... |
| 1603 | `7f6e0a004e72bbd59252aa6d4d8d8f497386c02c` | 2026-10-20 | alan_cheng | fix(team): resolve expert package paths and skip duplicate marketplace ids |
| 1604 | `897c67d0a8134cb659b8bcfa8b6fcca7ee5b68bb` | 2026-10-20 | birfied | fix(skills): follow preferred_language in Knowledge-to-Skill prompt and missing SKILL.md error |
| 1605 | `978a3e6100243f01793775ef1ab32f52f4220bd3` | 2026-10-20 | min_gitcode | fix(team): disable task_tool on team leaders by pinning subagents empty |
| 1606 | `ab2c15d50afd83dce5cf16a8b19082075de4bc64` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8492: fix(designer): dispatch ComfyUI nodes through handlers |
| 1607 | `adb9f58ed2f2b6a571eb434329014325dbf93481` | 2026-10-20 | alan_cheng | fix(agent-group): validate member names against the team routing pattern |
| 1608 | `b3e796f7666bc2ef59efe2faa8860ebccb769ff9` | 2026-10-20 | GeorgeGcs | fix(web): hide context window in generation model settings |
| 1609 | `c31346560b77a9a1730fee51b6e29a812a51d318` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8801: fix(web): confirm before uninstalling an expert team |
| 1610 | `d814a3fcca3380b24e1018b155c32765a69c52f3` | 2026-10-20 | Rossoneri_1007 | fix：三方Agent，手动创建三方agent专家/普通专家，下载manifest.json失败；点了下载，下载失败后，再点复制，复制也失败（如果直接点复制，可以复制成功）等 |
| 1611 | `e1f526ec49f18965c5fb194965c3d5ca2ef6b066` | 2026-10-20 | yuhaiquan | fix:三方Agent1.检测悬浮文字修改；2.地址下拉框放开；3.一键安装成功提示 |
| 1612 | `e2b9b25e6e5a36bc3e2393cc1f25fa315cd2daf9` | 2026-10-20 | GeorgeGcs | fix(web): ignore stale project list responses |
| 1613 | `f6a88081e1e4481c05e344d3273fa62fdb390372` | 2026-10-20 | birfied | fix(web): 拒绝跨站 POST 到 /file-api 变更接口，防止 CSRF 驱动 localhost |
| 1614 | `f7e7dd5c82d4908276a8524fe94ceda41a8ae354` | 2026-10-20 | zhengkailan | fix(agent-group): preserve conversation names after uninstall |
| 1615 | `f90ae42f21f92c971f08e4f761d68757ed8364b9` | 2026-10-20 | zvby | fix(swarm): auto-suffix organization id claimed by an inactive owner team |
| 1616 | `0b6df7b3ea05bbb46561816ae6b7f3a011a89107` | 2026-10-21 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8874: fix(design): mount web search/fetch tools on the design leader |
| 1617 | `112336d5d732e1114a81e46e77b808f18cbe0501` | 2026-10-21 | Rossoneri_1007 | fix：【免费模型】免费模型描述有误 |
| 1618 | `13952c72f93790128632945169a9859b3a3dca0e` | 2026-10-21 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8899: fix(designer): upload canvas assets through the authenticated file RPC (#5325... |
| 1619 | `347170060e40fe35e9e4c11071200ac92af492dd` | 2026-10-21 | yuhaiquan | fix：定时任务“立即执行”成功后页面立即跳转，成功 Toast 被跳转吞掉 |
| 1620 | `47dacd1cef018660ffcbf8a75f916fc1ea0a5936` | 2026-10-21 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8895: fix(design): reveal the node prompt panel and explain moderation blocks |
| 1621 | `5b7a632646838175a5f6da18b623b4d90487455a` | 2026-10-21 | hw_liuyu | fix(runtime): 修复多session并发时心跳中断 |
| 1622 | `5bb9bf5e46e2207b7845112f322d3251e837650b` | 2026-10-21 | kdgmpw | fix(cron): 禁止手动创建/修改 proactive.tick 模式的定时任务 (#5128) |
| 1623 | `63b0d0fd447704f6da22381182d5b7702b893bf3` | 2026-10-21 | kdgmpw | fix(server): refresh cron context on first Web follow-up (#5511) |
| 1624 | `6d64d4ae7e4fa1a8c444b306d396714e94a1b8d0` | 2026-10-21 | yuhaiquan | fix：切换三方Agent导致安装态丢失 |
| 1625 | `702dbe066ba524f7a96d7a89839d99634fea48a3` | 2026-10-21 | alan_cheng | fix(team): announce the roster after spawn_expert registers a member |
| 1626 | `7d9b024e9c8baa524277f59743674c717d0654cc` | 2026-10-21 | hw_liuyu | fix(process-cli): 非交互式命令拒绝权限授权并结束执行 |
| 1627 | `b8b72d30f6ebc10719d156880e1761ddaf7f6a14` | 2026-10-21 | xiaoyao42 | fix(settings): use hover sidebar navigation on narrow screens |
| 1628 | `c444043371b280a0ebd8606323ca277baadd197e` | 2026-10-21 | hw_liuyu | fix(server): 修复 Worker 初始化与配置热更新的跨事件循环锁冲突 |
| 1629 | `dabf02fca005dfba634ff60fe29a1803d830d543` | 2026-10-21 | min_gitcode | fix(team): broadcast explicit pause terminal frame and stop synthetic true |
| 1630 | `e9b41ee35f26e6cadf6d4ee29ed3d9b59a3ed7e6` | 2026-10-21 | alan_cheng | fix(marketplace): drop orphaned built_in copies before Hub install |
| 1631 | `051e0810276ced8f11884d8ba3361ae85a4279e0` | 2026-10-22 | cyr20040123 | fix(web): align the diagnosis last-turn notice with the backend delivery node |
| 1632 | `0a8de56a35e63911bc51bb88270f933e97fefc7a` | 2026-10-22 | XinchenZzz | fix(team): honor configured model group timeout (#5439) |
| 1633 | `1bd3583934b76dd4b338ca959a6bff6b4cca3a2f` | 2026-10-22 | lamsaan_yau | fix(logging): Avoid recursion overflow when sanitizing large logs |
| 1634 | `1e5010802e0cdbd02fd1c2f74f143279900bcea6` | 2026-10-22 | hwxiajing | fix(design): clear stale node error when a node succeeds or restarts |
| 1635 | `2034b76357934692f9bc98f629948350a5481620` | 2026-10-22 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8504: fix(designer): keep the file extension when downloading a node's material |
| 1636 | `269af3e9dbc531ac5343f0accac3ce953600cce7` | 2026-10-22 | hw_liuyu | fix(worker): 防止会话扫描与 Team 文档读取阻塞业务循环 |
| 1637 | `49aefe9677302a91a2b288b0e4bc071ddea594bd` | 2026-10-22 | XinchenZzz | fix(windows): 升级保全 npm CLI、托管 SDK 及运行配置 |
| 1638 | `4d7055d2adf66afe05133f18adbbc99b63108a56` | 2026-10-22 | liangfuqi | fix(cron): cherry-pick:修复一体机创建定时任务时报 unexpected keyword argument 'mcp'。 |
| 1639 | `4f4fe39d549c7080ae9bd84dd6ad9a7dafb4dc94` | 2026-10-22 | zhengkailan | fix(agentManagement): enhance deriveAgentGroupId to ensure unique IDs for group names |
| 1640 | `52ef4929ccb6a761fdcb1d14a66f93068f9c0aff` | 2026-10-22 | Rossoneri_1007 | fix：我的技能页面，技能卡片名称文本较长，鼠标悬浮在卡片上，名称不会完整展示，文字被截断省略，无法看到全称 |
| 1641 | `539b2c6e513fac554311c0ab329bcb73351866ed` | 2026-10-22 | gcw_3GcDk5mt | fix(agentos): settle inflight ws future on leader cancellation |
| 1642 | `5a92bedd81ae6f690789c3d79e19e99701bf22c3` | 2026-10-22 | ding_chi | fix(marketplace): unlock publishing after an unverified upload for develop |
| 1643 | `5ab6459ba3a037f3cbeaf6a614773586e6bce34d` | 2026-10-22 | ding_chi | fix(marketplace): show the Hub rejection reason for develop |
| 1644 | `5ab7fe33a9bd7807815ec1a653b0021aaff556df` | 2026-10-22 | zvby | fix(swarm): restore organization history and expert names after restart |
| 1645 | `5f9b57cda030a6e7d02b78dea9a6663d42688f92` | 2026-10-22 | yuhaiquan | fix:1.三方Agent切换状态跳闪未安装状态；2.输入会话框中只有skill也可发送 |
| 1646 | `60875ee101fff1d74087c947f6e56eabe318424c` | 2026-10-22 | lzzzzzzasd | fix(内置浏览器) 缺少下载文件管理模块，压缩包下载进度不可见，未下载完成易被用户误判为已完成导致解压失败 |
| 1647 | `6214c9ebd6dd07be48e021b919301cc9ef6a0cbd` | 2026-10-22 | accident2ly | fix(design): design 模式下隐藏会话归档菜单项 |
| 1648 | `67560965e59e6af999859ec2769ad76b7f1db300` | 2026-10-22 | grandocean | fix(cli): report damaged instances.yaml without a traceback |
| 1649 | `68dd7fee04ba988b30fc223e847d308aa8ed40e3` | 2026-10-22 | zhengkailan | fix(agent-group): restore archived and child team names |
| 1650 | `7005a619805515379194dbc247b6cf872e11ca50` | 2026-10-22 | xiaoyao42 | fix(web): prevent SVG ID collisions |
| 1651 | `72c2c79da4fb32a5be272eae3651286c5dfa1aa9` | 2026-10-22 | accident2ly | [mirror] openJiuwen-ai/jiuwenswarm#9232: fix(design): 对齐视频API请求的API超时配置 |
| 1652 | `9bf2061be1d05a0175f1c6fa49d7cb5a8cf1b1e9` | 2026-10-22 | william9200000 | fix(skills): 修复指定skill creator后产物生成位置的问题 |
| 1653 | `b2e52a28d7fcbceb5d071ae86475cb8a49a90586` | 2026-10-22 | kdgmpw | fix(cron): 定时任务列表先按用户过滤再查准入闸门 |
| 1654 | `b62645cf76c4736f8b7e17efb8d57a2ab1cd9239` | 2026-10-22 | XinchenZzz | fix(config): sync missing config fields on startup (develop) |
| 1655 | `c89ac1f146a303935ab03bad9852396bb59b5f35` | 2026-10-22 | gcw_3GcDk5mt | fix(agentos): stabilize third-party Web proxy readiness, latency, and SSE timeouts |
| 1656 | `cd0d2cbf1ae4a2dcb9b79f313ea080f92ecb8c74` | 2026-10-22 | weichenhao | fix(agent-group): reject absolute skill paths during package import |
| 1657 | `d4b58076962791093719b4f2f21879dece65a116` | 2026-10-22 | kdgmpw | fix(skills): 修复 uninstall_skill 跨实例导入技能误报未安装 |
| 1658 | `d9260c26155b2f4210c627b0fcbab152549f1547` | 2026-10-22 | accident2ly | fix(designer): unlock canvas after a single-node rerun completes |
| 1659 | `da6df322aeafc4f07c2f1b1ec302e39df7ee830a` | 2026-10-22 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8868: fix(designer): honor the model selected on the design landing (#5277, #8411) |
| 1660 | `e392f54a7613319cd8926a6ed448a90eca2c5049` | 2026-10-22 | lzzzzzzasd | fix 【内置浏览器】地址栏输入不含空格或结尾带空格的文本无法触发 Bing 检索 |
| 1661 | `ec209798cc12f3a4da790648ebe0a27750c302e6` | 2026-10-22 | yechenzhen | fix(worker): activate managed external CLI SDK paths at worker startup |
| 1662 | `fabef65ef11df82516a6343218c1022effa92bd9` | 2026-10-22 | XinchenZzz | fix(models): 修复旧配置异常导致模型设置不可用 (#5102) |
| 1663 | `fe62fa8bf43e26991d236a82d134c293bc9f74d7` | 2026-10-22 | GeorgeGcs | fix(web): preserve queries containing upload marker |
| 1664 | `04c57f1f28431b2caf715830214b727a1cb1c7f0` | 2026-10-23 | lzzzzzzasd | fix(内置浏览器) 缺少下载文件管理模块，压缩包下载进度不可见，未下载完成易被用户误判为已完成导致解压失败 |
| 1665 | `0bfafb0d1705e7afbb8f2e5c1ccb46a370a8a275` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8491: fix(designer): reject agent graph edits during active runs |
| 1666 | `0dea34a3f45efa22cae6781ed3b5243af04a4f33` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9425: fix(permissions): adapt to agent-core pause semantics (pause no longer fires ... |
| 1667 | `0f1e8d66f853a97671b54500fa5172130deacd98` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9103: fix(designer): show linked video and audio materials as icons, not broken images |
| 1668 | `0f87a4ebbc2d1e3425770120994b9972db592f45` | 2026-10-23 | XinchenZzz | fix(models): discover Ark generation models via official regional APIs (develop) |
| 1669 | `13a4127d22387aacb96e258ca5ce43b1377066fb` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9293: fix(ide): show image upload limit feedback in chat webview |
| 1670 | `16c817ecf22974d477de78e9d3e631febb96ea5b` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9004: fix(design): store canvas uploads in the local web server |
| 1671 | `1d272a7a52610930fdf0a6eb16a27b67fc904d15` | 2026-10-23 | For_YL | bugfix(mcp): add connector with host-managed OAuth |
| 1672 | `20fbb15a00c62e34c1b19dba4f0be6f40df2be45` | 2026-10-23 | lin-xiaoyu | fix(symphony): enable thinking fallback for graph builds |
| 1673 | `24a321d97828d900e1c0a7003cdc994a5b419249` | 2026-10-23 | yuhaiquan | fix:三方Agent新增“正在安装”状态 |
| 1674 | `2f0c42ca29e915740d8bf4568aaf331b208b7fe4` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9292: fix(ide): track active editor for VS Code context injection |
| 1675 | `3b98236ccd847aa54c0272e53b0d8e68b515dc3f` | 2026-10-23 | xiaoyao42 | fix(web): preserve leading rows on initial timeline render |
| 1676 | `41d7c53df13f3522031cb6341e362def064b51ab` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9305: fix(ide): restore persisted session title on switch |
| 1677 | `5c7806f2951e650b5035dfd58c05c9641cf63569` | 2026-10-23 | hw_liuyu | fix(worker): 适配 Team 完成后恢复快照残留忙碌状态 |
| 1678 | `711d9f97b8dd6232ae3f422f0ea916182b53ee9a` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9295: fix(ide): show tooltips for all titled controls in JCEF |
| 1679 | `7695895016a7f6527667efee2740911d445717fe` | 2026-10-23 | weichenhao | fix(expert-team): validate external agent switches before selection |
| 1680 | `879de63dedf0d6e4ebecd4162c7f5612876fa890` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9300: fix(ide): finish plan approval turn on reject |
| 1681 | `8f1c8b7a1e4f98dd9c4eed852fe3c43202ba66b6` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9294: fix(ide): cancel session rename on Escape |
| 1682 | `9b4258d6b2a9d47aea893b76b081e7466d19af67` | 2026-10-23 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#9251: fix(VideoGen Setting): persist selected model provider in edit pane - Develop |
| 1683 | `ab80e45e3fd4b2b25c4de735328ef31a5032157d` | 2026-10-23 | Rossoneri_1007 | fix：系统中已配置两个同名模型（模型名相同、接口端点或密钥不同），添加模型组-候选下拉中两个同名模型不可明确区分（显示厂商/端点等差异信息） |
| 1684 | `b3f9c955c32e38c0c1f6f3c63152cb787cb3d31a` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9309: fix(ide): scope skill picker styles and make skills list adaptive |
| 1685 | `d32f11029465676e50d2aa066064c6dfa3729eb0` | 2026-10-23 | XinchenZzz | fix(build): 修复 Electron 安装包遗漏浏览器下载资源（develop） |
| 1686 | `d44f6dfb53ef8977ac9ec89a575b022047827c11` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8493: fix(designer): clamp low video resolutions to the minimum tier |
| 1687 | `d4bffb08bd4d9884b544578019bcc8920255e493` | 2026-10-23 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#9368: fix(settings): unify Jev configuration and scope browser feedback |
| 1688 | `dd6ef631b550113ff052be5a55b771147940c0b9` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9296: fix(ide): render team final content in chat transcript |
| 1689 | `ddd12f73180639fd10df271f79bbd9a3750f6d1f` | 2026-10-23 | hh0205 | fix(team): emit pause terminal frame from runtime lifecycle pause |
| 1690 | `df179efce016777264918cf09544286b6b5e9c34` | 2026-10-23 | XinchenZzz | fix(heartbeat): 删除会话前准备心跳清理，避免资源保留导致失败 |
| 1691 | `e1eb0e42aec1d81b7323463824a06c269119ed73` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9298: fix(ide): refresh model list when picker opens |
| 1692 | `e9727eb0ad3cd0dbcc4e642ca4c343f66572fc97` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9301: fix(ide): classify stopped turns correctly on stop click |
| 1693 | `fb2201d78f0315abc4e5b99eec6b374019f0dac0` | 2026-10-23 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9077: fix(sidebar): clear the unread dot of the design session being viewed |
| 1694 | `fcf7617cdb7b3a412e6950707558072958c71fb5` | 2026-10-23 | Rossoneri_1007 | fix：系统中已配置两个同名模型（模型名相同、接口端点或密钥不同），添加模型组-候选下拉中两个同名模型不可明确区分（显示厂商/端点等差异信息） |
| 1695 | `177807d8ad8995483ed37398e258333809e264a2` | 2026-10-24 | yuhaiquan | fix：三方Agent，Claude/Codex路径下拉框多出一个.CMD文件路径 |
| 1696 | `2456e02d2863abc4d2b8ea43c49c2cb4fe845d79` | 2026-10-24 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9297: fix(ide): forward team leader messages to chat transcript |
| 1697 | `3095c911475f06e95e128e3fde0ed5f99fe3cd2d` | 2026-10-24 | ZezeChang | fix(swarm): 修复重启进程后owner team的虚假忙碌现象 |
| 1698 | `370f4e48f591f87f3b47d5fd34c349ce21c665dd` | 2026-10-24 | weichenhao | fix(agent-group-creator): clarify member skill layout and validate generated packages |
| 1699 | `45ba6ad603190924733a8cd71248d768e105602b` | 2026-10-24 | yuhaiquan | fix：三方Agent切换地址出现未安装状态 |
| 1700 | `483c89424cfd29f842ade56b364be59bdcdda69e` | 2026-10-24 | yuhaiquan | fix：无描述专家团悬浮提示文字修改 |
| 1701 | `6431d6b2d09f4a32eed33aaea969785d5a2c3966` | 2026-10-24 | huangyuzhe | fix(memory): 适配 JiuwenMemory 2.0 可信身份注入；依赖加上JiuwenMemory |
| 1702 | `873722c299011e2ecca5dc11c85513c9eb60b5f4` | 2026-10-24 | min_gitcode | fix(runtime): keep attach-only subscriptions from refreshing session timestamps |
| 1703 | `9133102b2af1afa7b2fd0a3058daf87137ff8cef` | 2026-10-24 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9291: fix(ide): support JetBrains platform 2026.1 (build 262) |
| 1704 | `994c2970d0c1436bf64d4b637690df0fdf772205` | 2026-10-24 | openJiuwen-bot | fix(channels): decode picker subprocess output with an explicit encoding |
| 1705 | `99c599d21fc8e2d0a8b3cc43670814e0997a3828` | 2026-10-24 | openJiuwen-bot | fix(encoding): pin the codec on the remaining text-mode probes in agents and common |
| 1706 | `a39f0637681b6b73033ab6e76d2f147d0fe381c7` | 2026-10-24 | kdgmpw | fix(session): finalize archive and deletion in the background |
| 1707 | `b2c5422b0b02cbed22cc5958ed1ad8eeee923c78` | 2026-10-24 | zvby | fix(swarm): isolate expert team conversation state |
| 1708 | `c384cbcaaf95cff1246a213f0ab7ac179ed40e50` | 2026-10-24 | yuhaiquan | fix：专家文件上传创建导入成功但是安装失败的提示修改 |
| 1709 | `cb62cfa965976b018a86b4d92a91bdccd6e7ea85` | 2026-10-24 | hw_liuyu | fix(cron): restore cross-channel session runtime status |
| 1710 | `d322ae8d7fe6ea1db8ef1aa8567983527f00b1fc` | 2026-10-24 | ZezeChang | fix(swarm): preserve organization chats and expand info panel |
| 1711 | `e4a64a1ce45afaac650354ab139c174a8fc71bb0` | 2026-10-24 | openJiuwen-bot | fix(runtime): decode native tool and git probes with an explicit encoding |
| 1712 | `fd8f735c9fcb9b08657632fece07858be6ca4dc1` | 2026-10-24 | huanghongyun | fix(web): 修复敏感澄清详情及未登录发布名称（develop） |
| 1713 | `0a34c35995942603509fee2d0e89c82b90426060` | 2026-10-26 | zvby | fix(swarm): scope member mentions, restore stop buttons and disable uninstalled expert teams |
| 1714 | `0fece89860cb1d169c490dc2d411bea702d112a4` | 2026-10-26 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9168: fix(design): keep a single canvas preview audible (#5411, #8679) |
| 1715 | `3cc8622199604a2694e930f2d21d11ebf45260e4` | 2026-10-26 | ZezeChang | fix(swarm):修复组织内专家团忙碌时无法发送消息的问题 |
| 1716 | `3dedbf50aee8d8c2cb36740eec6ac763aa847b28` | 2026-10-26 | douran | fix(web): 输入栏新增永续会话只读标签 |
| 1717 | `3e76c604b4e92af6101a071720a9499a5a114c3b` | 2026-10-26 | maxmadmax | fix(permissions): 智能审批触发人工拒绝时，只针对单次调用有效 |
| 1718 | `423edbb8ac843d89e97e5dbeea574dc8abbfb64d` | 2026-10-26 | chunbs_cbs | fix: 0.2.8分支问题修复回合 |
| 1719 | `44db00916d9b57159ddb821e7a3c5c1ad4fb93a1` | 2026-10-26 | wncbdj | fix(todo): show the parameter shape for each code-mode todo_modify action |
| 1720 | `496e4e59f90f692d7cf3a915498af4fc54ddad33` | 2026-10-26 | yukuanj | fix(a4p): 修复A4P预授权会话刷新后实时流式输出 |
| 1721 | `4b00671f6775232f3305630e3ca410a96f6fe891` | 2026-10-26 | kdgmpw | fix(cron): 修复未选项目时定时任务归属与侧栏不显示（#5713） |
| 1722 | `585396be79e7232b60a8f1e47b1839a7d6622648` | 2026-10-26 | ray_le | fix(mcp): connect Hub MCPs on node-only hosts and surface connect failures |
| 1723 | `67b8c033d7e1f62eb16bbaa22fa5d869e4861fd0` | 2026-10-26 | kdgmpw | fix(session): 修复普通会话串用项目目录的问题（#5753） |
| 1724 | `6885838d7f9e614ebad18b0cd083905264841c84` | 2026-10-26 | ZHANgPP_ | fix(web): let artifact HTML previews run their own scripts |
| 1725 | `6e5e933f75dc60763e7918234c41aee77e147bc6` | 2026-10-26 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9459: fix(designer): keep un-wired uploads on the still and animate the uploaded frame |
| 1726 | `7327ba20ddd34595422d8bce6a1322416b9fb3cd` | 2026-10-26 | wangcan103 | fix(server): resume stale ask_user answers after service restart |
| 1727 | `73773b9d4f26e43ce4b5bbb4745ef95a695152ba` | 2026-10-26 | zhangyaomaggie | fix(swarm): Fix expert package skill path validation and extend Codex idle timeout. |
| 1728 | `8ec4df51cfa7506b937a225a42bfb0178f5bb989` | 2026-10-26 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9083: fix(designer): keep the canvas toolbar below the floating session sidebar |
| 1729 | `a4075ba60eb46627fbcd038f78239d5adaeaf6b4` | 2026-10-26 | kdgmpw | fix(cron): 修复定时任务会话模型选择误拦截（#5655） |
| 1730 | `a82615911a296fb54cf9e2645f6fb548a4b953e4` | 2026-10-26 | ray_le | fix(mcp): honor timeout_s and make hub uninstall idempotent |
| 1731 | `aa63697e968c442f3b7496129a10a5759d5b48de` | 2026-10-26 | XinchenZzz | fix(version): show package build dates |
| 1732 | `b33fde25715ed9218c226fc2548df5d8464ca5f6` | 2026-10-26 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9202: fix(designer): show uploaded predecessor thumbnails in the material strip (#5... |
| 1733 | `c5c59da8566e7e8fdc1c04d27e1c744eb392282c` | 2026-10-26 | william9200000 | fix(trajectory): 轨迹诊断页面前端样式修改与问题修复 |
| 1734 | `f92b976f3a708c9dd0dc8da6ccdfc897eb1eafb1` | 2026-10-26 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9468: fix(designer): say so when a node download fails |
| 1735 | `0155312b837d845fc2b5113a7d4a5feaf489d386` | 2026-10-27 | wangcan103 | fix(web): show full swarmflow name on hover |
| 1736 | `06c89c7a79d55f15a960c98d0fc77a6f0af4ce6b` | 2026-10-27 | accident2ly | [mirror] openJiuwen-ai/jiuwenswarm#9497: fix(design): 修复 "no rerun candidate" 报错，移除遗留的 candidate/choose_output 流程 |
| 1737 | `1367fb25a9353a5d6ed59684c40eadd224c11bf7` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9792: fix(design): title reference-led canvases by the story, not the prompt |
| 1738 | `16b1921626caa99441ca5cc8e8748013a309d124` | 2026-10-27 | accident2ly | [mirror] openJiuwen-ai/jiuwenswarm#9778: fix(design): 在设计模式下隐藏模型组配置 |
| 1739 | `1dbf76990d7efe584116a60804e064e630aa270f` | 2026-10-27 | birfied | fix(cron): reject user-path spoofing of builtin proactive-tick-auto job (#5853) |
| 1740 | `211b97a78a49b4fae8d12eddfcc4c0700296e649` | 2026-10-27 | paradiseyph | fix(sdd): 审查阶段「跳过」导致流程停滞，只产出需求分析文档 |
| 1741 | `2249f8eb0954e6f6aae88a8dd1e40ebbcf9b4fc3` | 2026-10-27 | gp1000 | fix(session): 归档收尾的「锁被占用」路径也消耗重试预算 |
| 1742 | `2b9cee84f6c2b63443a2964b9a2c13d996681f40` | 2026-10-27 | ldstyle8 | fix(history): preserve output across human intervention |
| 1743 | `2c3b3bf6ff9ea032238cd1197bebefa707dccb6e` | 2026-10-27 | accident2ly | fix(design): 设计模式的中文用户得到的提示词中英混杂 |
| 1744 | `34ce496621b42bebe4d51ee7252de4443aefc5ec` | 2026-10-27 | paradiseyph | fix(sdd): DesignRail R3 '需要复审' 语义冲突导致流程停滞 |
| 1745 | `387735f15b11a7abf2050820d0d892a5cd46e02f` | 2026-10-27 | maxmadmax | fix(permission): 修复Gateway forwarding queue is full问题 |
| 1746 | `38a53f4f6aac97b7efaa630557940c86041dc049` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9308: fix(ide): toggle skills by name |
| 1747 | `3e03eefbbcadf7104bd2edeae53e93e7e606b201` | 2026-10-27 | GeorgeGcs | fix(web): explain why queued attachments cannot be sent immediately |
| 1748 | `3f491dfc0706443d88a1fa82618b12a6e6360113` | 2026-10-27 | gp1000 | fix(deps): croniter 版本下限提高到 3.0.0 |
| 1749 | `418fea8cb06c0932701b9e96028c1192d6c06a1b` | 2026-10-27 | xiaotian077 | fix(xiaoyi): disable send_file capability unconditionally |
| 1750 | `44342221a28661fc950a1676f74b2ee4b283c577` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9391: fix(designer): keep the canvas locked while the design leader plans |
| 1751 | `443e6618d5bc55b5d80f7306042cc0c40cb644ea` | 2026-10-27 | maxmadmax | fix(permissions): 智能审批优化根据现有规则放行情况下审批卡片显示效果 |
| 1752 | `45c9f63ae8d5665410c564973aeafe57ae2fe73d` | 2026-10-27 | alan_cheng | fix(trajectory): keep restated request content shared instead of per span |
| 1753 | `4d3382576fb43fc623ee1afc9ece499466be8c05` | 2026-10-27 | ZezeChang | fix(swarm): 优化集群模式组织内汇总模式选择 |
| 1754 | `4e98ec676334d5e5254de72934a1467b8fbc5734` | 2026-10-27 | zhangyaomaggie | fix(swarm): fall back to manifest description when display_description is missing |
| 1755 | `5031ce830ee54f63a6dd1d4b75b72a6d19b664f4` | 2026-10-27 | xiaoyao42 | fix(web): continue history restore past filtered pages |
| 1756 | `56932e713b6bbeb5b9b8fc1ab07c24296b390b2e` | 2026-10-27 | huanghongyun | fix(web): assets are searched only by visible name |
| 1757 | `58eb51df415fa615aac2dc544049e2f8d49d9e0c` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9303: fix(ide): make file references in replies clickable |
| 1758 | `5baf5c3b1a5109689b8acd0cb5556519f9ddd657` | 2026-10-27 | kyrie_irving11 | fix(web): 不把 ttse_consult 纯文本里的 success=False 当成工具失败 |
| 1759 | `5f9b56446d913656bf547e00d22c1d51d9f7e738` | 2026-10-27 | xiaotian077 | fix(xiaoyi): disable scheduled tasks |
| 1760 | `69392dd41d5a03504bfaca7f08787dfe7d30ab52` | 2026-10-27 | xiaoyao42 | fix(web): defer elapsed display until timestamp confirmation |
| 1761 | `761ec07cc27f6014cf0ab6c66466d0331b0f1570` | 2026-10-27 | huanghongyun | fix(web): Compatibility of the expert team uninstallation return list and expert display name |
| 1762 | `77926354e39d9b5906d83d5f85b4a29e03b671e7` | 2026-10-27 | gp1000 | fix(cancel): 定向专家取消在 owner 未知时不再降级为会话级取消 |
| 1763 | `7cb217f0580ebb3b8b639cdc16bd98e2d2412915` | 2026-10-27 | xiaoyao42 | fix(web): keep expert descriptions independent |
| 1764 | `7cd73f44a50945476c5032e000f0937872b514c6` | 2026-10-27 | z2tng | fix(web): complete skill arguments for evolution commands |
| 1765 | `8261da4680ad61830b0bafc32016b49792b48908` | 2026-10-27 | alan_cheng | fix(trajectory): address review findings on shared sequence content |
| 1766 | `83a853a01a7aee5e24e59a2ceeca4fbb6e74b4de` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9112: fix(design): keep a design round processing while the leader's tools run (#54... |
| 1767 | `8485015dbc5af325a307242399074c4f81b20eca` | 2026-10-27 | ldstyle8 | fix(code): align multimodal tools with work mode |
| 1768 | `8d80a25c12dd0ca9a2c9acd4509421a38e324c99` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9447: fix(designer): apply a chat edit of the brief prompt when the brief reruns |
| 1769 | `9b48f0a9b79ebf0c81d2a1275e58f7faa69538cc` | 2026-10-27 | gp1000 | fix(common): safe_session_dirname 拒绝点目录，避免上传越出 sessions 根 |
| 1770 | `a93a5efe9bf78a0d28ec75b152dd0a617c6bfbfd` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9304: fix(ide): make the resend button more visible |
| 1771 | `b0c6a38f54eb3143f7dcf430c11cfe8b635066c8` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9301: fix(ide): classify stopped turns correctly on stop click |
| 1772 | `b49de35ee3ea25fe422bb4559212962b0925ea8d` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9005: fix(designer): make the canvas chat model switchable and bound to its session... |
| 1773 | `b4cedaa5035f3df8fa0dd00e2eb86fd99bcffca5` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9780: fix(design): stop the leader from hand-building graphs after autorun fails |
| 1774 | `b901e8cb611110a56ad27b76a64cfab5ecdd2cc5` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9306: fix(ide): refresh logs path and export after each turn |
| 1775 | `baac74172a044f23503d41fd38b87160650e01cc` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9069: fix(designer): reject unsupported canvas wires with a shared node-type matrix... |
| 1776 | `bcc309cfbddd34334da557bdbff67a2c2e2fa0c1` | 2026-10-27 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#9682: fix(gen_tools): fix naming format in material category - develop |
| 1777 | `bd63db5a96be3780171abf8dbd67c5ea63d59c12` | 2026-10-27 | alan_cheng | fix(web): derive team task progress from real task counts |
| 1778 | `bd901d72ca1bab5caf2dc2df9933f28f9c161232` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9257: fix(designer): keep clips on their storyboard rows after a shot is inserted |
| 1779 | `d3942fd81f38c0ba431ecca19b06d15b55388b3e` | 2026-10-27 | lzzzzzzasd | fix 1.专家团队详情页头像 2.复制抽屉的内容与关闭抽屉的冲突 |
| 1780 | `d7fbe78ed57c11b3efe5375433970247c6605524` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9672: fix(designer): list canvas uploads as uploaded in the assets panel |
| 1781 | `db0dd3dbb4edf2286147001d56231b88eeded35f` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9413: fix(designer): keep an edited prompt when a save and a rerun overlap |
| 1782 | `df107574d6d5a6efc7a55c84def357c97666fa9f` | 2026-10-27 | Rossoneri_1007 | fix：模型ID被设置成不能与其他配置的模型展示名称相同 |
| 1783 | `e2bd023178634b03033b57e7099c2979d1efd0b2` | 2026-10-27 | cyr20040123 | fix(attribution): 调整归因输出展示字段新增轨迹摘要，统一归因语言，解决归因无任务描述报错问题 |
| 1784 | `e38ffee11cd022191475e6a821232ed92f0f8a42` | 2026-10-27 | yuhaiquan | fix：三方Agent地址错误下方文字提示修改 |
| 1785 | `ef7473ba73de1e2bcc32c3c9f4f12a599373c58e` | 2026-10-27 | william9200000 | fix(trajectory): 轨迹分析页面导入轨迹问题修复 |
| 1786 | `efebdd70493150aa6645016e5f710d3ffd5e328e` | 2026-10-27 | wangcan103 | fix(swarm): suppress budget-exhausted chat.error when a run is present |
| 1787 | `ffb7058f6ed83496dfb71dfb64442877c82961f3` | 2026-10-27 | weichenhao | fix(team): remove user_todos from team tool registration |
| 1788 | `053c82b07056cf503e2b0229e1cbbb45a9c2bf7f` | 2026-10-28 | kdgmpw | fix(server): llm_output 不再过滤空白增量内容 |
| 1789 | `0711cbcb618fe13afaf7b5b524ee6ee6788920f5` | 2026-10-28 | william9200000 | fix(skills): 修复session关闭后技能市场打不开的问题 |
| 1790 | `082ba47b572f3c35afed9c09b2e800ba79b0b199` | 2026-10-28 | HenryTian98 | [mirror] openJiuwen-ai/jiuwenswarm#9655: fix(designer): Design fix stop design run |
| 1791 | `26b23119c76c4076501cb066a4eb092f20e91306` | 2026-10-28 | alan_cheng | fix(team): keep departed members as a directory beside the roster |
| 1792 | `361968af90b4f0fc3656afaef58bd3f325698319` | 2026-10-28 | alan_cheng | fix(team): keep shut-down members out of roster snapshots |
| 1793 | `373cf4e465f83e0a93be04857905a9e209414211` | 2026-10-28 | gl_zhou | fix(trajectory): refine diagnosis findings and summary-only fallback |
| 1794 | `396819cad042eb5dfe3c9bd0f19a5153fcf09314` | 2026-10-28 | GeorgeGcs | fix(session): strip debug directive from automatic titles |
| 1795 | `441802ec1c6d4fc1d5d50adaadd14053ae795e6d` | 2026-10-28 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#9833: fix(video-gen): proactively deliver a video job's result once it settles - de... |
| 1796 | `479951077ca33a89917d887843d3fd1487504184` | 2026-10-28 | openJiuwen-bot | fix(tui): guard memory commands in team mode |
| 1797 | `4f8eaaa7859918c6c268086d671b12470c4756ee` | 2026-10-28 | accident2ly | fix(design): 替换上传素材后，后续节点重新生成，未使用替换后的素材 |
| 1798 | `622edf8924d6bf2fb3f419d7af81e4d8f524e2ab` | 2026-10-28 | alan_cheng | fix(web): prefer tool call description in team process summary |
| 1799 | `6e1a60193f48fde409b74d01a5e80d822f61d759` | 2026-10-28 | william9200000 | fix(trajectory): clarify diagnosis agent selection controls |
| 1800 | `70496f35c99a81ea94ddb08bcb7c6b7e558d7a3b` | 2026-10-28 | amelafejza | [mirror] openJiuwen-ai/jiuwenswarm#7213: fix(web): copy pretty-printed JSON from RSI artifact preview |
| 1801 | `71dff8527f5907083980f7dfaa38cd82dff17ce8` | 2026-10-28 | kdgmpw | fix(runtime): 调度前纠正历史会话类型并记录日志（#5810） |
| 1802 | `7ed53d804c41d55e33333b9344080e7e370ceeb9` | 2026-10-28 | l_x_d | fix(context): persist cumulative token usage |
| 1803 | `833536d7b628bcbbf88bd85b6a26e443195fcd6d` | 2026-10-28 | openJiuwen-bot | fix(permissions): 修复会话记住规则因 ID 冲突未落盘 |
| 1804 | `9658db5657760a3a6b10bbd193fb266fa50e67cf` | 2026-10-28 | alan_cheng | fix(observability): archive incompatible databases before rebuilding |
| 1805 | `9d3c484504d6c0808166e75668e8ba86557f19ea` | 2026-10-28 | xiaotian077 | fix(xiaoyi): preserve team task across join |
| 1806 | `9fbfaea33f3d7be8e806a1e57a3d5e64ce31d46f` | 2026-10-28 | xiaoyao42 | fix(desktop): upgrade Electron to 43.7.6；调整jev模型交互 |
| 1807 | `a95a849b2d76935e6f665f8a7efefcda38b5e0eb` | 2026-10-28 | kingdomhit | fix(test): 放宽 runtime worker stub 绑定 IPC 等待窗口，消除 CI 偶发超时 |
| 1808 | `c5f018ea2af685c5a6aa04b209a99ed94bbfd16b` | 2026-10-28 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8232: fix(agent): persist the loop Session when a round ends |
| 1809 | `ce75461615478934a676097a907075852736dec7` | 2026-10-28 | Rossoneri_1007 | fix：切换会话再回到新建会话时，页面显示bug |
| 1810 | `de45ca10097e2b3b27611a71fa5a0350528f5f19` | 2026-10-28 | zvby | fix(swarm): Fix logic issues with models for different expert groups, the member list after unloading expert groups, frontend display of the team execution process, and the main group stop button |
| 1811 | `e19bbab352328997364010c39454b6d545ebd2da` | 2026-10-28 | zvby | fix(swarm): immediately clear owner busy state on stop |
| 1812 | `e9608f5a90890769c38f03b9292beb4b6d7a7f81` | 2026-10-28 | XinchenZzz | fix(worker): validate build identity and isolate deployment lifecycle |
| 1813 | `eb1ede853bac2739af5f0289c55db647ead8a0b3` | 2026-10-28 | XinchenZzz | fix(installer): show post-install CLI status (develop) |
| 1814 | `f408e552a8d133a14c357f17cd93003b850338fe` | 2026-10-28 | yechenzhen | fix(gateway): deliver in-flight web events after reconnect |
| 1815 | `f5cfc2b5dd1e7c6312d89f336a1dc214f835edb1` | 2026-10-28 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#9949: fix(gen-tool): stop unsent composer attachments leaking into other session |
| 1816 | `0211b6397ec1b34bd7cf6580380f199277c40c27` | 2026-10-29 | yuhaiquan | fix:三方Agent地址后端持久化优化 |
| 1817 | `0edac80e5cfa5ef49a8d5a696835ac10b96dc36b` | 2026-10-29 | paradiseyph | fix(sdd): 分段写入保护 + 模板脚本执行修正 |
| 1818 | `32d75d7d3a0b466412ee64d77c29460297515119` | 2026-10-29 | l_x_d | fix(rsi): replace default harness with session selection |
| 1819 | `3e97637a2ebc56d9bbcaff483d9a5193e3c911d4` | 2026-10-29 | min_gitcode | fix(team): preserve observers and finalize pause state |
| 1820 | `7b664cd1b24164b79620c87f4ffec47d454ca8da` | 2026-10-29 | yuhaiquan | fix(三方Agent)：1.初次检测清空无效地址；2.点击一键安装切换为内置cli地址；3.重启后默认状态为未安装 |
| 1821 | `8407baaeee9cf720901423517a6d9298af57db7d` | 2026-10-29 | amelafejza | [mirror] openJiuwen-ai/jiuwenswarm#7356: fix(web): localize chat Save Skill strings |
| 1822 | `85a507340b3145f4dded4ac9920845e1a8a64298` | 2026-10-29 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9989: fix(ide): render file links in live replies |
| 1823 | `88fbdd2a1e771045118b1eabd493b071e5b64b69` | 2026-10-29 | paradiseyph | fix(sdd): DesignRail 路径 fallback 与方法论注入提示修正 |
| 1824 | `9f53e4841314842980d89eca24666e1a10c4bc77` | 2026-10-29 | HenryTian98 | [mirror] openJiuwen-ai/jiuwenswarm#9797: fix(session): ride out metadata replacement when reading the model selection |
| 1825 | `b1301d04a94765290f2afd657fe4864734d40997` | 2026-10-29 | yuhaiquan | fix：上传专家文件失败导入却成功 |
| 1826 | `b89757071a26c926465477004279a6f9243f58cd` | 2026-10-29 | JustinBieber1223 | fix(gateway): stop withholding team round-terminal false frames |
| 1827 | `bbbfd2618799a372555913eca4f605da4d77df3e` | 2026-10-29 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9302: fix(ide): parse tool args and show created file content in diff viewer |
| 1828 | `e0f268231f70dfd8b1a81a9b1e8f366f4a132d35` | 2026-10-29 | kingdomhit | fix 应用更新检查将最新版本及发布时间正常展示出来 |
| 1829 | `e857596ea5e311d1ba57f0f7fa9d1d8e4b18e173` | 2026-10-29 | hh0205 | fix(team): list only model-group models as available models in model-group mode |
| 1830 | `fc88d9a419fefde92509363bbbbb30e6c710375f` | 2026-10-29 | alan_cheng | fix(team): keep departed human seats out of /join validation |
| 1831 | `ffc539d74ba6fe034c60b6eac5ae3ea1da04b7dc` | 2026-10-29 | yuhaiquan | fix：三方Agent成员状态判断逻辑优化 |
| 1832 | `123af15c499fd5a0fdf7bc393ec127dda93ed441` | 2026-10-30 | zhangxiangyu52 | fix(cron): 个人版空 group/bot/user 保持空串，兼容旧数据。 |
| 1833 | `447b262f62e5792acead1cd1c2be9cba69e494ef` | 2026-10-30 | GeorgeGcs | fix(harmonyos): normalize skill list path separators |
| 1834 | `487b77b5a0e460ee9c5a8c7e475a335184e79a72` | 2026-10-30 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#10146: fix(runtime): deliver interaction answers to the paused turn reliably |
| 1835 | `ad357b12505eb8980553991534e71c62719a6da9` | 2026-10-30 | openJiuwen-bot | fix(resources): decode project-maintainer subprocess output with an explicit encoding |
| 1836 | `cbfd3bd29e1e46c500ab212f6afe5af8aa4e6c6a` | 2026-10-30 | u010240195 | fix: 修复设计模式、团队订阅、长图导出及定时任务实时输出 |
| 1837 | `d83cfbb8ed3fefd9ee85f783199bb5eaeeb5359e` | 2026-10-30 | openJiuwen-bot | fix(resources): decode skill-omni-creation subprocess output with an explicit encoding |
| 1838 | `e0375e8ac94f31cd1971bed8347396b7a21b584d` | 2026-10-30 | meizi6 | fix(skills): 个人版内置技能恢复可编辑/卸载，优化企业版技能前端页面 |
| 1839 | `f046910a2a1988ace4be615fc7706ac431d5e543` | 2026-10-30 | lzzzzzzasd | fix 首页小蜜蜂元素遮挡导致无法点选被遮挡的部分文字 |
| 1840 | `f9289f57e61cd21a10ae7f3da947ca526291ce46` | 2026-10-30 | LasVie | [mirror] openJiuwen-ai/jiuwenswarm#10219: fix(desktop): use verified nonzero CDP for embedded OAuth (develop) |
| 1841 | `17a2b44541d1117d55dc231345d4acfa97d1139f` | 2026-10-31 | maxmadmax | fix(permissions): 补齐 #5738 的审批控制权交接与恢复 |
| 1842 | `1a6d8559888b8d07c70e01e44d2dfd9dad151271` | 2026-10-31 | GeorgeGcs | fix(paths): redact stale sandbox paths on Windows |
| 1843 | `491d9908408e549936c5c9fa058c96bf9a88e9cf` | 2026-10-31 | GeorgeGcs | fix(web): warn when a scheduled task model is unavailable |
| 1844 | `5f8debfef28fdeb0dac91deba904336422d55666` | 2026-10-31 | zhangxiangyu52 | cherry-pick/pr47-cron-fixes：修复企业版定时任务bug |
| 1845 | `85697e5ffcc126228d167612d6917f58ad85de18` | 2026-10-31 | kdgmpw | fix(web): 修复会话页冷加载白屏问题 |
| 1846 | `95b1e2f6cf5db4d16adf3f3f3f58d11285a47991` | 2026-10-31 | ruiqiang2 | fix(gateway): 修复企业特性造成的server依赖 |
| 1847 | `a127352b977f87760a5d2f48fcb092b1d3b92344` | 2026-10-31 | accident2ly | [mirror] openJiuwen-ai/jiuwenswarm#9983: fix(design): 对话修改内容后，大纲未修改，有时视频内容也未修改 |
| 1848 | `a790b823eb723fdc2990718c56c9b67941d87ce6` | 2026-10-31 | hw_liuyu | fix(steer): 支持图片补充并保留原任务未交付答案（#4516） |
| 1849 | `beb184d13e1c6e98e513ed9b1bcb9f8bfb977bab` | 2026-10-31 | alan_cheng | fix(web): stop re-showing answered Team ask_user questions |
| 1850 | `df43e502bc31044f98fad590f047cfbf676fc9e5` | 2026-10-31 | yechenzhen | fix(gateway): 修复企业版特性造成的路径类依赖回归——167→57（不动 server 耦合） |
| 1851 | `e45a83812d79339f8a0ecb967a8279b6f896b17b` | 2026-10-31 | william9200000 | fix(trajectory): place late context before reply and unlock lane pan |
| 1852 | `fd5a2d789878ee40c84550ab279a6648b043578f` | 2026-10-31 | maxmadmax | fix(permissions): align safety review prompt with trusted intent |
| 1853 | `08515d6e721edb9933b55ef389b07c6301f70f1f` | 2026-11-02 | l_x_d | fix(rsi): enable continuous two-stage harness promotion |
| 1854 | `33da216f6c7c63fa49d83cddc7ec2010534050a4` | 2026-11-02 | kdgmpw | fix(cron): 修复定时任务默认项目查询与失效状态执行问题 |
| 1855 | `389b22b2da0302916ff6c7381bd77d26087dcd39` | 2026-11-02 | zhaominjun1997 | fix(personal-context): detach PCS from Design mode |
| 1856 | `3f50cf7fd81a23e9ba2fb842b3557eaad0a502c4` | 2026-11-02 | zhaominjun1997 | fix(personal-context): enable PCS search through PersonalContextRail |
| 1857 | `400f56eeed6f33e9d27be0ceb91232ad6a26bdc0` | 2026-11-02 | u010240195 | fix: 用户面-修复路径结尾消息回车无法发送 |
| 1858 | `41c1e95eb6d21463403b45ae3d6af0d17d8da647` | 2026-11-02 | liangfuqi | fix(config):  一体机新增配置更新脚本 |
| 1859 | `4677deaefe23a451092b9712c97d052841950fdf` | 2026-11-02 | waunlee | fix(skills): 企业版OCP部署采用可配置方式支持连接SkillHub API时关闭TLS 校验 |
| 1860 | `4c3bb15555282670fe7dbdcbbed501578cee98f6` | 2026-11-02 | bhyeo | [mirror] openJiuwen-ai/jiuwenswarm#10228: fix(material-window): Prevent removing file in category after second attached... |
| 1861 | `81a96cd07b55a0a6ba2ba1c3688e7ca0b992c4c6` | 2026-11-02 | GeorgeGcs | fix(upload): sanitize Windows filename separators consistently |
| 1862 | `99aef4204d62bc81413b2a3e68ef9aa0dbf7d6c1` | 2026-11-02 | XinchenZzz | fix(installer): 优化 Windows 升级交互与 CLI 环境保留 |
| 1863 | `a75eccb69c310fc08981b54ec000a63ae08c84e6` | 2026-11-02 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#10106: fix(electron): reopen browser pane for new headed tasks |
| 1864 | `bd4a3b5234647dca22c2c021aa2097f71f22ccc5` | 2026-11-02 | u010240195 | fix: 企业版用户面隐藏新建会话分支入口 |
| 1865 | `d29aa0ed4b0455931a9cec15a8a08c19e8b50479` | 2026-11-02 | meizi6 | fix(skills): 企业版技能详情展示当前版本 |
| 1866 | `d9550678741452eda07cc4c9e464a4541963f6f1` | 2026-11-02 | kdgmpw | fix(web): 定时任务重名模型秒败且无报错的前端修复 |
| 1867 | `ed4fc03cda6934b0919176f796231a1999eb871b` | 2026-11-02 | MengHan18 | fix(tools): improve bilingual tool discovery and reminder routing |
| 1868 | `f72694f5df16c1dc760f48a9bdf8fd79268f0187` | 2026-11-02 | CharlesWang2030 | [bugfix] 文本产物预览按 BOM/UTF-8/GBK 兜底解码，修复 GBK 产物乱码 |
| 1869 | `f99f3e8ae0dc5b1a9fa6dc87976952f45e05a61e` | 2026-11-02 | u010240195 | fix: 用户面-修复Design首条消息跳转闪现 |
| 1870 | `fb0be622535d11318d6310c56c44aea0e75288db` | 2026-11-02 | Rossoneri_1007 | fix: 切换会话再回到新建会话时，页面显示bug |
| 1871 | `00482bb291d1bafce333ec18eed45e0cb26070f2` | 2026-11-03 | XinchenZzz | fix(runtime): prevent stale team attachments from triggering empty turns |
| 1872 | `105bad92f76b3f87b4dcf5073af575ff805e65d6` | 2026-11-03 | GeorgeGcs | fix(agentserver): preserve runtime project path case |
| 1873 | `135e8fef45b23e3e7849327cd8f7b5832a7d204e` | 2026-11-03 | alan_cheng | fix(external-cli): probe a fresh model catalog when a CLI is enabled again |
| 1874 | `17625e5dd94550deec395af9865a96db4959a924` | 2026-11-03 | l_x_d | fix(rsi): preview Harness packages and fix worker shutdown |
| 1875 | `2cb9f7bbc7f8da5972d05a440d6c56318b0a22e4` | 2026-11-03 | openJiuwen-bot | fix(config-panel): model probe misses persisted custom_headers, causing false 401 in Settings test |
| 1876 | `2f5f0691fdba1d3219fc7aad97d4f9c81f6cecc9` | 2026-11-03 | hanjiahui | fix:企业版安全护栏隐藏'永久记住'选项 |
| 1877 | `33fd374b8b79bcec7949b34b3007245e0634847e` | 2026-11-03 | gcw_Q7qFL0nH | 修复BUG（仅做后端校验）：[Bug]: 当配置的提前唤醒（wake_offset_seconds）值超过触发间隔时，定时器将无效https://atomgit.com/openJiuwen/jiuwenswarm/issues/5934 |
| 1878 | `37d3b50aeb653b87e7ea6c30e1d6554c0603a4e3` | 2026-11-03 | liangfuqi | fix(feishu):修复飞书审批报错 |
| 1879 | `3926849e408020eed4b8d80c9eee7e01ff67a718` | 2026-11-03 | programmegirl | fix:企业版在进程预热的时候不探测模型是否支持多模态 |
| 1880 | `4512240b4242da3ff329c8b17fddc993f79cc9be` | 2026-11-03 | CharlesWang2030 | Bugfix(cron): 定时任务执行记录增加执行时间显示、字号对齐并支持分页查看更多 |
| 1881 | `4b495c758870119e808491a114d6fa76546af2d7` | 2026-11-03 | u010240195 | fix: 企业版用户面隐藏定时任务中项目选择 |
| 1882 | `5546aad0c84d4595ed06ffefe70ca07d5a185ddb` | 2026-11-03 | yangzequ | fix(auth): support HarmonyOS Huawei login sessions |
| 1883 | `5740ad2a84ca0d75911b1739c42257acd158298b` | 2026-11-03 | openJiuwen-bot | fix(web): derive expert catalog category tabs from loaded data |
| 1884 | `6aea50ef78c2dd2eac975b8e168788593352bc46` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9946: fix(designer): keep node errors inside the card |
| 1885 | `704cb0279fd11aa24bf701a65562b3f5332eaeae` | 2026-11-03 | programmegirl | fix:企业版同会话并发SSE不再误取消父流 |
| 1886 | `7ec5d5a7fe6fe6315a2df65afd73e445367eecd9` | 2026-11-03 | zhangxiangyu52 | fix(web): 【企业版】定时任务到点后 自动刷新侧栏 cron 会话列表。 |
| 1887 | `8cde71284cb46fd37e77d3725b75925d79c2d3f8` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#9957: fix(designer): stop clip audio from doubling the supplied BGM |
| 1888 | `99b286d2f7e47e96b9feee7a0cfe5966f8aa2065` | 2026-11-03 | hh0205 | fix(trajectory): improve scheduled control live feedback |
| 1889 | `9c5c92bf9a028d0c3d519fd099477bf00c2d0201` | 2026-11-03 | Zora7583 | fix: 提示词优化 |
| 1890 | `a2dc7ec8ce1c2c58e7d95b9224c76e0dbb1ae5a7` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#10068: fix(designer): stop the Director failing on messy JSON replies |
| 1891 | `aa30af084d37b5d59552a564849f7834aa9f5cb3` | 2026-11-03 | nawenyu139 | fix(feishu): 下载 post 富文本内嵌图片，修复 Agent 只收到 [图片] 占位符 |
| 1892 | `ae2472cf7d80ac69fa7939326d17b4e44ca24347` | 2026-11-03 | u010240195 | fix: 用户面-修复会话页刷新后Logo缺失 |
| 1893 | `b3188493e28fc75662afa9d9ef804b3cd5514c33` | 2026-11-03 | zhangxiangyu52 | fix/develop-cron-live-unread：修复企业版cron定时任务，到点触发后，未读的消息，没有提醒（蓝色圆点） |
| 1894 | `b825ac9380d9cdc7cf806c16e4f8f61bca9f5967` | 2026-11-03 | CharlesWang2030 | fix(web):  压缩过程长时间无反馈，增加已等待时长避免被用户误判为卡死 |
| 1895 | `b8c8206c4ede7122f9ee3f81770b8f86dd5659ed` | 2026-11-03 | hw_liuyu | bugfix(ipc): use front loopback transport on HarmonyOS (develop) |
| 1896 | `c1225d870266dc268e8b2c0e5b9b755f25af583a` | 2026-11-03 | zvby | fix(swarm): optimize team organization frontend UI |
| 1897 | `c124817650ac6596d4ba3f279bdd599ac1d38954` | 2026-11-03 | u010240195 | fix: 修复导出对话命令入口及分享接口 |
| 1898 | `c90a01c0c9b0062c6558aa9d3758d2af5328241d` | 2026-11-03 | gcw_db2yUMYp | [Bug]:当前如果发生的文件产物URL带有中文路径，在客户端中下载会失败 |
| 1899 | `cc4da46769a43af036fc70a8f872772e9324978a` | 2026-11-03 | u010240195 | fix: 权限审批-企业版补齐命中规则展示 |
| 1900 | `ccebe0445221541830ccc65693224e19433210a6` | 2026-11-03 | kdgmpw | fix(web): disable cron run-status notifications |
| 1901 | `cd9bdf24af3fabe2686e5b648929d8a9ddf22607` | 2026-11-03 | alan_cheng | fix(trajectory): keep JSON copy button inside the detail scroll chain |
| 1902 | `db0ea142a8a28a75762ce8c5c29f96cfc94194c0` | 2026-11-03 | zhaozixuan | fix: avoid multiple tool sandbox |
| 1903 | `edc97b7ad5db6bd5e9225854894bde3a089ceefd` | 2026-11-03 | kdgmpw | fix(cron): 未选项目创建定时任务落库恢复物化 default/default_code |
| 1904 | `fa5ea481d3479b458c857528ece22df9572ef8eb` | 2026-11-03 | openJiuwen-bot | fix(web): honor byte-range requests for /file-api/raw-file |

## 优化改进（32 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `632c8b0702687fb0fabef16849c9eb64a1cafbe1` | 2026-06-23 | lin-xiaoyu | 完善桌面版技能交响乐显示 |
| 2 | `f493960515fb50eb42959575b42631cdbd539fa4` | 2026-06-23 | lin-xiaoyu | 更新技能交响乐配置图片 |
| 3 | `c673d587969352539fa5c729e24c1555fd6d0c46` | 2026-07-15 | douran | update(project-maintainer): update artifacts |
| 4 | `0a73a2012d26bf203f12b76040dcc620688b5411` | 2026-07-31 | taoliankuan123 | update: 更新文件 README_CN.md |
| 5 | `130a99c2131319e49cb087c348c2d812c5e2f7ee` | 2026-07-31 | taoliankuan123 | update: 更新文件 README.md |
| 6 | `7daa0d2ec0bebc0f073b40cf10b86e6e5622023d` | 2026-08-01 | alan_cheng | perf(server): cache the parsed config.yaml by file identity |
| 7 | `8d658aa5cbeb9048b68a98051864dccdcedc9e12` | 2026-08-01 | alan_cheng | perf(server): resolve unchanging git facts once per project |
| 8 | `d7e32e4a09a37753d005a8920790fe955102cdca` | 2026-08-01 | alan_cheng | perf(server): build the root DeepAgent on demand |
| 9 | `ed98b1eb73ffeb857f0d70bdeb459772cc4f4910` | 2026-08-01 | alan_cheng | perf(server): register cron tools at init instead of every turn |
| 10 | `5040da5d56732ab92ef007f829521aff2d6cf727` | 2026-08-03 | lixiaofeng6 | update: 更新文件 README.md Updated the official website address in the README file to the English website address. |
| 11 | `427966e26aebae707ae6c7fc8ab7c7d123c02718` | 2026-08-18 | lixiaofeng6 | update: 更新文件 002-feature-request.yml fix issue template bug |
| 12 | `a28cf74d6df155eeaf2a0610d1afa078536cdf8c` | 2026-08-18 | lixiaofeng6 | update: 更新文件 002-feature-request.yml fix issue template bug |
| 13 | `c287ac099d90e6c40911ace6772b1927ec9da449` | 2026-09-02 | Huang, Zeyu | update note (README) and foundation |
| 14 | `07c17949aefaf377a706f30c3977338ca6b1a143` | 2026-09-08 | alan_cheng | perf(swarm): store trajectory payloads compressed |
| 15 | `0b8b7ba5ac196f5dc131d27dcf9f40f6fe8f3f3e` | 2026-09-08 | alan_cheng | perf(swarm): skip parsing payloads that carry no status code |
| 16 | `4528407e0b5eed03875993cee48aa0e78830c991` | 2026-09-08 | alan_cheng | perf(swarm): widen the snapshot coalescing window to 500ms |
| 17 | `7d2b234738a39056cafc1b9958bd10ea1738e483` | 2026-09-08 | alan_cheng | perf(swarm): route session records without a forwarding thread |
| 18 | `a2dae6083e33fea29adba51dac0baf7dc6d0ccad` | 2026-09-08 | alan_cheng | perf(observability): stop reading span payloads on the ingress path |
| 19 | `f959dc03f18ac4ee5d8a535311326dd2806505dd` | 2026-09-08 | alan_cheng | perf(swarm): stop copying final payloads into the current-record table |
| 20 | `3e269b58f9656ff34ec674b51f01495a42107a40` | 2026-09-09 | alan_cheng | perf(swarm): drop the frame index the primary key already provides |
| 21 | `7e0a67820bf02a516dfd6d712f5cc89e1791769f` | 2026-09-09 | alan_cheng | perf(swarm): let the file state the session instead of every frame |
| 22 | `4957a90c705f9dfc9e69f13e9bd427c02d7cc31c` | 2026-09-11 | alan_cheng | perf(swarm): fetch detail, usage and frames in one round trip |
| 23 | `ce95e3bfd47794267d607065244cc487330c6394` | 2026-09-11 | alan_cheng | perf(swarm): name a frame's span once instead of on every frame |
| 24 | `3db413a977a3ce07c83a0c7739e7d0db034cf3e2` | 2026-09-15 | william9200000 | perf(web): 技能广场页面布局优化并增加缓存 |
| 25 | `f9209507ea705e169a0dccfe83efd0c4a30cbd1a` | 2026-09-15 | Henry TIAN | Update README.md |
| 26 | `7180ed7d6cfaef9b3c96ca9f1379938c0056938c` | 2026-09-16 | alan_cheng | perf(trajectory): re-project only what a live update changed |
| 27 | `1236f407f0636bdecc366b009a4f59d3c8dbd979` | 2026-09-17 | alan_cheng | perf(test): remove fixed waits from slow UTs and fix quadratic log masking regex |
| 28 | `c65fb89d2aed0736f1b8d6aec1352854a5436bf6` | 2026-09-17 | kdgmpw | perf(session): 消除归档删除链路的事件循环阻塞 |
| 29 | `ffc4461a5628c020da7e36f09955c5ebcd12bd66` | 2026-10-10 | luojiyin | perf(skill-graph): stop polling when skills page is inactive |
| 30 | `52fc3b1c1ffc2266fbfbb8e84029ec480bfce122` | 2026-10-15 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#8199: 完善全双工语音设置、状态球交互与文字会话上下文衔接 |
| 31 | `010bc43e6117a26bfc9ce73a0d37e11ee30a85fa` | 2026-10-26 | luojiyin87 | perf(skill-graph): cache graph status and read build log tail |
| 32 | `5e4ecf421b371ec2e71adf67e6a1cb8ea63d8324` | 2026-10-31 | alan_cheng | perf(trajectory): page stream frames by frame_seq instead of scanning every frame |

## 重构（101 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `3c92ad08b7f6a27fb15aa9aa5b0bf40b324f9ab9` | 2026-06-01 | douran | refactor(mode): hide plan mode for code and team |
| 2 | `becbaea1b1411da37470d763d0635dc83f5d14e0` | 2026-06-02 | openJiuwen-bot | refactor(code-adapter): 将 LspRail 从动态配置移为固定 Rail 并增强降级日志分类 |
| 3 | `b7462b1f269f462f0354ffd9c5baabbcfd960dfc` | 2026-06-25 | min_gitcode | refactor(agent-teams): support concurrent multi-session runtime in local mode |
| 4 | `13ccaad6503c702d42fd1bbe5460525ba80c3428` | 2026-07-15 | alan_cheng | refactor(team): converge task event handling on authoritative status |
| 5 | `03a93e0c1e7490aedbddafb0bc45aab62d360b00` | 2026-07-28 | alan_cheng | refactor(swarm): deliver member workspace path as a prompt attachment |
| 6 | `237692800972ef2035ac61acb51e8f8b32e6530d` | 2026-07-29 | alan_cheng | refactor(swarm): drop the member workspace attachment from the skill rail |
| 7 | `efa4704f311de7b50fdb4cec808d0b1c64b38d7d` | 2026-08-01 | alan_cheng | refactor(server): make ToolCard.stateless the one owner judgement |
| 8 | `28ffb20b8b2cd45bc0ba5ac1c7287d10307648de` | 2026-08-07 | alan_cheng | refactor: clear static analysis findings across launcher and runtime |
| 9 | `7d0849d78c1bbf74600f9d24e9559cb03fd97713` | 2026-08-07 | Haowei_Du | refactor(experience): refactor the experience module from swarm to core |
| 10 | `fbf7205e20d8fe5600317f11a9ca97ba837cd30f` | 2026-08-07 | layShang264 | refactor(symphony): remove migrated fingerprint and evaluation |
| 11 | `1b7b04ba700d6eddff752dbbd439ae56d103d203` | 2026-08-17 | alan_cheng | refactor(swarm): mount the harness agent rail for single-agent runs |
| 12 | `e46930e1de1212194a7c688eebfcac27fc522263` | 2026-08-17 | alan_cheng | refactor(swarm): move single-agent observability into the openjiuwen harness |
| 13 | `2cc2048b74b794adf7e1839f5169b1a42759f08d` | 2026-08-24 | ldstyle8 | refactor(code-prompt): change code mode prompt and switch browser rail to load-aware |
| 14 | `403fe354ad9ffbce36683a9b223ba9a36179bc06` | 2026-08-24 | kdgmpw | refactor(agentos): AgentOS 架构下 Gateway 去业务状态化，适配多用户场景 |
| 15 | `661758de1af69c9bfbdfd99998be774bbcc9682a` | 2026-09-01 | lzzzzzzasd | - 智能体/技能/扩展三面板抽取共享壳组件 PickerPanel（tab+搜索+列表+更多）， 列表高度按行数×行高显式计算、封顶 5 行内部滚动 - 面板改为菜单内 absolute 定位，三面板互斥展开、触发项带选中态； direction 贯通三面板，菜单上弹时面板与触发项底边齐平向上生长， 不再溢出视口底部 - 思考过程面板：用户上滚查看历史时暂停自动跟随滚动（距底 <32px 恢复） - 小蜜蜂动画图从 public 迁移至 src/assets（Vite import，LFS 路径同步） - Switch 组件改用 switch-track 主题 token 并缩小尺寸 - 消息气泡/文件列表去除 720px 限宽，改为撑满 - 静态 SVG 统一 currentColor 主题化；i18n 文案微调 - chat user气泡最大宽度 限定为max - 88px |
| 16 | `e705f0cf681375ad70a8922bdcfc3ff6f4e21119` | 2026-09-02 | openJiuwen-bot | cli channel化 runtime/server 重构 |
| 17 | `41e41de7803b57ef9d34bce9974c6363e1c681fa` | 2026-09-03 | hw_liuyu | refactor(runtime): 收口 interaction、cancel、cleanup 与 session.delete 公共 API |
| 18 | `9a47ab9992025b9799b19b48ff801f4a9d492bab` | 2026-09-03 | xiaoyao42 | refactor(web): 删除团队管理页签 |
| 19 | `cdb88ad34e15caa7495db68a01d45171784004b3` | 2026-09-03 | alan_cheng | refactor(observability)!: converge redundant semconv keys onto GenAI standard |
| 20 | `1002c84fbfc4177736bed3dc87988f0033d7749d` | 2026-09-04 | alan_cheng | refactor(trajectory): read turn and step identity from what spans state |
| 21 | `324e1f3baaedf2eac5547db9672a70949202c77d` | 2026-09-04 | MengHan18 | refactor(ascend-affinity): align agent-core kvc refactor |
| 22 | `01a75f827fa6abcea7926461d47af527e2ed67c9` | 2026-09-08 | hw_liuyu | refactor(runtime): 迁移 session.fork 并收口 Provision 生命周期 |
| 23 | `b368890b937b43a60a70a0b518fc76cd04ca1754` | 2026-09-08 | hw_liuyu | refactor(runtime): 补齐 Session 行为基线并定义 Provisioner 两阶段契约 |
| 24 | `5156bc39564d87ac175205bc93f9248983bfbca5` | 2026-09-09 | alan_cheng | refactor(swarm)!: point the trajectory client at the subject routes |
| 25 | `ba7ecd15398543b20ab2c0f705ec3a5ed0f5f39f` | 2026-09-09 | alan_cheng | refactor(swarm)!: load the trajectory panel one subject chain at a time |
| 26 | `d8cddd4fd16a5150bb6383f40643ba008cd6bc7a` | 2026-09-09 | alan_cheng | refactor(swarm)!: read trajectories by execution subject, not by trace |
| 27 | `fa2ab25a4cdb7b81fa60a3f9788cf42629acba38` | 2026-09-09 | hw_liuyu | refactor(runtime): 收口 session.switch/resume 至 Runtime Public API |
| 28 | `37798586798f877e2a09bbec035de9dde317482e` | 2026-09-10 | hw_liuyu | refactor(runtime): 收口 session.create 至 Runtime Public API |
| 29 | `858bcbf4144234a5061b41eba5bf95ffbe1bbea3` | 2026-09-10 | douran | refactor(runtime): own goal executions in session runtime |
| 30 | `ef6a0d6d1debe7dc2e39a1a9f6e23de15ed38793` | 2026-09-14 | douran | refactor(runtime): narrow session runtime public surface |
| 31 | `abee348a3ec68b735256904c3bf3f9aa9be34837` | 2026-09-15 | MengHan18 | refactor(ascend-affinity): centralize session lifecycle ownership |
| 32 | `03da04e177d7da5a95ad6b42a7edfdec487a3d78` | 2026-09-16 | alan_cheng | refactor(trajectory)!: read only what Agent Core writes |
| 33 | `6052b88585eb49ac460c3a70ef3546a3996d1357` | 2026-09-16 | alan_cheng | refactor(trajectory)!: schema v4 store without migrations or a change journal |
| 34 | `db0638df2dee654d77408b537340a538f045ae20` | 2026-09-16 | alan_cheng | refactor(trajectory): remove the in-process trajectory update broker |
| 35 | `f95a55437f984e9051fab738a6313026246b6681` | 2026-09-16 | alan_cheng | refactor(trajectory): drop unwired viewer surfaces and show the time switch |
| 36 | `d53a5720c4148031ada91a9c9f1bdf8579d2eb60` | 2026-09-17 | d-0771 | refactor(gateway): 同步Gateway模块在agent-os分支的修改到develop分支 |
| 37 | `e7cf2a4d3abc3b9336e3ebe58aa9474f51ac4eb1` | 2026-09-20 | MengHan18 | refactor(ascend-affinity): generalize session input intent lifecycle |
| 38 | `94487692b81ca719bd34b7a041a533646e4bd157` | 2026-09-22 | d-0771 | refactor(common): Gateway拆分工程，解耦Jiuwenswarm对Gateway模块的依赖 #1 |
| 39 | `fef842696d9d4e59305ac765b7c75c3bb41fb323` | 2026-09-23 | Huang, Zeyu | Refactor Design into dedicated project workspaces (#8) |
| 40 | `312e0b929c2eaf210745427adb0ad507bfb883db` | 2026-10-08 | Huang, Zeyu | refactor(designer): dead code cleanup |
| 41 | `33ee462388a9b07347662d0f2a5204da297d17af` | 2026-10-10 | yechenzhen | refactor(gateway): keep Opencode Zen on AgentServer and forward its results |
| 42 | `504473434c744bed792fc75818d8507d7e864a50` | 2026-10-11 | BarakeelFanseuKamhoua | refactor(designer): hard-break character, clip, and consistency naming (#5) |
| 43 | `e7f0f0cc2a4690192b34bccb588893cbea321bf5` | 2026-10-11 | Huang, Zeyu | refactor(designer): redesign canvas layout with docked sidebars (#2) |
| 44 | `8315c183a5d02fdab1674ab5120449c1d4b6728c` | 2026-10-12 | yechenzhen | refactor(gateway):gateway-unreachable-session-fallback |
| 45 | `a4e49b1368ea0ba1f307bc737307ffa418e56bf9` | 2026-10-12 | yechenzhen | refactor(gateway): own attachment, hook, and small runtime helpers |
| 46 | `b88a2bddc2ba5f2e4c499af7229bbda10589639a` | 2026-10-12 | Huang, Zeyu | refactor(design): make storyboard a strict table, other stuff moved to brief (#10) |
| 47 | `c0a3ef9fbf073e41959293ed90a4824d48a75f6a` | 2026-10-12 | yechenzhen | refactor(gateway): own session helpers that run while AgentServer is reachable |
| 48 | `3b43f524f6a2d3d53d3ddbde63df8feeb25692ef` | 2026-10-13 | d-0771 | refactor(gateway): Gateway拆分方案变更，接口与SDK基类回退为本仓实现副本 |
| 49 | `a35a05651b1d691d47725be294780c0ca17afd00` | 2026-10-13 | chenchunzhou | refactor(swarmflow): Address coding-standard findings in phase sealing and the team mirror |
| 50 | `bc5ad4af201d47269efc7b9ec0e0535e942bcc36` | 2026-10-13 | yechenzhen | refactor(gateway): common 下沉 agents 侧共享逻辑 + 删除 gateway tui会话本地兜底 |
| 51 | `d9366a4ef9a2746cb582717e77e33f1f72df6481` | 2026-10-13 | alan_cheng | refactor(todo): follow todo_create's todos input key and keep reading legacy tasks |
| 52 | `5be608e277ca7804e563c88be3f768fb0634ff80` | 2026-10-14 | xiajing | refactor(swarm): design leader surface sanitizer — tool keep-list, section removal list |
| 53 | `9609e83d015b326b42e30c10f6a9fdabe58a1f49` | 2026-10-16 | yechenzhen | refactor(common): common 反向依赖注入化 + 纯模块下沉（gateway 解耦 B1'） |
| 54 | `9c472b2d9db4c0f207027a18db3a777365216270` | 2026-10-16 | hw_liuyu | refactor(runtime): 完善资源复用、分阶段启动与 Worker 常驻恢复 |
| 55 | `5b0a33e0d9a7c6a54035fb3e292652656e745078` | 2026-10-19 | xiajing | refactor(design): move journal/WAL/compiled scripts to the native swarmflow layout |
| 56 | `aecb97936ea1ff135ade55bdb96266969bd723fb` | 2026-10-19 | xiajing | refactor(design): drop the DesignerWorkerBackend dead branch |
| 57 | `cef3075c80858682bc8f49300c73075e87adb07e` | 2026-10-19 | yechenzhen | refactor(gateway): runtime 子集复制进 gateway + host_services 下沉（gateway 解耦 B2.5，依赖 !8029） |
| 58 | `d63d5bce8dda5d1722129225349821da84bb345a` | 2026-10-19 | yechenzhen | refactor(common): A5' common 完全自包含——零本仓引用成为 UT 不变量（gateway 解耦） |
| 59 | `dd67687ba0f0151bdaaaa4e4d5856731dfd1d92b` | 2026-10-19 | yechenzhen | refactor(gateway): 吸收 gateway 专属扩展件 + 扩展加载清单化（gateway 解耦 B2，依赖 !8029） |
| 60 | `39280b7c911af6faa7b1205d7265caf6b3dc72c6` | 2026-10-20 | yechenzhen | refactor(gateway): A1g web 媒体上传 E2A 化——删除 legacy 本地落盘（gateway 解耦 A 线 1/5） |
| 61 | `7f98078fc8ee01621470042f2906c7d4c086f411` | 2026-10-20 | yechenzhen | refactor(gateway): A2g 会话面板 legacy 分支删除——统一走 E2A（gateway 解耦 A 线 2/5，依赖 !8333） |
| 62 | `f953922da394fc783b7cb8e62776b68b94e308ff` | 2026-10-20 | yechenzhen | refactor(gateway): A3g 项目/git 迁出 embedded + cron 绑定统一 E2A（gateway 解耦 A 线 3/5） |
| 63 | `2b145f1b2cada4668439a5858fc2779436053ede` | 2026-10-21 | yechenzhen | refactor(gateway): A4g config 面板统一 E2A——删除 legacy 本地执行回落（gateway 解耦 A 线 4/5） |
| 64 | `dd89f26b9ae8ca312b8852afa213a23093ff72e5` | 2026-10-21 | yechenzhen | refactor(gateway): A5 收尾——embedded/ 目录消亡 + R4 规则移除（gateway 解耦 A 线 5/5） |
| 65 | `13c207cf61802d1ca43135d8f8de6f4955220077` | 2026-10-22 | yechenzhen | refactor(gateway): M0-a 拆仓前置清理——死别名/死代码/shim/越界引用（gateway 拆仓 M0 1/2） |
| 66 | `bb809a05124d5853f7a28f9d2111f9ee9f7d29e5` | 2026-10-22 | alan_cheng | refactor(team): drop spawn_external_cli now that spawn_teammate covers it |
| 67 | `e6af32f6713598b4c2de50fa1a2bf5578643e2a0` | 2026-10-23 | d-0771 | refactor(protocol): 恢复openjiuwen-protocol外部包依赖为re-export; 消解部分extensions对gateway的依赖 |
| 68 | `6c6ff6f0188b49e7ed3bb0d3a961f4a428c7958a` | 2026-10-24 | yechenzhen | refactor(gateway): M0-b gateway 自有件物理迁入 gateway/——依赖证据四轮收敛（gateway 拆仓 M0 2/2） |
| 69 | `845d84a2305d3e2816981d99f8d805fab625f670` | 2026-10-24 | yechenzhen | refactor(gateway): M0-b 第二波——闭包口径真独占 common 件迁入 gateway/（gateway 拆仓 M0 3/2） |
| 70 | `2157799094b0b24944e652f106965b5447797c9d` | 2026-10-26 | d-0771 | refactor(protocol): ReqMethod 统一切换到 openjiuwen-protocol |
| 71 | `56fb880dda5437af924fababa3975d7e75ff58d5` | 2026-10-26 | alan_cheng | refactor(skills): unify swarm code review under jw-review |
| 72 | `979432a5a881fe506e0fb4a47af82054298d0b0f` | 2026-10-26 | alan_cheng | refactor(skills): ground swarm reviews in module contracts |
| 73 | `ea68d6ed792cd5cfaadd7038c0666f6faa9d24ec` | 2026-10-26 | yechenzhen | refactor(gateway): M0-c gateway 升为顶层包——新仓无 jiuwenswarm 层（gateway 拆仓 M0 3/3） |
| 74 | `020c82095d237fa5886cd08708e80f8798bae888` | 2026-10-27 | yechenzhen | refactor(gateway): session_metadata 域 WS 远端 provider（拆仓 bridge 替代 · 首个域） |
| 75 | `07fbdf3f79be7ba4114c3e9d16d3faef931f7551` | 2026-10-27 | d-0771 | refactor(gateway): Gateway独立目录下解耦common和extensions模块的依赖 |
| 76 | `24e8a24fd62959bb57187ca06bfd9ee5413e9ffe` | 2026-10-27 | yechenzhen | refactor(gateway): server_store_bridge 退役——gateway 运行时 server import 归零（拆仓 B3 代码门槛达成） |
| 77 | `69e8e00f97a476b67e96400a0405e94a2c0bb33a` | 2026-10-27 | yechenzhen | refactor(gateway): proxy trajectory reads through the AgentServer |
| 78 | `714b00a8458aff21f52740632bb42dfeb2dc7e90` | 2026-10-27 | yechenzhen | refactor(gateway): trajectory metadata loader 迁异步入口，补完 session_metadata 域远端化（基于 !8977） |
| 79 | `91ad07e4a7a4c015c5a0d9aa34272d8563cdbe1a` | 2026-10-27 | yechenzhen | refactor(gateway): own GatewayLock so startup skips instance_manager |
| 80 | `c540e04afcffc4e37d0bbdca7103e140d338d639` | 2026-10-27 | yechenzhen | refactor(gateway): mcp_registry 域 WS 远端 provider（command.mcp 扩展 + channel 门控，拆仓 bridge 替代） |
| 81 | `efd7d0d4eb2ac0fb29305866cb5197e18bcd1416` | 2026-10-27 | yechenzhen | refactor(gateway): sink browser config so gateway stops importing agents |
| 82 | `ff0cf1e25ab1f28e885e5883febbef2c953c2da9` | 2026-10-27 | yechenzhen | refactor(gateway): take diagnosis HTTP status from the AgentServer |
| 83 | `21a211852bc78b2ff109dd27ee638a7acf84d771` | 2026-10-28 | yechenzhen | refactor(gateway): auth 登录栈拆分至 gateway/auth——sessions.json 真源单点（拆仓中线） |
| 84 | `6fb32fce05a5a621a140c80df7bba4ceae9f1179` | 2026-10-28 | d-0771 | refactor(gateway): jiuwenswarm侧的extensions收编到gateway进程 |
| 85 | `997d28eb9cfcab055f8fd6f29974135677b2ed86` | 2026-10-28 | yechenzhen | refactor(gateway): load dotenv from a gateway-owned copy |
| 86 | `a509639aaebffb7605af9934c1ed9f50eb76de2a` | 2026-10-28 | yechenzhen | refactor(gateway): model_catalog 归位 gateway/auth——auth 栈收尾（config 域拆分 1/?） |
| 87 | `4012febcd9a35ae83012a001d2514c2ee1886fa0` | 2026-10-29 | d-0771 | refactor(gateway): Gateway拆分工程，日志和配置管理、common剩余单点依赖解耦 |
| 88 | `a11b7aad54c81b056641afe19560d47660d2c038` | 2026-10-29 | maxmadmax | refactor(permission):优化智能审批提示词和输出 |
| 89 | `d5ec979af2ccebe32a5e638e0067873bdd08b271` | 2026-10-29 | gl_zhou | refactor(diagnosis): group same-problem findings |
| 90 | `222df1638a47b0c4edaad582a7c57f64e89c83b8` | 2026-10-31 | yechenzhen | refactor(gateway): secrets/local_env_config 真源随 gateway + 母仓别名 shim（顺带修复 crypto provider 死查找） |
| 91 | `225b844a1e1e2e344d659f615402adcfbba8830c` | 2026-10-31 | whds5279 | refactor: 按企业平面拆开专家允许名单和 HTTP 入口 |
| 92 | `72e5381aa8cce7b6b4b515827d1005576959b59e` | 2026-10-31 | layShang264 | refactor(symphony): move host adapters onto Core APIs |
| 93 | `9db549bed15e4b4396403bf10f299498834c22df` | 2026-10-31 | yechenzhen | refactor(gateway): obs_files 与 tenant_key 契约随 gateway 拆仓 + 母仓别名/re-export |
| 94 | `1e390e00839309f1e705a8c5fb6b45f4ba2c9f89` | 2026-11-02 | yechenzhen | refactor(gateway): gateway 自建日志级别热更入口，清零 common.utils 运行时依赖 |
| 95 | `23017dd137b52497cc13bcd791aaf137110c4589` | 2026-11-02 | d-0771 | refactor(protocol): 企业版特性新增e2a协议迁移到openjiuwen-protocol |
| 96 | `4d6e6ce3f7772aedb117150fb441b562e2ccdb19` | 2026-11-02 | ruiqiang2 | refactor(gateway): 消除企业版引入的gateway对jiuwenswarm依赖 |
| 97 | `5a2432bad551207e78c607a9527e4a26b8897368` | 2026-11-02 | d-0771 | refactor(gateway): 企业版skill_source和telemetry.audit功能解耦 |
| 98 | `79f2dd4f89cf61424ceda2607a2748cdc07bc0f9` | 2026-11-02 | d-0771 | refactor(gateway): 修复企业版合入时引入了已经改名的gateway-protocol包的问题 |
| 99 | `cabd4ae016dd5b69e8a523c734b070c92f629ea3` | 2026-11-02 | ruiqiang2 | refactor(gateway): 解耦memory和task memory配置更新对Agent实现的依赖 |
| 100 | `cb523042e81068b8e4fae651316482fbae7f2480` | 2026-11-02 | yechenzhen | refactor(gateway): A 类单点依赖清理——attachment_safety 真源切换 + share_image_export 归属反转 + TYPE_CHECKING 口径 |
| 101 | `a002be6be0af13714292eca3bce206fdf9747c35` | 2026-11-03 | d-0771 | refactor(gateway): 下沉 cron active session redis key，消除上游新增反向依赖 |

## 文档（90 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `cd6636a6639f9a8253f20ab99039f9f17b5c7ad9` | 2026-06-02 | xiaotian077 | fix(docs): change name to jiuwenswarm |
| 2 | `dc835631812e028e7fa8752cd38a8ba00b413771` | 2026-06-03 | z2tng | docs(skill-evolution): align self-evolution guide with current commands |
| 3 | `f39d1e643dbae5490445b6ec3a4a93a4294c655c` | 2026-06-04 | xiaoxiawang | docs(browser): Browser Service Update |
| 4 | `de0f79997844cc3161cb714fe276597d9adc9760` | 2026-06-05 | xiaoxiawang | docs: Optimising the structure of the memory system documentation |
| 5 | `f657efd8348ff83ce06f171bc5b13ef50476663c` | 2026-06-05 | Haowei_Du | feat(doc)：add detailed /permissons docs in ToolPermissionsSecurity.md |
| 6 | `d1a36676ac85cadc82722f3f1e0448ba72cc395d` | 2026-06-10 | xing-yiren | doc(README): Add a README outline and adjust the paths of README files under the docs directory |
| 7 | `e4ce996f3d3ee40613715d6a4797a0b0223fd4b9` | 2026-06-12 | xiaoxiawang | doc(quick-start): Instructions for the automatic creation of configuration directories and the storage path for configuration files |
| 8 | `e7d72c433eb781eb219fc7b4d5b6b3bd709af88c` | 2026-06-12 | wangxiaolong100 | doc: Documentation images updated and optimised. |
| 9 | `fcd8c3a1842baffe2506a66935fa7f48329faaac` | 2026-06-12 | cycong_1 | docs: refresh configuration documentation and screenshots |
| 10 | `4add8f623fe2ff799f6c09ecfeda05621522a80e` | 2026-06-13 | z2tng | docs: Document image update |
| 11 | `68e7448c9164341b74fc314d6505bb2aa7e8b2f9` | 2026-06-13 | wangxiaolong100 | doc: Updates and optimisations to the page overview. |
| 12 | `c7679be4c1cfa9b2ed8c574e428f4cf2ae29e57d` | 2026-06-13 | wncbdj | docs: todo、heartbeat、cron、channel update and optimization |
| 13 | `49263ac407a7e071d26da8cdc17e277109b3b811` | 2026-06-15 | lin-xiaoyu | doc(symphony): Supplementary documentation on the use of the Symphony skill |
| 14 | `9540d8d9dcbe81dd6dbf088fd65103adbf206b07` | 2026-06-15 | openJiuwen-bot | docs: Update the description of the Agent page (Markdown) |
| 15 | `9618bd14bc619e9ab5af606fd36df66210c817ac` | 2026-06-15 | lin-xiaoyu | doc(symphony): Supplementary Skills: Symphony User Guide |
| 16 | `99bcb8fe437d5f0ba8e76ff4f741cb5120620e2c` | 2026-06-15 | xiaotian077 | fix(doc): skill doc update |
| 17 | `dd37a689c14dbdb8116731d60915643692a8ca0b` | 2026-06-15 | moyu026 | docs: update docs README |
| 18 | `29df3e6545ab2ae137a63eaef87c6ba70f2618b0` | 2026-06-16 | iamcandiceguo | docs(swarmflow): 补充swarmflow使用文档 |
| 19 | `5f8d2ee8ac79154b879dbe5a3a6f8eb91881e211` | 2026-06-16 | xutuo | docs: 补充 AgenticSkillRetrieval 文档 |
| 20 | `5629ee7b07b5c3b365fcc0b5bff9ae055763910a` | 2026-06-17 | chenchunzhou | docs(README): Add README document |
| 21 | `e9858540e81f704e54992176f947c052a4f00c2f` | 2026-06-18 | xutuo | docs(symphony): 更新 Symphony 文档 |
| 22 | `70dff644d634dcd526904c87ce1a48f70d35aa75` | 2026-06-22 | ldstyle8 | docs(TUI): add /btw, /review, /simplify, and /security-review to slash command documentation |
| 23 | `163903a5ff6183f9d81fdba85d3285667765e7e1` | 2026-06-25 | renquanjia | docs(dialog): Dialogue documentation |
| 24 | `fbeb6866c6d40b9fddc0c82794d68f9b52c303de` | 2026-06-25 | xiaoxiawang | docs(heart-beat): 更新心跳文档 |
| 25 | `05be8fee8c0ed7be281a678a0d72f4eddd80eed0` | 2026-06-26 | xinyang_bali | docs(Agent): Agent.md improve |
| 26 | `b17e1d99ff6df991f504c87b18e152a61391159d` | 2026-06-26 | xiaoxiawang | docs(Page-overview): Fix the page overview |
| 27 | `ef260c48d97043558573c5234709beac3d1d6799` | 2026-07-02 | sapeurs | docs: 补充 jiuwenbox 单独出包指导 |
| 28 | `3cfafec71ce7a90e82a341b0b6a730fe7999a852` | 2026-07-03 | xiaoxiawang | docs(heart-beat): 心跳文档中英文重构 |
| 29 | `96f42e1d7470aad3420242e325b5e7b0c604538c` | 2026-07-03 | xiaoxiawang | docs(channels): 更新频道文档结构，拆分国内频道和国外频道 |
| 30 | `b7309bf5e4f3c76fb57c733a3bbaf184bcc40330` | 2026-07-06 | renquanjia | doc: 会话、定时任务 |
| 31 | `09341030cf3dc645fcb52e73ac38cfa43dc56042` | 2026-07-09 | xiaoxiawang | docs: 优化上下文压缩与技能演进文档 |
| 32 | `3f52853d4b3bf1171fda881d48539b500fdbbd17` | 2026-07-16 | xiaoxiawang | fix(docs): 更新readme文档导航 |
| 33 | `01c76edb24f391075fe7cc45baf133e9b0f1231e` | 2026-07-21 | hwxiajing | docs: add Agent Team human collaboration guide (HITT) |
| 34 | `a8b5f02804d7966c5157763be53d40637d4e210b` | 2026-07-24 | xiaoxiawang | fix(docs): 修正配置信息.md |
| 35 | `6f643cdd8a4a1c46e7b511d97f28a1217927e0b1` | 2026-07-31 | openJiuwen-bot | docs: fix broken links, image paths, and stale references |
| 36 | `8d10eccbabc05ffe4083803f35d3a6f4f3221c80` | 2026-08-01 | alan_cheng | docs: record the rail construction timing breakdown |
| 37 | `8e1315601d197b804453e2d5c7a5cf5bd3ed13e2` | 2026-08-01 | alan_cheng | docs: archive the message pipeline performance investigation |
| 38 | `e78f5bc65d4d930087e924bc231592536fc9e9ce` | 2026-08-01 | yangzequ | docs(zh): sync Chinese docs with current Web UI and replace broken screenshots |
| 39 | `1c4f9c9b62ad7a72f11d507e4549b1997d33cf1a` | 2026-08-03 | douran | docs(tui): sync slash command behavior |
| 40 | `cedd7325fd2debbdcf2a39fe46e4890b304ce864` | 2026-08-03 | yangzequ | docs: sync English docs with current Web UI and replace broken screenshots |
| 41 | `125eb9670681570c8b336ef363e55ce4415fc32b` | 2026-08-05 | xiaoxiawang | docs(dialog): 重构对话文档结构 |
| 42 | `12bd63fd36a29a585802d8ddbb465fd427a99fe8` | 2026-08-05 | xiaoxiawang | docs(Agent): 修复智能体文档 |
| 43 | `c2f82fc20ff2a4ea3fe3db87a9f4ab650231e7fd` | 2026-08-11 | SushiNinja | [mirror] openJiuwen-ai/jiuwenswarm#2556: docs(E2A+mode): 更新模式系统以及E2A协议文档 |
| 44 | `99f8b477501df56f2c7e316eae58a8e330548851` | 2026-08-12 | chenchunzhou | docs(swarmflow): refresh SwarmFlow TUI guides and command references |
| 45 | `bedd8d1847ad87e2d02a2ec85955f1d3ca591e9d` | 2026-08-12 | alan_cheng | docs(swarm): describe team Skill visibility declarations |
| 46 | `f060904e34e30d8444446ceca47c9fb89bd88e5a` | 2026-08-12 | alan_cheng | docs(swarm): note that team declaration seeding lives in agent-core |
| 47 | `1a02a76dc6ad1c1b3fbd2f7d183df84316af4ad2` | 2026-08-13 | yangzequ | docs: remove heartbeat feature documentation |
| 48 | `31daca98e52358e8972bd0b894587a8f87fa4142` | 2026-08-13 | XinchenZzz | fix(docs):安装文档相关内容修改 |
| 49 | `8fbf2b3b54734b9a03a81a93ad8050c5c2ec6771` | 2026-08-13 | XinchenZzz | fix(docs): quickstart.md相关内容修改 |
| 50 | `cf87888b8391fab21d1089d927544696919d9018` | 2026-08-13 | yangzequ | docs(config): fix accuracy and usability issues in config doc |
| 51 | `2edfc03aad8c65256869495dc6635b361e2a4482` | 2026-08-14 | XinchenZzz | fix(docs): 安装指导文档内容修改 |
| 52 | `3478cd79a3008674bfea877b8b7d15035341ed5c` | 2026-08-14 | yangzequ | docs: correct technical accuracy issues in the agent documentation |
| 53 | `3c0daafad29e8a5c794eee3f474c53be4d022cf8` | 2026-08-14 | yangzequ | docs: Improve document indexing and remove redundant files |
| 54 | `3d881da5f0619b6f290a8eda3d9c1dbb5746c810` | 2026-08-14 | yangzequ | docs: Clean up outdated content in log documents and remove frontend logging function descriptions |
| 55 | `6f4fb68fd5fe5b9d30e1a11138de13a0840ed1ee` | 2026-08-14 | yangzequ | docs: supplement documentation specifications and unify document formats and structures |
| 56 | `c99adb59735fe536f5f7f0cb736623f582daac9c` | 2026-08-14 | yangzequ | docs(zh): fix conversation doc |
| 57 | `e55b6a0ba4ecf755d90ce6fa62b7c97d857d606b` | 2026-08-17 | z2tng | docs(mode): document code.normal self-evolution limitation |
| 58 | `e9c3fdb7512f7f2d7938c74e68a3e07583f4f836` | 2026-08-18 | XinchenZzz | fix(doc): 修复安装文档的错误描述与遗漏 |
| 59 | `472dc324af6bcae383a7d1c7d1a8b261e239f476` | 2026-08-19 | XinchenZzz | fix(doc): 修正 macOS 架构说明并补充beta pip安装方式 |
| 60 | `0ec1dc20b3c8405173448fe478f29829ce6356ed` | 2026-08-27 | Ziliang_Zhou | docs: Supplement configuration document for external jiuwen‑memory |
| 61 | `432c19a5ebaca9915a1fa36633e8cbaa7e8113b8` | 2026-08-27 | xiaoxiawang | docs: 更新文档图片 |
| 62 | `4669ce8a8d5afb38e106e10c618c4529180c0991` | 2026-09-03 | xiaoxiawang | fix(docs): 修复多文档技术准确性与易用性问题 |
| 63 | `ba99b6f763e9c4988f9668aa1cbcede13cc2bfb7` | 2026-09-09 | huanghongyun | docs: trim README navigation trailing whitespace |
| 64 | `9c0e743a79e73a68e684785a23b533f72cc6ed12` | 2026-09-10 | z2tng | docs(skill-evolution): align guides with current workflow |
| 65 | `fd0bb8982da4b637175166e45ae65db59cced919` | 2026-09-23 | m056838700 | [mirror] openJiuwen-ai/jiuwenswarm#7099: docs(browser): clarify SDK guide and desktop scope |
| 66 | `8b3d49c331c2fbd90b08ae661a30ba3c90892e73` | 2026-10-06 | BarakeelFanseuKamhoua | Document and ship Designer agent-spawning quality pipeline on design. |
| 67 | `a9e88c7e2dea1c5066d41ea28d405c5904cd4f19` | 2026-10-06 | Huang, Zeyu | docs: 新增设计模块使用文档（中英文）并登记到文档索引 |
| 68 | `a2679bdab25f7f9d3cb6a9a0940a210b5107b4b9` | 2026-10-07 | ZezeChang | docs(swarm): add organization configuration and acceptance scenario |
| 69 | `20a09fc7c88372b4a4737f23d24e8148c39c7594` | 2026-10-08 | liangfuqi | fix(doc):更新文档包名为workswarm |
| 70 | `3ca6ebaaa2f01942c9c19e80d628f9ca5bdd4b8c` | 2026-10-12 | chenchunzhou | docs(swarmflow): Document tool activity visibility, run grouping and the tool-call label |
| 71 | `457d25dbe01fb4ecd78ac94e900ec6ab75f44e15` | 2026-10-12 | chenchunzhou | docs(swarmflow): Document third-party agent nodes, their Web avatars and fork rules |
| 72 | `794fa940ba6a32c4e351588f50a92c68f2f544e1` | 2026-10-12 | chenchunzhou | docs(swarmflow): Document the swarmflow_progressive_tool config key |
| 73 | `f559c4f9dd9927029ef7ee077a8081e54db234fc` | 2026-10-12 | chenchunzhou | docs(swarmflow): Document third-party model options and the no-fallback rule |
| 74 | `48442f9a801a04e9f795a5a5c08a608fdce932ef` | 2026-10-13 | alan_cheng | docs: add repository contributor guidelines |
| 75 | `55bc5edf9c5af6d37e7dfe626c1528cdb69413c3` | 2026-10-13 | chenchunzhou | docs(swarmflow): Complete the third-party option rows and the config excerpt |
| 76 | `77f26cebf9b01809d2f24cd2a529c1003c1c9721` | 2026-10-14 | chenchunzhou | docs(swarmflow): Document agent_template expert workers |
| 77 | `7826ee439fe66a77319499be97c0c8a1f4db3388` | 2026-10-15 | douran | docs: add JiuwenSwarm PR review skill |
| 78 | `9b61b6e791fb28792e0e2dd5bde59f14b8136b1b` | 2026-10-15 | chenchunzhou | docs(swarmflow): Describe how expert workers appear on the canvas and roster |
| 79 | `ddbe0696ff33eff4c467a946b90f97d66c0121ea` | 2026-10-15 | hh0205 | docs: 添加模型组使用文档 |
| 80 | `9fc365b0a19b2ef5db482c1dc1d2b0e1f5a8ad14` | 2026-10-20 | gp1000 | docs(observability): clarify trajectory_ui implicitly enables OTel provider |
| 81 | `b13e446a652f6ec272c237f701b05a2bca30580e` | 2026-10-20 | alan_cheng | docs(team): document bringing installed experts into a team |
| 82 | `904c688c966a3eaf48c3dac5d5e38d3180b38dcc` | 2026-10-21 | alan_cheng | docs(agent-group): allow underscores in roster ids in the package spec |
| 83 | `15588e1dd8d27363d21b3387dd95c28f4509c5e1` | 2026-10-24 | birfied | docs(en): fix symphony.md link case in Configuration and Skills (#5659) |
| 84 | `bd61503fc8006bf71700f2586710e56cd43d451b` | 2026-10-26 | alan_cheng | feat(docs): sync author metadata and module-based navigation |
| 85 | `1807d2c8ad710f7fcaeef8fe3c3ac16809b46534` | 2026-10-27 | ZezeChang | docs(swarm):更新Team Organization相关文档 |
| 86 | `7f24366e9fb6d6e9964d036898f3013e049bdcf6` | 2026-10-28 | luojiyin87 | docs(testing): correct stale CI and test layout references |
| 87 | `c48048751fee7b44479f27288f0810152288e9b9` | 2026-10-28 | GeorgeGcs | docs: align Python version badges with package metadata |
| 88 | `1fcffc7a8939507fe13420b63b4be32c75e9a7a4` | 2026-10-31 | GeorgeGcs | fix(docs): correct Feishu resource permission scope |
| 89 | `01c69908f3156efb57648df9b78cc79f92dec4c8` | 2026-11-02 | GeorgeGcs | docs(ui-e2e): align guide with current runner behavior |
| 90 | `4bc4ede0089ad95a53b74841fd5700331f387190` | 2026-11-03 | alan_cheng | fix(docs): remove document counts from generated indexes |

## 测试（38 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `06e4193e128e14b7088847292d3fc6846def389f` | 2026-08-01 | alan_cheng | test(server): await the now-async rewind agent resolution |
| 2 | `b484219629592b9f05dbeceeca38f6fe84e0386d` | 2026-08-04 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#1416: test(gateway): prevent team_name regression in session list |
| 3 | `90917ec7afc518a4f378e8e84e8eae302a78ea18` | 2026-08-12 | alan_cheng | test(swarm): cover visibility RPC, migration and team skill assembly |
| 4 | `9cd91d8b5d9aa29a0f2ef2dd2e4a9c2f9c51b83a` | 2026-08-12 | alan_cheng | test(swarm): guard that only the workspace seeds the team declaration |
| 5 | `bb5c6427f960ba41f05f6a403b6df1afea7f614b` | 2026-08-31 | min_gitcode | test(agent_teams): cover optional CLI runtime installer |
| 6 | `f6376747643b9540a8f0aa76e06469fa59c4e841` | 2026-09-02 | Tiffany_F | test(web): 增加心跳页面、插件、mcp等页面testid |
| 7 | `45b8dac25ff359cf37ac569f582e0a916d0ceed3` | 2026-09-03 | hw_liuyu | test(runtime): 补齐边界特征测试并恢复错误 metadata 透传 |
| 8 | `27d93b3d72256e5ae91527506c7c308646a28987` | 2026-09-07 | Tiffany_F | test(web): 增加testid规则放在AGENTS.md |
| 9 | `cf91140b8cbadc384298bc7db1d84f66bb168682` | 2026-09-14 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#5474: test: use the pinned openjiuwen instead of sibling checkouts |
| 10 | `20d81702b47380078f06fc544d12e4db13d894c6` | 2026-09-16 | alan_cheng | test(trajectory): pin that Team member records pass the ownership rule |
| 11 | `26d0ff247334463b565d1e24de6412017f514cc4` | 2026-09-16 | alan_cheng | test(trajectory): replay the v2 vectors shared with agent-core |
| 12 | `b3364c24d00ce68eac3986c925633209b41cdd11` | 2026-09-17 | alan_cheng | test(trajectory): cover retention edge cases and align Python with viewer rules |
| 13 | `49c9f5b4c66d193e4168bfa9c82c2df025c773f0` | 2026-09-25 | kdgmpw | test(server): 移除用例补 sessions_dir/project_store_dir 隔离 |
| 14 | `644e769bdf9a5c3a35b89a132a84f7f5720b2be1` | 2026-09-29 | Huang, Zeyu | test(designer): fix design tests that drifted from intentional behavior changes (#76) |
| 15 | `4232e89ad0bf8841ed8b1d8694c329c96dc71721` | 2026-10-06 | Huang, Zeyu | test(designer): move design-specific unit tests under tests/unit_tests/designer |
| 16 | `7fd24f1ac2fbdf5d4f87d930a88422200f6d5a1c` | 2026-10-06 | Huang, Zeyu | test(designer): drop stale STORYBOARD SHOT marker assertion |
| 17 | `94d6e68efc3ca6a27458748a525e145bcba8d088` | 2026-10-06 | Huang, Zeyu | test(designer): drop poll_interval_ms from trajectory settings fixture |
| 18 | `c83206cca763ec0ea3f58a098797fa127b72a520` | 2026-10-06 | Huang, Zeyu | test(system): run service subprocesses with INFO console logging |
| 19 | `edc2284c949faf232d84d5519f4a81a0911ed664` | 2026-10-06 | Huang, Zeyu | test(designer): mock audio backend detection in reference-led scenarios |
| 20 | `d9d77e0f8c4eb495a746e6efe603a37d50ebf3cb` | 2026-10-07 | ZezeChang | test(swarm): verify experimental UI and expert conversation isolation |
| 21 | `5f28ddaf288be799733fa861157ca261b66a6bf2` | 2026-10-10 | ZezeChang | test: align snapshot monitor fake with team identity contract |
| 22 | `333142343470a3a7f6372722760e0d3c29649835` | 2026-10-12 | chenchunzhou | test(swarmflow): Cover activity consumption, batching, run grouping and phase sealing |
| 23 | `68391f3867d1fcf4fda9e176585666ab4057e5e1` | 2026-10-12 | chenchunzhou | test(swarmflow): Cover node model refresh on completion and failure |
| 24 | `7e955d3cb570df21306eae84980b8e4a6714050d` | 2026-10-12 | chenchunzhou | test(swarmflow): Cover swarmflow_progressive_tool mapping and default |
| 25 | `b7808c1f96f14a633294a05ebc160009b408a993` | 2026-10-12 | chenchunzhou | test(swarmflow): Cover agent_type pass-through and node identity resolution |
| 26 | `9669c927c07206b013b8554214a6363d136e4c7a` | 2026-10-14 | xiajing | test(designer): restore old designer UTs that still pass on the new architecture |
| 27 | `fd67a4bd8f4298143d0d3d6e5945e991993191ec` | 2026-10-14 | chenchunzhou | test(swarmflow): Cover expert worker events, validator and fixture group |
| 28 | `186bccc65271053163b483aa366c36fcce985036` | 2026-10-15 | alan_cheng | test(agentserver): await attach_gateway_connection in diagnostic worker |
| 29 | `20023421c4a276665bd3d0021caac518b0901fbf` | 2026-10-15 | alan_cheng | test: isolate unit tests from the real ~/.jiuwenswarm |
| 30 | `2d49768eced54676682c3d8861b33cbee45881cc` | 2026-10-15 | Huang, Zeyu | test(design): drop combinatorial unit suites and merge designer into design (#43) |
| 31 | `5eed050c53ce72e7c7cd960a2bbe45c1f625c083` | 2026-10-15 | chenchunzhou | test(swarmflow): Accept the checkpoint persist's write options in monitor mocks |
| 32 | `cba06200706b9a8fe6b9e0240bc7244543993396` | 2026-10-15 | chenchunzhou | test(swarmflow): Cover expert badges, restore identity and persistence races |
| 33 | `cf0429a8b4677caeddeef13ce6feb44f865b888e` | 2026-10-15 | alan_cheng | test: isolate subprocesses spawned by unit tests from ~/.jiuwenswarm |
| 34 | `29c759e02bb837aefe2cc56fdd90b7c105ab7b0f` | 2026-10-20 | alan_cheng | test(team): cover the expert catalog, card-based reads and assembly baseline |
| 35 | `604769fb97fcd137aa2a2257e31ab848d86b9460` | 2026-10-20 | xiajing | test(design): align handler dispatch tests with the unified write |
| 36 | `44919104bfca0a81b573c78f063e6e0254684890` | 2026-11-02 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7679: test: let caplog see the jiuwenswarm logger tree |
| 37 | `08324aee5a7a6a0dd26be24ee8cadf49a4785295` | 2026-11-03 | alan_cheng | test(permissions): respect enterprise approval options |
| 38 | `6e4d67481b673a9c517e3a6598bbff299ec2f1a3` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7680: test: redirect every workspace path when a test moves the home |

## 工程/依赖/构建（33 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `03bd81dd6d272eb614d9027d48a4163aa9e55624` | 2026-08-03 | lixiaofeng6 | Updated the website address in the README file. |
| 2 | `c7fd957e144f64addc72e116090e2f17b229335a` | 2026-08-03 | lixiaofeng6 | Updated the website address in the README file. |
| 3 | `c000a645e2f7f58389b45059d061ebb4904bfa48` | 2026-08-04 | ldstyle8 | chore(code): temporarily disable explore subagent |
| 4 | `021e308286d135a7d6b9bc6b2467f185b364d112` | 2026-08-07 | alan_cheng | chore(deps): bump openjiuwen to develop head c1a58c09 |
| 5 | `4f9c74810b26226ef71b18ac9f24618080d92719` | 2026-08-13 | alan_cheng | build(swarm): add Makefile with openjiuwen update and run targets |
| 6 | `ac365fe52c51f763e17ebf16220f988d641b2547` | 2026-09-11 | alan_cheng | chore(swarm): drop the trajectory poll interval nothing reads |
| 7 | `035f07132c030863ea4185f48937a806319dc3d4` | 2026-09-14 | alan_cheng | chore(deps): bump openjiuwen to the latest agent-core develop |
| 8 | `41ef7f911271d6f3801974ce10c33b7f805c697b` | 2026-09-17 | yangzequ | chore: keep RSI task creation on  desktop  usable without an installed Harness |
| 9 | `b72ca980ffceab0c1700ff82a9c63e7feeb41fdb` | 2026-09-17 | alan_cheng | build(deps): bump openjiuwen to agent-core develop cd1dbb66 |
| 10 | `05219d691c1cf669a509cc52b4fabb8e951e700b` | 2026-09-23 | alan_cheng | chore(deps): bump openjiuwen to latest develop |
| 11 | `7d1bbe2cc2c9c509db9967dcbaa36859d112f05b` | 2026-09-23 | lin-xiaoyu | chore: pin Symphony Core develop merge |
| 12 | `ce8af2051fd7c5dff85a09f8185fce64d32893a6` | 2026-09-28 | ldstyle8 | chore: remove aaa_design directory |
| 13 | `6948136a5fe5c4dd27587139be5b5d3894604d39` | 2026-09-29 | BarakeelFanseuKamhoua | style: update toast wrapper and connection toast styles for improved … (#75) |
| 14 | `691d1f44cb77678c1fa475bc87918d4efdf525ac` | 2026-10-09 | ZezeChang | build: update agent-core to latest develop commit |
| 15 | `9ef1c8adc81d97c6717eef7e811f4d55b0dd7409` | 2026-10-09 | alan_cheng | chore(trajectory): drop Huawei copyright headers from trajectory viewer |
| 16 | `1c45ea42c86b211d24a555706fc10d7ffb154dd6` | 2026-10-13 | chenchunzhou | chore(swarmflow): Bump agent-core to 72cf5a4b for the third-party agent engine |
| 17 | `1fb105a28cc901a19cc18115b5ae28dafc94fe59` | 2026-10-13 | alan_cheng | chore: normalize code ownership files and document their relations |
| 18 | `118952e974b8776ac088f933714410ee70dbdb25` | 2026-10-14 | alan_cheng | chore(make): add restart-debug-fast to restart without rebuilding the frontend |
| 19 | `62c03d6a63934eb78fda0abca59c0d53af96e526` | 2026-10-14 | yechenzhen | chore(scripts): 分层检查脚本 check_layering + UT 守护测试 |
| 20 | `c0224b87335648aff8b7f6f8fdabadaa6a8adc7d` | 2026-10-14 | xiajing | chore(team.design): drop the DESIGNER_MOCK_MEDIA debug seam |
| 21 | `1b0ab9ba21383ee24877e4109bf18867ada8d177` | 2026-10-15 | alan_cheng | style(config): wrap overlong ValueError message |
| 22 | `3f8a9bf6a553038c733fa1e3e6fae5625ffc5d26` | 2026-10-15 | alan_cheng | chore(deps): bump openjiuwen to latest develop |
| 23 | `bc9a5823a7f1bf18f7b78a33b7c2489e695e8282` | 2026-10-15 | chenchunzhou | chore(deps): Bump openjiuwen to 2504939 for agent_template expert workers |
| 24 | `ce8719f0b768acb24e53a2a71d32cc210ac8a624` | 2026-10-15 | alan_cheng | build(deps): bump openjiuwen to 680795de for dormant retire status |
| 25 | `f821135a47807a546d9bd87880e18cceb1dcc476` | 2026-10-15 | chenchunzhou | chore(swarmflow): Rewrap the validator option set after the line-width fix |
| 26 | `2fa263c9c6bad3ec05f9ac9484ae3de8ce796ddc` | 2026-10-16 | alan_cheng | build(team): bump openjiuwen to f9f5036 for MessageInfo.meta |
| 27 | `8086898e3be2e6d423ba86c28ed3494aaddd2311` | 2026-10-21 | gl_zhou | chore(repo): remove accidentally committed empty files |
| 28 | `6344ba2d5ab7d9406b881a56a860046cb52246fe` | 2026-10-22 | alan_cheng | chore(deps): bump openjiuwen to the core commit with scheduled member messaging |
| 29 | `39ceb53130a2e810c95e5cbf020098eadea28934` | 2026-10-26 | alan_cheng | chore(skills): migrate coding workflows and swarm designs |
| 30 | `c34ee456a7219b4375c45732181247d42b34ab6b` | 2026-10-27 | accident2ly | [mirror] openJiuwen-ai/jiuwenswarm#9767: chore(design): 实验功能增加设计模式 ComfyUI 工作流开关 |
| 31 | `fe2886223a24aff3392246abd1ecd6e25e6abd41` | 2026-10-29 | alan_cheng | build(deps): bump openjiuwen to e0891988 |
| 32 | `527ebfaaf4e863161b0d6f0ab370f1561f22077a` | 2026-10-31 | min_gitcode | chore(team): log processing status lifecycle and handoff |
| 33 | `60eb1355e9a6ee38faeaf02331418ab5661f441b` | 2026-10-31 | layShang264 | chore(symphony): remove unused migration leftovers |

## 安全加固（1 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `bd18c0241b25de35d53cecda65942bca53995ced` | 2026-07-29 | yyuse | 企业分支安全护栏特性回合 |

## 回滚（8 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `1454fa017dbd42df1f2d9a60c1295a2d2af9982e` | 2026-08-18 | yyuse | Revert "add permission modes (Full access/Auto/Strict)" |
| 2 | `e3ee2dec04ae8992cf3f5a1cc9bef334d58a01d1` | 2026-09-17 | yukuanj | Revert "feat(a4p): integrate A4P intent authorization" |
| 3 | `898d00acaa779053971912c805672eaf5d78441b` | 2026-09-25 | Huang, Zeyu | Revert "Configure Designer defaults for DeepSeek and MiniMax" |
| 4 | `6634fab15a7ca876c913967d0433fe15f52e72a2` | 2026-10-06 | Huang, Zeyu | revert(config): keep develop's INFO console log level |
| 5 | `f798ee3868ac2a39220e200b9dd2a5ef94b71195` | 2026-10-14 | d-0771 | revert(protocol): 临时回退gateway-protocol外部包依赖 |
| 6 | `c11c9c7623a1fd8fa77ee7ef6dfdb8c67f15f26c` | 2026-10-15 | xiajing | revert(gateway): web-channel listening timeout back to 5s |
| 7 | `3ef1de9bf0b4813e2a0e8674dedc499c78e53df2` | 2026-10-21 | XinchenZzz | revert(build): remove packaged source revisions from develop |
| 8 | `33e55148952567dba5bb090679383f7c17d199d7` | 2026-10-29 | XinchenZzz | revert(worker): roll back !9157 lifecycle changes |

## 其他（100 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `953a77de1de090703d36a57c62a4106ac78450c2` | 2026-07-16 | douran | Merge upstream/dev_aipc_feat into upstream/develop |
| 2 | `8460a3cc7d2efa4fc7c877e410dc47153744033d` | 2026-08-06 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#2405: Fix/skill omni creation update |
| 3 | `e62be3a34522fe829433624b752239328511ce22` | 2026-08-06 | Huang, Zeyu | Apply suggestions from code review |
| 4 | `16932100d6b660e466e90330a9b75fd991308d3a` | 2026-08-19 | Tiffany_F | 合入 ModelSetupGuide引导、SessionSidebar、BrowserPanel浏览器类型、ChatPanel主动推荐行、InputArea模型组、ConfigPanel免费模型相关的testid |
| 5 | `4f5ec59fdef30a7f1ee47b535ffcbcfbea74741a` | 2026-08-24 | Tiffany_F | 补充定时任务界面testid |
| 6 | `cd1ddad978eac7fdd893c44f80aa0e78c1d6d98f` | 2026-09-01 | Huang, Zeyu | init |
| 7 | `180cb74db3d732a7f6e66777e92e70e3871660b9` | 2026-09-02 | Huang, Zeyu | remove obsolete AI note in README |
| 8 | `44d7b5949b62b2ab24802a5a18c30e17cef0e50c` | 2026-09-02 | Huang, Zeyu | stub entrypoint for design page |
| 9 | `1db176abd52970e32a84850fd3d8aec04b2a021a` | 2026-09-03 | Huang, Zeyu | edge interaction |
| 10 | `28fb039b9584d74b0607e1a97e04b641cf25612e` | 2026-09-03 | Huang, Zeyu | fake workflow execution |
| 11 | `9945c6bef56014f02e3f2edd9cccf23bbf4d2746` | 2026-09-03 | Huang, Zeyu | jump to feature page & tempalate content |
| 12 | `a1f6e676d0e602dc34eb777479eae6204d4e7c54` | 2026-09-03 | Huang, Zeyu | toolbar for text node |
| 13 | `cc9c99c3561d413f41360b76ea1c02e5b75e47c5` | 2026-09-03 | Huang, Zeyu | toolbar for multimodal nodes |
| 14 | `002a3b95ab03a77e30e36aded4e3094f1634d85f` | 2026-09-04 | Huang, Zeyu | node upload asset & asset mgmt |
| 15 | `6cd4de227a1a5d00bd105aaf3327455a88f0aa79` | 2026-09-04 | Huang, Zeyu | Integrate UI from workflow branch |
| 16 | `81ba1c516c26490f7f569e5acc9a65aae5f3b008` | 2026-09-04 | Huang, Zeyu | complete mock generate behaviors |
| 17 | `b4e32b22329db53fc1767a6c9aabe3ad5244224b` | 2026-09-04 | Huang, Zeyu | node control's assets strip |
| 18 | `0780a2d145d55d56364e3f955ee56600038d6341` | 2026-09-07 | Huang, Zeyu | additionally resolve logic after rebase |
| 19 | `b34b64a626a5aab7c9205c0206a6d17b38d10470` | 2026-09-07 | Huang, Zeyu | cherry pick workflow branch as of 0907 |
| 20 | `8a1235fe17ff6b7830ea4d7b6be793e0b0b60bb0` | 2026-09-08 | Huang, Zeyu | simpler regenerate |
| 21 | `54aaed8cb126eb09d944f1cc442ee5954793358b` | 2026-09-09 | Henry TIAN | Feed clip generation explicit character, scene, and keyframe inputs. |
| 22 | `54d105e62611af5fdfc3345fae7ea8bf5a87bb73` | 2026-09-09 | Huang, Zeyu | Ship Designer shot pipeline with storyboard-driven keyframe prompts. |
| 23 | `87d8dceae885f810abce383cb104d3ff5aeb6bec` | 2026-09-09 | Henry TIAN | Make storyboard Brief-driven, editable, and feed keyframes prompt plus images. |
| 24 | `892ab7d2345d2957cdbba8617b66c9fe1a2ff509` | 2026-09-09 | huanghongyun | Remove the redundant harness evaluation dataset from the RSI parent line |
| 25 | `30bf5b5ce22be2e813cf538ababbc29ac96fc44d` | 2026-09-10 | AFSDFASD | Preserve RSI polling and parallel evaluation behavior after merge cleanup |
| 26 | `3b81592e725a1ca266c7a7beebb67cdf0c00c67e` | 2026-09-10 | Henry TIAN | Unchain keyframes so clips can start as soon as their own still is ready, and restore Designer chat prompts when reopening a graph. |
| 27 | `6515a580c55b83be455c51d21a3cd3b27b6766ef` | 2026-09-10 | l_x_d | Preserve paper optimization result projection on the RSI parent line |
| 28 | `77f3869fc9438a94283fa9325322e57cb6a0d8b7` | 2026-09-10 | Henry TIAN | Keep later keyframes from cloning shot 1, and restore the Designer graph after refresh. |
| 29 | `e3f86e02e45e7b867fc94431afdcc4ef300a5d8b` | 2026-09-10 | l_x_d | Keep paper output and proxy notices on the RSI parent line |
| 30 | `e46d07f29de2f2e02510533e4d35b94a639ccad2` | 2026-09-11 | Huang, Zeyu | README |
| 31 | `57fdee3323d3692f8b410e36f0e793955f58d821` | 2026-09-12 | yangzequ | Keep MCP credential documentation aligned with the legacy RSI resource format |
| 32 | `b9dd09e0748d520b2634b4ac7c1267f993fff8d2` | 2026-09-12 | yangzequ | Align RSI install refresh tests with the pre-release package filter |
| 33 | `a45b344c26e1874b5184acec49ddf4d6aedf9f49` | 2026-09-14 | Henry TIAN | Align Designer canvas nodes with MiniMax media types. |
| 34 | `3b796d47ac1e4fdbcfd19e964e09bafea35d5381` | 2026-09-16 | BarakeelFanseuKamhoua | Ship Designer v5 compose-from-solos continuity with scene-bible prompt handoff. |
| 35 | `6f5af4faeceea5451172bfe9f6af3f7178123520` | 2026-09-16 | BarakeelFanseuKamhoua | Harden Designer Enter approvals, occupancy locks, and serial clip continuity. |
| 36 | `8ac540306a68d674d1d8836bf49e3cc830e9850c` | 2026-09-17 | Haowei_Du | fix（proactive)： modify colour of button |
| 37 | `5fe314b8c04128793a1550fe4b9f79263ee05f45` | 2026-09-18 | BarakeelFanseuKamhoua | Ship Designer LLM shot budget, storyboard-sync clips, and compose wait gates. |
| 38 | `22eb015921c73b509ccebe0a02ab0ae4989d62cc` | 2026-09-21 | Haowei_Du | fix（skill pack)：show skill pack on web |
| 39 | `3cdcfe6a8d377b9a76b68e772d414c816bc64d00` | 2026-09-21 | Henry TIAN | Revise the front-end ui (#7) |
| 40 | `4983b0215339d8bb8504bc95f1d0509fccc32d17` | 2026-09-21 | Henry TIAN | Merge Front-end setting together with existing agent framework (#5) |
| 41 | `5106de1d7b890611f854d6c826a822cd5958ec65` | 2026-09-21 | Haowei_Du | fix（proactive)：add en setting for proactive recommendation and avoid delete all file when restart the exe |
| 42 | `657e8863405b94bc868a19201e312244aaf3d5a5` | 2026-09-22 | Henry TIAN | BGM module fixed; Visualized the input reference image; Remove the TTS function (#9) |
| 43 | `8277bc58763e26975987b50ff702f4853c4da7bd` | 2026-09-23 | huanghongyun | [develop] fix(agent-group): preserve package type and remove sample |
| 44 | `5a65b8c7e92332beaacf2e63032414ff37a1870d` | 2026-09-24 | BarakeelFanseuKamhoua | design-no-keyframes-2.0: storyboard continuity + concurrent clips (no keyframes) (#11) |
| 45 | `db607c72c22eb8a13c747cdd14fb00259dc82485` | 2026-09-24 | Alicia | Configure Designer defaults for DeepSeek and MiniMax |
| 46 | `e1464d004d70470450860b9739bba0f6ca4d4bfb` | 2026-09-24 | Haowei_Du | fix（skill pack)：avoid repeating skill pack distillation |
| 47 | `3d3b77e02a312aa0d5e020868b7dd81240afc277` | 2026-09-25 | huanghongyun | [develop] fix(web): complete marketplace regression fixes |
| 48 | `55ebc92fff6bdc500d0f9aac686a750bd5a5544e` | 2026-09-25 | Alicia | Highlight adjacent edges when selecting canvas nodes (#26) |
| 49 | `a7a4d80b887f608411efa737dd2221727f4bce35` | 2026-09-25 | Henry TIAN | Keep canvas node activity, uploaded stills, and the BGM placeholder on the latest design (#17) |
| 50 | `4c279f546a30f562671bd4f80b04665cdff7206e` | 2026-09-27 | Huang, Zeyu | Move designer clip helpers from experiments into the core pipeline. (redo #48) (#51) |
| 51 | `9c27c0a6e78a96a458ff7b27849cf8ee93f1bed2` | 2026-09-27 | Henry TIAN | Let canvas edges drive clip inputs and keep node agents on the saved graph (#40) |
| 52 | `d28b7ecf497adb1484e10491b74ba1577c9934ee` | 2026-09-27 | BarakeelFanseuKamhoua | Use one designer vocabulary for scene, shot, and specs (#46) |
| 53 | `f518b0a0d01bc54b46382978cdbc8bb230624824` | 2026-09-27 | Huang, Zeyu | enforce r2v mode and models with omnipotent task type (#37) |
| 54 | `fb0fbc807371241e07e0e6786de301610a8e8799` | 2026-09-27 | Henry TIAN | Unify predecessor outputs and stop inline uploads on regenerate. (#45) |
| 55 | `499175e2bbc9a2f619aa07c1f71ae8c907109f72` | 2026-09-28 | Huang, Zeyu | Design director merged manager supervisor 1.0 (correct way to do #49) (#52) |
| 56 | `8cf6f84500f30dbebff1c04f6f66d5dcef055c4f` | 2026-09-28 | Henry TIAN | Design0928b (#59) |
| 57 | `ebb7caaa4bf0de1c0b9b2346cfa6ffd067c9aabf` | 2026-09-28 | Huang, Zeyu | Remove Design heuristic LLM fallbacks; fail closed with shared DesignerLlmError (#43) |
| 58 | `05a0eefff62071da7fa959c016db0c422dc4f88c` | 2026-09-29 | BarakeelFanseuKamhoua | Let the user or director choose video resolution (#70) |
| 59 | `08394ba3889e06190047dee646d7ae8898ffa293` | 2026-09-29 | Huang, Zeyu | Remove generate button from selected designer edges (#78) |
| 60 | `1ce69bdf8eded00996cd10773ef2f795b7255c99` | 2026-09-29 | Henry TIAN | Storyline climax, empty scene optimization, style specification (#72) |
| 61 | `32ec579bcae0a3962a691634ea145b18924ee957` | 2026-09-29 | Henry TIAN | Design0929a: Enhance the ability of better shot splitting (#69) |
| 62 | `56405935fe1363a61e38067f67341c63fe99e294` | 2026-09-29 | Huang, Zeyu | Remove designer optimization and rerun controls (#68) |
| 63 | `868ab8527ae88af97eb2b27d6745bc2de183154f` | 2026-09-29 | Huang, Zeyu | Designer: import ComfyUI vLLM-Omni workflows as canvas nodes (#60) |
| 64 | `93bea7099a68112ace808d41edef4c8c5c1efe8f` | 2026-09-29 | Huang, Zeyu | Enrich sparse video requests with story and script plans (#58) |
| 65 | `d54a53a9b2a137e0d22e3a92c84609ae9bb6e4fd` | 2026-09-29 | Huang, Zeyu | Design: run image/video generation on 0.2.7's generation settings, add DashScope and vLLM-Omni (#74) |
| 66 | `d74ff8c62ddf5fb4614ee04082ab2ba466e9daca` | 2026-09-29 | Haowei_Du | fix（skill pack): modify config |
| 67 | `14844152bf38a80dedadf2328cf8554993bed00f` | 2026-09-30 | Huang, Zeyu | Move Designer trajectory to data dir as per-project OTLP and design J… (#80) |
| 68 | `7d4331fcc2e34ed8f85d095684ba02a1f52d16d3` | 2026-09-30 | Huang, Zeyu | Cherry-pick: Synchronize outline after chat-based storyboard edits (#20) into design-0.2.7 (#83) |
| 69 | `8f03ec4cc908ccfbd38112392400233c3978ac8e` | 2026-09-30 | Huang, Zeyu | Delete jiuwenswarm/channels/web/frontend/vite.config.js (#98) |
| 70 | `ab59e70c05511a3d727a63d4a6e911c30bfd18c7` | 2026-09-30 | Huang, Zeyu | remove docs (#93) |
| 71 | `cea72ec4c57ab799618f5a8ed11790de9efae7a2` | 2026-09-30 | Henry TIAN | Improved the consistency between the brief and the storyboard by modifying only the prompt strategy. (#96) |
| 72 | `e0c68ad8ae2adb9e780d7338f7d6b7cc03b74be8` | 2026-09-30 | Henry TIAN | Remove limitation of shots (#97) |
| 73 | `fc83f4ed0fb8915151e74d8925901b421c310961` | 2026-09-30 | Henry TIAN | Give every on-screen person an identity card, including unnamed groups. (#99) |
| 74 | `e21ad616b37636e0c9f30d70ca2c1dfca7e75913` | 2026-10-06 | BarakeelFanseuKamhoua | ﻿Ship Designer quality.v4: spatial master plate, audible audio mux, anti-clone I2V. |
| 75 | `2a98fb0c15cbb3b70cadd13f1dd4167fc57e271e` | 2026-10-12 | Huang, Zeyu | ui(designer): unify dnd knot & buttons for edge control (#11) |
| 76 | `f3d9f95de8cbf007bbfcada5f1da84da34725855` | 2026-10-12 | Huang, Zeyu | Route designer canvas chat through the Director (#3) |
| 77 | `04f477fb34f44f7352bae243486c9eb33a076340` | 2026-10-13 | BarakeelFanseuKamhoua | designer: pass agent/tool prompts as-is; tool limits become advisory (#14) |
| 78 | `067373b9560520d3095fddfc3c3fd6eb722a7047` | 2026-10-13 | Huang, Zeyu | designer: canvas-wide video/image settings with model-aware toolbar controls (#9) |
| 79 | `cb7d6ffae33e1463b7e96937b695b29758fe6b31` | 2026-10-13 | Henry TIAN | Design canvas layout label (#19) |
| 80 | `ddf4844b8cfda19efc0b28b6354c8da90883d407` | 2026-10-13 | Henry TIAN | Design&work alignment (#12) |
| 81 | `ea3aadbb80ff96392bef1de2b032dd069fdd6fcc` | 2026-10-13 | Henry TIAN | Design canvas toolbar zoom (#18) |
| 82 | `1012b70a03822ff56b0f6aa4532987240718935c` | 2026-10-14 | Huang, Zeyu | Merge pull request #20 from AI-Framework-leibniz/cursor/design-landing-composer-guards-53a9 |
| 83 | `212142b540e5e00bb02f43605afee20fd87c60ab` | 2026-10-14 | Henry TIAN | Design pr17 port menus (#22) |
| 84 | `263b5a00992e1d03dbc465834f79c35024b31a06` | 2026-10-14 | Huang, Zeyu | Pre-merge cleanup of non-design files before merging Design into develop (#21) |
| 85 | `5d0fdb9d5991e27533eb730f0fa151986352fc9d` | 2026-10-14 | Huang, Zeyu | chat panel basic UI change |
| 86 | `9e8a8d05084e2cf31dd4b302b65ed1a43e00cb68` | 2026-10-14 | Huang, Zeyu | ui(design): Icons, menus and placeholder text adjustment (#23) |
| 87 | `e33fe26c4fad6961590b773bee142ceb22eaecff` | 2026-10-14 | Huang, Zeyu | designer: move error toast style out of App.css as .designer-error-toast |
| 88 | `17cad598a906e766f4c1832f62a91db9a8411053` | 2026-10-15 | Huang, Zeyu | resolve further conflicts after the merge |
| 89 | `70ea3bdf629a2714f4ef9adc696e8917ea6266d7` | 2026-10-15 | hw_liuyu | 隐藏Process CLI 非交互式命令 --output |
| 90 | `884b60190939a872bd04c76657ba5ce1d103ddc6` | 2026-10-15 | Huang, Zeyu | design: run swarmflow workers under the user's session (#37) |
| 91 | `b129e1ff164d9c849bc05d57a426a1548f42e8c5` | 2026-10-15 | Huang, Zeyu | Adopt SwarmFlow integration to the latest codebase |
| 92 | `f3266b6059683ebeecb567992b8b20a2c8983e42` | 2026-10-15 | Huang, Zeyu | resolve static lint error |
| 93 | `aadef421e921f29d943e81f94168778e980060dc` | 2026-10-19 | yuhaiquan | dev_1016 |
| 94 | `63c60e59f76ac8ecc6ec04ac33604e63e53480b5` | 2026-10-23 | maxmadmax | refector(permission): 智能审批快速放行内部工具 |
| 95 | `ddf47f6dc70faaacba92e76fb1d72d5f6ace86bc` | 2026-10-24 | openJiuwen-bot | [ai-fix] Fix #5724: 集群模式 TeamLeader 调用 `ask_user` 后，交互请求从未下发到 web 前端：工具真实执行并进入「等 |
| 96 | `77a9c4784bf99faa815df6317feaa88e274da31c` | 2026-10-26 | Haowei_Du | refactor（topic recommendation and chips recommendation)主动推荐运行时迁到 AgentServer |
| 97 | `4f0348f3e05af891897b405bc7cfc493e87f22f9` | 2026-10-28 | accident2ly | [mirror] openJiuwen-ai/jiuwenswarm#9652: ui(design): 设计模式跟进 UCD 设计稿件的样式细节和切图素材 |
| 98 | `0544970381065bf253ddb8909fb4c6a1c68a0483` | 2026-10-30 | chen-hui-zhe-xi | cherry-pick-fixes |
| 99 | `388089f4161ed445c062d7684c1f8eb93000012b` | 2026-10-31 | openJiuwen-bot | [mirror] openJiuwen-ai/jiuwenswarm#7347: [develop] feat: support MCP Apps (io.modelcontextprotocol/ui) in web and desk... |
| 100 | `8827ad7b5eddd1f37f0f8a3156150599df7f2099` | 2026-11-02 | Haowei_Du | fix（topic recommendation and chips recommendation) 控制话题推荐token消耗 |

# openjiuwen / agent-core — 完整提交清单

- 仓库：https://gitcode.com/openJiuwen/agent-core
- 基线提交：`a889d9fcc05a4cd45a81776012d3c6b7f414e5ee`
- 目标提交：`5a35c39f0163a4e6190800df53ad7e24848756eb`
- 提交总数：**1572**（不含合并提交）

## 类别统计

| 类别 | 提交数 | 占比 |
| --- | --- | --- |
| 新功能 | 331 | 21.1% |
| 缺陷修复 | 811 | 51.6% |
| 优化改进 | 33 | 2.1% |
| 重构 | 61 | 3.9% |
| 文档 | 167 | 10.6% |
| 测试 | 123 | 7.8% |
| 工程/依赖/构建 | 37 | 2.4% |
| 回滚 | 4 | 0.3% |
| 其他 | 5 | 0.3% |

> 提交按类别分组、组内按时间正序排列。`commit` 列为完整 40 位 SHA，可 `git show <sha>` 查看详情。

## 新功能（331 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `07caab56dc9dd8b34a9422ac0ed2115157dc7007` | 2026-05-20 | alan_cheng | feat(agent_teams): wire external-cli runtime into spawn/configurator (P2) |
| 2 | `3b99cecf427146b88e167e8b96f314e3027fd34b` | 2026-05-20 | alan_cheng | feat(agent_teams): add external-cli runtime building blocks (P2) |
| 3 | `57fe67e87b1b9a3af157ba8c64eb162455917237` | 2026-05-20 | alan_cheng | feat(agent_teams): add external-agent join surface (skill+cli / mcp) |
| 4 | `6bbcd8a61cd15d5ffa434ccbc6225db9c7a8918c` | 2026-05-20 | alan_cheng | feat(agent_teams): add bridge agent role for external agent integration |
| 5 | `917ef6fc0ca7e34ee1b0c7567369ef87606c79f3` | 2026-05-20 | alan_cheng | feat(agent_teams): flesh out claude/codex/openclaw/hermes cli adapters |
| 6 | `a4c682ed9e450b4f1791d94db8dd9532a338bd84` | 2026-05-20 | alan_cheng | feat(agent_teams): add re-invoke-per-turn runtime for one-shot cli agents |
| 7 | `d8c4c2ffe6aa327fbfa45fa23b783ebdd3b68b3a` | 2026-05-20 | alan_cheng | feat(agent_teams): add external-cli member spawn entry (P2) |
| 8 | `bc52f4b2df630f3e15ff77229b90c457f0e5f352` | 2026-05-25 | alan_cheng | feat(agent_teams): external-cli spawn_member role + MCP auto-injection |
| 9 | `23a46b28df8383354abd4407c4ed4a0f650275a7` | 2026-05-27 | alan_cheng | feat(swarm): extend agent time rendering to remaining surfaces |
| 10 | `45497306bd693347c0915eff58859e86d6e47ddc` | 2026-05-27 | alan_cheng | feat(swarm): render absolute+relative time in agent-facing text |
| 11 | `63e006fe08b9c4e235111f8b6e2164d028515a3d` | 2026-05-27 | alan_cheng | feat(swarm): harden external CLI members and add gemini support |
| 12 | `8ad91e0c7de73cc900a6a88414339bbcd7de3c79` | 2026-05-27 | alan_cheng | feat(swarm): split external access into operator / member scopes |
| 13 | `88fbe1d108cb65772238d0f7714e59ba0f9296d4` | 2026-05-28 | alan_cheng | feat(harness): add SuperHarness for concurrent-safe DeepAgent interaction |
| 14 | `d1324dd93a020a1323ccc376576d3f4d8f3fc143` | 2026-06-01 | alan_cheng | feat(swarm): split spawn_member into four role-specific tools |
| 15 | `4e3793ec7ff413b5ce0dd1b36d9779fb8168893e` | 2026-06-02 | alan_cheng | feat(swarm): expose NativeHarness lifecycle events and interrupt resume |
| 16 | `d1b54a7303a36e0b1f8080ebc5ec56a424c38eec` | 2026-06-02 | alan_cheng | feat(swarm): drive NativeHarness over the full DeepAgent task loop |
| 17 | `f60043911a353b9676e7b9606870b1a3adc3c3cf` | 2026-06-02 | alan_cheng | feat(swarm): add swarmflow workflow orchestration |
| 18 | `4107e688c263fb5c9b48095a74ce2f09e38df7b8` | 2026-06-03 | alan_cheng | feat(swarm): leader self-monitor + result-aware repeat detection |
| 19 | `588be7cee4e8fedd48c44f5a46590237d1981285` | 2026-06-03 | alan_cheng | feat(swarm): adopt TeamHarness/StreamController onto NativeHarness surface |
| 20 | `8d1ebafbf705db75c444e14cb199a053f046a95c` | 2026-06-03 | alan_cheng | feat(session): add vcs package for context/state version control |
| 21 | `8ee829f809764c5c1e0579b66e7b6f95f14f9efb` | 2026-06-03 | alan_cheng | feat(agent_teams): thread optional BuildContext through spec build |
| 22 | `b3f880e227a77003bc6b74e38d2d1233bc6bf2ba` | 2026-06-03 | alan_cheng | feat(swarm): add reliability framework for member health monitoring |
| 23 | `ba64b4ee9f962b2258bdda66e56e48574d6cb0a2` | 2026-06-03 | alan_cheng | feat(swarm): route per-call agent(model=) to team model pool |
| 24 | `d795d9df287f403baabb7620475a470b1c45958d` | 2026-06-03 | alan_cheng | feat(agent_teams): rebuild build context from a serializable seed across spawn |
| 25 | `e63c0c37057fa3a52efb47f94f4c3da0d1f46fa0` | 2026-06-03 | alan_cheng | feat(agent_teams): publish parent model on build context for sub-agent providers |
| 26 | `323043afc198ecced9cf82709b1d64d4e39a95ff` | 2026-06-04 | alan_cheng | feat(agent_teams): add core web_paid_search / vision / audio tool elements |
| 27 | `34c2ed52db852cb9c3af84be40bd67e5a16759e2` | 2026-06-04 | alan_cheng | feat(agent_teams): add serializable harness-element manifest framework under harness/manifest |
| 28 | `cb61e0c28b07a37e67384115d517444ddc8db4e3` | 2026-06-04 | alan_cheng | feat(agent_teams): own generic harness elements under the core.* namespace |
| 29 | `fc4d7a4b62913884132e12b78293274f2237b9c3` | 2026-06-04 | alan_cheng | feat(swarm): unify DeepAgent assembly to declarative manifest |
| 30 | `4acee5b96d1e18e52d7ee33b93eabb898a619ff8` | 2026-06-05 | alan_cheng | feat(swarm): add native-harness async-tool framework and port swarmflow |
| 31 | `53c31a65372cd7ed89ca7e3a052e7939847fdccb` | 2026-06-05 | alan_cheng | feat(swarm): run swarmflow workers as teammate-equivalent TeamHarness |
| 32 | `ed885a0048076a8591f731bd3989077d86793c05` | 2026-06-05 | alan_cheng | feat(swarm): simplify native harness surface and gate first-start message |
| 33 | `6e5c4b3f36583f1b571114d388f22034202a7fc1` | 2026-06-06 | alan_cheng | feat(harness): add async aiohttp-based web tools package |
| 34 | `abf295a469c5318f66105464d1f805e669536c40` | 2026-06-06 | chenchunzhou | feat(swarmflow): add prompt/model/phases fields and WORKFLOW_FAILED/AGENT_FAILED kinds |
| 35 | `768dfa6c0d0612b72702b90ebaa13d0064f0209a` | 2026-06-07 | min_gitcode | feat(harness): add skill snapshot signature for incremental refresh in before_model_call |
| 36 | `7f78244bc3743bcac81322b8d98a088162dda839` | 2026-06-07 | chenchunzhou | feat(swarmflow): Add agent error propagation and fix build_context missing for worker |
| 37 | `49eccea5cfacfce6e0b805096466eb55ee8a2e01` | 2026-06-08 | alan_cheng | feat(swarm): deterministic correlation id for human turns |
| 38 | `4eccf9ee99daced398525bec8e941921d8843cd3` | 2026-06-08 | chenchunzhou | feat(swarmflow): typed PhasePlan, text-based outcome, worker workspace fix, structured-output enforcement |
| 39 | `985f6541b9d109279d66149c6a2b4a0f5362c48f` | 2026-06-08 | alan_cheng | feat(swarm): wire swarmflow human sessions end-to-end |
| 40 | `ea431f6f2fae247180fa0ed194d19097621f267e` | 2026-06-08 | alan_cheng | feat(swarm): add stateful agent/human sessions to swarmflow |
| 41 | `389ed6a3a85a26de55fddb47cb5c04fa7a82b7f6` | 2026-06-09 | alan_cheng | feat(swarm): namespace web search / fetch tools by member card id |
| 42 | `26aab8282099d8b824687ff8883600cdf7e18016` | 2026-06-10 | qhily9991 | feat(runner): MessageQueuePulsar / MessageQueueBase enhancement: retry with fallback, graceful stop drain, and unsubscribe_all |
| 43 | `2d56a55849124aa2cc527fe47dc26ea5a0260cce` | 2026-06-11 | chenchunzhou | feat(swarmflow): include run_id and description in workflow output |
| 44 | `fc84d3b059b8a2f1a438d569f8305d9f70bcc228` | 2026-06-12 | openJiuwen-bot | feat(auto-harness): harden issue fix foundation |
| 45 | `f571c23bc6250da115e7e7ec54c42b861220451f` | 2026-06-13 | xinyu-jiuwen | feat(evolving): add and generalize evolution subject services |
| 46 | `f639c69e6353a13ccd51ce8aa65615f48fb88624` | 2026-06-13 | zhanglongmin | feat(agent_teams): add team permission system with leader-mediated ASK resolution |
| 47 | `8b2a384ee4b283a56fe1961eb7d0c5f3f33ee520` | 2026-06-15 | hwxiajing | feat(observability): agent team observability |
| 48 | `1b2931f7f50dc0ffb4ce3c905243d065cec5746e` | 2026-06-16 | alan_cheng | feat(swarm): persist swarmflow resume journal under team sessions dir |
| 49 | `604009311b131edea1c58f39c3f3c1356f56a513` | 2026-06-16 | alan_cheng | feat(swarm): harden swarmflow worker turns, nesting, and teardown |
| 50 | `7a64c4273d5a44570378b806e6604790be3d0f6d` | 2026-06-16 | openJiuwen-bot | feat(retrieval): add ontology based triple extractor |
| 51 | `8b31fa887a6b3d8bd8f2178cb427d6ee1ae4454c` | 2026-06-16 | openJiuwen-bot | feat(retrieval): Add validation to triple extractor |
| 52 | `f5cc7c8e1da8d11427c7ed478d8f1304f7cdc72e` | 2026-06-16 | JustinBieber1223 | feat(intelliRouter): add IntelliRouter advanced usage doc (zh/en) |
| 53 | `5e0aff4b3d4ddf276eaa34b763dc89aebe23381d` | 2026-06-17 | zhanglongmin | feat(agent_teams): add permission tightening guidance in base permission description |
| 54 | `9918631e9f444c4c081db7a8a322b21c9f16c34d` | 2026-06-17 | alan_cheng | feat(swarm): journal program-order, crash-durable WAL, atomic async I/O |
| 55 | `cd29fa5eb7a5b88a9e97eb855f3ca41bbc8b7739` | 2026-06-17 | ubaidillah-ariq | feat(skill_tool): extend skill use for multimodal skill |
| 56 | `e9af77f736971d1c87cfce3a3488c8538c7a8701` | 2026-06-17 | openJiuwen-bot | feat(team):align team teammate worktree isolation with AgentTool flow |
| 57 | `1844d907ac53cdaef487d1b86f74971ac6cfdb31` | 2026-06-22 | alan_cheng | feat(swarm): unified id generator + async control tools + disk spill |
| 58 | `7ed6d88bfb7c8e34704e10cd0895c021c0e48db8` | 2026-06-22 | zhaoyulu1 | feat(trace): tracer support plugin handler |
| 59 | `b056a098f658a8355cbe6ca5f257a96e3ec332a4` | 2026-06-22 | alan_cheng | feat(swarm): align swarmflow tool with Claude Code surface |
| 60 | `2c1a5e6130cd7fa4726e176c2086c8ed61447ded` | 2026-06-23 | openJiuwen-bot | feat(team):support swarmflow worktree isolation |
| 61 | `3f995df099661aa20b97f29e6ea157ae2db88bda` | 2026-06-23 | JustinBieber1223 | feat: Adapt to Intelli Router to support observability |
| 62 | `5ece8f703c11393d9b09e9acaa3d1a7d256df6e7` | 2026-06-23 | kwangyyinc | feat(browser): add parallel browser isolation with headless/headed mode support |
| 63 | `7f37bfed0b8bb74122969a728f58ec51f7be6432` | 2026-06-23 | openJiuwen-bot | feat(llm): create anthropic model client |
| 64 | `ba174b5ce93237039331dc25e72e43bb63d51be3` | 2026-06-23 | alan_cheng | feat(swarm): swarmflow pause/resume via background task controller |
| 65 | `db9378ae9d43223d2ffe0cb61b5ed6e6743645d8` | 2026-06-23 | ShowMeYourCodePrompt | feat(memory): add lakebase memory provider |
| 66 | `e956586a8f91b27672972387b2c4e86cd28e0024` | 2026-06-23 | alan_cheng | feat(swarm): stop writing swarmflow workers to team DB; drop team_backend |
| 67 | `f7e2f0441132d271c8ed7db3a9b600b3335df0cb` | 2026-06-23 | zhaoyulu1 | feat(tracer): tracer otel handler implementation |
| 68 | `bbf6cb313832e9c96626d97d66f58b4376715735` | 2026-06-25 | hwxiajing | feat(agent_teams): MemberInfo add role field: human_agent/teammate/leader |
| 69 | `e110aeb63585ae7df15265c1334b9017cd3cf390` | 2026-06-26 | bhyeo | feat(context_evolver): include quickstart rail with cognition |
| 70 | `8a9e27e82d11d70d0ca317127ab893c45efa0187` | 2026-06-29 | Haowei_Du | feat(harness): Security protection external directory check |
| 71 | `998f5caa279b25c7f77f18882a3685d29216b540` | 2026-06-29 | hwxiajing | feat(observability): add file exporter writing standard OTLP JSON |
| 72 | `d41b38c3269a9407cd8a6c9fa6728a7c9b71a6f2` | 2026-06-29 | chenchunzhou | feat(agent_teams): add Swarmflow ConcurrencyGovernor with multi-run run_id isolation |
| 73 | `12ae3465580389ce8289c379df10c40edb11036f` | 2026-07-01 | openJiuwen-bot | feat(memory): add lakebase memory provider api doc |
| 74 | `1fa11ea27756d1513cb60aaa5e3334601696a2c9` | 2026-07-01 | alan_cheng | feat(swarm): route leader between swarmflow and build_team by task nature |
| 75 | `5dd052a67c7ed6f6fbe59ad76143dca18a996dad` | 2026-07-01 | alan_cheng | feat(swarm): refine swarmflow routing with deliverable signals and restraint |
| 76 | `f53d4c309ea5d35ce09ab50d63c07533b1052371` | 2026-07-01 | alan_cheng | feat(swarm): wire inline script execution for swarmflow tool |
| 77 | `0884e3ade73807d8b5f39b04912f0a0e562c120a` | 2026-07-03 | alan_cheng | feat(swarm): split HITT contract/roster and finish md externalization |
| 78 | `37d6162b61071f52888600830c65d486ed87044a` | 2026-07-03 | alan_cheng | feat(swarm): unify member desc/prompt split and MemberSpecBase |
| 79 | `7cce389aaaff3178c01e306edb00cd3ba754410a` | 2026-07-06 | maxiutomoq | [mirror] openJiuwen-ai/agent-core#14: feat(browser): add batch form filling and dedicated browser logging |
| 80 | `8f5d06177e33ab445a4bfc145b4b3c8cf9ad433e` | 2026-07-06 | alan_cheng | feat(swarm): unify external CLI inbound rendering to XML and relocate tag notices |
| 81 | `917174129895a6c2c49caa69c9c8e6ef30b25c34` | 2026-07-06 | alan_cheng | feat(swarm): unify member roster and make team sections static |
| 82 | `990f20d6ff12f666c6bb673a66e66881e90ed85e` | 2026-07-06 | alan_cheng | feat(swarm): surface resolved script_path in swarmflow launched receipt |
| 83 | `ecb7e5ce11ecdf3b860d9cb02dfa09eda5d6db52` | 2026-07-06 | mingw69 | [mirror] openJiuwen-ai/agent-core#15: feat(browser,context): window tool results; wire ToolResultWindowProcessor wi... |
| 84 | `3c4bf42be57bbea0f917e77408f68498695df873` | 2026-07-07 | alan_cheng | feat(swarm): make stale-task nudge self-only, minimal, and appended |
| 85 | `5c0700265b2f95dad51d63752c2a4c01979e130f` | 2026-07-07 | alan_cheng | feat(swarm): reassign via TASK_REVOKED event and cap one active claim per member |
| 86 | `b754ce6f9cd33777294ec769980ca56496987847` | 2026-07-07 | ubaidillah-ariq | feat(harness): add auto-detection of model input modalities to deep agent |
| 87 | `c86bb746ec8ed9e66a8de7d87ff01881750c2f0c` | 2026-07-07 | alan_cheng | feat(swarm): wake teammate only on claimable growth; emit TASK_RELEASED |
| 88 | `4bca8acc1685b81cb33e8f20c5704c21fc515461` | 2026-07-08 | dreamofapsychiccat | feat(llm): Add explicit caching for openrouter's anthropic & qwen models |
| 89 | `72550d6eca0f15d63b9349e7882a6608a232d59a` | 2026-07-08 | alan_cheng | feat(swarm): atomic create_task graph + narrow one-active-claim probe |
| 90 | `868f914a3d75496e153ea4cf99e9f874cc7a1bdd` | 2026-07-08 | alan_cheng | feat(swarm): rank swarmflow triggers by priority, force on keyword |
| 91 | `a1061fbe848607deca31c5388e0a26cae7f59a26` | 2026-07-08 | alan_cheng | feat(swarm): atomic reassign and task-level cancel/edit signals replace member-wide cancel |
| 92 | `0faa39e0b030a69fa39712f592f7af9170e7c7a1` | 2026-07-09 | karina0522 | feat(openai-auth): add OpenAI account OAuth provider |
| 93 | `18bfdadfa2e6697fb27f54bcdff6552d363fe92f` | 2026-07-09 | alan_cheng | feat(swarm): add the verify gate — reviewer column and verify_task |
| 94 | `3e447dd22f33d0cd4e73bcfad6f27fc49724f6cb` | 2026-07-09 | Ziliang_Zhou | feat: support jiuwen_memory provider |
| 95 | `43de5fc06ca74cd89253d3f875812540f5cb07eb` | 2026-07-09 | alan_cheng | feat(swarm): condition-named task state machine and dispatch-mode tool variants |
| 96 | `72d0c23b2aee65272015f2cd96abbd981d66bbff` | 2026-07-09 | alan_cheng | feat(swarm): cold-resume a paused round across stop/start |
| 97 | `f448159d99e5099df4bd944867b010d6c14d0215` | 2026-07-09 | alan_cheng | feat(swarm): pause/abort at inner react iteration boundaries, add resume |
| 98 | `fd5c5c33e11aeb4f24883154ac34a3bed5d086fe` | 2026-07-10 | alan_cheng | feat(swarm): scheduled dispatch runtime with review voting |
| 99 | `b352111106a1600a58aabfdad892d1a33c2ba78a` | 2026-07-13 | zhanglongmin | feat(agent_teams): use Claude Agent SDK backend |
| 100 | `5995372878744a3f4b63961637248325f601156d` | 2026-07-14 | zhanglongmin | feat(agent_teams): resume claude external backend sessions |
| 101 | `6262ffc4b3986fc85f839478e6652f6be62fa1e2` | 2026-07-14 | alan_cheng | feat(swarm): render scheduler handoffs from templates at delivery time |
| 102 | `ffd71d7efaf5043b0789a5ef51bdea4eb0d0c793` | 2026-07-14 | openJiuwen-bot | feat(mcp): add runtime team broadcast delivery |
| 103 | `c3c9c0480f42fb3d9d9739a95badcfb910e2f355` | 2026-07-15 | alan_cheng | feat(monitor): forward new task lifecycle events to team channels |
| 104 | `e6b8781904db4246750582f36d252f5597bbd21f` | 2026-07-16 | alan_cheng | feat(swarm): route messages by content shape and cap send_message content |
| 105 | `fe00baa57c915ccbe42ca2217d0a9b5561f0a8c5` | 2026-07-16 | alan_cheng | feat(swarm): detect member stalls from a runtime idle clock |
| 106 | `07b09b39bbdb4b068c988386b7abdc45384ca5b3` | 2026-07-17 | alan_cheng | feat(swarm): enforce swarmflow token budget from real model usage |
| 107 | `8667f59e08648e552c82c5059bfe2c865f0b5bd2` | 2026-07-17 | zhangyaomaggie | feat(harness): adding spec-driven deepagent |
| 108 | `a6d987578a681a9213db03a19ebd1b2031dc77c3` | 2026-07-17 | alan_cheng | feat(swarm): add intelli_router model allocation strategy |
| 109 | `bdd801d019c7152ef2caaab99074df6c8276dfb0` | 2026-07-17 | zhanglongmin | feat(agent_teams): sync external member team context |
| 110 | `bb2ca969b91f4196b675d4b09856f976e90a69c4` | 2026-07-18 | zhanglongmin | feat(agent_teams): resolve external claude cwd |
| 111 | `3ae4fb99366c599aa6af808206c7498682042a4c` | 2026-07-20 | zhanglongmin | feat(agent_teams): expose workspace meta to external members |
| 112 | `67822fa1bf140aaad41b54a887adfa7fd029afc4` | 2026-07-20 | wncbdj | feat(harness): Session-Scoped Supports Goal Execution |
| 113 | `d89d62c5875d14e0bf73abcc6805e4516c506a4a` | 2026-07-20 | zhanglongmin | feat(agent_teams): align external teammate workspace context |
| 114 | `765638e06efcb13b601a334242b5b5a845c32af1` | 2026-07-22 | chenchunzhou | feat(swarmflow): SwarmFlow human-in-the-loop and run-scoped reply routing |
| 115 | `e9aabef957248e5e35d9022642f8632deef687e9` | 2026-07-23 | 2401_87256159 | feat(agent-teams): integrate Codex Python SDK backend |
| 116 | `6c4ce4519b6e08fba3bfcefef225697847d72b77` | 2026-07-25 | openJiuwen-bot | feat(agent-teams): publish hybrid events via team.mq.publish |
| 117 | `4e47ec908942de35a79376060e7ec65f45d51512` | 2026-07-28 | alan_cheng | feat(swarm): cover assigned-but-unstarted tasks in autonomous nudging |
| 118 | `71f4bcd302687e0483a066ad0985c8e2c7f0c813` | 2026-07-28 | alan_cheng | feat(swarm): separate cwd, workspace and project root |
| 119 | `de725c7e97c7ad31e4e49d4756428b04e382398e` | 2026-07-28 | l_x_d | feat(context): add asynchronous session memory processor |
| 120 | `e657bbc2014d37783f20679620f3d62069598167` | 2026-07-28 | alan_cheng | feat(swarm): move member identity out of the shared prompt prefix |
| 121 | `344164fc73e5bb6a5a97a99d2dca0cc8978e49d3` | 2026-07-29 | yyuse | feat: Backport the security guardrail features from the enterprise branch to the develop branch |
| 122 | `7b4b2c5d27a01428658f9dd6e4924f1014945d2f` | 2026-07-29 | min_gitcode | feat(agent_teams): normalize external tool call chunks |
| 123 | `7f3c00fcd456f6cf3819911fd8d5372ade4ca3fd` | 2026-07-29 | alan_cheng | feat(swarm): deliver team context into member conversations |
| 124 | `91f4077bfb4bdcb79774e8a73a5048445f4dc960` | 2026-07-29 | lin-xiaoyu | feat(symphony): Add new Symphony module and README.md |
| 125 | `93156c94c7995906e2b4b41c6617a3a0ea07352c` | 2026-07-29 | openJiuwen-bot | feat(examples): add adaptive multi-agent collaboration example |
| 126 | `97408550ce77c2241e47b2783520aa0969752050` | 2026-07-29 | alan_cheng | feat(core): add ON_USER_MESSAGE rail hook for consumed inputs |
| 127 | `c08c4b814c1b27e31d751ae7a56e4d8452a2ceeb` | 2026-07-29 | min_gitcode | feat(agent_teams): preserve claude tool result names |
| 128 | `db7f1410267f78d9ae6154e750f880a4a5cd7123` | 2026-07-29 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#152: feat(example):merge perstream |
| 129 | `06378f9c4144729e706f99b51e5a285ecb79cb76` | 2026-07-30 | wncbdj | feat(session): Add session_id field to tracer |
| 130 | `4529331a802d041dd9f189e68babb28aad612f0f` | 2026-07-30 | openJiuwen-bot | feat(agent-teams): Reproducibility test case supplement |
| 131 | `86ebe7990563ca5d4fc43964c3a9d5ff733a8ad7` | 2026-07-30 | min_gitcode | feat(agent_teams): use in-process MCP for Claude |
| 132 | `fe77ae972525ad70261136cae5bee93a15f54849` | 2026-07-30 | renquanjia | feat(memory): auto-load today/yesterday daily_memory into system prom… |
| 133 | `405d03240f75e3f7efe549455ab29b3131e43c85` | 2026-07-31 | openJiuwen-bot | feat(tiny-agent): make SecurityRail opt-in |
| 134 | `5a21aa95309957b46e566bcb81172b48011459a7` | 2026-07-31 | 2401_87256159 | feat(agent-teams): add Codex OpenTelemetry observability |
| 135 | `916517621f3b17c29388f3ced29f0e474a5a108b` | 2026-07-31 | alan_cheng | feat(swarm): nest team notes inside the block they annotate |
| 136 | `d1e8a3748547f64744d3c120712146eef5e0e95b` | 2026-07-31 | alan_cheng | feat(swarm): hand rails the input batch and drop superseded boards |
| 137 | `1208ed1d6f6ebcd41671690195258e9a8e11e6b7` | 2026-08-01 | openJiuwen-bot | feat: change reviewer startup logic to use temporary harness, and update prompts accordingly (scheduler mode only) |
| 138 | `2907c9c0c273d75e8b5ab2f36cbdad2f46f96b95` | 2026-08-01 | alan_cheng | feat(harness): time rail init through a single entry point |
| 139 | `c2a85aef3ddc658125a79a880c4cad3f12a9097f` | 2026-08-03 | liulili-huawei | feat(evolution):  JiuwenSwarm online RL optimize for AgentOS |
| 140 | `eefde52e0f57508c2441a3cf5c5390648e5bb672` | 2026-08-03 | zhangyaomaggie | feat(harness): support the creation, loading, and execution of templates and plugins |
| 141 | `f6d38283fc2e2c7df4b2f6858edaa2f7d383d350` | 2026-08-03 | zhanglongmin | feat(agent_teams): add claude otel bridge |
| 142 | `174297537653572c69996c309760895f02d04bad` | 2026-08-04 | AFSDFASD | feat(rsi): add recursive self-improvement framework |
| 143 | `6e8bb1b050ae86e85d9a179868bb21ea307e12dd` | 2026-08-04 | lin-xiaoyu | feat(symphony): add symphony module |
| 144 | `b2ff486e40314de9648bdd686a699b3a030efcee` | 2026-08-04 | openJiuwen-bot | feat(team): JSON string tool parameter missing |
| 145 | `1f9c77a1b37e32a017e3650c20114a7537b1f6d5` | 2026-08-05 | Haowei_Du | feat(symphony): add experience module for skill retrieval |
| 146 | `433f363ca4d9055a5c6946151bfb4f3ac2f0cc9b` | 2026-08-05 | layShang264 | feat(symphony): add capability fingerprint evaluation |
| 147 | `933e09476c11b56936aeb21f15effb67e94a36b0` | 2026-08-05 | chenchunzhou | feat(swarmflow): add budget/tokens and nested workflow progress observability |
| 148 | `f3fa3c2400e900a5f4fcf4eaffd2ef2738022c5b` | 2026-08-05 | alan_cheng | feat(swarm): emit a debounced team-idle marker from the leader stream |
| 149 | `6e52cf49a61b8f1176851be3d4f9dd6c119b3494` | 2026-08-06 | xutuo | feat(symphony): symphony move to retrieval |
| 150 | `85a347f8f5f3fb5aa620cc1af5384169f1ebc56f` | 2026-08-06 | liulili-huawei | feat(rl): add online RL gateway client and validation guide |
| 151 | `a678b17a0094b6adbb782f5718f76f3cd5c9cb56` | 2026-08-06 | alan_cheng | feat(swarm): expose cli_agent on MemberInfo |
| 152 | `3f0a54fba56024e689ce26422d02a20fc23f0fdc` | 2026-08-07 | min_gitcode | feat(agent_teams): support ssh exec toggle |
| 153 | `5612254e66815d6587b7f7e97b3f4d1d8ac0cc4f` | 2026-08-07 | dunphy1 | feat(agent-teams): add fork with checkpoint and compaction |
| 154 | `9229d876229c7e2274cc18d0f7eda012e8a612e8` | 2026-08-07 | openJiuwen-bot | feat(team): rework & replan at review fail + add roles for reviewer |
| 155 | `85c652eebbc2875dee043719088daf123c9eede5` | 2026-08-08 | alan_cheng | feat(swarm): gate fork behind TeamAgentSpec.enable_fork |
| 156 | `d2f79a8e68cb6eab1a45ce4d69456f3ed0bd7247` | 2026-08-08 | alan_cheng | feat(swarm): disclose leader collaboration policy via build_team result |
| 157 | `eeb6a50c2b0de677f1a416ac1c01d32ece52d8d8` | 2026-08-08 | alan_cheng | feat(swarm): gate the verify gate on dispatch mode |
| 158 | `5aad9c9c8211a252af3ffde92cecf638866959ae` | 2026-08-10 | 2401_87256159 | feat(evolution): support reviewer feedback driven skill evolution |
| 159 | `7893a8954d2cd168943e97bc7c5086e04c061382` | 2026-08-10 | alan_cheng | feat(swarm): gate the team-idle marker on a settled task board |
| 160 | `93098c8caf5b29a20ccc207e4df91a9e9d295d3f` | 2026-08-10 | layShang264 | feat(symphony): add pairwise Skill equivalence grouping |
| 161 | `9a418c0a0dcec8a608bcc52b1b655e46a50298d3` | 2026-08-10 | alan_cheng | feat(swarm): gate build_team's verify flag on dispatch mode |
| 162 | `37ed410598107f7dc52977cd02ef6e99057cda52` | 2026-08-11 | z2tng | feat(observability): register trajectory span processor |
| 163 | `5322d5f9f9939fec84519408bf499908958c778f` | 2026-08-11 | alan_cheng | feat(swarm): cap non-leader steering batches at two |
| 164 | `5d0db42c7d4d82c3bda75694062a1a2231642079` | 2026-08-11 | alan_cheng | feat(swarm): let an inherited team reach build_team, refuse it on recovery |
| 165 | `d3faf65c7f4ac9f98d2e6d6931ad1109d5010260` | 2026-08-11 | dunphy1 | feat(swarm): persist named checkpoints and harden fork capture |
| 166 | `f921a6a125194998e24e1efedc7858d34251ccd6` | 2026-08-11 | alan_cheng | feat(core): let rails bound each steering drain |
| 167 | `8016d7ce11d52ecfebb5ac3a40bf9b8a05dffbe8` | 2026-08-12 | z2tng | feat(trajectory): unify evolution and RL span consumption |
| 168 | `818730b108ab0b6cd9ba9702647eab8774be71bd` | 2026-08-12 | sodawlh | feat(swarm): notify checkpoint creation via team event, add list_checkpoints |
| 169 | `86e6b455abe122c81a65165c03256ab85718c769` | 2026-08-12 | alan_cheng | feat(swarm): give teams their own Skill rail over one shared library |
| 170 | `1d901bf8706659425f801337335e374a74481bfc` | 2026-08-13 | sodawlh | feat(swarm): fork identity conversion — capability statement + converted_from |
| 171 | `71ec8e2224845226caa4d19846f5acdb7e6e515d` | 2026-08-13 | min_gitcode | feat(agent_teams): support external cli path configuration |
| 172 | `817508680c091e14b1269088271a7af759de0f7a` | 2026-08-17 | yyuse | feat(security): add permission modes (Full Access/Auto/Strict) |
| 173 | `af2ca0c87fd01a33cd5c9dac9bc9d0394604ecdb` | 2026-08-17 | alan_cheng | feat(swarm): reconcile member startup on the leader's round-idle edge |
| 174 | `d0bc457c32bfb1c8a36a1026afd522aa62af0861` | 2026-08-17 | alan_cheng | feat(swarm): start members from the task-creation path |
| 175 | `f59b8c4dda18642506ecdf44c4c29d4e851daffd` | 2026-08-17 | alan_cheng | feat(harness): add the single-agent observability subsystem |
| 176 | `47ab4ff309e30d1044afd8f692a3f0eb173153a2` | 2026-08-18 | zhaominjun1997 | feat(context_engine): integrate proactive context |
| 177 | `6698484bdf0fe900e5fe6e3b06a6e782a1e3888d` | 2026-08-18 | sodawlh | feat(swarm): replace compact with fork_mode, support five fork modes |
| 178 | `ca99e37c1c6d2e062aa0e60b6d009a9b51355af6` | 2026-08-18 | liulili-huawei | feat(agent-rl): add online rl and sft core flow |
| 179 | `cb80cea0b0e8ca67b5ff7d2072357f5aa2e689d0` | 2026-08-18 | wangxiaolong100 | feat(security): add permission modes for Full Access/Auto/Strict |
| 180 | `cbff7277b598a90c3ece2ad633c5a369404317ee` | 2026-08-18 | wncbdj | feat(llm): LLM module protocol optimization |
| 181 | `537985e2e7e46cb4a0814047883ba04f0a7b93f5` | 2026-08-20 | alan_cheng | feat(agent-teams): define external harness protocol |
| 182 | `86b859a28dce86c996e1f025a8d085da336893df` | 2026-08-20 | openJiuwen-bot | feat(agent-template): support flat manifest identity fields |
| 183 | `ffc0cbf485741d12b1175c555ad71b616718cb90` | 2026-08-20 | openJiuwen-bot | feat(harness): support AgentTemplate snapshot loading for team members |
| 184 | `35474e9e0123882c4c868221cdb832abd3b397d3` | 2026-08-21 | zhangyaomaggie | feat(harness): support the creation, loading, and execution of templates and plugins |
| 185 | `4e967583501d9f90237ebf189ac73b9f1b2933ba` | 2026-08-21 | cycong_1 | feat(subagent): add subagent_spawn/wait/list/send_input/close/resume tools |
| 186 | `68d7d832955144498851a61147a480fe2d05e535` | 2026-08-21 | alan_cheng | feat(agent-teams): integrate DeepSeek Harness SDK |
| 187 | `b0134c30d5a10cb157a110309c9c59f78f9af0b8` | 2026-08-21 | alan_cheng | feat(observability): add realtime single-agent trajectory spans |
| 188 | `4567ac1b29480e31bb3546b19dcde146ee410e5f` | 2026-08-24 | sodawlh | feat(swarmflow): add agent_session fork for swarmflow |
| 189 | `fd6c478542010576661806af55234aed796c59fb` | 2026-08-24 | hwxiajing | feat(agent_teams): spill message/task content into session files |
| 190 | `4ab22772a799be1aecbd648ebc52aa6f00bcd9ac` | 2026-08-25 | sodawlh | feat(swarm): add fork use-case examples and improve proactive fork usage |
| 191 | `7033f7c29d64469c75f7fb92d40cad021c2e6a22` | 2026-08-25 | AFSDFASD | feat(rsi): add evidence-driven recursive harness evolution framework |
| 192 | `7f54485a0c241296fba03c9720dc094cf6625ad7` | 2026-08-25 | hwxiajing | feat(agent_teams): re-announce evolved member_prompt on same-session resume |
| 193 | `839fe1f8e7a3595dcf44348ea29683961ed1c3fa` | 2026-08-25 | hwxiajing | feat(agent_teams): add member-directory linker |
| 194 | `98d3f63c0bd62122be2db28c3e57caeb045959a4` | 2026-08-25 | xutuo | feat(symphony): optimize the usage logic for integrating jiuwenswarm into symphony |
| 195 | `ca87a26799bccf79711c4465b2e8ec0a1f158467` | 2026-08-25 | hwxiajing | feat(agent_teams): add evolvable workspace (assembly/layout/cache/wiring + spec) |
| 196 | `01611cc508cb4a83ce441cc0138677cacb5ffe49` | 2026-08-26 | alan_cheng | feat(observability): emit canonical trajectory context events |
| 197 | `6ea9110441d02d38a3a35006889d9f96fc2a091c` | 2026-08-26 | layShang264 | feat(symphony): add atomic batch graph mutations |
| 198 | `20e35736b4fe3060662e507a7efdcb5490cdde8e` | 2026-08-27 | yyuse | feat(security): Decoupling of the permission engine and product permission model, directory restructuring, support for session-level persistence, and network protection. |
| 199 | `63746869f7ab0e34eebd8d8c2822e4a5f17b97a4` | 2026-08-28 | sodawlh | feat(swarmflow): add the verify() primitive and build_reviewers business helper |
| 200 | `fbbb2735268397465ffd3a75380994d1332a1eb7` | 2026-08-28 | zhaominjun1997 | feat(context_engine): Add capabilities for collection management, progress query and context browsing |
| 201 | `188966006844748d0d8c7d51c619153dd22eebc5` | 2026-08-29 | chenchunzhou | feat(swarmflow): add workflow pause/resume/stop with workflow budget and run_id cache isolation |
| 202 | `318f94dfc9fb1783cf2d80928963acbd4807443a` | 2026-08-29 | wncbdj | feat(llm): LLM module supports thinking modes and reasoning intensity |
| 203 | `31b6650ed92d6171f761273edd6ac3af7a313eb4` | 2026-08-29 | alan_cheng | feat(llm): support OpenAI Responses API for api_key configs |
| 204 | `4afaaaf31cd5492edb9257fe091c3f70b5683767` | 2026-08-29 | alan_cheng | feat(observability): add epoch-scoped trajectory events |
| 205 | `59a0f8de28f04cf277dc25bb7fc701ddec0f5733` | 2026-08-29 | mengyuanli | feat(online‑rl): Complete the closed‑loop integrating AIGW task collection, policy training and cross‑process validation |
| 206 | `a51e9fe08bf04cb908f9d7b9a356ddd5457346af` | 2026-08-29 | alan_cheng | feat(evolving): read LLM messages from the standard GenAI attributes |
| 207 | `bd9cf4f89dc6af134bdcbd147c2f2ad84f81193c` | 2026-08-29 | alan_cheng | feat(observability): derive backend shapes at export |
| 208 | `18744c6994453b9ae64c82dafc4e2761c04df0c5` | 2026-08-31 | zhanglongmin | feat(agent_teams): add external runtime failure reporting |
| 209 | `3dc2a79cfd82a7eaa7d957dbac9bfe33d05df202` | 2026-08-31 | zhanglongmin | feat(agent_teams): handle Codex runtime failures |
| 210 | `45f5b8c86a50ccd0e193152155e6bc90e98e7dcf` | 2026-08-31 | openJiuwen-bot | feat(context-evolution): add ContextEvolution framework and Metis task memory |
| 211 | `5634fdbdc6c4276f0e05c1ade6a78c42dfeebde7` | 2026-08-31 | zhanglongmin | feat(agent_teams): handle Claude runtime failures |
| 212 | `a4bb9e47661afb2f0bb2bf6ea9a1b461f543c722` | 2026-08-31 | zhanglongmin | feat(agent_teams): add auth fallback models for CLI agents |
| 213 | `4d68a3d142afc8de12e8b72977576ba692c91e42` | 2026-09-01 | alan_cheng | feat(swarm): separate send_message multicast into targets field |
| 214 | `7b12459785a06d54c1b36f55192024373c83055c` | 2026-09-01 | lin-xiaoyu | feat(symphony): adjust symphony orchestration interface to standard JGF format |
| 215 | `ac87152d1d1887e60d31238ce83f48a895da4855` | 2026-09-01 | alan_cheng | feat(harness): record ask_user lifecycle and team execution subject |
| 216 | `f1be55437e5647bd4319f2f8088bd47bb9776219` | 2026-09-01 | alan_cheng | feat(swarm): stamp team member execution subject on agent spans |
| 217 | `73696ff8a4ff364021b11e47b6c93cd561ea02d8` | 2026-09-03 | alan_cheng | feat(observability): surface real error causes on trajectory spans |
| 218 | `bdde34d5a18614a7158189fc7030e850f91a8db5` | 2026-09-03 | AFSDFASD | feat(rsi): emit single-harness engine events |
| 219 | `e305880af8125a9257a54f04a7e753308c56c39f` | 2026-09-03 | alan_cheng | feat(observability): align telemetry with GenAI semconv |
| 220 | `1b7b31205e3603101b8a91918e1812c037a8d3da` | 2026-09-04 | zhiqiuzoeyye | feat(rsi): add rsi paper pipeline initial version with test |
| 221 | `2eb4dba032f216c53ee4333f964414a19c5291aa` | 2026-09-04 | l_x_d | feat(rsi): PUCT program artifact provider, aligned with the new contracts |
| 222 | `3a53e0c9481e502d9485af65b59871307cfdac0d` | 2026-09-04 | lin-xiaoyu | feat(symphony): add graph experience accumulation |
| 223 | `9294c2c35fc410a7412bebc921c44024809c4483` | 2026-09-04 | layShang264 | feat(symphony): 支持基于执行证据的动态图谱更新 |
| 224 | `1d508b3e45891ec2fbfe84334992bada6f3439df` | 2026-09-07 | Haowei_Du | feat(symphony): add experience distill for skill pack |
| 225 | `1e143347a62dcfdbfc31bdae612fc94ac3a84e2d` | 2026-09-07 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#987: feat(browser): support bundled Playwright MCP commands |
| 226 | `019bb967b2cd53951dfd7b0d909accb9d076c14b` | 2026-09-09 | AFSDFASD | feat(rsi): integrate evaluator agent and align native harness publication |
| 227 | `59e085a88a6e7f0f421f94ae95a99a1e4c6ced13` | 2026-09-09 | alan_cheng | feat(swarm): route Codex request_user_input to the host as user input |
| 228 | `72325b2c28772fc0204b4178d58d15f3940009d0` | 2026-09-09 | alan_cheng | feat(observability): number each context compaction of a subject |
| 229 | `923658fc32cc1ff1207f0faa839c2c2775c48afe` | 2026-09-09 | zhaominjun1997 | feat(context): add proactive context support for GitCode service and improve rule‑based context collation |
| 230 | `a9f840257f707b0992b9d9c73e08f58fe3436a4d` | 2026-09-09 | alan_cheng | feat(swarm): add protocol harness providers and migrate claude/codex members |
| 231 | `2d6c5f6e117e30ead90a70c64bb30246d169572f` | 2026-09-10 | alan_cheng | feat(swarm): adapt NativeHarness to the public harness protocol |
| 232 | `337a90f8ab4b88d75c00b7f5b2b87986eccd557b` | 2026-09-11 | alan_cheng | feat(agent-teams): passive human member with tool-call passthrough |
| 233 | `6574f21795e893534618e72101fc5c522ba080b5` | 2026-09-11 | alan_cheng | feat(swarm): add DSH MCP support and harness prompt modes |
| 234 | `7e7871199997c28c95d3c761dfcc9027404a46f9` | 2026-09-11 | alan_cheng | feat(observability): address restated GenAI attributes by content |
| 235 | `d2663efc9f365d7ff7dfdd6acc0aa49464491055` | 2026-09-11 | alan_cheng | feat(swarm): copy portable skills into external harness projects |
| 236 | `e62a44ec5aa12aa098b2a736eee4f37088eb022e` | 2026-09-11 | alan_cheng | feat(swarm): register native_v2 and recover harness sessions |
| 237 | `29dae0141e7f117a6f485b78fb4fa63bbf27dbcf` | 2026-09-14 | openJiuwen-bot | feat(harness): resolve ripgrep for GrepTool and map type aliases |
| 238 | `46fe66196d78c032ec707312b5eace36ec222529` | 2026-09-14 | openJiuwen-bot | feat(llm): let init_model forward reasoning_effort and extra_body |
| 239 | `7e98a60f96a6fa988c55af38c5eb1a2b6108bb23` | 2026-09-15 | lin-xiaoyu | feat(symphony): Improve the Symphony experience distillation pipeline |
| 240 | `1cebece3928b956a5a799172579b7889f5336e34` | 2026-09-16 | alan_cheng | feat(harness): add per-model supports_vision declaration for native image input |
| 241 | `2022e8a3ece2fd47dd7f9f7494dc1dbb05b51307` | 2026-09-16 | alan_cheng | feat(observability): close the trajectory record and event kind sets |
| 242 | `4ddca8ff45c75a9e977ff56faa3616337a052fb8` | 2026-09-16 | alan_cheng | feat(trajectory)!: read reasoning as reasoning_content and skip compaction turns |
| 243 | `53c9a29c03751aead601f025601020e2e6109c63` | 2026-09-16 | alan_cheng | feat(trajectory): capture v2 events and replay their context windows |
| 244 | `6758808e44e2c8aec8ab9244a14701ca70ed2339` | 2026-09-16 | alan_cheng | feat(observability)!: one span-level trajectory schema version |
| 245 | `820c9104a6a676d21aef6b65e1d786a9b9206787` | 2026-09-16 | alan_cheng | feat(harness): render tool results as plain text for the model |
| 246 | `b40b484bc15ef89bc25be9ce6cbcfa2c2228bd0f` | 2026-09-16 | alan_cheng | feat(trajectory)!: rebuild evolution messages from the context window chain |
| 247 | `6d8c4575f87d7659c90fdabb0123285be538514e` | 2026-09-17 | alan_cheng | feat(agent_teams): open trajectory turns per team member round |
| 248 | `c0491a4dc222021b866d06b6261ee31d42c0190a` | 2026-09-17 | kyrie_irving11 | feat: port TTSE algorithm package onto develop |
| 249 | `d0bd83352f5ef4070baf44d37e788ba692a95b37` | 2026-09-17 | kyrie_irving11 | feat(rail): port TTSE rail onto develop |
| 250 | `d314738fcc935665ec3d15f7382ebe16bc9f04a9` | 2026-09-17 | alan_cheng | feat(context): report modified messages and offload handles in compression state |
| 251 | `5f0d395c620ea620aa2e732045bfb874f9138886` | 2026-09-18 | alan_cheng | feat(harness): take Claude Code request timing from its own span |
| 252 | `69bff3ed7aa610d8d68b64778d04c660455416f1` | 2026-09-18 | alan_cheng | feat(harness): take Claude Code facts from its telemetry, not the stream |
| 253 | `6dfda012193d09dc3908524d5f8ae406811d909f` | 2026-09-18 | openJiuwen-bot | feat(team): add group conversations and reliable member inputs |
| 254 | `83aa7194fe7f630d5de3e71b64589f8b3570e57f` | 2026-09-18 | alan_cheng | feat(harness)!: report third-party model requests as protocol events |
| 255 | `e5c0cf1a9a6fb602e76a4a485e90ae6350029a47` | 2026-09-20 | alan_cheng | feat(harness): let third-party harnesses switch model and effort |
| 256 | `ebd2c8a603039f49c0e4cbf3423882fad47d84f4` | 2026-09-20 | alan_cheng | feat(swarm): pick and switch built-in models for external CLI members |
| 257 | `2116850aca9ce6c601aa6d52a5ee7ead22bb2c0c` | 2026-09-21 | alan_cheng | feat(codex)!: append the host prompt to the CLI instructions by default |
| 258 | `b950fb5acc7eeb4c1e108cc977f417d1a29a3e03` | 2026-09-21 | alan_cheng | feat(codex): take tool facts from the CLI's own telemetry |
| 259 | `422dc44df25c0f6af9673215b2b214f21a044e34` | 2026-09-22 | wncbdj | feat(llm): Support for model group configuration and policies |
| 260 | `75f9ec591344b21a85658d60c68d6a9c07cee3e7` | 2026-09-22 | kyrie_irving11 | feat(ttse): persist BM25/Chroma indexes and slim ttse_consult |
| 261 | `05fc1de534372422a2b36106f38918ca0271cd4c` | 2026-09-23 | alan_cheng | feat(agent_teams): observe external CLI members via their own telemetry |
| 262 | `853e636c19d91985e0189360219d307b47d5d891` | 2026-09-23 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#748: feat(swarmflow): emit live AGENT_ACTIVITY events for single‑shot team-workers |
| 263 | `9e3390195a9ea15235b2b5f7412cb2aa440622cc` | 2026-09-23 | openJiuwen-bot | feat(agent-teams): The expert team has connected with the third-party agent. |
| 264 | `715585818067969bef199e5d2b5c9a79c758eff1` | 2026-09-29 | hwxiajing | feat(swarmflow): swarmflow-verify-fork |
| 265 | `11fda57fd64acc40fdd56c99ab57b8b60561f5b8` | 2026-10-07 | ZezeChang | feat(swarm): integrate in-process team organization with develop |
| 266 | `4dbe81f0923c70cb45a02d5a3a239c31381bb679` | 2026-10-08 | alan_cheng | feat(observability): measure TTFT on token-carrying chunks and record TTFB |
| 267 | `56a1963aa24529e16c7a5930e0e8cc43af347e92` | 2026-10-08 | hh0205 | feat(team): support team model group routing |
| 268 | `d93769175fc68f1a63953b587ea6197563ff331f` | 2026-10-08 | alan_cheng | feat(llm): stream Responses function call arguments as they generate |
| 269 | `018331eefeaf0c2b3b9c13264b2c0bb70bcece6f` | 2026-10-09 | openJiuwen-bot | feat(otel): Process-level OTel HTTP automatic instrumentation switch (W3C traceparent propagation) |
| 270 | `e1c2b442adf3478c057f70b2f222e0940eb7eaf8` | 2026-10-09 | alan_cheng | feat(trajectory): mark inferences recorded without observing their request |
| 271 | `02c18213883abdff6268042832f9985850d2daa9` | 2026-10-12 | kwangyyinc | [mirror] openJiuwen-ai/agent-core#1881: feat(harness): integrate Jev browser decisions and runtime refinements |
| 272 | `2f3a5b6afcb6a51cea559541b562155bb71c90cc` | 2026-10-12 | alan_cheng | feat(harness): let providers map host model choices onto their config |
| 273 | `ab825d17a2e729b9067c5057cce5c27bd792aa82` | 2026-10-12 | openJiuwen-bot | feat(otel): complete GenAI semantic convention attributes |
| 274 | `b8e43ab6b359d3f6ec0c4ba896f7858f8362b928` | 2026-10-12 | alan_cheng | feat(swarm): dispatch third-party harness providers as team members |
| 275 | `c8950e9b013769cae3cf4139e75addff096151a6` | 2026-10-12 | alan_cheng | feat(swarm): configure member models through provider model config |
| 276 | `d9cd20fe35aaae59f5775a0be92ef02ebcab37a3` | 2026-10-12 | alan_cheng | feat(harness): resolve third-party harness providers from entry points |
| 277 | `db044733362befc9651270b6cc59cb152e1c6afa` | 2026-10-12 | zhangyaomaggie | feat(harness): Single-Agent Mode Supprts Third-Party Harness Integration |
| 278 | `1f33f876184ddd8e289adfebb86c31cd65b6c50b` | 2026-10-13 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1862: feat(harness): preserve results at the ReAct iteration limit |
| 279 | `5be05b604d3a6344939f1e462f94dc397fef756c` | 2026-10-13 | alan_cheng | feat(swarm): negotiate the auth fallback of third-party members |
| 280 | `6daf980c22704549cae5264271b50eef2960aada` | 2026-10-13 | alan_cheng | feat(harness): reimport installed providers replaced on disk |
| 281 | `6ecef9fc961a69de77b354609051017e92a4d659` | 2026-10-13 | alan_cheng | feat(harness): run the auth fallback in the harness skeleton as a capability |
| 282 | `75936fff35ac0909316a534c83aff33d3d1edca0` | 2026-10-13 | alan_cheng | feat(agent-teams): show swarmflow workers in the trajectory view |
| 283 | `ad5f8f3c77aac628174455f4e37599b682fa4697` | 2026-10-13 | chenchunzhou | feat(swarm): swarmflow supports third-party agents, tool call display, and member grouping |
| 284 | `ae89e0b56d55bb43e1637712185cfa07eb84db0f` | 2026-10-13 | alan_cheng | feat(swarm): add task-scoped members with shutdown/dormant lifetime |
| 285 | `af85bd646ce6fdac928729928f0939a3751357b0` | 2026-10-13 | alan_cheng | feat(agent-teams): carry the assignee's submission note into the temp reviewer's review request |
| 286 | `c3f1b710eee55c5f3fed98b7defa301717586e14` | 2026-10-13 | alan_cheng | feat(agent-teams): show scheduled-mode temp reviewers in the trajectory view |
| 287 | `c98777af5aec33aa0f9cdde6a130bc24f1eedc99` | 2026-10-13 | alan_cheng | feat(agent-teams): carry a temp reviewer's previous-round feedback into its review request |
| 288 | `261a83feb4c31f3741ca04720e364e53c88c1269` | 2026-10-14 | chenchunzhou | feat(swarm): Pick AgentGroup experts as swarmflow workers via agent_template |
| 289 | `4cfaa0bac3f0cdc2de86b70b6de60f6b5e7ee5a1` | 2026-10-14 | alan_cheng | feat(agent-teams): hand scheduled completions over through task summary and note |
| 290 | `7711af2f8835b6ea279a0924dc7eee32c20a524d` | 2026-10-14 | min_gitcode | feat(agent_teams): preserve per-input RunContext through team harness and runner |
| 291 | `9c1d36c0dbed2baffbb418b094a09bb3d6e44951` | 2026-10-14 | alan_cheng | feat(agent-teams): wake the autonomous leader only for board events that need its decision |
| 292 | `a70a054eaeda0a4f641322c8f81278d8885f7e30` | 2026-10-14 | alan_cheng | feat(agent-teams): carry completion summaries through the autonomous board and claims |
| 293 | `c31bafce07ddb25e461e8083268817b924499635` | 2026-10-14 | alan_cheng | feat(agent-teams): narrow temp reviewer capabilities and list upstream tasks in review requests |
| 294 | `680795de3b0b02b7e4951f92cbf0576480cfc8c1` | 2026-10-15 | alan_cheng | feat(agent-teams): expose TeamMonitor.team_agent accessor |
| 295 | `b2fde5eb3172481ca94dbe1145539fc564b487c7` | 2026-10-15 | kwangyyinc | feat(cua): cua agent initialisation |
| 296 | `b36375af2b63d3352a3d08fac1c732ce4ebabd9f` | 2026-10-15 | alan_cheng | feat(swarm): keep team tools direct under progressive tool exposure |
| 297 | `cf741f8faaac199a617d78235042610976f2e592` | 2026-10-15 | alan_cheng | feat(swarm): bootstrap scheduled teams from the framework on start |
| 298 | `d88821892fd62f0ac43e1370f1f290715c16e4c2` | 2026-10-15 | chenchunzhou | feat(swarm): Reject agent_template 'leader' with a pointed error |
| 299 | `e698b56eaf5114bc3b1a5b7c260e8598715f8a64` | 2026-10-15 | lamsaan_yau | feat(moa): add MoA with JEV and LLM verifier routing |
| 300 | `f00022c7a8c28ac9fe0dd661a1055005d7fe721b` | 2026-10-15 | hh0205 | feat(team): support specifying member model in model group |
| 301 | `ad3f3a3ca56e54dfe1260208b999539139d0fc7d` | 2026-10-16 | alan_cheng | feat(swarm): expose template meta on monitor MessageInfo |
| 302 | `b4fe9d2c2912dfcaa74ab12370ea8c6d0b01fcb8` | 2026-10-17 | cyr20040123 | feat(attribution): harden reward stage and node slicing for e2e |
| 303 | `5109025d234ca9d8fcabace01101d045cf453855` | 2026-10-19 | fenglitong | feat(attribution): stream Fast Reward progress and lead with open claims |
| 304 | `54492ad47e5eef1f5d5cdbbefad5d7f417fd8350` | 2026-10-19 | fenglitong | feat(attribution): review only deliverables whose final version is selected |
| 305 | `68d71bb8be318a788d466e8d3acf232e92181f95` | 2026-10-19 | hh0205 | feat(team): support specifying member model in model group |
| 306 | `8353edb0ca7c611e489b558ae9c6ada1ca1acc59` | 2026-10-19 | fenglitong | feat(attribution): judge user follow-ups and superseded deliverables in task parse |
| 307 | `a373b42c2a0012794e3f8491bab828fcf948f912` | 2026-10-19 | fenglitong | feat(attribution): score with Fast Reward and keep process signals |
| 308 | `3ef022ec4976dd26a260803419193b96b6b88f44` | 2026-10-20 | cyr20040123 | feat(attribution): add user-note fallback and field window tool, rename reward stage, tune budgets |
| 309 | `6f70f6b74fc19774a846bf3adb3b1c7fa14b0a67` | 2026-10-20 | alan_cheng | feat(swarm): let the team leader browse and spawn installed experts |
| 310 | `e896b9e3f962bea04e93b49180bf51b0c1de54b4` | 2026-10-20 | alan_cheng | feat(harness): add AgentTemplate cards and an async package loader |
| 311 | `073ece6ed1c7ef02c04d9013f34db5b3ffa0effe` | 2026-10-21 | cyr20040123 | feat(evolving): sync attribution & reward from #3313 to release/v0.1.20 |
| 312 | `0bca65278e0505e7aa89ac17c9769ca60b284a46` | 2026-10-21 | alan_cheng | feat(swarm): add jw-testing skill for test handoff documents |
| 313 | `689a1c6dcd01608575d82f2aad7f367202d14e6e` | 2026-10-21 | alan_cheng | feat(skills): group design index by module |
| 314 | `181ac59a861628e0156da50fe302adfe7560304e` | 2026-10-22 | alan_cheng | feat(swarm): merge spawn_external_cli into spawn_teammate |
| 315 | `1aed5729ca437353cffde061919c1857d129576e` | 2026-10-22 | huangyuzhe | feat(memory): add JiuwenMemoryRail for AgentMemory external memory |
| 316 | `5dbe5872af0281296f193777a10e45bedd4be48a` | 2026-10-22 | alan_cheng | feat(swarm): let scheduled create_task inline members run on an external CLI |
| 317 | `a3f16078c8eaef923c534d7a2609026ce1854179` | 2026-10-22 | alan_cheng | feat(swarm): allow scheduled members to message each other behind a spec flag |
| 318 | `b20240fccbb55b3bc1f11823868611119fd52ca6` | 2026-10-22 | alan_cheng | feat(swarm): allow worktree isolation on scheduled inline task members |
| 319 | `c8e5d4432f3f27aa7c29bea648b500e60db7fbda` | 2026-10-22 | alan_cheng | feat(swarm): rank view_experts and view_expert_groups queries with BM25 |
| 320 | `28c27ed0741968615f3a017030851d78b2d9aa02` | 2026-10-23 | alan_cheng | feat(swarm): let scheduled members split their task into subtasks |
| 321 | `5cfd1ab1fcc8016a101e3db8a6f6e30e9ba2329b` | 2026-10-24 | alan_cheng | feat(swarm): start scheduled expert-group teams empty and load group members on demand |
| 322 | `4a541d19627f037365d99cab7a32b9c01ad029f6` | 2026-10-25 | alan_cheng | feat(swarm): track task revisions so executors always see edits |
| 323 | `be966392fa84b056616d381698a825df50870973` | 2026-10-25 | alan_cheng | feat(skills): define local code review workflow |
| 324 | `cbc216c7e0843b5a49bc44da3aaffd5354301427` | 2026-10-26 | alan_cheng | feat(skills): add causal flow and context confidence guidance |
| 325 | `cfed3bfb27077f1e5b9986fcc1114247a1b7425d` | 2026-10-26 | alan_cheng | feat(skills): strengthen decision and behavior evidence workflows |
| 326 | `ea8500bdd6b502d9f98dcf9e76fa02eb0fda0738` | 2026-10-30 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2315: feat(tools): run client tools as paused calls answered by the client |
| 327 | `c8c621b22f1ad5da6acee0ceee33778007c06efb` | 2026-10-31 | layShang264 | feat(symphony): finish migration APIs and harden retrieval |
| 328 | `4dbaa84ca568af3f181da4f9cd26b713a5b73039` | 2026-11-03 | layShang264 | feat(symphony): improve SkillPack member verification and compatibility with historical recipes |
| 329 | `5077b6b493f1f30fc98d4e899a894f556ca7eca2` | 2026-11-03 | openJiuwen-bot | feat: add medical knowledge base plugin for healthcare domain |
| 330 | `5a35c39f0163a4e6190800df53ad7e24848756eb` | 2026-11-03 | hwxiajing | feat(swarmflow): open the swarmflow tool to in-process teammates |
| 331 | `b917ef0ed62440b0468ad4f1c7c8c8e3404db59f` | 2026-11-03 | xinyang_bali | feature: 适配ttse插件 |

## 缺陷修复（811 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `3ca195ead619ffd4abb74db143b35f4d7a309c4e` | 2026-05-20 | alan_cheng | fix(agent_teams): correct hermes cli adapter to one-shot model |
| 2 | `ed69f8b45ee330edd518e64635c487d16e2bdb34` | 2026-05-25 | alan_cheng | fix(agent_teams): harden external-cli members for real cross-process runs |
| 3 | `ee0027b98d59524df9c7331df7faebe9441c140f` | 2026-05-30 | alan_cheng | fix(swarm): resolve lint warnings from Huawei coding standard audit |
| 4 | `372a4cfde6fbbb1442a26e6f1a70ba7c5abd8cbe` | 2026-06-01 | wncbdj | fix(rails): Delete dynamic prompts in HeartbeatRail |
| 5 | `43d18a45755b48705c6b86bf203b9bbbfd5fd2e7` | 2026-06-01 | alan_cheng | fix(core): honor boundary force_finish on ReActAgent loop exhaustion |
| 6 | `66b570b7a49847cae5b16528f9809f27608c1a3a` | 2026-06-01 | michaelhuawei | fix(mcp-openapi): correctly read tool parameters for OpenAPI MCP tools |
| 7 | `77df4fa7f48c1aa82434827b4ef0611fe74a0230` | 2026-06-01 | ray_le | fix(auto_harness): preset community skills & functionality improvements |
| 8 | `85c1e6f543da69328d1781f6d11a0af8581dc81c` | 2026-06-01 | yan_zj | fix(context):Remove redundant parameters in message_summary_offload, and do not offload assistant messages for tool calls. |
| 9 | `9cebd0f446c5ec5a3121350221545318259ab766` | 2026-06-01 | alan_cheng | fix(harness): rework SuperHarness round model and fix lifecycle bugs |
| 10 | `b834e74180d9d971b335b6f41936cd10c1f8c4fe` | 2026-06-01 | z2tng | fix(evolution): harden skill evolution approval persistence |
| 11 | `df67c97fb49f48dbdcd6dc3fd7f677bee7e747e4` | 2026-06-01 | yan_zj | fix(context):Optimize the method for obtaining the maximum value of the model context window |
| 12 | `fc81142aa604d71fc02ee4393c983bd9eb8e91d9` | 2026-06-01 | min_gitcode | fix(agent_teams): leader initial routed input dispatches directly without LLM round |
| 13 | `537ebc4c8cd85658760b44bfae252eb526ab752a` | 2026-06-02 | lamsaan_yau | fix: handle force finish before tool call |
| 14 | `8314a5136759c10d43763f2b510da849c8d7422b` | 2026-06-02 | lamsaan_yau | fix(harness): correct intent detection history assembly order |
| 15 | `8bc2e02a6458e48d697f0c24a4be9a36ae83a373` | 2026-06-02 | taojianjun | fix(dev_tools):delete skill creator |
| 16 | `8eb6f3d6574c978d26d34d353d1ae8ea8ce9c394` | 2026-06-02 | ray_le | fix(auto_harness): fix load_harness_config skills loading failure |
| 17 | `9c8f1f39609b29aafa7fa0ef3eb5de796ae1ee33` | 2026-06-02 | lamsaan_yau | fix(multi-agent): forward agent-as-tool stream chunks to parent stream |
| 18 | `ac28d8910d5ee46a363fc222621ce1f900730682` | 2026-06-02 | karina0522 | fix(harness): prevent subagents from bypassing parent sandbox restriction |
| 19 | `ac6bb098d589df8f2f0f5812a6e6dc65a3deb358` | 2026-06-02 | weichenhao | fix(tools): Windows bash tool |
| 20 | `aeceda1ca329c380d2fc609cbc15592c39190d58` | 2026-06-02 | iamcandiceguo | fix(agent): llm agent consume all stream when exception |
| 21 | `bd5ff5c282952d3a92f88469e781121f0bd033ea` | 2026-06-02 | haowenzhong1 | fix(Agent): Add performance data capture for single requests |
| 22 | `e2f492c89101815b38a55c46ef9d14eb7265bba4` | 2026-06-02 | lamsaan_yau | fix(llm-agent): resume plain query against first interrupted component |
| 23 | `e94e2f4d03d5b9fd47ea2419948438f4241ab9d8` | 2026-06-02 | ray_le | fix(multi-agent): forward agent-as-tool stream chunks to parent stream |
| 24 | `edea53e8bafceca29c4cf7e124b64cee2735b0cb` | 2026-06-02 | dev_chenyan | fix(evolving):Evaluation of Pipeline Module Refactoring and UT Supplement |
| 25 | `f460d56a2be9a64b92b955ea2cffa13ece3fafef` | 2026-06-02 | iamcandiceguo | fix(team): fix token cal error |
| 26 | `144debbdb7f708cb447b27bae5cf3c3119cf8d8d` | 2026-06-03 | karina0522 | fix(harness):prevent subagents from bypassing parent sandbox restriction |
| 27 | `ca551cf83f90e18fa024cb33f02bd1977dff9d37` | 2026-06-03 | yangzequ | fix(llm): add OpenRouterModelClient with attribution headers protection |
| 28 | `e68c3f8306f7cac6e654b62b69d9b40f3cd95dac` | 2026-06-03 | wncbdj | fix(tools): The cron task occasionally passes the wake_offset_seconds parameter, which is not as expected. |
| 29 | `46e7338a8c1b35492758d2403e651307f28a964d` | 2026-06-04 | ray_le | fix(auto-harness): update tool param prompt for impl & validation |
| 30 | `688b38f53016557ece608505fd3f606d2b8ad747` | 2026-06-04 | Ziliang_Zhou | fix(memory): resolve inaccurate time extraction of episodic memory |
| 31 | `6e1fce16c1ae4406afed00d01adb17b7d8807c52` | 2026-06-04 | wncbdj | fix(harness): Modify the subagent's workspace |
| 32 | `b0162a22939d578bad6a19bc8b331637daee02b5` | 2026-06-04 | m056838700 | fix(harness): fix audio tool bugs |
| 33 | `e868918b1fa3315c7c5e6377c17f581c00c93732` | 2026-06-04 | weichenhao | fix(worktree):worktree Path Traversal Issue |
| 34 | `0e017f7adb01d61a201a1899c28909d761e8e88c` | 2026-06-05 | kdgmpw | fix(filesystem): Fix the is_partial determination issue |
| 35 | `81c9448f0b16c4c340495166ad8753cfee10b85c` | 2026-06-05 | alan_cheng | fix(swarm): mirror team session id into logging trace_id contextvar |
| 36 | `ceaf14eb1ad8db11734c11fe2151e3eb3f4e58f5` | 2026-06-05 | alan_cheng | fix(swarm): create per-session tables only under a bound session |
| 37 | `cef3d3669b6544bdb622d6533ebce6ebacfa9fa0` | 2026-06-05 | zhaozixuan | fix(sandbox): delete sandbox when SandboxGatewayClient.release is called |
| 38 | `e6395460b13afdad0e38ae7b3b57f21815adc509` | 2026-06-05 | alan_cheng | fix(swarm): de-duplicate team tool registration across native rebuilds |
| 39 | `6d5491a6622f42eca062b3b85078b97b4bb68c4d` | 2026-06-06 | ldstyle8 | fix(harness): enhance plan mode rail with static injection, write-op blocking, and mode restoration |
| 40 | `58a07fde8de60b4d9d0cd334ae43d6bd75f3e74b` | 2026-06-08 | alan_cheng | fix(harness): decouple fetch byte cap and scope web tool error codes |
| 41 | `69c383fabcd598509dffa5bbe3428350df859f0c` | 2026-06-08 | alan_cheng | fix(harness): honor inline proxy credentials and enrich proxy config |
| 42 | `958fd6142e76ae3221df5117b186820b062edd81` | 2026-06-08 | MengHan18 | fix(PromptAttachment): introduce PromptAttachmentManager and migrate dynamic prompt context out of system prompt |
| 43 | `ce76141580ec762702d5edbbe1d828ee76eff092` | 2026-06-08 | wncbdj | fix(llm): Model response parsing supports cache_tokens |
| 44 | `422bfa030598e49d83fb60a3ec70f9d8910ac51c` | 2026-06-09 | alan_cheng | fix(swarm): route human-agent drive through avatar coordination |
| 45 | `456ddef5f5e628017343ae88776a5cacae5df451` | 2026-06-09 | ray_le | fix(harness): use steering queue in TaskPlanningRail to preserve message sequence |
| 46 | `823c8c753b2c81981eee0a8d87d398a4ab974702` | 2026-06-09 | min_gitcode | fix(agent_teams): gate deliver_input on runtime readiness to prevent NativeHarness startup race |
| 47 | `a3bf2b9dfad8be63ef1c0dc698552009a90f9cd0` | 2026-06-09 | SushiNinja | fix(auto-harness): fetch actual pull request template with config language |
| 48 | `387b72d2c3dd419bae97530e8b3fb1f49848db6e` | 2026-06-10 | kwangyyinc | fix (context): added guardrails to context.py when round_index < 0 |
| 49 | `c685269632704e7b95024acc4eb1393daea51f20` | 2026-06-10 | MengHan18 | fix(PromptAttachment): simplify prompt attachment lifecycle |
| 50 | `3585aaf1062fb379fcad950cd28b9e315acdcfd0` | 2026-06-11 | yangzequ | fix(llm): override _extract_reasoning_content in OpenRouterModelClient to support 'reasoning' field |
| 51 | `80a2133d7be3639918a0e39a191ea84b51913b55` | 2026-06-11 | openJiuwen-bot | fix: A2A extension invoke should return combined results |
| 52 | `97b66c8982b1132139aad914e567729ad8df1d4b` | 2026-06-11 | alan_cheng | fix(harness): expose _http package API as public to fix G.CLS.11 |
| 53 | `6eba3e29a76d13d5b9ec37c263298a6b6ffeaf02` | 2026-06-12 | dreamofapsychiccat | fix(tools): Allow tools to be executed in sequence instead of parallel |
| 54 | `d95cc414fb0e00b151ef6abc863a91811404ae4c` | 2026-06-12 | m056838700 | fix (harness): fix hot reload based on config for audio tools |
| 55 | `ee92d08bb27c758cf0abd4453852bfcce395ad50` | 2026-06-12 | MengHan18 | fix(PromptAttachment): split prompt attachment sections by stability |
| 56 | `5a569f534c202c74c8e973c0cd2e43eee7556441` | 2026-06-13 | z2tng | fix(skill-evlution): improve skill evolution active review for fuzzy signals |
| 57 | `dd38db7ea0560464d9893468dfbae677a4749d19` | 2026-06-13 | yan_zj | fix(harness):Optimize the dynamic memory content concatenated into the system prompt |
| 58 | `0769818d0b01c2671065553747275c1922ed1eb7` | 2026-06-15 | iamcandiceguo | fix(team): clean up _running_tasks before publishing task event to prevent concurrent ReAct loops |
| 59 | `198f881299d8b95355c935f682e849a438220399` | 2026-06-15 | ray_le | fix(auto-harness): ci gate runner & ruff check bugfix |
| 60 | `4b713f03d630bb4da1dd20f6c4ae558f0b9db823` | 2026-06-15 | MengHan18 | fix(PromptAttachment): delete duplicates |
| 61 | `8f3b4d1202cf7559a7c9fe9a95a14b5e953e37aa` | 2026-06-15 | iamcandiceguo | fix(team): cancel zombie task after completion_timeout to unblock subsequent rounds |
| 62 | `b7e19a33a40564d68056cb84bf7c82d7ce5be1b0` | 2026-06-15 | tangfei0306 | fix(agent-team): agent team can execute in memory mode |
| 63 | `0b0a564104b8f4d7e611e6b4343c5778e4268416` | 2026-06-16 | MengHan18 | fix(PromptAttachment): fix unstable skill section in system prompts, preserve shared prompt builder after model reload |
| 64 | `249a1ede8989a5ff158513bb9b667dd636643587` | 2026-06-16 | ray_le | fix(tune): 修复 JointOptimizer 在 num_examples 小于数据集 Case 数量时的偶发 ValueError |
| 65 | `32dbc1bc1fdbd4a9711d086191823a2fbe227e97` | 2026-06-16 | z2tng | fix(evolution): harden active self skill evolution |
| 66 | `41d409b0b0128a346fd76f43bf9d6b79df2138a7` | 2026-06-16 | ray_le | fix(chat_agent): 修复 ChatAgent.invoke self._session 未初始化导致迭代优化报错 |
| 67 | `4bff33a18aed2f91acee9f028eba4c3b22f88c68` | 2026-06-16 | dreamofapsychiccat | fix(tools): Fix P2PAbilityManager's execute() to inherit correctly |
| 68 | `7281fac9f21bdc1cf013c3941ae81696e9b6d288` | 2026-06-16 | huangyuzhe | fix(vertex, workflow): 修复超时重试逻辑的条件判断错误；erase inner_error status when retry |
| 69 | `98933714146582601e045c585bc306d7180a559d` | 2026-06-16 | alan_cheng | fix(swarm): align stateful session path with _BackendCallResult |
| 70 | `9aa3455f0c788ff15890230e466fc5aee1b69557` | 2026-06-16 | zhanglongmin | fix(agent_teams): disable source_metadata injection in AgentTeams sessions |
| 71 | `c77b65d0a5120a4726ba291e94030e1f32d6765a` | 2026-06-16 | lamsaan_yau | fix(harness): isolate agent rail callbacks by instance namespace & cancel deep task on stream disconnect |
| 72 | `fc601114e7baa516cd7a2707f2d5e24f6d282b04` | 2026-06-16 | z2tng | fix(evolution): clarify active evolution confirmation prompts |
| 73 | `2fa3fa897319bff1e0493d27d4019f20a297f483` | 2026-06-17 | maxiutomoq | fix(browser): improve caching performance and extend probing element targets |
| 74 | `c0856121a66f93296781f3e42cec3135f60c508c` | 2026-06-17 | karina0522 | fix(harness): allow edit_file after partial offset/limit reads |
| 75 | `ceb21416840c1f2e39e75f723a591366a427e8ce` | 2026-06-17 | zhanglongmin | fix(agent_teams): remove duplicate _ensure_team_member_options_column and unused local import |
| 76 | `fb33ad9c0856cdeb982fe9920dc6fcffe034b1b7` | 2026-06-17 | zhanglongmin | fix(agent_teams): promote protected static methods to public to comply with G.CLS.11 |
| 77 | `b28977b725979e2d2b225171e3b3903547d00e17` | 2026-06-18 | z2tng | fix(harness): count team spawn tools for skill creation rail |
| 78 | `f89536251524fcc2ccbf4224d01222ce25337ddb` | 2026-06-18 | ray_le | fix(auto_harness): resolve worktree isolation & CI gate issues in harness pipeline |
| 79 | `117b1290f25c8cd1d2e214317b2b8bc3df383172` | 2026-06-22 | hzx99 | fix(memory): solve the problem of LongTermMemory's mandatory reliance on db_store |
| 80 | `5ccb1ac8517dadb63383ddd2998fb59ded794aee` | 2026-06-22 | hwxiajing | fix(observability): refactor span hierarchy management, streamline public API, and add mock LLM tests |
| 81 | `7f998d05ef4c2967cf4ab500363013c0f3553aad` | 2026-06-22 | openJiuwen-bot | bugfix(toolrail):fix tool rule removal not effective |
| 82 | `06cd7d78d3fbdbaedea88f8293e401036202e66e` | 2026-06-23 | tangfei0306 | fix(agent-team): modify prompt for human-agent must be assigned by leader |
| 83 | `0b8e4f54c4860cf1eea5f86c12fbf03e7dad8415` | 2026-06-23 | alan_cheng | fix(harness): fire after_task_iteration on task-loop executor error path |
| 84 | `49b767e10a427adcad57151e0cabd98fe0616d41` | 2026-06-23 | min_gitcode | fix(agent_teams): configure Codex MCP startup |
| 85 | `5af27ac9d4175b8319a297f1777d4c237af00e97` | 2026-06-23 | alan_cheng | fix(swarm): fire per-round invoke lifecycle in native harness multi-round mode |
| 86 | `5cf75bd67dceb9ccf6be265f9a582b52a03f99a7` | 2026-06-23 | zhaozixuan | fix: add fallback_on_failure for sandbox |
| 87 | `5d7cef2c57795717c4da9078658e923d427b9413` | 2026-06-23 | yangwenhan | fix(workflow): fix the issue of incorrectly identifying nested branch predecessors as an AND group. |
| 88 | `6ff7599b7406e930faa9ae217d21479a3901a9a6` | 2026-06-23 | hwxiajing | fix(observability): update LLM input delta display strategy and remove deprecated config |
| 89 | `8712d64a1e007338d4af908551defee3dd3cb09f` | 2026-06-23 | MengHan18 | fix(PromptAttachment): stabilize skill prompt sections |
| 90 | `89255dc3e3d467dc541c825dbd1e37f4eafe696d` | 2026-06-23 | alan_cheng | fix(swarm): restore session context on swarmflow resume relaunch |
| 91 | `a6b3b8daf97dbd5e52e35c561b3f1cd25f1ec45f` | 2026-06-23 | z2tng | fix(evolving): route dedicated interrupt resume input |
| 92 | `6bb621d62892d213f07ed694dcee75db4617be1d` | 2026-06-24 | z2tng | fix(evolving): schedule team completion follow-up in task loop |
| 93 | `7830bc3a215db3658657ca92294c008cdb296250` | 2026-06-24 | yangwenhan | fix(workflow): avoid none value causing exceptions when judging condition length |
| 94 | `5e685f6b4a30d972de05528d43b471487cfc0778` | 2026-06-25 | dev_chenyan | fix(evolving): Fix the Agent skill reading exception caused by encoding issues |
| 95 | `f7dbd5d6abdefacecad0aa2fbd0313bcd2f43ff5` | 2026-06-25 | openJiuwen-bot | fix(team): anchor teammate worktrees to project dir |
| 96 | `7ea51bd94d4db618a1d9edb1a0cbbda5dabdc54e` | 2026-06-26 | m056838700 | fix(multimodal): implement vision model config validation and enhance tool creation logic |
| 97 | `da5809359bb4d247257ea3c852394f96330ccbc7` | 2026-06-26 | openJiuwen-bot | fix(bash): guide git worktree add target to project .worktrees dir |
| 98 | `9dbdeacbd12f8f0e833f2c9797ce117471fbe2a7` | 2026-06-27 | openJiuwen-bot | fix(evolving): add paired evolution archive service |
| 99 | `80febdefc5e26a6ceceb1cdc7b0b977bf3a07416` | 2026-06-28 | ranranshengqi1999 | fix(end):modify end template error |
| 100 | `06def473f0ea0428e833e0c8aa204225b2642bcd` | 2026-06-29 | openJiuwen-bot | fix(memory): resolve memory tools init failed and team memory generate failed in team mode |
| 101 | `0ddb77b7670bb7d0392417c4a0a086738750412f` | 2026-06-29 | huqi | fix: honor no_proxy cidr for schemeless hosts |
| 102 | `24baa0c18ecfb522673cc2d15db69d9089bbe29e` | 2026-06-29 | lamsaan_yau | fix(controller): normalize wrapped tool names in legacy task routing |
| 103 | `35ad6bdc27efd0655d5aa7a9abbf66efdbee5bc0` | 2026-06-29 | lamsaan_yau | fix(lsp): prevent nearest_root from crossing stop_dir |
| 104 | `3b2247beb20d6b2fe2b77f2e3c05f779c9fea7c1` | 2026-06-29 | yan_zj | fix(tool):Modify the restrictions on repeated tool calls in the system prompt, and add explanatory notes for empty files or empty tool call return results. |
| 105 | `425fe9844de3bbd2e1042d2c4762739ffc7d252d` | 2026-06-29 | ldstyle8 | fix: support review in plan mode |
| 106 | `4d8c3ab8c467c90819f7b19541cdec7d2e9a1832` | 2026-06-29 | zhaoyulu1 | fix(trace): modify otel agent handler，llm input/output format |
| 107 | `53a32e8c6e4ec28677350f1a44d2d1aab88a1b1d` | 2026-06-29 | lamsaan_yau | fix(ReActAgentEvolve): avoid circular import in ReActAgentEvolve |
| 108 | `5cfc4f7cb14889623b5bda3255442839904465d7` | 2026-06-29 | lamsaan_yau | fix(harness): persist repaired tool call arguments |
| 109 | `600b468f057ccbc4cbd90e058e3e59fb91ddd372` | 2026-06-29 | lairuofei | fix(graph-memory): dedupe relations by fact field |
| 110 | `746cbb3e8c999690449ae5e0b8e1b685234dad94` | 2026-06-29 | tangfei0306 | fix(harness): fix llm's tpot's time |
| 111 | `a51674047ee4ad00562756e5201470c95979ed69` | 2026-06-29 | lamsaan_yau | fix(harness): parallelize skill subdirectory registration |
| 112 | `be0e5ca4c3155a04337b7ab0c15f01ddcd89d519` | 2026-06-29 | yangwenhan | fix(workflow): optimize the registration frequency of component callback methods from instance-level to class-level |
| 113 | `c82d7a7d6a246f013cb72cc90b1a4f4e5f706726` | 2026-06-29 | min_gitcode | fix(agent_teams): collect only newest broadcast id for mailbox watermark |
| 114 | `d1ac6b366f72edce41f944afa0fd35312f70177f` | 2026-06-29 | yangwenhan | fix(workflow): revert optimize the registration frequency of component callback methods from instance-level to class-level |
| 115 | `ec26a306a3fcb686fde34565e0a352888d53d459` | 2026-06-29 | lamsaan_yau | fix(harness): preserve streaming finish_reason in ReActAgent Refs: #867 |
| 116 | `f086bfe8db434112c0662f0535bdf59fe649fb1c` | 2026-06-29 | yangwenhan | fix(workflow): optimize the registration frequency of component callback methods from instance-level to class-level |
| 117 | `0dbd2dc491dc0f1292e05305a76ce69738f50521` | 2026-06-30 | zhaoyulu1 | fix(trace): should not run tracer.trigger() if handlers empty |
| 118 | `43e77754c2bd4afa02b71d029dcfa8f390e337b0` | 2026-06-30 | alan_cheng | fix(logging): dedupe loguru sinks by target to cap file descriptors |
| 119 | `4dbeb1af8dd33d0346090780272740503cd55fb6` | 2026-06-30 | iamcandiceguo | fix(workflow): release more deepcopy |
| 120 | `785a0fcfba0061e6e0a6b487ca6adcb90ada0b2a` | 2026-06-30 | openJiuwen-bot | bugfix(toolrail): remember option optimize |
| 121 | `8e8dd32feccde6590eae87a0c53489c7af5f222b` | 2026-06-30 | zhangmengyin | fix(workflow)：Revert optimize the registration frequency of component callback methods from instance-level to class-level |
| 122 | `93fd4b37c4e4ec8dbe3c90fdf78b16b472631e73` | 2026-06-30 | hwxiajing | fix(observability): plan mode, subagent span bugfix. leader span name fix. |
| 123 | `b4a5a967b506a51c0103abff435de63cf35492df` | 2026-06-30 | alan_cheng | fix(tools): cap shell tool output by default to prevent context bloat |
| 124 | `f21315ea542bedea05b84e96f1dbc0011b3df84d` | 2026-06-30 | alan_cheng | fix(logging): disable loguru enqueue on file sinks to stop FD exhaustion |
| 125 | `3bfed6d2709b88ba02c754e3d6a7a089dc945622` | 2026-07-01 | zhangyaomaggie | fix(harness): calling task_tool without the subagent_type  parameter |
| 126 | `5d8b91ea50c459721eb9077274186710f60280ae` | 2026-07-01 | yangwenhan | fix(workflow): optimize the registration frequency of component callback methods from instance-level to class-level |
| 127 | `078a941e549d907ea646c20112680ecd4034c926` | 2026-07-02 | alan_cheng | fix(swarm): collapse broadcast reads to watermark in message manager |
| 128 | `11694f1a2bc327deff30f1b1c595a391fb6f4ed3` | 2026-07-02 | min_gitcode | fix(agent_teams): surface native harness completion timeouts |
| 129 | `14a7033b745f9380d8e24b8759ef9a32523e252a` | 2026-07-02 | yan_zj | fix(context):Reload tool prompt optimization and path issue fix |
| 130 | `361e39d5fbdd83585efa82e5503992549ca8fe33` | 2026-07-02 | weichenhao | fix(team): store teammate worktrees under system session root |
| 131 | `512e4738e5ef319cbd4bc19ae93645bed00cde72` | 2026-07-02 | zhaoyulu1 | fix(trace): add gen_ai.operation.name in llm-related span |
| 132 | `63efa098eba3efba1d17328021308b3665fb25b8` | 2026-07-02 | weichenhao | fix(team): clean teammate worktrees with team lifecycle |
| 133 | `cd3041d0f5c61e2d7cfba062cc2adaac16d5f06a` | 2026-07-02 | weichenhao | fix(team): track session ownership for teammate worktrees |
| 134 | `d0adebacc341a2a4946a25a5057ab2ae5f4f5a7b` | 2026-07-02 | l_x_d | fix: context compression budget guards |
| 135 | `d6ba4d355b4d79e1db70da500afcc27afbacfb4e` | 2026-07-02 | alan_cheng | fix(swarm): restore db_type="memory" as a :memory: SQLite alias |
| 136 | `e1b4f5c5d5187836fe03d4c91821f8f7b8bdd5fc` | 2026-07-02 | z2tng | fix(agent_evolving): reduce false positive skill evolution prompts |
| 137 | `4285bb9cfacfc4046b406d7489f60d4a18fa4d9d` | 2026-07-03 | wncbdj | fix(todo): To-do task status updates are slow |
| 138 | `671f83cb0fdb80460b63a54553e11252ce623e7a` | 2026-07-03 | huangyuzhe | fix(callback, session): cache function signature check to improve performance; extract regex patterns as module-level constants to avoid repeated compilation |
| 139 | `970206498d1d6ca2701d1556dda477ce75847053` | 2026-07-03 | iamcandiceguo | fix(state): relase deepcopy when save checkpointer |
| 140 | `0948739f6dca1c5ece397b731e69e2583185d8f7` | 2026-07-04 | chenchunzhou | fix(team): drop list_members from leader tool set |
| 141 | `36059ffad698ed598d2a5a80b2b8ef602a4c57ea` | 2026-07-04 | weichenhao | fix(team): align isolated teammate project context |
| 142 | `bc5747425bc417eb1699b3d657b665d9246afeee` | 2026-07-04 | weichenhao | fix(team): restrict worktree isolation usage |
| 143 | `3d6ce9558d13ead16dab703fe67032e1f2379c35` | 2026-07-06 | zhangmengyin | fix(log): add level validation at log entry to filter out logs below configured level, reducing unnecessary operations and improving performance. |
| 144 | `503b4c577fe306e50fd8597aaddcbfae4fa4d185` | 2026-07-06 | min_gitcode | fix(agent_teams): persist child agent session state on round stop |
| 145 | `55116d61100fdb98299e37271c3b7165a0c65cde` | 2026-07-06 | wncbdj | fix(llm): handle stalled streaming chunks |
| 146 | `a36f62dce7694b6e08a76a858a3b71c904788539` | 2026-07-06 | l_x_d | fix(context): Add team collaboration reinjection for compressors |
| 147 | `bf3f8c6ed0f9c27724aac69c1c137b48ed47887e` | 2026-07-06 | min_gitcode | fix(agent_teams): avoid native harness round timeout |
| 148 | `cd3c15e2b44a02cea26963fce2ee92dfa3468d51` | 2026-07-06 | yangzequ | fix(team): harden workspace mounts when symlinks are forbidden |
| 149 | `e343eedda4c3400d5d54c32e7f52abc67bbf2a5a` | 2026-07-06 | Ziliang_Zhou | fix: resolve inaccurate temporal extraction in episodic memory |
| 150 | `e9d852659bfd24ce3acade208000470aa42acf5c` | 2026-07-06 | MengHan18 | fix(Prompt Attachment): Move the skill section back into the system prompt. |
| 151 | `f8e450b2fda90c8324efa206be6be5435bedd826` | 2026-07-06 | zepinzhang | fix(state): state commit remove deepcopy |
| 152 | `fd01b33ce2d830346c2312df85bcb7c0c38f1e6b` | 2026-07-06 | yan_zj | fix(context):read file tool fix and remove reload_original_context_message tool |
| 153 | `7216b8af6fdc3c04949e245855aae6ba7f5fc555` | 2026-07-07 | openJiuwen-bot | fix(evolving): add OTLP-first trajectory tracing |
| 154 | `a2922bf93649aee8d8cd42e0292f193be5ff4147` | 2026-07-07 | alan_cheng | fix(swarm): retry abnormally-dead rounds and add stall watchdogs |
| 155 | `ff12108e58e2d7d0d9d09e38f01096cc1cc2b2b7` | 2026-07-08 | zepinzhang | fix(state):remove commit_user_inputs deepcopy |
| 156 | `5ba83785e78ffd5ee23c009186584f5013ad33ff` | 2026-07-09 | zhangmengyin | fix(memory): fix message sorting disorder caused by inconsistent time zones of message creation time |
| 157 | `e5a6ef0483064ffde9dd77a8a6001314ec6c2c96` | 2026-07-09 | xinyang_bali | fix(evolution): remove duplicate versions in the archive directory after rollback slash command |
| 158 | `e96508b5700263550b8f52c3fec53307d9979cdf` | 2026-07-09 | min_gitcode | fix(agent_teams): let model self-correct on structured_output JSON errors |
| 159 | `f44da3256a981bea1eb38fbd9cb7defc362df6ac` | 2026-07-09 | xinyang_bali | fix(evolution): enhance the max output limit during experience generation for Huawei Cloud model |
| 160 | `5377ade8384cafcb3ac45e3832f2e37d8570e661` | 2026-07-10 | Ziliang_Zhou | fix(memory): adjust import packages of jiuwen_memory_provider |
| 161 | `92a04257c5ae72457685371cf9599bcccc9c23a2` | 2026-07-10 | wncbdj | fix(mcp): fix parsing int type as float |
| 162 | `e6b259e0d160eb8b01eeb0b968bda4e1883226f9` | 2026-07-10 | MengHan18 | fix(harness): stabilize agent identity persistence via IDENTITY.md |
| 163 | `0988e98c782750cbdd752d1ee4727a146a1a6c75` | 2026-07-13 | iamcandiceguo | fix(mcp): fix CPU 100% caused by cross-task async context exit |
| 164 | `0d622183e458acc646ee4dbb62add19cd3938973` | 2026-07-13 | hzx99 | fix(memory): 解决embedding配置更新时，记忆索引无法重构，无法热更新的问题，删除mock embedding代码 |
| 165 | `1fced4290276913e7864b7403f912dc2a3e061ef` | 2026-07-13 | zhangmengyin | fix(memory):Fix embedding error when summary memory content is an empty string |
| 166 | `231c45ca376d5e8a262565a28f8a94c0aa27b67a` | 2026-07-13 | Ziliang_Zhou | fix: add timeout params in hybrid_search function |
| 167 | `48b2c12d50a884e4cedbf215f90ba9b7a3414e56` | 2026-07-13 | openJiuwen-bot | fix(operator): use cross-process async read-write lock |
| 168 | `810bb0710d7bb8f697d7f4813264c49dc3ce386d` | 2026-07-13 | karina0522 | fix(tool): truncate PDF/notebook reads over token budget |
| 169 | `86b58bc4e0133c3f5e51b643f1b6273f43087772` | 2026-07-13 | Ziliang_Zhou | fix(memory): unify the behaviors between the SDK and server sides in jiuwen_memory_provider |
| 170 | `dbb1e0a1858027ce0ee62441cf7b0024482661d9` | 2026-07-13 | tangfei0306 | fix(workflow): release more deepcopy |
| 171 | `f38e0d96cd14de5f0b36e741869efbb3918445ad` | 2026-07-13 | LasVie | fix(llm): respect provider-specific credential requirements |
| 172 | `17194dec6b5b4fcb8dd5e469b36d4f0ac3d5e1b1` | 2026-07-14 | wncbdj | fix(context): fix model keeps looping the same reasoning contents and tool calls |
| 173 | `3bd81f313033c2433c83d3d3a89f6606deb18fd3` | 2026-07-14 | zhanglongmin | fix(agent_teams): recognize sdk cli backends |
| 174 | `6c701e1d5c46024b19f0cf16bfdebe7132c2e73f` | 2026-07-14 | zhanglongmin | fix(agent_teams): avoid claude ssh read close race |
| 175 | `8d12b4b18edcab1ae1c258d763c660c94fb39991` | 2026-07-14 | yangwenhan | fix(callback): skip trigger coroutine when no subscribers |
| 176 | `b075022deadf7e4f1b41ad3890d1c34575e11014` | 2026-07-14 | openJiuwen-bot | fix(worktree):During the operation of the temporary team "clean team", the "worktree" encountered an error. |
| 177 | `bd13e35aa255d9b53d8dd620363818eb1c9255b0` | 2026-07-14 | lamsaan_yau | fix(llm): include extra request params in LLM_INPUT callbacks |
| 178 | `cccbaf21fd7516b3b78b1a256a3ddbc041c76e98` | 2026-07-14 | openJiuwen-bot | fix(memory): use raw query to improve memory relevance |
| 179 | `dbc7225adfac1a419d5c64ab3f4772bf1c787e08` | 2026-07-14 | alan_cheng | fix(swarm): let the leader shut down human members like any teammate |
| 180 | `e55f2b9c2a824989b65f3b91c9a1ebcbfe124758` | 2026-07-14 | zhanglongmin | fix(agent_teams): stop external runtime on spawn cancel |
| 181 | `fd6febc5642c2ca48c978aea9a3b52b38e5d82ad` | 2026-07-14 | openJiuwen-bot | fix(evolving): adapt evolution rails to OTLP-first trajectory format |
| 182 | `0fc618ad17658f4e5137a7d228671cc08f1ececb` | 2026-07-15 | Ziliang_Zhou | fix(memory): degrade to FTS5-only search when embedding is unconfigured |
| 183 | `5dcd135719fe47557abb25cdc719248c90033ece` | 2026-07-15 | hzx99 | fix(memory): resolve issue where re-indexing fails after embedding configuration changes |
| 184 | `a312353c836d19091cfe5ed86a5e931c58a80bf5` | 2026-07-15 | zhanglongmin | fix(agent_teams): clean external cli backend sessions |
| 185 | `2e7629e22d09b2f649267a131cd774fce1e6e782` | 2026-07-16 | zhanglongmin | fix(agent_teams): align external CLI startup contracts |
| 186 | `53912439b78d9dad126ddb05d99aa01c27fb5997` | 2026-07-16 | alan_cheng | fix(swarm): resume a paused round from kernel.start, not notify_team_built |
| 187 | `719e33df63804a6be832b516f582a62c6e3cbfec` | 2026-07-16 | zhanglongmin | fix(agent_teams): address external CLI quality warnings |
| 188 | `c502ace9c1aef630375aca295beea0aa8fc787d6` | 2026-07-16 | l_x_d | fix(context): add switchable refactored processors |
| 189 | `c67d405a03d757fc5470e954dbadf18f31b26bab` | 2026-07-16 | w1101627533 | fix(evolution): skill evolution score update and two-stage optimizer with root-cause analysis and tool-call chain |
| 190 | `e3bb3fe79313ecceb68c289e675ac2c9f7c787b3` | 2026-07-16 | zhanglongmin | fix(agent_teams): close messager on coordination stop |
| 191 | `1e3a5c7a3d2f654ff9edf9a5d91384e7f3863223` | 2026-07-17 | zhaozixuan | fix: isolate sandbox_id from different agent |
| 192 | `adf5e67d77922b3bfbffab62363f4b3d5c58a292` | 2026-07-17 | alan_cheng | fix(swarm): declare an object schema for async_tasks_list |
| 193 | `49969321b59c27f390a56f6de986508c95212eca` | 2026-07-18 | yan_zj | fix(context):remove chinese query translator |
| 194 | `1bf14832bc4e89a29ed0c090c96650c18b83b1b9` | 2026-07-20 | l_x_d | fix(skill_rail): persist session baseline and track runtime changes |
| 195 | `6fc3de5187ffb769349cc7cdca7a7d8167d933d2` | 2026-07-20 | zhanglongmin | fix(agent_teams): add empty schema for async task list tool |
| 196 | `8c5b537b20f8708eb50d588b9a6165de1ea0e3a6` | 2026-07-20 | z2tng | fix(agent_evolving): validate skill package install paths |
| 197 | `aa643dee3a26843e7e8da6b643388cfad2e36c39` | 2026-07-20 | z2tng | fix(evolving): refine skill creation and evolution triggers |
| 198 | `b7e5d29c5c791da3a32c73e2ea8410f12dce680d` | 2026-07-20 | openJiuwen-bot | fix(fs_operation):There is a memory leak issue with the file read/write lock. |
| 199 | `1df6d247d9aded0331d8c2c1771d5eb938585009` | 2026-07-21 | Ziliang_Zhou | fix(memory): resolve BM25 retrieval tokenizer defects |
| 200 | `71349cf958d92a0484eb3c7b7bc10060cddfa695` | 2026-07-21 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#71: fix(browser subagent): fix browser batch interact tool and allow browser suba... |
| 201 | `73b80e4c6d0224ee2ce3a4aa15c62d568e1f2412` | 2026-07-21 | yan_zj | fix(harness):1.Fix concurrent edit overwrite issue. 2.Fix chunked full-read detection for write. |
| 202 | `968f375e06c79082258a7d1ac48d200cd45fd5ed` | 2026-07-21 | z2tng | fix(evolving): stabilize skill evolution runtime flow |
| 203 | `63380f17e8516c641435192efeeb8af5e286705f` | 2026-07-22 | MengHan18 | fix(ascend affinity): integrate AscendAffinity KV Cache affinity management and lifecycle support |
| 204 | `7bbcdb1df0c17b5f1f07a33e53b6718a2b21f207` | 2026-07-22 | cycong_1 | fix(agent): main agent and subagent share artifact directory |
| 205 | `834a42c3ee059c931b6b55224413476da9edd42c` | 2026-07-22 | openJiuwen-bot | fix(operator): close rw lock probe before deleting database |
| 206 | `c9508cf35e8b94c889427134acf45f00c7d12664` | 2026-07-23 | chunbs_cbs | fix:使用浏览器工具检索之后，chrome窗口没有自动关闭，手动关闭导致后台僵尸进程 |
| 207 | `d6d480b6b267aaa1511e9c45fa31cb177eadc4d1` | 2026-07-23 | weichenhao | fix(team):Add checkpoint to the dispose method of team_harness |
| 208 | `ebd12498c21d28f933e61cb0406a43f9d9779791` | 2026-07-23 | alan_cheng | fix(swarm): correct send_message comms policy for teammate vs leader |
| 209 | `10bb3afde5e740ca850b256228408c709ca58f1c` | 2026-07-24 | karina0522 | fix(filesystem): avoid duplicate PIL decode/base64 encode in _read_image |
| 210 | `129f064f9f8cfa529b9674fda3935c5efba4834a` | 2026-07-24 | alan_cheng | fix(swarm): harden teammate ban on conveying messages via plain text |
| 211 | `4da89a734d0c2148ea81f66d4ffe5a9906c60742` | 2026-07-24 | wncbdj | fix(harness): Optimize the effectiveness of the goal function |
| 212 | `710b2b09963cb2c76ce2189c3f85c9a3a893a9e8` | 2026-07-24 | openJiuwen-bot | fix(skill): add checker to SkillTool's relative_file_path |
| 213 | `72b7fed3e782bcb95d4b3fad22b715ad121b878c` | 2026-07-24 | chenchunzhou | fix(agent-teams): add public TeamMonitor.get_task(task_id) query API |
| 214 | `bf87579a419768ccabe34e3351181f15e39dfbd7` | 2026-07-24 | wncbdj | fix(context_engine): Add exception handling as a fallback in the infinite loop |
| 215 | `f9a3cbe712e1c9fc6d84a23bd774d0c2aafd56e7` | 2026-07-24 | openJiuwen-bot | fix(security): allow SSL verification without custom cert |
| 216 | `15cbdb6ba259e8296accce272bec3bcc6f176a6a` | 2026-07-25 | tangfei0306 | fix(agent-teams): separate task failures from runtime crashes |
| 217 | `1b28ec45ca5f50e3e323ab5865e396c9fbdb6d92` | 2026-07-25 | wncbdj | fix(context): When canceled, save the message history |
| 218 | `9945d187ed81c2156a94747b6d1c3095e6483e60` | 2026-07-25 | m056838700 |  fix(browser subagent): add reset_active_browser_runtimes function |
| 219 | `e9ec6d770980e42fbcc23a3a66f57b495aecfe28` | 2026-07-25 | min_gitcode | fix(agent_teams): retry on pool-exhaustion timeout in retry_on_locked |
| 220 | `31c5733b447abf018260b8ed39ea0e8fdf55b803` | 2026-07-27 | wncbdj | fix(context): Continuously invoking the same tool triggers compression and throws exceptions |
| 221 | `6a821a451bae12d1e644a6e203df168779942c1b` | 2026-07-27 | wncbdj | fix(tool): default wake_offset_seconds to 0 when omitted |
| 222 | `82e508116dc4b5d90f35d50ec0d4a1ebdae83129` | 2026-07-27 | wncbdj | fix(llm): The model enhances pooling capability and reduces call latency |
| 223 | `894b49a5a676f6516b5d429c64a7665f4d5e7de0` | 2026-07-27 | LasVie | fix(harness): remove unsupported regex lookarounds from worktree schema |
| 224 | `f28f95a65dac8843ac60479f7979ae80ec932a27` | 2026-07-27 | zhaoyulu1 | fix(trace): remove changing llm inputs&outputs to str in otel_rail |
| 225 | `1f72b3e4dd595a00b1467465dabc0e98c0752d45` | 2026-07-28 | xiaowenzihwl | fix(code): fix code tool description |
| 226 | `3c97b565eca967107139627e0bd263bcb1117b82` | 2026-07-28 | alan_cheng | fix(swarm): make the user role explicit to every team member |
| 227 | `46a0a81bae61cd81860cb2bf580818c3c926b99a` | 2026-07-28 | wncbdj | fix(sub_agent): The description of input parameters misleads the model |
| 228 | `5b2dce304e13580e5f85ace36d8df4593a2c296c` | 2026-07-28 | tangfei0306 | fix(agent-team): rollback dependency agent-protocol way |
| 229 | `62ff5ccccd7e2b891fa6d1d2f853c2d1ad803436` | 2026-07-28 | alan_cheng | fix(swarm): skip observability rail when the extra is not installed |
| 230 | `6601f2294e93818ba958f00f22c1055e32c3b6af` | 2026-07-28 | l_x_d | fix(context): switch ContextProcessorRail presets to forked processors |
| 231 | `6dc5095deca0b6482522eabca166d27cef735adb` | 2026-07-28 | min_gitcode | fix(agent_teams): ignore shutdown members in unread completion gate |
| 232 | `788acbd2586aca724332e40f14dd56ebf6cdee49` | 2026-07-28 | LasVie | [mirror] openJiuwen-ai/agent-core#127: fix(openai-auth): retry transient device polling failures |
| 233 | `92d9c32473d6582e29a2ddc418efd90f80157db8` | 2026-07-28 | wncbdj | fix(goal): Persistently add time data to the goal |
| 234 | `997e0b55e7d84173e78b4de6558a5c8b73a404b6` | 2026-07-28 | chunbs_cbs | fix(llm): All think information of the MiniMax-M3 model is displayed on the front end |
| 235 | `b28b4f3a6248f116a66c97c8a73f2b89432dd3f5` | 2026-07-28 | tangfei0306 | fix(agent-team): rollback dependency agent-protocol way |
| 236 | `bf146316d6b1dbf1efb02cf90aebae8d0e30a7d6` | 2026-07-28 | l_x_d | fix(browser): restore tool result windowing in browser subagent |
| 237 | `d1065495120259057b04ba6bfce26678ab58ecb2` | 2026-07-28 | min_gitcode | fix(agent_teams): start human-assigned ready tasks |
| 238 | `d6cff034a64013cbb095a793a66f5297efeb44d4` | 2026-07-28 | zhaozixuan | fix: add support for yuanrong sandbox |
| 239 | `e57fd711c39958fcb2d9b13970afd1d158e21ce3` | 2026-07-28 | hwxiajing | fix(observability): reasoning span duration + reasoning/cache token capture |
| 240 | `e9d4c1296a4a7e3d3193ed71625d04e660b703af` | 2026-07-28 | min_gitcode | fix(agent_teams): support assigned autonomous tasks |
| 241 | `fe99da2188aa84cc848b70b62900cfe1bd9ff30f` | 2026-07-28 | gcw_6ZTnJdNN | [mirror] openJiuwen-ai/agent-core#91: fix(tool_call): The call does not have a timeout limit, resulting in … |
| 242 | `2a4c9fa23326747910aae1e171016a90e1543880` | 2026-07-29 | hwxiajing | fix(prompts): add type= mapping in teammate communication protocol |
| 243 | `31c6411f805c3c19075933369a7ab2fc842d5981` | 2026-07-29 | alan_cheng | fix(swarm): tell each member its own workspace path |
| 244 | `4674d6fc82ac9399fc3df0e3301bfd0c37766458` | 2026-07-29 | ubaidillah-ariq | fix(harness): add timeout for modality probe |
| 245 | `687be26c373d678f7ab199af14c196bd89831d36` | 2026-07-29 | chunbs_cbs | fix:openai preserve real finish_reason |
| 246 | `e79e0da51543fcc80168046163df828e190a1c4f` | 2026-07-29 | alan_cheng | fix(swarm): deliver team context on the admitted input |
| 247 | `f2f86d5e34074c58c3ed330c157661502d42d97f` | 2026-07-29 | openJiuwen-bot | fix(team): skip foreign session worktree recovery |
| 248 | `1bd6360c9f1b18f666f81fa3c105ea3a0f357308` | 2026-07-30 | gcw_6ZTnJdNN | fix(mcp): Resolve the issue where, in SSE mode, if the TCP connection with the MCP service is reset, the next call will attempt to reconnect |
| 249 | `483e316dada3e3262b899faa484f1e4e68fb4da0` | 2026-07-30 | hwxiajing | fix(observability): add gen_ai.prompt.N.tool_calls attrs for assistant messages with tool_calls |
| 250 | `495b3b9faee98f8be406500a11a979fcd85624ec` | 2026-07-30 | l_x_d | fix(harness): switch default processor preset to forked and enable compression recall |
| 251 | `4fd1ca2d61bcec5dcf5445261dd50d1c97434deb` | 2026-07-30 | wncbdj | fix(tools): Refine todo prompts to prevent over-planning and enforce partial updates. |
| 252 | `5cf1f6c7545c28a4e050e988d9303f432172ce02` | 2026-07-30 | xiaowenzihwl | fix(core): enable skill_tool to return directory tree for agent sub-skill discovery |
| 253 | `7ac99212d3ec49f36666b2b202c6438217fe927a` | 2026-07-30 | dunphy1 | fix(agent_teams): add team existence check in spawn_member |
| 254 | `8d54a2a3f6866e5935f97ee6885e1d214d1b82d4` | 2026-07-30 | dunphy1 | fix(swarm): access team_name through blueprint.spec in scheduler activate |
| 255 | `993813a4c69146489fdc12a1a86d007a686ed9e1` | 2026-07-30 | hwxiajing | fix(prompts): guard human_agent shutdown against active tasks |
| 256 | `e8f44ea0761a285ab9e844e84e4fdc7b594c3047` | 2026-07-30 | hwxiajing | fix(swarm): echo summary in send_message map_result to prevent duplicate P2P messages |
| 257 | `1f0a0508e2db91c915ce700d480c7f7d90b33727` | 2026-07-31 | min_gitcode | fix(agent_teams): reject leader messages to user |
| 258 | `4ba66bf1e265fc921cb28f2f8c37e96e7611dc06` | 2026-07-31 | l_x_d | fix(context): save cleaned context to session state on cancel |
| 259 | `485732c359a053a5dbe6cb71ffffb5a5d772348f` | 2026-08-01 | alan_cheng | fix(harness): keep prompt sections and rail tool ownership from clobbering |
| 260 | `06c4293236ad1b924585a317d5c19307d480e0d6` | 2026-08-03 | lamsaan_yau | fix(LSP): avoid blocking LSP rail initialization |
| 261 | `6403d86ca89a8dc71cec9eb67a212f1159f0a24d` | 2026-08-03 | zhanglongmin | fix(harness): use builtin list annotations |
| 262 | `70d1b482582a9b21bb3e63a0bf8b184dbf042af2` | 2026-08-03 | tangfei0306 | fix(agent-team): humaninbox can auto_start_member when target is none |
| 263 | `7c50c48da82854992382d642c643b38abf21c8d0` | 2026-08-03 | rolandyang | fix(evolving): docs(evolving): Supplement the self-evolving online RL documents Refs: [#1439](https://gitcode.com/openJiuwen/agent-core/issues/1439) |
| 264 | `a61bc5c2abf2e3a7c21c9db19747da14cf6a8b39` | 2026-08-03 | zhanglongmin | fix(agent_teams): bind external cli context to member backend |
| 265 | `660e9c5738c158c8f7c31eca92824e074bdd003f` | 2026-08-04 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#255: fix(context): stop counting base64 image payloads as text |
| 266 | `803d854e55f58865edf24f391ec51295381904c9` | 2026-08-04 | m056838700 | fix(browser subagent): improve browser subagent tools and action speed |
| 267 | `8c1654f85a7ca7dc7da32b2c7491ffba66a25d3f` | 2026-08-04 | l_x_d | fix(context): refresh built-in model context window table |
| 268 | `aeb88cd85cc43197a00615818f124e85cca053da` | 2026-08-04 | tangfei0306 | fix(agent-team): add hitt、trace、permissions docs |
| 269 | `c1092f0bef044da66d932d197c05179ba07b38a3` | 2026-08-04 | z2tng | fix(agent-evolving): tighten skill evolution contracts |
| 270 | `c64fc130d67f65567b557ceca1036d98f1f546ee` | 2026-08-04 | tangfei0306 | fix: preserve boolean substrings in condition variables |
| 271 | `cad85ec2114ef40409305c818031b209bbb1f955` | 2026-08-04 | l_x_d | fix(context): invalidate stale usage token baselines |
| 272 | `f46c9e7f2795ae9ba632d5278dedf90bfc96c9e5` | 2026-08-04 | openJiuwen-bot | fix(tiny-agent): require structured output for schema runs |
| 273 | `fc9f9f9ad7f4220dbaf55929c858e8a95be3c063` | 2026-08-04 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#288: fix(llm): drop top_p when temperature set for Anthropic requests |
| 274 | `1cf7ceb23cb9d4795aa1e9a1a7ba4d0bd97ed5fb` | 2026-08-05 | Ziliang_Zhou | fix(memory): support jiuwen memory(mem2.0) external provider |
| 275 | `4f4b73625c4cdca403eb691d87ce1bab33ea76f5` | 2026-08-05 | alan_cheng | fix(swarm): separate an avatar's controller channel from team user |
| 276 | `dff399b7b7590ec78ca9dc8a18dd5b8b2a9ac6cf` | 2026-08-05 | alan_cheng | fix(observability): resolve the trace root span outside its ContextVar |
| 277 | `09b26df18ef35cdc91e8085e6437a858f5542c83` | 2026-08-06 | alan_cheng | fix(observability): stop reporting unmeasured reasoning as instant |
| 278 | `214daf0e8a980c874b6af09b0066a15d26980170` | 2026-08-06 | hwxiajing | fix(llm): use safe _extract_reasoning_tokens in IntelliRouterModelClient |
| 279 | `4b4809343489f7ca3b48d5aab8993cc7c6b25998` | 2026-08-06 | alan_cheng | fix(observability): correlate llm.call spans by request id |
| 280 | `58d7284a767b7c033161b9e12cb8f62e5fa635c3` | 2026-08-06 | alan_cheng | fix(harness): pass the parent sys_operation to injected sub-agents |
| 281 | `5a5d7c619fa6547aad8d20776279b6655f82dd5d` | 2026-08-06 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#291: fix (mcp): stop dropping tool result content blocks |
| 282 | `61e6933b2c96520c054860965e226d1e82f478b6` | 2026-08-06 | yan_zj | fix: Apply prompt improvements and incomplete-read guidance |
| 283 | `7cae6bc500e72dd60ed11f79a8b1513c43fd3187` | 2026-08-06 | alan_cheng | fix(observability): stop double-counting cached and reasoning tokens |
| 284 | `c7ac99108668d57c93d1373e5061f925ad494688` | 2026-08-06 | openJiuwen-bot | fix(worktree): initialize worktree session before task scheduler |
| 285 | `cb26cd08e8556687b3db23c6e43ce0c45363204b` | 2026-08-06 | alan_cheng | fix(observability): nest a sub-agent under the tool call that dispatched it |
| 286 | `e3048c644c12fb953ac4933eaf599b1505ef5bfd` | 2026-08-06 | alan_cheng | fix(observability): trace every sub-agent, and nest rounds under their invoke |
| 287 | `4e3e1a743ac932bc44178e5cf1bfeece33a8f140` | 2026-08-07 | min_gitcode | fix(team): clarify team_desc is context, not a directive for members |
| 288 | `6bd66b12a8f054a3196a5eb5e64b6b52c68af45e` | 2026-08-07 | alan_cheng | fix(harness): honor blank endpoint overrides for paid search |
| 289 | `709eb5d29ee1cf157d3288762a93fa4cfbc77c39` | 2026-08-07 | openJiuwen-bot | fix(tiny_agent): make tiny-agent structured output retries model-compatible |
| 290 | `9d358de5658a50bd9448044124c83d2b41f80df0` | 2026-08-07 | wncbdj | fix(goal): Change prompt injection to dynamic injection |
| 291 | `c1a58c097762ac136740c0e6e7b4276f34ac1f9d` | 2026-08-07 | alan_cheng | fix(swarm): hide swarmflow from the leader when the tool is gated out |
| 292 | `bf0a3eb2c70fcbae404403530519ca02e7fc4692` | 2026-08-08 | l_x_d | fix: persist React context after interrupted runs |
| 293 | `cccb9ae86f3efd08a1b88949ed560d495288311b` | 2026-08-08 | alan_cheng | fix(swarm): scope schema creation to the static team tables |
| 294 | `17bcd1d4abf5f4399f7d4661d4ea4b26f9b5d472` | 2026-08-10 | z2tng | fix(trajectory): establish canonical model and persistence boundary |
| 295 | `3a6deda8a10f4dea34e636c0ba10ee44389807c8` | 2026-08-10 | min_gitcode | fix(agent_teams): default to autonomous dispatch and disable task verification |
| 296 | `7c23401b972d73b6b64e4581b0d1b099f66aa755` | 2026-08-10 | lin-xiaoyu | fix(symphony): 修复质量评估模块接口 |
| 297 | `dc57d1234b7edbad2c4da3db676ac44f3ad1292f` | 2026-08-10 | lin-xiaoyu | fix(symphony): 修复Windows系统SKILL目录扫描失败的问题 |
| 298 | `e9cb1eac6efc055097a2ee9d1087d71740b12ed5` | 2026-08-10 | l_x_d | fix(context): honor tiktoken counter switch and turn off tiktoken |
| 299 | `f623b57ceb6c5053f6e222c76d01184747e57068` | 2026-08-10 | lin-xiaoyu | fix(symphony): 修复模型不可用异常未返回给swarm图谱构建的问题 |
| 300 | `f68d4c31d070ad72c2fbfca6af231f3318ffacad` | 2026-08-10 | openJiuwen-bot | fix(browser subagent): add current page state and durable context layer for |
| 301 | `fcc45655847b84a6b87283789b2bd2d250bb827d` | 2026-08-10 | lin-xiaoyu | fix(symphony): 图谱构建时，单个SKILL异常不影响整体图谱构建 |
| 302 | `70a833c8aa8e75eb85bb052bfc4beb13f2be3819` | 2026-08-11 | 2401_87256159 | fix(swarm): address reviewer feedback evolution findings |
| 303 | `816376e2aa2c14b1c9f22363ec061ae4c97df240` | 2026-08-11 | layShang264 | fix(symphony): preserve JSONL records in incremental builds |
| 304 | `939c73e66525f34512802706ac37db58a218a4f9` | 2026-08-11 | karina0522 | [mirror] openJiuwen-ai/agent-core#395: fix(harness): store agent-history under jiuwenswarm dir |
| 305 | `44b5edce8c178578f8273d4f65ad96463182bc81` | 2026-08-12 | zhaozixuan | fix: excluded_commands support hybrid command |
| 306 | `526bff1c8c39cebcfb4964e16b0925e7e7b21568` | 2026-08-12 | wangcan103 | fix(swarm): attribute burned tokens to failed agent events |
| 307 | `94e10cb6102c36fe78a64547957c0def97299273` | 2026-08-12 | wncbdj | fix(rail): Rectify model anomaly detection rail |
| 308 | `bbf5a80c01e2bc384cef82ad8dac01fe2b6642db` | 2026-08-12 | l_x_d | fix(context): extract text from structured user content for compression recall query |
| 309 | `534e26095e9949772d53c00bdbffcb750c1a83c6` | 2026-08-13 | z2tng | fix(trajectory): unify message reconstruction for online evolution |
| 310 | `6f892f0ac796265dbc5b323898528ac5027ac19b` | 2026-08-13 | openJiuwen-bot | fix(agent-teams): reconnect hybrid publisher after idle disconnect |
| 311 | `73cfc3bc8b74386c5d91c6d1ff11f50e6df510df` | 2026-08-13 | layShang264 | fix(symphony): 修复等价候选补齐异常 |
| 312 | `886654fb249103f58082eab605a9be93d22ae44b` | 2026-08-13 | chenchunzhou | fix(worktree): serialize concurrent create/remove and enable core.longpaths |
| 313 | `8f9a298e20c59e871607ac09fd7a6e7fd0a8e445` | 2026-08-13 | lin-xiaoyu | fix(symphony): 模型返回错误增加重试机制，修复单次模型失败导致最终汇总失败的问题 |
| 314 | `ac2feb2c01326e0bd62570c144390cf00659d7a8` | 2026-08-13 | sodawlh | fix(swarm): reject duplicate checkpoint names team-wide |
| 315 | `c2984b354e400c4fc0a47b8f0baf0ade4c9778cd` | 2026-08-13 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#356: fix(context_evolver): add offline memory storage, prompts, and role-t… |
| 316 | `f0e7ad10b130e0dda864d99ba2dfd559a00900e9` | 2026-08-13 | wangcan103 | fix(swarmflow): handle pydantic objects in async tool result rendering |
| 317 | `1c4f560b4329decb90bae50fa8de0c7456c7cdf8` | 2026-08-14 | wncbdj | fix(rails): match tool-loop compact on tool set and args only |
| 318 | `27a2e99b28d1aae92a984244c248bfaee86f24ad` | 2026-08-14 | yan_zj | fix(tool):Introduce `tool_search` and set infrequently used tools to `deferred`, so they can be discovered through `tool_search`. |
| 319 | `60333697703784cf1707b7ec16151bdda550e67b` | 2026-08-14 | ldstyle8 | fix(task-loop): cancel scheduler task on round timeout |
| 320 | `a485d799d0f54a06785aa16845c8e1b237c0a726` | 2026-08-14 | min_gitcode | fix(agent_teams): surface external cli crash diagnostics |
| 321 | `c5937bc39648c8453c19537805ea8bff7ff41609` | 2026-08-14 | zhangmengyin | fix(memory): adapt to the new namespace and interface names of jiuwen_memory |
| 322 | `755622e76ec299a057001a09d7992dcb2a109f82` | 2026-08-15 | min_gitcode | fix(agent_teams): make team deletion idempotent |
| 323 | `8df2ac860bcabb8aa3c8f7037b04f0c21bd5c00f` | 2026-08-15 | alan_cheng | fix(swarm): stop inferring disband intent from an empty roster |
| 324 | `8ec439e6d936be532108e6abc7c1b37d235900e5` | 2026-08-15 | alan_cheng | fix(swarm): allow reassigning a task before it starts |
| 325 | `a951363621cb15ac2b9697d9f1a2faa2760ab82e` | 2026-08-15 | alan_cheng | fix(harness): make the nested-skill hint reachable and actionable |
| 326 | `f84b65c1903a961d0154b8cf55ff1b7001ec87f5` | 2026-08-15 | alan_cheng | fix(harness): discover skills filed under grouping directories |
| 327 | `41220948314e3722aede98469e422e248e9542aa` | 2026-08-17 | z2tng | fix(evolution): use typed image config for reviewer |
| 328 | `75adc2b44edf91bd808b78a8df1153fb96fec4bb` | 2026-08-17 | MengHan18 | fix(ascend-affinity): Fixed issues encountered during vLLM joint debugging |
| 329 | `8b619852903634d55a1835be7f7f7d0f6bb33b06` | 2026-08-17 | tangfei0306 | fix(agent-teams): split inline member mentions |
| 330 | `90358ad36cbac5aea6f3b2d966dea5fd8a851a8e` | 2026-08-17 | z2tng | fix(evolution): resolve team review scope from root span |
| 331 | `ee9d00a2aec56b29e5469272e2dc81170b00f892` | 2026-08-17 | maxmadmax | fix(harness): guard fresh input lifecycle |
| 332 | `f1603c9d710357ad36e6c277ec2626a2f6136bef` | 2026-08-17 | lin-xiaoyu | fix(symphony): 补充图谱构建过程中，输出关系详细进度 |
| 333 | `6d5c669b69473dce9d2f7272c3ead17d1e5a3819` | 2026-08-18 | ray_le | fix(mcp): Pass the real reason of the failure to connect the mcp server to the downstream error_message |
| 334 | `71d0be95626046be646ec81baf2e3dea9e81d622` | 2026-08-18 | min_gitcode | fix(agent_teams): improve external cli diagnostics |
| 335 | `9e33f451ad9aa565312aec3ab844ee457867266e` | 2026-08-18 | wncbdj | fix(deepagent): Add the plan tool to the exposed whitelist |
| 336 | `ab17fa07f7c973c44f649452eb55272d9352a45f` | 2026-08-18 | hh0205 | fix(observability): stamp agentteam.agent.role with real role value |
| 337 | `1d144d5a898517cbfc3d557a9a9a93d7f8825dc5` | 2026-08-19 | lamsaan_yau | fix(harness): move LSP initialization off model path |
| 338 | `38975d79ce6cbe5e4d5cfb2d78d75bead52ecd78` | 2026-08-19 | wncbdj | fix(llm): api_base is no longer hard-coded to v1 |
| 339 | `5b8702e768bb8fe50d0b3bf088570a97d705f1e4` | 2026-08-19 | yan_zj | fix(skill):Move the tools from the skill section to the top-level tools. |
| 340 | `5e597c4b67ae177bfddae246d43a5debbbf36487` | 2026-08-19 | hh0205 | fix(agent_teams): drop unreachable team workspace mount for external CLI members |
| 341 | `71503ad6ccddc68ee0848f9c84326c4fa2ab73dc` | 2026-08-19 | zhaozixuan | fix: add jiuwenbox-conch mode |
| 342 | `86df2c5aa19f8d82ef4e10c9d67db3e46412d30b` | 2026-08-19 | wncbdj | fix(deepagent): Add the plan tool to the exposed whitelist |
| 343 | `b294e146c1f30868747e32fc516d768d5a1367d4` | 2026-08-19 | hh0205 | fix(agent_teams): drop unreachable team workspace mount for external CLI members |
| 344 | `cb07b458b6aeacfae0078806619016ff6694d049` | 2026-08-19 | yan_zj | fix(skill):Move the tools from the skill section to the top-level tools. |
| 345 | `d6292ad7369bd80cc620e25a2810cc341d34a652` | 2026-08-19 | lamsaan_yau | fix(harness): move LSP initialization off model path |
| 346 | `ea60e721f20b5b5616f388722f3e015c344cf391` | 2026-08-19 | lin-xiaoyu | fix: avoid ignored exception continue in disabled thinking fallback |
| 347 | `458306a7e2ef3a745e1d02c94d514c8a36508077` | 2026-08-20 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#781: fix(context_evolver): add level 2 (pairwise collaboration) in l2.py |
| 348 | `74b583e7cb3155ab3b87910f249d09d3a38195f6` | 2026-08-20 | openJiuwen-bot | fix(react-agent): strengthen identity prompt to prevent repeated tool calls |
| 349 | `8231df83bd03006c9e65ce1334af45659574948a` | 2026-08-20 | lin-xiaoyu | Fix: Resolved an issue where the Zhipu model no longer supports disabling reasoning; passing thinking.type as "disabled" in API requests now results in an error. |
| 350 | `b6aef66a7649ab6a2f8708e348095a8fc19ad511` | 2026-08-20 | alan_cheng | fix(agent-teams): harden external harness turn semantics |
| 351 | `bce41b60926bdf22227dc8510038fe84c920a57f` | 2026-08-20 | alan_cheng | fix(agent-teams): stabilize external harness wire protocol |
| 352 | `d568e2c1dc6a0eddc67dd6d2a1b570f3c7d31a97` | 2026-08-20 | alan_cheng | fix(agent-teams): enrich external harness event contracts |
| 353 | `da021f994908e6459177f408bbbfbd71e9f43d83` | 2026-08-20 | min_gitcode | fix(agent_teams): wire external model config into codex and claude CLI members |
| 354 | `22e349af414028a688ef94bedb263e3c8b3ddca0` | 2026-08-21 | 2401_87256159 | fix(agent-teams): preserve dispatch defaults |
| 355 | `4f2c29c34899a45cec56a7d765fcc95e4002f60a` | 2026-08-22 | yan_zj | fix(attachment): optimize attachment logic and inject as system messages |
| 356 | `02cc37e9ec9b07eb33e53189330ee73fba198cd1` | 2026-08-24 | Ziliang_Zhou | fix(memory): guard mem2_search top_k against null to prevent int(None) crash |
| 357 | `5774362dd8bc83204ef1825c9aea944ab7cea8ff` | 2026-08-24 | liulili-huawei | fix(agent-rl): fix several issues in online sft self‑test |
| 358 | `7594f6e3b3107b65508cecc740d6673e2710570c` | 2026-08-24 | hh0205 | fix(team): 优化第三方 CLI 成员产物文件路径处理 |
| 359 | `8535d0f8182e9e4612523a12162c762b06d613f1` | 2026-08-24 | openJiuwen-bot | fix(teams): hide internal tool names from team plans |
| 360 | `0377afe21293ef08d9dff72c4f293dd03fd1f59e` | 2026-08-25 | alan_cheng | fix(observability): preserve realtime subagent trajectories |
| 361 | `6390bbf230f4ea2dd7446bc01ee882e6a4413d4c` | 2026-08-25 | hwxiajing | fix(agent_teams): skip migration scan for new session tables on bind |
| 362 | `69ce2182fc00d9621f348073107873c913b17496` | 2026-08-25 | wncbdj | fix(llm): Modify the default value of top_p |
| 363 | `51c55c33a4920cd43b5940672aa7dc6c4fcf84dd` | 2026-08-26 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#365: fix(evolving): stop staging sandbox payloads at predictable temp paths |
| 364 | `79032696d3bb9ac4cbdf9b48d918e6f3eb4eb46b` | 2026-08-26 | alan_cheng | fix: isolate subagent observability roots and preserve request messages |
| 365 | `dce36fc8a3a4db8b914687d8bb63a7fffa1405f1` | 2026-08-26 | yan_zj | fix(tool_search): add tool_call for unified deferred-tool execution |
| 366 | `1254a180dd20db2c12801d2a580ec91ec9dc8419` | 2026-08-27 | l_x_d | fix(context): allow context recovery during streaming |
| 367 | `277f118d2de8c64ecd024086697c68036f6ab623` | 2026-08-28 | hwxiajing | fix(agent_teams): write evolved md value to db at spawn_member time |
| 368 | `30ff4072ae7a83b0be7acb070384ac83c64220bd` | 2026-08-28 | hwxiajing | fix(agent_teams): attach shared workspace cache to external CLI member backend |
| 369 | `4cd577e19f97f8337d0d7776b032246536a46612` | 2026-08-28 | m056838700 | fix(browser): improve runtime progress and recovery |
| 370 | `c06091c5194a895361f8e34a5fc0a84e88b269a9` | 2026-08-28 | wncbdj | fix(llm): The OpenAIAccount uses the openai_account_oauth authentication method |
| 371 | `013879dcc2c466ca0fb59c6d9a90876cd8b89f4c` | 2026-08-29 | alan_cheng | fix(observability): record tool failures on both paths |
| 372 | `2362ee48f66bd50f913617808c1675067d9b51ac` | 2026-08-29 | alan_cheng | fix(observability): keep injected system turns in the chat history |
| 373 | `36c0e9de699aa1527933f19ce3674040feafc3ec` | 2026-08-29 | alan_cheng | fix(observability): record LLM messages only as standard attributes |
| 374 | `526f7c69df47f9625433ae25fe6e14359dbc8657` | 2026-08-29 | alan_cheng | fix(agent-teams): record harness bridge messages as standard attributes |
| 375 | `783a186b50e217769e85090e50c1af40f949a4b3` | 2026-08-29 | alan_cheng | fix(observability): mark epoch context baselines |
| 376 | `798c3c95660f4764fdbf966b5249e37d5a5791fe` | 2026-08-29 | zhangyaomaggie | fix(harness): support the creation, loading, and execution of templates and plugins |
| 377 | `9e098138afb8d829e3832d2cb29d583e350c3007` | 2026-08-29 | alan_cheng | fix(observability): number every LLM request, root span or not |
| 378 | `af3970ff397c231dff26eff1e7c3764b53a63e4e` | 2026-08-29 | alan_cheng | fix(evolving): rebuild tool calls when flattening standard messages |
| 379 | `fe9c32c30f03ad2623432201be7615d80a091902` | 2026-08-29 | alan_cheng | fix(llm): keep mid-conversation system turns in place on Anthropic |
| 380 | `1d257566987461fcaa2d11c19e8e3678f9d0a54f` | 2026-08-31 | yan_zj | fix(tool_search): prioritize exact tool-name matches |
| 381 | `38da8620dda94aaf6ad597d7f40ae8d4e376469b` | 2026-08-31 | l_x_d | fix(context):add tokenizer-aware usage and KV cache telemetry |
| 382 | `4139e10a8064ad84afbba617b635adb3183d1d14` | 2026-08-31 | hwxiajing | fix(team_workspace): resolve cleanup via link and drop dead leader refs |
| 383 | `abbcd424e2e306fe440e405ef170d647974bd59a` | 2026-08-31 | alan_cheng | fix(harness): stream interactive resume rounds |
| 384 | `d0265ec13335de5f7219620907f6bf4e8b45248d` | 2026-08-31 | alan_cheng | fix(llm): restore local helpers in cache-creation extraction after rebase |
| 385 | `d2525b6ee339a3511a891b6f59fbf8b4cf561bea` | 2026-08-31 | alan_cheng | fix(observability): encode OTLP enums as integers |
| 386 | `da1a248e48bc6e95a3c57918d0130df6fd26f902` | 2026-08-31 | yan_zj | fix(attachment): inject as user messages |
| 387 | `df7c717df653427c929e768d8d30b242a95aec33` | 2026-08-31 | alan_cheng | fix(observability): emit standard cache usage fields |
| 388 | `e72e209e04476ff7f1d9355259f497c0dafb7035` | 2026-08-31 | alan_cheng | fix(observability): stabilize browser trajectory context |
| 389 | `fa3eac48a5cdf1fe509841c59f0210684d52bb6f` | 2026-08-31 | zhanglongmin | fix(agent_teams): make Codex PID probing safe on Windows |
| 390 | `0b1371bf0a8669f8a1e3d98dc0cd33eb0250c057` | 2026-09-01 | l_x_d | fix(rsi): add artifact provider contracts |
| 391 | `26b75696889946dae9881ac64daf897e0708a937` | 2026-09-01 | yangzequ | fix(rsi): expose AutoHarness downstream symbols from openjiuwen.rsi |
| 392 | `7215321be991e73aa4123d4e426f6a9a0651b7d4` | 2026-09-01 | l_x_d | fix(context): use three-character fallback for token estimates |
| 393 | `91469d592c471e49bbc1ab40c8e0da4c810a09a6` | 2026-09-01 | m056838700 | fix: decouple native image support from vision tools |
| 394 | `9f18242e08544dcda97fed4b57225c5f005934b5` | 2026-09-01 | yyuse | fix: 修复team模式下_parse_level函数导入错误问题 |
| 395 | `d28ec7ca81fb61289f06d44bc0e818da7122fe86` | 2026-09-01 | yan_zj | fix(attachment): Decide the message role based on the agreement and provider |
| 396 | `d73ba549b555e57174018e5c89feacd37d0bf0e1` | 2026-09-01 | alan_cheng | fix: repair six failing unit tests |
| 397 | `f186dc27702fbd527696d555005dd48c713c26a0` | 2026-09-01 | lin-xiaoyu | fix(symphony): fix the issue where disabling skills fails due to fetching skill names as ASCII codes in the skill map |
| 398 | `fd30965b4e1622accef82d212716787c3fe36da9` | 2026-09-01 | layShang264 | fix(symphony): 保留图谱构建和增量更新的原始异常 |
| 399 | `e8ebffa8068357b10539a2b122113042908d187d` | 2026-09-02 | l_x_d | fix(rsi): align artifact contracts and stage events |
| 400 | `5930a8cf442fda322280c28741f9311651a2c5d7` | 2026-09-03 | openJiuwen-bot | fix(harness): route 202/499 and JS-shell pages through jina reader |
| 401 | `691347b97ef5089a0b0caf7861c98cb9ad35aa2b` | 2026-09-03 | zhangyaomaggie | fix(harness): fix plugin‑loading tool disappearing in conversation after hot‑swapping model |
| 402 | `716c183e58c2f54b232a060b6f42a4c4664d2c52` | 2026-09-03 | z2tng | fix(agent-evolving): add evolution and trajectory examples |
| 403 | `721600e33a18d229a311d420cbdad5bba7a63270` | 2026-09-03 | min_gitcode | fix(agent_teams): preserve external runtime failure details |
| 404 | `78243326c4661d6a04ada1819bccb20979a1121d` | 2026-09-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#28: fix(async): eliminate event‑loop blocking caused by time.sleep and synchronou... |
| 405 | `7915973171351c3e8b673e863d00b01eb16d6ccd` | 2026-09-03 | yan_zj | fix(tool search): Rebuild the BM25 index when tool parameters change. |
| 406 | `9630f3cf0bc0e909e7d3a61c2a1d3bb9e17f641c` | 2026-09-03 | alan_cheng | fix(observability): clarify OTel attribute truncation |
| 407 | `a50722a401a84f9bf004926dccc62595c9128b9c` | 2026-09-03 | xutuo | fix(symphony): 优化symphony工具返回结果 |
| 408 | `b48e26a6aad57360ab263442a0928115f408e14c` | 2026-09-03 | openJiuwen-bot |  fix(errors): add StoreError and pregel super-step status code |
| 409 | `b79d15901c2506bb9983b54369945c26783f704d` | 2026-09-03 | openJiuwen-bot | fix(harness): recognize serde image rejection in image modality probe |
| 410 | `bc15720c20f341695bc4c0b3db2e3decf2c30f72` | 2026-09-03 | wncbdj | fix(llm): Adaptation for Anthropic Version Upgrade |
| 411 | `bd576fc4d23b25a065af1ca677e67e90d3f210f0` | 2026-09-03 | liulili-huawei | fix(agent-rl): tolerate completed Ascend SFT teardown |
| 412 | `c6482ec28a5150412ef1d508b39fd04b52d5dc5a` | 2026-09-03 | alan_cheng | fix(observability): preserve complete system prompts |
| 413 | `d32a2274a1c64dec51e47b116281353487265d99` | 2026-09-03 | hwxiajing | fix(workspace): direct artifacts to shared outputs and drop .team routes |
| 414 | `08b77956fc05e803192ef0c26704b4e363196c1d` | 2026-09-04 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1130: fix(context_evolver): add level 3 (team collaboration) in l3.py |
| 415 | `46b288b5f1323e399447a10964b797b8300f8e71` | 2026-09-04 | alan_cheng | fix(observability): keep a resumed step's identity a stated fact |
| 416 | `74fa61096bf1043bc293f6ade9288c5a7305e3ca` | 2026-09-04 | yyuse | 修复内置安全规则不生效问题 |
| 417 | `93e9ddf6069580094690c0c56b06044d585fb017` | 2026-09-04 | weixin_45721752 | fix(trace): Fix OTel trace breakage, missing component.name and the issue where memory variable inputs is None |
| 418 | `9cb5f5e9192162f21a72d4aa59a64ef2f6583a29` | 2026-09-04 | hwxiajing | fix(workspace): 产物指向共享 outputs、去 .team 路由 |
| 419 | `bbe17039bad900e4fb44e715ee2483028b174f33` | 2026-09-04 | min_gitcode | fix(agent_teams): disable codex request compression for external endpoints |
| 420 | `2105c5e1cd4332cdd7e6c3c23055928b16dc299b` | 2026-09-05 | alan_cheng | fix(observability): classify trajectory spans by GenAI operation |
| 421 | `b00822ea563f0c29770299fbc3a58d31ba7f1ac3` | 2026-09-05 | birfied | fix(rsi): three program_opt defects surfaced by an AlgoTune polynomial_real run |
| 422 | `b90dbeccfca8d1d413936bff9dbbd619892bb4cd` | 2026-09-05 | alan_cheng | fix(harness): point symphony graph evolution at the converged semconv keys |
| 423 | `02c0fd9af08b7f9cb0e85bd6d395245814d85124` | 2026-09-07 | l_x_d | fix(rsi): bridge paper models and publish stage progress |
| 424 | `05db325217db05f67be56ed91b709e2052b6d643` | 2026-09-07 | m056838700 | [mirror] openJiuwen-ai/agent-core#1147: fix(subagent): route browser through synchronous task tool |
| 425 | `05fea89388e239bf547e083a80a71ffb015489de` | 2026-09-07 | xutuo | fix(symphony): 优化检索工具 |
| 426 | `33675f991d88cd924770cb0c1b1f9e0cc73bb387` | 2026-09-07 | ray_le | fix(rsi): emit structured per-case and candidate stage events |
| 427 | `41561a83e6c09c1070062c7338bc063ec5f6cd70` | 2026-09-07 | min_gitcode | fix(agent_teams): bypass Codex approval by default |
| 428 | `6705856618e76a7c179730bbcaeb99bd142bbca2` | 2026-09-07 | AFSDFASD | fix(rsi): Align and optimize registry and native Plugin loading |
| 429 | `73ff714765b41ce5934537aa90ac0781ff49e50e` | 2026-09-07 | AFSDFASD | fix(rsi): reconcile upstream stage events with epoch workflow |
| 430 | `cd5eba43c448d4bb7a9a805997fcc5545126191e` | 2026-09-07 | zhiqiuzoeyye | fix(rsi): fix paper score missing issue |
| 431 | `005f56488eff87deefa8ebf5e238a700351f60d1` | 2026-09-08 | min_gitcode | fix(agent_teams): use stable Jiuwen provider for external Codex endpoints |
| 432 | `01a08c130fc734c5d3aa638b1991805598fcc0f5` | 2026-09-08 | wncbdj | fix(llm): deepseek adds a reasoning_content field to the context based on the model name |
| 433 | `0af325ebe9f891e53fa211dae3392a4aff9d923d` | 2026-09-08 | z2tng | fix(tool-search): preserve deferred tool interrupts |
| 434 | `1c22b9f273757f7625c044c0e3842ff1c9bbb0b1` | 2026-09-08 | yyuse | fix(guardrail): permission engine 统一收编命令行安全检查 |
| 435 | `32d138adfbba0b70a3e62ff929f3f20f77a4164a` | 2026-09-08 | ray_le | fix(rsi): support cooperative task termination |
| 436 | `337dc2298c67194991cbcd887d7e218b90e88daf` | 2026-09-08 | l_x_d | fix(rsi): preserve paper evidence and task resources |
| 437 | `4665c60802d982d6dd2499bffe3a4d32ce65e276` | 2026-09-08 | yyuse | fix(guardrail): permission engine 统一收编命令行安全检查 |
| 438 | `4bf73be1c92e673fcea0b86fcad6e9b297940d91` | 2026-09-08 | AFSDFASD | fix(rsi): Align the dataset, ground‑truth scores and diagnostic evidence chain |
| 439 | `7ac20d2365aadb32fd72c351190bc697a93d4d63` | 2026-09-08 | xutuo | fix(symphony): 优化list提示词 |
| 440 | `7b2cc03d64fb26a1d1c829ec862b5236c5c449c5` | 2026-09-08 | cycong_1 | fix(subagent): make closed subagents resumable after session adapter eviction |
| 441 | `8105a3629a1d9dd62b3a60098e0c367f135a5138` | 2026-09-08 | min_gitcode | fix(agent_teams): clarify external failure recovery |
| 442 | `b2f0cfde0d618ffdcdfb469c3f803645392ec16a` | 2026-09-08 | l_x_d | fix(rsi): refine no-proxy source fallback guidance |
| 443 | `b95148b2bf21d135260c2b0a6d266a07eec8878d` | 2026-09-08 | z2tng | fix(tool-search): preserve deferred tool interrupts |
| 444 | `bf1032f7e1d6c9b8b445d01ec418264bbdfa86a2` | 2026-09-08 | AFSDFASD | fix(rsi): prepare native skill rails and align epoch event snapshots |
| 445 | `c1daffc25cea59f8009ce6c2c1e88d4249610309` | 2026-09-08 | wncbdj | fix(harness): rebind goal manager after hot reload so protocol still injects |
| 446 | `c1e9f8b334950c6cc97112561f5719526c923f1d` | 2026-09-08 | cycong_1 | fix(subagent): make closed subagents resumable after session adapter eviction |
| 447 | `c285c703a4b24a3a59fdb658853046b864b5c1ba` | 2026-09-08 | birfied | fix(program_opt): false judgment of zero‑timeout count, three causes for "no files modified", card iterations |
| 448 | `e45fbeb57023525f03b19bcec926f1b8c7a4bef3` | 2026-09-08 | openJiuwen-bot |  Fix(graph,agent): typed errors at pregel boundary, tracebacks in tool invoker |
| 449 | `fee4e34c8afbfb45371e28cbb1b1cb2d8b61c2f3` | 2026-09-08 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1027: Fix hung agent loop: no-progress stop, vision fallback, outer-round cap, and ... |
| 450 | `14a7fe2d0a2bf0cf2a25abd90f05f5ae6a9bf2d5` | 2026-09-09 | m056838700 | [mirror] openJiuwen-ai/agent-core#1253: fix (search): Restrict paid search to configured providers and update documen... |
| 451 | `196ff61b3cd550c569ff686b8569b24485626a19` | 2026-09-09 | chenchunzhou | fix(swarm): bind swarmflow run lifecycle to the embedding team runtime |
| 452 | `26726e22d4ad7cc7d22b3250b66b575ac62af099` | 2026-09-09 | alan_cheng | fix(observability): keep compaction prompts out of the conversation chain |
| 453 | `35f85ff167081320113e608ebc73126c39f18967` | 2026-09-09 | zhanglongmin | fix(agent_teams): improve external member recovery |
| 454 | `38bc4f13ed71a11548b855e9f3c203203f1fe890` | 2026-09-09 | zhiqiuzoeyye | fix(rsi): loose paper upload verifications |
| 455 | `49fe9be336515939db739fafe5a8cb1e4d3d2e46` | 2026-09-09 | zhiqiuzoeyye | fix(rsi): loosen paper scoring ingestion validation;  loosen topic survey source and finding validation |
| 456 | `4b8bf501dbb7ff20e7a9393d085853134f3fb351` | 2026-09-09 | alan_cheng | fix(swarm): ratify auth fallback with the host before committing to it |
| 457 | `5b25eb85a808a97ef17ae3441a896e4f77c3e502` | 2026-09-09 | AFSDFASD | fix(rsi): align LLM judge pass threshold across optimization |
| 458 | `5d7ba67bc850923a9a59bac4ce436ff7dad7721a` | 2026-09-09 | alan_cheng | fix(observability): correlate a compaction by its step, not by a request id |
| 459 | `69812b9eb620943b0ed29f76bea25e4ab8ae4607` | 2026-09-09 | alan_cheng | fix(swarm): queue codex steer until the sdk turn handle exists |
| 460 | `71924591bd8193771d3996fd0d733478cecb6511` | 2026-09-09 | alan_cheng | fix(swarm): initialize the deepagent harness before starting its interaction loop |
| 461 | `ac484c50d3ad489ed17f0733d6df6d12d7f7ba17` | 2026-09-09 | openJiuwen-bot | fix(session): restore current_session when ContextVar tokens cross Contexts |
| 462 | `c5280e201b1533f1b439537f7d7c42282beafdcc` | 2026-09-09 | Ziliang_Zhou | fix(memory): adapt jiuwen_memory_provider for the new mem2.0 code |
| 463 | `c679ea135dea6d514d64df528fd21cf18772caca` | 2026-09-09 | AFSDFASD | fix(rsi): recover judge output and isolate case resource cleanup |
| 464 | `cf4256175e822156008cf281482b6787a3cf7f82` | 2026-09-09 | AFSDFASD | fix(rsi): normalize nested judge references and grouped rubrics |
| 465 | `d5124c789e1fec7751ea3adfc2d20fb6d55d44d9` | 2026-09-09 | zhanglongmin | fix(agent_teams): report Codex auth retries before fallback |
| 466 | `e552947504d39a3b2f311e08cf5fbe080c5bedcc` | 2026-09-09 | AFSDFASD | fix(rsi): support natural language scoring rubric |
| 467 | `09ef9c147cca97f30e2ef57ec9b0292d2dde498f` | 2026-09-10 | wssq_2026 | fix: allow fastmcp 3.x and declare opentelemetry-sdk to drop vulnerable diskcache |
| 468 | `0c481e4aa0007af2bf9f92a3df332568e25c6037` | 2026-09-10 | zhiqiuzoeyye | fix(rsi): show a friendly reason for a rejected/blocked tree node |
| 469 | `1717937b5e87916e73dcb435a79005eaa765272d` | 2026-09-10 | l_x_d | fix(rsi): stabilize paper optimization pipeline |
| 470 | `1c49af1eafd5c7d814d11194f02a26c2fc71a5b5` | 2026-09-10 | zhiqiuzoeyye | fix(rsi): disable pdf hard validation in report module, allow tex to be final output |
| 471 | `4b3860b5c804b60cfd376826f54bfad5fad393fd` | 2026-09-10 | openJiuwen-bot | fix(harness): return write_file errors instead of ENAMETOOLONG crashes |
| 472 | `621dba5a0a4f25d1864525468a1115d8440579ec` | 2026-09-10 | AFSDFASD | fix(rsi): Unify max_tokens to 100000 in the RSI model loading layer |
| 473 | `77521f8ea1d2c447c96e122c068b9ec46b51f722` | 2026-09-10 | hwxiajing | fix(swarmflow): parallelize abort_all to shorten the pause critical path |
| 474 | `92340d0c6e0a19193c355f1acfe532442382d077` | 2026-09-10 | openJiuwen-bot | fix(harness): break stream cancel RecursionError and cancel on aclose |
| 475 | `a40473d5a8c9163b2f102c5d8935a610b98909cc` | 2026-09-10 | AFSDFASD | fix(rsi): Backend disables Thinking strategy, which takes precedence over frontend model settings |
| 476 | `a586d48aed66940c5df1d8547a6161925a8d8cbf` | 2026-09-10 | wncbdj | fix(llm): top_p and temperature no longer have default values |
| 477 | `c61caeec3afb61b6a29cdd3c476f790966bcebd1` | 2026-09-10 | z2tng | fix(trajectory): preserve resource and scope attribution when trimming |
| 478 | `e5c818e2b9295e6b30ad5a31ea03815cc61ffcd2` | 2026-09-10 | AFSDFASD | fix(rsi): align evaluation concurrency, epoch evidence reuse and partial repair pipeline |
| 479 | `2a2864bbf832c16877937d3581c9b79f945f366e` | 2026-09-11 | alan_cheng | fix(observability): address only arrays so a chain rebuilds as one |
| 480 | `3b3a4c6a809ef66ecbc8c5430783c52ab5113060` | 2026-09-11 | liulili-huawei | fix(agent-evolve): solve aigw sft upload issue |
| 481 | `5f944bb0bf8c3ef86f327e69232d641a6153a69e` | 2026-09-11 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1326: fix(context_evolver): add l3 reflection tests |
| 482 | `b5f189ba1054a338d8fa3e009b13053776340e44` | 2026-09-11 | zhaominjun1997 | fix(personal-context): fix context-link validation and collection liveness |
| 483 | `15bb58c7ef24e75c6d4e8b5bf5baf8aa90ce1262` | 2026-09-12 | alan_cheng | fix(swarm): report codex auth retries before the fallback activates |
| 484 | `1d4a7af03927f507825cbdad123d5626fa852831` | 2026-09-12 | alan_cheng | fix(swarm): classify rejected requests and keep failure details |
| 485 | `6a41a2e57120a1c14bb73709bd4f55dbd2454d73` | 2026-09-12 | alan_cheng | fix(swarm): recover external members without a checkpoint and name the cli |
| 486 | `d890f707f305877b3dfe3b9b46246bd8a79c5631` | 2026-09-12 | alan_cheng | fix(swarm): disable codex request compression for external endpoints |
| 487 | `dae074d19cbbee057badb876c039a64bc8603476` | 2026-09-12 | alan_cheng | fix(swarm): export claude native otel spans to the bridge receiver |
| 488 | `13d226d91ccc1ff4c041e984fb6aa90de6c5e0ff` | 2026-09-14 | yyuse | fix(security guardrail): Fix the issue where the full command is not displayed in the user approval dialog when a built-in blacklist command rule is triggered. |
| 489 | `a1cbe37a50f051d9ebabad7ddc4e1ee3c704f5f3` | 2026-09-14 | yangzequ | fix(rsi): bind epoch promotion to a global non-regression gate |
| 490 | `ed198d1afd14e1e2a6355d9e1f6ea5654f2f58eb` | 2026-09-14 | yangwenhan | fix(workflow): change the log level within the vertex from info to debug |
| 491 | `0bee034d2e5487c72ae466efab7e5e74c40a12a7` | 2026-09-15 | zhaominjun1997 | fix(personal-context): preserve context links and collection liveness |
| 492 | `1d9e165da7475f980c84ccf23ede531dc87226b5` | 2026-09-15 | ding_chi | fix(rsi): accept skill query |
| 493 | `4206764a0df568e98593f3b397d5197a644c08ba` | 2026-09-15 | AFSDFASD | fix(rsi): isolate judge parsing and retry reliability fixes |
| 494 | `4cead81de4842341f3b4acdd29f70bff5f520719` | 2026-09-15 | yangwenhan | fix(workflow): change the log level from warning to debug |
| 495 | `599e59bb185d00d7d64d251079c74a2ff00bb1d2` | 2026-09-15 | weixin_45721752 | fix(otel): skip attribute serialization and buffer ops for non-recording spans |
| 496 | `85d4ee1a460742e1e6f7c55b6b6af3ffa3748910` | 2026-09-15 | zhanglongmin | fix(agent_teams): drop dead claude client after turn failure and default 32MiB SDK buffer |
| 497 | `c1ae5bfa630d8542803f5f5c17bf12edc8c7f4bc` | 2026-09-15 | yan_zj | fix(system priority): 1.Update section priorities and maintain the static registry. 2. Warn and error on conflicting priorities; sort ties by section name. |
| 498 | `ddadc104bdf522bedb8a2798c15594656b65c203` | 2026-09-15 | zhanglongmin | fix(agent_teams): report runtime model on external-cli failures and gate auth fallback by category |
| 499 | `f1d7fd6923a6fadda52621385450ff8fdff0ea3f` | 2026-09-15 | hwxiajing | fix(swarm): compact sealed-run WAL and repair worktree cache-hit semantics |
| 500 | `0f691daf97bba15268286e85dc5e5dd1c52a9e9d` | 2026-09-16 | ray_le | fix(personal-context): skip time-less candidates and auto-install lark-cli |
| 501 | `13ed6c2d33cc34142be6736f2c88d8ef9a733790` | 2026-09-16 | layShang264 | fix(symphony): 修复 Skill 安装目录 ID 与图谱能力 ID 不一致导致的过滤问题 |
| 502 | `56cb92559941ac172a34cb79f2358fa2ee00b74a` | 2026-09-16 | alan_cheng | fix(observability): record tool arguments without the invocation signature |
| 503 | `6100c35f399d08e19444c481a861ed1a37cb1bfc` | 2026-09-16 | alan_cheng | fix(observability)!: remove the deprecated backend selector |
| 504 | `755b06222ab404f870eeec40f3cad77163a16a94` | 2026-09-16 | alan_cheng | fix(evolution): run evolution after_tool_call after the tool span ends |
| 505 | `7e7879d51444c42bcfe496822eccbd2ce402ac04` | 2026-09-16 | m056838700 | fix (browser agent): Preserve results and simplify runtime contracts |
| 506 | `88e1252bd77a29b36dc393e86174965383e18537` | 2026-09-16 | alan_cheng | fix(observability): commit a compaction's output window as its own turn |
| 507 | `c6659f10d54f154d745021df10cc4c8516548246` | 2026-09-16 | l_x_d | fix(context): preserve usage baseline across append-only messages |
| 508 | `f0160f81302770284a8effbddf0e5722dad8c731` | 2026-09-16 | zhanglongmin | fix(agent_teams): preserve retry-time auth category in codex final failure reports |
| 509 | `10b1722edfc5c7fb03165cb6a897612d52d5b977` | 2026-09-17 | alan_cheng | fix(observability): open the authoritative tool span for team members |
| 510 | `1c1b47cffb93641aaff52a47c61b2431a9314e91` | 2026-09-17 | alan_cheng | fix(agent_teams): reject external CLI members whose SDK is not installed at spawn |
| 511 | `3bd0bb46ab38ee427401cc94936bd598ae978603` | 2026-09-17 | ray_le | fix(personal-context): Windows sandbox cleanup, progress reporting, and runtime recovery |
| 512 | `51d198408d17c5a99df03b4c517ff3f0f292618e` | 2026-09-17 | min_gitcode | fix(harness): warn and skip unknown rail types in RailSpec.build |
| 513 | `5d2eef7fe373eef50ce74166aeb2a4f24d7bb098` | 2026-09-17 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1215: fix(mcp): preserve error status through result extraction |
| 514 | `64aa42614de6343a80826a38bac39400abb80267` | 2026-09-17 | yangwenhan | fix(callback): cache signature to improve performance |
| 515 | `7223d6302b1135772fa1c17465843deff64c9e33` | 2026-09-17 | alan_cheng | fix(agent_teams): restrict cold resume to leader and mark crashed external CLI members as error |
| 516 | `7812bca775de42dece628b19cebe97fbc1b3f7a3` | 2026-09-17 | ding_chi | fix(rsi): keep accumulated usage when a call omits counters |
| 517 | `782fda5aea9de018e81c6f539f87abed9163af51` | 2026-09-17 | min_gitcode | fix(agent_teams): preserve external CLI config round trips |
| 518 | `7e300fac3c34a9972fd1a2f2d1a87ea606bfa1b9` | 2026-09-17 | alan_cheng | fix(agent_teams): allow codex spec round-trip with unset claude_max_buffer_size |
| 519 | `82748f69c700943b7f499feed0baff1c98cc0cc6` | 2026-09-17 | alan_cheng | fix(context): keep source files read by path out of rule compression |
| 520 | `b5bcf7c5324d91768c5665c4bdcdeadd0d1e2648` | 2026-09-17 | yangzequ | fix(rsi): preserve active cases when full evaluation is skipped |
| 521 | `bc3a44eedfc01d4ced45bcb22160a93d5e7b3456` | 2026-09-17 | hwxiajing | fix(swarm): split WAL/journal per run_id and never delete the WAL |
| 522 | `cfc5443ac0ed0b1364af7fa823f8237b487d2f7d` | 2026-09-17 | alan_cheng | fix: resolve static check findings and grep test pollution |
| 523 | `28bb02ae3e2863a9085202669d7bba05ff647ebd` | 2026-09-18 | alan_cheng | fix(harness): state member messages as text and Codex tools as definitions |
| 524 | `2f9fd56d45154bd6ea56706cab88adf8eee7fb9e` | 2026-09-18 | gp1000 | fix(agent-teams): normalize member/execution state transitions on recovery (#4318) |
| 525 | `31b12e8184a40af2660f8a6bd771b93092b9b6c5` | 2026-09-18 | gp1000 | fix(agent-teams): demote pending terminal-transition rejection to debug (#1822) |
| 526 | `3870b0d49cd1d1f1d34482480e7a2295799427ae` | 2026-09-18 | ray_le | fix(personal-context): Windows sandbox cleanup, progress reporting, and runtime recovery |
| 527 | `71b803ef3e848185cc5f38a2480d3a44cb02920f` | 2026-09-18 | zhaominjun1997 | fix(personal-context): resolve collection stalls from runaway directory growth |
| 528 | `7e777190138aebb4d5df1ebfe2f2279491b65ee2` | 2026-09-18 | gp1000 | fix(logging): mark llm_call_error and other failure events as failure (#4320) |
| 529 | `ade3b3c8745f3eed0c4577ee1dcb2146969c76dc` | 2026-09-18 | zhaominjun1997 | fix(personal-context): resolve collection stalls from runaway directory growth |
| 530 | `e96540e5fb0e11a4a7f6bcd071c92bcc55e2132c` | 2026-09-18 | alan_cheng | fix(harness): complete the model request a third-party member reports |
| 531 | `1691ef41c6cd38c835552f95a5ecbd2a630090cf` | 2026-09-20 | zhaominjun1997 | fix(personal-context): refine run progress intervals and organizing-stage reporting granularity |
| 532 | `3606c9715d2db23ee64c6d6c87cab045a3277878` | 2026-09-20 | yangzequ | fix(rsi): persist termination on evaluator cancellation |
| 533 | `81e79fd8d6e7563e5971a70af5fc1e372a66ac1c` | 2026-09-20 | wncbdj | fix(goal): Language configuration error in the goal mode evaluation module prompt |
| 534 | `92eb535baa07c94f5dfb4d20ac5c20d40d19a9ac` | 2026-09-20 | aitianshi | [mirror] openJiuwen-ai/agent-core#771: fix(llm): read reasoning_tokens from Responses usage |
| 535 | `b6db2733bb9119f3b2e137969baca18d69b9f4c6` | 2026-09-20 | AFSDFASD | fix(rsi): harden judge evidence recovery and request budgeting |
| 536 | `dc8f626affaf688588d07d837bd93b2a34d11f74` | 2026-09-20 | alan_cheng | fix(swarm): point a built-in model in model_name at the right parameter |
| 537 | `f9cb58e7fbc44d059623ba3f4bc6fb80da83730c` | 2026-09-20 | wncbdj | fix(llm): The model throws an exception according to the status code specifications |
| 538 | `fc8ef32a94847cd6596e38d724f2f65fac4009c4` | 2026-09-20 | alan_cheng | fix(harness): separate what the CLI injects from what the user said |
| 539 | `ff5391b275287daf7ed34207190385eb8634939a` | 2026-09-20 | ray_le | fix(personal_context): add _update_fetch_service_config hot-update for schedule fields |
| 540 | `0c52520b3850315846201434756b861996bb0669` | 2026-09-21 | alan_cheng | fix(observability): register the team root span under its session |
| 541 | `168d88cce153958c24d22ba27c6826f11651498e` | 2026-09-21 | alan_cheng | fix(workspace): keep a re-seeded baseline file's timestamp |
| 542 | `1fbd6515a355b2d57c47032e83b897af6e79c8c2` | 2026-09-21 | alan_cheng | fix(claudecode)!: end a turn when every submitted message is answered |
| 543 | `20d56d4b84d288811d06967cadbebcd9064e9b2d` | 2026-09-21 | alan_cheng | fix(observability): open the team root on the interact path too |
| 544 | `23eda9c2761082d572dc9362fc1475f792ffd655` | 2026-09-21 | cycong_1 | fix(subagent): subagent_resume is sent twice after the subagent is closed |
| 545 | `34838d8e45eac3e5fb61bf0588392f2521c955fb` | 2026-09-21 | alan_cheng | fix(codex): record what a tool search returned and the tools it found |
| 546 | `51301e3b6d6be2475a5c231bd79e5f508ec92b03` | 2026-09-21 | alan_cheng | fix(claudecode): keep the stdio permission prompt tool on the transport |
| 547 | `7658cb0cd44d5b99f305c09ae279413c458fb10e` | 2026-09-21 | AFSDFASD | fix(rsi): budget model output against remaining request context |
| 548 | `85e55f86bb59e7fda95b7aa5719ebd9102ca7a2c` | 2026-09-21 | zhaominjun1997 | fix(personal-context): Improve the initial authorization of Feishu, and modify the verification of the collection service name |
| 549 | `9d0402563c79ac123317a923f318ef6f26cbfa47` | 2026-09-21 | alan_cheng | fix(trajectory): keep message provenance and stop recording a member's tool twice |
| 550 | `a1f7e3c4ad66825427115e2ff0b243e853cf39e9` | 2026-09-21 | alan_cheng | fix(trajectory): read a team event as the member's turn, not as context |
| 551 | `af2f66a4e30d9dbb959a7889ea387e38742aaecb` | 2026-09-21 | m056838700 | fix(browser): backport observations and task cleanup to release/v0.1.19 |
| 552 | `af99694ad276faea965baa62ae808d032c0fc059` | 2026-09-21 | zhiqiuzoeyye | fix(rsi): fix compileall subprocess env issue and error msg detection issue |
| 553 | `b3c6d1a938857b9d65318d1e92589c7a1145e910` | 2026-09-21 | lin-xiaoyu | fix(symphony): 解决轨迹截断导致经验沉淀异常的问题，迁移symphony skillpackadaptor代码 |
| 554 | `b534bc80d93b1f0adbdb1b640fc4b7194400b92a` | 2026-09-21 | hwxiajing | fix(agent-teams): rename members/ layer to jiuwen_team_members + fix migration lock sidecar |
| 555 | `b83d5fd6a7acde37276f9f7a47769c34bcd94c34` | 2026-09-21 | alan_cheng | fix(trajectory): let delivery decide provenance, not the wording |
| 556 | `d344fa56265b21162dcbd2a62f7a8aacf03d4b15` | 2026-09-21 | alan_cheng | fix(claudecode): observe threaded model requests, not just the first |
| 557 | `e23806c1073275780dced2b71ddd592ffb0a21fd` | 2026-09-21 | lin-xiaoyu | fix(symphony): 解决轨迹截断导致经验沉淀异常的问题，迁移symphony skillpackadaptor代码 |
| 558 | `e90e228cf80f1b63ec72a79e8e1bc262d10bccf4` | 2026-09-21 | alan_cheng | fix(trajectory): tell context from what was said, and survive a member restart |
| 559 | `f7dbffff40845af48f2188c91dff1aefa48b2e8c` | 2026-09-21 | min_gitcode | fix(agent_teams): route Codex team tools through dynamic tools |
| 560 | `03d99a970c518e19dc96818c9c03efcbdd2b1182` | 2026-09-22 | AFSDFASD | fix(rsi): identify judge text evidence by content instead of suffix |
| 561 | `08d83b6540a460856a9b802cc533c1ea58ddbf16` | 2026-09-22 | alan_cheng | fix(providers): keep what a vendor states once and report what it withheld |
| 562 | `2cc8b706caa36e7bc2a5d7b94b9c43967f15aac8` | 2026-09-22 | zhaominjun1997 | fix(personal-context): stabilize fetch run stop finalization |
| 563 | `39c2559c7d1a9d94dd3d7a8eb0ab63595806e726` | 2026-09-22 | AFSDFASD | fix(rsi): recover JSON verdict syntax and preserve nested evidence fences |
| 564 | `6628f1c103f8ff6160632e3adc925be4e8403988` | 2026-09-22 | alan_cheng | fix(claudecode): pair request spans that state no API request id |
| 565 | `86c5f4e815820f5988f3464288c2ece2f7b948f1` | 2026-09-22 | alan_cheng | fix(claudecode): a continuation states the billing header, not an empty prompt |
| 566 | `980ba3427ecd6294dec20290b381494afbf47011` | 2026-09-22 | wncbdj | fix(react): Honor configured inner ReAct max_iterations; default to unbounded when unset. |
| 567 | `981b4d0c59f899ee1eb321867cb639d9a1e95d35` | 2026-09-22 | alan_cheng | fix(codex): name a tool the way the model addresses it |
| 568 | `acdb910419bb39e55db2daf42af6a48a6f769087` | 2026-09-22 | zhaominjun1997 | fix(personal-context): tolerate partial fetch failures |
| 569 | `b34ffed4531ee3ec60052920c5d734c78f5696d0` | 2026-09-22 | AFSDFASD | fix(rsi): remove estimated capacity rejection from judge requests |
| 570 | `be7dadd28e0afe60ab6738eb664f81c1b115193d` | 2026-09-22 | l_x_d | fix(context): report manual compact usage accurately |
| 571 | `d4e8f81ecc222a4f7095108789b823ee72de50a1` | 2026-09-22 | wncbdj | fix(agent): Honor configured inner ReAct max_iterations; default to unbounded when unset. |
| 572 | `f6ef0eaba6cc40b35f0877a57c639ee3037b9140` | 2026-09-22 | alan_cheng | fix(teams): say which server a bare tool name belongs to |
| 573 | `f8f46ae4156596d1535c356cfd2b0adc4820561b` | 2026-09-22 | birfied | fix(program_opt): stop the search once a candidate reaches the solved threshold |
| 574 | `0700637b22951bf69d3fec0b6c6cc0c6eeade15c` | 2026-09-23 | alan_cheng | fix(claudecode): keep a call from claiming a window the CLI opened for itself |
| 575 | `0d67e585cfff7a45537c606794edd081289d914a` | 2026-09-23 | min_gitcode | fix(agent_teams): validate worktree isolation on member registration |
| 576 | `31229245b73e528bbe22d2a486c4ed10dfcd4b89` | 2026-09-23 | zhiqiuzoeyye | fix(rsi):  add bash tool fallback option to prevent windows python call issue; cap exploration budget and tighten manager diagnosis honesty |
| 577 | `41d2417bce4e3d5426ef0a7cd8539370eca55f2d` | 2026-09-23 | min_gitcode | fix(agent_teams): reset execution state during team recovery |
| 578 | `5ef8409ea5d61b82d1c979ed2fa390842165d1ff` | 2026-09-23 | m056838700 | [mirror] openJiuwen-ai/agent-core#1609: fix(harness): align browser page ownership and completion |
| 579 | `6f3a33fbb93aece65105c477c573057fead0e8dd` | 2026-09-23 | m056838700 | [mirror] openJiuwen-ai/agent-core#1612: Fix(audio): fix sensevoice transcription |
| 580 | `77c38da1eb02d53f7b1753507800750d434a55dc` | 2026-09-23 | AFSDFASD | fix(rsi): tolerate unreadable auxiliary judge evidence |
| 581 | `8bfcf1f62303fc75cec299137ff76acfc052a676` | 2026-09-23 | alan_cheng | fix(providers): isolate online rail observability and clear lint warnings |
| 582 | `9d38f93e800037eb31fa4023d5f9a18e8382d334` | 2026-09-23 | xinyang_bali | fix(agent-evolving): TTSE dreaming manage experience support LLM when embedding Provider is none |
| 583 | `2b728192403b546400a698cf9b43ed86c477520b` | 2026-09-24 | AFSDFASD | fix(rsi): validate behavior citations against decoded traces |
| 584 | `3e5de34075fe7c02944fc6d80bcf9139dc521064` | 2026-09-24 | ubaidillah-ariq | fix(rsi): add tool for code implementation module explore openJiuwen |
| 585 | `4fd009e175515775af1d069978d833d7989d5fad` | 2026-09-24 | zhaominjun1997 | fix(personal-context): resolve Edge bookmark folder aliases |
| 586 | `51317bc74e31f44c41f013131a232d7836772867` | 2026-09-24 | lamsaan_yau | fix(harness): validate URL host matching |
| 587 | `73e8255df5edc2448202a71d58c15fd1acaffa2e` | 2026-09-24 | wncbdj | fix(llm): retry transient model failures before visible output |
| 588 | `9a6e5b6a1dbdc218f4e9c3c8752681959b6cadb3` | 2026-09-24 | kyrie_irving11 | fix(ttse): blame only rules consulted in the current task |
| 589 | `cc270f89b48d1a3dd40a0ed40c4e7446403a73e7` | 2026-09-24 | Haowei_Du | fix(skill pack): avoid repeated skill pack distillation |
| 590 | `ec9019c06371f5094f47287fc5ba9726f7a06676` | 2026-09-24 | kyrie_irving11 | fix(ttse): fix issue where similar tasks generate excessive new experiences each round without accumulating the count |
| 591 | `0f5b7008cfa5009f0d5caf81052675c60c819698` | 2026-09-28 | kyrie_irving11 | fix(core): keep BackgroundTask.wait from cancelling work on 3.13 |
| 592 | `1326565d3a7cf4da96c2f69df77a6f1e1d0a0462` | 2026-09-28 | zhiqiuzoeyye | fix(rsi): Fix code_implementation iteration limits and path resolution |
| 593 | `b05be971b82dc47177df92ec2ca11cdfdddcb1e7` | 2026-09-28 | xinyang_bali | fix(agent-evolving): simplify ttse trajectory processing, refer to skill_evolution_rail |
| 594 | `c30a6a56c82792bbc276ff37362888e841b01ed5` | 2026-09-28 | zhaominjun1997 | fix(personal-context): cover publication results and cancellation boundaries |
| 595 | `df67f86bf93575dd58e9379ecf95b92a0cd07be9` | 2026-09-28 | Ziliang_Zhou | fix(memory): fix triple contract drift between JiuwenMemoryProvider server mode and the /v1 API |
| 596 | `28405e7b55fe7d990a174bd70c23745dc3d0c162` | 2026-09-29 | openJiuwen-bot | fix(security): 修改工具护栏bash情况下的匹配问题 |
| 597 | `51efc13e4eafeda0e3c54a222259d1b9fed96025` | 2026-09-29 | lin-xiaoyu | fix(symphony): restore fallback compatibility for closed‑thinking from PR #2410 |
| 598 | `5ea6772e3cc651bb9360615c78ca7da9c44fd023` | 2026-09-29 | openJiuwen-bot | fix(security): 修复通配符无法匹配非ascii字符的问题 |
| 599 | `9d3c864d993c3198d920c3f3df2232260ec36250` | 2026-09-29 | zhaominjun1997 | fix(personal-context): cover slow authorization and global pipeline cancellation |
| 600 | `d552a40ceb921cf5433021305c2bbd8ee1365230` | 2026-09-29 | AFSDFASD | fix(rsi): simplify judge recovery from frozen evidence |
| 601 | `d8854a1c2075edc0d38d22f5ed444cfe1ddb8499` | 2026-09-29 | xutuo | fix(symphony): fix missing skill index building |
| 602 | `ef1e9c5918747c142412786f8b43ad5988d90b8f` | 2026-09-29 | yyuse | fix(rail): modify user allowance rule priority |
| 603 | `1f62c0cc4d9584273be3db1eccf36b78d92cf9b3` | 2026-09-30 | alan_cheng | fix(observability): scope team root lookup to its own session |
| 604 | `3b5491d9200e3d59f3b7ccc3f3d0e12e10d27812` | 2026-09-30 | wangcan103 | fix(swarm): fail fast on unknown agent(model=...) hints |
| 605 | `a17150680510d09be6f08eb1d26be3b1716a3455` | 2026-09-30 | zhaominjun1997 | fix(personal-context): replace bundled tiktoken vocabulary with mocks |
| 606 | `c993823a6cdcfee7616266f7e4fc317c02296505` | 2026-09-30 | gp1000 | fix: 修复技能包解压路径遍历与 get_local_ip 超时等 3 类缺陷 |
| 607 | `9f9f74adeb3df4cf9a3eeb460ae13207512a469c` | 2026-10-08 | ZezeChang | fix(team-org): address code check findings and cover interrupt guards |
| 608 | `e1177190b5fca7038834864b2b55701659e388bd` | 2026-10-08 | MengHan18 | fix(ascend-affinity): add turn number to agent hint |
| 609 | `16a0890137ffbe021dee172117fd5d755c1b1cc5` | 2026-10-09 | alan_cheng | fix(claudecode): start request log wait after the reply finishes streaming |
| 610 | `38bf243ea2b407b88f391eb314aa245ca4ebcf83` | 2026-10-09 | zhiqiuzoeyye | fix(rsi): enhance tex file render for front-end render |
| 611 | `703cddb3ead263285054fa80c92dff2358234f27` | 2026-10-09 | ZezeChang | fix(swarm): address summary failure code check findings |
| 612 | `b36fcc02dedcd3b9eb15083bd93a6aeaaddf607a` | 2026-10-09 | ZezeChang | fix(team-org): complete summary submission and delivery |
| 613 | `030187f30caa6dac6b72bbceae3d94ac147278f3` | 2026-10-10 | weichenhao | fix(agent-teams): isolate group chat with a fixed runtime mode |
| 614 | `6df9699e650b3ad9b9835b46ae3c3b7572adb04b` | 2026-10-10 | wssq_2026 | fix(tracer): inject per-event trace_id/session_id in trigger |
| 615 | `b68dccbfaf89ea2d393c7cfc552aa36d1223a95f` | 2026-10-12 | maoxiangyang | fix: 提交飞书任务通知配置信息 |
| 616 | `fbeaeeef533d58d58aa1d5ea91072ee1059b21c6` | 2026-10-12 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1856: Fix in-memory session release prefix matching |
| 617 | `13eda6d04e455d28f44b2b469ab908b02705c8e0` | 2026-10-13 | alan_cheng | fix(swarm): keep shared role prompts dispatch-neutral |
| 618 | `2a16151aa16089c705df775ea1f70139f70ff8a7` | 2026-10-13 | alan_cheng | fix(swarm): resolve retire target via team_backend so dormant members settle |
| 619 | `2cddab54fff35f8427b181816ff98f1e24f1f524` | 2026-10-13 | alan_cheng | fix(logging): honor exc_info in the loguru backend so tracebacks are printed |
| 620 | `3b499931eec60383b07f3cf346a14643b151c839` | 2026-10-13 | alan_cheng | fix(swarm): drop stale board count from leader digests and explain cancel of completed tasks |
| 621 | `3cccd84bcf67d87f1bf7f718c9853abddcf5341a` | 2026-10-13 | alan_cheng | fix(agent-teams): give each temp reviewer run its own trajectory subject |
| 622 | `4aa0bb373b0f2377f9b84f71c4770e177cba005b` | 2026-10-13 | alan_cheng | fix(agent-teams): keep team.context attachment active across member rebuilds |
| 623 | `578830147939f171f12ef641ecf47f80923e79db` | 2026-10-13 | alan_cheng | fix(agent-teams): bind temp reviewer view_task to its task manager |
| 624 | `6e8996f80046b328c934a34f37bdc78256d2d63b` | 2026-10-13 | alan_cheng | fix(swarm): import ExternalCliModelConfig for spawn type hints |
| 625 | `6fe3c3539e509370440569eb285b0021a1fb4285` | 2026-10-13 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1832: fix(llm): classify request encoding errors and retain error logs |
| 626 | `72cf5a4bc4fff8fecb03aa808f161e46288f909c` | 2026-10-13 | zvby | fix(swarm): skip late duplicate round-start STARTING writes on running members |
| 627 | `8b6d7e1f99d14da936d7386d3618df4f6bad201e` | 2026-10-13 | alan_cheng | fix(agent-teams): run temp reviewers on the team session so their agent span opens |
| 628 | `9ef9c13427093a0f500cf8bd0c79a9b336286596` | 2026-10-13 | alan_cheng | fix(agent_teams): let the scheduler start human-assigned tasks under scheduled dispatch |
| 629 | `b2688d054e61364e8ef2da4751f613ad21c2dfe3` | 2026-10-13 | alan_cheng | fix(swarm): steer scheduled teams away from peer messaging and idle waits |
| 630 | `c79d73fc4073d568f8e0dccc808983cd21f494b6` | 2026-10-13 | weichenhao | fix(agent-teams): restrict spawn_passive_human to group chat |
| 631 | `ce2aedda90810d77cc00523ee4dac67f26d4f998` | 2026-10-13 | gcw_g4GeJCEm | fix(sys_operation): decode streamed subprocess output with an incremental decoder |
| 632 | `dc15407996eadeb9dcbe2add06671dc73c151a44` | 2026-10-13 | alan_cheng | fix(agent-teams): let create_task assignees reference inline members of the same batch |
| 633 | `ff505d9991e53cd5f1027888bb61b3e30c85ca0e` | 2026-10-13 | alan_cheng | fix(swarm): accept send_message targets that only repeat to |
| 634 | `0f7651ef2c0e1f1afc03d60ab18ca41b37bb3cf0` | 2026-10-14 | alan_cheng | fix(swarm): tolerate create_task assignee repeating the inline member name |
| 635 | `142820494d7c71eb34e251ebe8b04e2b26c7fe06` | 2026-10-14 | kyrie_irving11 | fix(ttse): probe embedding once and skip the semantic index when it is down |
| 636 | `256711a8a1449e92d17bf8863b5a88768bd3dc30` | 2026-10-14 | alan_cheng | fix(agent-teams): keep task bodies out of the leader's board and drop redundant upstream-read instructions |
| 637 | `3e909d376b52b10feb89e7bbf11325fe361a29b6` | 2026-10-14 | alan_cheng | fix(agent-teams): discourage wrap-up broadcasts and ask for short deliverables inline in summaries |
| 638 | `51849ef590b43504d290ad328f2201a0a1730c8f` | 2026-10-14 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1913: fix(store): make DbBasedKVStore work on PostgreSQL and MySQL |
| 639 | `589da12ab34f64c4b70311b35f3a6ccc232145dc` | 2026-10-14 | alan_cheng | fix(harness): count only back-to-back identical tool rounds as a loop |
| 640 | `618325dbb2e5915e5ca4bf6d8ffd071e5ea5a90b` | 2026-10-14 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1916: fix(callback): stop counting agent interrupts as callback failures |
| 641 | `7627fe45c0157edbc812304273e7c92bab4adb68` | 2026-10-14 | alan_cheng | fix(agent-teams): drop task restatement from rework notices and demote reviewer view_task to a fallback |
| 642 | `91d0ebcb2681cead4a1008471fc79f62c28587c5` | 2026-10-14 | alan_cheng | fix(agent-teams): wake teammates only for tasks they can start and inline assigned task content |
| 643 | `a0be74b8a363c8f4fdf7c29f82d9427c3ed778bb` | 2026-10-14 | openJiuwen-bot | fix(tracer_otel): tag retrieval component spans with gen_ai.retrieval attributes |
| 644 | `acc35ed323ba04e9a77f7a276639ca8497af79c6` | 2026-10-14 | zvby | fix(swarm): gate org tool mounting behind spec-level enable_organization flag |
| 645 | `b8ceb049194deabe8ca0f033d3059d5cc5a9b2cf` | 2026-10-14 | yuji66666 | fix(tool): add timeout and response status validation for Agent tool HTTP requests |
| 646 | `c139537042a7b336634d285a2c1111bd5476bf11` | 2026-10-14 | wssq_2026 | fix(prompt): tolerate empty placeholders in TextableVariable |
| 647 | `c433e47223be9786a99ff2c8e212a26c9795a50b` | 2026-10-14 | alan_cheng | fix(agent-teams): stop leaders from asking members for completion reports in task content |
| 648 | `dc54191ffb28d917c4b87f71d25ec37f0a26dc84` | 2026-10-14 | wangcan103 | fix(swarm): persist interrupt checkpoint and clarify worktree errors |
| 649 | `edcd51fec690113f6cd77cdb4a4df2318b54e503` | 2026-10-14 | zhaozixuan | fix(sandbox): merge yuanrong sandbox from agentos branch |
| 650 | `ef950092233d470c5027615fbb40b91e81eb363e` | 2026-10-14 | alan_cheng | fix(agent-teams): stop echoing the completion note in member_complete_task results |
| 651 | `f28ea9ec48091840ad5545392e4c6bd955e441e9` | 2026-10-14 | ZezeChang | fix(swarm):修复解散组织后临时专家团不解散的问题 |
| 652 | `16b009f775ed163d71c6e9842a8d171b014c4eb2` | 2026-10-15 | alan_cheng | fix(swarm): deliver unread mail oldest-first and anchor inbound time |
| 653 | `2dd351ebf86a125276fea17e23f4ca4aa91005c7` | 2026-10-15 | chenchunzhou | fix(swarm): Pass the evaluated rail list when deriving expert specs |
| 654 | `5cc21e0f2e4f9e2cd3183be0352e66e0359df5ea` | 2026-10-15 | weichenhao | fix(swarm): allow omitted provider version in external agent manifests |
| 655 | `80c6faac25acfe901079590587f57d6eb8c7ca7c` | 2026-10-15 | alan_cheng | fix(swarm): keep create_task inline member out of autonomous dispatch |
| 656 | `933758711b57839ad028977042f0024d2b8764ef` | 2026-10-15 | wssq_2026 | fix(prompt): keep placeholder when resolved value is None |
| 657 | `98e0ddfd37ff77e878acc92d0d957ad8a6ff94f9` | 2026-10-15 | alan_cheng | fix(agent_teams): carry retire target status on MemberShutdownEvent |
| 658 | `a3677e7cd46b7d0f342ce9a8c24a61f1245131af` | 2026-10-15 | alan_cheng | fix(harness): rebind cached context processors through the public update_model_context chain |
| 659 | `a72319407e567da3f57a463ff4d88ef6c629d919` | 2026-10-15 | openJiuwen-bot | fix(otel): retrieval configuration properties + full-link embedding span |
| 660 | `ab1ad6202019d51661a81121ddc2b721ccdb24d7` | 2026-10-15 | openJiuwen-bot | fix(harness): rebind context-processor models on hot-reload so compressors follow the live primary model |
| 661 | `c0853daca677c64e260944f9aac2bde78b464b79` | 2026-10-15 | wncbdj | fix(llm): Add logs for model group routing failures |
| 662 | `d03be0a38e9955858d3893d1b4184d37c9eebf40` | 2026-10-15 | alan_cheng | fix(swarm): hand reassigned active tasks to their new owner under scheduled dispatch |
| 663 | `d67ff7d01d034e44075507e92dc0b0d40188a307` | 2026-10-15 | alan_cheng | fix(swarm): ignore client identity when comparing model pool entries |
| 664 | `d8c226fa37bf61f71525927214a102983cb759ef` | 2026-10-15 | wssq_2026 | fix(prompt): tolerate empty placeholders in DictableVariable |
| 665 | `f9a39da75ebd0435719aa6ac2cdd842be5470c68` | 2026-10-15 | wangcan103 | fix(swarm): carry post-write status on TASK_UPDATED event |
| 666 | `9eb1f4559a6373340937ac11a4738f726d04fcba` | 2026-10-16 | m056838700 | [mirror] openJiuwen-ai/agent-core#1990: fix(jev): preserve scoped Jev observations and local recovery |
| 667 | `cc703a094ad9d97f6f0a903b41f5da29525bc2f6` | 2026-10-16 | zhiqiuzoeyye | fix(rsi): enhance report writing stability to prevent user prompts conflict with designed pipeline |
| 668 | `86fc907f16a9aa94ee7ecdc53af868d1f7b6bf89` | 2026-10-17 | cyr20040123 | fix(attribution): log attribution run failures with traceback |
| 669 | `00a01b27744543f5b52e98a21c19530922403b32` | 2026-10-19 | gcw_PSut7Kkp | fix(http): close aiohttp session by checking closed property |
| 670 | `0eed70f6c479c39fccc1d3b5baaff67c0647eb87` | 2026-10-19 | fenglitong | fix(attribution): find the reply when no message artifact is reported |
| 671 | `11ba974722bd009f81abf27a98979df99b0e0620` | 2026-10-19 | fenglitong | fix(attribution): set Fast Reward signal tiers from claim status |
| 672 | `1fe6a03690766e64a686ac51e873495a617b5ca5` | 2026-10-19 | alan_cheng | fix(swarm): pre-bind swarmflow primitives into workflow modules |
| 673 | `2b8998cbceb8fc99777da3465588b5adfeb32b44` | 2026-10-19 | zvby | fix(swarm): auto-suffix organization id claimed by an inactive owner team |
| 674 | `34cb872d9ae2350fcff18631204511cd001f938a` | 2026-10-19 | fenglitong | fix(attribution): show what the selection leaves out, with times |
| 675 | `47a16032072a7d8132fe00c5ffa6d7c56c562589` | 2026-10-19 | hh0205 | fix(swarm): validate display_name at the spawn tool boundary |
| 676 | `47eef7bcdf04f0b5f5ec053e0112fb93befd776e` | 2026-10-19 | maoxiangyang | fix：更新飞书通知人信息 |
| 677 | `55b9c571a3c0b954bf1c69068ce2c25d2d6f26d7` | 2026-10-19 | alan_cheng | fix(harness): honor Bash timeout across execution layers |
| 678 | `565cf3269b94f36b43f10970cbba6ba5064ac645` | 2026-10-19 | fenglitong | fix(fast_reward): judge timing claims by what they serve |
| 679 | `8c2e72a6b8d8fe86252b6700537557adce65a2f0` | 2026-10-19 | fenglitong | fix(attribution): name the final reply from the whole session |
| 680 | `9a7db1a3940a44502af7b899ca20a20bf85f960c` | 2026-10-19 | fenglitong | fix(attribution): take the task from the user's own messages |
| 681 | `b3e0c3f90954c943e94855213acc954f80a042fa` | 2026-10-19 | alan_cheng | fix(swarm): hand reassigned active tasks to their new owner under scheduled dispatch |
| 682 | `c7b557680d7d73085debe3852c4eea4637db65cc` | 2026-10-19 | fenglitong | fix(attribution): keep only failed tool calls beside Fast Reward signals |
| 683 | `ca091d07682638771beb0f349c94ef9fcd9bc982` | 2026-10-19 | yangzequ | fix(controller): prevent canceled tasks from being scheduled |
| 684 | `f2a2aa19227317147b74c0cd88f0b603211cac4c` | 2026-10-19 | grandocean | fix(a2a): close SDK stream when client stream is abandoned |
| 685 | `295fd6226d84b445723d20bf378d3ed89a49aa20` | 2026-10-20 | fenglitong | fix(attribution): keep the evidence progress moving past the check estimate |
| 686 | `426001617082869270d2dfc09810755cf01452a6` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2085: fix(tools): relay tool images through tool_call, allow show-once images |
| 687 | `4cd9df83855c0504af314655fbd7e2f34a0442fa` | 2026-10-20 | zhaozixuan | fix: yuanrong sandbox support conch executor |
| 688 | `4e6f324d5f39c95fccfc7479da62da9f99c1e9e4` | 2026-10-20 | fenglitong | fix(attribution): name the final files from the whole session |
| 689 | `5ac3d793396ea752801b747199f04378b40de76d` | 2026-10-20 | gp1000 | fix(clients): keep RefCountedResource._last_used on the wall clock |
| 690 | `5f017e34dbdf3b56e0baa93ba6394c252140f846` | 2026-10-20 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2080: fix(react_agent): fail when output limit is hit before any answer |
| 691 | `612fe1aca5dd6f82ee929967b15003bcadd982b6` | 2026-10-20 | fenglitong | fix(attribution): require the leader's hand-over step to review deliverables |
| 692 | `6aaf5498a9aea03d8e0d1cdcf0c39c60989dadce` | 2026-10-20 | fenglitong | fix(fast_reward): leave an unknown challenge claim out of the score |
| 693 | `84d433ab88005eea9d10721acd4ebab0c7e6b8d9` | 2026-10-20 | fenglitong | fix(attribution): review deliverables only when the last hand-over step is selected |
| 694 | `9dacf14e7e19efb07bfea7ed5fb2365e7a9705b3` | 2026-10-20 | fenglitong | fix(attribution): adapt to JiuwenSwarm develop 289e09330 |
| 695 | `a132cb82aa05181ff856e69df16a63bd997b9c7d` | 2026-10-20 | alan_cheng | fix(swarm): align spawned expert personas with predefined members |
| 696 | `a6e34d06f1ad261d6c4a4784279f613bc399ee46` | 2026-10-20 | zvby | fix(swarm): auto-suffix organization id claimed by an inactive owner team |
| 697 | `ac0b4f9ee62e6a8426cdd60358d0fc5a3ece6a58` | 2026-10-20 | cyr20040123 | fix: resolve codecheck level-1 findings |
| 698 | `b4ee4c6678edfb569ddddf055db8bc72454d51c3` | 2026-10-20 | zhiqiuzoeyye | fix(rsi): loose up validation criterias in report; bundle all skills/ assets as package data |
| 699 | `4170e756504d70d8324c2ac18a5afdd770a99dc9` | 2026-10-21 | grandocean | fix(mq): skip task.exception() for cancelled MQ tasks in done callback |
| 700 | `87c0379aae99cbf8713c77a6f15e4d30ede027e6` | 2026-10-21 | alan_cheng | fix(swarm): accept underscores in AgentGroup roster names |
| 701 | `8aa9940e41de3ed5ff423dd8c276bf7e582e807d` | 2026-10-21 | alan_cheng | fix(swarm): let scheduled create_task declare inline members from experts |
| 702 | `dd38d3cf9d32ccae533f2991dd28ff3b385692f9` | 2026-10-21 | grandocean | fix(events): normalize metadata in Pydantic post-init |
| 703 | `05ef81958075f5f45bd5720bc120f6468d960df9` | 2026-10-22 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2117: fix(tools): relay tool images through tool_call, allow show-once images |
| 704 | `0ab5913b250d565b35bff3828dae438939cb1acd` | 2026-10-22 | wangcan103 | fix(swarm): restore team ask-user interrupts after restart |
| 705 | `1ab6a410dda1e98727704ef5e11ff59030421931` | 2026-10-22 | grandocean | fix(vcs): reject event refs beyond the current head on rewind |
| 706 | `3d31974970acedfcc838092d22efc05474217dac` | 2026-10-22 | grandocean | fix(resources): load package manifests with a UTF-8 BOM |
| 707 | `503d0cdfce195bb64c65e45d9927928226c27328` | 2026-10-22 | grandocean | fix(vcs): compute log CRC from JSON-mode model dump |
| 708 | `524d48fb430adae5add05bb86397801dcb02fe34` | 2026-10-22 | grandocean | fix(cli): keep /sessions working when a session file is damaged |
| 709 | `65dc4b133495d77be9c2de3614d1005c379ea959` | 2026-10-22 | grandocean | fix(stream): raise when a chunk cannot be queued after retries |
| 710 | `68cfb445dd67ec4a1a57d7a9d1b61b76b60a2a27` | 2026-10-22 | zvby | fix(swarm): restore organization history and expert names after restart |
| 711 | `7f89918031511a8e75a52193a24d70ab52c657dd` | 2026-10-22 | wangcan103 | fix(swarm): resume team interrupts through a leader-ready gated stream |
| 712 | `90a2804f89cbadab79b6c475909ae5d3583d970c` | 2026-10-22 | grandocean | fix(cli): report wrong-typed max tokens/iterations as one error line |
| 713 | `ac8c28d5bba83e61b5529d716a5f39c8b98f65a8` | 2026-10-22 | grandocean | fix(cli): report the installed distribution version in --version |
| 714 | `b0bc22b0cc290fe44033bd1f86bce48230ab1275` | 2026-10-22 | grandocean | fix(teams): escape separators in team/session topic components |
| 715 | `bf013d56531dc7e74de53b012c8ecb02473e39e3` | 2026-10-22 | grandocean | fix(a2a): close remote stream when consumer stops early |
| 716 | `c2d1544ce9d9a74dddee5dbb73a9e662c0a9130e` | 2026-10-22 | alan_cheng | fix(harness): keep duck-typed tools and killed shells sound under input-aware timeouts |
| 717 | `c54d9592be3708009117c3994a8268ba5607bf84` | 2026-10-22 | alan_cheng | fix(swarm): make the private prompt of external CLI members optional |
| 718 | `ccea5a8e899d2853955df8efb70eecf9becd0064` | 2026-10-22 | grandocean | fix(logging): keep exception-derived error_code a string |
| 719 | `cd7090591304f9e173e2371af519024f1134fa6a` | 2026-10-22 | grandocean | fix(cli): report auto-harness setup errors without a traceback |
| 720 | `d7e5d15cf260e694c6a2a637f6618ad6e1a0daf1` | 2026-10-22 | grandocean | fix(controller): make FileDataFrame.bytes usable and enforce frame discriminators |
| 721 | `dec1e41108fef948a1689938a407e99a2266db59` | 2026-10-22 | grandocean | fix(packaging): ship the agent_builder plugin manifest in the wheel |
| 722 | `e0d6ac75d798ad1179376cd46fdf1765d51475d6` | 2026-10-22 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2116: fix(interrupt): deliver each interrupt answer only to the rail that raised it |
| 723 | `e0d6b1fa38f9134d745f869c172047477c5cad3a` | 2026-10-22 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2059: fix(interrupt): deliver each interrupt answer only to the rail that raised it |
| 724 | `fee4fced26d768c9aeaf157be5e0b8b6604c2881` | 2026-10-22 | alan_cheng | fix(swarm): resolve static check findings in expert tools |
| 725 | `27acfe9834e76360848226943ea437766f4eab3d` | 2026-10-23 | wncbdj | fix(context): ignore unread streaming response fields during overflow checks |
| 726 | `9ab1967a7abd1df87e32cd23dfd755ce3fef83f2` | 2026-10-23 | zhiqiuzoeyye | fix(rsi): upgrade compatibility: encode compatibility, allow replace instead of hard failure;  add fallback if llm not supporting images |
| 727 | `af85e33de075f9a356633eb49b3e3f1ff70fe26d` | 2026-10-23 | karina0522 | [mirror] openJiuwen-ai/agent-core#2146: fix(sys-operation): correct Windows shell auto-detection and capture backgrou... |
| 728 | `e8f4e4575bc95c830bec8dc43f8c8d849ca7c3b7` | 2026-10-23 | lamsaan_yau | fix(moa): preserve partial verifier scores and log periodic JEV decisions |
| 729 | `ffdd7075fef80723c2239cefa0172f99205e6fd0` | 2026-10-23 | alan_cheng | fix(swarm): wake the recipients of member-to-member messages |
| 730 | `046a869101cb895f1ac0ce6d2220111046fdea61` | 2026-10-24 | openJiuwen-bot | fix(otel): 流式路径回填 TTFT/inter-token 时延到 usage_metadata，接通 #1833 时延属性 |
| 731 | `a4f81793b09f9868d30176473f11f683ae4880a9` | 2026-10-24 | cyr20040123 | fix(evolving): 完善归因配置开关并修复慢模型诊断与图片交付物 |
| 732 | `a51e1c048de2e570912bc34a55f3bcb6a9ce2ba8` | 2026-10-25 | alan_cheng | fix(swarm): address review findings on scheduled member subtasks |
| 733 | `0e3e40248d15bdccb5c8e4e23f0c3e0ff128b04f` | 2026-10-26 | alan_cheng | fix(swarm): base worktrees on local branches and report startup failures per member |
| 734 | `12b76630a2a49f5e49a3617ab8b23ba5c3966db5` | 2026-10-26 | alan_cheng | fix(swarm): bind task notice baselines to the member session |
| 735 | `23d2ba33cb306fb0c7c1c6d21b40b8701e86c6e0` | 2026-10-26 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2147: fix(llm): log Jev System One responses and failures to llm.log |
| 736 | `25288a6fd01441683d09ed275d287c381c295b76` | 2026-10-26 | ray_le | fix(personal-context): isolate per-article fetch failures in Toutiao and Zhihu |
| 737 | `2fec26102a9699d13c0e24077bf418a952828556` | 2026-10-26 | alan_cheng | fix(swarm): address review findings on task revisions |
| 738 | `3bb2ede5b679e4c93e1325a2637cb9cac470bfdd` | 2026-10-26 | weixin_39960157 | fix(tracer_otel): emit a single execute_tool span per tool call |
| 739 | `4782d8a74e04716a6f3450fb6bddc8b793215059` | 2026-10-26 | cyr20040123 | fix(evolving): 完善归因配置开关并修复慢模型诊断与图片交付物 |
| 740 | `56b841f8f0401a3f6d41cf31b26b567e52cfcc5f` | 2026-10-26 | MengHan18 | fix(swarm): 修复单分支仓库工作区启动与成员失败通知 |
| 741 | `6e7f3bd90daf30cc87efc6f11dcfab932249585e` | 2026-10-26 | wncbdj | fix(tools): reject todo update and append calls that omit the todos array |
| 742 | `8c5338a33cca88e8ea4bc0665334013b0fcfacf9` | 2026-10-26 | zhangyaomaggie | fix(harness): cascade task-loop cancel to inner DEEP scheduler tasks |
| 743 | `939ad4092fa5cc54d4e3bf12dd1ae37801e8d2a1` | 2026-10-26 | ZezeChang | fix(swarm):优化集群模式组织里汇总模式选择 |
| 744 | `a13ba1566243a36928db6d07bbcf69d8c711b126` | 2026-10-26 | gl_zhou | fix(attribution): 补齐 release 同步归因与 Reward 时相对 develop 的差异 |
| 745 | `c3f7f024c9d8f8076d899ef1e61a85e47f1a712c` | 2026-10-26 | alan_cheng | fix(swarm): release bound sessions and carry unpersisted task notices |
| 746 | `d8602ba00eae783c3a13f5a7169388f378102018` | 2026-10-26 | tangfei0306 | fix(core): modify httpxclient's param |
| 747 | `d90a4423245e34c84d5122d7df6ed7a51bbfc501` | 2026-10-26 | ray_le | fix(mcp): reconnect streamable-http session after server restart |
| 748 | `e3f4cd9e62fa91556f4b045e6a104212f3454fa1` | 2026-10-26 | alan_cheng | fix(swarm): bind task notice baselines to the session object |
| 749 | `eb42b7491bc5472fe1594e039deef80999d04e92` | 2026-10-26 | cyr20040123 | fix(attribution): 调整归因agent设计，新增轨迹摘要并修复输出语言和设置没对齐的问题 |
| 750 | `0a8ba5dea0b3b85fb5d14aa6aff95b5309918fa5` | 2026-10-27 | liuxueyuanjwt | [mirror] openJiuwen-ai/agent-core#2227: fix: stop has_subscribers() false-positive and metric leaks on unregister |
| 751 | `115583b05b57db9cef0039f75ade80b0a91a5e75` | 2026-10-27 | tangfei0306 | fix(core): 解决在core里httpclient不能复用得问题 |
| 752 | `339d64dfaa7f60dbb1bc57d33de52539d97269da` | 2026-10-27 | alan_cheng | fix(agent_teams): close a member's memory index managers on dispose |
| 753 | `3ac8a9ac3fe0748da1650d52a85a8cbda5d0185b` | 2026-10-27 | alan_cheng | fix(agent_teams): read spilled bodies off the loop and skip them in scans |
| 754 | `4efbef028594fcb3e1e25951efb49545e48e11ae` | 2026-10-27 | alan_cheng | fix(swarm): keep the leader creating tasks after the board drains |
| 755 | `50e97707b8277cde4c835717ed555302f7d4bc40` | 2026-10-27 | gp1000 | fix(otel): TTFT 改用语义明确的 ttft_seconds，first_token_time 保持「时刻」语义 |
| 756 | `b1ea803dd11f2daf5f73c96a0ff2d6104b9028d0` | 2026-10-27 | wangcan103 | fix(swarm): prevent session bind cleanup race |
| 757 | `b2a0a248c554753ba31727415fe17a03eed60b46` | 2026-10-27 | cyr20040123 | fix(attribution): Adjust attribution output and complete trace summary and output language |
| 758 | `bdc74e3eaf27b6c1418f84f3a7201c5ec3e3976e` | 2026-10-27 | hh0205 | fix(swarm): unstick scheduled reviews and gate inline task members |
| 759 | `d3ccf8ab3248a182cc05a3c2afc5692531d2e223` | 2026-10-27 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2133: fix(harness): hosts answer approvals and questions by interrupt kind |
| 760 | `e3e19a9509a00959a9d72479f07b141d57cd01d9` | 2026-10-27 | JustinBieber1223 | fix(llm): rebuild cached IntelliRouter on event-loop change |
| 761 | `ea3d113ec7781c1c0e5f87c3ee3dbb5fd1b0a793` | 2026-10-27 | lamsaan_yau | fix(moa): reuse task-scoped proposals |
| 762 | `ef69a75e1ca308a4b4b917ce57716809f88ff1cc` | 2026-10-27 | cyr20040123 | fix(attribution): normalize reported file artifact paths |
| 763 | `f86cf2c6e6c5339d943202ec581167d5abf98202` | 2026-10-27 | cyr20040123 | fix(attribution): normalize reported file artifact paths |
| 764 | `0bc33c69635418146d169048d2a5a70bb61d641f` | 2026-10-28 | openJiuwen-bot | fix(tracer_otel): emit a single execute_tool span per tool call (#2046) (develop) |
| 765 | `2e77c1af1b2d284a5a02cbac070397048bc5678f` | 2026-10-28 | MengHan18 | fix(tool): support discovery-only tool metadata |
| 766 | `577dfa38213f2a1d3196c4f3e988d600c62b38dd` | 2026-10-28 | alan_cheng | fix(swarm): stop scheduled tasks stalling on retired members |
| 767 | `71244097e2026c2c814ec97e42184e14577c9ff4` | 2026-10-28 | l_x_d | fix(rsi): record runtime evidence and bound harness execution |
| 768 | `8f101e92dc473c4ae6e4a0f7bf23e5b201916524` | 2026-10-28 | openJiuwen-bot | fix(tracer_otel): fail open when the dedupe probe raises (#2046 follow-up) |
| 769 | `af7e66882887416a8b4ef90706e80eb65d75cab2` | 2026-10-28 | hh0205 | fix(team): improve failure handling in model group routing |
| 770 | `d6a815488d2e1242f8a4d860cb5f96b8a5a5a906` | 2026-10-28 | MengHan18 | fix(tool): support discovery-only tool metadata |
| 771 | `e5e738b5a2217302d951bb89a7b0e920e893b52d` | 2026-10-28 | alan_cheng | fix(swarm): escalate only policy-retired owners and refuse departing assignees |
| 772 | `fce5f9229cea8f37bbab6479ed3858c975de6c2b` | 2026-10-28 | l_x_d | fix(rsi): promote harness changes with continuous two-stage scores |
| 773 | `07d5c4b590799033d7ecea7078299a8bb4a60f6d` | 2026-10-29 | yyuse | fix(permissions): fix issues including inconsistency between the title and body of permission approval, and duplicate rule persistence caused by incorrect file path extraction |
| 774 | `24a9e52b06ee1dcb46341a3c0c73c460325abdb5` | 2026-10-29 | liuxueyuanjwt | fix(multi_agent): 会话清理与 stop 时真正摘除 MQ 订阅，避免订阅泄漏 |
| 775 | `4cc900ca4b2c3a1b1e7a83ad94c8128a8eac0672` | 2026-10-29 | tangfei0306 | fix(core): 解决在core里httpclient不能复用得问题 + modify httpxclient's param |
| 776 | `9f162b7077e62c4d174892184d06a512ffb710b4` | 2026-10-29 | tangfei0306 | fix(core): update version to 0.1.20 |
| 777 | `acfc070dd1d41f04f36566d0de65fb57aaaf76f4` | 2026-10-29 | yyuse | fix(permissions): fix issues including inconsistency between the title and body of permission approval, and duplicate rule persistence caused by incorrect file path extraction |
| 778 | `e089198835426cd7681761b0cc278670cd83031f` | 2026-10-29 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2310: fix(harness): hosts answer approvals and questions by interrupt kind |
| 779 | `e140e79cd306a431d4a220d93cb8f6db24a56ed6` | 2026-10-29 | cyr20040123 | fix(reward): reuse the core model client to fix Reward LLM calls when using Anthropic URL |
| 780 | `fe4eb216e001c50cbb713f954a2344060d4e2ead` | 2026-10-29 | JustinBieber1223 | fix(llm): rebuild cached IntelliRouter on event-loop change (release/v0.1.20) |
| 781 | `0e79fb153cdd609cfeb7dc8d7974e3d8aa7b92d5` | 2026-10-30 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2333: fix(session): keep stream send cancellable on Python 3.11 |
| 782 | `2eaf30a8672e9e0f843291a0c7cbecc4462d4ac4` | 2026-10-30 | wncbdj | fix(llm): Change the Anthropic max_tokens value. |
| 783 | `9efe425fb1fc9b55df1ecbc3b741c5848424c59a` | 2026-10-30 | KevinLLLove | fix(graph): 取消传播保留 checkpoint + InteractiveInput 恢复失败显式报错 |
| 784 | `00a6e8e61815a298550b3d928a3c201fbbb65b14` | 2026-10-31 | alan_cheng | fix(swarm): run org schema DDL under the shared team DB write lock |
| 785 | `05c0e8228a20ba48cf9e73569740305c656b6369` | 2026-10-31 | alan_cheng | fix(swarm): share one TeamDatabase per SQLite file regardless of path spelling |
| 786 | `0a9a49b3c5783ee718b8cbbdb6ff6a58b670ca99` | 2026-10-31 | alan_cheng | fix(swarm): make TeamDatabase close terminal and bound watchdog retries |
| 787 | `5e4daee7588d8cffd15fdcf6a5c295827c97023d` | 2026-10-31 | wncbdj | fix(deps): update intelli-router version to 0.1.0b1 |
| 788 | `9cb073b8957dcd26d30d72bdbd3d699d864dc378` | 2026-10-31 | alan_cheng | fix(swarm): bind org DB context once and harden shared session lifecycle |
| 789 | `b98a1cab1ccf7b6ac0326e95e58b948c661ea574` | 2026-10-31 | alan_cheng | fix(swarm): release engines on failed static DDL and serialise close with initialize |
| 790 | `0f91da167f62a7d85413ab64a429efcacebb51e2` | 2026-11-02 | zhaominjun1997 | fix(personal-context): enable PCS search through PersonalContextRail |
| 791 | `051ae9c879d613dfb0523611dbcb5ab1731581dc` | 2026-11-03 | wssq_2026 | fix(react_agent): keep skills section when the system prompt is empty |
| 792 | `1db527584c1b518d842698f8ac2a3369ddf39e83` | 2026-11-03 | grandocean | fix(vcs): clear stale contexts when applying a snapshot |
| 793 | `31c73fd721bfb14ebc8702d2a0148627686c38f3` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1789: fix(workflow): preserve nullable node config round trips |
| 794 | `36847b59b73cae4b3364918f1415ad3b01c97cae` | 2026-11-03 | alan_cheng | fix(runtime): preserve compatibility across session and stream boundaries |
| 795 | `384d146655b4afa53a699adc25af1e698553ea04` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2399: fix(context): treat Bailian "Range of input length" errors as context overflow |
| 796 | `3b8b3dd34147c5d7d2607114493c9efcd47287fd` | 2026-11-03 | grandocean | fix(vcs): escape dots in state delta paths |
| 797 | `619aafa1a11fd38f027f13932572b25d48536826` | 2026-11-03 | m056838700 | [mirror] openJiuwen-ai/agent-core#2386: fix(browser): preserve Jev facts and simplify recovery handoff |
| 798 | `63fb88845b162ed13054b6678ff4c6d44847956f` | 2026-11-03 | hanjiahui | fix:安全护栏权限模式功能问题 |
| 799 | `69e0ce1b0aef91029389dae977e1737a253c8df8` | 2026-11-03 | grandocean | fix(mq): escape serializer marker keys in plain payload dicts |
| 800 | `78590598a1b7812e57cfa6dbd4d6b923b633a192` | 2026-11-03 | grandocean | fix(session): encode ':' in GroupUserSubject ids |
| 801 | `7a15c0fa1c3cd0ee1491cd8627a4379ee33245a7` | 2026-11-03 | grandocean | fix(controller): make Intent.target_task_id optional at the schema boundary |
| 802 | `951ca1fd0fff5d05685d42d98910af2f03070783` | 2026-11-03 | alan_cheng | fix(mq): expose explicit wire compatibility settings |
| 803 | `ad668f63b96a095475d58126cf251f9cc6ac7fdd` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2400: fix(react_agent): retry once, then fail, when a turn returns no answer and no... |
| 804 | `adce3cdae8895e58ec9c89e341479579365028e7` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2404: fix(memory): back up every message before a migration and never delete unback... |
| 805 | `ae7127a81caecaea38b66040c1a9bfc7778a2766` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#2406: fix(attribution): read JEV_BASE_URL and JEV_MODEL when JevClient gets no argu... |
| 806 | `b13b9f39010c7c4511406052ef1efa843b8f0fbd` | 2026-11-03 | l_x_d | fix(rsi): bound Windows diagnosis paths and isolate unit fixtures |
| 807 | `e00ae735da11470827fdbe7743ac80dd2cc79ffc` | 2026-11-03 | grandocean | fix(a2a): mark artifact last_chunk for terminal streaming results |
| 808 | `e5dd53839cc243ec0d9fb7d9132da089dd744a4d` | 2026-11-03 | hh0205 | fix(team): name the routed model across model-group calls |
| 809 | `eb85a4a110ef7b7fce285cdbc2193af5174b885d` | 2026-11-03 | Zora7583 | fix: 提示词优化 |
| 810 | `f3347789f509bc8fe0548ca4dcec848494ea9e9b` | 2026-11-03 | wangcan103 | fix(swarm): bind avatar child-session spans to Team root |
| 811 | `f4e6e9fc2166fec8c5ae9dd98b503c5a94867a7b` | 2026-11-03 | grandocean | fix(session): persist downstream ids in chain session links |

## 优化改进（33 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `8efb2403fd3a905e07513b076cb751e8b483a9ef` | 2026-06-02 | iamcandiceguo | update version to v0.1.14 |
| 2 | `39102935d2b3cc3798e84263596bfb00186f2086` | 2026-06-16 | iamcandiceguo | update v0.1.14 to v0.1.15 |
| 3 | `ea81274f2d872f8c8d78694e9ad46ce9a826c8a6` | 2026-06-16 | alan_cheng | perf(swarm): tune SQLite read/IO pragmas |
| 4 | `1d097361dc339640df7c1bbc55e464dd50e3ad76` | 2026-07-02 | alan_cheng | perf(swarm): push has_unread broadcast to EXISTS, member updates to CAS |
| 5 | `3756bb74e07dac5cc442460f905eb1d6380d8989` | 2026-07-02 | alan_cheng | perf(swarm): collapse get_task_detail downstream N+1 into one query |
| 6 | `3839bcd38b20ab4d5622268c5d2e8d540e39d6d0` | 2026-07-02 | alan_cheng | perf(swarm): drain check via one aggregate count, not a full task scan |
| 7 | `3aef04e9b2499c64def11528a6f0b8b5660bf6be` | 2026-07-02 | alan_cheng | perf(swarm): query-driven message indexes to cut INSERT write amplification |
| 8 | `85340d2a02b3a38af79f68b70419c1ff9cc141c2` | 2026-07-02 | alan_cheng | perf(swarm): collapse list_tasks_with_deps N+1 into two queries |
| 9 | `94e798d0a2b72ef212467c1051238ede0535e512` | 2026-07-02 | alan_cheng | perf(swarm): opt-in background WAL checkpointer to move checkpoint off write path |
| 10 | `9cccd52df41f626999b122646422b1ed99ab9bce` | 2026-07-02 | alan_cheng | perf(swarm): claim_task single-statement CAS to shorten write lock hold |
| 11 | `a61544607ad181cfd6e9a3029aca858253255994` | 2026-07-02 | alan_cheng | perf(swarm): split read/write SQLite pools with tunable pool/PRAGMA knobs |
| 12 | `c2843caa021b983726eeb93ac818f0ad167e8b40` | 2026-07-02 | alan_cheng | perf(swarm): fold task assignee into composite index, drop dead updated_at |
| 13 | `e8d4835c4351513b40fb815f7c1b85d0a43353ec` | 2026-07-02 | alan_cheng | perf(swarm): batch direct mark-read + fix broadcast watermark dup PK |
| 14 | `f36527652e6cedcb6684b0c948ea21382e2c3a61` | 2026-07-02 | alan_cheng | perf(swarm): batch multicast into one transaction to cut fsync fan-out |
| 15 | `2a5774dfc611e913ceaea25600567673c048afb7` | 2026-07-03 | alan_cheng | perf(swarm): switch recovery/lifecycle roster reads to list_member_roster |
| 16 | `90ad98b8a10976e4155decca9b4fc9963cc35ea7` | 2026-07-03 | alan_cheng | perf(swarm): project list_members to roster columns only |
| 17 | `e4fafad0ca0109cf9ddc0b39a4cc3adb5acde923` | 2026-07-03 | alan_cheng | perf(swarm): switch multicast roster check to list_member_roster |
| 18 | `ed23e6ef3f7995b5fcee86dc644c619282b2ff77` | 2026-07-03 | alan_cheng | perf(swarm): use EXISTS probe for member presence checks |
| 19 | `336f3a80e2d0bde787f2d2d34fcfc19dcc80b931` | 2026-07-08 | alan_cheng | perf(swarm): skip team-wide cycle scan when a mutation adds no edges |
| 20 | `a9e87c346ada9c85ae4d907de834f8ab2ccf04b8` | 2026-07-10 | openJiuwen-bot | perf(sandbox): improve sandbox performance |
| 21 | `0af63f481847363c6ed993c6157dd4f8f3a36858` | 2026-07-31 | taoliankuan123 | update: 更新文件 README.zh.md |
| 22 | `2d688a06b8ebf1e4beb86a47e3f1b0886d43ee1d` | 2026-07-31 | taoliankuan123 | update: 更新文件 README.md |
| 23 | `0476ee0c689b9e1c98cc3a4274812fe0db5e0986` | 2026-08-01 | alan_cheng | perf(harness): cache context files by their on-disk identity |
| 24 | `a6207c549c2669caaf86f9059ecca1fb470cd6d2` | 2026-08-01 | alan_cheng | perf(harness): stop paying for probes, locks and log payloads on hot paths |
| 25 | `3c094c5cb67f9d1125987e81b0fbd648e95d3ceb` | 2026-08-20 | tangfei0306 | update to v0.1.17 |
| 26 | `d71a43f725c48cf1d75f9b8d3d91cbcf68456582` | 2026-08-20 | tangfei0306 | update to v0.1.17 |
| 27 | `04b3410b6f574874da6718d6b7511a5bf8143bde` | 2026-09-09 | alan_cheng | perf(observability)!: state the context window once per chain |
| 28 | `1d3ebcef687fa37adab1f4941a9223a7d36bb5bb` | 2026-09-09 | alan_cheng | perf(observability)!: state span input and output once per span |
| 29 | `1e9f8cb708e83af9e56e8e214198f09ad06b701f` | 2026-09-09 | alan_cheng | perf(observability)!: carry stream frames off the span snapshot path |
| 30 | `b84617feeef7a64be02336d8bf385af5a2a47bca` | 2026-09-09 | alan_cheng | perf(observability): encode span payloads on the consumer thread |
| 31 | `7d75e7d24771c6dd3e86c36fa56d3776ae95249f` | 2026-09-11 | alan_cheng | perf(observability)!: state a streamed answer once, not once per chunk |
| 32 | `cc0e58fb1d548572326b173c451d03abbf7d1084` | 2026-09-11 | tangfei0306 | update version to 0.1.18 |
| 33 | `6ada1c6224b64cea8e37710d9b980bc8bd29c7bd` | 2026-09-22 | alan_cheng | perf(codex): ask the CLI to flush telemetry promptly |

## 重构（61 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `103a3a55f6353f1a52b1fe17fbcc4fe7c4e58bc1` | 2026-06-01 | alan_cheng | refactor(swarm): migrate typing aliases to PEP 585/604 in domain tool files |
| 2 | `5ffc18748f9708abe265d48be43789ac9681d8f7` | 2026-06-01 | alan_cheng | refactor(swarm): split team_tools.py into domain-specific modules |
| 3 | `ac8c66144e1b13517d8223d11fcc7cf02a4e4062` | 2026-06-02 | alan_cheng | refactor(swarm): migrate SuperHarness to agent_teams as NativeHarness |
| 4 | `cc40cce49512ef3a0df05b385f2e86ff06e27c65` | 2026-06-03 | alan_cheng | refactor(swarm): split NativeHarness configure from start |
| 5 | `3497b1b7e8c98fa406250f18f0b995a9d9efe7fd` | 2026-06-05 | alan_cheng | refactor(swarm): construct native harness with the spec's real card |
| 6 | `805e064ad6960c04381c50fe6a4de35766a799b9` | 2026-06-05 | alan_cheng | refactor(harness): unify tool registration via AbilityManager.add_ability |
| 7 | `884c9af54fbe6024ba23c190a3bf68b888429238` | 2026-06-05 | alan_cheng | refactor(harness): drop per-agent stateful tools on native teardown |
| 8 | `b030ed44adb87eb2c6009b8ac0bf7d78711012f6` | 2026-06-05 | alan_cheng | refactor(swarm): register swarmflow facade alias once at import |
| 9 | `b5fbca5dff3f9f0e495bb4ae162070555e442681` | 2026-06-05 | alan_cheng | refactor(swarm): rename enqueue_mailbox_after_first_iteration |
| 10 | `d46ee53ac10a87e885557f5d4b5f525612907319` | 2026-06-05 | alan_cheng | refactor(swarm): build team rails fresh per cycle, inject reusable state |
| 11 | `db4b5cb8906783961f41452c563c43ae355b5dda` | 2026-06-05 | alan_cheng | refactor(harness): drop owner tag when add_ability registers tools |
| 12 | `2ffc1af096bd98e48ec2c4821adfbedcf6795b8e` | 2026-06-06 | alan_cheng | refactor(harness): drop builtin search tool sharing and tidy helpers |
| 13 | `a7e76d56f79b48c9a09ee11276723a6c9196af27` | 2026-06-06 | alan_cheng | refactor(swarm): harden teardown logging and simplify approval gate |
| 14 | `7f0175090246ae350124c5e46f9f956e76fdcfd5` | 2026-06-08 | alan_cheng | refactor(harness): route web tools via package entry, use async impl |
| 15 | `8bbd5b5b8dd7b49e0a444b70ab73e7a0376c49d5` | 2026-06-16 | zhanglongmin | refactor(agent_teams): remove unused spec param from _build_session and unify session creation |
| 16 | `804bfd568234e9c4b3cb3b314a9bdc79086c6657` | 2026-06-17 | zhanglongmin | refactor(agent_teams): move team-specific types to agent_teams and replace should_persist_to_disk with _persist_allow_always |
| 17 | `f1bd869554815b48171bdcba65203a5c4d7d41e4` | 2026-06-17 | zhanglongmin | refactor(agent_teams): move field docstrings into Field(description) for Pydantic/SQLModel fields |
| 18 | `9d12c9fd209793ae0a78bda44d72dc4a5b315d8d` | 2026-06-18 | min_gitcode | refactor(agent_teams): remove silent field and should_emit_interrupt_output |
| 19 | `c5fa41c2b5e69eb0c063c547f444e8409fd4b827` | 2026-06-23 | alan_cheng | refactor(swarm): move agent() tuning params into options bag |
| 20 | `f97bd9831ede9e1f133ae0f4b9a5ed1eef57e9b1` | 2026-06-25 | alan_cheng | refactor: replace threading primitives with asyncio equivalents |
| 21 | `ff2ee73d252154dfae54b6bec4283931e6a786a5` | 2026-06-27 | alan_cheng | refactor(harness): remove deprecated synchronous web_tools module |
| 22 | `67f2463763c4ea43a2776874e3643fdbad58346b` | 2026-07-02 | alan_cheng | refactor(swarm): drop InMemoryTeamDatabase, use sqlite :memory: in tests |
| 23 | `0a8a982a53b47b9cc4212731cca4910c4e4f187e` | 2026-07-03 | alan_cheng | refactor(swarm): remove dead role_policy chain and delete policy.py |
| 24 | `2a4cb602376930f786cdf3bc5ef72f65c1ce9229` | 2026-07-03 | alan_cheng | refactor(swarm): externalize HITT/bridge prompts to md templates |
| 25 | `be9d8afc638abe0bd2a721fae2a4095350f208b5` | 2026-07-03 | alan_cheng | refactor(swarm): drop legacy build_system_prompt assembly path |
| 26 | `1be63ae59460999f59c8fdeefc7dec1f01a4d60e` | 2026-07-13 | zhanglongmin | refactor(agent_teams): remove generic ssh transport |
| 27 | `89bb056638b67716fa50f3bfd7ab0e3667fcd41b` | 2026-07-13 | zhanglongmin | refactor(agent_teams): expose cli runtime base |
| 28 | `d9269f64b719cdfb758452757dc1cf80a064440c` | 2026-07-13 | zhanglongmin | refactor(agent_teams): remove legacy Claude CLI adapter |
| 29 | `f52df821956f5d1655339a3ba6cd93e8eedda8d1` | 2026-07-14 | alan_cheng | refactor(swarm): resolve static-check findings in locales, rail and DAOs |
| 30 | `a20083a68d5f31b7974f7d7dc82bade59b1fb27a` | 2026-08-01 | alan_cheng | refactor(harness): order rail init by priority too |
| 31 | `487333895adff0b65285c895125f32e24325802e` | 2026-08-06 | AFSDFASD | refactor(rsi): streamline integration code |
| 32 | `d56661fd9eaa32d4a7b3bc7857a2699df297cf3c` | 2026-08-06 | lin-xiaoyu | refactor(symphony): refactor symphony |
| 33 | `1b38b26fa961a623b25c5613cfa603259d37b14b` | 2026-08-08 | alan_cheng | refactor(swarm): group tool descriptions into domain directories |
| 34 | `7e48843c218a48ac398a3702d1b453b8d3ea8b77` | 2026-08-10 | 2401_87256159 | refactor(evolution): move reviewer feedback orchestration into core rails |
| 35 | `89b9cbe8f0c7621c79118529044a55daf0f9e345` | 2026-08-11 | z2tng | refactor(trajectory): migrate consumers to stateless span accessors |
| 36 | `f6005427c5f9e53770feeab44b80af5608fc6a32` | 2026-08-12 | alan_cheng | refactor(swarm): read the Skill library state through one shared reader |
| 37 | `3acde127b6b7313a93dc2b2fb686ea775de641fa` | 2026-08-13 | z2tng | refactor(evolution): query review evidence from canonical spans |
| 38 | `7d26aaad60555dfb719a64a2b865595a82984b04` | 2026-08-13 | z2tng | refactor(trajectory): isolate offline tooling and remove legacy implementation |
| 39 | `4072fb658df75d2c657be84def46ad006b2f32bb` | 2026-08-14 | mengyuanli | refactor(online-rl): simplify gateway collector boundary |
| 40 | `80d1c456662d592766e97ddc03bc8e569099de4c` | 2026-08-17 | alan_cheng | refactor(swarm): split the agent-tier span off the team observability rail |
| 41 | `2341d5ff47dc16c11eafb4ca8de60d8dc7decf43` | 2026-08-20 | 2401_87256159 | refactor(evolution): move reviewer feedback orchestration into core rails |
| 42 | `81567529fd9e7012ca9693f1c3da7938bb0db9d7` | 2026-08-29 | alan_cheng | refactor(observability): drop the openjiuwen request-messages mirror |
| 43 | `e076d1a3e2c08a7e093fa2298db4eae135441324` | 2026-08-29 | alan_cheng | refactor(observability): read LLM messages only in the standard shape |
| 44 | `978cdd9622b10da401c96faaf9a1ab4258285404` | 2026-09-02 | alan_cheng | refactor(harness)!: extract provider-neutral harness protocol |
| 45 | `0c3f90947662a14c27bc6e5dace322c3db08f949` | 2026-09-03 | alan_cheng | refactor(observability)!: isolate Langfuse projection in the exporter adapter |
| 46 | `544ea28388844b7ada580cd5570daa4e59996827` | 2026-09-03 | alan_cheng | refactor(observability)!: converge redundant semconv keys onto GenAI standard |
| 47 | `dcbfef35b16c7da4dae3055c0807080502510e72` | 2026-09-03 | openJiuwen-bot | refactor(workflow-engine): rename engine-local WorkflowError to EngineError |
| 48 | `e1b227f8d34e9b0756cebcfc8ae3c78fb54fbf6e` | 2026-09-03 | MengHan18 | refactor(ascend-affinity): unify KV cache lifecycle management |
| 49 | `64950135491756c8ff6b95ae6037f0d097061a70` | 2026-09-04 | alan_cheng | refactor(observability): drop the last semconv mirror and group ms durations |
| 50 | `564997732e22fcdd204959b79b38b4768f5220c0` | 2026-09-12 | alan_cheng | refactor(swarm): address coding-standard findings without changing behavior |
| 51 | `bd05f82629dad8338b44e7cd35aef99a94aaeccc` | 2026-09-16 | alan_cheng | refactor(trajectory)!: remove every historical trajectory read path |
| 52 | `8879e07d1a61bf1250b510db0f249f257eab2da0` | 2026-09-21 | alan_cheng | refactor(teams)!: deliver member identity only as team context |
| 53 | `8345a2730d6e4fad33ee5760a37b506855461049` | 2026-09-30 | weichenhao | refactor(agent-teams): unify group chat input routing and consolidate group chat logic |
| 54 | `2af5375accf4e82e6214cf7dc269a98e522eda71` | 2026-10-13 | alan_cheng | refactor(todo): rename todo_create input and todo_list output key to todos |
| 55 | `7d009bfea6a96a91bed561b377c247460e6a2d3a` | 2026-10-13 | alan_cheng | refactor(swarm): expose validate_member_name as module-level function |
| 56 | `170b0b90f5118bb524dcd2162e9fc75e01d148d6` | 2026-10-20 | fenglitong | refactor(reward): rename fast_reward to reward |
| 57 | `b70f2211e5c20eaaf5b299e43f58e62f25dd1450` | 2026-10-20 | fenglitong | refactor(attribution): review deliverables only in the leader's delivery round |
| 58 | `08343e1d164d11bdbe0e61d6ffb546369cef0894` | 2026-10-21 | alan_cheng | refactor(skills): unify design templates and separate implementation records |
| 59 | `47cc52e7982b67b543b6c416e530b9d6162fc29c` | 2026-10-21 | alan_cheng | refactor(swarm): simplify design lifecycle and use D-prefixed identifiers |
| 60 | `edad98cf4fc308bca9485bbdf5aaf7438041076f` | 2026-10-26 | alan_cheng | refactor(skills): ground core reviews in module contracts |
| 61 | `1d9ca7163b8d0106216a43f45e543ab3b9e92576` | 2026-10-29 | alan_cheng | refactor(swarm): clear code check warnings in task notices and fork spawn |

## 文档（167 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `2039e9368b70e43c1e6de6181bd5e7837fc62fda` | 2026-05-27 | alan_cheng | docs(swarm): archive external CLI hardening feature and sync prompts spec |
| 2 | `7ac3d376d91fc12f90df3b81d95504583046033d` | 2026-05-27 | alan_cheng | docs(swarm): mandate code/doc split into two consecutive commits |
| 3 | `8e61e3ec9b691ee69b544c70414d6cabdf75a8eb` | 2026-05-27 | alan_cheng | docs(swarm): archive F_26 external operator/member scopes, sync CLAUDE.md |
| 4 | `af0c27a0827e56abecee326cf8a32ab51526e760` | 2026-05-27 | alan_cheng | docs(swarm): archive F_24 agent time awareness, sync specs |
| 5 | `be20a2b761407434820967420221c7175ef0242e` | 2026-05-27 | alan_cheng | docs(swarm): split code, tests, and docs into three commits |
| 6 | `dc09952678987eb267d9327c394de6818c29f5a0` | 2026-05-27 | alan_cheng | docs(swarm): record time-rendering edge surfaces in F_24 |
| 7 | `233b7ff569c8a34358f8ee60aea8409a9e63ed2a` | 2026-06-01 | alan_cheng | docs(swarm): archive spawn-tools split (F_26) and sync contracts |
| 8 | `6c19e2b9d0c16e6a211742058c727a8ed793365c` | 2026-06-01 | alan_cheng | docs(swarm): update tools/CLAUDE.md module map for domain split |
| 9 | `25b94237d57cf94e2dc046c206e665fb8983b71a` | 2026-06-02 | dev_chenyan | docs(evolving): Add evaluation pipeline module API documentation |
| 10 | `4976ece148af048dfc259f67b7f9989683d57752` | 2026-06-02 | rolandyang | docs(evolving): add API docs for experience sharing module |
| 11 | `529444fe29d4df687d8e96b3c3b53cffcb758881` | 2026-06-02 | alan_cheng | docs(swarm): archive F_27 + S_18 for NativeHarness task-loop |
| 12 | `67587d9c6ecf0ca91ff08b227f770d69da7dbb83` | 2026-06-02 | alan_cheng | docs(swarm): archive swarmflow feature F_27 and spec S_18 |
| 13 | `362159974d60a2da338f6c845c679fbdf259a990` | 2026-06-03 | alan_cheng | docs(swarm): archive swarmflow per-call model routing as F_31 |
| 14 | `5deb680183e02bfbdd9e650e00af0cf5ee31a5df` | 2026-06-03 | alan_cheng | docs(swarm): archive F_28 + sync specs for NativeHarness team adoption |
| 15 | `7ae44f33809a835cfc10b6f323c8d10457eb36c4` | 2026-06-03 | alan_cheng | docs(swarm): note live-native E2E in F_28 |
| 16 | `85a79f959a19eed7b1b7b0e48380f1b83aac9c71` | 2026-06-03 | alan_cheng | docs(swarm): archive F_28 + S_19 for reliability framework |
| 17 | `adb1ee9fb5998ad82d27f4c1ff4f2329b4c8d039` | 2026-06-03 | alan_cheng | docs(swarm): archive F_29, revise S_19 for reliability follow-ups |
| 18 | `d645c4a9ca6ef8a4b0b21f27d19341b628cbdeac` | 2026-06-03 | alan_cheng | docs(session): add vcs design doc |
| 19 | `9da5dbfcafe26184c71a5a38b5c3ebb928a12578` | 2026-06-04 | alan_cheng | docs(swarm): archive declarative harness assembly as F_32 |
| 20 | `3357c4d039f28bc38c707c27d095296f4da4ed17` | 2026-06-05 | alan_cheng | docs(swarm): record team rails are never cached; reusable state injected |
| 21 | `8b123acdecf7e10cb32a3ed9c6b15e15ef0a2415` | 2026-06-05 | alan_cheng | docs(swarm): record harness subscribe surface and first-start injection |
| 22 | `db5577e3b0be68801990d6c9e5a5420391a88178` | 2026-06-05 | alan_cheng | docs(swarm): sync enqueue_initial_mailbox_poll rename |
| 23 | `dda31b61ca0f4d6d66522a9058077e6fe71f96b5` | 2026-06-05 | alan_cheng | docs(swarm): archive native-harness async-tool framework feature and specs |
| 24 | `f6252222b4799084aa1329ac814bc4ae666831ec` | 2026-06-05 | alan_cheng | docs(swarm): record swarmflow import-alias simplification |
| 25 | `ffba5668b02f2eb7ae789c9f2a46e59a44661b63` | 2026-06-05 | alan_cheng | docs(swarm): record worker TeamHarness + run_once + structured_output |
| 26 | `6b635c95be4697d22374e6cbfb1f0f49c9bef1da` | 2026-06-08 | alan_cheng | docs(swarm): document swarmflow stateful sessions and human |
| 27 | `9394ce08aecbf8ca2618ad2aaff2e7c02b7294d2` | 2026-06-08 | alan_cheng | docs(swarm): record deterministic human correlation id design |
| 28 | `9f1a7ffd9e19f8a50a5dfbad106133b81a959a55` | 2026-06-08 | alan_cheng | docs(swarm): record swarmflow human wiring; unify three-commit rule |
| 29 | `bfb0bcad77f310a0f993243708834c1137eb5625` | 2026-06-11 | alan_cheng | docs(swarm): add constraint that doc updates target AGENTS.md not CLAUDE.md |
| 30 | `f7654db6829e725b19c2ad05e5bfe417a5210b03` | 2026-06-11 | alan_cheng | docs(swarm): migrate agent_teams CLAUDE.md to AGENTS.md as single source of truth |
| 31 | `24cceb0a1bd1181bdcb84032a11468da15b530e7` | 2026-06-16 | z2tng | docs(skill-evolution): add self-evolution docs under Development Guide |
| 32 | `a15f7531efc47d6d49485b90adadd1b23345d4d3` | 2026-06-16 | alan_cheng | docs(swarm): archive swarmflow E2E hardening (F_39) and sync S_18 contracts |
| 33 | `cf0e03e8ecc55605e2bf71e19ae03f89b72fea4f` | 2026-06-16 | alan_cheng | docs(swarm): record swarmflow journal persistence and dir layout |
| 34 | `560ef4039b0e0a6417fe75ebdc75c9dfc75d7816` | 2026-06-17 | alan_cheng | docs(swarm): archive journal WAL/program-order (F_40) and fix engine-deps rule |
| 35 | `63e6705a04ed00453383b326ccee4ca351222721` | 2026-06-17 | zhanglongmin | docs(agent_teams): sync specs and AGENTS.md with team permission system |
| 36 | `738128eadd7c9e19b8943e3fd3b0341cec703f80` | 2026-06-22 | alan_cheng | docs(swarm): archive async-tool DAB (F_41) + sync S_20 / AGENTS |
| 37 | `c061a5e388e6158b2261c855dbcf65ac86ed5843` | 2026-06-22 | alan_cheng | docs(swarm): archive swarmflow Claude Code alignment (F_42) |
| 38 | `0126007c7efe2960de92300fb0490aad049468f5` | 2026-06-23 | alan_cheng | docs(swarm): record resume session-context fix + every-phase e2e (F_43/S_18) |
| 39 | `1d71d7afd310957840af1a3925562dd5874973a1` | 2026-06-23 | alan_cheng | docs(swarm): record worker-not-teammate / no-DB change (F_44, S_18) |
| 40 | `5cc225f9b82cccc76d690eefaeb488468a59c20d` | 2026-06-23 | alan_cheng | docs(swarm): archive swarmflow pause/resume (F_43) + sync S_18 / AGENTS |
| 41 | `7f7501dd25fb67d159f1667d3ce38aacd968c6c6` | 2026-06-23 | alan_cheng | docs(swarm): enrich swarmflow tool description toward Claude Code parity |
| 42 | `2ad42af7e74d1130817b692038cad3d52e450e09` | 2026-06-25 | alan_cheng | docs(swarm): record tiny agent feature (F_45) |
| 43 | `c74682a9ac6dcbfc57585bc7c7cc5abdfc31f37b` | 2026-06-30 | zhaoyulu1 | doc(trace): add tracer otel_handler relevant docs |
| 44 | `3e489eafb90d3c14736232b42b8c4b665c12a4b7` | 2026-07-03 | alan_cheng | docs(swarm): scope list_members to one-off roster query |
| 45 | `6231996fa408abe026ac0a46124ab120fcf0896a` | 2026-07-03 | alan_cheng | docs(swarm): sync prompts AGENTS.md for md template externalization |
| 46 | `6547d75b25697fb2978fd4e7f0c640df2d225ebd` | 2026-07-03 | alan_cheng | docs(swarm): archive F_49 and sync specs/AGENTS for desc/prompt split |
| 47 | `84afe42746560dd5e2637e9df118ffe4599c559d` | 2026-07-03 | alan_cheng | docs(swarm): archive F_50 HITT contract/roster split and sync spec/AGENTS |
| 48 | `863b32191997b20a1257f207e996bb6a2f923674` | 2026-07-03 | alan_cheng | docs(swarm): sync specs/AGENTS to single prompt assembly path |
| 49 | `8b8e77254358215779d0734152c11d8a58a0de91` | 2026-07-03 | alan_cheng | docs(swarm): translate tools AGENTS.md to Chinese + read-projection principle |
| 50 | `e95316a34529f4b93e049e7bacc0313675a9e0bf` | 2026-07-03 | alan_cheng | docs(swarm): drop policy.py from prompt-assembly docs |
| 51 | `aeea444cdbf6c2b0880c22c08f91881cf156eb3f` | 2026-07-06 | alan_cheng | docs(swarm): require leader-written swarmflow scripts under team workspace |
| 52 | `27a18f3d86b8738832fbc6d40f0bf0808e99f1a5` | 2026-07-07 | alan_cheng | docs(swarm): archive F_54 and sync S_03/S_08 for revoke-based reassign |
| 53 | `31fe38087e277e27869c49471687262ad824d8b8` | 2026-07-08 | alan_cheng | docs(swarm): archive F_56 and sync S_03/S_08 for task-level signals |
| 54 | `a61846bb0f57f3f3e965895d7d242f2214d4715d` | 2026-07-08 | alan_cheng | docs(swarm): require reading S_/F_ docs before designing, not just at commit |
| 55 | `5ed38555fc202905abe1d3b44867336036d58ae0` | 2026-07-09 | alan_cheng | docs(swarm): archive F_57/F_59, sync S_08/S_12 for the state machine |
| 56 | `802c5a1682163a53cf9ce8dd9de3f1886138dd52` | 2026-07-09 | alan_cheng | docs(swarm): archive F_60, sync S_18 and AGENTS for pause/abort/resume |
| 57 | `818a3b290992a130081ff8f1ec84f765cfe4ef75` | 2026-07-09 | alan_cheng | docs(swarm): finalize F_59 Phase 2, sync S_08/S_12 for the verify gate |
| 58 | `ce38f3817b2d482cc068a3b4e1bb2b8aadc19877` | 2026-07-09 | alan_cheng | docs(swarm): archive F_61, sync S_18 and AGENTS for cold resume |
| 59 | `ff3ac417d6cc638cd2099d2d5444d8a17e6aa114` | 2026-07-10 | alan_cheng | docs(swarm): archive F_62, add S_22 and sync specs for scheduled dispatch |
| 60 | `303d50e90fbe2678187b0af46f0fa3e82ed341e4` | 2026-07-14 | alan_cheng | docs(swarm): archive F_63 and sync S_12/S_22 for message templating |
| 61 | `4ec0a88fd9810fa20379a84e72ee88de06df992d` | 2026-07-17 | alan_cheng | docs(swarm): archive F_66 and sync the budget contract |
| 62 | `385c83c0aaf854fe01fb85c38af5196c8104baf5` | 2026-07-24 | karina0522 | docs(openai-auth): add OpenAIAccount OAuth provider guide and API docs |
| 63 | `040aac401be1770eb4128840c19220eaa2bc86ab` | 2026-07-28 | alan_cheng | docs(swarm): archive F_69 and rewrite the workspace/worktree invariants |
| 64 | `6f92bcd300b961249c3f2d4e706598912d744572` | 2026-07-28 | alan_cheng | docs(swarm): document handler decision policy in handlers/AGENTS.md |
| 65 | `82cec1cf9503bfcb2763922e78ea3714acbc4f04` | 2026-07-28 | alan_cheng | docs(swarm): archive F_69, sync S_03 / S_12 and coordination AGENTS |
| 66 | `43e3a95eec3018aa9d9ed51e7ff0431907274d47` | 2026-07-29 | alan_cheng | docs(swarm): archive F_70 and revise the team context contract |
| 67 | `64c80648daa2646d1623b646e8b6091f80170c68` | 2026-07-29 | alan_cheng | docs(swarm): record the input-hook delivery contract in F_70 and S_09 |
| 68 | `6ce267bc311f98947ea1a601d8c0883450ba6eae` | 2026-07-30 | openJiuwen-bot | doc(operator): Reproducibility test case supplement |
| 69 | `f17aa7a35f319cd7aef69ed8a1a4d13559f1b926` | 2026-07-31 | z2tng | docs(skill-evolution): align API references and navigation |
| 70 | `ff4c826d6163933b7eb9c832a89221b1083eedd9` | 2026-07-31 | alan_cheng | docs(swarm): archive F_71 and revise the input batch contracts |
| 71 | `479f0e7d2499c7eaede1f4a5bf29885b0c0cd7b5` | 2026-08-03 | openJiuwen-bot | doc(operator): Operator documentation supplement |
| 72 | `e2d6b6858af0b936b9f480e1357c0201f658a061` | 2026-08-03 | yangzequ | docs: update docs based on v0.1.16.post1 |
| 73 | `7f813f4d758f2bcf96061a2e7ddbbe2919868afc` | 2026-08-05 | l_x_d | docs(context_engine): document the default forked processor chain |
| 74 | `f56b26164de1e10978d77904a1090bef31c744a7` | 2026-08-05 | yangzequ | docs: add missing English translations for comprehensive zh/en alignment |
| 75 | `1c8cb2fac882937d0ad7fc80d7b8b29be6219b6c` | 2026-08-06 | alan_cheng | docs(swarm): spell out broadcast cost in send_message docs and prompts |
| 76 | `7e9ac5ba1b35b08f0fc6e28e0ea8989cb27186ff` | 2026-08-06 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#273: feat(docs): browser subagent docs |
| 77 | `63c822bf2c2143ae02e2b514d6c3a6d3b5aa57f9` | 2026-08-08 | alan_cheng | docs(swarm): archive F_76 and sync prompt/tool contracts |
| 78 | `6a9d71640f5d1de7fa5efe20720e93bcf841cee9` | 2026-08-10 | 2401_87256159 | docs(evolution): document rail-owned reviewer feedback flow |
| 79 | `808d4d0ac09f71f4e38d5f6a45f396e06af77a21` | 2026-08-11 | alan_cheng | docs(harness): document the rails package |
| 80 | `9e50ea57fcea0446447112c90cf905b76281380a` | 2026-08-11 | alan_cheng | docs: correct the callback priority direction |
| 81 | `e9229e95e1693381a3374c1da570741e04453821` | 2026-08-11 | alan_cheng | docs(swarm): record the steering batch quota hook |
| 82 | `49b1b6a9f1b1acf33b06ece7c6db965d44784d9f` | 2026-08-12 | alan_cheng | docs(swarm): record why the library state reader moved to harness |
| 83 | `9c9fafd0441ca93ceb0e50850a4dc7bede2efd05` | 2026-08-12 | alan_cheng | docs(swarm): name the workspace as the only team declaration seeder |
| 84 | `c75594a279610a04473792d94b351496bf5a2969` | 2026-08-12 | alan_cheng | docs(swarm): record the team-scoped Skill decision in F_79 |
| 85 | `d5ebf5487ad319bd7c2e6e385b94ed1263976bd9` | 2026-08-15 | alan_cheng | docs(swarm): record the explicit-disband invariant |
| 86 | `e42555181bd6be2e617033f29cb3616b27357a09` | 2026-08-15 | alan_cheng | docs(swarm): document the reassignable task statuses |
| 87 | `cf8ed4d7d6dfc2c4f81b517efc8b615c27d4e757` | 2026-08-17 | z2tng | docs(trajectory): align API guides with canonical runtime |
| 88 | `d92ff5e1dc57e6904ea8ed6bca2d9c5f0fccb125` | 2026-08-17 | alan_cheng | docs(swarm): archive the agent-tier rail split |
| 89 | `a756648954b76fba60d142780bd4f3f6d6489f04` | 2026-08-24 | yangzequ | docs(harness): add subsystem spec suite S_01-S_18 |
| 90 | `425dde7f08abe3f68578dc196fd15cf441e3b25a` | 2026-09-01 | alan_cheng | docs(harness): archive F_01 ask_user cross-interruption OTel events |
| 91 | `61becb1756a4f15c13c7dec1f8a0100c99bd5237` | 2026-09-01 | z2tng | docs(evolving): add scoped agent instructions |
| 92 | `c30659680bb83dd064e8d1a930eb6ca1d1424947` | 2026-09-01 | alan_cheng | docs(swarm): archive F_109 send_message recipient split |
| 93 | `a937baa9c7b3345f7ab3d75c683838ee2f284b59` | 2026-09-03 | alan_cheng | docs(tracer_otel): declare independent semconv policy; fix 2 broken tests |
| 94 | `b14bb795a24d392bd2cd32a3c57286bf17c65c70` | 2026-09-04 | wangcan103 | docs(swarmflow): forbid invented workflow_token_limit and fix budget doc to per-run scope Refs1728 |
| 95 | `a44ddacba6b4071264e7342813b6091cedabf76e` | 2026-09-07 | wangcan103 | docs(swarmflow): clarify workflow_token_limit and per-run budget scope |
| 96 | `29dfb09fdabeae17707455739e808401363eaec8` | 2026-09-09 | alan_cheng | docs(swarm): record native e2e baseline and harness initialization invariant |
| 97 | `a7d62871dcf8bfa88e1806d6edc25c82598eebd0` | 2026-09-09 | alan_cheng | docs(swarm): document Codex user input support |
| 98 | `d581ba62d43b3d78136f7f6e0d1a79999e6bd525` | 2026-09-09 | alan_cheng | docs(swarm): archive harness providers design and sync external harness docs |
| 99 | `e3360299dcf3529dfb62043ce71ba3a024b5cb3a` | 2026-09-09 | alan_cheng | docs(swarm): record codex e2e baseline for harness providers |
| 100 | `f2a063fefb5dbb612e17b2a92b02b34d1047e5b1` | 2026-09-09 | alan_cheng | docs(swarm): record auth fallback ratification in provider and member specs |
| 101 | `86b64b482d0a897fee59211b1578e60969e59bdf` | 2026-09-10 | alan_cheng | docs(swarm): document the NativeHarness protocol adapter |
| 102 | `621c7b68f59b5ed5ffb2956cab4bec754d9431bc` | 2026-09-11 | alan_cheng | docs(swarm): document native_v2 and harness recovery contracts |
| 103 | `6fcf2396544be11a3dfcf54b2ca3c52819c56c5e` | 2026-09-11 | liulili-huawei | docs(agent-rl): add online-rl doc |
| 104 | `cd378e4c8aef0034465c584b24e432fc67f53ad8` | 2026-09-11 | liulili-huawei | docs(agent-rl): add online-rl doc |
| 105 | `db763bcb97ef8e80f16843a4d6bb676e1cdd579b` | 2026-09-11 | alan_cheng | docs(swarm): document DSH capabilities and prompt modes |
| 106 | `e71647439d78e62ae56b0df0e819ea2d8601c071` | 2026-09-11 | alan_cheng | docs(swarm): document portable skills and conflict policies |
| 107 | `eee19055ba2e4f6e7ebf9facf1f3a9c826f286ca` | 2026-09-16 | alan_cheng | docs(harness): document the tool result rendering contract |
| 108 | `1f55b4c6150a449779638bf6dd6754081af2439d` | 2026-09-20 | alan_cheng | docs(observability): state the boundary with tracer_otel |
| 109 | `4f890b8ef2cf9031246fa7d3f879f6f60b5334fd` | 2026-09-20 | alan_cheng | docs(swarm): record why model_name rejections name the other parameter |
| 110 | `9331041cc756edc763ce518f8e20c6a2c84d8a50` | 2026-09-20 | alan_cheng | docs(swarm): record built-in model selection for external CLI members |
| 111 | `6786cbe958b03c456440927319a894e0692b6c6d` | 2026-09-24 | Ziliang_Zhou | docs(memory): supplement documentation for jiuwen_memory_provider |
| 112 | `48d56d114f04c8e9c315c9c1e0506d898c24652e` | 2026-09-30 | wangcan103 | docs(swarm): archive F_115 model-hint fail-fast and sync specs |
| 113 | `e64028d3d5039d6ad84d269fee4567cb738b4083` | 2026-09-30 | wangcan103 | docs(swarm): drop the known-leftovers section from F_115 |
| 114 | `7fa5354a34c8ea04d59d8bed9c3090f6e28e5282` | 2026-10-07 | ZezeChang | docs(swarm): document organization contracts and usage |
| 115 | `0b7cfd05ab30dc212a34c3bb41c33b566afa7625` | 2026-10-12 | alan_cheng | docs(swarm): document provider member model negotiation |
| 116 | `3113f2addf4dbdfd0e6caa8c3157490bc6ec0ce0` | 2026-10-12 | alan_cheng | docs(swarm): document third-party provider team members |
| 117 | `5d74a4e9a5f194efd6d01c60d63b38e42ba4d563` | 2026-10-12 | alan_cheng | docs(harness): record kimi-code verification in F_07 |
| 118 | `ac75d6a7050087f50389f8a8f221d09619561e2c` | 2026-10-12 | alan_cheng | docs(harness): document managed install of provider packages in jiuwenswarm |
| 119 | `e70db35cee68183b5e3ebfda22b691c8d51a2bae` | 2026-10-12 | alan_cheng | docs(harness): document provider-owned model config |
| 120 | `fba94eaca9252dc62cab85ffcaa0d99f9cd6dad3` | 2026-10-12 | alan_cheng | docs(harness): document third-party harness provider packages |
| 121 | `67ec00b928d64679af2b8692476e325e81adf344` | 2026-10-13 | alan_cheng | docs(harness): document hot replacement of installed providers |
| 122 | `6c388b21639620ca0b5c29de4d92f2985bc4e54f` | 2026-10-13 | alan_cheng | docs(harness): document auth fallback as a protocol capability |
| 123 | `92bddae72682c46aef6b4adb99108c806a3c1904` | 2026-10-13 | alan_cheng | docs(swarm): document fallback negotiation for third-party members |
| 124 | `edecae72625cfa0d188e9ec4720b523d421bdfa4` | 2026-10-13 | alan_cheng | docs(swarm): archive F_118 and sync specs for task-scoped members |
| 125 | `1f61da7c816372d976061a28131f684815519de6` | 2026-10-14 | chenchunzhou | docs(swarm): Document agent_template expert worker in F_118 and S_18 |
| 126 | `2bd2375b8cf584587a2b64dd37c7fe69c7211a5f` | 2026-10-14 | alan_cheng | docs(agent-teams): archive F_119 and revise scheduling, schema and tool specs |
| 127 | `a2be94406091877a70a851e924e729a338e442bc` | 2026-10-14 | alan_cheng | docs(agent-teams): archive F_121 and revise the coordination spec |
| 128 | `f7b4505e9f9f9b2a87187b49f7a62d2f9e7c85cb` | 2026-10-14 | alan_cheng | docs(agent-teams): archive F_120 and revise coordination and tool specs |
| 129 | `1c8b9f1d9dd90664e1b66633b75c959afe73613a` | 2026-10-15 | chenchunzhou | docs(swarm): Note that leader is not a swarmflow worker |
| 130 | `8159e5be54e5660e39b630f64380c7d20815e1fe` | 2026-10-15 | alan_cheng | docs(swarm): archive F_123 team tools direct under progressive exposure |
| 131 | `933401e7850b26650e2215b9d4c4ddad42c33e9a` | 2026-10-15 | chenchunzhou | docs(swarm): Wrap two over-long docstring lines in WorkflowProgressEvent |
| 132 | `9f9765495ba10151f127895fce7bffc577a01635` | 2026-10-15 | alan_cheng | docs(swarm): archive F_122 framework-bootstrapped generic team |
| 133 | `e6444c938c0896fe5c39a1407e75d3b0d9eae7c1` | 2026-10-16 | alan_cheng | docs(swarm): archive F_124 monitor message template meta |
| 134 | `1c1468c72eab0ce59b6a0b39ca0fa0dffd48a538` | 2026-10-19 | fenglitong | docs(attribution): record follow-up feedback and superseded deliverables |
| 135 | `2d11588c2954b32f2a9ec5a8941983f075f02828` | 2026-10-19 | LyttonFeng | docs(attribution): add demo cases and change notes |
| 136 | `40eefedcad2ec083bf646f29248e6ef48d2acfb6` | 2026-10-19 | fenglitong | docs(attribution): note the final-reply fix |
| 137 | `9c8baa7d570e7ff4fd880c4e6d3ec559231e1200` | 2026-10-19 | fenglitong | docs(attribution): record selection scope and process-signal rules |
| 138 | `9e6a709f6edcfac0f7e45a7a5f646c07371679fd` | 2026-10-19 | fenglitong | docs(attribution): note the outside-selection block fix |
| 139 | `06ed3bb8e060443545528678eaeb68a00e7904e3` | 2026-10-20 | fenglitong | docs(attribution): record the delivery-round rule and the requirement list |
| 140 | `14ab0b63626e6e05c96dfa0b54d29f7027a79d96` | 2026-10-20 | fenglitong | docs(attribution): note the final-files fix |
| 141 | `1f44b0dff275e9b0e2d63a0352abdb270ca9659f` | 2026-10-20 | fenglitong | docs(reward): rewrite the README for readability |
| 142 | `32a7cd7075c9c21661905186d4d6bd4ebb135eed` | 2026-10-20 | fenglitong | docs(attribution): record the JiuwenSwarm develop adaptation |
| 143 | `3a87ba9296d799e7dbdbcf236b39b98acb10ea93` | 2026-10-20 | fenglitong | docs(attribution): record the README rewrite |
| 144 | `46aa7cda546cf4ef731d4dfa4ae0d3ed1408e2d2` | 2026-10-20 | fenglitong | docs(attribution): explain how the two kinds of user input are used |
| 145 | `6038fd53e9960f79919d4407d1b39277556dcffc` | 2026-10-20 | alan_cheng | docs(harness): archive F_22 and revise S_13 for cards and async loading |
| 146 | `612cfdb35145cc876f2181e23a201f48dd561517` | 2026-10-20 | fenglitong | docs(attribution): mark all commits as pushed |
| 147 | `7909ada5584247dd6c1e108e79281612766ef751` | 2026-10-20 | fenglitong | docs(attribution): add the Saturday-planning session as a demo case |
| 148 | `847ce8831583f5f20daa2ddb73c0adfb773696e1` | 2026-10-20 | fenglitong | docs(attribution): mark all commits pushed and note TTSE selection scope |
| 149 | `a3d5f92a85aacb9de16f8df3f62b6f732050a5cd` | 2026-10-20 | alan_cheng | docs(swarm): archive F_125 and revise team specs for expert spawning |
| 150 | `a476ee725ccd9a3a30b7243dd736a1f76d462957` | 2026-10-20 | fenglitong | docs(attribution): state the selection rule and items 6, 8 plainly |
| 151 | `adc5d26374fb21b4b747e05daa8d112a2d7d1fbe` | 2026-10-20 | fenglitong | docs(attribution): record the unknown-challenge scoring change |
| 152 | `df3ec3de356446ccdf25a1f263c41810b7a53e52` | 2026-10-20 | fenglitong | docs(attribution): record the reward rename |
| 153 | `dff347de99ed27010a7364bef3f9fe25de929537` | 2026-10-20 | fenglitong | docs(attribution): record the hand-over step rule |
| 154 | `f88214033878ed97deb1f048615d3262c55060e6` | 2026-10-20 | fenglitong | docs(reward): drop the RewardPack section and state the evidence limits |
| 155 | `fdb206e0ab9d4c648aba2d3cc395fda38e1faadd` | 2026-10-20 | fenglitong | docs(attribution): drop the config migration note, note the schema version |
| 156 | `b31e9cf1376c24d1bb10c7fc0fa792001f4e9090` | 2026-10-21 | alan_cheng | docs(harness): relocate documentation into agent infrastructure |
| 157 | `4495035659cfa093d2ad0b7fc3c0f21ea6eee0a9` | 2026-10-22 | alan_cheng | docs(swarm): document scheduled member peer messaging (F_126) |
| 158 | `491d590c2cf98ff4566a868320a7b4b4090c141e` | 2026-10-22 | alan_cheng | docs(swarm): document inline external CLI members |
| 159 | `eba3566309feafabeb67018fedfdcc6ce141a9bc` | 2026-10-23 | alan_cheng | docs(swarm): document scheduled member subtasks |
| 160 | `4ff04282023d0ae136885d140986b6259dc71aa2` | 2026-10-25 | alan_cheng | docs(designs): move legacy expert group design out of SDD-0010 folder |
| 161 | `6bab502718405b34dfbe114432f2e5e1cab84a59` | 2026-10-25 | alan_cheng | docs(swarm): document task revisions and staleness |
| 162 | `8a7d03da469c202a8b0d5151387e9031725806a3` | 2026-10-26 | alan_cheng | docs(designs): move swarm application designs to host repository |
| 163 | `c901180d2cccfd3d5509e11a3ada723d413fd84a` | 2026-10-26 | alan_cheng | feat(docs): preserve authors and generate module-based indexes |
| 164 | `1dd1c8b3b658311c1ffaed2c6fefdd6a1c571a77` | 2026-10-29 | alan_cheng | docs(swarm): renumber inline worktree isolation feature doc to the timestamp scheme |
| 165 | `da6045f3b0c66de64b3d9bf8cfebfbd950154b18` | 2026-11-02 | hwxiajing | docs(swarm): record the teammate swarmflow orchestration design |
| 166 | `116987c6df594e35b091afef53a48c88b4e9629f` | 2026-11-03 | alan_cheng | fix(docs): remove document counts from generated indexes |
| 167 | `feabf852c580a8f85b6c79ef6a80687ed8893138` | 2026-11-03 | alan_cheng | docs(runtime): record compatibility contracts and regression evidence |

## 测试（123 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `f904f6357de0f87c90d2e0d94e234e496f38b81a` | 2026-05-25 | alan_cheng | test(bash): isolate destructive-warning amend to a throwaway repo |
| 2 | `e29814f044f14232af7ff7c8018ef3373b4133d5` | 2026-05-27 | alan_cheng | test(swarm): cover operator / member external-access scopes |
| 3 | `25a6d6eddc9e457575e50bdb90bde2cdf61ade5d` | 2026-05-30 | alan_cheng | test(swarm): update build_team call sites to use CapabilityOverrides |
| 4 | `456a6fd87d0d49ac7a2141b7eecd63670034e7a2` | 2026-06-01 | alan_cheng | test(swarm): cover four role-specific spawn tools and capability gating |
| 5 | `8ad1124851e428316ca7d6a825d73834cace7c41` | 2026-06-01 | alan_cheng | test(harness): cover SuperHarness critical regressions |
| 6 | `4afcc4e705e2e3d29963fed07589c0dbb24c37ca` | 2026-06-02 | alan_cheng | test(swarm): cover NativeHarness state/round events and resume |
| 7 | `6b62b64ee1bfbe7e49246fe577f6cf05e4b61ffb` | 2026-06-02 | alan_cheng | test(swarm): fix stale import/patch paths in team harness tests |
| 8 | `a12252677f27ed8a0d1dc9783e2f0f85c23d7742` | 2026-06-02 | alan_cheng | test(swarm): cover NativeHarness over real task loop |
| 9 | `af33dbe03630fcd79c9612e171cf2972c4f8b5ec` | 2026-06-02 | alan_cheng | test(swarm): cover swarmflow engine, worker backend, and leader wiring |
| 10 | `1165476caa404b8bbf763f0d43358719d91f2044` | 2026-06-03 | alan_cheng | test(swarm): cover leader self-monitor and result-aware repeat |
| 11 | `854b2b13f100127a0ee181f600bea1547cd8b8ed` | 2026-06-03 | alan_cheng | test(swarm): cover reliability detectors, rail, handler and dispatch |
| 12 | `e54524a2703e41418e871c4fdc4e988782863517` | 2026-06-03 | alan_cheng | test(swarm): add live-native TeamHarness + StreamController E2E |
| 13 | `f350e3cbd96f3e67ae22afa79420bd27ee9dadfd` | 2026-06-03 | alan_cheng | test(swarm): cover swarmflow per-call model routing |
| 14 | `fbebb34bff00b38f5fcb8acb315cb953049ae824` | 2026-06-03 | alan_cheng | test(swarm): cover MemberRuntime adoption across team harness layers |
| 15 | `178a93b52abd58190fc0ae75a7e41791f5644906` | 2026-06-04 | alan_cheng | test(swarm): adapt harness tests to forward construction + manifest rails |
| 16 | `007a2ff9fdacdb0ec3873b677671c952c6f17896` | 2026-06-05 | alan_cheng | test(swarm): cover lazy swarmflow import inside run body |
| 17 | `617baa564dab971bd7ca2e04dff5ecd10425ed9a` | 2026-06-05 | alan_cheng | test(swarm): cover async-tool runtime and swarmflow feedback |
| 18 | `7509acf1b921bd66c7c8ed8e3a4e709cf0977a0f` | 2026-06-05 | alan_cheng | test(swarm): follow enqueue_initial_mailbox_poll rename |
| 19 | `977beb6bfffe580c4382e514b4b2b67c41c56fe4` | 2026-06-05 | alan_cheng | test(swarm): guard team rails are provider-built fresh, never cached |
| 20 | `9dcd87e05e6922622e8b58bf0ed22a4e0eed25c4` | 2026-06-05 | alan_cheng | test(swarm): cover harness subscribe and spawn first-start injection |
| 21 | `fa992c9b898772de017e2b3408fb1f3d00976172` | 2026-06-05 | alan_cheng | test(swarm): cover run_once and worker TeamHarness derivation |
| 22 | `59a675dd57ea929e071f0a410ba7db1d22679869` | 2026-06-06 | alan_cheng | test(memory): anchor pyproject path to repo root in agentarts test |
| 23 | `6659d06d9ce9f55c92ab492fe37e6be2cfbc1050` | 2026-06-06 | alan_cheng | test(memory): align coding memory rail test with add_ability API |
| 24 | `22feff3639f4499bf15c09c56a10e3c762736b9d` | 2026-06-08 | alan_cheng | test(swarm): assert human correlation id is deterministic |
| 25 | `373c1d4df42bc4a97d696b5601b1301e4f88fccd` | 2026-06-08 | alan_cheng | test(swarm): cover stateful session primitives and avatar backend |
| 26 | `4199ba4608124b37e8ec5b53d9b9332993016a0f` | 2026-06-08 | alan_cheng | test(swarm): cover swarmflow human reply routing and narration |
| 27 | `21de804bb75fb9c66de5f39d57c91576ee05c343` | 2026-06-16 | alan_cheng | test(swarm): swarmflow E2E from team entry + unit coverage for fixes |
| 28 | `e1b52995b003807c6c3dde4d29df3d7f8c863441` | 2026-06-16 | alan_cheng | test(swarm): cover journal path wiring and meta-only loader |
| 29 | `1dff1518437bfb89fa853753b3056eb45413381c` | 2026-06-17 | alan_cheng | test(swarm): journal WAL/program-order unit tests + optional e2e teardown |
| 30 | `2ed2fe35865e6d22a732fd3bd42b93fff0ebe798` | 2026-06-22 | alan_cheng | test(swarm): async tool DAB unit coverage |
| 31 | `30618a0d70f04df7dd28ba230ecebe5b2d3a742f` | 2026-06-22 | rolandyang | test(evolving): fix operator init order in test helper so operators pick up configured prompt |
| 32 | `67b6917a096d77c524e8837453f2a57e29f1ebe2` | 2026-06-22 | z2tng | test(agent_evolving): stabilize react agent evolve ST |
| 33 | `aebbd6fa1aa757a289dd737a6f828adbb2fa0ff4` | 2026-06-22 | alan_cheng | test(swarm): cover swarmflow tool/primitive surface alignment |
| 34 | `4f781fb82c226f6a46f5abf60869f2ab9cdae720` | 2026-06-23 | zhaoyulu1 | test(trace): add test case for otel tracer handler |
| 35 | `84c4de45a8b8e83b82d25e34c04a488ba3e2e66e` | 2026-06-23 | alan_cheng | test(swarm): swarmflow pause/resume engine unit tests + e2e |
| 36 | `a15469dd9985e4e9de3c90d62c28896c043bacca` | 2026-06-23 | alan_cheng | test(swarm): drop team_backend kwarg from swarmflow worker/tool tests |
| 37 | `dcdd88fc915a64e3223c79a917cc7ee5e6c643e5` | 2026-06-23 | alan_cheng | test(swarm): real-LLM every-phase pause/resume e2e + controller/abort_all units |
| 38 | `163048109dacfac294ac46d855518de3ee24049c` | 2026-07-02 | alan_cheng | test(swarm): exercise task cancel in the db stress workload |
| 39 | `1f5abff1590d1d0481c72ba0fed0e2bd15e548d1` | 2026-07-02 | alan_cheng | test(swarm): cover task dependencies and view_task detail in db stress |
| 40 | `0f71b003f66a54572fd0ed736f7f87a2ed595589` | 2026-07-03 | alan_cheng | test(swarm): adapt tests to desc/prompt split, add leader prompt test |
| 41 | `75d51455ff1d0d225ccc8efbb00989920d6a6f59` | 2026-07-03 | alan_cheng | test(swarm): migrate policy tests off role_policy |
| 42 | `8b2ea09b336839d6bb78a909dbde9a95eba01168` | 2026-07-03 | alan_cheng | test(swarm): cover HITT contract/roster split in team policy rail |
| 43 | `e366973c7a45071b8327e1e11baed761b1ac8aef` | 2026-07-03 | alan_cheng | test(swarm): migrate policy tests off build_system_prompt |
| 44 | `c1dea40af12080b48418f08a24e837491f26e7d2` | 2026-07-07 | alan_cheng | test(swarm): cover one-active-claim guard and revoke-based reassignment |
| 45 | `02d06982f83460721c42b8599096a39c302bc26c` | 2026-07-08 | alan_cheng | test(swarm): cover atomic reassign, targeted cancel/edit signals, and dispatch routing |
| 46 | `2dfc65975fc000411ad1e46044ea5d7a16f11324` | 2026-07-09 | alan_cheng | test(swarm): cover the verify gate, reviewer column and migration |
| 47 | `322dc7e000b0b27d3f99f3011bdf9ee046124b53` | 2026-07-09 | alan_cheng | test(swarm): cover condition-named state machine and tool variants |
| 48 | `407c7a7b09846eae680b32e93bd6330763e9b31e` | 2026-07-09 | alan_cheng | test(swarm): cover pending-resume marker, cold resume and its query |
| 49 | `470f75f5097a9c6ec3284e0a922ecb225974e5d8` | 2026-07-09 | alan_cheng | test(swarm): cover phase-aware pause/abort, warm resume, continuation |
| 50 | `bd8b8d1478f9b928ffe3808385dfbb288b3f9d42` | 2026-07-10 | alan_cheng | test(swarm): cover the scheduler, review voting and dispatch config |
| 51 | `44823ad8a2ad00480d021f45e52db8fb48c59962` | 2026-07-13 | min_gitcode | test(agent_teams): cover external CLI SSH transport |
| 52 | `ac2b8bba5ec61791bd32444124cb155345976ef9` | 2026-07-14 | alan_cheng | test(swarm): cover message templating, meta storage and delivery expansion |
| 53 | `e060ca3951aa9b3cf915011803b982d26b98cb9f` | 2026-07-14 | zhanglongmin | test(agent_teams): add external claude pause resume e2e |
| 54 | `6044264d900ad8614c84519d05c7fbbdb69c1ab8` | 2026-07-16 | zhanglongmin | test(agent_teams): fix Claude SDK runtime database config |
| 55 | `37293562209d5e9d80069da89c0c7138b58eea07` | 2026-07-17 | alan_cheng | test(swarm): cover the swarmflow token budget end to end |
| 56 | `8f1754efdff29b3a5f719699999aa1920c3d7bfa` | 2026-07-17 | alan_cheng | test(swarm): unify system-test model endpoints behind one config |
| 57 | `dc498378b26eabb53a79ecd46f5de0a8e22eb778` | 2026-07-17 | alan_cheng | test(swarm): move the swarmflow E2E suite into its own directory |
| 58 | `725a0639cc5e72033e8bf7817f3b65ca2ba845be` | 2026-07-28 | alan_cheng | test(swarm): cover cwd/workspace separation for members and workers |
| 59 | `c4b7f1d51b169090fa5076d08fad464b2d7b5c19` | 2026-07-28 | alan_cheng | test(swarm): cover startup board survey and backlog stall sweep |
| 60 | `a8eb4cdcf3b3574ccb2e388f273f5e72a7c3760e` | 2026-07-29 | alan_cheng | test(swarm): cover the user-message hook and team context delivery |
| 61 | `bb13d35b7f971e6a666a72a37174d2f095b98d3f` | 2026-07-29 | alan_cheng | test(swarm): cover team context delivery and roster deltas |
| 62 | `6758d2f071f70a6c19d686dd296ab8f2c09d79ba` | 2026-07-31 | alan_cheng | test(swarm): cover input batches and superseded board dropping |
| 63 | `0401bfa2d44ac802f06d04660a9864ff0526c9ba` | 2026-08-08 | alan_cheng | test(swarm): cover the leader's two prompt lanes |
| 64 | `21187b2c493e6a473d03f898886c00c07f27d777` | 2026-08-08 | alan_cheng | test: drop fixed waits from unit tests that only assert output |
| 65 | `50359d5dc4e5d9d2d52e2d013b1bd501aa1773e3` | 2026-08-10 | 2401_87256159 | test(evolution): cover rail-owned reviewer feedback flow |
| 66 | `f8053abeb89afb39f2b5dd99fa3aedc1fea22448` | 2026-08-11 | alan_cheng | test(swarm): cover the bounded steering drain |
| 67 | `86b0891c1a2b9a37ccd96538613b8c1970b77b2b` | 2026-08-12 | alan_cheng | test(swarm): guard that the library state file has a single parser |
| 68 | `9a1a3e3e6e3bd06b387bdee6c670c9c75afb57f4` | 2026-08-12 | alan_cheng | test(swarm): cover the team Skill rail and visibility declarations |
| 69 | `ce21a9b7cfcce28923fba6c47758d60c624b69be` | 2026-08-13 | openJiuwen-bot | test(context_evolver): add unit tests for bank_io.py and role_normalizer.py |
| 70 | `844dd4895727edd5fd71e2e929406728274be9e5` | 2026-08-15 | alan_cheng | test(harness): assert the nested-skill hint names a working call |
| 71 | `888d92fba7e32410639518b9dd06c37aaaec5941` | 2026-08-15 | alan_cheng | test(swarm): assert an all-shutdown roster never auto-cleans |
| 72 | `8acf6790ffeeb34c683f83b5187d0c09465dd9eb` | 2026-08-15 | alan_cheng | test(harness): cover grouped, nested and symlinked skill layouts |
| 73 | `fe87e4685ea3317948f1b2db0a32d108e6a3f2f6` | 2026-08-15 | alan_cheng | test(swarm): cover reassign across the task status boundary |
| 74 | `33eb4c1a3d9fc13c1d8eea4d4e0c1f995cf32ab4` | 2026-08-17 | alan_cheng | test(swarm): cover the agent-tier rail split and its handoff |
| 75 | `18d45d6e90d3735ef72c1ee9ad2467821212f7db` | 2026-08-28 | openJiuwen-bot | test(context_evolver): cover offline L2 pairwise reflection |
| 76 | `6251331b482062f89fe258b87a3dd4ebf499b477` | 2026-09-01 | alan_cheng | test(harness): cover ask_user trajectory events and team subject roots |
| 77 | `7669758b1f862bbcd1963d950b3289779b77d3ef` | 2026-09-01 | alan_cheng | test(swarm): cover team member execution subject and team span identity |
| 78 | `bc10d5b074d3a3b5aab9017a1426ef0f75e9a6d8` | 2026-09-01 | alan_cheng | test(swarm): cover send_message targets field validation |
| 79 | `060f4c31c6ed527a6390e4aff2c0542b3ab2e895` | 2026-09-09 | alan_cheng | test(swarm): run native e2e on a real model and cover codex observability wiring |
| 80 | `5540c94ff11446b3791e194658607ab069775e1b` | 2026-09-09 | alan_cheng | test(swarm): cover auth fallback ratification and native endpoint restore |
| 81 | `79503bb6c332cae3bbdfbc3d8d979dc71b396ad4` | 2026-09-09 | alan_cheng | test(swarm): cover Codex request_user_input mapping and e2e |
| 82 | `c2d79b57a728de192e4066a67faeb19008ca5ffa` | 2026-09-09 | alan_cheng | test(swarm): cover harness providers, io adapter and protocol member wiring |
| 83 | `e91ba198d9f63734b3e6dd94f670336aa84a2f3f` | 2026-09-09 | alan_cheng | test(swarm): cover early codex steer queueing |
| 84 | `6e66de2f82fa8c38af700d7a3b8748a089294748` | 2026-09-10 | alan_cheng | test(swarm): cover NativeHarness protocol controls and manifest loading |
| 85 | `2429b87dfc01584d869b51f7012bd3fa8145c852` | 2026-09-11 | alan_cheng | test(swarm): verify portable skill copying and CLI discovery |
| 86 | `7c4dd33c46d0e8e54ed058f2b64ca7fa993d855b` | 2026-09-11 | alan_cheng | test(swarm): cover native_v2 checkpoints and endpoint recovery |
| 87 | `f623808495a7f510d99685eade44480bf5322b0a` | 2026-09-11 | alan_cheng | test(swarm): cover DSH capabilities and harness prompt policies |
| 88 | `0bf618cc039c1bb56550561dd01fa18d57de36c2` | 2026-09-16 | alan_cheng | test(harness): cover model-facing tool result rendering |
| 89 | `91b4001ff9cb2ae71926cc23608dd9839fa0629d` | 2026-09-17 | alan_cheng | test(harness): adapt upstream tests to render_for_llm |
| 90 | `1b7c2b7198ca26319b83a17badcfbc3dea4837e9` | 2026-09-20 | alan_cheng | test(swarm): cover the model_name rejection hints |
| 91 | `bccbaf71dccfd29022f35bfb554f44f6b2c50a0d` | 2026-09-20 | alan_cheng | test(swarm): cover built-in model selection for external CLI members |
| 92 | `280b7ca18d21c50edd0a352e230f0d6eb0ae6dfe` | 2026-09-22 | ubaidillah-ariq | test(rsi): pass required submit_tool to _build_reflection_agent in test |
| 93 | `7e08eff180e71efb99bac0315f2a10f9d6ef8eb8` | 2026-09-23 | yangzequ | test(mcp): restore streamable HTTP integration coverage |
| 94 | `33d189486689413c8c6ffa5375c17e57f70c1730` | 2026-09-30 | wangcan103 | test(swarm): cover model fail-fast, session pinning, and timeout budget |
| 95 | `90a1501c57c4762a5c8196819f021a31625e2bab` | 2026-10-07 | ZezeChang | test(swarm): cover organization lifecycle and workspace concurrency |
| 96 | `fa50040662f54875285cb1453a682007602d0d34` | 2026-10-09 | ZezeChang | test: stabilize training stop and route stream tests |
| 97 | `05b801f3daf61e6ce1f15d34161c54ef9a4fe041` | 2026-10-12 | alan_cheng | test(swarm): cover third-party provider team members |
| 98 | `2d56da09d1d5cb11175cc7c95a665b25a08000ef` | 2026-10-12 | alan_cheng | test(harness): cover provider model config mapping |
| 99 | `385a5e90906d53c25bed5676f164b6836609c4b0` | 2026-10-12 | alan_cheng | test(swarm): cover third-party provider member models |
| 100 | `4180c1860870d4a4e6d6f7e6e748370c558eb5b3` | 2026-10-12 | alan_cheng | test(harness): cover third-party provider discovery and skill scan dirs |
| 101 | `22f090d82bed7f25469efbc00013ff8d5083d784` | 2026-10-13 | alan_cheng | test(swarm): cover inline members, dormancy and handoff briefs |
| 102 | `641394fd55405f7748e9cb29110bd26691127451` | 2026-10-13 | alan_cheng | test(harness): cover the skeleton auth fallback |
| 103 | `66139de8f3b6e37bb53139cc5183301cb02cdcb2` | 2026-10-13 | alan_cheng | test(harness): cover reimporting a replaced provider |
| 104 | `7ef0e19b1a7db0f5004d24415101d382759b337f` | 2026-10-13 | alan_cheng | test(personal-context): keep the pipeline event wait from timing out on busy CI |
| 105 | `d7497ae4be141b61e603f2e34aec24d8d531dc29` | 2026-10-13 | alan_cheng | test(swarm): cover fallback negotiation for third-party members |
| 106 | `15c5c2480bc6a665af0f62b7d078c822cd316ec8` | 2026-10-14 | alan_cheng | test(agent-teams): cover autonomous completion summaries, claim upstream and handoff wording |
| 107 | `1a6cae0dad51a92309e03dd585ea92ceff6d7a86` | 2026-10-14 | chenchunzhou | test(swarm): Cover agent_template expert worker routing and locking |
| 108 | `38e79449257399c90775e88c0bf049905c3af774` | 2026-10-14 | alan_cheng | test(agent-teams): cover scheduled completion summary, outcome notice and note view |
| 109 | `c21431d3c76f4a2cb2f5ed41944b07c913db3978` | 2026-10-14 | alan_cheng | test(agent-teams): cover the autonomous leader's quiet board events |
| 110 | `08d8aa0b649effaadb9c6a100e862b846fc12a76` | 2026-10-15 | alan_cheng | test(core): reset MockMemory singleton between memory component tests |
| 111 | `1b09a576553240839021943f69619f88ebb09d3e` | 2026-10-15 | alan_cheng | test(swarm): cover framework team bootstrap and policy delivery |
| 112 | `85d8e9479b35bc3885dd6121b8b9cc153c0d85e9` | 2026-10-15 | alan_cheng | test(swarm): cover TeamToolRail direct exposure under progressive policy |
| 113 | `d42b093bca94670c37e5fed7c712c49159f3c989` | 2026-10-15 | chenchunzhou | test(swarm): Cover the leader-specific agent_template rejection |
| 114 | `14ded979419ee7ab1f5ab445b503d8cba409a7a5` | 2026-10-16 | cyr20040123 | test(attribution): cover attribution core module with 130 unit tests |
| 115 | `2fe92da6b93837f9f01d88342153660ada4b8090` | 2026-10-16 | alan_cheng | test(swarm): cover MessageInfo template meta |
| 116 | `de87a5ffc36e2e668db1852443ee39059e27e230` | 2026-10-16 | cyr20040123 | test(attribution): cover attribution core module with 130 unit tests |
| 117 | `892783f92276b4044f05641ef336628a0c081de4` | 2026-10-20 | cyr20040123 | test(attribution): mark bare async tests, skip browser reward tests without playwright |
| 118 | `9c5bc9a03e158f9d4da358967176163a3ac75481` | 2026-10-20 | alan_cheng | test(harness): cover AgentTemplate cards |
| 119 | `dff7069c3f33608cd90fae33b80631f77645cac7` | 2026-10-20 | alan_cheng | test(swarm): cover expert tools, AgentGroup loader and template members |
| 120 | `a1b8fba23e82f93e36dbbb493bb7d8f6599dbfbe` | 2026-10-22 | alan_cheng | test(swarm): cover inline external CLI members in scheduled create_task |
| 121 | `b719218385b6a123fedbef5453a0a1b47519f50c` | 2026-10-22 | alan_cheng | test(swarm): cover scheduled member peer messaging |
| 122 | `29b830c52835bbd2896de217691a9c04e453514c` | 2026-10-23 | alan_cheng | test(swarm): cover scheduled member subtasks |
| 123 | `91cb3d33db60aa889bbcaf6e449ef77acea24a67` | 2026-10-25 | alan_cheng | test(swarm): cover task revisions and staleness |

## 工程/依赖/构建（37 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `132b8d550609c63f3b6e0916768150693b3b0621` | 2026-06-02 | alan_cheng | chore: register level0/level1 pytest markers |
| 2 | `d87cf51c0909efb8a35d3c9315b76912145a8c59` | 2026-06-03 | alan_cheng | style(swarm): clear Huawei lint findings in swarmflow engine and vcs |
| 3 | `1ea35b80b38f9e7f94681f1248c61c2140e3c578` | 2026-06-05 | alan_cheng | chore(deps): make greenlet a core dep via sqlalchemy[asyncio] |
| 4 | `490bfc750b2176a4e1eca22bf9c3df8abcbf90fc` | 2026-06-23 | alan_cheng | style(swarm): resolve codecheck findings and fix test module collision |
| 5 | `dfffda561d543c6ea8ce3469d568db2bf1a0e3a8` | 2026-07-14 | iamcandiceguo | chore: update 0.1.15 to 0.1.16 |
| 6 | `90ecf451e9d816ab87f35329b879d9026c278259` | 2026-07-17 | alan_cheng | chore: ignore harness workspace scaffolding written by unit tests |
| 7 | `21f8beaebf159d8605f2291f887b05a821bbfd2b` | 2026-08-01 | alan_cheng | chore(core): report a per-callback split of every rail chain |
| 8 | `6998c1a8442ae8f179a1c07575d4455f2fabc336` | 2026-08-01 | alan_cheng | chore(core): gate per-chunk stream tracing behind a switch |
| 9 | `9502a53d7649cd5b9e61d5329c14b72f768ba35e` | 2026-08-01 | alan_cheng | chore(core): report slow rail chains and invoke preparation |
| 10 | `c362db57690a885c52405ab8fc8f786fd1b237ac` | 2026-08-05 | wncbdj | chore(llm): Add anthropic dependency |
| 11 | `75c687a066b111c2bb450a790e3b85401eeff1f6` | 2026-08-31 | yangzequ | chore(rsi): relocate rsi code under harness_rsi and scaffold artifact_rsi |
| 12 | `c90e6dd29d9f9d9b1e2d6304f024a3addb1510c1` | 2026-08-31 | alan_cheng | chore: ignore records and sub_agents directories |
| 13 | `395ad194e36063cd00514c73e46e091381b30f32` | 2026-09-01 | alan_cheng | style: fix Huawei coding-standard findings |
| 14 | `3a5b68610e36cd84d04b4c4c229cdc6ebcd506ee` | 2026-09-01 | alan_cheng | chore: drop branch-local config and ignore rules |
| 15 | `4e337e6f2823d4677e50f2ee47768a214d3ecbe8` | 2026-09-01 | alan_cheng | chore(swarm): declare the DSH SDK extra and gate its timing test |
| 16 | `67686ac35436e267380db9c737e0ea4f5eb64168` | 2026-09-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#30: chore(lint): add no-silent-except semgrep rules and except-Exception ratchet |
| 17 | `c18adea4f2a7e8e63f003e0f6dc5bc38a88e453f` | 2026-09-09 | huanghongyun | chore(rsi): exclude examples and their dedicated tests |
| 18 | `7be1c720226130d80151b5fb5ed91c6ea9e4204a` | 2026-09-12 | alan_cheng | style(swarm): wrap lines over the 120-column limit |
| 19 | `893760d9c72dfc9c984e75f642a85897e0e098dd` | 2026-09-20 | zhiqiuzoeyye | style(rsi): fix codecheck format issues |
| 20 | `5c286ae31957e7450baa0c4ed5c8fdfab8d3731a` | 2026-09-29 | iamcandiceguo | modify 0.1.18 to 0.1.19 |
| 21 | `072a53ca8954c715fbcf29e3cd9a32c89f0d3c02` | 2026-10-12 | alan_cheng | chore(harness): rank integration modes and document codex app-server |
| 22 | `41d423258e2b3fb8eea297d1d578274a2b505b93` | 2026-10-12 | ldstyle8 | chore(harness): raise default subagent max_iterations to 50 |
| 23 | `49731d9cb66538ab95dfa85f0779153c7299788f` | 2026-10-12 | alan_cheng | chore(harness): add harness provider integration skill |
| 24 | `5f01e560a085463298682b9a61e715eda26f283c` | 2026-10-12 | alan_cheng | chore(harness): let the provider skill depend on an openjiuwen source checkout |
| 25 | `65f111835d7e55f36528d99b9817ac649af7362f` | 2026-10-12 | alan_cheng | chore(harness): map model choices and switch ACP models in provider templates |
| 26 | `a60a944dbfc159bf225a8b523989d71af2d760bb` | 2026-10-12 | alan_cheng | chore: update code ownership files and document their relations |
| 27 | `e842f3df7b3805d706500915e2f7a87c51382269` | 2026-10-12 | alan_cheng | chore(harness): select ACP models per session and wait for late tool arguments |
| 28 | `300a98889b7e5c95dcb78bac6384042e64b90167` | 2026-10-13 | alan_cheng | style(harness): wrap lines over 120 columns |
| 29 | `5678f5fdd4428c6c9724840a23cdcf198ffbda03` | 2026-10-13 | alan_cheng | chore(harness): add endpoint models and auth fallback to the ACP template |
| 30 | `7bfa35e19d8de6a9cac61297d4917b2f0006b54c` | 2026-10-13 | alan_cheng | chore(harness): derive model requests from the ACP stream in the provider template |
| 31 | `2ad76623aa3bfbb44aa80ce659828f1af4e117f6` | 2026-10-17 | cyr20040123 | chore(deps): sync uv.lock filelock constraint with pyproject |
| 32 | `495e2ef93938f83bb954055daae777a013ced627` | 2026-10-19 | alan_cheng | chore: scope COMMITTER mergers by directory |
| 33 | `a83df87dd2b86e9402adc2aeaab4f20428f5bad7` | 2026-10-20 | cyr20040123 | style(reward): keep readable comprehension within line limit |
| 34 | `f7db1d3a809baa5055337b19c036277b99daa7b9` | 2026-10-20 | cyr20040123 | style(attribution): address codecheck layout and docstring findings |
| 35 | `0288b868bc178158e49c6cb3a8bcf85b885e38e4` | 2026-10-21 | alan_cheng | chore(swarm): stop tracking design testing documents |
| 36 | `32d39fa30205a01822e2391de9f3e4e6228fadbd` | 2026-10-21 | alan_cheng | chore(swarm): integrate AI coding workflow infrastructure |
| 37 | `ecb4cbdc0856bfeb3028009a57b3b21704e1b182` | 2026-10-28 | wncbdj | chore: pin intelli-router to tag feature/intelliRouter_261030 |

## 回滚（4 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `5ed9751bd1109547a178ff5c85514146886ecb4f` | 2026-06-02 | iamcandiceguo | revert: prevent subagents from bypassing parent sandbox restriction |
| 2 | `ed87fd5812a4d3bb55956051d27868a862cb8cd2` | 2026-06-10 | alan_cheng | revert: "fix(agent_teams): gate deliver_input on runtime readiness to prevent NativeHarness startup race " |
| 3 | `ed483a248abbe48654a4219fdf765fce468669cb` | 2026-08-20 | iamcandiceguo | Revert "fix: avoid ignored exception continue in disabled thinking fallback" |
| 4 | `0f1b465be7d9c32a030e7770d267ba5d5f7d7319` | 2026-09-23 | alan_cheng | Revert "feat(agent_teams): observe external CLI members via their own telemetry" |

## 其他（5 个提交）

| # | commit | 日期 | 作者 | 提交说明 |
| --- | --- | --- | --- | --- |
| 1 | `d03ef28ee8812df0206a142096379c4fc6e22032` | 2026-06-30 | dehaiwang | Co-authored-by: dehaiwang<dehaiwang@noreply.gitcode.com> |
| 2 | `54b71563dfd920c3a01e3a976e799918d0cd2259` | 2026-07-28 | liuyuang1124 | [mirror] openJiuwen-ai/agent-core#119: mango merge |
| 3 | `fd916ae537b0a88b41ba65c1ba5e004c42dc6e62` | 2026-08-17 | gcw_6ZTnJdNN | c |
| 4 | `a7c8d7b13e17c4e9d08749b3405c56ce50795d41` | 2026-09-09 | m056838700 | [mirror] openJiuwen-ai/agent-core#1259: [br_0.1.18] Backport browser and search fixes #1147, #987, #1253 |
| 5 | `b2796dfb09f09a58e2ec1e245ff86a69a46bc15c` | 2026-11-03 | openJiuwen-bot | [mirror] openJiuwen-ai/agent-core#1857: Skip online training rail when disabled |

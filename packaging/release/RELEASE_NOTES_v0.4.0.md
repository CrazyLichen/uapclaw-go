# uapclaw-go v0.4.0 发布说明

自 v0.3.3（2026-11-04）以来的第 9 个版本，共 **3046 个提交**（约 1188 feat / 313 fix）。本版是自发布以来体量最大的一次更新，核心为**多智能体团队体系整体落地**与**上下文压缩/记忆能力升级**。

## 主要更新

### 1. 多智能体团队（Agent Teams）体系

- **成员生命周期与外部 CLI 成员**：成员事件循环、spawn 编排、外部 CLI（codebuddy/dsh 等）成员接入与生命周期管理
- **调度与编排**：调度模式（scheduled dispatch）、review 半区、可靠性前置（投递半区/重试）、团队上下文追踪
- **Steer 接线**：实时转向指令全量接入，取消语义对齐上游
- **Fork 与 Checkpoint**：并发会话 fork、快照与恢复
- **Worktree 隔离**：团队成员文件操作隔离到独立 git worktree
- **技能演进工具**：团队级技能在线迭代与审批链

### 2. 中断恢复链路

- 中断/恢复（interrupt/resume）全链路对齐上游：内核 held/paused_call 状态机、build 层五分支补齐、plan_approval 补齐、旧数据 interrupt_id 派生兼容

### 3. 上下文压缩与记忆

- **Forked 压缩栈**：三压缩器、单链 preset、规则压缩、offloader、recall 工具全链落地
- **Topic 流水线**：主题归并与产物呈现
- coding memory 单连接自锁等多项 P0 修复

### 4. 模型路由与池

- 模型组与 intellirouter 路由编译器、模型池、gateway 模型目录
- OpenRouter 显式缓存、思考输出流式渲染
- token 上限可配化（窗口映射/请求上限）

### 5. Web / 网关

- **富媒体**：图片/音频/视频消息八 method 支持，150MiB 帧修复
- HTTP 调度面（mapped 全量路由）、workspace handler 并入
- 多模态内容结构对齐、file-api 迁 channels/web

### 6. 双工音视频

- 全双工语音通话：control 流程接线、容量结算与等待释放、取消语义

### 7. 权限系统

- 权限工具大批量增强（约 48 提交）：审批流、send_file 资产账本防 TOCTOU、沙箱路径解析修复

### 8. Workflow

- swarmflow 工作流引擎移植（约 40 文件 / 11,900 行）

### 9. 稳定性

- 313 项 fix：并发竞态（transport send-on-closed、SSE goroutine 泄漏）、流式 tool_call 增量聚合、GBK 编码、PTY/timeout 语义等

## 资产

- `uapclaw-0.4.0-linux-amd64.tar.gz`（内嵌 Web 前端，解压即用）
- `uapclaw-0.4.0-windows-amd64.zip`
- `sha256sums.txt`

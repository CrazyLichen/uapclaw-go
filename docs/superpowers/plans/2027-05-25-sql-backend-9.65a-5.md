# 9.65a-5 SQL 后端实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `agent_teams/tools/database` 包实现 SqlTeamDatabase + 4 个 SQL DAO，对齐 Python engine.py + 4 个 DAO 的 SQL 后端。

**Architecture:** 混合 GORM 方案——GORM 管理引擎生命周期和静态表（AutoMigrate），动态表手写 DDL + `db.Table()` CRUD。事务管理混合模式：简单方法用 `db.Transaction()`，跨表操作用 `WithTx()`。

**Tech Stack:** Go + GORM + SQLite（测试） / PostgreSQL（生产） / MySQL（可选），`database/sql` 驱动已在 go.mod

**Design Doc:** `docs/superpowers/specs/2027-05-25-sql-backend-9.65a-5-design.md`

---

## File Structure

```
internal/agent_teams/tools/database/
├── doc.go               # 修改：更新文件目录
├── config.go            # 不变
├── models.go            # 不变
├── database.go          # 修改：新增 NewTeamDatabase 工厂
├── fsm.go               # 不变
├── engine.go            # 修改：删除5个占位函数
├── memory_impl.go       # 不变
├── team_dao.go          # 删除
├── member_dao.go        # 删除
├── task_dao.go          # 删除
├── message_dao.go       # 删除
├── sql_engine.go        # 新增：SqlTeamDatabase 门面 + newGormDB + DDL + 清理 + WithTx
├── sql_team_dao.go      # 新增：SQLTeamDao 5方法
├── sql_member_dao.go    # 新增：SQLMemberDao 8方法（含 CAS）
├── sql_task_dao.go      # 新增：SQLTaskDao 18方法 + 5辅助函数 + withTx
├── sql_message_dao.go   # 新增：SQLMessageDao 7方法（含重试+watermark）
├── sql_engine_test.go   # 新增：引擎初始化 + DDL + 清理测试
├── sql_team_dao_test.go # 新增
├── sql_member_dao_test.go # 新增
├── sql_task_dao_test.go   # 新增
└── sql_message_dao_test.go # 新增
```

---

### Task 1: 删除占位文件 + 清理 engine.go

**Files:**
- Delete: `internal/agent_teams/tools/database/team_dao.go`
- Delete: `internal/agent_teams/tools/database/member_dao.go`
- Delete: `internal/agent_teams/tools/database/task_dao.go`
- Delete: `internal/agent_teams/tools/database/message_dao.go`
- Modify: `internal/agent_teams/tools/database/engine.go`

- [ ] **Step 1: 删除4个占位 DAO 文件**
- [ ] **Step 2: 清理 engine.go，删除5个占位函数，保留 GetCurrentTime + SanitizeSessionIDForTable**
- [ ] **Step 3: 编译验证**
- [ ] **Step 4: 提交**

---

### Task 2: 新增 sql_engine.go — SqlTeamDatabase 门面 + 引擎

**Files:**
- Create: `internal/agent_teams/tools/database/sql_engine.go`

- [ ] **Step 1: 编写 SqlTeamDatabase 结构体 + newGormDB + Initialize + TeamDatabase 接口方法**
- [ ] **Step 2: 编写 DDL 常量 + createSessionTablesDDL + dropSessionTablesDDL + 辅助函数**
- [ ] **Step 3: 编译验证**
- [ ] **Step 4: 提交**

---

### Task 3: 新增 sql_team_dao.go — SQLTeamDao (5 方法)

**Files:**
- Create: `internal/agent_teams/tools/database/sql_team_dao.go`

- [ ] **Step 1: 编写 SQLTeamDao（CreateTeam/GetTeam/TeamExists/DeleteTeam/GetTeamUpdatedAt）**
- [ ] **Step 2: 提交**

---

### Task 4: 新增 sql_member_dao.go — SQLMemberDao (8 方法，含 CAS)

**Files:**
- Create: `internal/agent_teams/tools/database/sql_member_dao.go`

- [ ] **Step 1: 编写 SQLMemberDao（8方法，重点 TryTransitionMemberStatus CAS）**
- [ ] **Step 2: 提交**

---

### Task 5: 新增 sql_task_dao.go — SQLTaskDao (18 方法 + 5 辅助函数)

**Files:**
- Create: `internal/agent_teams/tools/database/sql_task_dao.go`

- [ ] **Step 1: 编写 SQLTaskDao 结构体 + 动态表名 + withTx + 5个底层辅助函数**
- [ ] **Step 2: 编写 18 个 TaskDao 接口方法（重点 MutateDependencyGraph 5步管线）**
- [ ] **Step 3: 编译验证**
- [ ] **Step 4: 提交**

---

### Task 6: 新增 sql_message_dao.go — SQLMessageDao (7 方法)

**Files:**
- Create: `internal/agent_teams/tools/database/sql_message_dao.go`

- [ ] **Step 1: 编写 SQLMessageDao（7方法，重点 CreateMessage 重试 + watermark）**
- [ ] **Step 2: 编译验证**
- [ ] **Step 3: 提交**

---

### Task 7: 修改 database.go — 新增 NewTeamDatabase 工厂函数

**Files:**
- Modify: `internal/agent_teams/tools/database/database.go`

- [ ] **Step 1: 在 database.go 中添加 NewTeamDatabase 工厂函数**
- [ ] **Step 2: 编译验证**
- [ ] **Step 3: 提交**

---

### Task 8: 更新 doc.go

**Files:**
- Modify: `internal/agent_teams/tools/database/doc.go`

- [ ] **Step 1: 更新文件目录**
- [ ] **Step 2: 提交**

---

### Task 9: 新增 sql_engine_test.go

**Files:**
- Create: `internal/agent_teams/tools/database/sql_engine_test.go`

- [ ] **Step 1: 编写引擎初始化 + DDL + 清理测试**
- [ ] **Step 2: 运行测试**
- [ ] **Step 3: 提交**

---

### Task 10: 新增 sql_team_dao_test.go

**Files:**
- Create: `internal/agent_teams/tools/database/sql_team_dao_test.go`

- [ ] **Step 1: 编写 Team DAO 测试**
- [ ] **Step 2: 运行测试 + 提交**

---

### Task 11: 新增 sql_member_dao_test.go

**Files:**
- Create: `internal/agent_teams/tools/database/sql_member_dao_test.go`

- [ ] **Step 1: 编写 Member DAO 测试（重点 CAS）**
- [ ] **Step 2: 运行测试 + 提交**

---

### Task 12: 新增 sql_task_dao_test.go

**Files:**
- Create: `internal/agent_teams/tools/database/sql_task_dao_test.go`

- [ ] **Step 1: 编写 Task DAO 测试（重点5步管线+终止传播）**
- [ ] **Step 2: 运行测试 + 提交**

---

### Task 13: 新增 sql_message_dao_test.go

**Files:**
- Create: `internal/agent_teams/tools/database/sql_message_dao_test.go`

- [ ] **Step 1: 编写 Message DAO 测试（重点watermark+重试）**
- [ ] **Step 2: 运行测试 + 提交**

---

### Task 14: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 9.65a-5 状态从 ☐ 改为 ✅**
- [ ] **Step 2: 提交**

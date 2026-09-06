# 10.6.5 Permissions Rail + SecurityRail 回填实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 全量回填 swarm 侧 Permissions Rail 胶水层 + SecurityRail 一行回填，使 agentcore 层已实现的权限引擎在运行时生效。

**Architecture:** 6 步逐项回填——owner_scopes 补充 → buildPermissionRail（Deep+Code）→ 热重载 → 请求级 context 注入 → permissions_config_rpc 真实实现 → SecurityRail 回填 + 标记清理。agentcore 层（9.19）已全部实现，本计划仅涉及 swarm 侧胶水层。

**Tech Stack:** Go 1.22+, agentcore/harness/security (PermissionEngine, TieredPolicy, ToolPermissionHost, Patterns), agentcore/harness/rails/security (PermissionInterruptRail, SafetyPromptRail), swarm/schema (PermissionContext, context 注入)

---

## Task 1: owner_scopes 补充

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes_test.go`

- [ ] **Step 1: 编写 CheckAvatarPermission 失败测试**

在 `owner_scopes_test.go` 中添加测试：

```go
func TestCheckAvatarPermission_无权限上下文(t *testing.T) {
    _, err := CheckAvatarPermission("read_file", map[string]any{}, "ch1", "")
    if err == nil {
        t.Fatal("应返回错误：无权限上下文")
    }
}

func TestCheckAvatarPermission_ownerScopes为空(t *testing.T) {
    // mock GetConfig 返回无 owner_scopes 配置
    // 应返回 "allow"
}

func TestCheckAvatarPermission_数字分身允许(t *testing.T) {
    // mock GetConfig 返回 owner_scopes 配置
    // tool 级别 "allow"
}

func TestCheckAvatarPermission_数字分身拒绝(t *testing.T) {
    // tool 级别 "deny" → 应返回 "deny"
}

func TestCheckAvatarPermission_ASK降级为DENY(t *testing.T) {
    // tool 级别 "ask" → 数字分身场景自动降级为 "deny"
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestCheckAvatarPermission -v`
Expected: 编译失败（函数未定义）

- [ ] **Step 3: 实现 CheckAvatarPermission**

在 `owner_scopes.go` 中添加：

```go
// CheckAvatarPermission 数字分身场景单工具 owner_scopes 权限检查。
// 返回 "allow" 或 "deny"（ASK 自动降级为 DENY，数字分身不支持交互审批）。
//
// 对齐 Python: check_avatar_permission() (owner_scopes.py L96-169)
func CheckAvatarPermission(toolName string, toolArgs map[string]any, channelID string, sessionID string) (string, error) {
    // 1. 从 context 获取 PermissionContext（通过 GetConfig + metadata）
    // 2. 无 principal_user_id → deny
    // 3. 读取 config["permissions"]
    // 4. 创建 PermissionEngine
    // 5. 读取 owner_scopes
    // 6. 按 owner_scopes.<channel>.<user> 查找 scopeCfg
    // 7. 调用 ResolveOwnerScopeLevel(scopeCfg, toolName, toolArgs)
    // 8. 调用 engine.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
    // 9. 取 owner_level 和 global_level 中更严者
    // 10. "ask" → "deny"（ASK 自动降级）
    // 11. 返回 "allow" 或 "deny"
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestCheckAvatarPermission -v`
Expected: PASS

- [ ] **Step 5: 编写 ResolveOwnerScopeLevel 失败测试**

```go
func TestResolveOwnerScopeLevel_空配置(t *testing.T) {
    result, err := ResolveOwnerScopeLevel(nil, "read_file", map[string]any{})
    if err != nil { t.Fatal(err) }
    if result != "" { t.Fatalf("应为空，得到 %s", result) }
}

func TestResolveOwnerScopeLevel_字符串级别(t *testing.T) {
    scopeCfg := map[string]any{"tools": map[string]any{"read_file": "allow"}}
    result, err := ResolveOwnerScopeLevel(scopeCfg, "read_file", map[string]any{})
    if err != nil { t.Fatal(err) }
    if result != "allow" { t.Fatalf("应为 allow，得到 %s", result) }
}

func TestResolveOwnerScopeLevel_patterns优先(t *testing.T) {
    // patterns > wildcard(*) > defaults
}

func TestResolveOwnerScopeLevel_defaults兜底(t *testing.T) {
    // 无 tool 级别配置，走 defaults.*
}
```

- [ ] **Step 6: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestResolveOwnerScopeLevel -v`
Expected: 编译失败

- [ ] **Step 7: 实现 ResolveOwnerScopeLevel**

```go
// ResolveOwnerScopeLevel 在 owner-scope 层按三级优先级匹配。
// 返回 "allow"/"deny"/"ask" 或空字符串（无匹配）。
//
// 优先级：1. patterns → 2. wildcard(*) → 3. defaults.*
//
// 对齐 Python: _resolve_owner_scope_level() (owner_scopes.py L172-206)
func ResolveOwnerScopeLevel(scopeCfg map[string]any, toolName string, toolArgs map[string]any) (string, error) {
    if scopeCfg == nil { return "", nil }
    toolsCfg, _ := scopeCfg["tools"].(map[string]any)
    if toolEntry, ok := toolsCfg[toolName]; ok {
        switch v := toolEntry.(type) {
        case string:
            return v, nil
        case map[string]any:
            // patterns 优先
            if patterns, ok := v["patterns"].(map[string]any); ok {
                for pattern, perm := range patterns {
                    if MatchArgs(pattern, toolArgs) {
                        if s, ok := perm.(string); ok { return s, nil }
                    }
                }
            }
            // wildcard
            if star, ok := v["*"]; ok {
                if s, ok := star.(string); ok { return s, nil }
            }
        }
    }
    // defaults
    if defaults, ok := scopeCfg["defaults"].(map[string]any); ok {
        if star, ok := defaults["*"]; ok {
            if s, ok := star.(string); ok { return s, nil }
        }
    }
    return "", nil
}
```

- [ ] **Step 8: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestResolveOwnerScopeLevel -v`
Expected: PASS

- [ ] **Step 9: 编写 MatchArgs 失败测试**

```go
func TestMatchArgs_命令匹配(t *testing.T) {
    result, err := MatchArgs("git status", map[string]any{"command": "git status"})
    if err != nil { t.Fatal(err) }
    if !result { t.Fatal("command 应匹配") }
}

func TestMatchArgs_路径匹配(t *testing.T) {
    result, err := MatchArgs("/home/user/*", map[string]any{"path": "/home/user/file.txt"})
    if err != nil { t.Fatal(err) }
    if !result { t.Fatal("path 应匹配") }
}

func TestMatchArgs_通配符匹配(t *testing.T) {
    result, err := MatchArgs("git*", map[string]any{"raw": "git status"})
    if err != nil { t.Fatal(err) }
    if !result { t.Fatal("wildcard 应匹配") }
}

func TestMatchArgs_无匹配(t *testing.T) {
    result, err := MatchArgs("npm*", map[string]any{"command": "git status"})
    if err != nil { t.Fatal(err) }
    if result { t.Fatal("不应匹配") }
}
```

- [ ] **Step 10: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestMatchArgs -v`
Expected: 编译失败

- [ ] **Step 11: 实现 MatchArgs**

```go
// MatchArgs 简化的参数模式匹配。
// 复用 agentcore harness security patterns 包的 MatchCommand / MatchPath / MatchURL / MatchWildcard。
//
// 对齐 Python: _match_args() (owner_scopes.py L209-231)
func MatchArgs(pattern string, toolArgs map[string]any) bool {
    for key, value := range toolArgs {
        s, ok := value.(string)
        if !ok { continue }
        if (key == "command" || key == "cmd") && harnesssecurity.MatchCommand(pattern, s) {
            return true
        }
        if key == "url" && harnesssecurity.MatchURL(pattern, s) {
            return true
        }
        if (key == "path" || key == "file_path") && harnesssecurity.MatchPath(pattern, s) {
            return true
        }
        if harnesssecurity.MatchWildcard(s, pattern) {
            return true
        }
    }
    return false
}
```

注意：需确认 `harnesssecurity.MatchCommand` / `MatchPath` / `MatchURL` / `MatchWildcard` 在 `agentcore/harness/security/patterns.go` 中的导出函数名，如不同需调整。

- [ ] **Step 12: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestMatchArgs -v`
Expected: PASS

- [ ] **Step 13: 更新 doc.go**

在 `permissions/doc.go` 文件目录中添加 `owner_scopes.go` 条目描述更新（新增 3 个函数）。

- [ ] **Step 14: 运行完整包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -v`
Expected: 全部 PASS

- [ ] **Step 15: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/
git commit -m "feat(permissions): 补充 CheckAvatarPermission/ResolveOwnerScopeLevel/MatchArgs"
```

---

## Task 2: buildPermissionRail 回填（DeepAdapter）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go`（host 回调方法）
- Test: `internal/swarm/server/adapter/deep_adapter_rails_test.go`（新增）

- [ ] **Step 1: 编写 buildPermissionRail 失败测试**

```go
func TestDeepAdapter_buildPermissionRail_未启用(t *testing.T) {
    d := &DeepAdapter{}
    rail := d.buildPermissionRail(map[string]any{})
    if rail != nil { t.Fatal("未启用时应返回 nil") }
}

func TestDeepAdapter_buildPermissionRail_已启用(t *testing.T) {
    configBase := map[string]any{
        "permissions": map[string]any{"enabled": true},
    }
    d := &DeepAdapter{model: someModel}
    rail := d.buildPermissionRail(configBase)
    if rail == nil { t.Fatal("已启用时应返回非 nil") }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -run TestDeepAdapter_buildPermissionRail -v`
Expected: 编译或逻辑失败

- [ ] **Step 3: 实现 DeepAdapter 的 7 个 host 回调方法**

在 `deep_adapter.go` 中添加以下方法：

```go
// getPermissionsSnapshot 返回当前权限配置快照。
// 对齐 Python: get_permissions_snapshot = lambda: get_config().get("permissions", {})
func (d *DeepAdapter) getPermissionsSnapshot() map[string]any {
    cfg := d.configCache  // 或从 agentConfigService 读取
    if cfg == nil { return map[string]any{} }
    perm, _ := cfg["permissions"].(map[string]any)
    if perm == nil { return map[string]any{} }
    return perm
}

// persistAllowRule 将合并后的 permissions 配置写回 config.yaml。
// 对齐 Python: _persist_allow_rule(permissions)
func (d *DeepAdapter) persistAllowRule(permissions map[string]any) bool {
    // 1. 读取 workspace.ConfigFile() 的 YAML
    // 2. 替换 data["permissions"] = permissions
    // 3. 写回 YAML
    // 4. 失败返回 false
}

// resolveWorkspaceDir 返回 workspace 根目录。
// 对齐 Python: get_workspace_dir
func (d *DeepAdapter) resolveWorkspaceDir() string {
    return workspace.WorkspaceDir()
}

// getPermissionYAMLPath 返回 Agent 配置文件路径。
// 对齐 Python: get_config_file
func (d *DeepAdapter) getPermissionYAMLPath() string {
    return workspace.ConfigFile()
}

// requestPermissionConfirmation 对 ASK 级别征求用户确认。
// 对齐 Python: _request_permission_confirmation
// 非 ACP 通道返回 ConfirmActionInterrupt（走内置中断），ACP 通道走 JSON-RPC。
func (d *DeepAdapter) requestPermissionConfirmation(req security.PermissionConfirmationRequest) (*security.PermissionConfirmResponse, error) {
    channelID := sschema.ToolPermissionChannelIDFromCtx(req.Ctx.Ctx)
    if channelID != "acp" {
        // 非 ACP → 走内置中断流程
        return &security.PermissionConfirmResponse{Action: security.ConfirmActionInterrupt}, nil
    }
    // ACP 通道 → 走 JSON-RPC session/request_permission
    // 如果 ACP output manager 尚未实现，降级为 ConfirmActionInterrupt
    return &security.PermissionConfirmResponse{Action: security.ConfirmActionInterrupt}, nil
}

// permissionSceneHook 宿主场景钩子（数字分身/owner_scopes）。
// 对齐 Python: _permission_scene_hook
func (d *DeepAdapter) permissionSceneHook(input security.PermissionSceneHookInput) ([]string, error) {
    permCtx := sschema.PermissionContextFromCtx(input.Ctx.Ctx)
    if permCtx == nil { return nil, nil }

    if permCtx.Scene() == "group_digital_avatar" {
        if input.UserInput != nil {
            return []string{"reject", "[PERMISSION_DENIED] 数字分身场景不支持交互审批"}, nil
        }
        level, err := permowner.CheckAvatarPermission(
            input.NormalizedToolName, input.ToolArgs,
            permCtx.ChannelID, "",
        )
        if err != nil { return nil, err }
        if level == "allow" { return []string{"approve"}, nil }
        return []string{"reject", "[PERMISSION_DENIED] 该工具未被授权在数字分身场景下使用"}, nil
    }

    // owner_scopes 场景
    principalUID := strings.TrimSpace(permCtx.PrincipalUserID)
    chID := strings.TrimSpace(permCtx.ChannelID)
    if principalUID == "" || chID == "" { return nil, nil }

    permAll := d.getPermissionsSnapshot()
    ownerScopes, _ := permAll["owner_scopes"].(map[string]any)
    if ownerScopes == nil { return nil, nil }
    chScopes, _ := ownerScopes[chID].(map[string]any)
    scopeCfg, _ := chScopes[principalUID].(map[string]any)

    ownerLevel, _ := permowner.ResolveOwnerScopeLevel(scopeCfg, input.NormalizedToolName, input.ToolArgs)
    if ownerLevel == "" { return nil, nil }
    if ownerLevel == "allow" { return []string{"approve"}, nil }
    return []string{"reject", fmt.Sprintf("[PERMISSION_DENIED] 该工具未被授权 (owner_scopes: %s)", ownerLevel)}, nil
}
```

- [ ] **Step 4: 实现 collectOptionalToolTags 辅助函数**

```go
// collectOptionalToolTags 从 permissions 配置中收集工具名标签（仅作展示/日志用）。
// 对齐 Python: _collect_optional_tool_tags() (interrupt_helpers.py L56-80)
func collectOptionalToolTags(permissionConfig map[string]any) []string {
    names := make(map[string]struct{})
    if toolsCfg, ok := permissionConfig["tools"].(map[string]any); ok {
        for k := range toolsCfg {
            if s := strings.TrimSpace(k); s != "" { names[s] = struct{}{} }
        }
    }
    if rules, ok := permissionConfig["rules"].([]any); ok {
        for _, entry := range rules {
            m, ok := entry.(map[string]any)
            if !ok { continue }
            rawTools := m["tools"]
            switch v := rawTools.(type) {
            case string:
                if s := strings.TrimSpace(v); s != "" { names[s] = struct{}{} }
            case []any:
                for _, item := range v {
                    if s, ok := item.(string); ok {
                        if s = strings.TrimSpace(s); s != "" { names[s] = struct{}{} }
                    }
                }
            }
        }
    }
    result := make([]string, 0, len(names))
    for n := range names { result = append(result, n) }
    sort.Strings(result)
    return result
}
```

- [ ] **Step 5: 实现 DeepAdapter.buildPermissionRail**

替换 `deep_adapter_rails.go` 中的 nil 返回：

```go
func (d *DeepAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
    permissionConfig, _ := configBase["permissions"].(map[string]any)
    if permissionConfig == nil { return nil }
    enabled, _ := permissionConfig["enabled"].(bool)
    if !enabled { return nil }

    toolNames := collectOptionalToolTags(permissionConfig)
    modelName := extractModelName(configBase)

    host := &security.ToolPermissionHost{
        GetPermissionsSnapshot:     d.getPermissionsSnapshot,
        PersistAllowRule:           d.persistAllowRule,
        ResolveWorkspaceDir:        d.resolveWorkspaceDir,
        PermissionYAMLPath:         d.getPermissionYAMLPath(),
        ToolPermissionChecksActive: func() bool { return true },
        RequestPermissionConfirmation: d.requestPermissionConfirmation,
        PermissionSceneHook:        d.permissionSceneHook,
    }

    workspaceRoot := workspace.WorkspaceDir()

    return security.BuildPermissionInterruptRail(
        permissionConfig, nil, host, workspaceRoot, d.model, modelName,
        secrail.NewPermissionInterruptRail,
    )
}
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -run TestDeepAdapter_buildPermissionRail -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/server/adapter/
git commit -m "feat(adapter): DeepAdapter buildPermissionRail 回填 + ToolPermissionHost 7 个回调注入"
```

---

## Task 3: buildPermissionRail 回填（CodeAdapter）

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 实现 CodeAdapter.buildPermissionRail**

CodeAdapter 的逻辑与 DeepAdapter 基本相同，但额外接收 `llm` 和 `modelName` 参数：

```go
func (c *CodeAdapter) buildPermissionRail(configBase map[string]any, llm any, modelName string) sainterfaces.AgentRail {
    permissionConfig, _ := configBase["permissions"].(map[string]any)
    if permissionConfig == nil { return nil }
    enabled, _ := permissionConfig["enabled"].(bool)
    if !enabled { return nil }

    toolNames := collectOptionalToolTags(permissionConfig)

    host := &security.ToolPermissionHost{
        GetPermissionsSnapshot:     c.deep.getPermissionsSnapshot,
        PersistAllowRule:           c.deep.persistAllowRule,
        ResolveWorkspaceDir:        c.deep.resolveWorkspaceDir,
        PermissionYAMLPath:         c.deep.getPermissionYAMLPath(),
        ToolPermissionChecksActive: func() bool { return true },
        RequestPermissionConfirmation: c.deep.requestPermissionConfirmation,
        PermissionSceneHook:        c.deep.permissionSceneHook,
    }

    workspaceRoot := workspace.WorkspaceDir()

    return security.BuildPermissionInterruptRail(
        permissionConfig, nil, host, workspaceRoot, llm, modelName,
        secrail.NewPermissionInterruptRail,
    )
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "feat(adapter): CodeAdapter buildPermissionRail 回填"
```

---

## Task 4: _update_permission_rail 热重载

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`

- [ ] **Step 1: 编写 updatePermissionRail 测试**

```go
func TestDeepAdapter_updatePermissionRail_已有Rail原地更新(t *testing.T) {
    // 构建 DeepAdapter + mock permissionRail
    // 调用 updatePermissionRail → 应调用 rail.UpdateConfig
}

func TestDeepAdapter_updatePermissionRail_首次创建(t *testing.T) {
    // permissionRail == nil, enabled=true → 应调用 buildPermissionRail
}

func TestDeepAdapter_updatePermissionRail_未启用不创建(t *testing.T) {
    // permissionRail == nil, enabled=false → 应保持 nil
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -run TestDeepAdapter_updatePermissionRail -v`
Expected: 失败

- [ ] **Step 3: 实现 updatePermissionRail**

在 `deep_adapter.go` 中替换 ⤵️ 标记：

```go
// updatePermissionRail 原地更新已有 PermissionRail 配置，或在首次启用时新建。
// 对齐 Python: _update_permission_rail() (interface_deep.py L2286-2302)
func (d *DeepAdapter) updatePermissionRail(configBase map[string]any) {
    permissionConfig, _ := configBase["permissions"].(map[string]any)

    if d.permissionRail != nil {
        if rail, ok := d.permissionRail.(*secrail.PermissionInterruptRail); ok {
            rail.UpdateConfig(permissionConfig)
            logger.Info(logComponent).Msg("permissionRail 配置热更新完成")
        }
        return
    }

    if permissionConfig != nil {
        if enabled, _ := permissionConfig["enabled"].(bool); enabled {
            rail := d.buildPermissionRail(configBase)
            if rail != nil {
                d.permissionRail = rail
                logger.Info(logComponent).Msg("permissionRail 首次创建完成（热重载触发）")
            }
        }
    }
}
```

在 `ReloadAgentConfig` 步骤 10 处调用 `d.updatePermissionRail(configBase)` 替换 ⤵️ 标记。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -run TestDeepAdapter_updatePermissionRail -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go
git commit -m "feat(adapter): updatePermissionRail 热重载回填"
```

---

## Task 5: 请求级 Context 注入

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`
- Modify: `internal/swarm/schema/permission.go`（补充 NewPermissionContextFromRequest）
- Test: `internal/swarm/schema/permission_test.go`

- [ ] **Step 1: 编写 NewPermissionContextFromRequest 测试**

```go
func TestNewPermissionContextFromRequest_数字分身模式(t *testing.T) {
    request := &schema.AgentRequest{
        ChannelID: "feishu_ch1",
        Metadata: map[string]any{
            "avatar_mode":           true,
            "group_digital_avatar":  true,
            "principal_user_id":     "user_123",
            "triggering_user_id":    "sender_456",
            "avatar_principal_name": "助手A",
        },
    }
    pc := NewPermissionContextFromRequest(request)
    if pc == nil { t.Fatal("应返回非 nil") }
    if pc.Scene() != "group_digital_avatar" { t.Fatalf("scene=%s", pc.Scene()) }
    if pc.PrincipalUserID != "user_123" { t.Fatalf("principal=%s", pc.PrincipalUserID) }
}

func TestNewPermissionContextFromRequest_非数字分身_禁用记忆(t *testing.T) {
    request := &schema.AgentRequest{
        ChannelID: "web",
        Metadata: map[string]any{
            "enable_memory": false,
        },
    }
    pc := NewPermissionContextFromRequest(request)
    if pc == nil { t.Fatal("禁用记忆应返回非 nil") }
    if pc.EnableMemory { t.Fatal("EnableMemory 应为 false") }
}

func TestNewPermissionContextFromRequest_无metadata返回nil(t *testing.T) {
    request := &schema.AgentRequest{ChannelID: "web"}
    pc := NewPermissionContextFromRequest(request)
    if pc != nil { t.Fatal("无 avatar_mode 且非禁用记忆时应返回 nil") }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/... -run TestNewPermissionContextFromRequest -v`
Expected: 编译失败

- [ ] **Step 3: 实现 NewPermissionContextFromRequest**

```go
// NewPermissionContextFromRequest 从 AgentRequest 的 metadata 构造 PermissionContext。
// 对齐 Python: setup_permission_context(request) (owner_scopes.py L61-88)
// 返回 nil 表示无需设置权限上下文（非数字分身且非禁用记忆场景）。
func NewPermissionContextFromRequest(request *schema.AgentRequest) *PermissionContext {
    meta := request.Metadata
    if meta == nil { meta = map[string]any{} }

    avatarMode := false
    if v, ok := meta["avatar_mode"].(bool); ok { avatarMode = v }

    if !avatarMode {
        // 非 avatar_mode：仅当 enable_memory=false 时才设置上下文（用于禁用记忆）
        if enableMem, ok := meta["enable_memory"].(bool); ok && !enableMem {
            return NewPermissionContext(
                WithPermissionChannelID(request.ChannelID),
                WithPermissionEnableMemory(false),
                WithPermissionAvatarMode(false),
            )
        }
        return nil
    }

    return NewPermissionContextFromDict(meta)
}
```

注意：`NewPermissionContextFromDict` 需要额外处理 `request.ChannelID`，因为 Python 端从 `request.channel_id` 而非 metadata 取 channelID。可能需要扩展 `NewPermissionContextFromDict` 或在 `NewPermissionContextFromRequest` 中覆盖。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/... -run TestNewPermissionContextFromRequest -v`
Expected: PASS

- [ ] **Step 5: 在 DeepAdapter.processMessageImpl 中注入 context**

在 `processMessageImpl` 和 `processMessageStreamImpl` 入口处添加：

```go
// 注入权限上下文 context
ctx = sschema.WithToolPermissionChannelID(ctx, request.ChannelID)
permCtx := sschema.NewPermissionContextFromRequest(request)
if permCtx != nil {
    ctx = sschema.WithPermissionContextValue(ctx, permCtx)
}
```

- [ ] **Step 6: 在 CodeAdapter 中同样注入**

CodeAdapter 的 `processMessageImpl` / `processMessageStreamImpl` 中同样添加 context 注入。

- [ ] **Step 7: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/...`
Expected: 编译通过

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/schema/permission.go internal/swarm/schema/permission_test.go internal/swarm/server/adapter/
git commit -m "feat(permissions): 请求级 PermissionContext context 注入"
```

---

## Task 6: permissions_config_rpc 真实实现

**Files:**
- Modify: `internal/swarm/server/handle_permissions.go`（替换 stub）
- Modify: `internal/swarm/server/handle_permissions_test.go`（补充真实测试）
- 新增: `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go`
- 新增: `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc_test.go`

- [ ] **Step 1: 编写 permissions_config_rpc 分发函数的测试**

在 `permissions_config_rpc_test.go` 中：

```go
func TestDispatchPermissionsConfigRequest_未知方法(t *testing.T) {
    req := &schema.AgentRequest{ReqMethod: schema.ReqMethod("unknown")}
    resp := DispatchPermissionsConfigRequest(req)
    if resp.OK { t.Fatal("未知方法应返回失败") }
}

func TestDispatchPermissionsConfigRequest_ToolsGet(t *testing.T) {
    // mock config 返回 tools 配置
    req := &schema.AgentRequest{ReqMethod: schema.ReqMethodPermissionsToolsGet}
    resp := DispatchPermissionsConfigRequest(req)
    if !resp.OK { t.Fatal("ToolsGet 应返回成功") }
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 DispatchPermissionsConfigRequest**

```go
// DispatchPermissionsConfigRequest 执行一条权限配置 RPC。
// 对齐 Python: dispatch_permissions_config_request() (permissions_config_rpc.py L57-156)
func DispatchPermissionsConfigRequest(request *schema.AgentRequest) *schema.AgentResponse {
    m := request.ReqMethod
    params, _ := request.Params.(map[string]any)

    switch m {
    case schema.ReqMethodPermissionsToolsGet:
        tools := GetPermissionsTools()
        return okResponse(request, map[string]any{"tools": tools})

    case schema.ReqMethodPermissionsToolsSet:
        tools := params["tools"]
        ReplacePermissionsToolsInConfig(tools)
        return okResponse(request, map[string]any{"ok": true})

    case schema.ReqMethodPermissionsToolsUpdate:
        tool := strVal(params, "tool", "name")
        level := params["level"]
        result := UpdatePermissionsToolInConfig(tool, level)
        return okResponse(request, result)

    case schema.ReqMethodPermissionsToolsDelete:
        tool := strVal(params, "tool", "name")
        ok := DeletePermissionsToolInConfig(tool)
        if !ok { return errResponse(request, "tool not found", "NOT_FOUND") }
        return okResponse(request, map[string]any{"tools": GetPermissionsTools()})

    case schema.ReqMethodPermissionsRulesGet:
        rules := GetPermissionsRules()
        return okResponse(request, map[string]any{"rules": rules})

    case schema.ReqMethodPermissionsRulesCreate:
        rule, _ := params["rule"].(map[string]any)
        stored := CreatePermissionsRuleInConfig(rule)
        return okResponse(request, map[string]any{"rule": stored})

    case schema.ReqMethodPermissionsRulesUpdate:
        id := strVal(params, "id")
        patch, _ := params["patch"].(map[string]any)
        merged := UpdatePermissionsRuleInConfig(id, patch)
        return okResponse(request, map[string]any{"rule": merged})

    case schema.ReqMethodPermissionsRulesDelete:
        id := strVal(params, "id")
        ok := DeletePermissionsRuleInConfig(id)
        if !ok { return errResponse(request, "rule not found", "NOT_FOUND") }
        return okResponse(request, map[string]any{"ok": true})

    case schema.ReqMethodPermissionsApprovalOverridesGet:
        overrides := GetPermissionsApprovalOverrides()
        return okResponse(request, map[string]any{"overrides": overrides})

    case schema.ReqMethodPermissionsApprovalOverridesDelete:
        id := strVal(params, "id")
        ok := DeletePermissionsApprovalOverrideInConfig(id)
        if !ok { return errResponse(request, "approval_override not found", "NOT_FOUND") }
        return okResponse(request, map[string]any{"ok": true})

    default:
        return errResponse(request, "unknown permissions req_method", "BAD_REQUEST")
    }
}
```

- [ ] **Step 4: 实现 10 个 config CRUD 辅助函数**

这些函数操作 `workspace.ConfigFile()` 的 YAML 文件中的 `permissions` 节：

```go
// GetPermissionsTools 读取 permissions.tools 配置
func GetPermissionsTools() map[string]any { ... }

// ReplacePermissionsToolsInConfig 替换整个 permissions.tools 配置
func ReplacePermissionsToolsInConfig(tools any) { ... }

// UpdatePermissionsToolInConfig 更新单个工具权限级别
func UpdatePermissionsToolInConfig(tool string, level any) map[string]any { ... }

// DeletePermissionsToolInConfig 删除单个工具权限配置
func DeletePermissionsToolInConfig(tool string) bool { ... }

// GetPermissionsRules 读取 permissions.rules 配置
func GetPermissionsRules() []map[string]any { ... }

// CreatePermissionsRuleInConfig 创建新的权限规则
func CreatePermissionsRuleInConfig(rule map[string]any) map[string]any { ... }

// UpdatePermissionsRuleInConfig 按 ID 更新权限规则
func UpdatePermissionsRuleInConfig(id string, patch map[string]any) map[string]any { ... }

// DeletePermissionsRuleInConfig 按 ID 删除权限规则
func DeletePermissionsRuleInConfig(id string) bool { ... }

// GetPermissionsApprovalOverrides 读取 permissions.approval_overrides
func GetPermissionsApprovalOverrides() []map[string]any { ... }

// DeletePermissionsApprovalOverrideInConfig 按 ID 删除 approval_override
func DeletePermissionsApprovalOverrideInConfig(id string) bool { ... }
```

每个函数的实现模式：读取 YAML → 修改 permissions 节 → 写回 YAML。可复用 `harnesssecurity.WritePermissionsSectionToAgentConfigYAML` 的写盘模式。

- [ ] **Step 5: 更新 handle_permissions.go 调用真实分发函数**

将 `handlePermissionsConfig` 中的 stub switch 替换为调用 `permrpc.DispatchPermissionsConfigRequest(request)`（转换为 `*schema.AgentRequest` 指针）。

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -run TestHandlePermissions -v && go test ./internal/swarm/agents/harness/common/rails/permissions/... -v`
Expected: PASS

- [ ] **Step 7: 更新 permissions/doc.go**

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/ internal/swarm/server/handle_permissions.go internal/swarm/server/handle_permissions_test.go
git commit -m "feat(permissions): permissions_config_rpc 10 个 RPC 方法真实实现"
```

---

## Task 7: SecurityRail 回填 + 标记清理

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/agentcore/harness/schema/config.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: SecurityRail 回填（DeepAdapter）**

```go
// buildSecurityRail 构建安全护栏。
// ✅ 已回填：SafetyPromptRail（对齐 Python: _build_security_rail() — SecurityRail()）
func (d *DeepAdapter) buildSecurityRail(configBase map[string]any) sainterfaces.AgentRail {
    rail := secrail.NewSafetyPromptRail()
    logger.Info(logComponent).Msg("SafetyPromptRail 创建成功")
    return rail
}
```

- [ ] **Step 2: SecurityRail 回填（CodeAdapter）**

在 `code_adapter.go` 的 `buildCodeAgentRails` 中确认 SecurityRail 已正确回填（或添加同样逻辑）。

- [ ] **Step 3: 清理 ⤵️ 标记**

搜索并清理以下文件中所有 `⤵️ 10.6.3-10: PermissionInterruptRail` 和 `⤵️ 10.6.3-10: SecurityRail` 标记：

- `deep_adapter_rails.go` — buildPermissionRail 注释 + buildSecurityRail 注释
- `deep_adapter.go` — securityRail / permissionRail 字段注释 + updatePermissionRail 注释
- `code_adapter.go` — buildPermissionRail 注释 + buildSecurityRail 注释 + buildCodeAgentRails 注释

- [ ] **Step 4: 清理 agentcore 层 ⤵️ 标记**

- `agentcore/harness/deep_agent.go` — `⤵️ 9.11 回填：build_permission_interrupt_rail` 标记
- `agentcore/harness/schema/config.go` — `PermissionHost any` 占位注释

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md**

将 10.6.3-10 行中 Permissions 和 SecurityRail 相关状态更新为 ✅。

- [ ] **Step 6: 编译确认**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add -A
git commit -m "feat(adapter): SecurityRail 回填 + ⤵️ 标记清理 + IMPLEMENTATION_PLAN 更新"
```

---

## Task 8: 全量验证

**Files:** 无新文件

- [ ] **Step 1: 运行 permissions 包全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -v`
Expected: 全部 PASS

- [ ] **Step 2: 运行 adapter 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -v`
Expected: 全部 PASS

- [ ] **Step 3: 运行 server 包权限测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -run TestHandlePermissions -v`
Expected: PASS

- [ ] **Step 4: 运行 schema 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/schema/... -v`
Expected: PASS

- [ ] **Step 5: 运行 agentcore security 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/security/... -v`
Expected: PASS

- [ ] **Step 6: 运行 agentcore rails/security 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/security/... -v`
Expected: PASS

- [ ] **Step 7: 全项目编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译通过

- [ ] **Step 8: 覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/agents/harness/common/rails/permissions/... ./internal/swarm/server/adapter/...`
Expected: ≥ 85%

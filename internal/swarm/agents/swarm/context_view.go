package swarm

// ──────────────────────────── 导出函数 ────────────────────────────

// 以下访问器方法实现 providers.SwarmContextView（偏差 D-15.16f-view：
// Python providers 直接属性访问 context.xxx；Go 侧 providers↔swarm 循环
// 依赖以窄接口拆解，访问器为接口适配物，Python 无对应方法）。
// Python: SwarmBuildContext 字段（context.py）

// ViewRole 返回成员角色。
func (c *SwarmBuildContext) ViewRole() string { return c.Role }

// ViewTeamID 返回团队名。
func (c *SwarmBuildContext) ViewTeamID() string { return c.TeamID }

// ViewSessionID 返回会话 id。
func (c *SwarmBuildContext) ViewSessionID() string { return c.SessionID }

// ViewChannelID 返回渠道 id。
func (c *SwarmBuildContext) ViewChannelID() string { return c.ChannelID }

// ViewChannel 返回渠道键。
func (c *SwarmBuildContext) ViewChannel() string { return c.Channel }

// ViewMode 返回请求模式。
func (c *SwarmBuildContext) ViewMode() string { return c.Mode }

// ViewUserID 返回请求属主。
func (c *SwarmBuildContext) ViewUserID() string { return c.UserID }

// ViewMemberCardID 返回成员卡片 id。
func (c *SwarmBuildContext) ViewMemberCardID() string { return c.MemberCardID }

// ViewRequestMetadata 返回请求元数据。
func (c *SwarmBuildContext) ViewRequestMetadata() map[string]any { return c.RequestMetadata }

// ViewHeartbeatJobService 返回心跳作业服务句柄（D-15.16-c2③ any 别名）。
func (c *SwarmBuildContext) ViewHeartbeatJobService() any { return c.HeartbeatJobService }

// ViewConfig 返回 config.yaml 映射。
func (c *SwarmBuildContext) ViewConfig() map[string]any { return c.Config }

// ViewProjectDir 返回用户项目目录。
func (c *SwarmBuildContext) ViewProjectDir() string { return c.ProjectDir }

// ViewTrustedDirs 返回受信目录列表。
func (c *SwarmBuildContext) ViewTrustedDirs() []string { return c.TrustedDirs }

// ViewGlobalSkillsDir 返回全局 Skill 库目录。
func (c *SwarmBuildContext) ViewGlobalSkillsDir() string { return c.GlobalSkillsDir }

// ViewTeamWsRoot 返回团队共享工作区根。
func (c *SwarmBuildContext) ViewTeamWsRoot() string { return c.TeamWsRoot }

// ViewTeamOutputsDir 返回项目less 团队交付物目录。
func (c *SwarmBuildContext) ViewTeamOutputsDir() string { return c.TeamOutputsDir }

// ViewTaskWorkspaceRoot 返回共享交付物根。
func (c *SwarmBuildContext) ViewTaskWorkspaceRoot() string { return c.TaskWorkspaceRoot }

// ViewLanguage 返回成员语言码。
func (c *SwarmBuildContext) ViewLanguage() string { return c.Language }

// ViewMemberWorkspaceRoot 返回成员工作区根（workspace.root_path）。
// Python: _workspace_root(ctx)（providers/member_rails.py:214-218）
func (c *SwarmBuildContext) ViewMemberWorkspaceRoot() string {
	if c.Workspace == nil {
		return ""
	}
	return c.Workspace.RootPath
}

// ViewResolveMemberWorkDir 解析成员工作目录。
// Python: context.resolve_member_work_dir()
func (c *SwarmBuildContext) ViewResolveMemberWorkDir() (string, error) {
	return c.ResolveMemberWorkDir()
}

// ViewMemberName 返回成员名（基类字段）。
func (c *SwarmBuildContext) ViewMemberName() string { return c.MemberName }

// ViewTeamSkillVisibilityPath 返回团队技能可见性文件路径。
func (c *SwarmBuildContext) ViewTeamSkillVisibilityPath() string { return c.TeamSkillVisibilityPath }

// ViewResolveMemberSkillVisibilityPath 解析成员技能可见性文件路径。
// Python: context.resolve_member_skill_visibility_path()
func (c *SwarmBuildContext) ViewResolveMemberSkillVisibilityPath() string {
	return c.ResolveMemberSkillVisibilityPath()
}

// ViewRequestID 返回发起请求 id。
func (c *SwarmBuildContext) ViewRequestID() string { return c.RequestID }

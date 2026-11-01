# Remote Skills Fixture

`RemoteSkillUtil` 集成测试的 GitHub fixture（Trees API + Contents API）。

目录结构刻意覆盖三条分支：

- `skills/example-skill/SKILL.md` + 同级 `references/example-reference.md`：同目录兄弟文件应被一并收集
- `skills/second-skill/SKILL.md`：多技能聚合
- `README.md`：非 SKILL.md，必须被排除

注意：本目录**不放**根级 `SKILL.md`。测试用 `GitHubTree.Directory` 定位子树，
路径带前缀后 `len(parts)==1` 的根级跳过分支不会被触发，该分支由包内单测覆盖。

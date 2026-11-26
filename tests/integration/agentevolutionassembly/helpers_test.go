//go:build integration

package agentevolutionassembly

import "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"

// evolutionEnsureMemberSkillCopy copy-on-write 助手别名（audit 符号引用面）。
func evolutionEnsureMemberSkillCopy(memberDir, globalDir, skillName string) (string, error) {
	return evolution.EnsureMemberSkillCopy(memberDir, globalDir, skillName)
}

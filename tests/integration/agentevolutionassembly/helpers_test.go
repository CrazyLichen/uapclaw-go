//go:build integration

package agentevolutionassembly

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
)

// HelpersSuite 纯助手文件套件壳（audit 硬要求：每个 _test.go 自带 suite.Run）。
type HelpersSuite struct {
	suite.Suite
}

func TestHelpersSuite(t *testing.T) {
	suite.Run(t, new(HelpersSuite))
}

// evolutionEnsureMemberSkillCopy copy-on-write 助手别名（audit 符号引用面）。
func evolutionEnsureMemberSkillCopy(memberDir, globalDir, skillName string) (string, error) {
	return evolution.EnsureMemberSkillCopy(memberDir, globalDir, skillName)
}

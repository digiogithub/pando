package evaluator

import "github.com/digiogithub/pando/internal/llm/provider"

// SetJudgeProvider installs a fake judge provider for tests.
func SetJudgeProvider(s *EvaluatorService, p provider.Provider) {
	s.judge = &Judge{p: p}
}

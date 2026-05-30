package db

import "context"

// deactivateUnderperformingSkillsSQL removes skills that have a low success rate
// despite having been used many times, indicating they consistently fail to help.
const deactivateUnderperformingSkillsSQL = `
UPDATE skill_library
SET    is_active  = 0,
       updated_at = strftime('%s', 'now')
WHERE  is_active    = 1
  AND  success_rate < 0.3
  AND  usage_count  > 10
`

// DeactivateUnderperformingSkills deactivates skills whose success_rate is below 0.3
// AND whose usage_count exceeds 10, pruning low-quality entries from the skill library.
func (q *Queries) DeactivateUnderperformingSkills(ctx context.Context) error {
	_, err := q.db.ExecContext(ctx, deactivateUnderperformingSkillsSQL)
	return err
}

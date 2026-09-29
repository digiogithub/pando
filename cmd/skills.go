package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/evaluator"
	"github.com/digiogithub/pando/internal/message"
)

var skillsStatusFilter string

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Review the skills learned by the self-improvement evaluator",
	Long: `The evaluator's judge proposes reusable rules from sessions. Each proposal is
a file under .pando/skills/learned/<id>.md with status pending. Only approved
skills are injected into prompts, and an approval reaches the next new session
(never one in progress).

Examples:
  pando skills list --status pending
  pando skills approve verify-the-build-before-reporting-done
  pando skills reject some-skill-id`,
}

var skillsListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List learned skills",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		status := strings.ToLower(strings.TrimSpace(skillsStatusFilter))
		if status != "" && !evaluator.ValidSkillStatus(status) {
			return fmt.Errorf("--status must be pending, approved or rejected")
		}
		wd, svc, closeFn, err := openSkillsBackend()
		if err != nil {
			return err
		}
		defer closeFn()

		var skills []evaluator.Skill
		if svc != nil {
			skills, err = svc.ListSkills(skillsCtx(cmd), status, "")
		} else {
			skills, err = skillsFromFiles(wd, status)
		}
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(skills) == 0 {
			fmt.Fprintln(out, "No learned skills.")
			return nil
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tTASK\tCONF\tSUCCESS\tUSES\tTITLE")
		for _, sk := range skills {
			fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\t%.2f (%d)\t%d\t%s\n",
				sk.ID, sk.Status, sk.TaskType, sk.Confidence, sk.SuccessRate, sk.EvalCount, sk.UsageCount, sk.Title)
		}
		return w.Flush()
	},
}

func newSkillsReviewCmd(use, status, short string) *cobra.Command {
	return &cobra.Command{
		Use:          use + " <id>",
		Short:        short,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, svc, closeFn, err := openSkillsBackend()
			if err != nil {
				return err
			}
			defer closeFn()

			var f evaluator.Skill
			if svc != nil {
				sk, err := svc.ReviewSkill(skillsCtx(cmd), args[0], status)
				if err != nil {
					return err
				}
				f = *sk
			} else {
				sf, err := evaluator.SetSkillFileStatus(wd, args[0], status)
				if err != nil {
					return err
				}
				f = evaluator.Skill{ID: sf.ID, Title: sf.Title}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", status, f.ID)
			if status == evaluator.SkillStatusApproved {
				fmt.Fprintln(cmd.OutOrStdout(), "It will be injected into new sessions (not the ones in progress).")
			}
			return nil
		},
	}
}

// openSkillsBackend returns the project directory and, when the database can be
// opened, an evaluator service that also refreshes the mirror and statistics.
// Without a database the commands work on the files alone: the files are the
// source of truth and the mirror is re-synced on the next prompt build.
func openSkillsBackend() (string, *evaluator.EvaluatorService, func(), error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, func() {}, err
	}
	if _, err := config.Load(cwd, false, ""); err != nil {
		return "", nil, func() {}, fmt.Errorf("load config: %w", err)
	}
	wd := config.WorkingDirectory()

	conn, err := db.Connect()
	if err != nil {
		return wd, nil, func() {}, nil
	}
	q := db.New(conn)
	cfg := config.EvaluatorWithDefaults(config.Get().Evaluator)
	cfg.Enabled = true
	cfg.Async = false
	svc, err := evaluator.New(cfg, q, message.NewService(q))
	if err != nil || svc == nil {
		conn.Close()
		return wd, nil, func() {}, nil
	}
	svc.SetWorkDir(wd)
	return wd, svc, func() { conn.Close() }, nil
}

func skillsFromFiles(wd, status string) ([]evaluator.Skill, error) {
	files, err := evaluator.ReadLearnedSkills(wd)
	if err != nil {
		return nil, err
	}
	var out []evaluator.Skill
	for _, f := range files {
		if status != "" && f.Status != status {
			continue
		}
		out = append(out, evaluator.Skill{ID: f.ID, Title: f.Title, Content: f.Content, TaskType: f.TaskType,
			Status: f.Status, Confidence: f.Confidence, Created: f.Created})
	}
	return out, nil
}

func init() {
	skillsListCmd.Flags().StringVar(&skillsStatusFilter, "status", "", "Only list skills with this status (pending, approved, rejected)")
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(newSkillsReviewCmd("approve", evaluator.SkillStatusApproved, "Approve a learned skill (injected into new sessions)"))
	skillsCmd.AddCommand(newSkillsReviewCmd("reject", evaluator.SkillStatusRejected, "Reject a learned skill (never injected, never proposed again)"))
	rootCmd.AddCommand(skillsCmd)
}

func skillsCtx(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/project"
	"github.com/spf13/cobra"
)

// projectCmd is the top-level "pando project" command.
var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage registered projects",
	Long: `Manage Pando projects.

Projects allow you to switch between multiple codebases without restarting Pando.
Each project runs its own child Pando ACP instance with its own configuration.`,
	Example: `
  # List all registered projects
  pando project list

  # Register a new project
  pando project add ~/code/myapp --name myapp

  # Remove a project
  pando project remove ~/code/myapp

  # Initialize Pando config in a directory
  pando project init ~/code/newproject

  # Show last active project
  pando project status`,
}

// projectListCmd lists all registered projects.
var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithProjectService(func(ctx context.Context, svc project.Service) error {
			projects, err := svc.List(ctx)
			if err != nil {
				return fmt.Errorf("list projects: %w", err)
			}

			if len(projects) == 0 {
				fmt.Println("No projects registered.")
				return nil
			}

			home, _ := os.UserHomeDir()

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tPATH\tSTATUS")
			for _, p := range projects {
				displayPath := p.Path
				if home != "" && strings.HasPrefix(displayPath, home) {
					displayPath = "~" + displayPath[len(home):]
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Name, displayPath, p.Status)
			}
			w.Flush()
			return nil
		})
	},
}

// projectAddCmd registers a new project.
var projectAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Register a new project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		rawPath := args[0]

		// Expand ~ in path.
		resolvedPath, err := expandPath(rawPath)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}

		// Validate path exists and is a directory.
		info, err := os.Stat(resolvedPath)
		if err != nil {
			return fmt.Errorf("path does not exist: %s", resolvedPath)
		}
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", resolvedPath)
		}

		return runWithProjectService(func(ctx context.Context, svc project.Service) error {
			// Check if already registered.
			existing, err := svc.GetByPath(ctx, resolvedPath)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("check existing project: %w", err)
			}
			if existing != nil {
				return fmt.Errorf("project already registered: %s (id: %s)", resolvedPath, existing.ID)
			}

			if name == "" {
				name = filepath.Base(resolvedPath)
			}

			p, err := svc.Create(ctx, name, resolvedPath)
			if err != nil {
				return fmt.Errorf("register project: %w", err)
			}
			fmt.Printf("Project registered: %s (%s)\n", p.Name, p.Path)
			return nil
		})
	},
}

// projectRemoveCmd unregisters a project by ID or path.
var projectRemoveCmd = &cobra.Command{
	Use:   "remove <id|path>",
	Short: "Remove a registered project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		arg := args[0]

		return runWithProjectService(func(ctx context.Context, svc project.Service) error {
			var proj *project.Project
			var err error

			// If the argument looks like a path, resolve and use GetByPath.
			if strings.Contains(arg, "/") || strings.HasPrefix(arg, ".") || strings.HasPrefix(arg, "~") {
				resolved, resolveErr := expandPath(arg)
				if resolveErr != nil {
					return fmt.Errorf("resolve path: %w", resolveErr)
				}
				proj, err = svc.GetByPath(ctx, resolved)
				if err != nil {
					return fmt.Errorf("project not found at path %s: %w", resolved, err)
				}
			} else {
				// Treat argument as project ID.
				proj, err = svc.Get(ctx, arg)
				if err != nil {
					return fmt.Errorf("project not found with id %s: %w", arg, err)
				}
			}

			if err := svc.Delete(ctx, proj.ID); err != nil {
				return fmt.Errorf("remove project: %w", err)
			}
			fmt.Printf("Project removed: %s\n", proj.Name)
			return nil
		})
	},
}

// projectInitCmd initializes Pando config at a path (no DB registration).
var projectInitCmd = &cobra.Command{
	Use:   "init <path>",
	Short: "Initialize Pando configuration at a directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rawPath := args[0]

		resolvedPath, err := expandPath(rawPath)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}

		if _, err := os.Stat(resolvedPath); err != nil {
			return fmt.Errorf("path does not exist: %s", resolvedPath)
		}

		if config.HasConfigFileAt(resolvedPath) {
			fmt.Printf("Project already has a configuration at %s (no changes made)\n", resolvedPath)
			return nil
		}

		if err := config.InitializeProjectAt(resolvedPath); err != nil {
			return fmt.Errorf("initialize project: %w", err)
		}
		fmt.Printf("Project initialized at %s\n", resolvedPath)
		return nil
	},
}

// projectStatusCmd shows the last active project.
var projectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the last active project",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithProjectService(func(ctx context.Context, svc project.Service) error {
			projects, err := svc.List(ctx)
			if err != nil {
				return fmt.Errorf("list projects: %w", err)
			}

			if len(projects) == 0 {
				fmt.Println("No projects registered.")
				return nil
			}

			// Find the most recently opened project.
			var latest *project.Project
			for i := range projects {
				p := &projects[i]
				if p.LastOpened == nil {
					continue
				}
				if latest == nil || latest.LastOpened == nil || p.LastOpened.After(*latest.LastOpened) {
					latest = p
				}
			}

			if latest == nil {
				// No project has been opened yet — show the first registered.
				latest = &projects[0]
				fmt.Printf("Last active: %s (%s) [%s]\n", latest.Name, latest.Path, latest.Status)
				return nil
			}

			fmt.Printf("Last active: %s (%s) [%s]\n", latest.Name, latest.Path, latest.Status)
			return nil
		})
	},
}

func init() {
	rootCmd.AddCommand(projectCmd)

	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectRemoveCmd)
	projectCmd.AddCommand(projectInitCmd)
	projectCmd.AddCommand(projectStatusCmd)

	projectAddCmd.Flags().String("name", "", "Project name (default: directory basename)")
}

// runWithProjectService loads config and DB, creates a project.Service and
// calls fn with it. The DB connection is closed when fn returns.
func runWithProjectService(fn func(ctx context.Context, svc project.Service) error) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if _, err := config.Load(cwd, false, ""); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	conn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close()
	svc := project.NewService(db.New(conn))
	return fn(context.Background(), svc)
}

// expandPath expands a leading ~ to the user home directory and returns an
// absolute path. It does not evaluate symlinks (unlike resolvePath in manager).
func expandPath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p, err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Abs(p)
}

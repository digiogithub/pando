package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"
)

var cronJobCmd = &cobra.Command{
	Use:   "cronjob",
	Short: "Manage Pando cronjobs",
	Long: `Manage Pando scheduled jobs (cronjobs).

CronJobs allow Pando to automatically run prompts on a schedule using
the Mesnada orchestrator. Jobs are configured in .pando.toml under [CronJobs].`,
	Example: `
  # List all configured jobs
  pando cronjob list

  # Run a job immediately (bypass schedule)
  pando cronjob run daily-review

  # Install a job into the OS scheduler (user crontab on Unix)
  pando cronjob install daily-review

  # Dry-run install (show what would be added without modifying)
  pando cronjob install daily-review --dry-run

  # Remove a job from the OS scheduler
  pando cronjob uninstall daily-review`,
}

var cronJobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured cronjobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCronJobList()
	},
}

var cronJobRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a cronjob immediately (bypass schedule)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCronJobRun(args[0])
	},
}

var cronJobInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install a cronjob into the OS scheduler",
	Long: `Install a Pando cronjob into the operating-system scheduler.

On Unix/macOS this adds an entry to the user crontab (crontab -e equivalent).
On Windows this creates a Scheduled Task via PowerShell.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		return runCronJobInstall(args[0], dryRun)
	},
}

var cronJobUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Remove a cronjob from the OS scheduler",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCronJobUninstall(args[0])
	},
}

func init() {
	rootCmd.AddCommand(cronJobCmd)
	cronJobCmd.AddCommand(cronJobListCmd)
	cronJobCmd.AddCommand(cronJobRunCmd)
	cronJobCmd.AddCommand(cronJobInstallCmd)
	cronJobCmd.AddCommand(cronJobUninstallCmd)

	cronJobInstallCmd.Flags().Bool("dry-run", false, "Show what would be installed without making changes")
}

// runCronJobList loads config and prints all configured jobs with their next scheduled run.
func runCronJobList() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	cfg, err := config.Load(cwd, false, "")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	jobs := cfg.CronJobs.Jobs
	if len(jobs) == 0 {
		fmt.Println("No cronjobs configured.")
		fmt.Println("Add jobs to .pando.toml under [CronJobs] to get started.")
		return nil
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	now := time.Now()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSCHEDULE\tENABLED\tNEXT RUN\tPROMPT")
	for _, job := range jobs {
		enabled := "yes"
		if !job.Enabled {
			enabled = "no"
		}
		nextRun := "-"
		if job.Enabled && cfg.CronJobs.Enabled {
			if sched, err := parser.Parse(job.Schedule); err == nil {
				nextRun = sched.Next(now).Format(time.DateTime)
			}
		}
		prompt := job.Prompt
		if len(prompt) > 40 {
			prompt = prompt[:37] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", job.Name, job.Schedule, enabled, nextRun, prompt)
	}
	w.Flush()

	if !cfg.CronJobs.Enabled {
		fmt.Println("\nNote: CronJobs are globally disabled (CronJobs.Enabled = false).")
	}
	return nil
}

// runCronJobRun triggers an immediate run of the named job via the Mesnada orchestrator.
func runCronJobRun(name string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	cfg, err := config.Load(cwd, false, "")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if !cfg.Mesnada.Enabled {
		return fmt.Errorf("Mesnada is not enabled; cronjobs require Mesnada to be enabled in config")
	}

	// Verify the job exists in config before starting the app.
	found := false
	for _, j := range cfg.CronJobs.Jobs {
		if strings.EqualFold(j.Name, name) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("cronjob %q not found in configuration", name)
	}

	conn, err := db.Connect()
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	a, err := app.New(ctx, conn, app.AppOptions{SkipLSP: true})
	if err != nil {
		return fmt.Errorf("initialize app: %w", err)
	}
	defer a.Shutdown()

	if a.CronService == nil {
		return fmt.Errorf("CronService is not available; verify that Mesnada is properly configured")
	}

	fmt.Printf("Running cronjob %q...\n", name)
	task, err := a.CronService.RunNow(ctx, name)
	if err != nil {
		return fmt.Errorf("run cronjob: %w", err)
	}
	fmt.Printf("Task spawned: %s\n", task.ID)
	return nil
}

// runCronJobInstall adds the named cronjob to the OS scheduler.
func runCronJobInstall(name string, dryRun bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	cfg, err := config.Load(cwd, false, "")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var job *config.CronJob
	for i := range cfg.CronJobs.Jobs {
		if strings.EqualFold(cfg.CronJobs.Jobs[i].Name, name) {
			job = &cfg.CronJobs.Jobs[i]
			break
		}
	}
	if job == nil {
		return fmt.Errorf("cronjob %q not found in configuration", name)
	}

	pandoBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	if runtime.GOOS == "windows" {
		return installWindowsTask(job, pandoBin, cwd, dryRun)
	}
	return installUnixCrontab(job, pandoBin, cwd, dryRun)
}

// runCronJobUninstall removes the named cronjob from the OS scheduler.
func runCronJobUninstall(name string) error {
	if runtime.GOOS == "windows" {
		return uninstallWindowsTask(name)
	}
	return uninstallUnixCrontab(name)
}

// --- Unix crontab helpers ---

const unixCrontabMarkerPrefix = "# pando-cronjob:"

func installUnixCrontab(job *config.CronJob, pandoBin, cwd string, dryRun bool) error {
	marker := unixCrontabMarkerPrefix + job.Name
	command := fmt.Sprintf("cd %s && %s cronjob run %s", shellQuote(cwd), shellQuote(pandoBin), job.Name)
	newLine := fmt.Sprintf("%s %s", job.Schedule, command)

	existing, err := readCrontab()
	if err != nil {
		// crontab -l exits non-zero when there is no crontab yet; treat as empty.
		existing = ""
	}

	// Remove any previous installation of this job.
	cleaned := removeCrontabJob(existing, job.Name)

	// Append marker + schedule line.
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(cleaned, "\n"))
	if sb.Len() > 0 {
		sb.WriteByte('\n')
	}
	sb.WriteString(marker)
	sb.WriteByte('\n')
	sb.WriteString(newLine)
	sb.WriteByte('\n')

	if dryRun {
		fmt.Println("Dry run — the following crontab entry would be added:")
		fmt.Printf("  %s\n", marker)
		fmt.Printf("  %s\n", newLine)
		return nil
	}

	if err := writeCrontab(sb.String()); err != nil {
		return fmt.Errorf("write crontab: %w", err)
	}
	fmt.Printf("CronJob %q installed in user crontab.\n", job.Name)
	fmt.Printf("  %s\n", newLine)
	return nil
}

func uninstallUnixCrontab(name string) error {
	existing, err := readCrontab()
	if err != nil {
		return fmt.Errorf("read crontab: %w", err)
	}

	cleaned := removeCrontabJob(existing, name)
	if cleaned == existing {
		fmt.Printf("CronJob %q not found in user crontab.\n", name)
		return nil
	}

	if err := writeCrontab(cleaned); err != nil {
		return fmt.Errorf("write crontab: %w", err)
	}
	fmt.Printf("CronJob %q removed from user crontab.\n", name)
	return nil
}

// removeCrontabJob strips the marker line and its following schedule line for the given job name.
func removeCrontabJob(crontabContent, jobName string) string {
	marker := unixCrontabMarkerPrefix + jobName
	lines := strings.Split(crontabContent, "\n")
	result := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		if strings.TrimSpace(line) == marker {
			skip = true // also skip the next (schedule) line
			continue
		}
		if skip {
			skip = false
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func readCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeCrontab(content string) error {
	f, err := os.CreateTemp("", "pando-crontab-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cmd := exec.Command("crontab", f.Name())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// shellQuote wraps a string in single quotes for sh-compatible shells,
// escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// --- Windows Task Scheduler helpers ---

const windowsTaskPrefix = "pando-cronjob-"

func installWindowsTask(job *config.CronJob, pandoBin, cwd string, dryRun bool) error {
	taskName := windowsTaskPrefix + job.Name
	trigger, warning := cronToWindowsTrigger(job.Schedule)

	psScript := fmt.Sprintf(`$action = New-ScheduledTaskAction -Execute '%s' -Argument 'cronjob run %s' -WorkingDirectory '%s'
$trigger = %s
Register-ScheduledTask -TaskName '%s' -Action $action -Trigger $trigger -Force`,
		strings.ReplaceAll(pandoBin, "'", "''"),
		job.Name,
		strings.ReplaceAll(cwd, "'", "''"),
		trigger,
		taskName,
	)

	if warning != "" {
		fmt.Printf("Warning: %s\n", warning)
	}

	if dryRun {
		fmt.Println("Dry run — the following PowerShell script would be executed:")
		scanner := bufio.NewScanner(strings.NewReader(psScript))
		for scanner.Scan() {
			fmt.Printf("  %s\n", scanner.Text())
		}
		return nil
	}

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("register scheduled task: %w", err)
	}
	fmt.Printf("CronJob %q installed as Windows Scheduled Task %q.\n", job.Name, taskName)
	return nil
}

func uninstallWindowsTask(name string) error {
	taskName := windowsTaskPrefix + name
	psScript := fmt.Sprintf("Unregister-ScheduledTask -TaskName '%s' -Confirm:$false", taskName)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", psScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unregister scheduled task %q: %w", taskName, err)
	}
	fmt.Printf("CronJob %q removed from Windows Task Scheduler.\n", name)
	return nil
}

// cronToWindowsTrigger converts a 5-field Unix cron expression to a best-effort
// PowerShell New-ScheduledTaskTrigger call. Returns the trigger expression and
// an optional warning when the conversion is approximate.
func cronToWindowsTrigger(schedule string) (trigger, warning string) {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return `New-ScheduledTaskTrigger -Daily -At "00:00"`,
			fmt.Sprintf("could not parse schedule %q; defaulting to daily at midnight", schedule)
	}

	minute, hour, dom, _, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Build a time string from minute/hour if they are simple integers.
	atTime := "00:00"
	if hour != "*" && minute != "*" && !strings.ContainsAny(hour+minute, "/-,") {
		atTime = fmt.Sprintf("%s:%s", zeroPad(hour), zeroPad(minute))
	}

	// Weekly trigger: specific day(s) of week, any dom.
	if dow != "*" && dom == "*" && !strings.ContainsAny(dow, "-,/") {
		dayName := cronDOWToWindowsDay(dow)
		if dayName != "" {
			return fmt.Sprintf(`New-ScheduledTaskTrigger -Weekly -DaysOfWeek %s -At "%s"`, dayName, atTime), ""
		}
	}

	// Daily trigger: every day at a specific time.
	if dom == "*" && dow == "*" {
		return fmt.Sprintf(`New-ScheduledTaskTrigger -Daily -At "%s"`, atTime), ""
	}

	// Complex expression: fall back to daily with a warning.
	return fmt.Sprintf(`New-ScheduledTaskTrigger -Daily -At "%s"`, atTime),
		fmt.Sprintf("complex cron schedule %q converted to daily trigger at %s; review the scheduled task", schedule, atTime)
}

var cronDOWNames = map[string]string{
	"0": "Sunday", "7": "Sunday",
	"1": "Monday",
	"2": "Tuesday",
	"3": "Wednesday",
	"4": "Thursday",
	"5": "Friday",
	"6": "Saturday",
}

func cronDOWToWindowsDay(dow string) string {
	if name, ok := cronDOWNames[dow]; ok {
		return name
	}
	return ""
}

func zeroPad(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

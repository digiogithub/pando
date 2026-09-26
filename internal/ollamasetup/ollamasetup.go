// Package ollamasetup backs the Remembrances step of the first-run setup
// assistant: it detects a local Ollama installation, installs or starts it on
// request, and pulls embedding models through the Ollama HTTP API while
// reporting progress.
//
// Long operations (install, pull) run as in-memory jobs polled by the client,
// so they survive the HTTP request that started them and work the same in a
// browser tab and in the desktop webview.
package ollamasetup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/procgroup"
)

// Download pages and command lines shown by the assistant.
const (
	DownloadURL       = "https://ollama.com/download"
	LinuxInstallCmd   = "curl -fsSL https://ollama.com/install.sh | sh"
	BrewInstallCmd    = "brew install ollama"
	WingetInstallCmd  = "winget install --id Ollama.Ollama -e --accept-source-agreements --accept-package-agreements"
	DockerInstallCmd  = "docker run -d -v ollama:/root/.ollama -p 11434:11434 --name ollama ollama/ollama"
	jobRetention      = 30 * time.Minute
	maxJobOutputLines = 200
)

// modelNameRe restricts pullable model references to the characters Ollama
// model names and hf.co references use.
var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)

// InstallOption is one way of installing Ollama on the current OS.
type InstallOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
	// Recommended marks the option the assistant highlights.
	Recommended bool `json:"recommended"`
	// Runnable is true when Pando can execute Command itself (the tool is on
	// PATH and needs no interactive password prompt).
	Runnable bool `json:"runnable"`
	// Reason explains why Runnable is false.
	Reason string `json:"reason,omitempty"`
}

// Status is the Ollama state reported to the assistant.
type Status struct {
	OS             string          `json:"os"`
	BaseURL        string          `json:"baseUrl"`
	Installed      bool            `json:"installed"`
	BinaryPath     string          `json:"binaryPath,omitempty"`
	Running        bool            `json:"running"`
	Version        string          `json:"version,omitempty"`
	Models         []string        `json:"models"`
	InstallOptions []InstallOption `json:"installOptions"`
	// CanStart is true when Ollama is installed but not answering and Pando
	// knows how to launch it.
	CanStart bool `json:"canStart"`
}

// Job is a long-running install or pull operation.
type Job struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	State     string    `json:"state"` // running | done | error
	Status    string    `json:"status,omitempty"`
	Completed int64     `json:"completed"`
	Total     int64     `json:"total"`
	Output    []string  `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
}

// Manager tracks jobs. The zero value is not usable; call NewManager.
type Manager struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	seq    int
	client *http.Client
}

// NewManager returns an empty job manager.
func NewManager() *Manager {
	return &Manager{
		jobs:   make(map[string]*Job),
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Detect reports whether Ollama is installed, answering at baseURL, and which
// models it has.
func (m *Manager) Detect(ctx context.Context, baseURL string) Status {
	st := Status{OS: runtime.GOOS, BaseURL: baseURL, Models: []string{}}
	st.BinaryPath = findOllamaBinary()
	st.Installed = st.BinaryPath != ""

	if version, err := m.fetchVersion(ctx, baseURL); err == nil {
		st.Running = true
		st.Installed = true
		st.Version = version
		if names, err := m.ListModels(ctx, baseURL); err == nil {
			st.Models = names
		}
	}
	st.CanStart = !st.Running && st.BinaryPath != ""
	st.InstallOptions = installOptions()
	return st
}

func (m *Manager) fetchVersion(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/version", nil)
	if err != nil {
		return "", err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama answered %d", resp.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Version, nil
}

// ListModels returns the names of the models present in Ollama.
func (m *Manager) ListModels(ctx context.Context, baseURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama answered %d", resp.StatusCode)
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(body.Models))
	for _, model := range body.Models {
		names = append(names, model.Name)
	}
	return names, nil
}

// HasModel reports whether name is among models, accepting the implicit
// ":latest" tag Ollama adds to untagged names.
func HasModel(models []string, name string) bool {
	for _, have := range models {
		if strings.EqualFold(have, name) || strings.EqualFold(have, name+":latest") {
			return true
		}
	}
	return false
}

// findOllamaBinary looks for the ollama CLI on PATH and in the default
// install locations of the desktop apps.
func findOllamaBinary() string {
	if path, err := exec.LookPath("ollama"); err == nil {
		return path
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Ollama.app/Contents/Resources/ollama",
			"/usr/local/bin/ollama",
			"/opt/homebrew/bin/ollama",
		}
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			candidates = append(candidates, filepath.Join(local, "Programs", "Ollama", "ollama.exe"))
		}
	default:
		candidates = []string{"/usr/local/bin/ollama", "/usr/bin/ollama"}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// installOptions lists the install methods for the current OS. Downloading
// the official app comes first (recommended); Docker comes last.
func installOptions() []InstallOption {
	download := InstallOption{ID: "download", Label: "Download the Ollama app", URL: DownloadURL, Recommended: true}
	docker := InstallOption{ID: "docker", Label: "Docker container", Command: DockerInstallCmd, Reason: "run it yourself in a terminal"}

	switch runtime.GOOS {
	case "darwin":
		download.URL = DownloadURL + "/mac"
		brew := InstallOption{ID: "brew", Label: "Homebrew", Command: BrewInstallCmd}
		if _, err := exec.LookPath("brew"); err == nil {
			brew.Runnable = true
		} else {
			brew.Reason = "Homebrew is not installed"
		}
		return []InstallOption{download, brew, docker}
	case "windows":
		download.URL = DownloadURL + "/windows"
		winget := InstallOption{ID: "winget", Label: "winget", Command: WingetInstallCmd}
		if _, err := exec.LookPath("winget"); err == nil {
			winget.Runnable = true
		} else {
			winget.Reason = "winget is not available"
		}
		return []InstallOption{download, winget, docker}
	default:
		download.URL = DownloadURL + "/linux"
		// On Linux the official script is the download: highlight it.
		download.Recommended = false
		script := InstallOption{ID: "script", Label: "Official install script", Command: LinuxInstallCmd, Recommended: true}
		switch {
		case !hasCommand("curl") || !hasCommand("sh"):
			script.Reason = "curl is not installed"
		case os.Geteuid() == 0 || passwordlessSudo():
			script.Runnable = true
		default:
			script.Reason = "the script needs sudo with a password; run it in a terminal"
		}
		return []InstallOption{script, download, docker}
	}
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func passwordlessSudo() bool {
	if !hasCommand("sudo") {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil
}

// Start launches the Ollama server in the background, detached from Pando's
// process group so it keeps running after Pando exits.
func (m *Manager) Start() error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/Ollama.app"); err == nil {
			cmd = exec.Command("open", "-a", "Ollama")
		}
	}
	if cmd == nil {
		binary := findOllamaBinary()
		if binary == "" {
			return errors.New("ollama is not installed")
		}
		cmd = exec.Command(binary, "serve")
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	procgroup.Ensure(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ollama: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// WaitRunning polls baseURL until Ollama answers or ctx expires.
func (m *Manager) WaitRunning(ctx context.Context, baseURL string) bool {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := m.fetchVersion(ctx, baseURL); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// Get returns a snapshot of a job.
func (m *Manager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	snapshot := *job
	snapshot.Output = append([]string(nil), job.Output...)
	return snapshot, true
}

func (m *Manager) newJob(kind, target string) *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	m.seq++
	job := &Job{
		ID:        fmt.Sprintf("%s-%d-%d", kind, time.Now().Unix(), m.seq),
		Kind:      kind,
		Target:    target,
		State:     "running",
		StartedAt: time.Now(),
	}
	m.jobs[job.ID] = job
	return job
}

// runningJob returns the running job of kind for target, if any, so a double
// click does not start the same download twice.
func (m *Manager) runningJob(kind, target string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if job.Kind == kind && job.Target == target && job.State == "running" {
			return *job, true
		}
	}
	return Job{}, false
}

func (m *Manager) pruneLocked() {
	cutoff := time.Now().Add(-jobRetention)
	for id, job := range m.jobs {
		if job.State != "running" && job.EndedAt.Before(cutoff) {
			delete(m.jobs, id)
		}
	}
}

func (m *Manager) update(job *Job, fn func(*Job)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(job)
}

func (m *Manager) finish(job *Job, err error) {
	m.update(job, func(j *Job) {
		j.EndedAt = time.Now()
		if err != nil {
			j.State = "error"
			j.Error = err.Error()
			return
		}
		j.State = "done"
	})
}

func appendOutput(j *Job, line string) {
	j.Output = append(j.Output, line)
	if len(j.Output) > maxJobOutputLines {
		j.Output = j.Output[len(j.Output)-maxJobOutputLines:]
	}
}

// Pull starts downloading model into Ollama through POST /api/pull and
// returns the job tracking it.
func (m *Manager) Pull(baseURL, model string) (Job, error) {
	model = strings.TrimSpace(model)
	if !modelNameRe.MatchString(model) {
		return Job{}, fmt.Errorf("invalid model name %q", model)
	}
	if job, ok := m.runningJob("pull", model); ok {
		return job, nil
	}
	job := m.newJob("pull", model)
	go m.runPull(job, baseURL, model)
	snapshot, _ := m.Get(job.ID)
	return snapshot, nil
}

func (m *Manager) runPull(job *Job, baseURL, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	payload, _ := json.Marshal(map[string]any{"model": model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(payload))
	if err != nil {
		m.finish(job, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// No client timeout: a model download can take many minutes. ctx bounds it.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		m.finish(job, fmt.Errorf("contact ollama: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		m.finish(job, fmt.Errorf("ollama answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Error != "" {
			m.finish(job, errors.New(event.Error))
			return
		}
		m.update(job, func(j *Job) {
			j.Status = event.Status
			if event.Total > 0 {
				j.Total = event.Total
				j.Completed = event.Completed
			}
			if n := len(j.Output); n == 0 || j.Output[n-1] != event.Status {
				appendOutput(j, event.Status)
			}
		})
		if event.Status == "success" {
			m.finish(job, nil)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		m.finish(job, err)
		return
	}
	m.finish(job, errors.New("ollama closed the download stream before it finished"))
}

// Install runs the install option id when it is runnable on this machine.
func (m *Manager) Install(id string) (Job, error) {
	var chosen *InstallOption
	for _, option := range installOptions() {
		if option.ID == id {
			option := option
			chosen = &option
			break
		}
	}
	if chosen == nil {
		return Job{}, fmt.Errorf("unknown install option %q", id)
	}
	if !chosen.Runnable {
		return Job{}, fmt.Errorf("%s cannot be run by Pando: %s", chosen.Label, chosen.Reason)
	}
	if job, ok := m.runningJob("install", id); ok {
		return job, nil
	}

	var cmd *exec.Cmd
	switch id {
	case "script":
		// Fixed command line, no user input: the official installer.
		cmd = exec.Command("sh", "-c", LinuxInstallCmd)
	case "brew":
		cmd = exec.Command("brew", "install", "ollama")
	case "winget":
		cmd = exec.Command("winget", "install", "--id", "Ollama.Ollama", "-e",
			"--accept-source-agreements", "--accept-package-agreements")
	default:
		return Job{}, fmt.Errorf("install option %q cannot be run", id)
	}

	job := m.newJob("install", id)
	go m.runCommand(job, cmd)
	snapshot, _ := m.Get(job.ID)
	return snapshot, nil
}

func (m *Manager) runCommand(job *Job, cmd *exec.Cmd) {
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		m.finish(job, err)
		return
	}
	m.update(job, func(j *Job) { appendOutput(j, "$ "+strings.Join(cmd.Args, " ")) })

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\r")
			if line == "" {
				continue
			}
			m.update(job, func(j *Job) {
				j.Status = line
				appendOutput(j, line)
			})
		}
	}()

	err := cmd.Wait()
	_ = pw.Close()
	<-done
	if err != nil {
		m.finish(job, fmt.Errorf("install command failed: %w", err))
		return
	}
	m.finish(job, nil)
}

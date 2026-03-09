// Package store provides task persistence and retrieval.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

// Store defines the interface for task storage.
type Store interface {
	Save(task *models.Task) error
	Get(id string) (*models.Task, error)
	List(filter ListFilter) ([]*models.Task, error)
	Delete(id string) error
	UpdateStatus(id string, status models.TaskStatus) error
	Close() error
}

// ListFilter defines criteria for listing tasks.
type ListFilter struct {
	Status []models.TaskStatus
	Tags   []string
	Limit  int
	Offset int
}

// FileStore implements Store using a JSON file for persistence.
type FileStore struct {
	path      string
	tasks     map[string]*models.Task
	mu        sync.RWMutex
	saveOnce  sync.Once
	closeOnce sync.Once
	dirty     bool
	closeCh   chan struct{}
}

// NewFileStore creates a new file-based store.
func NewFileStore(path string) (*FileStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	fs := &FileStore{
		path:    path,
		tasks:   make(map[string]*models.Task),
		closeCh: make(chan struct{}),
	}

	if err := fs.load(); err != nil {
		return nil, err
	}

	// Start background saver
	go fs.backgroundSaver()

	return fs, nil
}

func (fs *FileStore) load() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read store file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var tasks map[string]*models.Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("failed to parse store file: %w", err)
	}

	fs.tasks = tasks
	return nil
}

func (fs *FileStore) save() error {
	fs.mu.RLock()
	data, err := json.MarshalIndent(fs.tasks, "", "  ")
	fs.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal tasks: %w", err)
	}

	tmpPath := fs.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, fs.path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

func (fs *FileStore) backgroundSaver() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fs.mu.RLock()
			dirty := fs.dirty
			fs.mu.RUnlock()

			if dirty {
				if err := fs.save(); err == nil {
					fs.mu.Lock()
					fs.dirty = false
					fs.mu.Unlock()
				}
			}
		case <-fs.closeCh:
			fs.save()
			return
		}
	}
}

// Save stores or updates a task.
func (fs *FileStore) Save(task *models.Task) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.tasks[task.ID] = task
	fs.dirty = true

	return nil
}

// Get retrieves a task by ID.
func (fs *FileStore) Get(id string) (*models.Task, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	task, exists := fs.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	return task, nil
}

// List retrieves tasks matching the filter.
func (fs *FileStore) List(filter ListFilter) ([]*models.Task, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var result []*models.Task

	for _, task := range fs.tasks {
		if fs.matchesFilter(task, filter) {
			result = append(result, task)
		}
	}

	// Sort by creation time, newest first
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply offset and limit
	if filter.Offset > 0 {
		if filter.Offset >= len(result) {
			return []*models.Task{}, nil
		}
		result = result[filter.Offset:]
	}

	if filter.Limit > 0 && filter.Limit < len(result) {
		result = result[:filter.Limit]
	}

	return result, nil
}

func (fs *FileStore) matchesFilter(task *models.Task, filter ListFilter) bool {
	// Filter by status
	if len(filter.Status) > 0 {
		matched := false
		for _, s := range filter.Status {
			if task.Status == s {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Filter by tags
	if len(filter.Tags) > 0 {
		for _, filterTag := range filter.Tags {
			found := false
			for _, taskTag := range task.Tags {
				if taskTag == filterTag {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// Delete removes a task by ID.
func (fs *FileStore) Delete(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if _, exists := fs.tasks[id]; !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	delete(fs.tasks, id)
	fs.dirty = true

	return nil
}

// UpdateStatus updates only the status of a task.
func (fs *FileStore) UpdateStatus(id string, status models.TaskStatus) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	task, exists := fs.tasks[id]
	if !exists {
		return fmt.Errorf("task not found: %s", id)
	}

	task.Status = status
	fs.dirty = true

	return nil
}

// Close stops the background saver and performs final save.
// Safe to call multiple times.
func (fs *FileStore) Close() error {
	fs.closeOnce.Do(func() {
		close(fs.closeCh)
	})
	return nil
}

// Reload reloads the store from disk.
func (fs *FileStore) Reload() error {
	return fs.load()
}

// ForceSave immediately persists all tasks to disk.
func (fs *FileStore) ForceSave() error {
	fs.mu.Lock()
	fs.dirty = false
	fs.mu.Unlock()
	return fs.save()
}

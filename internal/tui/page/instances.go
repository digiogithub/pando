// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package page

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/tui/components/instances"
	"github.com/digiogithub/pando/internal/tui/layout"
)

// InstancesPage is the page.PageID for the instance browser.
const InstancesPage PageID = "instances"

// instancesPageModel wraps instances.Model and implements the tea.Model and
// layout.Sizeable interfaces expected by the app model.
type instancesPageModel struct {
	model instances.Model
}

// Init delegates to the inner model.
func (p *instancesPageModel) Init() tea.Cmd {
	return p.model.Init()
}

// Update delegates to the inner model.
func (p *instancesPageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := p.model.Update(msg)
	p.model = updated.(instances.Model)
	return p, cmd
}

// View delegates to the inner model.
func (p *instancesPageModel) View() string {
	return p.model.View()
}

// GetSize implements layout.Sizeable.
func (p *instancesPageModel) GetSize() (int, int) {
	return p.model.Width(), p.model.Height()
}

// SetSize implements layout.Sizeable.
func (p *instancesPageModel) SetSize(width, height int) tea.Cmd {
	p.model.SetSize(width, height)
	return nil
}

// BindingKeys implements layout.Bindings.
func (p *instancesPageModel) BindingKeys() []key.Binding {
	return nil
}

// NewInstancesPage creates a new instances browser page.
func NewInstancesPage() tea.Model {
	return &instancesPageModel{
		model: instances.New(),
	}
}

// Ensure interfaces are satisfied at compile time.
var (
	_ tea.Model      = (*instancesPageModel)(nil)
	_ layout.Sizeable = (*instancesPageModel)(nil)
	_ layout.Bindings = (*instancesPageModel)(nil)
)

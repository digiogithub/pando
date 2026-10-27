package client

import (
	"context"
	"net/url"
)

// SkillCatalogItem is a search hit of the online skills catalogue.
type SkillCatalogItem struct {
	ID       string `json:"id"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int    `json:"installs"`
	Source   string `json:"source"`
}

// InstalledSkill is a skill on disk.
type InstalledSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	Scope       string `json:"scope"`
	Active      bool   `json:"active"`
	SkillID     string `json:"skillId"`
}

// SearchSkills queries the catalogue.
func (c *Client) SearchSkills(ctx context.Context, q string) ([]SkillCatalogItem, error) {
	var out struct {
		Skills []SkillCatalogItem `json:"skills"`
	}
	err := c.Get(ctx, "/api/v1/skills/catalog?q="+url.QueryEscape(q), &out)
	return out.Skills, err
}

// InstalledSkills lists the installed skills.
func (c *Client) InstalledSkills(ctx context.Context) ([]InstalledSkill, error) {
	var out struct {
		Skills []InstalledSkill `json:"skills"`
	}
	err := c.Get(ctx, "/api/v1/skills/installed", &out)
	return out.Skills, err
}

// InstallSkill installs a catalogue skill ("global" or "project").
func (c *Client) InstallSkill(ctx context.Context, it SkillCatalogItem, scope string) error {
	return c.Post(ctx, "/api/v1/skills/install", map[string]string{
		"name": it.Name, "source": it.Source, "skillId": it.SkillID, "scope": scope}, nil)
}

// UninstallSkill removes an installed skill.
func (c *Client) UninstallSkill(ctx context.Context, name string) error {
	return c.Delete(ctx, "/api/v1/skills/"+Esc(name), nil)
}

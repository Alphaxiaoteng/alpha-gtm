package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type AIImageProject struct {
	ID            int64
	Name          string
	WorkspaceJSON string
	CreatedAt     string
	UpdatedAt     string
}

func (db *DB) ListAIImageProjects() ([]AIImageProject, error) {
	rows, err := db.Query(`
SELECT id, name, workspace_json, created_at, updated_at
FROM ai_image_projects
ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []AIImageProject{}
	for rows.Next() {
		var project AIImageProject
		if err := rows.Scan(&project.ID, &project.Name, &project.WorkspaceJSON, &project.CreatedAt, &project.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (db *DB) GetAIImageProject(id int64) (*AIImageProject, error) {
	var project AIImageProject
	err := db.QueryRow(`
SELECT id, name, workspace_json, created_at, updated_at
FROM ai_image_projects
WHERE id = ?`, id).Scan(&project.ID, &project.Name, &project.WorkspaceJSON, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (db *DB) CreateAIImageProject(name, workspaceJSON string) (*AIImageProject, error) {
	name = strings.TrimSpace(name)
	if name == "" || workspaceJSON == "" {
		return nil, fmt.Errorf("invalid AI image project")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`
INSERT INTO ai_image_projects (name, workspace_json, created_at, updated_at)
VALUES (?, ?, ?, ?)`, name, workspaceJSON, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.GetAIImageProject(id)
}

func (db *DB) UpdateAIImageProject(id int64, name, workspaceJSON string) (*AIImageProject, error) {
	name = strings.TrimSpace(name)
	if id <= 0 || name == "" || workspaceJSON == "" {
		return nil, fmt.Errorf("invalid AI image project")
	}
	result, err := db.Exec(`
UPDATE ai_image_projects
SET name = ?, workspace_json = ?, updated_at = ?
WHERE id = ?`, name, workspaceJSON, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed == 0 {
		return nil, sql.ErrNoRows
	}
	return db.GetAIImageProject(id)
}

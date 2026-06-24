package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store persists Blackboard snapshots.
type Store interface {
	Save(workDir string, bb *Blackboard) error
	Load(workDir string) (*Blackboard, error)
}

// JSONStore saves .blackboard.json under each workspace directory.
type JSONStore struct {
	workspaceRoot string
}

// NewJSONStore creates a JSON-backed store rooted at workspaceRoot.
func NewJSONStore(workspaceRoot string) *JSONStore {
	return &JSONStore{workspaceRoot: workspaceRoot}
}

func (s *JSONStore) Save(workDir string, bb *Blackboard) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	path := filepath.Join(workDir, ".blackboard.json")
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(bb, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal blackboard: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write blackboard tmp: %w", err)
	}
	return os.Rename(tmp, path)
}

func (s *JSONStore) Load(workDir string) (*Blackboard, error) {
	path := filepath.Join(workDir, ".blackboard.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bb Blackboard
	if err := json.Unmarshal(data, &bb); err != nil {
		return nil, fmt.Errorf("unmarshal blackboard: %w", err)
	}
	if bb.Hypotheses == nil {
		bb.Hypotheses = make(map[string]*HypothesisNode)
	}
	if bb.Tasks == nil {
		bb.Tasks = make(map[string]*DroneTask)
	}
	return &bb, nil
}

func (s *JSONStore) dirFor(target string) string {
	name := strings.TrimSpace(target)
	name = strings.ReplaceAll(name, "https://", "")
	name = strings.ReplaceAll(name, "http://", "")
	name = strings.ReplaceAll(name, "://", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" {
		name = "unknown"
	}
	return filepath.Join(s.workspaceRoot, name)
}

// WorkspacePath returns the directory that would be used for a target.
func (s *JSONStore) WorkspacePath(target string) string {
	return s.dirFor(target)
}

// List returns metadata for every workspace that has a .blackboard.json file.
func (s *JSONStore) List() ([]WorkspaceMeta, error) {
	entries, err := os.ReadDir(s.workspaceRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []WorkspaceMeta{}, nil
		}
		return nil, err
	}
	var metas []WorkspaceMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		workDir := filepath.Join(s.workspaceRoot, e.Name())
		path := filepath.Join(workDir, ".blackboard.json")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		bb, err := s.Load(workDir)
		if err != nil {
			continue
		}
		status := "inactive"
		if bb.Active {
			status = "active"
		}
		metas = append(metas, WorkspaceMeta{
			ID:        e.Name(),
			Target:    bb.Target,
			Status:    status,
			UpdatedAt: info.ModTime(),
		})
	}
	return metas, nil
}

// WorkspaceMeta describes a persisted workspace.
type WorkspaceMeta struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Report helpers.

// WriteReport writes a report file under workDir/reports/.
func WriteReport(workDir, name string, data []byte) error {
	dir := filepath.Join(workDir, "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TimestampedReportName returns a report basename with current timestamp.
func TimestampedReportName(base, ext string) string {
	ts := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_%s.%s", base, ts, ext)
}

// SanitizeFilename makes a string safe for use as a filename.
func SanitizeFilename(name string) string {
	var out []rune
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

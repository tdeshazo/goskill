package lockfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	globalVersion = 3
	localVersion  = 1
)

type GlobalEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	Ref             string `json:"ref,omitempty"`
	SkillPath       string `json:"skillPath,omitempty"`
	SkillFolderHash string `json:"skillFolderHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	PluginName      string `json:"pluginName,omitempty"`
}

type Global struct {
	Version            int                    `json:"version"`
	Skills             map[string]GlobalEntry `json:"skills"`
	Dismissed          map[string]bool        `json:"dismissed,omitempty"`
	LastSelectedAgents []string               `json:"lastSelectedAgents,omitempty"`
}

type LocalEntry struct {
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SourceType   string `json:"sourceType"`
	SkillPath    string `json:"skillPath,omitempty"`
	ComputedHash string `json:"computedHash"`
}

type Local struct {
	Version int                   `json:"version"`
	Skills  map[string]LocalEntry `json:"skills"`
}

func GlobalPath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "skills", ".skill-lock.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents", ".skill-lock.json")
}

func LocalPath(cwd string) string {
	return filepath.Join(cwd, "skills-lock.json")
}

func ReadGlobal() Global {
	path := GlobalPath()
	var lock Global
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &lock) != nil || lock.Version < globalVersion || lock.Skills == nil {
		return Global{Version: globalVersion, Skills: map[string]GlobalEntry{}, Dismissed: map[string]bool{}}
	}
	if lock.Dismissed == nil {
		lock.Dismissed = map[string]bool{}
	}
	return lock
}

func WriteGlobal(lock Global) error {
	lock.Version = globalVersion
	if lock.Skills == nil {
		lock.Skills = map[string]GlobalEntry{}
	}
	return writeJSON(GlobalPath(), lock, false)
}

func AddGlobal(skillName string, entry GlobalEntry) error {
	lock := ReadGlobal()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if old, ok := lock.Skills[skillName]; ok && old.InstalledAt != "" {
		entry.InstalledAt = old.InstalledAt
	} else {
		entry.InstalledAt = now
	}
	entry.UpdatedAt = now
	lock.Skills[skillName] = entry
	return WriteGlobal(lock)
}

func RemoveGlobal(skillName string) error {
	lock := ReadGlobal()
	delete(lock.Skills, skillName)
	return WriteGlobal(lock)
}

func ReadLocal(cwd string) Local {
	var lock Local
	data, err := os.ReadFile(LocalPath(cwd))
	if err != nil || json.Unmarshal(data, &lock) != nil || lock.Version < localVersion || lock.Skills == nil {
		return Local{Version: localVersion, Skills: map[string]LocalEntry{}}
	}
	return lock
}

func WriteLocal(cwd string, lock Local) error {
	lock.Version = localVersion
	if lock.Skills == nil {
		lock.Skills = map[string]LocalEntry{}
	}
	return writeJSON(LocalPath(cwd), orderedLocal(lock), true)
}

func AddLocal(cwd, skillName string, entry LocalEntry) error {
	lock := ReadLocal(cwd)
	lock.Skills[skillName] = entry
	return WriteLocal(cwd, lock)
}

func RemoveLocal(cwd, skillName string) error {
	lock := ReadLocal(cwd)
	delete(lock.Skills, skillName)
	return WriteLocal(cwd, lock)
}

func writeJSON(path string, value any, trailingNewline bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if trailingNewline {
		data = append(data, '\n')
	}
	return os.WriteFile(path, data, 0o644)
}

func orderedLocal(lock Local) Local {
	keys := make([]string, 0, len(lock.Skills))
	for key := range lock.Skills {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := Local{Version: lock.Version, Skills: map[string]LocalEntry{}}
	for _, key := range keys {
		out.Skills[key] = lock.Skills[key]
	}
	return out
}

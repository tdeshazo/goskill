package installer

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tdeshazo/goskill/internal/agents"
	"github.com/tdeshazo/goskill/internal/skills"
)

type Mode string

const (
	Symlink Mode = "symlink"
	Copy    Mode = "copy"
)

type Result struct {
	Success       bool
	Path          string
	CanonicalPath string
	Mode          Mode
	SymlinkFailed bool
	Skipped       bool
	Err           error
}

type Installed struct {
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Path          string        `json:"path"`
	CanonicalPath string        `json:"canonicalPath"`
	Scope         string        `json:"scope"`
	Agents        []agents.Type `json:"agents"`
}

func InstallSkill(skill skills.Skill, agent agents.Type, global bool, cwd string, mode Mode) Result {
	name := skills.SanitizeName(skill.Name)
	canonicalBase := agents.CanonicalSkillsDir(global, cwd)
	canonicalDir := filepath.Join(canonicalBase, name)
	agentBase := agents.BaseDir(agent, global, cwd)
	agentDir := filepath.Join(agentBase, name)
	if !skills.PathSafe(canonicalBase, canonicalDir) || !skills.PathSafe(agentBase, agentDir) {
		return Result{Success: false, Path: agentDir, Mode: mode, Err: errors.New("invalid skill name: potential path traversal detected")}
	}
	if mode == Copy {
		err := cleanAndCopy(skill, agentDir)
		return resultFromErr(err, agentDir, "", Copy, false)
	}
	if err := cleanAndCopy(skill, canonicalDir); err != nil {
		return resultFromErr(err, agentDir, canonicalDir, Symlink, false)
	}
	if samePath(canonicalDir, agentDir) {
		return Result{Success: true, Path: canonicalDir, CanonicalPath: canonicalDir, Mode: Symlink}
	}
	if !global && agent == agents.ClaudeCode {
		if _, err := os.Stat(filepath.Join(cwd, ".claude")); err != nil {
			return Result{Success: true, Path: canonicalDir, CanonicalPath: canonicalDir, Mode: Symlink, Skipped: true}
		}
	}
	if err := createSymlink(canonicalDir, agentDir); err != nil {
		if copyErr := cleanAndCopy(skill, agentDir); copyErr != nil {
			return resultFromErr(copyErr, agentDir, canonicalDir, Symlink, false)
		}
		return Result{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: Symlink, SymlinkFailed: true}
	}
	return Result{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: Symlink}
}

func IsInstalled(skillName string, agent agents.Type, global bool, cwd string) bool {
	name := skills.SanitizeName(skillName)
	base := agents.BaseDir(agent, global, cwd)
	target := filepath.Join(base, name)
	if !skills.PathSafe(base, target) {
		return false
	}
	_, err := os.Stat(target)
	return err == nil
}

func InstallPath(skillName string, agent agents.Type, global bool, cwd string) string {
	return filepath.Join(agents.BaseDir(agent, global, cwd), skills.SanitizeName(skillName))
}

func CanonicalPath(skillName string, global bool, cwd string) string {
	return filepath.Join(agents.CanonicalSkillsDir(global, cwd), skills.SanitizeName(skillName))
}

func List(global *bool, filter []agents.Type, cwd string) ([]Installed, error) {
	var scopes []bool
	if global == nil {
		scopes = []bool{false, true}
	} else {
		scopes = []bool{*global}
	}
	checkAgents := filter
	if len(checkAgents) == 0 {
		checkAgents = agents.DetectInstalled(cwd)
		if len(checkAgents) == 0 {
			checkAgents = agents.Ordered()
		}
	}
	byKey := map[string]*Installed{}
	for _, isGlobal := range scopes {
		scopeLabel := "project"
		if isGlobal {
			scopeLabel = "global"
		}
		dirs := []struct {
			path  string
			agent agents.Type
		}{{path: agents.CanonicalSkillsDir(isGlobal, cwd)}}
		for _, agent := range agents.Ordered() {
			base := agents.BaseDir(agent, isGlobal, cwd)
			if !containsDir(dirs, base, isGlobal) {
				if _, err := os.Stat(base); err == nil {
					dirs = append(dirs, struct {
						path  string
						agent agents.Type
					}{path: base, agent: agent})
				}
			}
		}
		for _, dir := range dirs {
			entries, err := os.ReadDir(dir.path)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
					continue
				}
				skillDir := filepath.Join(dir.path, entry.Name())
				skill, ok := skills.ParseSkillMD(filepath.Join(skillDir, "SKILL.md"), false)
				if !ok {
					continue
				}
				key := scopeLabel + ":" + skill.Name
				item := byKey[key]
				if item == nil {
					item = &Installed{Name: skill.Name, Description: skill.Description, Path: skillDir, CanonicalPath: skillDir, Scope: scopeLabel}
					byKey[key] = item
				}
				if dir.agent != "" {
					addAgent(item, dir.agent, checkAgents)
				} else {
					for _, agent := range checkAgents {
						if IsInstalled(skill.Name, agent, isGlobal, cwd) {
							addAgent(item, agent, checkAgents)
						}
					}
				}
			}
		}
	}
	out := make([]Installed, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, *item)
	}
	return out, nil
}

func Remove(skillName string, targetAgents []agents.Type, global bool, cwd string) error {
	canonical := CanonicalPath(skillName, global, cwd)
	name := skills.SanitizeName(skillName)
	for _, agent := range targetAgents {
		paths := map[string]bool{InstallPath(skillName, agent, global, cwd): true}
		cfg, _ := agents.Get(agent)
		if global {
			paths[filepath.Join(cfg.GlobalSkillsDir, name)] = true
		} else {
			paths[filepath.Join(cwd, cfg.SkillsDir, name)] = true
		}
		for path := range paths {
			if samePath(path, canonical) {
				continue
			}
			_ = os.RemoveAll(path)
		}
	}
	return os.RemoveAll(canonical)
}

func cleanAndCopy(skill skills.Skill, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if len(skill.Files) > 0 {
		for _, file := range skill.Files {
			target := filepath.Join(dest, file.Path)
			if !skills.PathSafe(dest, target) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(target, []byte(file.Contents), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	return copyDir(skill.Path, dest)
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "__pycache__" || d.Name() == "__pypackages__" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0o755)
		}
		if d.Name() == "metadata.json" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			path = target
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		outPath := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func createSymlink(target, linkPath string) error {
	_ = os.RemoveAll(linkPath)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return err
	}
	linkDir := filepath.Dir(linkPath)
	rel, err := filepath.Rel(linkDir, target)
	if err != nil {
		rel = target
	}
	if runtime.GOOS == "windows" {
		return os.Symlink(target, linkPath)
	}
	return os.Symlink(rel, linkPath)
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	ra, errA := filepath.EvalSymlinks(aa)
	rb, errB := filepath.EvalSymlinks(bb)
	if errA == nil {
		aa = ra
	}
	if errB == nil {
		bb = rb
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func resultFromErr(err error, path, canonical string, mode Mode, symlinkFailed bool) Result {
	return Result{Success: err == nil, Path: path, CanonicalPath: canonical, Mode: mode, SymlinkFailed: symlinkFailed, Err: err}
}

func containsDir(dirs []struct {
	path  string
	agent agents.Type
}, path string, global bool) bool {
	clean := filepath.Clean(path)
	for _, dir := range dirs {
		if filepath.Clean(dir.path) == clean {
			return true
		}
	}
	return false
}

func addAgent(item *Installed, agent agents.Type, filter []agents.Type) {
	if len(filter) > 0 {
		found := false
		for _, f := range filter {
			if f == agent {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	for _, existing := range item.Agents {
		if existing == agent {
			return
		}
	}
	item.Agents = append(item.Agents, agent)
}

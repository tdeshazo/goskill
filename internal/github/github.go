package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/tdeshazo/goskill/internal/skills"
)

type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
}

type RepoTree struct {
	SHA    string      `json:"sha"`
	Branch string      `json:"branch"`
	Tree   []TreeEntry `json:"tree"`
}

type DownloadResponse struct {
	Files []skills.SnapshotFile `json:"files"`
	Hash  string                `json:"hash"`
}

type BlobInstallResult struct {
	Skills []skills.Skill
	Tree   RepoTree
}

var downloadBase = envDefault("SKILLS_DOWNLOAD_URL", "https://skills.sh")

func Clone(repoURL, ref string) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "skills-")
	if err != nil {
		return "", func() {}, err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, repoURL, tmp)
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_LFS_SKIP_SMUDGE=1",
	)
	cmd.Env = append(cmd.Env,
		"GIT_CONFIG_COUNT=4",
		"GIT_CONFIG_KEY_0=filter.lfs.required", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=filter.lfs.smudge", "GIT_CONFIG_VALUE_1=",
		"GIT_CONFIG_KEY_2=filter.lfs.clean", "GIT_CONFIG_VALUE_2=",
		"GIT_CONFIG_KEY_3=filter.lfs.process", "GIT_CONFIG_VALUE_3=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmp)
		if ctx.Err() == context.DeadlineExceeded {
			return "", func() {}, fmt.Errorf("clone timed out for %s", repoURL)
		}
		return "", func() {}, fmt.Errorf("failed to clone %s: %s", repoURL, strings.TrimSpace(string(out)))
	}
	return tmp, func() { _ = os.RemoveAll(tmp) }, nil
}

func FetchRepoTree(ownerRepo, ref string) (RepoTree, bool) {
	if fixture := os.Getenv("GITHUB_TREE_FIXTURE"); fixture != "" {
		var payload struct {
			SHA  string      `json:"sha"`
			Tree []TreeEntry `json:"tree"`
		}
		if json.Unmarshal([]byte(fixture), &payload) == nil {
			branch := ref
			if branch == "" {
				branch = "HEAD"
			}
			return RepoTree{SHA: payload.SHA, Branch: branch, Tree: payload.Tree}, true
		}
	}
	branches := []string{"HEAD", "main", "master"}
	if ref != "" {
		branches = []string{ref}
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	for _, branch := range branches {
		tree, ok := fetchTreeBranch(ownerRepo, branch, token)
		if ok {
			return tree, true
		}
	}
	return RepoTree{}, false
}

func SkillFolderHash(tree RepoTree, skillPath string) string {
	folder := strings.ReplaceAll(skillPath, "\\", "/")
	lower := strings.ToLower(folder)
	switch {
	case strings.HasSuffix(lower, "/skill.md"):
		folder = folder[:len(folder)-len("/SKILL.md")]
	case lower == "skill.md":
		folder = ""
	case strings.HasSuffix(lower, "skill.md"):
		folder = strings.TrimSuffix(folder, "SKILL.md")
	}
	folder = strings.TrimSuffix(folder, "/")
	if folder == "" {
		return tree.SHA
	}
	for _, entry := range tree.Tree {
		if entry.Type == "tree" && entry.Path == folder {
			return entry.SHA
		}
	}
	return ""
}

func TryBlobInstall(ownerRepo, subpath, skillFilter, ref string, includeInternal bool) (BlobInstallResult, bool) {
	tree, ok := FetchRepoTree(ownerRepo, ref)
	if !ok {
		return BlobInstallResult{}, false
	}
	paths := FindSkillMDPaths(tree, subpath)
	if len(paths) == 0 {
		return BlobInstallResult{}, false
	}
	if skillFilter != "" {
		filterSlug := ToSkillSlug(skillFilter)
		var byFolder []string
		for _, p := range paths {
			dir := path.Base(path.Dir(p))
			if ToSkillSlug(dir) == filterSlug {
				byFolder = append(byFolder, p)
			}
		}
		if len(byFolder) > 0 {
			paths = byFolder
		}
	}
	var parsed []skills.Skill
	for _, mdPath := range paths {
		content, ok := fetchRawSkillMD(ownerRepo, tree.Branch, mdPath)
		if !ok {
			continue
		}
		tmp, ok := parseRemoteSkill(content, includeInternal)
		if !ok {
			continue
		}
		tmp.RepoPath = mdPath
		parsed = append(parsed, tmp)
	}
	if len(parsed) == 0 {
		return BlobInstallResult{}, false
	}
	if skillFilter != "" {
		filterSlug := ToSkillSlug(skillFilter)
		var byName []skills.Skill
		for _, s := range parsed {
			if ToSkillSlug(s.Name) == filterSlug {
				byName = append(byName, s)
			}
		}
		if len(byName) > 0 {
			parsed = byName
		}
	}
	var out []skills.Skill
	source := strings.ToLower(ownerRepo)
	for _, skill := range parsed {
		download, ok := fetchDownload(source, ToSkillSlug(skill.Name))
		if !ok {
			return BlobInstallResult{}, false
		}
		skill.Files = download.Files
		skill.Hash = download.Hash
		out = append(out, skill)
	}
	return BlobInstallResult{Skills: out, Tree: tree}, true
}

func FindSkillMDPaths(tree RepoTree, subpath string) []string {
	var all []string
	for _, entry := range tree.Tree {
		if entry.Type == "blob" && strings.HasSuffix(strings.ToLower(entry.Path), "skill.md") {
			all = append(all, entry.Path)
		}
	}
	prefix := ""
	if subpath != "" {
		prefix = strings.TrimSuffix(subpath, "/") + "/"
	}
	var filtered []string
	for _, p := range all {
		if prefix == "" || strings.HasPrefix(p, prefix) || p == prefix+"SKILL.md" {
			filtered = append(filtered, p)
		}
	}
	priorityPrefixes := []string{"", "skills/", "skills/.curated/", "skills/.experimental/", "skills/.system/", ".agents/skills/", ".claude/skills/", ".codex/skills/", ".cursor/skills/"}
	seen := map[string]bool{}
	var priority []string
	for _, pp := range priorityPrefixes {
		full := prefix + pp
		for _, p := range filtered {
			if !strings.HasPrefix(p, full) {
				continue
			}
			rest := strings.TrimPrefix(p, full)
			parts := strings.Split(rest, "/")
			if strings.EqualFold(rest, "SKILL.md") || (len(parts) == 2 && strings.EqualFold(parts[1], "SKILL.md")) {
				if !seen[p] {
					priority = append(priority, p)
					seen[p] = true
				}
			}
		}
	}
	if len(priority) > 0 {
		return priority
	}
	var fallback []string
	for _, p := range filtered {
		if len(strings.Split(p, "/")) <= 6 {
			fallback = append(fallback, p)
		}
	}
	sort.Strings(fallback)
	return fallback
}

func ToSkillSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.NewReplacer("_", "-", " ", "-").Replace(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
		} else if r == '-' && !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func fetchTreeBranch(ownerRepo, branch, token string) (RepoTree, bool) {
	apiBase := strings.TrimSuffix(envDefault("GITHUB_API_URL", "https://api.github.com"), "/")
	api := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", apiBase, ownerRepo, url.PathEscape(branch))
	var payload struct {
		SHA  string      `json:"sha"`
		Tree []TreeEntry `json:"tree"`
	}
	if !fetchJSON(api, token, &payload) {
		return RepoTree{}, false
	}
	return RepoTree{SHA: payload.SHA, Branch: branch, Tree: payload.Tree}, true
}

func fetchRawSkillMD(ownerRepo, branch, skillPath string) (string, bool) {
	rawBase := strings.TrimSuffix(envDefault("RAW_GITHUB_URL", "https://raw.githubusercontent.com"), "/")
	u := fmt.Sprintf("%s/%s/%s/%s", rawBase, ownerRepo, url.PathEscape(branch), skillPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false
	}
	body, err := io.ReadAll(res.Body)
	return string(body), err == nil
}

func fetchDownload(source, slug string) (DownloadResponse, bool) {
	parts := strings.SplitN(source, "/", 2)
	if len(parts) != 2 {
		return DownloadResponse{}, false
	}
	u := fmt.Sprintf("%s/api/download/%s/%s/%s", strings.TrimSuffix(downloadBase, "/"), url.PathEscape(parts[0]), url.PathEscape(parts[1]), url.PathEscape(slug))
	var out DownloadResponse
	return out, fetchJSON(u, "", &out)
}

func fetchJSON(u, token string, out any) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "skills-cli-go")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return false
	}
	return json.NewDecoder(res.Body).Decode(out) == nil
}

func parseRemoteSkill(raw string, includeInternal bool) (skills.Skill, bool) {
	data := skills.ParseFrontmatter(raw)
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	if name == "" || desc == "" {
		return skills.Skill{}, false
	}
	if metadata, ok := data["metadata"].(map[string]any); ok {
		if internal, _ := metadata["internal"].(bool); internal && !includeInternal && !skills.ShouldInstallInternal() {
			return skills.Skill{}, false
		}
	}
	return skills.Skill{Name: name, Description: desc, RawContent: raw}, true
}

func cloneTimeout() time.Duration {
	raw := os.Getenv("SKILLS_CLONE_TIMEOUT_MS")
	if raw == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(raw + "ms")
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func CheckGitAvailable() error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git is required for this source")
	}
	return nil
}

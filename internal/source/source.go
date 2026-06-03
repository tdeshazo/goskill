package source

import (
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type Type string

const (
	GitHub    Type = "github"
	GitLab    Type = "gitlab"
	Git       Type = "git"
	Local     Type = "local"
	WellKnown Type = "well-known"
)

type Parsed struct {
	Type        Type
	URL         string
	LocalPath   string
	Subpath     string
	Ref         string
	SkillFilter string
}

var aliases = map[string]string{
	"coinbase/agentWallet": "coinbase/agentic-wallet-skills",
}

func Parse(input string) (Parsed, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Parsed{}, errors.New("empty source")
	}
	if isLocalPath(input) {
		abs, err := filepath.Abs(input)
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{Type: Local, URL: abs, LocalPath: abs}, nil
	}

	inputNoFragment, ref, fragmentSkill := parseFragment(input)
	input = inputNoFragment
	if alias, ok := aliases[input]; ok {
		input = alias
	}

	if strings.HasPrefix(input, "github:") {
		return Parse(input[len("github:"):] + fragmentSuffix(ref, fragmentSkill))
	}
	if strings.HasPrefix(input, "gitlab:") {
		return Parse("https://gitlab.com/" + input[len("gitlab:"):] + fragmentSuffix(ref, fragmentSkill))
	}

	if m := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)/(.+)`).FindStringSubmatch(input); m != nil {
		sub, err := SanitizeSubpath(m[4])
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{Type: GitHub, URL: "https://github.com/" + m[1] + "/" + strings.TrimSuffix(m[2], ".git") + ".git", Ref: first(ref, m[3]), Subpath: sub, SkillFilter: fragmentSkill}, nil
	}
	if m := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)$`).FindStringSubmatch(input); m != nil {
		return Parsed{Type: GitHub, URL: "https://github.com/" + m[1] + "/" + strings.TrimSuffix(m[2], ".git") + ".git", Ref: first(ref, m[3]), SkillFilter: fragmentSkill}, nil
	}
	if m := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`).FindStringSubmatch(input); m != nil {
		return Parsed{Type: GitHub, URL: "https://github.com/" + m[1] + "/" + strings.TrimSuffix(m[2], ".git") + ".git", Ref: ref, SkillFilter: fragmentSkill}, nil
	}

	if m := regexp.MustCompile(`^(https?)://([^/]+)/(.+?)/-/tree/([^/]+)/(.+)`).FindStringSubmatch(input); m != nil && m[2] != "github.com" {
		sub, err := SanitizeSubpath(m[5])
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{Type: GitLab, URL: m[1] + "://" + m[2] + "/" + strings.TrimSuffix(m[3], ".git") + ".git", Ref: first(ref, m[4]), Subpath: sub, SkillFilter: fragmentSkill}, nil
	}
	if m := regexp.MustCompile(`^(https?)://([^/]+)/(.+?)/-/tree/([^/]+)$`).FindStringSubmatch(input); m != nil && m[2] != "github.com" {
		return Parsed{Type: GitLab, URL: m[1] + "://" + m[2] + "/" + strings.TrimSuffix(m[3], ".git") + ".git", Ref: first(ref, m[4]), SkillFilter: fragmentSkill}, nil
	}
	if m := regexp.MustCompile(`gitlab\.com/(.+?)(?:\.git)?/?$`).FindStringSubmatch(input); m != nil && strings.Contains(m[1], "/") {
		return Parsed{Type: GitLab, URL: "https://gitlab.com/" + strings.TrimSuffix(m[1], ".git") + ".git", Ref: ref, SkillFilter: fragmentSkill}, nil
	}

	if m := regexp.MustCompile(`^([^/]+)/([^/@]+)@(.+)$`).FindStringSubmatch(input); m != nil && !strings.Contains(input, ":") && !strings.HasPrefix(input, ".") {
		return Parsed{Type: GitHub, URL: "https://github.com/" + m[1] + "/" + m[2] + ".git", Ref: ref, SkillFilter: first(fragmentSkill, m[3])}, nil
	}
	if m := regexp.MustCompile(`^([^/]+)/([^/]+)(?:/(.+?))?/?$`).FindStringSubmatch(input); m != nil && !strings.Contains(input, ":") && !strings.HasPrefix(input, ".") {
		sub := ""
		if len(m) > 3 {
			var err error
			sub, err = SanitizeSubpath(m[3])
			if err != nil {
				return Parsed{}, err
			}
		}
		return Parsed{Type: GitHub, URL: "https://github.com/" + m[1] + "/" + m[2] + ".git", Ref: ref, Subpath: sub, SkillFilter: fragmentSkill}, nil
	}

	if isWellKnownURL(input) {
		return Parsed{Type: WellKnown, URL: input}, nil
	}
	return Parsed{Type: Git, URL: input, Ref: ref}, nil
}

func OwnerRepo(p Parsed) string {
	u := p.URL
	if p.Type == Local || u == "" {
		return ""
	}
	if strings.HasPrefix(u, "git@") {
		if i := strings.Index(u, ":"); i >= 0 {
			return strings.TrimSuffix(u[i+1:], ".git")
		}
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Path == "" {
		return ""
	}
	path := strings.Trim(strings.TrimSuffix(parsed.Path, ".git"), "/")
	if strings.Count(path, "/") >= 1 {
		return path
	}
	return ""
}

func SanitizeSubpath(subpath string) (string, error) {
	normalized := strings.ReplaceAll(subpath, "\\", "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return "", errors.New("unsafe subpath contains path traversal segments")
		}
	}
	return subpath, nil
}

func isLocalPath(input string) bool {
	if filepath.IsAbs(input) || input == "." || input == ".." || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return true
	}
	return regexp.MustCompile(`^[a-zA-Z]:[/\\]`).MatchString(input)
}

func parseFragment(input string) (string, string, string) {
	i := strings.Index(input, "#")
	if i < 0 {
		return input, "", ""
	}
	base, frag := input[:i], input[i+1:]
	if frag == "" || !looksLikeGitSource(base) {
		return input, "", ""
	}
	if at := strings.Index(frag, "@"); at >= 0 {
		return base, decode(frag[:at]), decode(frag[at+1:])
	}
	return base, decode(frag), ""
}

func looksLikeGitSource(input string) bool {
	if strings.HasPrefix(input, "github:") || strings.HasPrefix(input, "gitlab:") || strings.HasPrefix(input, "git@") {
		return true
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return false
		}
		if u.Host == "github.com" {
			return regexp.MustCompile(`^/[^/]+/[^/]+(?:\.git)?(?:/tree/[^/]+(?:/.*)?)?/?$`).MatchString(u.Path)
		}
		if u.Host == "gitlab.com" {
			return regexp.MustCompile(`^/.+?/[^/]+(?:\.git)?(?:/-/tree/[^/]+(?:/.*)?)?/?$`).MatchString(u.Path)
		}
		return strings.HasSuffix(u.Path, ".git")
	}
	return regexp.MustCompile(`^([^/]+)/([^/]+)(?:/(.+)|@(.+))?$`).MatchString(input)
}

func isWellKnownURL(input string) bool {
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if u.Host == "github.com" || u.Host == "gitlab.com" || u.Host == "raw.githubusercontent.com" {
		return false
	}
	return !strings.HasSuffix(input, ".git")
}

func fragmentSuffix(ref, skill string) string {
	if ref == "" {
		return ""
	}
	if skill != "" {
		return "#" + ref + "@" + skill
	}
	return "#" + ref
}

func decode(v string) string {
	out, err := url.QueryUnescape(v)
	if err != nil {
		return v
	}
	return out
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

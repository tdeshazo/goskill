package skills

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type SecurityRiskLevel string

const (
	RiskNone     SecurityRiskLevel = "NONE"
	RiskLow      SecurityRiskLevel = "LOW"
	RiskMedium   SecurityRiskLevel = "MEDIUM"
	RiskHigh     SecurityRiskLevel = "HIGH"
	RiskCritical SecurityRiskLevel = "CRITICAL"

	maxSecurityScanFileBytes = 512 * 1024
)

type SecurityFinding struct {
	SkillName string
	Path      string
	Line      int
	RiskLevel SecurityRiskLevel
	Category  string
	Message   string
	Evidence  string
}

type SecurityReport struct {
	RiskLevel SecurityRiskLevel
	Findings  []SecurityFinding
}

type securityRule struct {
	level    SecurityRiskLevel
	category string
	message  string
	pattern  *regexp.Regexp
}

var securityRules = []securityRule{
	{
		level:    RiskCritical,
		category: "destructive-command",
		message:  "Potentially destructive recursive removal",
		pattern:  regexp.MustCompile(`(?i)\brm\s+(?:-[a-z]*[rf][a-z]*\s+|-[a-z]*r[a-z]*\s+-[a-z]*f[a-z]*\s+|-[a-z]*f[a-z]*\s+-[a-z]*r[a-z]*\s+)(?:/|\$HOME|~|\*)`),
	},
	{
		level:    RiskCritical,
		category: "destructive-command",
		message:  "Potential disk formatting or filesystem destruction command",
		pattern:  regexp.MustCompile(`(?i)\b(?:mkfs(?:\.[a-z0-9]+)?|dd)\b.*\b(?:of=/dev/|/dev/(?:sd|hd|nvme|disk))`),
	},
	{
		level:    RiskHigh,
		category: "destructive-command",
		message:  "Broad permission or ownership mutation",
		pattern:  regexp.MustCompile(`(?i)\b(?:chmod|chown)\s+(?:-[a-z]*R[a-z]*\s+)?(?:777|666|root|[^&|;\n]*(?:/|\$HOME|~))`),
	},
	{
		level:    RiskHigh,
		category: "remote-code-execution",
		message:  "Downloads remote content and pipes it to a shell",
		pattern:  regexp.MustCompile(`(?i)\b(?:curl|wget)\b[^\n|;]*(?:\||>\s*/tmp/)[^\n]*(?:sh|bash|zsh|powershell|pwsh)\b`),
	},
	{
		level:    RiskHigh,
		category: "remote-code-execution",
		message:  "Evaluates dynamically constructed commands",
		pattern:  regexp.MustCompile(`(?i)\b(?:eval|exec)\s+["']?\$|\b(?:python|node|ruby|perl)\s+-e\s+`),
	},
	{
		level:    RiskHigh,
		category: "credential-access",
		message:  "References private keys or credential files",
		pattern:  regexp.MustCompile(`(?i)(?:id_rsa|id_ed25519|\.ssh/|\.aws/credentials|\.config/gcloud|\.docker/config\.json|\.npmrc|\.pypirc|\.netrc)`),
	},
	{
		level:    RiskMedium,
		category: "credential-access",
		message:  "References environment files or sensitive token names",
		pattern:  regexp.MustCompile(`(?i)(?:\.env(?:\.[a-z0-9_-]+)?|\b(?:GITHUB_TOKEN|OPENAI_API_KEY|ANTHROPIC_API_KEY|AWS_SECRET_ACCESS_KEY|NPM_TOKEN|PYPI_TOKEN|API_KEY|ACCESS_TOKEN)\b)`),
	},
	{
		level:    RiskHigh,
		category: "persistence",
		message:  "Modifies shell startup or system persistence locations",
		pattern:  regexp.MustCompile(`(?i)(?:>>|>|tee\s+-a)\s*(?:~|\$HOME)?/?\.(?:bashrc|zshrc|profile|bash_profile)|\b(?:crontab|systemctl|launchctl)\b|/Library/LaunchAgents|/etc/systemd`),
	},
	{
		level:    RiskHigh,
		category: "network-exfiltration",
		message:  "Potential upload of home, project, or secret data",
		pattern:  regexp.MustCompile(`(?i)\b(?:curl|wget|scp|rsync|nc|netcat)\b[^\n]*(?:--upload-file|-F|-d|--data|--data-binary|\s\./|\s~|\$HOME|\.env|\.ssh|credentials)`),
	},
	{
		level:    RiskMedium,
		category: "prompt-injection",
		message:  "Instructs the agent to ignore higher-priority or user instructions",
		pattern:  regexp.MustCompile(`(?i)\bignore\s+(?:all\s+)?(?:previous|prior|system|user|developer)\s+(?:instructions|messages|prompts)|\bdisregard\s+(?:system|user|developer)\s+(?:instructions|messages|prompts)`),
	},
	{
		level:    RiskMedium,
		category: "prompt-injection",
		message:  "Instructs the agent to hide behavior from the user",
		pattern:  regexp.MustCompile(`(?i)\b(?:do not|don't)\s+(?:tell|inform|ask)\s+the\s+user\b|\bhide\s+(?:this|your)\s+(?:actions?|behavior|steps?)\b`),
	},
	{
		level:    RiskLow,
		category: "third-party-content",
		message:  "Directs the agent to fetch or act on untrusted third-party content",
		pattern:  regexp.MustCompile(`(?i)\b(?:install|download|fetch|run)\b[^\n]*(?:from\s+)?(?:github\.com|gist\.github\.com|raw\.githubusercontent\.com|http://|https://|npm|pip|npx)`),
	},
}

var securityTextExtensions = map[string]bool{
	"":              true,
	".bash":         true,
	".fish":         true,
	".go":           true,
	".js":           true,
	".json":         true,
	".md":           true,
	".ps1":          true,
	".py":           true,
	".rb":           true,
	".sh":           true,
	".toml":         true,
	".txt":          true,
	".yaml":         true,
	".yml":          true,
	".zsh":          true,
	"dockerfile":    true,
	"makefile":      true,
	"justfile":      true,
	"procfile":      true,
	"requirements":  true,
	"gemfile":       true,
	"package-lock":  true,
	"pnpm-lock":     true,
	"yarn.lock":     true,
	"bun.lockb":     true,
	"install":       true,
	"postinstall":   true,
	"preinstall":    true,
	"entrypoint":    true,
	"metadata.json": true,
}

func AnalyzeSkillSecurity(skill Skill) SecurityReport {
	var findings []SecurityFinding
	for _, file := range skillSecurityFiles(skill) {
		findings = append(findings, analyzeSecurityContent(skill.Name, file.path, file.contents)...)
	}
	findings = dedupeSecurityFindings(findings)
	sort.SliceStable(findings, func(i, j int) bool {
		if securityRiskRank(findings[i].RiskLevel) != securityRiskRank(findings[j].RiskLevel) {
			return securityRiskRank(findings[i].RiskLevel) > securityRiskRank(findings[j].RiskLevel)
		}
		if findings[i].SkillName != findings[j].SkillName {
			return findings[i].SkillName < findings[j].SkillName
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	return SecurityReport{RiskLevel: aggregateSecurityRisk(findings), Findings: findings}
}

func MergeSecurityReports(reports ...SecurityReport) SecurityReport {
	var findings []SecurityFinding
	for _, report := range reports {
		findings = append(findings, report.Findings...)
	}
	findings = dedupeSecurityFindings(findings)
	sort.SliceStable(findings, func(i, j int) bool {
		if securityRiskRank(findings[i].RiskLevel) != securityRiskRank(findings[j].RiskLevel) {
			return securityRiskRank(findings[i].RiskLevel) > securityRiskRank(findings[j].RiskLevel)
		}
		if findings[i].SkillName != findings[j].SkillName {
			return findings[i].SkillName < findings[j].SkillName
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	return SecurityReport{RiskLevel: aggregateSecurityRisk(findings), Findings: findings}
}

type securityFile struct {
	path     string
	contents string
}

func skillSecurityFiles(skill Skill) []securityFile {
	var files []securityFile
	if strings.TrimSpace(skill.RawContent) != "" {
		files = append(files, securityFile{path: "SKILL.md", contents: skill.RawContent})
	}
	for _, file := range skill.Files {
		if securityScannablePath(file.Path) && textLike([]byte(file.Contents)) {
			files = append(files, securityFile{path: filepath.ToSlash(file.Path), contents: truncateSecurityContent(file.Contents)})
		}
	}
	if skill.Path != "" {
		files = append(files, localSkillSecurityFiles(skill.Path)...)
	}
	return files
}

func localSkillSecurityFiles(root string) []securityFile {
	var files []securityFile
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !securityScannablePath(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxSecurityScanFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !textLike(data) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = filepath.Base(path)
		}
		files = append(files, securityFile{path: filepath.ToSlash(rel), contents: string(data)})
		return nil
	})
	return files
}

func analyzeSecurityContent(skillName, path, raw string) []SecurityFinding {
	raw = truncateSecurityContent(raw)
	var findings []SecurityFinding
	for lineNo, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, rule := range securityRules {
			if !rule.pattern.MatchString(trimmed) {
				continue
			}
			findings = append(findings, SecurityFinding{
				SkillName: skillName,
				Path:      path,
				Line:      lineNo + 1,
				RiskLevel: rule.level,
				Category:  rule.category,
				Message:   rule.message,
				Evidence:  trimSecurityEvidence(trimmed),
			})
		}
	}
	return findings
}

func securityScannablePath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(name))
	return securityTextExtensions[ext] || securityTextExtensions[name] || strings.EqualFold(name, "skill.md")
}

func textLike(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if len(data) > maxSecurityScanFileBytes {
		data = data[:maxSecurityScanFileBytes]
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

func truncateSecurityContent(raw string) string {
	if len(raw) <= maxSecurityScanFileBytes {
		return raw
	}
	return raw[:maxSecurityScanFileBytes]
}

func trimSecurityEvidence(raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	const maxEvidence = 140
	if len(raw) <= maxEvidence {
		return raw
	}
	return raw[:maxEvidence-3] + "..."
}

func dedupeSecurityFindings(findings []SecurityFinding) []SecurityFinding {
	seen := map[string]bool{}
	out := make([]SecurityFinding, 0, len(findings))
	for _, finding := range findings {
		key := strings.Join([]string{
			finding.SkillName,
			finding.Path,
			finding.Category,
			finding.Evidence,
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, finding)
	}
	return out
}

func aggregateSecurityRisk(findings []SecurityFinding) SecurityRiskLevel {
	level := RiskNone
	for _, finding := range findings {
		if securityRiskRank(finding.RiskLevel) > securityRiskRank(level) {
			level = finding.RiskLevel
		}
	}
	return level
}

func securityRiskRank(level SecurityRiskLevel) int {
	switch level {
	case RiskCritical:
		return 4
	case RiskHigh:
		return 3
	case RiskMedium:
		return 2
	case RiskLow:
		return 1
	default:
		return 0
	}
}

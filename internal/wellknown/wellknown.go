package wellknown

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/tdeshazo/goskill/internal/skills"
)

const schemaV2 = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"

type Skill struct {
	skills.Skill
	SourceURL string
}

type indexV1 struct {
	Skills []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Files       []string `json:"files"`
	} `json:"skills"`
}

type indexV2 struct {
	Schema string `json:"$schema"`
	Skills []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		URL         string `json:"url"`
		Digest      string `json:"digest"`
	} `json:"skills"`
}

func FetchAll(baseURL string) ([]Skill, error) {
	idxURL, body, wellKnownPath, err := fetchIndex(baseURL)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Schema string `json:"$schema"`
	}
	_ = json.Unmarshal(body, &probe)
	if probe.Schema == schemaV2 {
		return fetchV2(idxURL, body)
	}
	return fetchV1(idxURL, body, wellKnownPath)
}

func SourceIdentifier(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return "wellknown/" + u.Host
}

func fetchIndex(baseURL string) (string, []byte, string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", nil, "", err
	}
	basePath := strings.TrimSuffix(parsed.Path, "/")
	var candidates []struct {
		u    string
		path string
	}
	for _, wk := range []string{".well-known/agent-skills", ".well-known/skills"} {
		candidates = append(candidates, struct {
			u    string
			path string
		}{fmt.Sprintf("%s://%s%s/%s/index.json", parsed.Scheme, parsed.Host, basePath, wk), wk})
		if basePath != "" {
			candidates = append(candidates, struct {
				u    string
				path string
			}{fmt.Sprintf("%s://%s/%s/index.json", parsed.Scheme, parsed.Host, wk), wk})
		}
	}
	for _, c := range candidates {
		body, ok := fetchBytes(c.u)
		if ok {
			return c.u, body, c.path, nil
		}
	}
	return "", nil, "", errors.New("no well-known skills index found")
}

func fetchV1(indexURL string, body []byte, wellKnownPath string) ([]Skill, error) {
	var idx indexV1
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(indexURL, "/"+wellKnownPath+"/index.json")
	var out []Skill
	for _, entry := range idx.Skills {
		if entry.Name == "" || len(entry.Files) == 0 {
			continue
		}
		s := Skill{Skill: skills.Skill{Name: entry.Name}}
		for _, file := range entry.Files {
			if !safeFile(file) {
				continue
			}
			fileURL := base + "/" + wellKnownPath + "/" + entry.Name + "/" + file
			content, ok := fetchBytes(fileURL)
			if !ok {
				continue
			}
			s.Files = append(s.Files, skills.SnapshotFile{Path: file, Contents: string(content)})
		}
		if desc, ok := yamlDescription(s.Files); ok {
			s.Description = desc
			s.SourceURL = base
			out = append(out, s)
		}
	}
	return out, nil
}

func fetchV2(indexURL string, body []byte) ([]Skill, error) {
	var idx indexV2
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, err
	}
	var out []Skill
	for _, entry := range idx.Skills {
		artifactURL, err := url.Parse(entry.URL)
		if err != nil {
			continue
		}
		base, _ := url.Parse(indexURL)
		resolved := base.ResolveReference(artifactURL).String()
		data, ok := fetchBytes(resolved)
		if !ok || !digestOK(data, entry.Digest) {
			continue
		}
		s := Skill{Skill: skills.Skill{Name: entry.Name}, SourceURL: resolved}
		switch entry.Type {
		case "skill-md":
			s.Files = []skills.SnapshotFile{{Path: "SKILL.md", Contents: string(data)}}
		case "archive":
			files, err := unpackArchive(resolved, data)
			if err != nil {
				continue
			}
			s.Files = files
		}
		if desc, ok := yamlDescription(s.Files); ok {
			s.Description = desc
			out = append(out, s)
		}
	}
	return out, nil
}

func yamlDescription(files []skills.SnapshotFile) (string, bool) {
	for _, file := range files {
		name := path.Base(strings.ReplaceAll(file.Path, "\\", "/"))
		if !strings.EqualFold(name, "SKILL.md") {
			continue
		}
		data := skills.ParseFrontmatter(file.Contents)
		desc, _ := data["description"].(string)
		desc = strings.TrimSpace(desc)
		if desc != "" {
			return desc, true
		}
	}
	return "", false
}

func fetchBytes(u string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 50*1024*1024+1))
	return data, err == nil && len(data) <= 50*1024*1024
}

func digestOK(data []byte, digest string) bool {
	if !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	sum := sha256.Sum256(data)
	return "sha256:"+hex.EncodeToString(sum[:]) == digest
}

func unpackArchive(name string, data []byte) ([]skills.SnapshotFile, error) {
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		var files []skills.SnapshotFile
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || !safeFile(f.Name) {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			content, err := io.ReadAll(io.LimitReader(rc, 50*1024*1024))
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
			files = append(files, skills.SnapshotFile{Path: f.Name, Contents: string(content)})
		}
		return files, nil
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	var files []skills.SnapshotFile
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.FileInfo().IsDir() || !safeFile(h.Name) {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(tr, 50*1024*1024))
		if err != nil {
			return nil, err
		}
		files = append(files, skills.SnapshotFile{Path: h.Name, Contents: string(content)})
	}
	return files, nil
}

func safeFile(file string) bool {
	clean := path.Clean(strings.ReplaceAll(file, "\\", "/"))
	return file != "" && !strings.HasPrefix(clean, "../") && clean != ".." && !strings.HasPrefix(clean, "/") && !strings.Contains(file, "\x00")
}

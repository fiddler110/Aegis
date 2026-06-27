// Package bundle defines a portable format that installs a set of Aegis
// artifacts — slash commands, agent definitions, and skills — together, the way
// Claude Code's plugins bundle commands/subagents/skills in one install.
//
// A bundle is a directory containing a bundle.yaml manifest and any of these
// artifact subdirectories: commands/, agents/, skills/. Installing copies those
// files into the chosen scope (the per-user data dir, or a project's .aegis/).
// Config-level pieces (MCP servers, hooks) are intentionally out of scope: the
// installer never edits a user's config.yaml.
package bundle

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// ManifestName is the required manifest file at a bundle's root.
const ManifestName = "bundle.yaml"

// artifactDirs are the subdirectories a bundle may carry, mapped to their
// install destination subdirectory (which happens to be the same name).
var artifactDirs = []string{"commands", "agents", "skills"}

// Manifest is the bundle.yaml metadata.
type Manifest struct {
	Name        string `koanf:"name"`
	Version     string `koanf:"version"`
	Description string `koanf:"description"`
	Author      string `koanf:"author"`
}

// Bundle is a loaded, validated bundle ready to install.
type Bundle struct {
	Manifest  Manifest
	Dir       string
	Artifacts []Artifact
}

// Artifact is one file the bundle will install.
type Artifact struct {
	Kind string // commands | agents | skills
	Rel  string // path relative to the artifact dir (preserves nesting)
}

// Load reads and validates the bundle rooted at dir.
func Load(dir string) (*Bundle, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("bundle path %q is not a directory", dir)
	}
	manifestPath := filepath.Join(dir, ManifestName)
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, fmt.Errorf("missing %s in bundle", ManifestName)
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(manifestPath), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := k.Unmarshal("", &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("manifest is missing a name")
	}

	b := &Bundle{Manifest: m, Dir: dir}
	for _, kind := range artifactDirs {
		root := filepath.Join(dir, kind)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			b.Artifacts = append(b.Artifacts, Artifact{Kind: kind, Rel: filepath.ToSlash(rel)})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(b.Artifacts) == 0 {
		return nil, fmt.Errorf("bundle %q contains no installable artifacts (expected files under commands/, agents/, or skills/)", m.Name)
	}
	sort.Slice(b.Artifacts, func(i, j int) bool {
		if b.Artifacts[i].Kind != b.Artifacts[j].Kind {
			return b.Artifacts[i].Kind < b.Artifacts[j].Kind
		}
		return b.Artifacts[i].Rel < b.Artifacts[j].Rel
	})
	return b, nil
}

// Install copies the bundle's artifacts into destRoot (a scope directory such as
// the data dir or a project's .aegis dir). Existing files are skipped unless
// overwrite is set. Returns the relative destination paths that were written.
func (b *Bundle) Install(destRoot string, overwrite bool) ([]string, error) {
	var written []string
	for _, a := range b.Artifacts {
		src := filepath.Join(b.Dir, a.Kind, filepath.FromSlash(a.Rel))
		dst := filepath.Join(destRoot, a.Kind, filepath.FromSlash(a.Rel))
		if !overwrite {
			if _, err := os.Stat(dst); err == nil {
				continue // don't clobber an existing artifact
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return written, err
		}
		if err := copyFile(src, dst); err != nil {
			return written, fmt.Errorf("install %s: %w", a.Kind+"/"+a.Rel, err)
		}
		written = append(written, filepath.Join(a.Kind, filepath.FromSlash(a.Rel)))
	}
	return written, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

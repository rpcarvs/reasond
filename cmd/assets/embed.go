package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// Provider identifies which coding-agent asset bundle is being installed or inspected.
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

// Label returns the provider name with a user-facing leading capital.
func (p Provider) Label() string {
	name := string(p)
	if name == "" {
		return ""
	}

	return strings.ToUpper(name[:1]) + name[1:]
}

// File describes one embedded asset file and where it should be installed in the target repo.
type File struct {
	EmbeddedPath string
	TargetPath   string
	Mode         fs.FileMode
}

// FS contains the bundled init assets shipped with reasond.
//
//go:embed claude_assets/** codex_assets/**
var FS embed.FS

var providerRoots = map[Provider]string{
	ProviderClaude: "claude_assets",
	ProviderCodex:  "codex_assets",
}

// FilesForProvider returns all bundled files that must be installed for a provider.
func FilesForProvider(provider Provider) ([]File, error) {
	root, ok := providerRoots[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}

	var files []File
	err := fs.WalkDir(FS, root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relativePath := strings.TrimPrefix(filePath, root+"/")
		targetPath, mode, err := targetFor(provider, relativePath)
		if err != nil {
			return err
		}

		files = append(files, File{
			EmbeddedPath: filePath,
			TargetPath:   targetPath,
			Mode:         mode,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(files, func(a, b File) int {
		return strings.Compare(a.TargetPath, b.TargetPath)
	})

	return files, nil
}

func targetFor(provider Provider, relativePath string) (string, fs.FileMode, error) {
	switch provider {
	case ProviderClaude:
		if strings.HasPrefix(relativePath, "claude/") {
			return path.Join(".claude", strings.TrimPrefix(relativePath, "claude/")), modeFor(relativePath), nil
		}
		return relativePath, modeFor(relativePath), nil
	case ProviderCodex:
		if strings.HasPrefix(relativePath, "codex/") {
			return path.Join(".codex", strings.TrimPrefix(relativePath, "codex/")), modeFor(relativePath), nil
		}
		return relativePath, modeFor(relativePath), nil
	default:
		return "", 0, fmt.Errorf("unsupported provider %q", provider)
	}
}

func modeFor(relativePath string) fs.FileMode {
	if strings.HasSuffix(relativePath, ".sh") {
		return 0o755
	}

	return 0o644
}

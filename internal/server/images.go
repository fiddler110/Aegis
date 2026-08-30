package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiddler110/aegis/internal/api"
	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/sandbox"
)

const (
	// maxImageBytes caps the decoded size of a single attached image. 5 MiB
	// matches the Anthropic per-image limit; larger images should be resized.
	maxImageBytes = 5 << 20
	// maxImages bounds how many images a single turn may carry.
	maxImages = 20
)

// supportedImageTypes is the media-type allowlist shared by the providers that
// accept images.
var supportedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// resolveSafeImagePath validates a user-supplied image path and constrains it
// to the current working directory tree.
func resolveSafeImagePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty image path")
	}
	// Refuse a leading separator in either spelling, on every platform, before
	// consulting sandbox.IsRooted. IsRooted is deliberately platform-conditional
	// — off Windows it is just filepath.IsAbs, so `\Windows\...` reads there as
	// a legitimate (if odd) relative filename — and this call site wants the
	// opposite property. An image path arrives from an API client as a name
	// relative to the working directory; there is no such thing as a rooted one
	// we mean to honour, and accepting the backslash spelling on POSIX while
	// refusing it on Windows is the same input behaving two ways, which is the
	// defect this refusal exists to close. Not an escape either way — the Rel
	// check below still holds — but identical behaviour is the point.
	if p[0] == '/' || p[0] == '\\' {
		return "", fmt.Errorf("absolute image paths are not allowed")
	}
	if sandbox.IsRooted(p) {
		return "", fmt.Errorf("absolute image paths are not allowed")
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve base dir: %w", err)
	}
	baseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base dir: %w", err)
	}

	candidate := filepath.Clean(filepath.Join(baseDir, p))
	rel, err := filepath.Rel(baseDir, candidate)
	if err != nil {
		return "", fmt.Errorf("invalid image path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("image path escapes allowed directory")
	}
	return candidate, nil
}

// buildImageBlocks turns API image inputs into provider image blocks, reading
// and base64-encoding any path-based inputs. It enforces a per-image size cap
// and the supported-media-type allowlist. Image bytes are never logged.
func buildImageBlocks(inputs []api.ImageInput) ([]provider.Block, error) {
	if len(inputs) > maxImages {
		return nil, fmt.Errorf("too many images (%d, max %d)", len(inputs), maxImages)
	}
	blocks := make([]provider.Block, 0, len(inputs))
	for i, in := range inputs {
		mediaType := normalizeMediaType(in.MediaType)
		var data string
		switch {
		case strings.TrimSpace(in.Path) != "":
			safePath, err := resolveSafeImagePath(in.Path)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", i+1, err)
			}
			raw, err := os.ReadFile(safePath)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", i+1, err)
			}
			if len(raw) > maxImageBytes {
				return nil, fmt.Errorf("image %d is too large (%d bytes, max %d)", i+1, len(raw), maxImageBytes)
			}
			if mediaType == "" {
				mediaType = detectImageType(safePath, raw)
			}
			data = base64.StdEncoding.EncodeToString(raw)
		case in.Data != "":
			raw, err := base64.StdEncoding.DecodeString(in.Data)
			if err != nil {
				return nil, fmt.Errorf("image %d: invalid base64 data", i+1)
			}
			if len(raw) > maxImageBytes {
				return nil, fmt.Errorf("image %d is too large (%d bytes, max %d)", i+1, len(raw), maxImageBytes)
			}
			if mediaType == "" {
				mediaType = normalizeMediaType(http.DetectContentType(raw))
			}
			data = in.Data
		default:
			return nil, fmt.Errorf("image %d: provide a path or base64 data", i+1)
		}
		if !supportedImageTypes[mediaType] {
			return nil, fmt.Errorf("image %d: unsupported media type %q (supported: png, jpeg, gif, webp)", i+1, mediaType)
		}
		blocks = append(blocks, provider.ImageBlock{MediaType: mediaType, Data: data})
	}
	return blocks, nil
}

// detectImageType infers an image media type from the file extension, falling
// back to content sniffing.
func detectImageType(path string, raw []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return normalizeMediaType(http.DetectContentType(raw))
}

// normalizeMediaType lowercases a media type and strips any parameters
// (e.g. "image/jpeg; charset=binary" -> "image/jpeg").
func normalizeMediaType(s string) string {
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/scottymacleod/aegis/internal/api"
	"github.com/scottymacleod/aegis/internal/provider"
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
			raw, err := os.ReadFile(in.Path)
			if err != nil {
				return nil, fmt.Errorf("image %d: %w", i+1, err)
			}
			if len(raw) > maxImageBytes {
				return nil, fmt.Errorf("image %d is too large (%d bytes, max %d)", i+1, len(raw), maxImageBytes)
			}
			if mediaType == "" {
				mediaType = detectImageType(in.Path, raw)
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

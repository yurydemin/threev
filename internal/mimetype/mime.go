package mimetype

import (
	"path/filepath"
	"strings"
)

// DefaultContentType is returned by ContentTypeForKey for extensions absent
// from ContentTypeByExtension, or when key has no extension at all.
const DefaultContentType = "application/octet-stream"

// ContentTypeByExtension maps a lower-cased file extension (without the
// leading dot) to a MIME type. This is a deliberately self-maintained,
// static table rather than the standard library's mime.TypeByExtension:
// that function consults OS-specific mime.types files/registry entries and
// so returns different results across Windows/macOS/Linux - unacceptable
// for a cross-platform desktop client where the same object must render an
// identical MIME type (and therefore preview/upload Content-Type behavior,
// FR-FM-003/007, FR-TR-001) regardless of which OS the app happens to be
// running on.
var ContentTypeByExtension = map[string]string{
	// Images
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"gif":  "image/gif",
	"webp": "image/webp",
	"svg":  "image/svg+xml",
	"bmp":  "image/bmp",
	"ico":  "image/x-icon",

	// Text / code
	"txt":  "text/plain",
	"md":   "text/markdown",
	"csv":  "text/csv",
	"json": "application/json",
	"xml":  "application/xml",
	"yaml": "application/yaml",
	"yml":  "application/yaml",
	"log":  "text/plain",
	"js":   "text/javascript",
	"ts":   "text/typescript",
	"jsx":  "text/jsx",
	"tsx":  "text/tsx",
	"css":  "text/css",
	"html": "text/html",
	"go":   "text/x-go",
	"py":   "text/x-python",
	"java": "text/x-java-source",
	"sh":   "application/x-sh",
	"sql":  "application/sql",

	// Documents
	"pdf":  "application/pdf",
	"doc":  "application/msword",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xls":  "application/vnd.ms-excel",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"ppt":  "application/vnd.ms-powerpoint",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",

	// Archives
	"zip": "application/zip",
	"tar": "application/x-tar",
	"gz":  "application/gzip",
	"rar": "application/vnd.rar",
	"7z":  "application/x-7z-compressed",

	// Audio / video
	"mp3":  "audio/mpeg",
	"wav":  "audio/wav",
	"mp4":  "video/mp4",
	"mov":  "video/quicktime",
	"avi":  "video/x-msvideo",
	"webm": "video/webm",
}

// ContentTypeForKey returns a MIME type for key based solely on its file
// extension (see ContentTypeByExtension), falling back to
// DefaultContentType when the extension is missing or unrecognized.
func ContentTypeForKey(key string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(key), "."))

	if ct, ok := ContentTypeByExtension[ext]; ok {
		return ct
	}

	return DefaultContentType
}

package mimetype

import "testing"

func TestContentTypeForKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"jpg", "photo.jpg", "image/jpeg"},
		{"png", "image.png", "image/png"},
		{"pdf", "document.pdf", "application/pdf"},
		{"json", "data.json", "application/json"},
		{"txt", "notes.txt", "text/plain"},
		{"zip", "archive.zip", "application/zip"},
		{"unknown extension", "file.xyz123", DefaultContentType},
		{"no extension", "README", DefaultContentType},
		{"uppercase extension", "IMAGE.JPG", "image/jpeg"},
		{"mixed case extension", "Photo.JpEg", "image/jpeg"},
		{"nested path resolves by last extension", "folder/sub/file.png", "image/png"},
		{"nested path unknown extension", "folder/sub/file.unknownext", DefaultContentType},
		{"trailing dot with no extension text", "folder/", DefaultContentType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ContentTypeForKey(tt.key); got != tt.want {
				t.Errorf("ContentTypeForKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

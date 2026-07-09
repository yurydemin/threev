package filemanager

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/domain"
)

// textPreviewLimitBytes is the maximum number of bytes of an object's
// content ever returned by GetTextPreview, matching FR-FM-007 ("текстовые
// файлы: отображение первых 100 КБ").
const textPreviewLimitBytes = 100 * 1024

// GetTextPreview returns up to the first 100 KB of bucket/key's content for
// the profile identified by profileID (FR-FM-007). Unlike image/PDF preview
// (handled entirely by the frontend opening a presigned URL directly, per
// the Stage 2 plan), a presigned URL cannot be safely size-limited, so text
// preview goes through this dedicated backend method instead: it first
// HeadObjects to learn the object's total size, then GetObjects only the
// leading slice it needs (a byte-range request when the object exceeds the
// limit, avoiding downloading the rest of a possibly huge file just to show
// a short excerpt).
func (f *FileManagerService) GetTextPreview(profileID int64, bucket, key string) (domain.TextPreviewResult, error) {
	meta, err := f.HeadObject(profileID, bucket, key)
	if err != nil {
		return domain.TextPreviewResult{}, err
	}

	client, err := f.resolveClient(profileID)
	if err != nil {
		return domain.TextPreviewResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	truncated := meta.Size > textPreviewLimitBytes
	if truncated {
		input.Range = aws.String(fmt.Sprintf("bytes=0-%d", textPreviewLimitBytes-1))
	}

	out, err := client.GetObject(ctx, input)
	if err != nil {
		return domain.TextPreviewResult{}, classifyOperationError("get text preview", err)
	}
	defer out.Body.Close()

	// Defensive: some S3-compatible servers do not strictly honor the
	// Range header (RFC 7233 support varies across implementations) and
	// may return the object's full body with a 200 instead of a partial
	// 206 response. io.LimitReader guarantees Content never exceeds
	// textPreviewLimitBytes regardless of how much the server actually
	// sent, rather than trusting Range to have been respected.
	content, err := io.ReadAll(io.LimitReader(out.Body, textPreviewLimitBytes))
	if err != nil {
		return domain.TextPreviewResult{}, classifyOperationError("get text preview", err)
	}

	return domain.TextPreviewResult{
		Content:   string(content),
		Truncated: truncated,
		TotalSize: meta.Size,
	}, nil
}

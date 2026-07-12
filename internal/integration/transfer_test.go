//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"threev/internal/domain"
)

// roundTripFileSize is deliberately well above transfer.singlePutThreshold
// (5MB): TransferService.QueueUpload only ever exercises the multipart
// upload path (internal/transfer/multipart_upload.go) once TotalBytes
// exceeds that threshold, and this test's entire point is to exercise that
// path against a real S3 protocol server rather than an httptest mock (see
// this file's own doc comment) - 20MB comfortably clears the threshold
// while staying fast enough for a routine local/CI run (docs/02-tech-spec.md
// AC-003's full 5GB scenario remains a manual regression, per the Этап 5
// plan's constraints).
const roundTripFileSize = 20 * 1024 * 1024

// transferCompletionTimeout bounds how long TestIntegrationUploadDownloadRoundTrip
// waits for each of the upload/download tasks it queues to reach
// transfer_history - generous for a 20MB transfer against a local MinIO
// instance, even under CI load.
const transferCompletionTimeout = 60 * time.Second

// TestIntegrationUploadDownloadRoundTrip is a smaller-scale, automated
// regression for AC-003 (docs/02-tech-spec.md section 13): a ~20MB local
// file is uploaded through TransferService.QueueUpload (real multipart
// path), then downloaded back through TransferService.QueueDownload (real
// download/range path), against a real MinIO instance - not an httptest
// mock. The downloaded file's content is compared byte-for-byte against the
// original.
func TestIntegrationUploadDownloadRoundTrip(t *testing.T) {
	svc := newIntegrationServices(t)
	bucket := newTestBucket(t, svc.profile)

	content := make([]byte, roundTripFileSize)
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("rand.Read() returned error: %v", err)
	}

	srcPath := filepath.Join(t.TempDir(), "roundtrip-source.bin")
	if err := os.WriteFile(srcPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", srcPath, err)
	}

	const key = "roundtrip/large-file.bin"

	uploadID, err := svc.tr.QueueUpload(domain.UploadRequest{
		ProfileID: svc.profileID,
		Bucket:    bucket,
		Key:       key,
		LocalPath: srcPath,
	})
	if err != nil {
		t.Fatalf("QueueUpload() returned error: %v", err)
	}

	uploadEntry := waitForTransferCompletion(t, svc.tr, uploadID, transferCompletionTimeout)
	if uploadEntry.TotalBytes != roundTripFileSize {
		t.Errorf("upload history entry TotalBytes = %d, want %d", uploadEntry.TotalBytes, roundTripFileSize)
	}

	dstPath := filepath.Join(t.TempDir(), "roundtrip-downloaded.bin")

	downloadID, err := svc.tr.QueueDownload(domain.DownloadRequest{
		ProfileID: svc.profileID,
		Bucket:    bucket,
		Key:       key,
		LocalPath: dstPath,
	})
	if err != nil {
		t.Fatalf("QueueDownload() returned error: %v", err)
	}

	waitForTransferCompletion(t, svc.tr, downloadID, transferCompletionTimeout)

	downloaded, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) returned error: %v", dstPath, err)
	}

	if len(downloaded) != len(content) {
		t.Fatalf("downloaded file size = %d, want %d", len(downloaded), len(content))
	}

	if !bytes.Equal(content, downloaded) {
		t.Fatal("downloaded file content does not match the originally uploaded content")
	}
}

package transfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/s3client"
)

// DownloadHooks lets the caller (the future task.go, Stage 3 Block F, same
// as UploadHooks) observe download progress as Download/downloadRange run,
// without this package depending on any concrete persistence/progress type
// - mirroring UploadHooks exactly, minus the multipart-upload-only
// OnMultipartUploadIDAssigned hook (a range download has no server-side
// resource analogous to an UploadId to persist: resume is driven entirely
// by the local, client-side resume-progress sidecar file downloadRange
// itself maintains next to LocalPath - see progressSidecarSuffix's doc
// comment in range_download.go for why a range GET, unlike a multipart
// upload, has no server-side "already-transferred" source of truth
// equivalent to ListParts).
type DownloadHooks struct {
	// OnBytesTransferred is called incrementally as bytes are read off a
	// segment's Range GET response body and written to the local file
	// (downloadSegment), reflecting bytes actually durably WriteAt-ten
	// rather than only bytes S3 has sent. It may be called many times per
	// segment (once per underlying Read), and - since segments run
	// concurrently - concurrently from multiple worker goroutines at
	// once. Callers must make it safe for concurrent use themselves
	// (e.g. via atomic.Int64.Add), exactly as UploadHooks.
	// OnBytesTransferred documents. May be nil, in which case no
	// progress reporting happens.
	OnBytesTransferred func(delta int64)
}

// DownloadParams is the input to Download.
type DownloadParams struct {
	// Pooled and Fresh are the two long-lived *s3.Client instances for
	// the profile this download runs against (s3client.ConnectionManager.
	// Get(profileID)), resolved by the caller - Download/downloadRange
	// never resolve a profile or build a client themselves. Pooled is
	// used on a request's first attempt; Fresh is used on every retry
	// (docs/02-tech-spec.md section 10.4), selected inside each
	// s3client.WithRetry attempt closure via its isRetry parameter -
	// identical to UploadParams.
	Pooled, Fresh *s3.Client
	// Breaker is the shared, per-process circuit breaker checked/updated
	// by every s3client.WithRetry call this download makes.
	Breaker *s3client.CircuitBreaker
	// RetryPolicies is the shared, per-process retry/timeout configuration
	// store every s3client.WithRetry/s3client.AdaptiveTimeout call this
	// download makes reads from (headObject's HeadObject call,
	// downloadSegment's Range GetObject calls), resolved by the caller -
	// see s3client.RetryPolicyStore's own doc comment.
	RetryPolicies *s3client.RetryPolicyStore
	// Host is the bare hostname (e.g. url.Parse(profile.EndpointURL).
	// Hostname()) Breaker tracks state for, resolved by the caller.
	Host string
	// Limiter, if non-nil, paces every response body read (downloadSegment)
	// against its download-direction token bucket
	// (BandwidthLimiter.WrapDownloadReader) - identical semantics to
	// UploadParams.Limiter, see its doc comment.
	Limiter *BandwidthLimiter

	Bucket, Key string
	// LocalPath is the local filesystem path Download saves the object
	// to - already a full path, including file name. Building this path
	// from the S3 key (sanitizing path separators, resolving a
	// destination directory, ...) is the responsibility of the caller
	// (the future download dispatcher in Stage 3 Block G/TransferService),
	// not this package: Download only ever reads/writes exactly
	// LocalPath as given.
	LocalPath string

	// PartSizeOverride, when > 0, is used verbatim as the range-download
	// segment size instead of PartSize(totalBytes)'s adaptive table (Этап 4
	// суб-этап 4.3, TransferService.SetPartSizeOverrideMB - see
	// planDownloadSegments' use of this field). 0 (the zero value) means
	// "use the adaptive table" - identical semantics to
	// UploadParams.PartSizeOverride.
	PartSizeOverride int64

	// Concurrency is the number of segments transferred in parallel. 0
	// means DefaultPartConcurrency; any value is clamped to
	// MaxPartConcurrency and additionally to the download's actual
	// segment count - identical semantics to UploadParams.Concurrency
	// (see effectiveConcurrency, shared by both).
	Concurrency int

	Hooks DownloadHooks
}

// Download fetches DownloadParams.Bucket/Key into DownloadParams.LocalPath:
//
//  1. HeadObject to learn the object's size (Content-Length) and ETag
//     (docs/02-tech-spec.md section 10.3, step 1), under
//     s3client.MetadataRetryPolicy - the same policy
//     ensureMultipartUploadID/listCompletedParts use for their own
//     metadata-only calls.
//  2. ensure LocalPath's parent directory exists (os.MkdirAll) -
//     downloadRange itself assumes LocalPath is already usable and does
//     not create directories.
//  3. downloadRange: the actual parallel Range GET worker pool, segment
//     planning, and resume logic (range_download.go).
//  4. a best-effort integrity check (verifyDownloadIntegrity, FR-TR-004) of
//     the fully-downloaded file against the HEAD-reported ETag. Like
//     upload.go's own verification calls, a plain-ETag mismatch or an
//     unrecognized ETag format is NOT treated as fatal here (mirroring
//     uploadSingle/completeMultipartUpload's documented MVP behavior:
//     neither result is currently acted on or logged, since this package
//     has no logging/telemetry plumbing yet). The one exception - and the
//     one place Download's behavior differs from Upload's - is a
//     multipart-source ETag whose local file size does not match the
//     HEAD-reported size: verifyDownloadIntegrity returns that specific
//     case as a non-nil error (see its doc comment for why), and Download
//     propagates it as a genuine failure rather than swallowing it, since
//     an incomplete file on disk after downloadRange reported success
//     indicates a real bug, not merely "verification not applicable".
//
// Download returns the object's ETag (quotes stripped) as reported by
// HeadObject.
func Download(ctx context.Context, p DownloadParams) (etag string, err error) {
	totalBytes, etag, err := headObject(ctx, p)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(p.LocalPath), 0o755); err != nil { //nolint:gosec // local download destination directory, not attacker-controlled beyond what the caller already resolved
		return "", fmt.Errorf("create directory for %s: %w", p.LocalPath, err)
	}

	if err := downloadRange(ctx, p, totalBytes); err != nil {
		return "", err
	}

	// Best-effort integrity check (FR-TR-004), see doc comment above for
	// why the multipart-source-with-mismatched-size case specifically is
	// propagated as a fatal error while every other verifyDownloadIntegrity
	// outcome is not.
	if _, verifyErr := verifyDownloadIntegrity(p.LocalPath, etag, totalBytes); verifyErr != nil {
		return "", fmt.Errorf("verify downloaded file: %w", verifyErr)
	}

	return etag, nil
}

// headObject runs HeadObject under s3client.MetadataRetryPolicy and returns
// the object's Content-Length and ETag (quotes stripped).
func headObject(ctx context.Context, p DownloadParams) (totalBytes int64, etag string, err error) {
	err = s3client.WithRetry(ctx, p.Breaker, p.RetryPolicies.Metadata(), p.Host, func(attemptCtx context.Context, isRetry bool) error {
		client := p.Pooled
		if isRetry {
			client = p.Fresh
		}

		out, headErr := client.HeadObject(attemptCtx, &s3.HeadObjectInput{
			Bucket: aws.String(p.Bucket),
			Key:    aws.String(p.Key),
		})
		if headErr != nil {
			return headErr
		}

		if out.ContentLength == nil {
			return errors.New("S3 did not return a Content-Length from HeadObject")
		}

		if out.ETag == nil {
			return errors.New("S3 did not return an ETag from HeadObject")
		}

		totalBytes = *out.ContentLength
		etag = strings.Trim(*out.ETag, `"`)

		return nil
	})
	if err != nil {
		return 0, "", fmt.Errorf("head object: %w", err)
	}

	return totalBytes, etag, nil
}

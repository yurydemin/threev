package transfer

import (
	"context"
	"crypto/md5" //nolint:gosec // see etag.go's package-level rationale: MD5 is used only for S3 ETag-format comparison (FR-TR-004), not for any security purpose.
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"threev/internal/s3client"
)

// DefaultPartConcurrency is the number of multipart-upload parts (or
// range-download segments, when download.go/range_download.go land in the
// next Stage 3 block) transferred in parallel when a caller does not
// specify UploadParams.Concurrency (FR-TR-001: "Конкурентность: ...
// default: 4, max: 32").
const DefaultPartConcurrency = 4

// MaxPartConcurrency is the hard ceiling on UploadParams.Concurrency,
// whatever value a caller requests (FR-TR-001).
const MaxPartConcurrency = 32

// singlePutThreshold is the file-size boundary at/under which Upload sends
// a single PutObject instead of a multipart upload (FR-TR-001:
// "Автоматический multipart upload для файлов > 5 МБ" - so a file of
// exactly 5MB still takes the plain PutObject path, matching the "> 5MB"
// wording literally: only strictly larger files go multipart).
const singlePutThreshold = 5 * 1024 * 1024 // 5 MB

// UploadHooks lets the caller (the future task.go, Stage 3 Block F) persist
// upload progress/state as Upload/uploadMultipart runs, without upload.go
// depending on *storage.TransferQueueRepository (or any other concrete
// persistence type) directly - keeping this package's only dependency
// surface s3client and the local filesystem.
type UploadHooks struct {
	// OnMultipartUploadIDAssigned is called exactly ONCE, synchronously,
	// immediately after a successful CreateMultipartUpload - never for
	// the single-PutObject path (files <= 5MB), since there is no
	// UploadId to persist there. The caller is expected to durably
	// persist uploadID before returning, since it is the sole means of
	// discovering an in-progress multipart upload via ListParts if the
	// process crashes before CompleteMultipartUpload runs. A non-nil
	// return from this hook is treated as fatal by uploadMultipart (see
	// its doc comment for the reasoning): continuing an upload whose
	// UploadId cannot be resumed on crash is considered worse than
	// failing the upload attempt outright while the caller still has
	// enough context to decide what to do next (retry persisting, abort
	// the now-orphaned server-side MPU via AbortMultipartUpload, surface
	// an error to the user).
	OnMultipartUploadIDAssigned func(uploadID string) error

	// OnBytesTransferred is called incrementally as bytes are read off
	// disk to be sent as part of an HTTP request body (PutObject or
	// UploadPart), reflecting bytes handed to the transport layer rather
	// than bytes S3 has acknowledged. It may be called many times per
	// part (once per underlying Read), and - for a multipart upload -
	// concurrently from multiple worker goroutines at once (one per part
	// in flight). Callers must make it safe for concurrent use
	// themselves, e.g. via atomic.Int64.Add, as the future progress.go
	// (Stage 3 Block E) will do. May be nil, in which case no progress
	// reporting happens.
	OnBytesTransferred func(delta int64)
}

// UploadParams is the input to Upload.
type UploadParams struct {
	// Pooled and Fresh are the two long-lived *s3.Client instances for
	// the profile this upload runs against (s3client.ConnectionManager.
	// Get(profileID)), resolved by the caller - Upload/uploadMultipart
	// never resolve a profile or build a client themselves. Pooled is
	// used on a request's first attempt; Fresh is used on every retry
	// (docs/02-tech-spec.md section 10.4), selected inside each
	// s3client.WithRetry attempt closure via its isRetry parameter.
	Pooled, Fresh *s3.Client
	// Breaker is the shared, per-process circuit breaker checked/updated
	// by every s3client.WithRetry call this upload makes.
	Breaker *s3client.CircuitBreaker
	// RetryPolicies is the shared, per-process retry/timeout configuration
	// store every s3client.WithRetry/s3client.AdaptiveTimeout call this
	// upload makes reads from (uploadSingle/uploadMultipart's own
	// CreateMultipartUpload/ListParts/UploadPart/CompleteMultipartUpload
	// calls), resolved by the caller - see s3client.RetryPolicyStore's own
	// doc comment.
	RetryPolicies *s3client.RetryPolicyStore
	// Host is the bare hostname (e.g. url.Parse(profile.EndpointURL).
	// Hostname()) Breaker tracks state for, resolved by the caller.
	Host string
	// Limiter, if non-nil, paces every request body read
	// (uploadSingle/uploadPart) against its upload-direction token bucket
	// (BandwidthLimiter.WrapUploadReader). A nil Limiter - the zero value
	// of this field, and what every caller gets unless it explicitly
	// wires one in (docs/02-tech-spec.md section 10.6: "По умолчанию
	// лимит выключен") - means unlimited, exactly as WrapUploadReader's
	// own nil-receiver handling documents.
	Limiter *BandwidthLimiter

	Bucket, Key string
	// LocalPath is the local filesystem path of the file to upload.
	LocalPath string
	// ContentType is the already-resolved Content-Type header value
	// (mimetype.ContentTypeForKey(Key), typically) - Upload does not
	// determine MIME type itself.
	ContentType string
	// TotalBytes is the local file's size, used both to pick the
	// single-PutObject vs multipart path and to size/count parts.
	TotalBytes int64

	// PartSizeOverride, when > 0, is used verbatim as the multipart part
	// size instead of PartSize(TotalBytes)'s adaptive table (Этап 4
	// суб-этап 4.3, TransferService.SetPartSizeOverrideMB - see
	// uploadMultipart's use of this field). 0 (the zero value, and what
	// every caller gets unless it explicitly sets one) means "use the
	// adaptive table", identical in spirit to Limiter's nil-means-unlimited
	// convention above.
	PartSizeOverride int64

	// ExistingUploadID, when non-empty, resumes a previously created
	// multipart upload (its S3 UploadId) instead of starting a new one:
	// uploadMultipart skips CreateMultipartUpload and instead calls
	// ListParts to discover which parts are already uploaded (FR-TR-003).
	// Left empty for a new upload; never meaningful for the
	// single-PutObject path (files <= 5MB have no multipart upload to
	// resume).
	ExistingUploadID string

	// Concurrency is the number of parts transferred in parallel. 0
	// means DefaultPartConcurrency; any value is clamped to
	// MaxPartConcurrency and additionally to the file's actual part
	// count (no point starting more workers than there are parts).
	Concurrency int

	Hooks UploadHooks
}

// Upload uploads UploadParams.LocalPath to UploadParams.Bucket/Key: a
// single PutObject if TotalBytes <= 5MB (FR-TR-001, singlePutThreshold),
// otherwise a multipart upload (uploadMultipart, multipart_upload.go). It
// returns the final, S3-reported ETag (quotes stripped) - verified against
// a locally computed digest where the ETag format allows it (FR-TR-004),
// though a verification mismatch or an inapplicable ETag format is not
// treated as a fatal error in this MVP (see verifySingleETag/
// verifyMultipartETag's doc comments).
func Upload(ctx context.Context, p UploadParams) (etag string, err error) {
	if p.TotalBytes <= singlePutThreshold {
		return uploadSingle(ctx, p)
	}

	return uploadMultipart(ctx, p)
}

// uploadSingle sends the entire file at p.LocalPath as one PutObject call,
// under s3client.PartRetryPolicy (the same policy a multipart part uses,
// since a whole small file is still a network transfer worth the same
// number of retry attempts as a single part - a metadata call's 3 attempts
// would be too few for what can still be several megabytes of data).
func uploadSingle(ctx context.Context, p UploadParams) (string, error) {
	var finalETag string

	err := s3client.WithRetry(ctx, p.Breaker, p.RetryPolicies.Part(), p.Host, func(attemptCtx context.Context, isRetry bool) error {
		client := p.Pooled
		if isRetry {
			client = p.Fresh
		}

		// A fresh, unread *os.File (and therefore a fresh reader chain
		// wrapping it) is opened on every attempt - required for retries
		// exactly as multipart_upload.go's io.NewSectionReader-per-attempt
		// is: an *os.File whose previous attempt already advanced its
		// read offset (or whose body reader errored out mid-read) cannot
		// simply be reused for a retry.
		file, err := os.Open(p.LocalPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", p.LocalPath, err)
		}
		defer func() { _ = file.Close() }()

		hasher := md5.New() //nolint:gosec // see package-level rationale above

		timeoutCtx, cancel := context.WithTimeout(attemptCtx, s3client.AdaptiveTimeout(p.TotalBytes, 0, p.RetryPolicies.TimeoutFloor()))
		defer cancel()

		var body io.Reader = &countingReader{r: io.TeeReader(file, hasher), onRead: p.Hooks.OnBytesTransferred}
		body = p.Limiter.WrapUploadReader(timeoutCtx, body)

		out, err := client.PutObject(timeoutCtx, &s3.PutObjectInput{
			Bucket:        aws.String(p.Bucket),
			Key:           aws.String(p.Key),
			Body:          body,
			ContentLength: aws.Int64(p.TotalBytes),
			ContentType:   aws.String(p.ContentType),
		}, unsignedPayload)
		if err != nil {
			return err
		}

		if out.ETag == nil {
			return errors.New("S3 did not return an ETag from PutObject")
		}

		finalETag = strings.Trim(*out.ETag, `"`)

		// Best-effort integrity check (FR-TR-004). Deliberately not
		// fatal: a mismatch, or an ETag format verifySingleETag cannot
		// recognize (SSE-KMS/non-standard providers), does not fail an
		// otherwise HTTP-200-successful upload in this MVP - see
		// verifySingleETag's doc comment. A full verification-failure
		// recovery policy (re-upload, surface a distinct warning to the
		// user, ...) is left as future work; this result is currently
		// discarded rather than acted on or logged anywhere, since
		// there is no logging/telemetry plumbing in place yet for this
		// package.
		var sum [md5.Size]byte
		copy(sum[:], hasher.Sum(nil))
		_ = verifySingleETag(finalETag, sum)

		return nil
	})

	return finalETag, err
}

// unsignedPayload is passed as a per-call functional option to
// client.PutObject/client.UploadPart, swapping out the SDK's default SigV4
// content-SHA256 payload-hashing middleware
// (aws-sdk-go-v2/aws/signer/v4.ComputePayloadSHA256) for one that always
// uses the UNSIGNED-PAYLOAD placeholder instead of a real hash.
//
// Without this, ComputePayloadSHA256's HandleFinalize reads the entire
// request body once via io.Copy(hash, stream) to compute its SHA256, then
// calls stream.(io.Seeker).Seek(0, io.SeekStart) to rewind it before the
// body is read a second time to actually transmit it. That has two
// consequences for this package's streaming, countingReader-wrapped
// bodies (io.NewSectionReader/os.File, chained through io.TeeReader for
// the single-PutObject MD5 pass): it requires Body to implement
// io.Seeker, which the chain does not; and even where the underlying
// reader happens to be seekable, it would read every byte of the
// part/file twice, silently doubling UploadHooks.OnBytesTransferred's
// reported progress. aws-sdk-go-v2 normally avoids this entirely for
// HTTPS S3 endpoints via v4.UseDynamicPayloadSigningMiddleware (which
// prefers UNSIGNED-PAYLOAD whenever TLS is already providing transport
// integrity) - but that dynamic behavior does NOT kick in for a plain
// HTTP endpoint, a realistic self-hosted MinIO configuration for this
// product (docs/02-tech-spec.md's own "flaky network" scenario).
// Explicitly forcing UNSIGNED-PAYLOAD here, scoped to just these two
// streaming-body calls, gets the same single-pass-read behavior
// unconditionally, regardless of the endpoint's TLS state. SigV4's
// payload hash is an extra request-integrity layer S3 does not require
// for correctness - this application's own integrity check is FR-TR-004's
// ETag/composite-ETag verification (etag.go), not the SigV4 signature.
func unsignedPayload(o *s3.Options) {
	o.APIOptions = append(o.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
}

// countingReader wraps an io.Reader, invoking onRead (if non-nil) with the
// number of bytes returned by each successful Read call, before returning
// control to the caller. It is used to drive UploadHooks.OnBytesTransferred
// as PutObject/UploadPart streams a request body, so progress reflects
// bytes actually handed to the HTTP transport rather than only bytes S3
// has acknowledged by the time the whole call returns.
//
// One accepted MVP limitation, documented here rather than worked around:
// if an attempt fails partway through (e.g. a network error mid-Read) and
// is retried, onRead will already have been called for the bytes the
// failed attempt managed to read before a fresh countingReader re-reads
// them on retry - so cumulative reported progress can transiently exceed
// the "durably sent" byte count on a flaky link. A precise bytes-sent vs
// bytes-confirmed distinction would require more than progress.go (a later
// Stage 3 step) currently plans to track, and is not addressed here.
type countingReader struct {
	r      io.Reader
	onRead func(delta int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(int64(n))
	}

	return n, err
}

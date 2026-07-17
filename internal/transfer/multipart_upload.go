package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"golang.org/x/sync/errgroup"

	"threev/internal/s3client"
)

// uploadMultipart runs the full multipart-upload algorithm
// (docs/02-tech-spec.md section 10.2) for p.LocalPath -> p.Bucket/p.Key,
// called by Upload once p.TotalBytes exceeds singlePutThreshold:
//
//  1. get an UploadId - either reuse p.ExistingUploadID (resume) or create
//     a new one (ensureMultipartUploadID);
//  2. discover already-uploaded parts via ListParts when resuming
//     (listCompletedParts), so they are not re-transferred;
//  3. upload every remaining part in parallel, bounded by an errgroup
//     worker pool (effectiveConcurrency);
//  4. CompleteMultipartUpload with the full, ordered part list
//     (completeMultipartUpload), which also performs the best-effort
//     composite-ETag verification (FR-TR-004).
//
// It deliberately never calls AbortMultipartUpload itself on error or
// context cancellation - per the Этап 3 plan's architecture decision, an
// abort is only ever issued by the caller (the future task.go) in response
// to an explicit user Cancel, never as an automatic side effect of a
// failed/paused attempt here: a paused or crashed upload must leave its
// server-side MPU alive so a later Resume (via ExistingUploadID) can find
// it through ListParts.
func uploadMultipart(ctx context.Context, p UploadParams) (etag string, err error) {
	uploadID, err := ensureMultipartUploadID(ctx, p)
	if err != nil {
		return "", err
	}

	partETags, err := listCompletedParts(ctx, p, uploadID)
	if err != nil {
		return "", fmt.Errorf("list existing parts (resume): %w", err)
	}

	partSize := p.PartSizeOverride
	if partSize <= 0 {
		partSize = PartSize(p.TotalBytes)
	}

	partCount := PartCount(p.TotalBytes, partSize)

	file, err := os.Open(p.LocalPath)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", p.LocalPath, err)
	}
	defer func() { _ = file.Close() }()

	var mu sync.Mutex // guards partETags for the duration of the worker pool below

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(effectiveConcurrency(p.Concurrency, partCount))

	for n := int64(1); n <= partCount; n++ {
		// partCount is bounded by PartSize's 10000-part clamp
		// (maxPartsPerUpload) applied when partSize was computed above,
		// so n always fits comfortably in an int32 - S3's PartNumber
		// field type, and what UploadPartInput.PartNumber requires.
		partNumber := int32(n) //nolint:gosec // see comment above

		// Guarded by mu even though this loop's own goroutine is the only
		// writer of NEW entries into partETags so far (via listCompletedParts,
		// finished before this loop starts) - a previously group.Go'd
		// goroutine may already be running concurrently with this very
		// iteration (group.SetLimit bounds how many run at once, but Go()
		// itself only blocks once that limit is reached, not on every call),
		// and that goroutine's own `partETags[partNumber] = partETag` write
		// below is otherwise a plain, unsynchronized-with-this-read Go map
		// access - a genuine data race (on the map's internal structure, not
		// merely "the same key"), caught by `go test -race` once enough
		// parts/concurrency make the timing window likely enough to hit.
		mu.Lock()
		_, alreadyUploaded := partETags[partNumber]
		mu.Unlock()

		if alreadyUploaded {
			continue
		}

		offset := (n - 1) * partSize
		size := partSize
		if remaining := p.TotalBytes - offset; remaining < size {
			size = remaining
		}

		group.Go(func() error {
			partETag, uploadErr := uploadPart(groupCtx, p, uploadID, partNumber, offset, size, file)
			if uploadErr != nil {
				return fmt.Errorf("part %d: %w", partNumber, uploadErr)
			}

			mu.Lock()
			partETags[partNumber] = partETag
			mu.Unlock()

			return nil
		})
	}

	// errgroup.WithContext cancels groupCtx as soon as any part's
	// closure returns a non-nil error, so the remaining in-flight/queued
	// parts stop (their s3client.WithRetry attempts see ctx.Done() and
	// return context.Canceled) rather than continuing to burn bandwidth
	// on a part upload that is already a lost cause.
	if err := group.Wait(); err != nil {
		return "", err
	}

	return completeMultipartUpload(ctx, p, uploadID, partETags)
}

// effectiveConcurrency resolves UploadParams.Concurrency into the actual
// number of worker goroutines uploadMultipart's errgroup runs at once:
// configured (or DefaultPartConcurrency if <= 0), clamped to
// MaxPartConcurrency, and additionally clamped to partCount - there is no
// benefit (and errgroup.Group.SetLimit would simply leave idle permits) to
// running more workers than there are parts to upload. The result is
// always >= 1: errgroup.Group.SetLimit(0) would block every future Go call
// forever (a limit of zero means "no new goroutines allowed", not "no
// limit"), which would matter here since partCount could in principle be 0
// on a fully-resumed upload where every part was already completed.
func effectiveConcurrency(configured int, partCount int64) int {
	concurrency := configured
	if concurrency <= 0 {
		concurrency = DefaultPartConcurrency
	}

	if concurrency > MaxPartConcurrency {
		concurrency = MaxPartConcurrency
	}

	if partCount > 0 && int64(concurrency) > partCount {
		concurrency = int(partCount)
	}

	if concurrency < 1 {
		concurrency = 1
	}

	return concurrency
}

// ensureMultipartUploadID returns the UploadId to use for this upload:
// p.ExistingUploadID as-is when resuming, or a freshly created one via
// CreateMultipartUpload otherwise. For a newly created UploadId,
// p.Hooks.OnMultipartUploadIDAssigned (if set) is invoked synchronously
// before returning.
//
// A failure from that hook is treated as fatal to the whole upload
// attempt (returned as an error, aborting uploadMultipart before any part
// is transferred): without durably persisting the UploadId, a crash any
// time before CompleteMultipartUpload would leave no way to discover this
// MPU again for Resume via ListParts, and continuing to upload parts into
// an unrecoverable-on-crash MPU risks silently wasting the bandwidth spent
// on them. This does leave the just-created server-side MPU itself
// orphaned (this function does not call AbortMultipartUpload - see
// uploadMultipart's doc comment on why abort is never automatic here) -
// already a documented, accepted piece of tech debt for the "persist
// UploadId" step not being atomic with CreateMultipartUpload (see the
// Этап 3 plan's "Известные риски" section).
func ensureMultipartUploadID(ctx context.Context, p UploadParams) (string, error) {
	if p.ExistingUploadID != "" {
		return p.ExistingUploadID, nil
	}

	var uploadID string

	err := s3client.WithRetry(ctx, p.Breaker, p.RetryPolicies.Metadata(), p.Host, func(attemptCtx context.Context, isRetry bool) error {
		client := p.Pooled
		if isRetry {
			client = p.Fresh
		}

		out, err := client.CreateMultipartUpload(attemptCtx, &s3.CreateMultipartUploadInput{
			Bucket:      aws.String(p.Bucket),
			Key:         aws.String(p.Key),
			ContentType: aws.String(p.ContentType),
		})
		if err != nil {
			return err
		}

		if out.UploadId == nil {
			return errors.New("S3 did not return an UploadId from CreateMultipartUpload")
		}

		uploadID = *out.UploadId

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload: %w", err)
	}

	if p.Hooks.OnMultipartUploadIDAssigned != nil {
		if hookErr := p.Hooks.OnMultipartUploadIDAssigned(uploadID); hookErr != nil {
			return "", fmt.Errorf("create multipart upload: persist upload id %s: %w", uploadID, hookErr)
		}
	}

	return uploadID, nil
}

// listCompletedParts returns the part number -> ETag (quotes stripped) map
// of parts already stored server-side for uploadID, by paginating
// ListParts until IsTruncated is false. It returns an empty (non-nil) map
// without making any request when p.ExistingUploadID is empty (a new
// upload has no existing parts to discover) - callers can safely index or
// range over the result either way.
func listCompletedParts(ctx context.Context, p UploadParams, uploadID string) (map[int32]string, error) {
	completed := make(map[int32]string)

	if p.ExistingUploadID == "" {
		return completed, nil
	}

	var partNumberMarker *string

	for {
		var page *s3.ListPartsOutput

		err := s3client.WithRetry(ctx, p.Breaker, p.RetryPolicies.Metadata(), p.Host, func(attemptCtx context.Context, isRetry bool) error {
			client := p.Pooled
			if isRetry {
				client = p.Fresh
			}

			resp, err := client.ListParts(attemptCtx, &s3.ListPartsInput{
				Bucket:           aws.String(p.Bucket),
				Key:              aws.String(p.Key),
				UploadId:         aws.String(uploadID),
				PartNumberMarker: partNumberMarker,
			})
			if err != nil {
				return err
			}

			page = resp

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("list parts: %w", err)
		}

		for _, part := range page.Parts {
			if part.PartNumber == nil || part.ETag == nil {
				continue
			}

			completed[*part.PartNumber] = strings.Trim(*part.ETag, `"`)
		}

		if page.IsTruncated == nil || !*page.IsTruncated {
			return completed, nil
		}

		partNumberMarker = page.NextPartNumberMarker
	}
}

// uploadPart uploads a single part (bytes [offset, offset+size) of
// p.LocalPath, opened as file by the caller) as PartNumber partNumber of
// uploadID, under s3client.PartRetryPolicy, and returns its ETag (quotes
// stripped).
//
// Each retry attempt gets a brand-new io.NewSectionReader(file, offset,
// size), created INSIDE the s3client.WithRetry attempt closure rather than
// once outside the retry loop: a *io.SectionReader that a failed prior
// attempt already partially read (e.g. the HTTP request body was read
// partway through before the connection dropped) cannot simply be resumed
// or reused - its internal offset would no longer point at the start of
// the part, silently uploading a corrupt/truncated body on retry. Since
// *io.SectionReader is otherwise stateless and cheap to construct, always
// building a fresh one per attempt is the simplest correct approach (see
// the Stage 3 network-engineer review of the earlier retry.go/manager.go
// steps, which flagged exactly this class of bug).
func uploadPart(ctx context.Context, p UploadParams, uploadID string, partNumber int32, offset, size int64, file *os.File) (string, error) {
	var partETag string

	err := s3client.WithRetry(ctx, p.Breaker, p.RetryPolicies.Part(), p.Host, func(attemptCtx context.Context, isRetry bool) error {
		client := p.Pooled
		if isRetry {
			client = p.Fresh
		}

		timeoutCtx, cancel := context.WithTimeout(attemptCtx, s3client.AdaptiveTimeout(size, 0, p.RetryPolicies.TimeoutFloor()))
		defer cancel()

		var body io.Reader = io.NewSectionReader(file, offset, size)
		body = &countingReader{r: body, onRead: p.Hooks.OnBytesTransferred}
		body = p.Limiter.WrapUploadReader(timeoutCtx, body)

		out, err := client.UploadPart(timeoutCtx, &s3.UploadPartInput{
			Bucket:        aws.String(p.Bucket),
			Key:           aws.String(p.Key),
			PartNumber:    aws.Int32(partNumber),
			UploadId:      aws.String(uploadID),
			Body:          body,
			ContentLength: aws.Int64(size),
		}, unsignedPayload)
		if err != nil {
			return err
		}

		if out.ETag == nil {
			return fmt.Errorf("S3 did not return an ETag for part %d", partNumber)
		}

		partETag = strings.Trim(*out.ETag, `"`)

		return nil
	})

	return partETag, err
}

// completeMultipartUpload sorts partETags into ascending PartNumber order,
// calls CompleteMultipartUpload with the full part list (both parts
// discovered via listCompletedParts on resume and parts just uploaded by
// uploadPart), and performs the best-effort composite-ETag verification
// (FR-TR-004, verifyMultipartETag) against the response.
func completeMultipartUpload(ctx context.Context, p UploadParams, uploadID string, partETags map[int32]string) (string, error) {
	partNumbers := make([]int32, 0, len(partETags))
	for n := range partETags {
		partNumbers = append(partNumbers, n)
	}

	sort.Slice(partNumbers, func(i, j int) bool { return partNumbers[i] < partNumbers[j] })

	parts := make([]types.CompletedPart, 0, len(partNumbers))
	orderedETags := make([]string, 0, len(partNumbers))

	for _, n := range partNumbers {
		partETag := partETags[n]

		parts = append(parts, types.CompletedPart{
			ETag:       aws.String(partETag),
			PartNumber: aws.Int32(n),
		})
		orderedETags = append(orderedETags, partETag)
	}

	var finalETag string

	err := s3client.WithRetry(ctx, p.Breaker, p.RetryPolicies.Metadata(), p.Host, func(attemptCtx context.Context, isRetry bool) error {
		client := p.Pooled
		if isRetry {
			client = p.Fresh
		}

		out, err := client.CompleteMultipartUpload(attemptCtx, &s3.CompleteMultipartUploadInput{
			Bucket:          aws.String(p.Bucket),
			Key:             aws.String(p.Key),
			UploadId:        aws.String(uploadID),
			MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
		})
		if err != nil {
			return err
		}

		if out.ETag == nil {
			return errors.New("S3 did not return an ETag from CompleteMultipartUpload")
		}

		finalETag = strings.Trim(*out.ETag, `"`)

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("complete multipart upload: %w", err)
	}

	// Best-effort integrity check (FR-TR-004): deliberately not fatal.
	// verifyMultipartETag itself already treats a non-composite-format
	// ETag (SSE-KMS/non-standard providers) as "not applicable" rather
	// than "failed" - see its doc comment - and a genuine mismatch (or a
	// verifyMultipartETag computation error, e.g. a malformed part ETag)
	// is likewise not surfaced as an error by this MVP: neither result is
	// currently acted on or logged, since this package has no
	// logging/telemetry plumbing yet. A dedicated verification-failure
	// policy is future work.
	//nolint:errcheck // best-effort verification only, see comment above
	_, _ = verifyMultipartETag(finalETag, orderedETags)

	return finalETag, nil
}

// AbortMultipartUpload calls S3's AbortMultipartUpload for uploadID
// against bucket/key, releasing any parts already stored server-side for
// it (and, on providers that enforce it, freeing the storage they
// occupied). It is a standalone primitive, deliberately NOT called from
// anywhere inside uploadMultipart itself - per the Этап 3 plan's
// architecture decision, aborting is reserved for an explicit,
// user-initiated Cancel (as opposed to a Pause or a transient Failed
// state, both of which must leave the server-side MPU alive for a later
// Resume via ExistingUploadID/ListParts). The future task.go (Stage 3
// Block F) is expected to call this directly, with whichever client
// (pooled or fresh) it has on hand and its own context, once it has
// decided a task's Cancel is unambiguous and final.
func AbortMultipartUpload(ctx context.Context, client *s3.Client, bucket, key, uploadID string) error {
	_, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("abort multipart upload %s: %w", uploadID, err)
	}

	return nil
}

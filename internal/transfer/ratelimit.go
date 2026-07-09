package transfer

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// BandwidthLimiter holds a pair of independent token-bucket rate limiters,
// one for upload and one for download (docs/02-tech-spec.md section 10.6:
// "Отдельные лимитеры для upload и download... применяется на уровне
// io.Reader/io.Writer wrapper"). A nil *rate.Limiter for a given direction -
// which is exactly what newBandwidthDirectionLimiter returns for a
// bytesPerSec value <= 0 - means that direction is unlimited; this,
// combined with BandwidthLimiter itself being safe to use as a nil pointer
// (see WrapUploadReader/WrapDownloadReader), is what makes "bandwidth
// limiting is disabled by default" (the Stage 3 plan's constraint 7: "По
// умолчанию лимит выключен") require no special-casing anywhere else in
// this package - upload.go/download.go's future integration with this type
// (a later, separate step per this file's task description) can always
// call WrapUploadReader/WrapDownloadReader unconditionally.
type BandwidthLimiter struct {
	upload, download *rate.Limiter
}

// NewBandwidthLimiter creates a BandwidthLimiter with the given per-second
// byte-rate limits. uploadBytesPerSec/downloadBytesPerSec <= 0 mean "no
// limit" for that direction (the corresponding *rate.Limiter field is left
// nil), independently of the other direction's value.
func NewBandwidthLimiter(uploadBytesPerSec, downloadBytesPerSec int64) *BandwidthLimiter {
	return &BandwidthLimiter{
		upload:   newBandwidthDirectionLimiter(uploadBytesPerSec),
		download: newBandwidthDirectionLimiter(downloadBytesPerSec),
	}
}

// newBandwidthDirectionLimiter returns a *rate.Limiter configured for
// bytesPerSec, or nil if bytesPerSec <= 0 ("no limit").
//
// The burst size is set equal to bytesPerSec itself - i.e. the bucket can
// hold up to one second's worth of tokens. This is a deliberate, simple
// choice (not tied to any particular caller's read-buffer size, since
// callers throughout this package use several different buffer sizes -
// downloadSegmentReadBufferSize's 32KB, whatever io.Copy's default 32KB
// buffer picks for uploadSingle/uploadPart's countingReader chain, etc):
// it allows a short burst up to the configured rate's one-second budget
// before smoothing down to the steady-state limit, a standard token-bucket
// pattern. Because a single Read can - and, at low configured limits, often
// will - return more bytes than this burst in one call,
// limitedReader.Read/wait never hands a whole Read's byte count to one
// rate.Limiter.WaitN call; see wait's doc comment for why and how it
// avoids the "n exceeds burst" error rate.Limiter.WaitN returns in that
// case.
func newBandwidthDirectionLimiter(bytesPerSec int64) *rate.Limiter {
	if bytesPerSec <= 0 {
		return nil
	}

	return rate.NewLimiter(rate.Limit(bytesPerSec), int(bytesPerSec))
}

// WrapUploadReader wraps r with b's upload-direction rate limiter, pacing
// Read calls against it as the wrapped reader is consumed - intended to
// wrap a request body while it is streamed to S3 (PutObject/UploadPart).
// If b is nil or its upload limit is unset (NewBandwidthLimiter's
// uploadBytesPerSec <= 0), r is returned unwrapped, unchanged, so an
// unlimited BandwidthLimiter adds no overhead beyond a couple of nil
// checks.
//
// ctx governs each internal rate.Limiter.WaitN call: canceling it (e.g. a
// task's Pause, mirroring how every other blocking call in this package is
// tied to a caller-owned, cancelable context) makes the wrapped reader's
// Read return ctx.Err() rather than block indefinitely waiting for tokens.
func (b *BandwidthLimiter) WrapUploadReader(ctx context.Context, r io.Reader) io.Reader {
	if b == nil || b.upload == nil {
		return r
	}

	return &limitedReader{ctx: ctx, r: r, limiter: b.upload}
}

// WrapDownloadReader is WrapUploadReader's download-direction counterpart,
// wrapping r with b's download rate limiter - intended to wrap a response
// body while it is streamed from S3 (GetObject). See WrapUploadReader's doc
// comment; the same nil-safety and ctx-cancellation behavior applies here.
func (b *BandwidthLimiter) WrapDownloadReader(ctx context.Context, r io.Reader) io.Reader {
	if b == nil || b.download == nil {
		return r
	}

	return &limitedReader{ctx: ctx, r: r, limiter: b.download}
}

// limitedReader wraps an io.Reader, pacing it against a *rate.Limiter (one
// token per byte) so its aggregate throughput does not exceed the
// limiter's configured rate.
type limitedReader struct {
	ctx     context.Context //nolint:containedctx // ctx is supplied once at wrap time (WrapUploadReader/WrapDownloadReader) rather than threaded through Read's signature, since io.Reader's standard interface has no room for one - the same tradeoff s3client's retry/timeout plumbing elsewhere in this codebase does not need to make only because it does not sit behind a stdlib interface boundary like io.Reader does.
	r       io.Reader
	limiter *rate.Limiter
}

// Read reads from the wrapped reader as normal, then blocks (via wait)
// until the limiter has released enough tokens to cover the bytes just
// read, before returning them to the caller. Bytes are paced AFTER being
// read (not before), matching how the S3 SDK's request body is actually
// consumed - by the time Read returns, the caller (the HTTP transport)
// already has the bytes in hand, so pacing controls how quickly Read
// yields them, not how quickly the underlying reader itself produces them.
func (l *limitedReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if n > 0 {
		if waitErr := l.wait(n); waitErr != nil {
			return n, waitErr
		}
	}

	return n, err
}

// wait blocks until the limiter has released n tokens (one per byte just
// read), in chunks no larger than the limiter's burst size.
//
// This is the fix for a real rate.Limiter gotcha: Limiter.WaitN(ctx, n)
// returns an error IMMEDIATELY, without waiting at all, if n exceeds the
// limiter's burst - so a naive single WaitN(ctx, n) call would fail outright
// (rather than simply waiting longer, as one might expect) whenever a
// single Read call returns more bytes than the configured burst. Since this
// package's callers use several different read-buffer sizes (see
// newBandwidthDirectionLimiter's doc comment) - and since the burst itself
// is deliberately sized off the configured rate, not off any particular
// buffer size, so a low configured limit (e.g. 10KB/s, a realistic
// "throttle this transfer" setting) can easily have a smaller burst than a
// single 32KB read - relying on every caller's buffer size happening to fit
// under whatever burst was configured is not robust. Splitting into
// burst-sized (or smaller) chunks and calling WaitN once per chunk, in a
// loop, is: it always keeps each individual WaitN call's n within the
// limiter's own limit, so an oversized single Read simply takes several
// WaitN calls (and therefore a bit longer, exactly as intended) rather than
// erroring out.
func (l *limitedReader) wait(n int) error {
	burst := l.limiter.Burst()
	if burst < 1 {
		// newBandwidthDirectionLimiter never actually constructs a
		// *rate.Limiter with burst < 1 (bytesPerSec <= 0 yields a nil
		// *rate.Limiter entirely, handled before a limitedReader is ever
		// created - see WrapUploadReader/WrapDownloadReader), but guard
		// against it anyway so a change there, or a *rate.Limiter built
		// some other way in the future, can never make this loop spin
		// without making forward progress.
		burst = 1
	}

	remaining := n
	for remaining > 0 {
		chunk := remaining
		if chunk > burst {
			chunk = burst
		}

		if err := l.limiter.WaitN(l.ctx, chunk); err != nil {
			return err
		}

		remaining -= chunk
	}

	return nil
}

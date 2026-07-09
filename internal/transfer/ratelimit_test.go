package transfer

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// TestBandwidthLimiter_NilWrapsPassThrough verifies a nil *BandwidthLimiter
// (the zero value a caller gets from simply not constructing one -
// NewBandwidthLimiter is never required to be called at all when limiting
// is unused) and a BandwidthLimiter constructed with <= 0 limits both
// return the exact same reader unwrapped, so an unlimited/absent limiter
// adds no indirection.
func TestBandwidthLimiter_NilWrapsPassThrough(t *testing.T) {
	var nilLimiter *BandwidthLimiter

	unlimited := NewBandwidthLimiter(0, -1)

	tests := []struct {
		name string
		b    *BandwidthLimiter
	}{
		{"nil *BandwidthLimiter", nilLimiter},
		{"NewBandwidthLimiter(0, -1) (both directions unset)", unlimited},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := bytes.NewReader([]byte("hello world"))

			gotUpload := tt.b.WrapUploadReader(context.Background(), src)
			if gotUpload != io.Reader(src) {
				t.Errorf("WrapUploadReader() returned a different reader, want src unwrapped")
			}

			gotDownload := tt.b.WrapDownloadReader(context.Background(), src)
			if gotDownload != io.Reader(src) {
				t.Errorf("WrapDownloadReader() returned a different reader, want src unwrapped")
			}
		})
	}
}

// TestBandwidthLimiter_WrapsWhenSet verifies that when a direction's limit
// IS set, Wrap*Reader returns something other than the bare source reader
// (i.e. it actually wraps).
func TestBandwidthLimiter_WrapsWhenSet(t *testing.T) {
	b := NewBandwidthLimiter(1024, 1024)
	src := bytes.NewReader([]byte("hello world"))

	gotUpload := b.WrapUploadReader(context.Background(), src)
	if gotUpload == io.Reader(src) {
		t.Errorf("WrapUploadReader() returned src unwrapped, want a limited wrapper")
	}

	gotDownload := b.WrapDownloadReader(context.Background(), src)
	if gotDownload == io.Reader(src) {
		t.Errorf("WrapDownloadReader() returned src unwrapped, want a limited wrapper")
	}
}

// TestLimitedReader_ThrottlesThroughput verifies a low bandwidth limit
// actually paces reads: 20KB of data through a 10KB/s limiter must take
// noticeably longer than the near-instant read an unlimited reader would
// give (FR-TR-006).
func TestLimitedReader_ThrottlesThroughput(t *testing.T) {
	const limitBytesPerSec = 10 * 1024
	const dataSize = 20 * 1024

	b := NewBandwidthLimiter(limitBytesPerSec, 0)

	data := bytes.Repeat([]byte("x"), dataSize)
	limited := b.WrapUploadReader(context.Background(), bytes.NewReader(data))

	start := time.Now()

	n, err := io.Copy(io.Discard, limited)
	if err != nil {
		t.Fatalf("io.Copy() error = %v", err)
	}
	if n != dataSize {
		t.Fatalf("io.Copy() copied %d bytes, want %d", n, dataSize)
	}

	elapsed := time.Since(start)

	// 20KB at 10KB/s, with burst == 10KB (one second's worth of tokens),
	// should take roughly (20KB-10KB)/10KB/s = 1s once the initial burst is
	// spent. Assert at least ~0.9s to allow for scheduling jitter while
	// still clearly distinguishing this from an unthrottled, sub-millisecond
	// read.
	if elapsed < 900*time.Millisecond {
		t.Errorf("io.Copy() of %d bytes at %d B/s took %v, want at least ~900ms", dataSize, limitBytesPerSec, elapsed)
	}
}

// TestLimitedReader_ReadLargerThanBurstDoesNotError is the key regression
// test for the rate.Limiter gotcha wait's doc comment describes: a single
// Read returning MORE bytes than the limiter's burst size must not make the
// wrapped reader fail outright (which a naive single WaitN(ctx, n) call
// would do - rate.Limiter.WaitN documents returning an error immediately
// when n exceeds burst, rather than waiting longer). It must instead
// transparently succeed via several WaitN calls chunked to the burst size.
func TestLimitedReader_ReadLargerThanBurstDoesNotError(t *testing.T) {
	const bytesPerSec = 1000 // burst == 1000, per newBandwidthDirectionLimiter
	const dataSize = 1500    // deliberately > burst, forced into a single Read below

	b := NewBandwidthLimiter(bytesPerSec, 0)

	data := bytes.Repeat([]byte("y"), dataSize)
	limited := b.WrapUploadReader(context.Background(), bytes.NewReader(data))

	// A buffer at least as large as dataSize forces bytes.Reader.Read to
	// hand back all dataSize bytes in a single call, so n (1500) > burst
	// (1000) for this one Read - exactly the case that would make a naive
	// WaitN(ctx, n) call fail immediately.
	buf := make([]byte, dataSize)

	n, err := limited.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v, want nil (single Read of %d bytes > burst %d must still succeed via chunked WaitN calls, not fail outright)", err, dataSize, bytesPerSec)
	}
	if n != dataSize {
		t.Fatalf("Read() n = %d, want %d", n, dataSize)
	}
}

// TestLimitedReader_ContextCancellation verifies a canceled context makes a
// blocked Read return promptly with an error, rather than hanging forever -
// important since task.go will tie this ctx to a task's own
// pause/cancel-driven context.
func TestLimitedReader_ContextCancellation(t *testing.T) {
	const bytesPerSec = 1 // pathologically slow, guarantees Read would otherwise block
	const dataSize = 1000

	b := NewBandwidthLimiter(bytesPerSec, 0)

	ctx, cancel := context.WithCancel(context.Background())

	data := bytes.Repeat([]byte("z"), dataSize)
	limited := b.WrapUploadReader(ctx, bytes.NewReader(data))

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	buf := make([]byte, dataSize)

	done := make(chan error, 1)
	go func() {
		_, err := limited.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Read() error = nil, want a context-cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read() did not return after context cancellation")
	}
}

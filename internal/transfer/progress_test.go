package transfer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTracker_AddBytesConcurrent verifies AddBytes is safe for concurrent
// use by many goroutines at once (as UploadHooks.OnBytesTransferred/
// DownloadHooks.OnBytesTransferred's own doc comments require of whatever
// they are wired to) and that the final transferred count Snapshot reports
// is exactly the sum of every delta added - run under -race.
func TestTracker_AddBytesConcurrent(t *testing.T) {
	const goroutines = 50
	const addsPerGoroutine = 1000
	const delta = 7

	tr := NewTracker(int64(goroutines * addsPerGoroutine * delta))

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			for range addsPerGoroutine {
				tr.AddBytes(delta)
			}
		}()
	}

	wg.Wait()

	transferred, total, _, _ := tr.Snapshot()

	wantTransferred := int64(goroutines * addsPerGoroutine * delta)
	if transferred != wantTransferred {
		t.Fatalf("Snapshot() transferred = %d, want %d", transferred, wantTransferred)
	}

	if total != wantTransferred {
		t.Fatalf("Snapshot() total = %d, want %d", total, wantTransferred)
	}
}

// TestTracker_SnapshotUnknownTotal verifies NewTracker(0)/negative totals
// never panic and Snapshot reports ETA as unavailable (-1) rather than
// computing anything from a zero/unknown total.
func TestTracker_SnapshotUnknownTotal(t *testing.T) {
	tr := NewTracker(0)
	tr.AddBytes(100)

	transferred, total, speed, eta := tr.Snapshot()
	if transferred != 100 {
		t.Errorf("transferred = %d, want 100", transferred)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if speed != 0 {
		t.Errorf("speed = %v, want 0 (no samples recorded yet)", speed)
	}
	if eta != -1 {
		t.Errorf("eta = %d, want -1 (unknown total)", eta)
	}
}

// TestTracker_SnapshotAlreadyDone verifies transferred >= total yields
// eta=0 ("nothing left"), distinct from the "-1 unknown" case, once a
// speed reading actually exists.
func TestTracker_SnapshotAlreadyDone(t *testing.T) {
	tr := NewTracker(100)
	tr.AddBytes(100)

	// Fabricate two in-window samples directly (white-box) so speed is
	// nonzero and computeETA's "done" branch, not its "speed unknown"
	// branch, is what's actually being exercised.
	base := time.Unix(0, 0)
	tr.nowFunc = func() time.Time { return base }
	tr.samples = []speedSample{
		{t: base.Add(-1 * time.Second), bytes: 50},
		{t: base, bytes: 100},
	}

	transferred, total, speed, eta := tr.Snapshot()
	if transferred != 100 || total != 100 {
		t.Fatalf("transferred=%d total=%d, want 100/100", transferred, total)
	}
	if speed <= 0 {
		t.Fatalf("speed = %v, want > 0", speed)
	}
	if eta != 0 {
		t.Errorf("eta = %d, want 0 (already done)", eta)
	}
}

// TestTracker_SpeedSyntheticSamples feeds a Tracker a hand-built set of
// samples via its unexported nowFunc/samples fields (white-box, same
// package as production code - matching s3client.CircuitBreaker's own
// nowFunc-injection test pattern) and verifies currentSpeedLocked's moving
// average matches hand-computed arithmetic, without needing a real clock,
// real sleeps, or Run's tickers at all.
func TestTracker_SpeedSyntheticSamples(t *testing.T) {
	tr := NewTracker(1_000_000)

	base := time.Unix(1_700_000_000, 0)
	tr.nowFunc = func() time.Time { return base }

	// Oldest sample 4s old (within the 5s window), newest "now": 400KB
	// transferred over 4s = 100KB/s.
	tr.samples = []speedSample{
		{t: base.Add(-4 * time.Second), bytes: 100_000},
		{t: base.Add(-2 * time.Second), bytes: 300_000},
		{t: base, bytes: 500_000},
	}
	tr.transferred.Store(500_000)

	_, _, speed, _ := tr.Snapshot()

	const want = 100_000.0 // (500_000-100_000)/4s
	if speed != want {
		t.Errorf("speed = %v, want %v", speed, want)
	}
}

// TestTracker_SpeedPrunesOldSamples verifies samples older than speedWindow
// (5s) are excluded from the moving average - a sample from 10s ago must
// not still be dragging the average down/up once it has fallen out of the
// window.
func TestTracker_SpeedPrunesOldSamples(t *testing.T) {
	tr := NewTracker(1_000_000)

	base := time.Unix(1_700_000_000, 0)
	tr.nowFunc = func() time.Time { return base }

	tr.samples = []speedSample{
		{t: base.Add(-10 * time.Second), bytes: 0}, // out of window, must be pruned
		{t: base.Add(-3 * time.Second), bytes: 300_000},
		{t: base, bytes: 600_000},
	}
	tr.transferred.Store(600_000)

	_, _, speed, _ := tr.Snapshot()

	const want = 100_000.0 // (600_000-300_000)/3s, NOT (600_000-0)/10s
	if speed != want {
		t.Errorf("speed = %v, want %v (stale 10s-old sample should have been pruned)", speed, want)
	}

	if len(tr.samples) != 2 {
		t.Errorf("len(samples) after prune = %d, want 2", len(tr.samples))
	}
}

// TestTracker_SpeedSingleSampleIsZero verifies a lone sample (no interval
// to measure a rate over yet) yields speed=0, not a division-by-zero panic
// or a nonsensical result.
func TestTracker_SpeedSingleSampleIsZero(t *testing.T) {
	tr := NewTracker(1_000_000)

	base := time.Unix(1_700_000_000, 0)
	tr.nowFunc = func() time.Time { return base }
	tr.samples = []speedSample{{t: base, bytes: 500}}
	tr.transferred.Store(500)

	_, _, speed, eta := tr.Snapshot()
	if speed != 0 {
		t.Errorf("speed = %v, want 0 (only one sample)", speed)
	}
	if eta != -1 {
		t.Errorf("eta = %d, want -1 (speed unknown)", eta)
	}
}

// TestTracker_Run verifies Run calls onUI on (roughly) every uiInterval
// tick and onPersist on (roughly) every, longer, persistInterval tick,
// using intervals fast enough to keep the test itself fast (10ms/30ms
// rather than production's 500ms/3s) rather than sleeping for real
// production-scale durations.
func TestTracker_Run(t *testing.T) {
	tr := NewTracker(1000)

	var uiCalls, persistCalls atomic.Int64

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		tr.Run(ctx, 10*time.Millisecond, 30*time.Millisecond,
			func(transferred, total int64, speedBytesPerSec float64, etaSeconds int64) {
				uiCalls.Add(1)
			},
			func(transferred, total int64) {
				persistCalls.Add(1)
			},
		)
	}()

	// Let several ticks of both intervals fire.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}

	gotUI := uiCalls.Load()
	gotPersist := persistCalls.Load()

	if gotUI < 5 {
		t.Errorf("onUI called %d times in 150ms at 10ms interval, want at least 5", gotUI)
	}
	if gotPersist < 2 {
		t.Errorf("onPersist called %d times in 150ms at 30ms interval, want at least 2", gotPersist)
	}
	if gotPersist >= gotUI {
		t.Errorf("onPersist (%d) called at least as often as onUI (%d), want onPersist strictly less frequent (30ms vs 10ms interval)", gotPersist, gotUI)
	}
}

// TestTracker_RunNilCallbacks verifies Run tolerates nil onUI/onPersist
// (a documented, valid input) without panicking.
func TestTracker_RunNilCallbacks(t *testing.T) {
	tr := NewTracker(1000)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		tr.Run(ctx, 5*time.Millisecond, 5*time.Millisecond, nil, nil)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}
}

// TestTracker_RunStopsOnCancel verifies Run returns promptly once ctx is
// canceled, rather than hanging - a Tracker.Run call that never returns
// would leak the goroutine the future task.go runs it in for every
// paused/completed/canceled task.
func TestTracker_RunStopsOnCancel(t *testing.T) {
	tr := NewTracker(1000)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before Run even starts

	done := make(chan struct{})
	go func() {
		defer close(done)
		tr.Run(ctx, time.Hour, time.Hour, nil, nil)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly for an already-canceled ctx")
	}
}

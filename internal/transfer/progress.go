package transfer

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// speedWindow is the sliding window Tracker's speed calculation averages
// over (FR-TR-005: "скорость (скользящее среднее за 5 сек)").
const speedWindow = 5 * time.Second

// speedSample is one point recorded in Tracker's speed sliding window: the
// cumulative transferred-bytes counter's value as observed at time t. Two
// samples far enough apart in the window let currentSpeedLocked compute an
// average rate over the interval between them.
type speedSample struct {
	t     time.Time
	bytes int64
}

// Tracker tracks the progress of a single upload or download task: a
// thread-safe transferred-bytes counter that multiple worker goroutines
// write to concurrently (via AddBytes - the exact func(delta int64)
// signature UploadHooks.OnBytesTransferred/DownloadHooks.OnBytesTransferred
// already expect, so the future task.go, Stage 3 Block F, wires a Tracker's
// AddBytes directly into whichever of those two hook fields a given task
// needs), plus a 5-second sliding-window moving average of transfer speed
// and an ETA derived from it.
//
// Like upload.go/download.go, this file has no dependency on *storage or
// Wails runtime: Run's onUI/onPersist callbacks are the only way progress
// data leaves this type, leaving the caller (task.go) free to wire them to
// runtime.EventsEmit and a *storage.TransferQueueRepository respectively
// without this package needing to know either exists.
type Tracker struct {
	// totalBytes is set once at construction (NewTracker) and never
	// modified afterward, so it needs no synchronization of its own to
	// read from Snapshot/Run.
	totalBytes int64

	// transferred is incremented by AddBytes, which - per
	// UploadHooks.OnBytesTransferred/DownloadHooks.OnBytesTransferred's own
	// documented contract - is called concurrently by multiple worker
	// goroutines (one per part/segment in flight) with no synchronization
	// of their own; atomic.Int64 is what makes that safe here without a
	// mutex on the hot path.
	transferred atomic.Int64

	// mu guards samples, which is written only by recordSample (called
	// exclusively from the single goroutine running Run's tick loop - see
	// Run's doc comment) but read by Snapshot, which may be called
	// concurrently from any goroutine (e.g. a caller inspecting progress
	// outside of Run's own onUI callback).
	mu      sync.Mutex
	samples []speedSample

	// nowFunc returns the current time and defaults to time.Now, mirroring
	// s3client.CircuitBreaker's nowFunc field: it exists solely so tests
	// can feed Tracker synthetic timestamps (via direct, white-box access
	// to nowFunc and the samples slice, both unexported and therefore only
	// reachable from this package's own tests) without a real clock or
	// real sleeps. NewTracker always sets it, so it is never nil on a
	// properly constructed Tracker.
	nowFunc func() time.Time
}

// NewTracker creates a Tracker for a task whose total size (totalBytes) is
// already known. totalBytes may be 0 (or otherwise <= 0) if the total size
// is not known yet at construction time - NewTracker never rejects or
// panics on this; Snapshot simply reports ETA as unavailable (see its doc
// comment) in that case, since an ETA cannot be computed without a total to
// measure remaining bytes against.
func NewTracker(totalBytes int64) *Tracker {
	return &Tracker{
		totalBytes: totalBytes,
		nowFunc:    time.Now,
	}
}

// AddBytes atomically increments the transferred-bytes counter by delta.
// Its signature intentionally matches
// UploadHooks.OnBytesTransferred/DownloadHooks.OnBytesTransferred exactly
// (func(delta int64)) - this is the primary integration point described in
// this file's package-level doc comment. Safe for concurrent use by
// multiple goroutines.
func (t *Tracker) AddBytes(delta int64) {
	t.transferred.Add(delta)
}

// Snapshot returns the Tracker's current state:
//
//   - transferred, total: the raw byte counters (total is simply
//     t.totalBytes, exactly as given to NewTracker - see its doc comment
//     for the totalBytes<=0/"unknown" case).
//   - speedBytesPerSec: the moving average over the trailing speedWindow
//     (5s) of samples recorded so far, computed from whatever samples
//     Run's UI ticker (see its doc comment) has recorded up to now. 0 is
//     returned both when Run has not recorded enough samples yet (fewer
//     than two within the window - a rate needs two points) and when the
//     computed rate would otherwise be pathological (e.g. a zero or
//     negative elapsed interval between the oldest and newest in-window
//     sample, which should not normally happen with a monotonic clock but
//     is guarded against defensively).
//   - etaSeconds: -1 when ETA cannot be computed (total<=0, meaning the
//     total size is unknown - see NewTracker; or speedBytesPerSec<=0,
//     meaning there is not yet a usable speed reading), 0 when transferred
//     has already reached or exceeded total (nothing left to transfer, not
//     "unknown"), and otherwise ceil((total-transferred)/speedBytesPerSec).
//     -1 (rather than overloading 0 for both "unknown" and "done") is
//     chosen specifically so a future frontend consuming
//     domain.TransferProgressEvent.ETASeconds can distinguish "no ETA to
//     show yet" from "essentially finished" without also having to
//     cross-check TransferredBytes/TotalBytes itself.
//
// Snapshot is safe for concurrent use by multiple goroutines, including
// concurrently with Run's own tick loop for the same Tracker.
func (t *Tracker) Snapshot() (transferred, total int64, speedBytesPerSec float64, etaSeconds int64) {
	transferred = t.transferred.Load()
	total = t.totalBytes

	t.mu.Lock()
	now := t.nowFunc()
	t.pruneSamplesLocked(now)
	speedBytesPerSec = t.currentSpeedLocked()
	t.mu.Unlock()

	etaSeconds = computeETA(transferred, total, speedBytesPerSec)

	return transferred, total, speedBytesPerSec, etaSeconds
}

// computeETA implements the etaSeconds convention documented on Snapshot.
func computeETA(transferred, total int64, speedBytesPerSec float64) int64 {
	if total <= 0 || speedBytesPerSec <= 0 {
		return -1
	}

	remaining := total - transferred
	if remaining <= 0 {
		return 0
	}

	return int64(math.Ceil(float64(remaining) / speedBytesPerSec))
}

// recordSample appends a new (now, cumulative transferred bytes) sample to
// the sliding window and prunes samples that have fallen out of it. Called
// exclusively by Run's own tick loop - see Run's doc comment for why
// sampling deliberately piggybacks on the UI ticker rather than running a
// third, dedicated ticker just for this.
func (t *Tracker) recordSample() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFunc()
	t.samples = append(t.samples, speedSample{t: now, bytes: t.transferred.Load()})
	t.pruneSamplesLocked(now)
}

// pruneSamplesLocked drops every sample older than speedWindow relative to
// now, keeping samples reslicing the existing backing array (no
// reallocation/copy) rather than allocating a fresh slice - callers of this
// method always hold t.mu.
func (t *Tracker) pruneSamplesLocked(now time.Time) {
	cutoff := now.Add(-speedWindow)

	i := 0
	for i < len(t.samples) && t.samples[i].t.Before(cutoff) {
		i++
	}

	if i > 0 {
		t.samples = t.samples[i:]
	}
}

// currentSpeedLocked computes the moving-average speed (bytes/sec) from the
// oldest and newest samples currently in the window - callers of this
// method always hold t.mu, and are expected to have already called
// pruneSamplesLocked so t.samples holds only in-window samples.
func (t *Tracker) currentSpeedLocked() float64 {
	if len(t.samples) < 2 {
		return 0
	}

	oldest := t.samples[0]
	newest := t.samples[len(t.samples)-1]

	elapsed := newest.t.Sub(oldest.t).Seconds()
	if elapsed <= 0 {
		return 0
	}

	return float64(newest.bytes-oldest.bytes) / elapsed
}

// Run drives a Tracker for the lifetime of one active transfer task: it
// blocks, running two independent tickers, until ctx is canceled (the
// future task.go, Stage 3 Block F, is expected to run one Run call per
// active task in its own goroutine, canceling ctx on Pause/Cancel/task
// completion exactly as it already does for the task's own S3 calls).
//
//   - every uiInterval (500ms in production, per FR-TR-005's "события
//     публикуются... каждые 500 мс") it records a new speed sample
//     (recordSample - this is deliberately the ONLY place samples are
//     recorded, piggybacking on this ticker rather than running a separate,
//     third ticker purely for sampling) and, if onUI is non-nil, calls it
//     with the resulting Snapshot(). onUI is intended for the caller to
//     publish a Wails Event; publishing itself is not this package's
//     concern.
//   - every persistInterval (3s in production, per the Stage 3 plan's
//     "Progress" architecture note: "отдельный тикер 3с для записи
//     прогресса в SQLite... троттлинг, чтобы частые UI-события не били по
//     БД") it calls onPersist, if non-nil, with the current (transferred,
//     total) - no separate Snapshot/speed computation, since persisted
//     progress does not need speed/ETA. Intended for the caller to persist
//     via TransferQueueRepository.UpdateProgress.
//
// Both callbacks may be nil (the corresponding tick is then a no-op, not a
// panic). FR-TR-005's "или при изменении > 1%" threshold-triggered
// publication (in addition to the 500ms cadence) is left to the caller of
// onUI to apply if desired - Run only provides the fixed-cadence half of
// that requirement, since ">1% change" is a policy decision about when an
// extra, off-cadence event is worth publishing, not something intrinsic to
// tracking progress itself.
func (t *Tracker) Run(
	ctx context.Context,
	uiInterval, persistInterval time.Duration,
	onUI func(transferred, total int64, speedBytesPerSec float64, etaSeconds int64),
	onPersist func(transferred, total int64),
) {
	uiTicker := time.NewTicker(uiInterval)
	defer uiTicker.Stop()

	persistTicker := time.NewTicker(persistInterval)
	defer persistTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-uiTicker.C:
			t.recordSample()

			if onUI != nil {
				transferred, total, speed, eta := t.Snapshot()
				onUI(transferred, total, speed, eta)
			}

		case <-persistTicker.C:
			if onPersist != nil {
				onPersist(t.transferred.Load(), t.totalBytes)
			}
		}
	}
}

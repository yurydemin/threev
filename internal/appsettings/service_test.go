package appsettings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"threev/internal/connection"
	"threev/internal/domain"
	"threev/internal/s3client"
	"threev/internal/storage"
	"threev/internal/transfer"
)

// testSettingsDeps bundles a fresh SettingsService (over a real, migrated,
// temp-file SQLite database - mirroring internal/transfer/service_test.go's
// testTransferDeps) with the pieces needed to create test profiles and
// exercise its underlying *transfer.TransferService directly.
type testSettingsDeps struct {
	settingsSvc *SettingsService
	transferSvc *transfer.TransferService
	profileRepo *storage.ProfileRepository
	key         [32]byte
}

func newTestSettingsService(t *testing.T) testSettingsDeps {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "appsettings_test.db")

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB(%q) returned error: %v", dbPath, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() returned error: %v", err)
		}
	})

	profileRepo := storage.NewProfileRepository(db)
	queueRepo := storage.NewTransferQueueRepository(db)
	historyRepo := storage.NewTransferHistoryRepository(db)

	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}

	connMgr := s3client.NewConnectionManager(profileRepo, key)
	breaker := s3client.NewCircuitBreaker()

	transferSvc := transfer.NewTransferService(profileRepo, key, queueRepo, historyRepo, connMgr, breaker)
	settingsSvc := NewSettingsService(db, transferSvc)

	return testSettingsDeps{
		settingsSvc: settingsSvc,
		transferSvc: transferSvc,
		profileRepo: profileRepo,
		key:         key,
	}
}

// testProfileNameCounter guarantees every createTestProfile call gets a
// unique profile Name within a test binary run, mirroring
// internal/transfer/service_test.go's identical helper.
var testProfileNameCounter atomic.Int64

func createTestProfile(t *testing.T, profileRepo *storage.ProfileRepository, key [32]byte, endpointURL string) int64 {
	t.Helper()

	connSvc := connection.NewConnectionService(profileRepo, key)

	saved, err := connSvc.SaveProfile(domain.Profile{
		Name:            fmt.Sprintf("appsettings-test-%d", testProfileNameCounter.Add(1)),
		EndpointURL:     endpointURL,
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "supersecret",
		PathStyle:       true,
		VerifySSL:       true,
	})
	if err != nil {
		t.Fatalf("SaveProfile() returned error: %v", err)
	}

	return saved.ID
}

// TestGetSettingsOnEmptyDatabaseReturnsDefaults verifies GetSettings' lazy
// per-field default behavior (see its own doc comment): a database with no
// "settings" rows at all returns every field's documented default, matching
// the application's behavior exactly as it was before this package existed.
func TestGetSettingsOnEmptyDatabaseReturnsDefaults(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	got, err := deps.settingsSvc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings() returned error: %v", err)
	}

	want := domain.AppSettings{
		Theme:                             "system",
		UIScalePercent:                    100,
		CloseBehavior:                     "exit",
		AutoResumeQueue:                   false,
		MaxConcurrentTransfers:            transfer.DefaultMaxConcurrentTasks,
		PartSizeOverrideMB:                0,
		BandwidthLimitUploadBytesPerSec:   0,
		BandwidthLimitDownloadBytesPerSec: 0,
	}

	if got != want {
		t.Errorf("GetSettings() on empty database = %+v, want %+v", got, want)
	}
}

// TestGetSettingsOnEmptyDatabaseDoesNotPersistDefaults is a direct
// regression test for GetSettings' documented "lazy default, never
// eagerly persisted" contract: calling it must not, as a side effect,
// write any "settings" row - unlike storage.GetOrCreateSetting's own,
// deliberately different, eager-persist-on-first-read pattern.
func TestGetSettingsOnEmptyDatabaseDoesNotPersistDefaults(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	if _, err := deps.settingsSvc.GetSettings(); err != nil {
		t.Fatalf("GetSettings() returned error: %v", err)
	}

	for _, key := range []string{
		keyTheme, keyUIScalePercent, keyCloseBehavior, keyAutoResumeQueue,
		keyMaxConcurrentTransfers, keyPartSizeOverrideMB,
		keyBandwidthLimitUploadBPS, keyBandwidthLimitDownloadBPS,
	} {
		if _, err := storage.GetSetting(context.Background(), deps.settingsSvc.db, key); !isErrNoRows(err) {
			t.Errorf("storage.GetSetting(%q) after a GetSettings() call = (_, %v), want sql.ErrNoRows (GetSettings must never eagerly persist a default)", key, err)
		}
	}
}

// isErrNoRows reports whether err is sql.ErrNoRows - a tiny local helper so
// the tests above read a little more clearly than repeating errors.Is at
// every call site.
func isErrNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// TestSaveSettingsThenGetSettingsRoundTrips verifies every domain.AppSettings
// field survives a SaveSettings -> GetSettings round trip unchanged, for a
// set of values that are all already within SaveSettings' valid/clamped
// ranges (so this test is purely about serialization, not clamping - see
// TestSaveSettingsClampsOutOfRangeValues for that).
func TestSaveSettingsThenGetSettingsRoundTrips(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	want := domain.AppSettings{
		Theme:                             "dark",
		UIScalePercent:                    110,
		CloseBehavior:                     "confirm",
		AutoResumeQueue:                   true,
		MaxConcurrentTransfers:            4,
		PartSizeOverrideMB:                32,
		BandwidthLimitUploadBytesPerSec:   1_000_000,
		BandwidthLimitDownloadBytesPerSec: 2_000_000,
	}

	if err := deps.settingsSvc.SaveSettings(want); err != nil {
		t.Fatalf("SaveSettings() returned error: %v", err)
	}

	got, err := deps.settingsSvc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings() returned error: %v", err)
	}

	if got != want {
		t.Errorf("GetSettings() after SaveSettings(%+v) = %+v, want %+v", want, got, want)
	}
}

// TestSaveSettingsClampsOutOfRangeValues verifies SaveSettings' "silent
// clamp, never reject" validation (see its own doc comment): an
// out-of-range or unrecognized value is silently clamped/reset rather than
// causing SaveSettings to return an error, and the CLAMPED value - not the
// raw input - is what a subsequent GetSettings() observes.
func TestSaveSettingsClampsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	input := domain.AppSettings{
		Theme:                             "not-a-real-theme",
		UIScalePercent:                    50,          // below minUIScalePercent (90)
		CloseBehavior:                     "hibernate", // not a recognized value
		AutoResumeQueue:                   true,
		MaxConcurrentTransfers:            999, // above maxMaxConcurrentTransfers (10)
		PartSizeOverrideMB:                1,   // above 0, below minPartSizeOverrideMB (5)
		BandwidthLimitUploadBytesPerSec:   -5,  // negative
		BandwidthLimitDownloadBytesPerSec: -1,  // negative
	}

	if err := deps.settingsSvc.SaveSettings(input); err != nil {
		t.Fatalf("SaveSettings() returned error: %v", err)
	}

	got, err := deps.settingsSvc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings() returned error: %v", err)
	}

	want := domain.AppSettings{
		Theme:                             "system", // reset to default
		UIScalePercent:                    90,       // clamped up to the floor
		CloseBehavior:                     "exit",   // reset to default
		AutoResumeQueue:                   true,
		MaxConcurrentTransfers:            10, // clamped down to the ceiling
		PartSizeOverrideMB:                5,  // clamped up to the floor
		BandwidthLimitUploadBytesPerSec:   0,
		BandwidthLimitDownloadBytesPerSec: 0,
	}

	if got != want {
		t.Errorf("GetSettings() after SaveSettings(%+v) = %+v, want clamped %+v", input, got, want)
	}
}

// TestSaveSettingsPartSizeOverrideZeroStaysZero verifies SaveSettings'
// PartSizeOverrideMB clamp special-cases 0 (and negative values) as "no
// override" rather than clamping them UP into [5,128] like any positive,
// too-small value would be - 0 must round-trip as 0, not 5.
func TestSaveSettingsPartSizeOverrideZeroStaysZero(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	for _, mb := range []int{0, -1, -100} {
		settings := domain.AppSettings{
			Theme:                  "system",
			UIScalePercent:         100,
			CloseBehavior:          "exit",
			MaxConcurrentTransfers: transfer.DefaultMaxConcurrentTasks,
			PartSizeOverrideMB:     mb,
		}

		if err := deps.settingsSvc.SaveSettings(settings); err != nil {
			t.Fatalf("SaveSettings(PartSizeOverrideMB: %d) returned error: %v", mb, err)
		}

		got, err := deps.settingsSvc.GetSettings()
		if err != nil {
			t.Fatalf("GetSettings() returned error: %v", err)
		}

		if got.PartSizeOverrideMB != 0 {
			t.Errorf("SaveSettings(PartSizeOverrideMB: %d) -> GetSettings().PartSizeOverrideMB = %d, want 0", mb, got.PartSizeOverrideMB)
		}
	}
}

// blockingGetObjectMock is a minimal S3-compatible mock server used by
// TestSaveSettingsAppliesMaxConcurrentTransfersToTransferService: HeadObject
// responds immediately (so downloadRange can learn the object's size/ETag
// and lay out a single, whole-object segment for these small test files),
// but every GetObject request increments startedCount and then blocks on
// release until it is closed - letting the test deterministically observe
// how many downloads TransferService has actually started running at once.
type blockingGetObjectMock struct {
	content []byte
	etag    string

	startedCount atomic.Int64
	release      chan struct{}
}

func newBlockingGetObjectMock(content []byte) *blockingGetObjectMock {
	return &blockingGetObjectMock{
		content: content,
		etag:    "22222222222222222222222222222222",
		release: make(chan struct{}),
	}
}

func (m *blockingGetObjectMock) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, m.etag))

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(m.content)))
		w.WriteHeader(http.StatusOK)

		return
	}

	m.startedCount.Add(1)
	<-m.release

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m.content)
}

// waitForStartedCount polls mock.startedCount until it reaches want, or
// fails the test after timeout.
func waitForStartedCount(t *testing.T, mock *blockingGetObjectMock, want int64, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		if got := mock.startedCount.Load(); got == want {
			return
		} else if got > want {
			t.Fatalf("startedCount = %d, want exactly %d (too many downloads started concurrently)", got, want)
		}

		if time.Now().After(deadline) {
			t.Fatalf("startedCount did not reach %d within %s (got %d)", want, timeout, mock.startedCount.Load())
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// waitForHistoryCount polls transferSvc.GetHistory until at least want
// entries are present, or fails the test after timeout.
func waitForHistoryCount(t *testing.T, transferSvc *transfer.TransferService, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		entries, err := transferSvc.GetHistory(100)
		if err != nil {
			t.Fatalf("GetHistory() returned error: %v", err)
		}

		if len(entries) >= want {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("history did not reach %d entries within %s (got %d: %+v)", want, timeout, len(entries), entries)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// TestSaveSettingsAppliesMaxConcurrentTransfersToTransferService is the
// central "SaveSettings actually pushes into the live TransferService"
// integration test (SettingsService has no exported getters of its own -
// see its doc comment for why it depends on *transfer.TransferService
// directly - so this observes ApplySettings' effect the same way
// internal/transfer's own tests observe TransferService behavior: by
// running real tasks against a controllable mock server).
//
// With the default concurrency limit (transfer.DefaultMaxConcurrentTasks,
// 2), queuing 3 downloads leaves exactly 2 running (blocked mid-GetObject)
// and 1 pending. Calling SaveSettings with MaxConcurrentTransfers raised to
// 3 must immediately let the third download start (SetMaxConcurrentTasks's
// own dispatch() call, invoked via ApplySettings) - observed here as
// mock.startedCount reaching 3 without any further action from the test.
func TestSaveSettingsAppliesMaxConcurrentTransfersToTransferService(t *testing.T) {
	t.Parallel()

	if transfer.DefaultMaxConcurrentTasks != 2 {
		t.Fatalf("this test assumes transfer.DefaultMaxConcurrentTasks == 2, got %d - update the number of queued downloads below", transfer.DefaultMaxConcurrentTasks)
	}

	deps := newTestSettingsService(t)

	mock := newBlockingGetObjectMock([]byte("small test object content"))
	server := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(server.Close)

	profileID := createTestProfile(t, deps.profileRepo, deps.key, server.URL)

	const downloadCount = 3 // DefaultMaxConcurrentTasks + 1

	for i := 0; i < downloadCount; i++ {
		localPath := filepath.Join(t.TempDir(), fmt.Sprintf("downloaded-%d.bin", i))

		if _, err := deps.transferSvc.QueueDownload(domain.DownloadRequest{
			ProfileID: profileID,
			Bucket:    "bucket1",
			Key:       fmt.Sprintf("key%d", i),
			LocalPath: localPath,
		}); err != nil {
			t.Fatalf("QueueDownload(%d) returned error: %v", i, err)
		}
	}

	// Only the default concurrency limit's worth of downloads should have
	// started - the third is left pending.
	waitForStartedCount(t, mock, transfer.DefaultMaxConcurrentTasks, 5*time.Second)

	settings, err := deps.settingsSvc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings() returned error: %v", err)
	}

	settings.MaxConcurrentTransfers = downloadCount

	if err := deps.settingsSvc.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings() returned error: %v", err)
	}

	// The previously-pending third download must now have started, purely
	// as a result of SaveSettings -> ApplySettings ->
	// TransferService.SetMaxConcurrentTasks's own dispatch() call.
	waitForStartedCount(t, mock, downloadCount, 5*time.Second)

	close(mock.release)

	waitForHistoryCount(t, deps.transferSvc, downloadCount, 5*time.Second)
}

// TestApplySettingsAtStartupDoesNotPersist verifies ApplySettings' own
// documented contract - called directly, without a matching SaveSettings,
// it must never write anything to the "settings" table (app.go's newApp
// relies on exactly this: it calls GetSettings then ApplySettings at boot,
// and that sequence must not itself create rows for a database that had
// none).
func TestApplySettingsAtStartupDoesNotPersist(t *testing.T) {
	t.Parallel()

	deps := newTestSettingsService(t)

	settings, err := deps.settingsSvc.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings() returned error: %v", err)
	}

	deps.settingsSvc.ApplySettings(settings)

	for _, key := range []string{
		keyTheme, keyUIScalePercent, keyCloseBehavior, keyAutoResumeQueue,
		keyMaxConcurrentTransfers, keyPartSizeOverrideMB,
		keyBandwidthLimitUploadBPS, keyBandwidthLimitDownloadBPS,
	} {
		if _, err := storage.GetSetting(context.Background(), deps.settingsSvc.db, key); !isErrNoRows(err) {
			t.Errorf("storage.GetSetting(%q) after ApplySettings() (no prior SaveSettings) = (_, %v), want sql.ErrNoRows", key, err)
		}
	}
}

package domain

// AppSettings holds every user-configurable application setting
// (FR-SET-001), persisted as individual key/value rows in the "settings"
// table (internal/appsettings.SettingsService - one exported field, one
// settings key each, not a single JSON blob, mirroring how
// storage.GetOrCreateSetting/GetSetting/SetSetting already work one key at a
// time).
//
// Fields here cover General/Appearance/Transfers per Этап 4 суб-этап 4.3;
// security-related settings (master password) are a separate, later
// суб-этап (4.4) and deliberately not part of this struct yet.
type AppSettings struct {
	// Theme is "system" | "light" | "dark" (FR-SET-001).
	Theme string
	// UIScalePercent is the interface zoom level, 90-125 (UX-спека 5.7);
	// 100 is the unscaled default.
	UIScalePercent int
	// CloseBehavior is "exit" | "confirm" - a systemwide tray-minimize is
	// NOT implemented (see Этап 4 plan's "Согласованные решения" - никакого
	// притворства нерабочей фичи), so this only ever toggles whether
	// closing the window asks for confirmation first.
	CloseBehavior string
	// AutoResumeQueue, when true, makes RecoverOrphanedTasks' recovered
	// (crash-orphaned) tasks resume automatically at the next startup
	// instead of sitting Paused for the user to resume manually
	// (FR-SET-001, Этап 3 plan constraint 4).
	AutoResumeQueue bool
	// MaxConcurrentTransfers is FR-QUEUE-004's parallel-transfer limit
	// (default transfer.DefaultMaxConcurrentTasks).
	MaxConcurrentTransfers int
	// PartSizeOverrideMB is 0 for the adaptive table (transfer.PartSize's
	// default behavior), or a fixed part size in MB (clamped [5,128]) that
	// bypasses it entirely.
	PartSizeOverrideMB int
	// BandwidthLimitUploadBytesPerSec/BandwidthLimitDownloadBytesPerSec are
	// 0 for "unlimited", matching transfer.NewBandwidthLimiter's own
	// <= 0-means-unlimited convention directly (no unit conversion at this
	// boundary - the frontend converts its MB/s input to bytes/sec before
	// calling SaveSettings, so backend code never has to guess units).
	BandwidthLimitUploadBytesPerSec   int64
	BandwidthLimitDownloadBytesPerSec int64
}

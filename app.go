package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"threev/internal/appsettings"
	"threev/internal/config"
	"threev/internal/connection"
	"threev/internal/crypto"
	"threev/internal/filemanager"
	"threev/internal/profiling"
	"threev/internal/s3client"
	"threev/internal/storage"
	"threev/internal/transfer"
)

// wailsConfigJSON embeds wails.json verbatim at compile time, so
// GetAppVersion (below) can read info.productVersion straight from it - the
// exact same field Wails' own build-time templating uses for
// CFBundleShortVersionString/the Windows exe's file version - without a
// second, hand-maintained copy of the version string anywhere in the
// frontend. Previously frontend/src/lib/appVersion.ts hardcoded its own
// "vX.Y.Z" constant, completely independent of wails.json, and silently
// went stale on every version bump until someone remembered to edit it too.
//
//go:embed wails.json
var wailsConfigJSON []byte

// GetAppVersion returns the app's version string (without a leading "v"),
// parsed from the embedded wails.json's info.productVersion - the single
// source of truth for the version baked into every build (also used by
// scripts/package-*.sh/.ps1 for installer filenames and by release.yml's
// check-version job). Returns an error only if wails.json's embedded
// content is somehow malformed, which would indicate a build-time problem,
// not a runtime one.
func (a *App) GetAppVersion() (string, error) {
	var cfg struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}

	if err := json.Unmarshal(wailsConfigJSON, &cfg); err != nil {
		return "", fmt.Errorf("parse embedded wails.json: %w", err)
	}

	return cfg.Info.ProductVersion, nil
}

// pprofAddrEnvVar is the environment variable that, when set to a
// non-empty address (e.g. "localhost:6060"), opts the running app into
// serving profiling.EnableDebugServer's pprof endpoints - see startup's
// doc comment. Left unset (the default for any shipped build), no debug
// server is ever started.
const pprofAddrEnvVar = "THREEV_PPROF_ADDR"

// cryptoSaltSettingKey is the "settings" table key under which the
// randomly-generated Argon2id salt used to derive the credential encryption
// key is persisted, so the same key can be re-derived on every app launch.
const cryptoSaltSettingKey = "crypto_salt"

// App struct
type App struct {
	ctx context.Context

	// db is opened once in NewApp (see newApp) and closed in shutdown. It
	// backs connectionService via a storage.ProfileRepository.
	db *sql.DB

	// connectionService implements docs/02-tech-spec.md section 9.1 and is
	// bound directly to the frontend (see main.go's options.App.Bind).
	connectionService *connection.ConnectionService

	// fileManagerService exposes bucket/object browsing (docs/02-tech-spec.md
	// section 9.2) and is bound directly to the frontend (see main.go's
	// options.App.Bind).
	fileManagerService *filemanager.FileManagerService

	// transferService exposes the upload/download transfer queue
	// (docs/02-tech-spec.md sections 9.3/9.5) and is bound directly to the
	// frontend (see main.go's options.App.Bind).
	transferService *transfer.TransferService

	// settingsService exposes the Settings screen's General/Appearance/
	// Transfers backend (FR-SET-001, Этап 4 суб-этап 4.3) and is bound
	// directly to the frontend (see main.go's options.App.Bind).
	settingsService *appsettings.SettingsService

	// pprofStop, when non-nil, shuts down the opt-in profiling debug
	// server started in startup (see pprofAddrEnvVar). It stays nil - and
	// is simply never called by shutdown - unless THREEV_PPROF_ADDR was
	// set to a non-empty value at startup.
	pprofStop func()
}

// NewApp creates a new App application struct, eagerly opening the SQLite
// database and wiring up every service that needs to be bound to the
// frontend.
//
// This work happens here, in the plain Go constructor, rather than in
// startup (which Wails invokes as an OnStartup callback once the runtime
// window is being created): Wails captures the *values* passed via
// options.App.Bind at the moment wails.Run is called, before OnStartup
// ever runs. If connectionService were only assigned inside startup, the
// Bind slice built in main() would still be holding a nil
// *connection.ConnectionService, and every bound method call from the
// frontend would panic on a nil receiver. Constructing it here guarantees
// a valid, non-nil pointer exists before main() ever builds the Bind
// slice.
//
// There is no clean way to propagate an initialization error out of
// NewApp without changing its Wails-mandated signature, and the
// application cannot function at all without its database, so failure
// here is fatal.
func NewApp() *App {
	app, err := newApp()
	if err != nil {
		log.Fatalf("threev: initialize application: %v", err)
	}

	return app
}

// newApp does the actual work behind NewApp, returning an error instead of
// exiting the process, so the failure path stays testable/inspectable.
func newApp() (*App, error) {
	dbPath, err := config.DBPath()
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	db, err := storage.OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	salt, err := resolveCryptoSalt(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("resolve crypto salt: %w", err)
	}

	// keyBox starts out empty and is shared, as a single *crypto.KeyBox
	// instance, by every service constructed below - see crypto.KeyBox's
	// own doc comment for why a master password (Этап 4 суб-этап 4.4) means
	// the encryption key can no longer be a constructor-time [32]byte
	// constant. It is filled in immediately below if no master password is
	// configured (today's exact pre-4.4 behavior, unchanged), or left empty
	// - deferring every guarded method to domain.ErrLocked - until the
	// frontend calls SettingsService.Unlock (Block I, not yet implemented
	// at the time this code was written).
	keyBox := crypto.NewKeyBox()

	hasPassword, err := appsettings.HasMasterPassword(context.Background(), db)
	if err != nil {
		// Cannot determine whether a master password is configured - safer
		// by default to assume one IS configured (leave keyBox empty,
		// require Unlock) than to risk wrongly unlocking with the
		// machine-only key when a password actually is set (which would be
		// a silent bypass of SEC-001's protection). Log and continue: the
		// user simply sees an UnlockScreen and enters their password (or,
		// if no password was ever actually configured, hits a bug - logged
		// verbosely enough here to be diagnosable in that case).
		log.Printf("threev: check master password: %v", err)
		hasPassword = true
	}

	if !hasPassword {
		machineKey, err := crypto.DeriveKey("", salt)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("derive machine key: %w", err)
		}

		keyBox.Set(machineKey)
	}

	repo := storage.NewProfileRepository(db)

	queueRepo := storage.NewTransferQueueRepository(db)
	historyRepo := storage.NewTransferHistoryRepository(db)
	connMgr := s3client.NewConnectionManager(repo, keyBox)
	breaker := s3client.NewCircuitBreaker()

	transferService := transfer.NewTransferService(repo, keyBox, queueRepo, historyRepo, connMgr, breaker)

	// Read and apply persisted Settings (FR-SET-001, Этап 4 суб-этап 4.3)
	// before RecoverOrphanedTasks below, since AutoResumeIfEnabled needs
	// settings.AutoResumeQueue and the recovered task ids from the very
	// same startup sequence. This is an apply-only step - GetSettings' own
	// result is never written back via SaveSettings here, matching
	// ApplySettings' documented contract (only SaveSettings persists).
	//
	// A GetSettings failure is not fatal (same reasoning as
	// RecoverOrphanedTasks below): the freshly constructed transferService
	// already has every one of its own defaults (DefaultMaxConcurrentTasks,
	// unlimited bandwidth, adaptive part sizing), so simply skipping
	// ApplySettings leaves it exactly as if no Settings screen existed yet.
	// settings itself stays its zero value in that case, so the
	// AutoResumeIfEnabled call below safely defaults to
	// AutoResumeQueue == false.
	//
	// GetSettings/AutoResumeIfEnabled below never touch the encryption key
	// themselves (see TransferService's own doc comment on which of its
	// methods are guarded) - both work identically whether keyBox is
	// currently empty (locked) or already filled: a locked application's
	// AutoResumeIfEnabled simply resumes tasks that then fail fast with
	// domain.ErrLocked via runTask's own guard (see task.go), logged and
	// left "failed" for the user to RetryTask once they unlock - never a
	// crash or a skipped step here.
	settingsService := appsettings.NewSettingsService(db, transferService, repo, keyBox, salt)

	settings, err := settingsService.GetSettings()
	if err != nil {
		log.Printf("threev: read persisted settings: %v", err)
	} else {
		settingsService.ApplySettings(settings)
	}

	// Reconcile any transfer_queue row left "running" by a process that was
	// killed mid-transfer (see RecoverOrphanedTasks' own doc comment) before
	// anything else - including the frontend, once it starts calling
	// GetQueue - ever observes the queue. Unlike a failure to open the
	// database above, this is not fatal to the application: the user can
	// still browse/connect/transfer normally, they would simply need to
	// notice and manually Resume any task left Paused by this step instead
	// of it having been done for them automatically.
	recoveredIDs, err := transferService.RecoverOrphanedTasks()
	if err != nil {
		log.Printf("threev: recover orphaned transfer tasks: %v", err)
	}

	transferService.AutoResumeIfEnabled(recoveredIDs, settings.AutoResumeQueue)

	return &App{
		db:                 db,
		connectionService:  connection.NewConnectionService(repo, keyBox),
		fileManagerService: filemanager.NewFileManagerService(repo, keyBox, connMgr, breaker),
		transferService:    transferService,
		settingsService:    settingsService,
	}, nil
}

// resolveCryptoSalt returns the Argon2id salt used to derive the
// application's credential-encryption key (crypto.DeriveKey), lazily
// creating - on first run only - a random salt and persisting it under
// cryptoSaltSettingKey in the "settings" table via storage.
// GetOrCreateSetting, base64-encoded since the settings.value column is
// TEXT. On every run (including the first), it decodes and returns that
// same salt unchanged.
//
// This used to be the first half of a function (deriveEncryptionKey) that
// also called crypto.DeriveKey("", salt) directly, back when a master
// password was not implemented (Stage 1) and the encryption key was always
// exactly that one, fixed, machine-only derivation. Since Этап 4 суб-этап
// 4.4 lets the key instead depend on a user-supplied master password (an
// entirely separate KeyBox-filling decision - see newApp's own comments for
// exactly when/how it derives either the machine-only key or, later via
// SettingsService.Unlock, a password-derived one), resolveCryptoSalt now
// does only the salt half of that original job: the SAME salt is reused for
// both the machine-only and password-derived key (see crypto.DeriveKey's
// own doc comment for why a single, non-secret Argon2id salt suffices for
// both - a separate "master password salt" is not needed).
func resolveCryptoSalt(db *sql.DB) ([]byte, error) {
	ctx := context.Background()

	saltB64, err := storage.GetOrCreateSetting(ctx, db, cryptoSaltSettingKey, func() (string, error) {
		salt, err := crypto.GenerateSalt()
		if err != nil {
			return "", err
		}

		return base64.StdEncoding.EncodeToString(salt), nil
	})
	if err != nil {
		return nil, fmt.Errorf("get or create crypto salt: %w", err)
	}

	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return nil, fmt.Errorf("decode crypto salt: %w", err)
	}

	return salt, nil
}

// startup is called when the app starts. The context is saved so we can
// call the runtime methods. All database/service wiring already happened
// in NewApp (see its doc comment for why).
//
// It also optionally starts the profiling package's debug pprof server,
// used purely as local developer tooling for Stage 5 AC-005/AC-006 RAM/CPU
// measurement (see profiling's own package doc comment) - never enabled
// unless THREEV_PPROF_ADDR is set to a non-empty address in the process
// environment, which will never be true for a normally-launched, shipped
// build. A failure to start it is logged and otherwise ignored: it is a
// developer convenience, not a capability the application depends on.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.transferService.SetContext(ctx)
	a.fileManagerService.SetContext(ctx)

	if addr := os.Getenv(pprofAddrEnvVar); addr != "" {
		stop, err := profiling.EnableDebugServer(addr)
		if err != nil {
			//nolint:gosec // addr comes from THREEV_PPROF_ADDR, a local developer-set env var, not attacker-controlled input; logged as-is for diagnosability.
			log.Printf("threev: start pprof debug server on %s: %v", addr, err)
		} else {
			a.pprofStop = stop
			//nolint:gosec // see rationale above
			log.Printf("threev: pprof debug server listening on %s", addr)
		}
	}
}

// shutdown is called when the app terminates, releasing the database
// connection opened in NewApp and, if it was started, stopping the pprof
// debug server started in startup.
func (a *App) shutdown(_ context.Context) {
	if a.pprofStop != nil {
		a.pprofStop()
	}

	if a.db == nil {
		return
	}

	if err := a.db.Close(); err != nil {
		log.Printf("threev: close database: %v", err)
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

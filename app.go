package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"

	"threev/internal/config"
	"threev/internal/connection"
	"threev/internal/crypto"
	"threev/internal/filemanager"
	"threev/internal/storage"
)

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

	key, err := deriveEncryptionKey(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}

	repo := storage.NewProfileRepository(db)

	return &App{
		db:                 db,
		connectionService:  connection.NewConnectionService(repo, key),
		fileManagerService: filemanager.NewFileManagerService(repo, key),
	}, nil
}

// deriveEncryptionKey computes the AES-256 key used to encrypt/decrypt
// stored profile credentials (ConnectionService's SecretAccessKey/
// SessionToken fields).
//
// It lazily creates - on first run only - a random Argon2id salt and
// persists it under cryptoSaltSettingKey in the "settings" table via
// storage.GetOrCreateSetting, base64-encoded since the settings.value
// column is TEXT. On every run (including the first), it decodes that
// salt and derives the key via crypto.DeriveKey with an empty passphrase:
// a master password is not implemented in Stage 1 (settled decision), so
// the derived key is machine-specific (crypto.DeriveKey mixes in
// crypto.MachineSeed) but not additionally passphrase-protected.
func deriveEncryptionKey(db *sql.DB) ([32]byte, error) {
	ctx := context.Background()

	saltB64, err := storage.GetOrCreateSetting(ctx, db, cryptoSaltSettingKey, func() (string, error) {
		salt, err := crypto.GenerateSalt()
		if err != nil {
			return "", err
		}

		return base64.StdEncoding.EncodeToString(salt), nil
	})
	if err != nil {
		return [32]byte{}, fmt.Errorf("get or create crypto salt: %w", err)
	}

	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return [32]byte{}, fmt.Errorf("decode crypto salt: %w", err)
	}

	key, err := crypto.DeriveKey("", salt)
	if err != nil {
		return [32]byte{}, fmt.Errorf("derive key: %w", err)
	}

	return key, nil
}

// startup is called when the app starts. The context is saved so we can
// call the runtime methods. All database/service wiring already happened
// in NewApp (see its doc comment for why).
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// shutdown is called when the app terminates, releasing the database
// connection opened in NewApp.
func (a *App) shutdown(_ context.Context) {
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

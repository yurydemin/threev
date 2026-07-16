package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"threev/internal/domain"
	"threev/internal/storage"
)

// ExportProfileEntry is the on-disk representation of one exported
// connection profile (Блок G, "Экспорт/импорт профилей подключений").
//
// Deliberately NOT included: AccessKeyID, SecretAccessKey, SessionToken.
// Credentials are encrypted at rest with a machine-derived key (see
// internal/crypto's own doc comments), so an exported credential would be
// useless outside the machine it was saved on anyway - exporting it, even
// in encrypted form, would be needless exposure for no practical benefit.
// A profile produced by importing this entry therefore always starts with
// blank credentials (domain.ProfileDTO.HasCredentials reports false for it)
// and requires the user to manually edit it and enter real credentials
// before it is usable.
//
// CustomHeaders is exported as-is (raw key/value strings). If a user has
// stashed something sensitive in a custom header value (e.g. an
// Authorization override), that value WILL end up in the exported file
// verbatim. This is a known, accepted limitation, not a bug: the frontend
// is expected to warn the user about this before export - that warning is
// out of scope for this file, which only implements the export/import
// mechanics themselves.
type ExportProfileEntry struct {
	Name          string
	EndpointURL   string
	Region        string
	PathStyle     bool
	VerifySSL     bool
	CustomHeaders map[string]string
}

// ImportResult summarizes the outcome of ImportProfiles/importProfilesFromFile.
type ImportResult struct {
	// ImportedCount is the number of profiles actually created.
	ImportedCount int

	// SkippedNames lists profiles that were skipped because a profile with
	// that exact name (case-sensitive exact match, via the same
	// storage.ProfileRepository.ExistsByName the rest of this package
	// already uses for SaveProfile's own name-uniqueness check) already
	// existed - lets the frontend report specifics beyond a bare count if it
	// wants to (e.g. "повторный импорт того же файла корректно пропускает
	// дубликаты по имени").
	SkippedNames []string
}

// ExportProfiles shows a native "save file" dialog, then writes every saved
// profile's non-secret fields to the chosen path as indented JSON
// ([]ExportProfileEntry - see its own doc comment for exactly which fields
// are, and are not, included).
//
// Unlike GetProfile/SaveProfile, ExportProfiles does not touch keyBox or any
// encrypted field at all: export excludes credentials entirely, so there is
// nothing to decrypt and no domain.ErrLocked guard applies here.
//
// If the dialog is dismissed without a choice, ExportProfiles writes nothing
// and returns nil - the same "empty path back = user cancelled, not an
// error" contract every Pick* dialog method in internal/transfer/dialogs.go
// documents.
func (c *ConnectionService) ExportProfiles() error {
	ctx, err := c.requireWailsContext()
	if err != nil {
		return err
	}

	path, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "Экспорт профилей подключений",
		DefaultFilename: "threev-profiles.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return fmt.Errorf("open export profiles dialog: %w", err)
	}

	if path == "" {
		return nil
	}

	return exportProfilesToFile(context.Background(), c.repo, path)
}

// exportProfilesToFile is ExportProfiles's testable core: it reads every
// profile from repo and writes their non-secret fields to path as indented
// JSON, with file mode 0o600 (not world-readable - CustomHeaders may still
// carry a sensitive value, per ExportProfileEntry's doc comment, even though
// the entry holds zero real credentials by design). ctx is used for the
// repo.GetAll call; ExportProfiles itself always passes context.Background()
// here, matching this package's established convention (see GetProfiles's
// own doc comment) of never plumbing the Wails runtime context - which
// requireWailsContext resolves purely for the dialog itself - into storage
// calls.
func exportProfilesToFile(ctx context.Context, repo *storage.ProfileRepository, path string) error {
	profiles, err := repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("export profiles: %w", err)
	}

	entries := make([]ExportProfileEntry, len(profiles))
	for i, p := range profiles {
		entries[i] = ExportProfileEntry{
			Name:          p.Name,
			EndpointURL:   p.EndpointURL,
			Region:        p.Region,
			PathStyle:     p.PathStyle,
			VerifySSL:     p.VerifySSL,
			CustomHeaders: p.CustomHeaders,
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("export profiles: marshal: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("export profiles: write %s: %w", path, err)
	}

	return nil
}

// ImportProfiles shows a native "select file" dialog, then imports every
// well-formed profile entry from the chosen JSON file (see
// importProfilesFromFile for the full parse/validate/dedup/create logic).
//
// Like ExportProfiles, ImportProfiles never touches keyBox: imported
// profiles are created with blank AccessKeyID/SecretAccessKey/SessionToken,
// so there is nothing to encrypt and no domain.ErrLocked guard applies.
//
// If the dialog is dismissed without a choice, ImportProfiles imports
// nothing and returns a zero ImportResult with a nil error - the same
// "empty path back = user cancelled, not an error" contract ExportProfiles
// documents.
func (c *ConnectionService) ImportProfiles() (ImportResult, error) {
	ctx, err := c.requireWailsContext()
	if err != nil {
		return ImportResult{}, err
	}

	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "Импорт профилей подключений",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf("open import profiles dialog: %w", err)
	}

	if path == "" {
		return ImportResult{}, nil
	}

	return importProfilesFromFile(context.Background(), c.repo, path)
}

// importProfilesFromFile is ImportProfiles's testable core: it reads and
// parses path as a []ExportProfileEntry, then for every entry that passes
// format validation (see below) and is not a duplicate name of an
// already-stored profile, creates a new domain.Profile with blank
// AccessKeyID/SecretAccessKey/SessionToken - deliberately calling
// repo.Create directly rather than (*ConnectionService).SaveProfile, since
// SaveProfile's ValidateProfile call unconditionally rejects an empty
// AccessKeyID/SecretAccessKey, which every imported profile has by design.
//
// ctx is used for the repo.ExistsByName/repo.Create calls; ImportProfiles
// itself always passes context.Background() here, the same convention
// exportProfilesToFile documents.
//
// Format validation, per entry, is deliberately the narrow subset
// ValidateProfile normally enforces MINUS the credential checks (Name is
// not empty, EndpointURL passes validateEndpoint) - calling the full
// ValidateProfile here would reject every entry over its intentionally
// blank credentials. A malformed entry (empty Name or invalid EndpointURL)
// is treated exactly like a duplicate name: its Name is appended to
// SkippedNames (or, for a genuinely un-named entry, a synthesized
// placeholder - see below) and import continues with the next entry, rather
// than aborting the whole file. This choice favors a partial, best-effort
// import over an all-or-nothing one: the plan's own checkpoint only ever
// asks for an "Импортировано: N, пропущено: M" summary, and aborting an
// entire multi-profile import over one bad row would be a worse user
// experience than importing everything else and reporting the rest as
// skipped.
//
// A malformed JSON file itself (path does not parse as a
// []ExportProfileEntry at all) is a hard error: unlike a single bad row
// inside an otherwise well-formed file, there is no reasonable partial
// result to report.
//
// A repo.Create failure for one otherwise-valid, non-duplicate entry is
// likewise skipped-and-continue rather than aborting the rest of the
// import: against a working SQLite file, Create should basically never fail
// for a well-formed profile (the only realistic failure is the name
// uniqueness race covered by the ExistsByName check just before it), so
// treating it as "this one entry didn't make it" and trying every remaining
// entry anyway gives a strictly better result than stopping partway through
// a multi-profile file. Such an entry's name is NOT added to SkippedNames
// (it was neither a validation failure nor a duplicate); it is simply not
// counted in ImportedCount.
func importProfilesFromFile(ctx context.Context, repo *storage.ProfileRepository, path string) (ImportResult, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the user's own OS "select file" dialog choice (ImportProfiles) or a test fixture, not attacker-controlled input
	if err != nil {
		return ImportResult{}, fmt.Errorf("import profiles: read %s: %w", path, err)
	}

	var entries []ExportProfileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return ImportResult{}, fmt.Errorf("import profiles: parse %s: %w", path, err)
	}

	result := ImportResult{SkippedNames: make([]string, 0)}

	for _, entry := range entries {
		if entry.Name == "" || validateEndpoint(entry.EndpointURL) != nil {
			result.SkippedNames = append(result.SkippedNames, invalidEntryLabel(entry))
			continue
		}

		exists, err := repo.ExistsByName(ctx, entry.Name, 0)
		if err != nil {
			return ImportResult{}, fmt.Errorf("import profiles: check name %q: %w", entry.Name, err)
		}

		if exists {
			result.SkippedNames = append(result.SkippedNames, entry.Name)
			continue
		}

		profile := domain.Profile{
			Name:          entry.Name,
			EndpointURL:   entry.EndpointURL,
			Region:        entry.Region,
			PathStyle:     entry.PathStyle,
			VerifySSL:     entry.VerifySSL,
			CustomHeaders: entry.CustomHeaders,
		}

		if _, err := repo.Create(ctx, profile); err != nil {
			continue
		}

		result.ImportedCount++
	}

	return result, nil
}

// invalidEntryLabel returns entry.Name if non-empty, or a placeholder
// identifying entry by its (also invalid, or absent) EndpointURL - used by
// importProfilesFromFile so a malformed entry with a blank Name still
// yields a non-empty, informative SkippedNames element rather than a bare
// "".
func invalidEntryLabel(entry ExportProfileEntry) string {
	if entry.Name != "" {
		return entry.Name
	}

	return fmt.Sprintf("(без имени: %q)", entry.EndpointURL)
}

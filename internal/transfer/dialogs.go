package transfer

import (
	"context"
	"errors"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// errWailsContextNotSet is returned by requireWailsContext (and, through it,
// every Pick* method below) before SetContext has ever been called - unlike
// emitProgressEvent (for which a not-yet-set wailsCtx is an accepted no-op,
// since publishing a progress event is best-effort UI plumbing never
// required for a transfer to run correctly), a system dialog genuinely
// cannot be shown without the real Wails runtime context: there is no
// meaningful fallback behavior, so the caller - ultimately the frontend,
// through the generated binding - must see a real error instead of silently
// hanging or no-op'ing.
var errWailsContextNotSet = errors.New("wails runtime context is not set yet")

// requireWailsContext returns the real Wails runtime context installed by
// SetContext (App.startup), or errWailsContextNotSet if that has not
// happened yet - see its own doc comment for why this, unlike
// emitProgressEvent's identical wailsCtx.Load check, treats an unset
// context as an error rather than a silent no-op.
func (s *TransferService) requireWailsContext() (context.Context, error) {
	holder, ok := s.wailsCtx.Load().(ctxHolder)
	if !ok || holder.ctx == nil {
		return nil, errWailsContextNotSet
	}

	return holder.ctx, nil
}

// PickUploadFiles opens the native "select files" dialog (multiple
// selection enabled) and returns the absolute local paths the user chose,
// or an empty slice (with a nil error) if the dialog was dismissed without
// a selection. The returned paths are suitable, unmodified, as the
// localPaths argument to QueueUploadPaths.
func (s *TransferService) PickUploadFiles() ([]string, error) {
	ctx, err := s.requireWailsContext()
	if err != nil {
		return nil, err
	}

	paths, err := runtime.OpenMultipleFilesDialog(ctx, runtime.OpenDialogOptions{
		Title: "Выберите файлы для загрузки",
	})
	if err != nil {
		return nil, fmt.Errorf("open upload files dialog: %w", err)
	}

	return paths, nil
}

// PickUploadDirectory opens the native "select folder" dialog and returns
// the absolute local path the user chose, or an empty string (with a nil
// error) if the dialog was dismissed without a selection. The returned path
// is suitable, unmodified, as a single element of the localPaths argument
// to QueueUploadPaths (which recursively expands a directory itself).
func (s *TransferService) PickUploadDirectory() (string, error) {
	ctx, err := s.requireWailsContext()
	if err != nil {
		return "", err
	}

	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "Выберите папку для загрузки",
	})
	if err != nil {
		return "", fmt.Errorf("open upload directory dialog: %w", err)
	}

	return path, nil
}

// PickDownloadDestination opens the native "save file" dialog, seeded with
// defaultFilename, and returns the absolute local path the user chose (the
// full path including the file name), or an empty string (with a nil error)
// if the dialog was dismissed without a choice. Intended for downloading a
// single object where the user explicitly picks both the destination
// directory and the file name.
func (s *TransferService) PickDownloadDestination(defaultFilename string) (string, error) {
	ctx, err := s.requireWailsContext()
	if err != nil {
		return "", err
	}

	path, err := runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
		Title:           "Сохранить файл как",
		DefaultFilename: defaultFilename,
	})
	if err != nil {
		return "", fmt.Errorf("open download destination dialog: %w", err)
	}

	return path, nil
}

// PickDownloadDirectory opens the native "select folder" dialog and returns
// the absolute local path the user chose, or an empty string (with a nil
// error) if the dialog was dismissed without a selection. Intended as the
// localDestDir argument to QueueDownloadPrefix (downloading a whole
// bucket/prefix, mirroring its structure into the chosen directory).
func (s *TransferService) PickDownloadDirectory() (string, error) {
	ctx, err := s.requireWailsContext()
	if err != nil {
		return "", err
	}

	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "Выберите папку для сохранения",
	})
	if err != nil {
		return "", fmt.Errorf("open download directory dialog: %w", err)
	}

	return path, nil
}

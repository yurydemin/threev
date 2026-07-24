import { ConfirmClose, GetAppVersion } from '../../../wailsjs/go/main/App';
import { call } from './errors';

/** App version (no leading "v"), read from the embedded wails.json at build time — the single source of truth (see app.go's GetAppVersion doc comment). */
export async function getAppVersion(): Promise<string> {
  return call(() => GetAppVersion());
}

/**
 * Tells the backend the window should actually close — called right before
 * `Quit()` once `useAppCloseConfirm`'s "app:close-requested" handler has
 * decided to exit, so the SECOND `beforeClose(ctx)` call that `Quit()`
 * triggers lets the window close for real instead of vetoing again (see
 * app.go's `ConfirmClose`/`beforeClose` doc comments).
 */
export async function confirmClose(): Promise<void> {
  return call(() => ConfirmClose());
}

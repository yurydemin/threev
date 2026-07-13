import { GetAppVersion } from '../../../wailsjs/go/main/App';
import { call } from './errors';

/** App version (no leading "v"), read from the embedded wails.json at build time — the single source of truth (see app.go's GetAppVersion doc comment). */
export async function getAppVersion(): Promise<string> {
  return call(() => GetAppVersion());
}

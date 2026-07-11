import { create } from 'zustand';
import {
  hasMasterPassword as apiHasMasterPassword,
  setMasterPassword as apiSetMasterPassword,
  removeMasterPassword as apiRemoveMasterPassword,
} from '../lib/wails/appsettings';
import { ApiError } from '../lib/wails/errors';
import { toast } from '../lib/toast';
import i18n from '../i18n';

interface SecurityState {
  /** `null` = not fetched yet (see `SecuritySection`'s loading state). */
  hasMasterPassword: boolean | null;
  isLoading: boolean;
  error: string | null;

  fetchHasMasterPassword: () => Promise<void>;
  /** Returns `true` on success, `false` on failure (see `error` for the message). */
  setMasterPassword: (password: string) => Promise<boolean>;
  /** Returns `true` on success, `false` on failure (see `error` for the message). */
  removeMasterPassword: (currentPassword: string) => Promise<boolean>;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return 'Unknown error';
}

/**
 * Master-password presence/mutation state, backed by
 * `appsettings.SettingsService` via `lib/wails/appsettings.ts`.
 *
 * Unlike `useSettingsStore`, this is fetched lazily by `SecuritySection`
 * itself when the user opens the "Безопасность" section — not eagerly at
 * the `App.tsx` root — since nothing else in the app needs this
 * information before then (see `App.tsx`'s separate `isLocked()` boot-gate
 * check, which is a distinct concept: "is the app locked right now" vs.
 * "has a master password ever been configured").
 */
export const useSecurityStore = create<SecurityState>()((set) => ({
  hasMasterPassword: null,
  isLoading: false,
  error: null,

  fetchHasMasterPassword: async () => {
    set({ isLoading: true, error: null });
    try {
      const hasMasterPassword = await apiHasMasterPassword();
      set({ hasMasterPassword, isLoading: false });
    } catch (err) {
      set({ error: errorMessage(err), isLoading: false });
    }
  },

  setMasterPassword: async (password) => {
    set({ isLoading: true, error: null });
    try {
      await apiSetMasterPassword(password);
      set({ hasMasterPassword: true, isLoading: false });
      toast.success(i18n.t('settings.setPasswordModal.saved'));
      return true;
    } catch (err) {
      const message = errorMessage(err);
      set({ error: message, isLoading: false });
      // No `toast.error` here: the caller (`SetPasswordModal`) keeps the
      // modal open and shows `error` inline instead, so the user can fix
      // their input without re-typing everything — a toast alone would be
      // insufficient (see the modal's own doc comment).
      return false;
    }
  },

  removeMasterPassword: async (currentPassword) => {
    set({ isLoading: true, error: null });
    try {
      await apiRemoveMasterPassword(currentPassword);
      set({ hasMasterPassword: false, isLoading: false });
      toast.success(i18n.t('settings.removePasswordModal.removed'));
      return true;
    } catch (err) {
      const message = errorMessage(err);
      set({ error: message, isLoading: false });
      // Same rationale as `setMasterPassword` above: `RemovePasswordModal`
      // shows `error` inline, no `toast.error` here.
      return false;
    }
  },
}));

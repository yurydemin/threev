import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useTheme } from './hooks/useTheme';
import { useUIScale } from './hooks/useUIScale';
import { useLanguageSync } from './hooks/useLanguageSync';
import { useTransferEvents } from './hooks/useTransferEvents';
import { useSettingsSync } from './hooks/useSettingsSync';
import { useConnectionStore } from './stores/useConnectionStore';
import { useFileManagerStore } from './stores/useFileManagerStore';
import { ConnectionsScreen } from './screens/ConnectionsScreen';
import { FileManagerScreen } from './screens/FileManagerScreen';
import { TransferScreen } from './screens/TransferScreen';
import { HistoryScreen } from './screens/HistoryScreen';
import { SettingsScreen } from './screens/SettingsScreen';
import { WelcomeScreen } from './screens/WelcomeScreen';
import { UnlockScreen } from './screens/UnlockScreen';
import { ToastContainer } from './components/ui/ToastContainer';
import { ConfirmDialog } from './components/ui/ConfirmDialog';
import { confirmDialog } from './lib/confirm';
import { isLocked as apiIsLocked } from './lib/wails/appsettings';
import { getAppVersion } from './lib/wails/app';
import { useAppStore } from './stores/useAppStore';
import type { ConnectionSummary } from './types';

/**
 * Whether the app is gated behind `UnlockScreen` on startup. Checked once,
 * via `appsettings.IsLocked`, before any of the normal `Screen`s render —
 * see the boot-gate `useEffect` below for the fail-open rationale on a
 * check failure.
 */
type BootState = { status: 'checking' } | { status: 'locked' } | { status: 'unlocked' };

/**
 * Top-level navigation state. `connections` covers both the Welcome and
 * Connections screens (which of the two is shown is still decided below by
 * `connections.length`) — `fileManager` is a distinct screen entered by
 * "Подключиться" on a connection card (Stage 2, Block F) — `transfers` is
 * the "Передачи" screen, reachable from the Sidebar of either other screen
 * (Stage 3, Block K) — `settings` is the "Настройки" screen, same
 * reachability (Stage 4, Block G) — `history` is the "История" screen, same
 * reachability, promoted out of the Transfers screen's own tabs into its own
 * top-level Sidebar entry.
 */
type Screen =
  | { name: 'connections' }
  | { name: 'fileManager'; profileId: number; profileName: string }
  | { name: 'transfers' }
  | { name: 'history' }
  | { name: 'settings' };

function App() {
    const { t } = useTranslation();
    useTheme();
    useUIScale();
    useLanguageSync();

    // Mounted unconditionally, once, at the root — regardless of which
    // `screen` is active — so `useTransferStore`'s `queue` (read by the
    // File Manager's `StatusBar` transfer indicator) stays up to date even
    // if the user never opens the Transfers screen. See the hook's own
    // doc-comment for the full rationale.
    useTransferEvents();

    // Same "mount once at the root" rationale as `useTransferEvents` —
    // theme/UI-scale reconciliation with the backend is relevant from
    // startup, on every screen, not just the Settings screen itself.
    useSettingsSync();

    const [boot, setBoot] = useState<BootState>({ status: 'checking' });

    useEffect(() => {
        apiIsLocked()
            .then((locked) => setBoot({ status: locked ? 'locked' : 'unlocked' }))
            .catch((err) => {
                // Fail-open on a plumbing failure (not a wrong password):
                // if a master password is actually set, every backend call
                // that needs the encryption key still enforces `ErrLocked`
                // on its own, so this only avoids a UI deadlock, it doesn't
                // open a real security hole.
                console.error('[App] IsLocked check failed:', err);
                setBoot({ status: 'unlocked' });
            });
    }, []);

    // Fetched once at boot, independent of the lock-gate above — the
    // version display doesn't need credentials/unlock state, just the
    // embedded wails.json (see app.go's GetAppVersion). A fetch failure is
    // silently ignored beyond the console log: useAppStore's appVersion
    // simply stays '', and Sidebar/AboutSection already handle that as
    // "nothing to show yet" rather than crashing.
    useEffect(() => {
        void getAppVersion()
            .then((version) => useAppStore.getState().setAppVersion(`v${version}`))
            .catch((err) => console.error('[App] GetAppVersion failed:', err));
    }, []);

    const connections = useConnectionStore((state) => state.connections);
    const isLoading = useConnectionStore((state) => state.isLoading);
    const fetchConnections = useConnectionStore((state) => state.fetchConnections);
    const [hasFetchedOnce, setHasFetchedOnce] = useState(false);
    const [screen, setScreen] = useState<Screen>({ name: 'connections' });

    useEffect(() => {
        fetchConnections().finally(() => setHasFetchedOnce(true));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    async function handleConnect(connection: ConnectionSummary) {
        // Only reset/reload File Manager state (buckets, navigation history,
        // selection, ...) when actually switching to a *different* profile —
        // re-clicking "Подключиться" on the already-open profile's own card
        // (or navigating back into it) must not blow away where the user
        // currently is, per the Block L2 fix.
        const { activeProfileId, activeProfileName } = useFileManagerStore.getState();
        const isSwitchingToAnotherProfile = activeProfileId !== null && activeProfileId !== connection.id;

        // Switching AWAY from a still-open session (as opposed to entering
        // one for the first time, or just returning to the same one) loses
        // that session's current folder/navigation history/selection — the
        // files themselves aren't touched, only the browsing position — so
        // confirm before discarding it (Stage 4 Block L4). Goes through
        // `confirmDialog` (React-rendered `ConfirmDialog`), not
        // `window.confirm`, which silently no-ops in the packaged WKWebView
        // app — see `useConfirmStore`'s doc comment.
        if (isSwitchingToAnotherProfile) {
            const confirmed = await confirmDialog(
                t('connections.screen.switchSessionConfirm', {
                    currentName: activeProfileName,
                    newName: connection.name,
                }),
            );
            if (!confirmed) return;
        }

        if (activeProfileId !== connection.id) {
            useFileManagerStore.getState().enterProfile(connection.id, connection.name);
        }
        setScreen({ name: 'fileManager', profileId: connection.id, profileName: connection.name });
    }

    // Returns to the already-open File Manager session from the Sidebar's
    // active-connection indicator (Block L2) — reads the still-live
    // `activeProfileId`/`activeProfileName` from the store rather than
    // keeping a second copy of them in `App`'s own state. The `null` guard
    // is defensive only: the indicator that calls this hides itself
    // whenever there's no active profile, so in practice this is never
    // invoked while both are `null`.
    function handleReturnToFileManager() {
        const { activeProfileId, activeProfileName } = useFileManagerStore.getState();
        if (activeProfileId === null || activeProfileName === null) return;
        setScreen({ name: 'fileManager', profileId: activeProfileId, profileName: activeProfileName });
    }

    // Closes the open File Manager session from the Sidebar's
    // active-connection indicator "X" button — unlike `handleConnect`'s
    // switch-session confirm, this is an explicit, self-explanatory,
    // reversible action (the session can simply be reopened from
    // Connections), so it skips `confirmDialog` entirely. Only navigates
    // away if the user is currently looking at the File Manager screen
    // itself; from every other screen, disconnecting just makes the Sidebar
    // indicator disappear in place.
    function handleDisconnect() {
        useFileManagerStore.getState().reset();
        if (screen.name === 'fileManager') {
            setScreen({ name: 'connections' });
        }
    }

    function handleSelectTransfers() {
        setScreen({ name: 'transfers' });
    }

    function handleSelectHistory() {
        setScreen({ name: 'history' });
    }

    function handleSelectConnections() {
        setScreen({ name: 'connections' });
    }

    function handleSelectSettings() {
        setScreen({ name: 'settings' });
    }

    // Until the first fetch settles, prefer the (Sidebar-equipped)
    // connections screen's own skeleton state over flashing the Welcome
    // screen — "no connections" is only meaningful once we actually know.
    const showWelcome = hasFetchedOnce && !isLoading && connections.length === 0;

    if (boot.status === 'checking') {
        // Empty shell in the app's own background color, not `null`, to
        // avoid a white flash before the check resolves.
        return <div className="flex h-screen bg-bg-primary" />;
    }

    if (boot.status === 'locked') {
        return (
            <div className="flex h-screen bg-bg-primary text-fg-primary">
                <UnlockScreen onUnlocked={() => setBoot({ status: 'unlocked' })} />
                <ToastContainer />
                <ConfirmDialog />
            </div>
        );
    }

    return (
        <div className="flex h-screen bg-bg-primary text-fg-primary">
            {screen.name === 'fileManager' ? (
                <FileManagerScreen
                    profileId={screen.profileId}
                    profileName={screen.profileName}
                    onSelectConnections={handleSelectConnections}
                    onSelectTransfers={handleSelectTransfers}
                    onSelectHistory={handleSelectHistory}
                    onSelectSettings={handleSelectSettings}
                    onDisconnect={handleDisconnect}
                />
            ) : screen.name === 'transfers' ? (
                <TransferScreen
                    onSelectConnections={handleSelectConnections}
                    onSelectHistory={handleSelectHistory}
                    onSelectSettings={handleSelectSettings}
                    onSelectFileManager={handleReturnToFileManager}
                    onDisconnect={handleDisconnect}
                />
            ) : screen.name === 'history' ? (
                <HistoryScreen
                    onSelectConnections={handleSelectConnections}
                    onSelectTransfers={handleSelectTransfers}
                    onSelectSettings={handleSelectSettings}
                    onSelectFileManager={handleReturnToFileManager}
                    onDisconnect={handleDisconnect}
                />
            ) : screen.name === 'settings' ? (
                <SettingsScreen
                    onSelectConnections={handleSelectConnections}
                    onSelectTransfers={handleSelectTransfers}
                    onSelectHistory={handleSelectHistory}
                    onSelectFileManager={handleReturnToFileManager}
                    onDisconnect={handleDisconnect}
                />
            ) : showWelcome ? (
                <WelcomeScreen />
            ) : (
                <ConnectionsScreen
                    onConnect={handleConnect}
                    onSelectTransfers={handleSelectTransfers}
                    onSelectHistory={handleSelectHistory}
                    onSelectSettings={handleSelectSettings}
                    onSelectFileManager={handleReturnToFileManager}
                    onDisconnect={handleDisconnect}
                />
            )}
            <ToastContainer />
            <ConfirmDialog />
        </div>
    );
}

export default App;

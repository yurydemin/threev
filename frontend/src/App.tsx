import { useEffect, useState } from 'react';
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
import { SettingsScreen } from './screens/SettingsScreen';
import { WelcomeScreen } from './screens/WelcomeScreen';
import { UnlockScreen } from './screens/UnlockScreen';
import { ToastContainer } from './components/ui/ToastContainer';
import { isLocked as apiIsLocked } from './lib/wails/appsettings';
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
 * reachability (Stage 4, Block G).
 */
type Screen =
  | { name: 'connections' }
  | { name: 'fileManager'; profileId: number; profileName: string }
  | { name: 'transfers' }
  | { name: 'settings' };

function App() {
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

    const connections = useConnectionStore((state) => state.connections);
    const isLoading = useConnectionStore((state) => state.isLoading);
    const fetchConnections = useConnectionStore((state) => state.fetchConnections);
    const [hasFetchedOnce, setHasFetchedOnce] = useState(false);
    const [screen, setScreen] = useState<Screen>({ name: 'connections' });

    useEffect(() => {
        fetchConnections().finally(() => setHasFetchedOnce(true));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    function handleConnect(connection: ConnectionSummary) {
        useFileManagerStore.getState().enterProfile(connection.id, connection.name);
        setScreen({ name: 'fileManager', profileId: connection.id, profileName: connection.name });
    }

    function handleExitFileManager() {
        useFileManagerStore.getState().reset();
        setScreen({ name: 'connections' });
    }

    function handleSelectTransfers() {
        setScreen({ name: 'transfers' });
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
            </div>
        );
    }

    return (
        <div className="flex h-screen bg-bg-primary text-fg-primary">
            {screen.name === 'fileManager' ? (
                <FileManagerScreen
                    profileId={screen.profileId}
                    profileName={screen.profileName}
                    onExit={handleExitFileManager}
                    onSelectTransfers={handleSelectTransfers}
                    onSelectSettings={handleSelectSettings}
                />
            ) : screen.name === 'transfers' ? (
                <TransferScreen onSelectConnections={handleSelectConnections} onSelectSettings={handleSelectSettings} />
            ) : screen.name === 'settings' ? (
                <SettingsScreen onSelectConnections={handleSelectConnections} onSelectTransfers={handleSelectTransfers} />
            ) : showWelcome ? (
                <WelcomeScreen />
            ) : (
                <ConnectionsScreen
                    onConnect={handleConnect}
                    onSelectTransfers={handleSelectTransfers}
                    onSelectSettings={handleSelectSettings}
                />
            )}
            <ToastContainer />
        </div>
    );
}

export default App;

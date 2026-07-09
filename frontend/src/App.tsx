import { useEffect, useState } from 'react';
import { useTheme } from './hooks/useTheme';
import { useConnectionStore } from './stores/useConnectionStore';
import { useFileManagerStore } from './stores/useFileManagerStore';
import { ConnectionsScreen } from './screens/ConnectionsScreen';
import { FileManagerScreen } from './screens/FileManagerScreen';
import { WelcomeScreen } from './screens/WelcomeScreen';
import type { ConnectionSummary } from './types';

/**
 * Top-level navigation state. `connections` covers both the Welcome and
 * Connections screens (which of the two is shown is still decided below by
 * `connections.length`) — `fileManager` is a distinct screen entered by
 * "Подключиться" on a connection card (Stage 2, Block F).
 */
type Screen =
  | { name: 'connections' }
  | { name: 'fileManager'; profileId: number; profileName: string };

function App() {
    useTheme();

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

    // Until the first fetch settles, prefer the (Sidebar-equipped)
    // connections screen's own skeleton state over flashing the Welcome
    // screen — "no connections" is only meaningful once we actually know.
    const showWelcome = hasFetchedOnce && !isLoading && connections.length === 0;

    return (
        <div className="flex h-screen bg-bg-primary text-fg-primary">
            {screen.name === 'fileManager' ? (
                <FileManagerScreen
                    profileId={screen.profileId}
                    profileName={screen.profileName}
                    onExit={handleExitFileManager}
                />
            ) : showWelcome ? (
                <WelcomeScreen />
            ) : (
                <ConnectionsScreen onConnect={handleConnect} />
            )}
        </div>
    );
}

export default App;

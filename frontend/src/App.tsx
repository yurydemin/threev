import { useEffect, useState } from 'react';
import { useTheme } from './hooks/useTheme';
import { useConnectionStore } from './stores/useConnectionStore';
import { ConnectionsScreen } from './screens/ConnectionsScreen';
import { WelcomeScreen } from './screens/WelcomeScreen';

function App() {
    useTheme();

    const connections = useConnectionStore((state) => state.connections);
    const isLoading = useConnectionStore((state) => state.isLoading);
    const fetchConnections = useConnectionStore((state) => state.fetchConnections);
    const [hasFetchedOnce, setHasFetchedOnce] = useState(false);

    useEffect(() => {
        fetchConnections().finally(() => setHasFetchedOnce(true));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    // Until the first fetch settles, prefer the (Sidebar-equipped)
    // connections screen's own skeleton state over flashing the Welcome
    // screen — "no connections" is only meaningful once we actually know.
    const showWelcome = hasFetchedOnce && !isLoading && connections.length === 0;

    return (
        <div className="flex h-screen bg-bg-primary text-fg-primary">
            {showWelcome ? <WelcomeScreen /> : <ConnectionsScreen />}
        </div>
    );
}

export default App;

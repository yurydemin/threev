import {Menu, MenuButton, MenuItem, MenuItems} from '@headlessui/react';
import {Greet} from '../wailsjs/go/main/App';
import {useState} from 'react';

function App() {
    const [resultText, setResultText] = useState('Please enter your name below');
    const [name, setName] = useState('');

    function greet() {
        Greet(name).then(setResultText);
    }

    return (
        <div className="flex h-screen bg-gray-50 text-gray-900 dark:bg-gray-900 dark:text-gray-100">
            <aside className="w-64 shrink-0 border-r border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-950">
                <Menu as="div" className="relative">
                    <MenuButton className="w-full rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-500">
                        Profiles menu
                    </MenuButton>
                    <MenuItems
                        anchor="bottom start"
                        className="mt-1 w-56 rounded-md border border-gray-200 bg-white p-1 shadow-lg focus:outline-none dark:border-gray-800 dark:bg-gray-950"
                    >
                        <MenuItem>
                            <button className="block w-full rounded px-3 py-2 text-left text-sm data-[focus]:bg-gray-100 dark:data-[focus]:bg-gray-800">
                                New profile
                            </button>
                        </MenuItem>
                        <MenuItem>
                            <button className="block w-full rounded px-3 py-2 text-left text-sm data-[focus]:bg-gray-100 dark:data-[focus]:bg-gray-800">
                                Settings
                            </button>
                        </MenuItem>
                    </MenuItems>
                </Menu>
            </aside>
            <main className="flex flex-1 flex-col items-center justify-center gap-4 p-8">
                <h1 className="text-2xl font-semibold">threev</h1>
                <p className="text-sm text-gray-500 dark:text-gray-400">{resultText}</p>
                <div className="flex gap-2">
                    <input
                        className="rounded-md border border-gray-300 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-gray-700 dark:bg-gray-800"
                        name="input"
                        type="text"
                        autoComplete="off"
                        onChange={(e) => setName(e.target.value)}
                    />
                    <button
                        className="rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500"
                        onClick={greet}
                    >
                        Greet
                    </button>
                </div>
            </main>
        </div>
    );
}

export default App;

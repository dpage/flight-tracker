import { useEffect, useState } from 'react';
import { createTheme, type Theme } from '@mui/material';

export type ThemePreference = 'light' | 'dark' | 'system';
export type ThemeMode = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'aerly:theme';

export function createAppTheme(mode: ThemeMode): Theme {
  return createTheme({
    palette: {
      mode,
      primary: { main: '#1f5fa8' },
      secondary: { main: '#d97706' },
      ...(mode === 'light'
        ? { background: { default: '#f5f6fa' } }
        : { background: { default: '#0d1117', paper: '#161b22' } }),
    },
    shape: { borderRadius: 8 },
    typography: {
      fontFamily:
        'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    },
  });
}

export function loadPreference(): ThemePreference {
  try {
    const raw = localStorage.getItem(THEME_STORAGE_KEY);
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw;
  } catch {
    // Ignore storage access failures and fall back to system.
  }
  return 'system';
}

function systemPrefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

export function resolveMode(preference: ThemePreference, systemDark: boolean): ThemeMode {
  if (preference === 'light') return 'light';
  if (preference === 'dark') return 'dark';
  return systemDark ? 'dark' : 'light';
}

// Module-level pub/sub keeps every useThemeMode() consumer in sync — the
// ThemeProvider at the root and the picker inside the account menu both read
// the same value without needing a React context.
type Listener = (p: ThemePreference) => void;
const listeners = new Set<Listener>();
let currentPreference: ThemePreference | null = null;

function getPreference(): ThemePreference {
  if (currentPreference === null) currentPreference = loadPreference();
  return currentPreference;
}

export function setThemePreference(p: ThemePreference): void {
  currentPreference = p;
  try {
    localStorage.setItem(THEME_STORAGE_KEY, p);
  } catch {
    // Ignore persistence failures; keep runtime preference in sync.
  }
  for (const l of listeners) l(p);
}

export function useThemeMode(): {
  preference: ThemePreference;
  mode: ThemeMode;
  setPreference: (p: ThemePreference) => void;
} {
  const [preference, setPref] = useState<ThemePreference>(getPreference);
  const [systemDark, setSystemDark] = useState<boolean>(systemPrefersDark);

  useEffect(() => {
    const onChange: Listener = (p) => setPref(p);
    listeners.add(onChange);
    return () => {
      listeners.delete(onChange);
    };
  }, []);

  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);

  return {
    preference,
    mode: resolveMode(preference, systemDark),
    setPreference: setThemePreference,
  };
}

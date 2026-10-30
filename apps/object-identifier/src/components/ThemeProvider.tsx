"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";

export type ThemeMode = "light" | "dark";

type ThemeContextValue = {
  theme: ThemeMode;
  setTheme: (mode: ThemeMode) => void;
  toggleTheme: () => void;
  ready: boolean;
};

const STORAGE_KEY = "a2n-theme";

const ThemeContext = createContext<ThemeContextValue>({
  theme: "light",
  setTheme: () => undefined,
  toggleTheme: () => undefined,
  ready: false,
});

function applyThemeClass(mode: ThemeMode) {
  const root = document.documentElement;
  root.classList.toggle("dark", mode === "dark");
  root.dataset.theme = mode;
}

function readStoredTheme(): ThemeMode {
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === "dark" || stored === "light") {
    return stored;
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

let currentTheme: ThemeMode = "light";
const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function getClientSnapshot(): ThemeMode {
  return currentTheme;
}

function getServerSnapshot(): ThemeMode {
  return "light";
}

function writeTheme(mode: ThemeMode) {
  currentTheme = mode;
  window.localStorage.setItem(STORAGE_KEY, mode);
  applyThemeClass(mode);
  emit();
}

if (typeof window !== "undefined") {
  currentTheme = readStoredTheme();
  applyThemeClass(currentTheme);
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const theme = useSyncExternalStore(
    subscribe,
    getClientSnapshot,
    getServerSnapshot,
  );

  const setTheme = useCallback((mode: ThemeMode) => {
    writeTheme(mode);
  }, []);

  const toggleTheme = useCallback(() => {
    writeTheme(currentTheme === "dark" ? "light" : "dark");
  }, []);

  const value = useMemo(
    () => ({
      theme,
      setTheme,
      toggleTheme,
      ready: typeof window !== "undefined",
    }),
    [theme, setTheme, toggleTheme],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}

export function useTheme() {
  return useContext(ThemeContext);
}

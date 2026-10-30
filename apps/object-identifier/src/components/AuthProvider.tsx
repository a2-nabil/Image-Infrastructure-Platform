"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { GoogleOAuthProvider } from "@react-oauth/google";
import {
  clearSession,
  loadSession,
  saveSession,
  type AuthSession,
  type AuthUser,
} from "@/lib/auth";
import { clearAllGalleryCaches } from "@/lib/gallery-cache";
import { getGuestSessionID } from "@/lib/image-api";
import {
  claimGuestMedia,
  fetchMe,
  loginWithGoogleIdToken,
  logoutLocal,
} from "@/lib/user-api";

type AuthContextValue = {
  user: AuthUser | null;
  accessToken: string | null;
  loading: boolean;
  claimedCount: number | null;
  signInWithGoogleCredential: (credential: string) => Promise<void>;
  refreshProfile: () => Promise<void>;
  signOut: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return ctx;
}

function AuthStateProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [claimedCount, setClaimedCount] = useState<number | null>(null);

  useEffect(() => {
    let alive = true;
    void (async () => {
      const existing = loadSession();
      if (!alive) {
        return;
      }
      setSession(existing);
      setLoading(false);
      if (!existing) {
        return;
      }
      try {
        const user = await fetchMe();
        if (!alive) {
          return;
        }
        setSession((prev) => (prev ? { ...prev, user } : prev));
        const guest = getGuestSessionID();
        if (guest) {
          try {
            const claim = await claimGuestMedia(guest);
            if (alive) {
              setClaimedCount(claim.claimed_count);
            }
          } catch {
            // Claim is best-effort on session restore.
          }
        }
      } catch {
        if (!alive) {
          return;
        }
        clearSession();
        setSession(null);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  const signInWithGoogleCredential = useCallback(async (credential: string) => {
    setLoading(true);
    try {
      // Drop prior account UI/cache before attaching a new identity.
      clearSession();
      clearAllGalleryCaches();
      setSession(null);
      setClaimedCount(null);
      const next = await loginWithGoogleIdToken(credential);
      // Refresh /users/me so gallery_access / is_super_admin flags are present.
      try {
        const user = await fetchMe();
        next.user = user;
        saveSession({ ...next, user });
        setSession({ ...next, user });
      } catch {
        setSession(next);
      }
      const guest = getGuestSessionID();
      if (guest) {
        const claim = await claimGuestMedia(guest);
        setClaimedCount(claim.claimed_count);
      } else {
        setClaimedCount(0);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshProfile = useCallback(async () => {
    if (!loadSession()) {
      return;
    }
    const user = await fetchMe();
    setSession((prev) => (prev ? { ...prev, user } : prev));
  }, []);

  const signOut = useCallback(() => {
    logoutLocal();
    setSession(null);
    setClaimedCount(null);
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user: session?.user ?? null,
      accessToken: session?.accessToken ?? null,
      loading,
      claimedCount,
      signInWithGoogleCredential,
      refreshProfile,
      signOut,
    }),
    [
      session,
      loading,
      claimedCount,
      signInWithGoogleCredential,
      refreshProfile,
      signOut,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const clientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID || "";

  if (!clientId) {
    return <AuthStateProvider>{children}</AuthStateProvider>;
  }

  return (
    <GoogleOAuthProvider clientId={clientId}>
      <AuthStateProvider>{children}</AuthStateProvider>
    </GoogleOAuthProvider>
  );
}

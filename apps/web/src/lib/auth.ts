export type AuthUser = {
  id: string;
  email: string;
  name?: string | null;
  avatar_url?: string | null;
  google_id?: string;
  created_at?: string;
  /** Personal gallery (/gallery + gallery APIs). */
  gallery_access?: boolean;
  /** Hard-coded gallery super admin (a2nabil000@gmail.com). */
  is_super_admin?: boolean;
};

/** Canonical super admin — backend also enforces via GALLERY_SUPER_ADMIN_EMAIL. */
export const GALLERY_SUPER_ADMIN_EMAIL = "a2nabil000@gmail.com";

export function userHasGalleryAccess(user: AuthUser | null | undefined): boolean {
  if (!user?.email) {
    return false;
  }
  if (user.gallery_access === true || user.is_super_admin === true) {
    return true;
  }
  return user.email.toLowerCase() === GALLERY_SUPER_ADMIN_EMAIL;
}

export type AuthSession = {
  accessToken: string;
  tokenType: string;
  expiresIn: number;
  expiresAt: number;
  user: AuthUser;
};

const SESSION_KEY = "image_api_auth_session";

export function loadSession(): AuthSession | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    const raw = window.localStorage.getItem(SESSION_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as AuthSession;
    if (!parsed?.accessToken || !parsed?.user?.id) {
      return null;
    }
    if (parsed.expiresAt && Date.now() > parsed.expiresAt) {
      clearSession();
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

export function saveSession(session: AuthSession): void {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.setItem(SESSION_KEY, JSON.stringify(session));
}

export function clearSession(): void {
  if (typeof window === "undefined") {
    return;
  }
  window.localStorage.removeItem(SESSION_KEY);
}

export function getAccessToken(): string | null {
  return loadSession()?.accessToken ?? null;
}

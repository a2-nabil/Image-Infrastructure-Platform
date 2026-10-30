"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { GoogleLogin } from "@react-oauth/google";
import {
  Home,
  ImageIcon,
  Images,
  LayoutDashboard,
  LogOut,
  Menu,
  Moon,
  MoreVertical,
  RefreshCw,
  ScanSearch,
  Sparkles,
  Sun,
} from "lucide-react";
import { useAuth } from "@/components/AuthProvider";
import PhotosSheet from "@/components/PhotosSheet";
import { useTheme } from "@/components/ThemeProvider";
import { userHasGalleryAccess } from "@/lib/auth";

const BASE_NAV_ITEMS = [
  { href: "/", label: "Home", icon: Home },
  {
    href: "/converter",
    label: "Converter",
    icon: ImageIcon,
    category: "Tools",
  },
  {
    href: "/identify",
    label: "Object Identifier",
    icon: ScanSearch,
    category: "Tools",
  },
] as const;

export default function SiteHeader() {
  const router = useRouter();
  const pathname = usePathname();
  const { theme, toggleTheme } = useTheme();
  const { user, loading, signInWithGoogleCredential, signOut, claimedCount } =
    useAuth();
  const galleryAllowed = userHasGalleryAccess(user);
  const navItems = BASE_NAV_ITEMS;
  const googleConfigured = Boolean(process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID);
  const hiddenGoogleRef = useRef<HTMLDivElement>(null);
  const [authBusy, setAuthBusy] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const [navOpen, setNavOpen] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const profileMenuRef = useRef<HTMLDivElement>(null);
  const switchAccountRef = useRef(false);

  const triggerGoogleSignIn = () => {
    setAuthError(null);
    const btn = hiddenGoogleRef.current?.querySelector(
      'div[role="button"]',
    ) as HTMLElement | null;
    if (!btn) {
      setAuthError(
        googleConfigured
          ? "Google sign-in is still loading. Try again."
          : "Set NEXT_PUBLIC_GOOGLE_CLIENT_ID",
      );
      return;
    }
    btn.click();
  };

  const onDashboardClick = () => {
    if (loading || authBusy) {
      return;
    }
    triggerGoogleSignIn();
  };

  const onSwitchAccount = () => {
    setProfileOpen(false);
    switchAccountRef.current = true;
    signOut();
  };

  useEffect(() => {
    if (!switchAccountRef.current || user || loading) {
      return;
    }
    switchAccountRef.current = false;
    // Next tick so Google button remounts after sign-out.
    const t = window.setTimeout(() => {
      triggerGoogleSignIn();
    }, 50);
    return () => window.clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only after switch-account sign-out
  }, [user, loading]);

  useEffect(() => {
    if (!profileOpen) {
      return;
    }
    const onPointerDown = (e: MouseEvent | PointerEvent) => {
      const root = profileMenuRef.current;
      if (root && !root.contains(e.target as Node)) {
        setProfileOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setProfileOpen(false);
      }
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [profileOpen]);

  const isActive = (href: string) => {
    if (href === "/") {
      return pathname === "/";
    }
    return pathname === href || pathname.startsWith(`${href}/`);
  };

  return (
    <header className="sticky top-0 z-40 border-b border-slate-200 bg-white/95 backdrop-blur-md dark:border-slate-800 dark:bg-[#0b1220]/90">
      <div className="mx-auto flex max-w-6xl items-center justify-between gap-3 px-4 py-3">
        <div className="flex min-w-0 items-center gap-2 sm:gap-3">
          <button
            type="button"
            onClick={() => setNavOpen(true)}
            className="inline-flex cursor-pointer items-center justify-center rounded-xl border border-slate-200 bg-white p-2 text-slate-700 shadow-sm transition hover:bg-slate-50 md:hidden dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
            aria-label="Open navigation"
          >
            <Menu className="h-4 w-4" />
          </button>

          <Link href="/" className="group flex cursor-pointer items-center gap-2">
            <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-slate-900 text-white shadow-sm transition group-hover:scale-[1.03] dark:bg-violet-600">
              <Sparkles className="h-4 w-4" />
            </span>
            <div className="leading-tight">
              <p className="font-[family-name:var(--font-display)] text-lg tracking-tight text-slate-900 dark:text-white">
                A2N Gallery
              </p>
              <p className="hidden text-xs text-slate-500 sm:block dark:text-slate-400">
                Identify · convert · own
              </p>
            </div>
          </Link>

          <nav className="ml-2 hidden items-center gap-1 md:flex" aria-label="Primary">
            {navItems.map((item) => {
              const active = isActive(item.href);
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={`inline-flex cursor-pointer items-center gap-1.5 rounded-xl px-3 py-2 text-sm font-semibold transition ${
                    active
                      ? "bg-slate-900 text-white dark:bg-white dark:text-slate-900"
                      : "text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white"
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>

        <div className="flex items-center gap-2 sm:gap-3">
          <button
            type="button"
            onClick={toggleTheme}
            className="inline-flex cursor-pointer items-center gap-1.5 rounded-xl border border-slate-200 bg-white px-2.5 py-2 text-slate-700 shadow-sm transition hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800"
            aria-label={
              theme === "dark" ? "Switch to white theme" : "Switch to dark theme"
            }
            title={theme === "dark" ? "White" : "Dark"}
          >
            {theme === "dark" ? (
              <Sun className="h-4 w-4" />
            ) : (
              <Moon className="h-4 w-4" />
            )}
            <span className="hidden text-xs font-semibold sm:inline">
              {theme === "dark" ? "White" : "Dark"}
            </span>
          </button>

          {user ? (
            <div ref={profileMenuRef} className="relative">
              <div className="inline-flex items-center gap-1 rounded-xl border border-slate-200 bg-white py-1 pl-1.5 pr-1 shadow-sm dark:border-slate-700 dark:bg-slate-900">
                <button
                  type="button"
                  onClick={() => {
                    setProfileOpen(false);
                    router.push("/dashboard");
                  }}
                  className="inline-flex min-w-0 cursor-pointer items-center gap-2 rounded-lg px-1 py-0.5 transition hover:bg-slate-50 dark:hover:bg-slate-800"
                  aria-label="Open dashboard"
                  title="Dashboard"
                >
                  {user.avatar_url ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      src={user.avatar_url}
                      alt=""
                      className="h-8 w-8 rounded-full object-cover"
                      referrerPolicy="no-referrer"
                    />
                  ) : (
                    <span className="flex h-8 w-8 items-center justify-center rounded-full bg-slate-200 text-xs font-semibold text-slate-700 dark:bg-slate-700 dark:text-slate-200">
                      {(user.name || user.email || "?")
                        .slice(0, 1)
                        .toUpperCase()}
                    </span>
                  )}
                  <div className="hidden min-w-0 max-w-[10rem] text-left sm:block">
                    <p className="truncate text-sm font-medium text-slate-900 dark:text-white">
                      {user.name || "Profile"}
                    </p>
                    <p className="truncate text-[11px] text-slate-500 dark:text-slate-400">
                      {user.email}
                    </p>
                  </div>
                </button>
                <button
                  type="button"
                  onClick={() => setProfileOpen((open) => !open)}
                  className="inline-flex cursor-pointer items-center justify-center rounded-lg p-1.5 text-slate-500 transition hover:bg-slate-100 hover:text-slate-800 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
                  aria-label="Account options"
                  aria-expanded={profileOpen}
                  aria-haspopup="menu"
                  title="Account"
                >
                  <MoreVertical className="h-4 w-4" />
                </button>
              </div>

              {profileOpen ? (
                <div
                  role="menu"
                  className="absolute right-0 top-[calc(100%+0.4rem)] z-50 w-48 overflow-hidden rounded-xl border border-slate-200 bg-white py-1 shadow-xl dark:border-slate-700 dark:bg-slate-900"
                >
                  {galleryAllowed ? (
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        setProfileOpen(false);
                        router.push("/gallery");
                      }}
                      className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm font-medium text-slate-800 transition hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
                    >
                      <Images className="h-3.5 w-3.5" />
                      Gallery
                    </button>
                  ) : null}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={onSwitchAccount}
                    className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm font-medium text-slate-800 transition hover:bg-slate-50 dark:text-slate-100 dark:hover:bg-slate-800"
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                    Switch account
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      setProfileOpen(false);
                      signOut();
                    }}
                    className="flex w-full cursor-pointer items-center gap-2 px-3 py-2.5 text-left text-sm font-medium text-rose-700 transition hover:bg-rose-50 dark:text-rose-300 dark:hover:bg-rose-950/40"
                  >
                    <LogOut className="h-3.5 w-3.5" />
                    Log out
                  </button>
                </div>
              ) : null}
            </div>
          ) : (
            <button
              type="button"
              onClick={onDashboardClick}
              disabled={loading || authBusy}
              className="inline-flex cursor-pointer items-center gap-1.5 rounded-xl bg-slate-900 px-3 py-2 text-sm font-medium text-white shadow-sm transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-violet-600 dark:hover:bg-violet-500"
            >
              <LayoutDashboard className="h-4 w-4" />
              <span>{authBusy ? "Signing in…" : "Dashboard"}</span>
            </button>
          )}

          {googleConfigured ? (
            <div
              ref={hiddenGoogleRef}
              className="pointer-events-none absolute h-0 w-0 overflow-hidden opacity-0"
              aria-hidden="true"
            >
              <GoogleLogin
                onSuccess={(res) => {
                  if (!res.credential) {
                    return;
                  }
                  setAuthBusy(true);
                  void signInWithGoogleCredential(res.credential)
                    .then(() => {
                      router.push("/dashboard");
                    })
                    .catch((err: unknown) => {
                      setAuthError(
                        err instanceof Error
                          ? err.message
                          : "Google sign-in failed",
                      );
                    })
                    .finally(() => {
                      setAuthBusy(false);
                    });
                }}
                onError={() => {
                  setAuthError("Google sign-in was cancelled or failed");
                }}
                useOneTap={false}
                theme="outline"
                size="medium"
                shape="pill"
                text="signin_with"
              />
            </div>
          ) : null}
        </div>
      </div>

      {authError && (
        <div className="border-t border-amber-100 bg-amber-50 px-4 py-2 text-center text-sm text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/40 dark:text-amber-100">
          {authError}
        </div>
      )}
      {!loading && claimedCount !== null && claimedCount > 0 && (
        <div className="border-t border-emerald-100 bg-emerald-50 px-4 py-2 text-center text-sm text-emerald-800 dark:border-emerald-900/40 dark:bg-emerald-950/40 dark:text-emerald-100">
          Claimed {claimedCount} guest upload{claimedCount === 1 ? "" : "s"} into
          your account.
        </div>
      )}

      <PhotosSheet
        open={navOpen}
        onClose={() => setNavOpen(false)}
        title="Navigate"
        subtitle="Home and tool categories"
      >
        <div className="space-y-4">
          <div>
            <p className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Home
            </p>
            <Link
              href="/"
              onClick={() => setNavOpen(false)}
              className={`flex cursor-pointer items-center gap-3 rounded-xl px-3 py-3 text-sm font-semibold transition ${
                isActive("/")
                  ? "bg-slate-900 text-white dark:bg-white dark:text-slate-900"
                  : "text-slate-800 hover:bg-slate-100 dark:text-slate-100 dark:hover:bg-white/10"
              }`}
            >
              <Home className="h-4 w-4" />
              Home
            </Link>
          </div>
          <div>
            <p className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Tools
            </p>
            <div className="space-y-1">
              {navItems
                .filter((i) => i.href !== "/")
                .map((item) => {
                const Icon = item.icon;
                const active = isActive(item.href);
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    onClick={() => setNavOpen(false)}
                    className={`flex cursor-pointer items-center gap-3 rounded-xl px-3 py-3 text-sm font-semibold transition ${
                      active
                        ? "bg-slate-900 text-white dark:bg-white dark:text-slate-900"
                        : "text-slate-800 hover:bg-slate-100 dark:text-slate-100 dark:hover:bg-white/10"
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </Link>
                );
              })}
            </div>
          </div>
        </div>
      </PhotosSheet>

    </header>
  );
}

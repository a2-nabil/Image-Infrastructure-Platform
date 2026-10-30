"use client";

import { useEffect, useSyncExternalStore, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";

function subscribeNever() {
  return () => {};
}

type PhotosSheetProps = {
  open: boolean;
  onClose: () => void;
  title: string;
  subtitle?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  /**
   * Force dark surface (e.g. gallery viewer).
   * When omitted, follows site light/dark theme.
   */
  dark?: boolean;
  labelledBy?: string;
};

/**
 * Responsive overlay:
 * - Mobile: Google Photos bottom sheet (photos-sheet)
 * - Desktop (md+): centered professional modal (photos-modal)
 *
 * Portaled to document.body so sticky headers cannot trap stacking context.
 */
export default function PhotosSheet({
  open,
  onClose,
  title,
  subtitle,
  children,
  footer,
  dark,
  labelledBy,
}: PhotosSheetProps) {
  const mounted = useSyncExternalStore(subscribeNever, () => true, () => false);

  useEffect(() => {
    if (!open) {
      return;
    }
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopImmediatePropagation();
        onClose();
      }
    };
    // Capture so overlays win over page-level Escape handlers (e.g. gallery viewer).
    window.addEventListener("keydown", onKey, true);
    return () => {
      document.body.style.overflow = prevOverflow;
      window.removeEventListener("keydown", onKey, true);
    };
  }, [open, onClose]);

  if (!open || !mounted) {
    return null;
  }

  const forceDark = dark === true;
  const forceLight = dark === false;

  const panelTone = forceDark
    ? "bg-[#1e1e1e] text-white"
    : forceLight
      ? "bg-white text-slate-900"
      : "bg-white text-slate-900 dark:bg-[#1e1e1e] dark:text-white";

  const handleTone = forceDark
    ? "bg-white/25"
    : forceLight
      ? "bg-slate-300"
      : "bg-slate-300 dark:bg-white/25";

  const titleTone = forceDark
    ? "text-white"
    : forceLight
      ? "text-slate-900"
      : "text-slate-900 dark:text-white";

  const subTone = forceDark
    ? "text-white/60"
    : forceLight
      ? "text-slate-500"
      : "text-slate-500 dark:text-white/60";

  const closeTone = forceDark
    ? "text-white/70 hover:bg-white/10"
    : forceLight
      ? "text-slate-500 hover:bg-slate-100"
      : "text-slate-500 hover:bg-slate-100 dark:text-white/70 dark:hover:bg-white/10";

  const backdropTone = forceDark
    ? "bg-black/50"
    : forceLight
      ? "bg-slate-900/40"
      : "bg-slate-900/40 dark:bg-black/60";

  const borderTone = forceDark
    ? "border-white/10"
    : forceLight
      ? "border-slate-200"
      : "border-slate-200 dark:border-white/10";

  return createPortal(
    <div
      className="fixed inset-0 z-[200] flex flex-col justify-end md:items-center md:justify-center md:p-6"
      role="dialog"
      aria-modal="true"
      aria-labelledby={labelledBy}
    >
      <button
        type="button"
        className={`absolute inset-0 cursor-pointer ${backdropTone}`}
        aria-label="Dismiss"
        onClick={onClose}
      />

      {/* Mobile bottom sheet */}
      <div
        className={`photos-sheet relative z-10 max-h-[78dvh] w-full overflow-y-auto rounded-t-3xl px-4 pb-[max(1rem,env(safe-area-inset-bottom))] pt-3 shadow-2xl md:hidden ${panelTone}`}
      >
        <div className={`mx-auto mb-3 h-1 w-10 rounded-full ${handleTone}`} />
        <SheetHeader
          title={title}
          subtitle={subtitle}
          labelledBy={labelledBy}
          onClose={onClose}
          titleTone={titleTone}
          subTone={subTone}
          closeTone={closeTone}
        />
        <div>{children}</div>
        {footer ? <div className="mt-5">{footer}</div> : null}
      </div>

      {/* Desktop professional modal */}
      <div
        className={`photos-modal relative z-10 hidden max-h-[min(85dvh,40rem)] w-full max-w-lg overflow-hidden rounded-2xl border shadow-[0_24px_80px_-12px_rgba(15,23,42,0.35)] md:flex md:flex-col ${panelTone} ${borderTone}`}
      >
        <div className={`shrink-0 border-b px-6 py-5 ${borderTone}`}>
          <SheetHeader
            title={title}
            subtitle={subtitle}
            labelledBy={labelledBy ? `${labelledBy}-desktop` : undefined}
            onClose={onClose}
            titleTone={titleTone}
            subTone={subTone}
            closeTone={closeTone}
            desktop
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">{children}</div>
        {footer ? (
          <div className={`shrink-0 border-t px-6 py-4 ${borderTone}`}>
            {footer}
          </div>
        ) : null}
      </div>
    </div>,
    document.body,
  );
}

function SheetHeader({
  title,
  subtitle,
  labelledBy,
  onClose,
  titleTone,
  subTone,
  closeTone,
  desktop,
}: {
  title: string;
  subtitle?: ReactNode;
  labelledBy?: string;
  onClose: () => void;
  titleTone: string;
  subTone: string;
  closeTone: string;
  desktop?: boolean;
}) {
  return (
    <div className="mb-3 flex items-start justify-between gap-3 md:mb-0">
      <div className="min-w-0">
        <h3
          id={labelledBy}
          className={`font-semibold tracking-tight ${titleTone} ${
            desktop ? "text-lg" : "text-base"
          }`}
        >
          {title}
        </h3>
        {subtitle ? (
          <div className={`mt-0.5 truncate text-sm ${subTone}`}>{subtitle}</div>
        ) : null}
      </div>
      <button
        type="button"
        onClick={onClose}
        className={`inline-flex shrink-0 cursor-pointer rounded-full p-2 ${closeTone}`}
        aria-label="Close"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}

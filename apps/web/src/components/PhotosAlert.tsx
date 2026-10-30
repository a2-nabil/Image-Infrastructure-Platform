"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { AlertTriangle, CheckCircle2, Info, Trash2 } from "lucide-react";
import PhotosSheet from "@/components/PhotosSheet";

export type AlertTone = "default" | "danger" | "warning" | "success";

export type ConfirmOptions = {
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: AlertTone;
  /** Force dark surface (gallery viewer). */
  dark?: boolean;
};

export type AlertOptions = {
  title: string;
  message: ReactNode;
  okLabel?: string;
  tone?: AlertTone;
  dark?: boolean;
};

type AlertContextValue = {
  confirm: (options: ConfirmOptions) => Promise<boolean>;
  alert: (options: AlertOptions) => Promise<void>;
};

type ActiveDialog =
  | {
      kind: "confirm";
      options: ConfirmOptions;
      resolve: (value: boolean) => void;
    }
  | {
      kind: "alert";
      options: AlertOptions;
      resolve: () => void;
    };

const AlertContext = createContext<AlertContextValue | null>(null);

function toneMeta(tone: AlertTone) {
  switch (tone) {
    case "danger":
      return {
        Icon: Trash2,
        iconWrap:
          "bg-rose-100 text-rose-700 dark:bg-rose-950/60 dark:text-rose-300",
        iconWrapDark: "bg-rose-500/20 text-rose-200",
        confirmBtn:
          "bg-rose-600 text-white hover:bg-rose-500 dark:bg-rose-600 dark:hover:bg-rose-500",
        confirmBtnDark: "bg-rose-600 text-white hover:bg-rose-500",
      };
    case "warning":
      return {
        Icon: AlertTriangle,
        iconWrap:
          "bg-amber-100 text-amber-800 dark:bg-amber-950/50 dark:text-amber-200",
        iconWrapDark: "bg-amber-500/20 text-amber-200",
        confirmBtn:
          "bg-slate-900 text-white hover:bg-slate-800 dark:bg-white dark:text-slate-900 dark:hover:bg-slate-100",
        confirmBtnDark: "bg-white text-slate-900 hover:bg-white/90",
      };
    case "success":
      return {
        Icon: CheckCircle2,
        iconWrap:
          "bg-emerald-100 text-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-200",
        iconWrapDark: "bg-emerald-500/20 text-emerald-200",
        confirmBtn:
          "bg-slate-900 text-white hover:bg-slate-800 dark:bg-white dark:text-slate-900 dark:hover:bg-slate-100",
        confirmBtnDark: "bg-white text-slate-900 hover:bg-white/90",
      };
    default:
      return {
        Icon: Info,
        iconWrap:
          "bg-sky-100 text-sky-800 dark:bg-sky-950/50 dark:text-sky-200",
        iconWrapDark: "bg-sky-500/20 text-sky-200",
        confirmBtn:
          "bg-slate-900 text-white hover:bg-slate-800 dark:bg-white dark:text-slate-900 dark:hover:bg-slate-100",
        confirmBtnDark: "bg-white text-slate-900 hover:bg-white/90",
      };
  }
}

export function AlertProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState<ActiveDialog | null>(null);
  const queueRef = useRef<ActiveDialog[]>([]);

  const enqueue = useCallback((dialog: ActiveDialog) => {
    setActive((current) => {
      if (current) {
        queueRef.current.push(dialog);
        return current;
      }
      return dialog;
    });
  }, []);

  const finish = useCallback((fn: () => void) => {
    fn();
    setActive(() => queueRef.current.shift() ?? null);
  }, []);

  const confirm = useCallback(
    (options: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        enqueue({ kind: "confirm", options, resolve });
      }),
    [enqueue],
  );

  const alert = useCallback(
    (options: AlertOptions) =>
      new Promise<void>((resolve) => {
        enqueue({ kind: "alert", options, resolve });
      }),
    [enqueue],
  );

  const value = useMemo(() => ({ confirm, alert }), [confirm, alert]);

  const tone = active?.options.tone ?? "default";
  const dark = active?.options.dark === true;
  const meta = toneMeta(tone);
  const Icon = meta.Icon;

  const messageTone = dark
    ? "text-white/75"
    : "text-slate-600 dark:text-slate-300";
  const cancelBtn = dark
    ? "border border-white/15 bg-white/5 text-white hover:bg-white/10"
    : "border border-slate-200 bg-white text-slate-800 hover:bg-slate-50 dark:border-slate-600 dark:bg-slate-900 dark:text-white dark:hover:bg-slate-800";
  const primaryBtn = dark ? meta.confirmBtnDark : meta.confirmBtn;
  const iconWrap = dark ? meta.iconWrapDark : meta.iconWrap;

  const title =
    active?.kind === "confirm"
      ? active.options.title
      : active?.kind === "alert"
        ? active.options.title
        : "";

  const message =
    active?.kind === "confirm"
      ? active.options.message
      : active?.kind === "alert"
        ? active.options.message
        : null;

  const confirmLabel =
    active?.kind === "confirm"
      ? (active.options.confirmLabel ?? "Confirm")
      : active?.kind === "alert"
        ? (active.options.okLabel ?? "OK")
        : "OK";

  const cancelLabel =
    active?.kind === "confirm"
      ? (active.options.cancelLabel ?? "Cancel")
      : "Cancel";

  return (
    <AlertContext.Provider value={value}>
      {children}
      <PhotosSheet
        open={Boolean(active)}
        onClose={() => {
          if (!active) {
            return;
          }
          if (active.kind === "confirm") {
            finish(() => active.resolve(false));
          } else {
            finish(() => active.resolve());
          }
        }}
        title={title}
        dark={dark || undefined}
        labelledBy="photos-alert-title"
        footer={
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            {active?.kind === "confirm" ? (
              <button
                type="button"
                className={`inline-flex cursor-pointer items-center justify-center rounded-full px-4 py-2.5 text-sm font-semibold ${cancelBtn}`}
                onClick={() => finish(() => active.resolve(false))}
              >
                {cancelLabel}
              </button>
            ) : null}
            <button
              type="button"
              className={`inline-flex cursor-pointer items-center justify-center rounded-full px-4 py-2.5 text-sm font-semibold ${primaryBtn}`}
              onClick={() => {
                if (!active) {
                  return;
                }
                if (active.kind === "confirm") {
                  finish(() => active.resolve(true));
                } else {
                  finish(() => active.resolve());
                }
              }}
            >
              {confirmLabel}
            </button>
          </div>
        }
      >
        <div className="flex gap-3">
          <div
            className={`mt-0.5 inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-full ${iconWrap}`}
            aria-hidden
          >
            <Icon className="h-5 w-5" />
          </div>
          <div className={`min-w-0 text-sm leading-relaxed ${messageTone}`}>
            {message}
          </div>
        </div>
      </PhotosSheet>
    </AlertContext.Provider>
  );
}

export function usePhotosAlert(): AlertContextValue {
  const ctx = useContext(AlertContext);
  if (!ctx) {
    throw new Error("usePhotosAlert must be used within AlertProvider");
  }
  return ctx;
}

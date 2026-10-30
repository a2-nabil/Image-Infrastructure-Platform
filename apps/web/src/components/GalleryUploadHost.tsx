"use client";

import { useEffect, useState } from "react";
import { CheckCircle2, Loader2, X } from "lucide-react";
import { useAuth } from "@/components/AuthProvider";
import {
  galleryUploadQueue,
  type GalleryUploadJob,
} from "@/lib/gallery-upload-queue";
import { formatBytes } from "@/lib/image-api";

/**
 * Layout-level upload tray. Keeps the queue UI (and auth binding) alive across
 * /gallery → /dashboard and other client navigations so background uploads continue.
 */
export default function GalleryUploadHost() {
  const { user } = useAuth();
  const [jobs, setJobs] = useState<GalleryUploadJob[]>([]);
  const [panelOpen, setPanelOpen] = useState(false);
  const [banner, setBanner] = useState<string | null>(null);

  useEffect(() => {
    galleryUploadQueue.configure({
      userId: user?.id ?? null,
    });
  }, [user?.id]);

  useEffect(() => {
    const sync = () => {
      const snap = galleryUploadQueue.snapshot();
      setJobs(snap.jobs);
      if (snap.uploading || snap.jobs.some((j) => j.status === "error")) {
        setPanelOpen(true);
      }
    };
    sync();
    galleryUploadQueue.addEventListener("change", sync);
    const onErr = (event: Event) => {
      const detail = (event as CustomEvent<{ message?: string }>).detail;
      if (detail?.message) {
        setBanner(detail.message);
      }
    };
    galleryUploadQueue.addEventListener("fileerror", onErr);
    return () => {
      galleryUploadQueue.removeEventListener("change", sync);
      galleryUploadQueue.removeEventListener("fileerror", onErr);
    };
  }, []);

  const activeUploads = jobs.filter(
    (j) => j.status === "queued" || j.status === "uploading",
  ).length;
  const doneUploads = jobs.filter((j) => j.status === "done").length;

  if (jobs.length === 0) {
    return banner ? (
      <div className="fixed bottom-4 left-1/2 z-[70] max-w-md -translate-x-1/2 rounded-full bg-rose-600 px-4 py-2 text-sm font-medium text-white shadow-lg">
        {banner}
        <button
          type="button"
          className="ml-3 underline"
          onClick={() => setBanner(null)}
        >
          Dismiss
        </button>
      </div>
    ) : null;
  }

  return (
    <>
      {banner ? (
        <div className="fixed bottom-24 left-1/2 z-[70] max-w-md -translate-x-1/2 rounded-full bg-rose-600 px-4 py-2 text-sm font-medium text-white shadow-lg">
          {banner}
          <button
            type="button"
            className="ml-3 underline"
            onClick={() => setBanner(null)}
          >
            Dismiss
          </button>
        </div>
      ) : null}

      {panelOpen ? (
        <div className="fixed bottom-4 right-4 z-[70] w-[min(100%-2rem,22rem)] overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-[0_8px_28px_rgba(0,0,0,0.18)] dark:border-slate-700 dark:bg-slate-900">
          <div className="flex items-center justify-between gap-2 border-b border-slate-100 bg-[#f8f9fa] px-3 py-2.5 dark:border-slate-800 dark:bg-slate-950">
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-slate-900 dark:text-white">
                {activeUploads > 0
                  ? `Uploading ${activeUploads} item${activeUploads === 1 ? "" : "s"}`
                  : `${doneUploads} upload${doneUploads === 1 ? "" : "s"} complete`}
              </p>
              <p className="text-[11px] text-slate-500 dark:text-slate-400">
                Continues if you leave this page
              </p>
            </div>
            <button
              type="button"
              onClick={() => setPanelOpen(false)}
              className="inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-full text-slate-500 hover:bg-slate-200/80 dark:hover:bg-slate-800"
              aria-label="Close upload panel"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <ul className="max-h-64 overflow-y-auto py-1">
            {jobs.slice(0, 12).map((job) => (
              <li
                key={job.id}
                className="flex items-start gap-2.5 px-3 py-2.5"
              >
                <span className="mt-0.5 shrink-0">
                  {job.status === "done" ? (
                    <CheckCircle2 className="h-4 w-4 text-emerald-600" />
                  ) : job.status === "error" ? (
                    <X className="h-4 w-4 text-rose-600" />
                  ) : (
                    <Loader2 className="h-4 w-4 animate-spin text-[#1a73e8]" />
                  )}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-slate-800 dark:text-slate-100">
                    {job.name}
                  </p>
                  <p className="text-[11px] text-slate-500 dark:text-slate-400">
                    {job.status === "error"
                      ? job.error || "Failed"
                      : job.status === "done"
                        ? formatBytes(job.size)
                        : `${job.percent}% · ${formatBytes(job.bytesLoaded || 0)} / ${formatBytes(job.bytesTotal || job.size)}`}
                  </p>
                  {job.status === "uploading" || job.status === "queued" ? (
                    <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
                      <div
                        className="h-full rounded-full bg-[#1a73e8] transition-[width] duration-150 ease-out"
                        style={{
                          width: `${Math.max(
                            job.status === "queued" ? 2 : job.percent,
                            2,
                          )}%`,
                        }}
                      />
                    </div>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setPanelOpen(true)}
          className="fixed bottom-4 right-4 z-[70] inline-flex cursor-pointer items-center gap-2 rounded-full bg-[#1a73e8] px-4 py-2.5 text-sm font-medium text-white shadow-lg hover:bg-[#1765cc]"
        >
          {activeUploads > 0 ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <CheckCircle2 className="h-4 w-4" />
          )}
          {activeUploads > 0 ? `Uploading ${activeUploads}` : "Uploads"}
        </button>
      )}
    </>
  );
}

export type IdentifyResult = {
  object_name: string;
  confidence_score: number;
  summary: string;
  key_details: string[];
  suggested_actions: string[];
  cache_hit?: boolean;
  cache_source?: string;
};

type ResultDisplayProps = {
  result: IdentifyResult;
};

export default function ResultDisplay({ result }: ResultDisplayProps) {
  const confidencePercent = Math.round(
    Math.min(Math.max(result.confidence_score, 0), 1) * 100,
  );

  return (
    <div className="animate-fade-in rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-700 dark:bg-slate-900/60">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-2xl font-semibold text-slate-900 dark:text-white">
          Analysis Results
        </h2>
        {result.cache_hit ? (
          <span className="rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/50 dark:text-emerald-200">
            Cache: {result.cache_source || "hit"}
          </span>
        ) : result.cache_source === "live" ? (
          <span className="rounded-full border border-sky-200 bg-sky-50 px-3 py-1 text-xs font-medium text-sky-800 dark:border-sky-800 dark:bg-sky-950/50 dark:text-sky-200">
            Live Gemini
          </span>
        ) : null}
      </div>

      <div className="mb-6">
        <h3 className="mb-2 text-sm font-medium uppercase tracking-wider text-slate-500 dark:text-slate-400">
          Identified Object
        </h3>
        <p className="text-xl font-medium text-slate-900 dark:text-white">
          {result.object_name}
        </p>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
          Confidence: {confidencePercent}%
        </p>
      </div>

      <div className="mb-6">
        <h3 className="mb-2 text-sm font-medium uppercase tracking-wider text-slate-500 dark:text-slate-400">
          Summary
        </h3>
        <p className="leading-relaxed text-slate-700 dark:text-slate-200">
          {result.summary}
        </p>
      </div>

      {result.key_details?.length > 0 && (
        <div className="mb-6">
          <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Key Details
          </h3>
          <div className="flex flex-wrap gap-2">
            {result.key_details.map((detail) => (
              <span
                key={detail}
                className="inline-flex items-center rounded-full border border-violet-200 bg-violet-50 px-3 py-1 text-sm font-medium text-violet-800 dark:border-violet-800 dark:bg-violet-950/50 dark:text-violet-200"
              >
                {detail}
              </span>
            ))}
          </div>
        </div>
      )}

      {result.suggested_actions?.length > 0 && (
        <div>
          <h3 className="mb-3 text-sm font-medium uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Suggested Actions
          </h3>
          <ul className="list-inside list-disc space-y-1 text-slate-700 dark:text-slate-200">
            {result.suggested_actions.map((action) => (
              <li key={action}>{action}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

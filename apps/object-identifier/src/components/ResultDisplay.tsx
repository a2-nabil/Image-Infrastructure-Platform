export type IdentifyResult = {
  object_name: string;
  confidence_score: number;
  summary: string;
  key_details: string[];
  suggested_actions: string[];
};

type ResultDisplayProps = {
  result: IdentifyResult;
};

export default function ResultDisplay({ result }: ResultDisplayProps) {
  const confidencePercent = Math.round(
    Math.min(Math.max(result.confidence_score, 0), 1) * 100,
  );

  return (
    <div className="bg-white rounded-xl shadow-lg p-6 animate-fade-in">
      <h2 className="text-2xl font-semibold mb-6 text-gray-800">
        Analysis Results
      </h2>

      <div className="mb-6">
        <h3 className="text-sm font-medium text-gray-500 uppercase tracking-wider mb-2">
          Identified Object
        </h3>
        <p className="text-xl font-medium text-gray-900">{result.object_name}</p>
        <p className="mt-1 text-sm text-gray-500">
          Confidence: {confidencePercent}%
        </p>
      </div>

      <div className="mb-6">
        <h3 className="text-sm font-medium text-gray-500 uppercase tracking-wider mb-2">
          Summary
        </h3>
        <p className="text-gray-700 leading-relaxed">{result.summary}</p>
      </div>

      {result.key_details?.length > 0 && (
        <div className="mb-6">
          <h3 className="text-sm font-medium text-gray-500 uppercase tracking-wider mb-3">
            Key Details
          </h3>
          <div className="flex flex-wrap gap-2">
            {result.key_details.map((detail) => (
              <span
                key={detail}
                className="inline-flex items-center px-3 py-1 rounded-full text-sm font-medium bg-blue-50 text-blue-700"
              >
                {detail}
              </span>
            ))}
          </div>
        </div>
      )}

      {result.suggested_actions?.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-gray-500 uppercase tracking-wider mb-3">
            Suggested Actions
          </h3>
          <ul className="list-disc list-inside space-y-1 text-gray-700">
            {result.suggested_actions.map((action) => (
              <li key={action}>{action}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

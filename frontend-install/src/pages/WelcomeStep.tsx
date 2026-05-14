interface Props { onNext: () => void }

export default function WelcomeStep({ onNext }: Props) {
  return (
    <div>
      <img src="/logo.png" alt="KRouter" className="w-14 h-14 rounded-2xl mb-5 shadow-sm" />
      <h1 className="text-2xl font-bold text-gray-900 mb-3 tracking-tight leading-snug">
        Stop overpaying for AI tokens.
      </h1>
      <p className="text-gray-500 mb-5 leading-relaxed">
        KRouter's smart routing picks the cheapest capable provider for every
        request — automatically saving you money on each call.
      </p>

      <ul className="space-y-3 mb-7">
        <li className="flex items-start gap-3 text-sm text-gray-600">
          <span className="mt-0.5 w-4 h-4 rounded-full bg-brand-light flex items-center justify-center flex-shrink-0">
            <svg className="w-2.5 h-2.5 text-brand" fill="none" viewBox="0 0 10 10" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M1.5 5l2.5 2.5 4.5-4.5" />
            </svg>
          </span>
          <span><strong className="text-gray-800 font-semibold">Runs entirely on your machine</strong> — no cloud uploads, no data collection</span>
        </li>
        <li className="flex items-start gap-3 text-sm text-gray-600">
          <span className="mt-0.5 w-4 h-4 rounded-full bg-brand-light flex items-center justify-center flex-shrink-0">
            <svg className="w-2.5 h-2.5 text-brand" fill="none" viewBox="0 0 10 10" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M1.5 5l2.5 2.5 4.5-4.5" />
            </svg>
          </span>
          <span><strong className="text-gray-800 font-semibold">API keys never written to disk</strong> — forwarded directly at request time</span>
        </li>
        <li className="flex items-start gap-3 text-sm text-gray-600">
          <span className="mt-0.5 w-4 h-4 rounded-full bg-brand-light flex items-center justify-center flex-shrink-0">
            <svg className="w-2.5 h-2.5 text-brand" fill="none" viewBox="0 0 10 10" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M1.5 5l2.5 2.5 4.5-4.5" />
            </svg>
          </span>
          <span><strong className="text-gray-800 font-semibold">No LLM calls of its own</strong> — just routes between the providers you already use</span>
        </li>
      </ul>

      <p className="text-xs text-gray-400 mb-6">
        This wizard handles the setup automatically. Takes about 30 seconds.
      </p>

      <button
        onClick={onNext}
        className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-3 px-6 rounded-xl transition-colors"
      >
        Get Started
      </button>
    </div>
  )
}

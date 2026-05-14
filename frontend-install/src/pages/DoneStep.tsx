export default function DoneStep() {
  return (
    <div className="text-center">
      <div className="w-16 h-16 rounded-full bg-brand-light flex items-center justify-center mx-auto mb-5">
        <svg className="w-8 h-8 text-brand" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2.5}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
        </svg>
      </div>
      <h2 className="text-2xl font-bold text-gray-900 mb-2 tracking-tight">All set!</h2>
      <p className="text-gray-500 mb-6 leading-relaxed">
        KRouter is running in the background. Your AI agents are now routing through the proxy and saving you tokens.
      </p>
      <a
        href="http://127.0.0.1:8403/ui/"
        className="inline-block w-full bg-brand hover:bg-brand-dark text-white font-semibold py-3 px-6 rounded-xl transition-colors text-center"
        target="_blank"
        rel="noreferrer"
      >
        Open KRouter Dashboard →
      </a>
      <p className="text-xs text-gray-400 mt-4">
        You can close this window at any time.
      </p>
    </div>
  )
}

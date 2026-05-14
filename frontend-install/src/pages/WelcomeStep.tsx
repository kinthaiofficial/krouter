interface Props { onNext: () => void }

export default function WelcomeStep({ onNext }: Props) {
  return (
    <div className="text-center">
      <img src="/logo.png" alt="KRouter" className="w-16 h-16 rounded-2xl mx-auto mb-5 shadow-sm" />
      <h1 className="text-2xl font-bold text-gray-900 mb-2 tracking-tight">Welcome to KRouter</h1>
      <p className="text-gray-500 mb-4 leading-relaxed">
        KRouter intercepts requests from your local AI agents and routes them to the
        cheapest suitable provider — transparently saving you tokens.
      </p>
      <p className="text-sm text-gray-400 mb-8">
        This wizard installs KRouter, registers it as a background service, and
        connects your AI agents. It takes about 30 seconds.
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

interface Props { onNext: () => void }

export default function WelcomeStep({ onNext }: Props) {
  return (
    <div className="text-center">
      <div className="text-4xl mb-4">🔀</div>
      <h1 className="text-2xl font-bold text-gray-900 mb-2">Welcome to krouter</h1>
      <p className="text-gray-500 mb-8">
        krouter intercepts requests from your local AI agents and routes them to the
        cheapest suitable provider — transparently saving you tokens.
      </p>
      <p className="text-sm text-gray-400 mb-8">
        This wizard will install krouter, register it as a background service, and
        connect your AI agents. It takes about 30 seconds.
      </p>
      <button
        onClick={onNext}
        className="w-full bg-blue-600 hover:bg-blue-700 text-white font-medium py-3 px-6 rounded-lg transition-colors"
      >
        Get Started
      </button>
    </div>
  )
}

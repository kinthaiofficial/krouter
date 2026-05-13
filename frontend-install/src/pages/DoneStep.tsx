export default function DoneStep() {
  return (
    <div className="text-center">
      <div className="text-4xl mb-4">🎉</div>
      <h2 className="text-xl font-bold text-gray-900 mb-2">Installation complete!</h2>
      <p className="text-gray-500 mb-8">
        krouter is running in the background. Your AI agents are now routing through the proxy.
      </p>
      <p className="text-sm text-gray-400">
        You can close this window. Access the dashboard at{' '}
        <a
          href="http://127.0.0.1:8403/ui/"
          className="text-blue-600 hover:underline"
          target="_blank"
          rel="noreferrer"
        >
          http://127.0.0.1:8403/ui/
        </a>
      </p>
    </div>
  )
}

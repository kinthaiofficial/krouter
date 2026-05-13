import React, { useEffect, useState } from 'react'

export default function Step3Install({ onNext }) {
  const [status, setStatus] = useState('running') // 'running' | 'done' | 'error'
  const [result, setResult] = useState(null)

  useEffect(() => {
    async function run() {
      try {
        const res = window.go?.main?.App?.RunInstall
          ? await window.go.main.App.RunInstall()
          : { binary_path: '~/.local/bin/krouter', plist_path: '', error: '' }

        if (res.error) {
          setStatus('error')
          setResult(res)
        } else {
          setStatus('done')
          setResult(res)
          setTimeout(() => onNext(res), 800)
        }
      } catch (e) {
        setStatus('error')
        setResult({ error: e.message })
      }
    }
    run()
  }, [onNext])

  return (
    <div className="space-y-6 text-center">
      <h2 className="text-xl font-semibold text-gray-800">Installing…</h2>
      {status === 'running' && (
        <div className="flex justify-center">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-200 border-t-blue-600" />
        </div>
      )}
      {status === 'done' && (
        <div className="space-y-2 text-sm text-gray-600">
          <p className="text-green-600 font-medium">Installation complete</p>
          {result?.binary_path && <p className="text-gray-400">{result.binary_path}</p>}
        </div>
      )}
      {status === 'error' && (
        <div className="space-y-3">
          <p className="text-red-500 font-medium">Installation failed</p>
          <p className="rounded bg-red-50 px-3 py-2 text-xs text-red-700">{result?.error}</p>
          <button
            onClick={() => onNext(result)}
            className="text-sm text-gray-400 hover:text-gray-600"
          >
            Skip and continue
          </button>
        </div>
      )}
    </div>
  )
}

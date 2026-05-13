import React from 'react'

export default function Step1Language({ onNext }) {
  return (
    <div className="space-y-6 text-center">
      <h2 className="text-xl font-semibold text-gray-800">Choose Language / 选择语言</h2>
      <div className="flex justify-center gap-4">
        <button
          onClick={() => onNext('en')}
          className="w-36 rounded-lg border border-gray-200 bg-white px-6 py-4 text-gray-700 shadow-sm hover:border-blue-400 hover:bg-blue-50 transition-colors"
        >
          English
        </button>
        <button
          onClick={() => onNext('zh-CN')}
          className="w-36 rounded-lg border border-gray-200 bg-white px-6 py-4 text-gray-700 shadow-sm hover:border-blue-400 hover:bg-blue-50 transition-colors"
        >
          中文
        </button>
      </div>
    </div>
  )
}

import React, { useState } from 'react'
import Step1Language from './wizard/Step1Language'
import Step2Permissions from './wizard/Step2Permissions'
import Step3Install from './wizard/Step3Install'
import Step4Agents from './wizard/Step4Agents'
import Step5Complete from './wizard/Step5Complete'

const STEPS = ['language', 'permissions', 'install', 'agents', 'complete']

export default function Wizard({ onComplete }) {
  const [step, setStep] = useState(0)

  const next = () => setStep((s) => Math.min(s + 1, STEPS.length - 1))
  const back = () => setStep((s) => Math.max(s - 1, 0))

  const stepName = STEPS[step]

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 p-6">
      <div className="w-full max-w-md rounded-2xl border border-gray-200 bg-white p-8 shadow-sm">
        {/* Progress dots */}
        <div className="mb-8 flex justify-center gap-2">
          {STEPS.map((_, i) => (
            <span
              key={i}
              className={`inline-block h-2 w-2 rounded-full transition-colors ${
                i === step ? 'bg-blue-600' : i < step ? 'bg-blue-200' : 'bg-gray-200'
              }`}
            />
          ))}
        </div>

        {stepName === 'language' && (
          <Step1Language onNext={(lang) => { void lang; next() }} />
        )}
        {stepName === 'permissions' && (
          <Step2Permissions onNext={next} onBack={back} />
        )}
        {stepName === 'install' && (
          <Step3Install onNext={next} />
        )}
        {stepName === 'agents' && (
          <Step4Agents onNext={next} />
        )}
        {stepName === 'complete' && (
          <Step5Complete onDone={onComplete} />
        )}
      </div>
    </div>
  )
}

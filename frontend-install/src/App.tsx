import { useState } from 'react'
import WelcomeStep from './pages/WelcomeStep'
import DetectStep from './pages/DetectStep'
import ServiceStep from './pages/ServiceStep'
import ShellStep from './pages/ShellStep'
import DoneStep from './pages/DoneStep'

export type Step = 'welcome' | 'detect' | 'service' | 'shell' | 'done'

export default function App() {
  const [step, setStep] = useState<Step>('welcome')

  const steps: Step[] = ['welcome', 'detect', 'service', 'shell', 'done']
  const stepIdx = steps.indexOf(step)

  return (
    <div className="min-h-screen bg-gray-50 flex flex-col items-center justify-center p-4">
      {/* Progress bar */}
      <div className="w-full max-w-lg mb-8">
        <div className="flex items-center gap-1">
          {steps.slice(0, -1).map((s, i) => (
            <div
              key={s}
              className={`h-1.5 flex-1 rounded-full transition-colors ${
                i < stepIdx ? 'bg-blue-500' : i === stepIdx ? 'bg-blue-300' : 'bg-gray-200'
              }`}
            />
          ))}
        </div>
        <p className="text-xs text-gray-400 mt-1 text-right">
          Step {Math.min(stepIdx + 1, steps.length - 1)} of {steps.length - 1}
        </p>
      </div>

      <div className="w-full max-w-lg bg-white rounded-2xl shadow-sm border border-gray-100 p-8">
        {step === 'welcome' && <WelcomeStep onNext={() => setStep('detect')} />}
        {step === 'detect'  && <DetectStep  onNext={() => setStep('service')} />}
        {step === 'service' && <ServiceStep onNext={() => setStep('shell')} />}
        {step === 'shell'   && <ShellStep   onNext={() => setStep('done')} />}
        {step === 'done'    && <DoneStep />}
      </div>
    </div>
  )
}

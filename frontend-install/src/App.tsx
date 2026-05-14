import { useState } from 'react'
import WelcomeStep from './pages/WelcomeStep'
import DetectStep from './pages/DetectStep'
import ServiceStep from './pages/ServiceStep'
import ShellStep from './pages/ShellStep'
import DoneStep from './pages/DoneStep'

export type Step = 'welcome' | 'detect' | 'service' | 'shell' | 'done'

const STEPS: Step[] = ['welcome', 'detect', 'service', 'shell', 'done']
const LABELS = ['Welcome', 'Detect agents', 'Install service', 'Shell setup', 'Done']

export default function App() {
  const [step, setStep] = useState<Step>('welcome')
  const stepIdx = STEPS.indexOf(step)
  const progressSteps = STEPS.slice(0, -1)

  return (
    <div className="min-h-screen bg-surface flex flex-col" style={{ fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue", Arial, sans-serif' }}>

      {/* Header */}
      <header className="flex items-center justify-center pt-10 pb-4">
        <div className="flex items-center gap-3">
          <img src="/logo.png" alt="KRouter" className="w-12 h-12 rounded-xl" />
          <span className="text-2xl font-bold text-gray-900 tracking-tight">KRouter</span>
        </div>
      </header>

      {/* Card */}
      <main className="flex-1 flex flex-col items-center px-4 pt-4 pb-10">
        <div className="w-full max-w-md bg-white rounded-2xl border border-border shadow-sm p-8">
          {step === 'welcome' && <WelcomeStep onNext={() => setStep('detect')} />}
          {step === 'detect'  && <DetectStep  onNext={() => setStep('service')} />}
          {step === 'service' && <ServiceStep onNext={() => setStep('shell')} />}
          {step === 'shell'   && <ShellStep   onNext={() => setStep('done')} />}
          {step === 'done'    && <DoneStep />}
        </div>

        {/* Progress dots */}
        {step !== 'done' && (
          <div className="mt-6 flex flex-col items-center gap-2">
            <div className="flex items-center gap-2">
              {progressSteps.map((s, i) => (
                <div
                  key={s}
                  className={`rounded-full transition-all duration-300 ${
                    i < stepIdx  ? 'w-2 h-2 bg-brand' :
                    i === stepIdx ? 'w-2.5 h-2.5 bg-brand' :
                    'w-2 h-2 bg-gray-200'
                  }`}
                />
              ))}
            </div>
            <p className="text-xs text-gray-400">
              {LABELS[stepIdx]} — step {stepIdx + 1} of {progressSteps.length}
            </p>
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="py-5 text-center text-xs text-gray-400">
        by{' '}
        <a href="https://kinthai.ai" target="_blank" rel="noreferrer" className="hover:text-brand transition-colors">
          kinthai.ai
        </a>
      </footer>
    </div>
  )
}

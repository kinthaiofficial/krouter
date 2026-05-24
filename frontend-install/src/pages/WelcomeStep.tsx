import { useTranslation } from 'react-i18next'

interface Props { onNext: () => void }

export default function WelcomeStep({ onNext }: Props) {
  const { t } = useTranslation()
  return (
    <div>
      <h1 className="text-2xl font-bold text-gray-900 mb-3 tracking-tight leading-snug">
        {t('welcome.headline')}
      </h1>
      <p className="text-gray-500 mb-5 leading-relaxed">
        {t('welcome.description')}
      </p>

      <ul className="space-y-3 mb-7">
        <li className="flex items-start gap-3 text-sm text-gray-600">
          <span className="mt-0.5 w-4 h-4 rounded-full bg-brand-light flex items-center justify-center flex-shrink-0">
            <svg className="w-2.5 h-2.5 text-brand" fill="none" viewBox="0 0 10 10" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M1.5 5l2.5 2.5 4.5-4.5" />
            </svg>
          </span>
          <span>{t('welcome.feature_local')}</span>
        </li>
        <li className="flex items-start gap-3 text-sm text-gray-600">
          <span className="mt-0.5 w-4 h-4 rounded-full bg-brand-light flex items-center justify-center flex-shrink-0">
            <svg className="w-2.5 h-2.5 text-brand" fill="none" viewBox="0 0 10 10" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M1.5 5l2.5 2.5 4.5-4.5" />
            </svg>
          </span>
          <span>{t('welcome.feature_keys')}</span>
        </li>
        <li className="flex items-start gap-3 text-sm text-gray-600">
          <span className="mt-0.5 w-4 h-4 rounded-full bg-brand-light flex items-center justify-center flex-shrink-0">
            <svg className="w-2.5 h-2.5 text-brand" fill="none" viewBox="0 0 10 10" stroke="currentColor" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M1.5 5l2.5 2.5 4.5-4.5" />
            </svg>
          </span>
          <span>{t('welcome.feature_nollm')}</span>
        </li>
      </ul>

      <p className="text-xs text-gray-500 mb-6">
        {t('welcome.feature_auto')}
      </p>

      <button
        onClick={onNext}
        className="w-full bg-brand hover:bg-brand-dark text-white font-semibold py-3 px-6 rounded-xl transition-colors"
      >
        {t('welcome.get_started')}
      </button>
    </div>
  )
}

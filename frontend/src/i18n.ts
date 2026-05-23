import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './locales/en.json'
import zh from './locales/zh.json'

// Language is cached in localStorage so the UI loads instantly without
// waiting for the settings API. Updated whenever the user changes language
// in Settings (see Settings.tsx).
const STORAGE_KEY = 'krouter:lang'

export function getStoredLang(): string {
  return localStorage.getItem(STORAGE_KEY) || 'en'
}

export function storeLang(lang: string) {
  localStorage.setItem(STORAGE_KEY, lang)
}

// Map settings.language values ('en', 'zh-CN') → i18next language keys.
export function settingsLangToI18n(lang: string): string {
  return lang.startsWith('zh') ? 'zh' : 'en'
}

i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    zh: { translation: zh },
  },
  lng: getStoredLang(),
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

export default i18n

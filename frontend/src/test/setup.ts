import '@testing-library/jest-dom'
import '../i18n'

// jsdom does not implement IntersectionObserver (used by Router infinite scroll).
if (typeof IntersectionObserver === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(globalThis as any).IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

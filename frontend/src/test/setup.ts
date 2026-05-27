import '@testing-library/jest-dom'
import '../i18n'

// jsdom does not implement IntersectionObserver (used by Router infinite scroll).
if (typeof IntersectionObserver === 'undefined') {
  global.IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof IntersectionObserver
}

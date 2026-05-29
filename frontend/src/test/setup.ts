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

// jsdom does not implement EventSource (used by Dashboard and About for SSE).
if (typeof EventSource === 'undefined') {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(globalThis as any).EventSource = class {
    addEventListener() {}
    removeEventListener() {}
    close() {}
  }
}

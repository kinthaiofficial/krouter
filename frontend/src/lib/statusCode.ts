// HTTP status code → short human-readable explanation for the dashboard.
//
// The dashboard surfaces upstream status codes (200, 401, 402, 429, 5xx, ...)
// in several places: routing-decision cards, the Logs page, the Providers
// page's last_error_code. Showing the raw number alone is meaningless to
// most users — these helpers map each code to a short i18n key whose
// localized text explains "what went wrong and where".
//
// Keys live under `status.code_*` in en.json / zh.json. The bucket
// fallbacks (`status.bucket_4xx` / `bucket_5xx`) cover anything we
// haven't catalogued explicitly.
import type { TFunction } from 'i18next'

export function statusCodeMeaning(code: number, t: TFunction): string {
  const key = statusCodeKey(code)
  // i18next exists is best-effort; just hand the key to t() — when missing
  // the fallback returns the key itself, which is still better than nothing.
  return t(key)
}

function statusCodeKey(code: number): string {
  switch (code) {
    case 200: return 'status.code_200'
    case 201: return 'status.code_201'
    case 204: return 'status.code_204'

    case 400: return 'status.code_400'
    case 401: return 'status.code_401'
    case 402: return 'status.code_402'
    case 403: return 'status.code_403'
    case 404: return 'status.code_404'
    case 408: return 'status.code_408'
    case 409: return 'status.code_409'
    case 413: return 'status.code_413'
    case 422: return 'status.code_422'
    case 429: return 'status.code_429'

    case 500: return 'status.code_500'
    case 502: return 'status.code_502'
    case 503: return 'status.code_503'
    case 504: return 'status.code_504'
  }
  if (code >= 200 && code < 300) return 'status.bucket_2xx'
  if (code >= 300 && code < 400) return 'status.bucket_3xx'
  if (code >= 400 && code < 500) return 'status.bucket_4xx'
  if (code >= 500 && code < 600) return 'status.bucket_5xx'
  return 'status.bucket_unknown'
}

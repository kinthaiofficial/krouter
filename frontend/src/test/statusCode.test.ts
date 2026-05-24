import { describe, it, expect } from 'vitest'
import '../i18n'
import i18n from 'i18next'
import { statusCodeMeaning } from '../lib/statusCode'

const t = i18n.t.bind(i18n)

describe('statusCodeMeaning', () => {
  it('translates well-known explicit codes', () => {
    expect(statusCodeMeaning(200, t)).toMatch(/OK|成功/)
    expect(statusCodeMeaning(401, t)).toMatch(/Unauthorized|未授权/)
    expect(statusCodeMeaning(402, t)).toMatch(/Payment|付费/)
    expect(statusCodeMeaning(429, t)).toMatch(/Too many|限流/)
    expect(statusCodeMeaning(500, t)).toMatch(/Internal|内部错误/)
    expect(statusCodeMeaning(504, t)).toMatch(/Gateway timeout|网关超时/)
  })

  it('falls back to the 4xx bucket for uncatalogued client errors', () => {
    expect(statusCodeMeaning(418, t)).toMatch(/Client error|客户端错误/)
    expect(statusCodeMeaning(451, t)).toMatch(/Client error|客户端错误/)
  })

  it('falls back to the 5xx bucket for uncatalogued server errors', () => {
    expect(statusCodeMeaning(599, t)).toMatch(/Server error|服务端错误/)
  })

  it('falls back to 2xx bucket for uncatalogued success codes', () => {
    expect(statusCodeMeaning(207, t)).toMatch(/Success|成功/)
  })

  it('returns the unknown-bucket message for codes outside HTTP range', () => {
    expect(statusCodeMeaning(0, t)).toMatch(/Unusual|异常/)
    expect(statusCodeMeaning(999, t)).toMatch(/Unusual|异常/)
  })
})

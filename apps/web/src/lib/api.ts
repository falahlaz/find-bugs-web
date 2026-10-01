import createClient, { type Middleware } from 'openapi-fetch'
import type { components, paths } from './api-schema'

export type Schemas = components['schemas']
export type User = Schemas['User']
export type SystemStatus = Schemas['SystemStatus']

// The CSRF token comes from /api/auth/login or /api/me and must be sent on
// every unsafe request; the session itself is an HttpOnly cookie.
let csrfToken = ''
export function setCsrfToken(token: string) {
  csrfToken = token
}

export class ApiError extends Error {
  status: number
  code?: string
  constructor(status: number, message: string, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

const csrf: Middleware = {
  onRequest({ request }) {
    if (request.method !== 'GET' && request.method !== 'HEAD' && csrfToken) {
      request.headers.set('X-CSRF-Token', csrfToken)
    }
    return request
  },
}

export const api = createClient<paths>({ baseUrl: '/', credentials: 'same-origin' })
api.use(csrf)

/** Unwraps an openapi-fetch result, throwing ApiError on failure. */
export function unwrap<T>(res: { data?: T; error?: unknown; response: Response }): T {
  if (res.error !== undefined || res.data === undefined) {
    const e = res.error as { error?: string; code?: string } | undefined
    throw new ApiError(res.response.status, e?.error ?? `HTTP ${res.response.status}`, e?.code)
  }
  return res.data
}

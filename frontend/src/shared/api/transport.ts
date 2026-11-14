import { apiBaseUrl } from '@/shared/utils/desktop'

export interface TransportResponse {
  status: number
  body: string
}

export class ApiError extends Error {
  status: number
  data: unknown

  constructor(status: number, data: any) {
    const message = data?.error || data?.message || `HTTP ${status}`
    super(typeof message === 'string' ? message : JSON.stringify(message))
    this.name = 'ApiError'
    this.status = status
    this.data = data
  }
}

export function parseResponse(response: TransportResponse): any {
  let data: any = null
  try {
    data = response.body ? JSON.parse(response.body) : null
  } catch {
    data = response.body
  }
  if (response.status < 200 || response.status >= 300) {
    throw new ApiError(response.status, data)
  }
  return data
}

export const httpTransport = {
  async request(method: string, path: string, body?: string): Promise<TransportResponse> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    const res = await fetch(apiBaseUrl() + path, { method, headers, body })
    return { status: res.status, body: await res.text() }
  },
}

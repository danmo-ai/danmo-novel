import { httpTransport, parseResponse } from './transport'

async function request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const response = await httpTransport.request(
    method,
    path,
    body == null ? undefined : JSON.stringify(body),
  )
  return parseResponse(response) as T
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: any) => request<T>(path, 'POST', body),
  put: <T>(path: string, body?: any) => request<T>(path, 'PUT', body ?? {}),
  patch: <T>(path: string, body?: any) => request<T>(path, 'PATCH', body ?? {}),
  del: <T>(path: string) => request<T>(path, 'DELETE'),
}

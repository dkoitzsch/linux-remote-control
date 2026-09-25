import type { Process, ShutdownState, Status, User, Role } from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'include',
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    ...init,
  })
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new ApiError(res.status, text || res.statusText)
  }
  if (res.status === 204) return undefined as T
  const contentType = res.headers.get('content-type') || ''
  if (!contentType.includes('application/json')) return undefined as T
  return (await res.json()) as T
}

export async function login(username: string, password: string): Promise<User> {
  return apiFetch<User>('/api/login', { method: 'POST', body: JSON.stringify({ username, password }) })
}

export async function logout(): Promise<void> {
  await apiFetch('/api/logout', { method: 'POST' })
}

export async function me(): Promise<User | null> {
  try {
    return await apiFetch<User>('/api/me')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

export async function getStatus(): Promise<Status> {
  return apiFetch<Status>('/api/status')
}

export async function getProcesses(): Promise<Process[]> {
  return apiFetch<Process[]>('/api/processes')
}

export async function killProcess(pid: number, force = false): Promise<void> {
  await apiFetch(`/api/processes/${pid}/kill${force ? '?force=1' : ''}`, { method: 'POST' })
}

export async function sendNotify(message: string): Promise<void> {
  await apiFetch('/api/notify', { method: 'POST', body: JSON.stringify({ message }) })
}

export async function getShutdownState(): Promise<ShutdownState> {
  return apiFetch<ShutdownState>('/api/shutdown')
}

export async function scheduleShutdown(delaySeconds: number, message: string): Promise<ShutdownState> {
  return apiFetch<ShutdownState>('/api/shutdown', {
    method: 'POST',
    body: JSON.stringify({ delaySeconds, message }),
  })
}

export async function cancelShutdown(): Promise<void> {
  await apiFetch('/api/shutdown', { method: 'DELETE' })
}

export async function listUsers(): Promise<User[]> {
  return apiFetch<User[]>('/api/users')
}

export async function createUser(username: string, password: string, role: Role): Promise<User> {
  return apiFetch<User>('/api/users', { method: 'POST', body: JSON.stringify({ username, password, role }) })
}

export async function deleteUser(id: number): Promise<void> {
  await apiFetch(`/api/users/${id}`, { method: 'DELETE' })
}

export function screenshotUrl(): string {
  return `/api/screenshot?ts=${Date.now()}`
}

export function streamUrl(fps: number, quality: number): string {
  return `/api/stream?fps=${fps}&quality=${quality}&ts=${Date.now()}`
}

export type Role = 'full' | 'viewer'

export interface User {
  id: number
  username: string
  role: Role
  createdAt: string
}

export interface Process {
  pid: number
  ppid: number
  name: string
  exe?: string
  cmdline?: string
  username?: string
  startTime: string
  uptimeSeconds: number
  rssBytes: number
  cpuPercent: number
  isService: boolean
  hasWindow: boolean
  netBytesSent: number
  netBytesRecv: number
  netLastActive?: string
}

export interface ShutdownState {
  active: boolean
  at?: string
  message?: string
  requestedBy?: string
}

export interface Status {
  helperConnected: boolean
  networkStatsAvailable: boolean
}

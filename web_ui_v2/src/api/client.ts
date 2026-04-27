/** REST API + WebSocket client for kimiSec cluster backend */

const API_BASE = import.meta.env.VITE_API_BASE || ''
const WS_URL = import.meta.env.VITE_WS_URL || `ws://${location.host}/ws/events`

// ── REST helpers ──────────────────────────────────────────────────

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  })
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`API ${res.status}: ${body.slice(0, 200)}`)
  }
  return res.json()
}

export const api = {
  getCluster: () => request<ClusterDashboard>('/api/cluster'),

  getFindings: (params?: { severity?: string; keyword?: string; limit?: number }) => {
    const qs = new URLSearchParams()
    if (params?.severity) qs.set('severity', params.severity)
    if (params?.keyword) qs.set('keyword', params.keyword)
    if (params?.limit) qs.set('limit', String(params.limit))
    const q = qs.toString()
    return request<FindingsResponse>(`/api/findings${q ? '?' + q : ''}`)
  },

  getQueue: () => request<QueueResponse>('/api/queue'),

  getJobs: () => request<JobsListResponse>('/api/jobs'),

  getJobSnapshot: (jobId: string) => request<BlackboardSnapshot>(`/api/jobs/${jobId}/snapshot`),

  getJobAudit: (jobId: string) => request<{ job_id: string; report: string }>(`/api/jobs/${jobId}/audit`),

  submitJobs: (targets: SubmitTarget[]) =>
    request<{ ok: boolean; message: string }>('/api/jobs', {
      method: 'POST',
      body: JSON.stringify({ targets }),
    }),

  cancelJob: (jobId: string) =>
    request<{ ok: boolean; message: string }>(`/api/jobs/${jobId}`, { method: 'DELETE' }),

  getInsights: () => request<{ report: string }>('/api/insights'),

  // ── Workspace ──
  getProjects: () => request<ProjectsResponse>('/api/workspace/projects'),

  getProjectReports: (name: string) =>
    request<ProjectReportsResponse>(`/api/workspace/projects/${name}/reports`),

  getReportContent: (name: string, filename: string) =>
    request<ReportContent>(`/api/workspace/projects/${name}/reports/${filename}`),

  getProjectBlackboard: (name: string) =>
    request<BlackboardSnapshot>(`/api/workspace/projects/${name}/blackboard`),

  deleteProject: (name: string) =>
    request<{ ok: boolean; message: string }>(`/api/workspace/projects/${name}`, { method: 'DELETE' }),
}

// ── WebSocket ─────────────────────────────────────────────────────

export type EventHandler = (event: WsEvent) => void

export function createEventStream(onEvent: EventHandler, onStatus?: (connected: boolean) => void) {
  let ws: WebSocket | null = null
  let retryCount = 0
  let stopped = false

  function connect() {
    if (stopped) return
    ws = new WebSocket(WS_URL)

    ws.onopen = () => {
      retryCount = 0
      onStatus?.(true)
    }

    ws.onmessage = (e) => {
      try {
        const parsed = JSON.parse(e.data) as WsEvent
        onEvent(parsed)
      } catch { /* ignore malformed */ }
    }

    ws.onclose = () => {
      onStatus?.(false)
      if (!stopped) {
        const delay = Math.min(1000 * 2 ** retryCount, 15000)
        retryCount++
        setTimeout(connect, delay)
      }
    }

    ws.onerror = () => ws?.close()
  }

  connect()

  return {
    stop: () => { stopped = true; ws?.close() },
  }
}

// ── Types ─────────────────────────────────────────────────────────

export interface SubmitTarget {
  target: string
  max_workers?: number
  max_rounds?: number
  max_tasks?: number
  max_wall_time?: number
  stagnation_rounds?: number
}

export interface ClusterDashboard {
  nodes: ClusterNode[]
  total_findings: number
  active_jobs: number
  queue_depth: number
}

export interface ClusterNode {
  node_id: string
  target: string
  active: boolean
  last_heartbeat: number
  stats: { hypotheses: number; findings: number }
}

export interface Finding {
  id: string
  title: string
  description: string
  evidence: string
  severity: 'critical' | 'high' | 'medium' | 'low'
  hypothesis_id: string
  created_at: number
}

export interface FindingsResponse {
  total: number
  findings: Finding[]
}

export interface QueueResponse {
  queue_depth: number
  jobs: Record<string, unknown>[]
}

export interface JobsListResponse {
  total: number
  job_ids: string[]
}

export interface HypothesisNode {
  id: string
  description: string
  confidence: number
  status: 'pending' | 'active' | 'suspected' | 'confirmed' | 'discarded'
  parent_id: string | null
  evidence: string[]
  tasks: string[]
  created_at: number
}

export interface BlackboardSnapshot {
  target: string
  active: boolean
  hypotheses: Record<string, HypothesisNode>
  tasks: Record<string, unknown>
  findings: Finding[]
}

export interface WsEvent {
  type: string
  node_id?: string
  data: Record<string, unknown>
  ts?: number
}

// ── Workspace Types ──

export interface ProjectInfo {
  name: string
  reports_count: number
  json_reports_count: number
  has_blackboard: boolean
  blackboard_size: number
  disk_size_mb: number
  last_modified: number
}

export interface ProjectsResponse {
  total: number
  projects: ProjectInfo[]
}

export interface ReportFile {
  filename: string
  type: string
  size: number
  modified: number
}

export interface ProjectReportsResponse {
  project: string
  reports: ReportFile[]
  meta: { has_blackboard: boolean; has_audit_notes: boolean }
}

export interface ReportContent {
  project: string
  filename: string
  type: string
  size: number
  content: string
}

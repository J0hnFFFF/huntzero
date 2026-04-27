import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api, createEventStream, type ClusterNode, type Finding, type WsEvent } from '../api/client'

export const useClusterStore = defineStore('cluster', () => {
  // ── State ──
  const nodes = ref<ClusterNode[]>([])
  const findings = ref<Finding[]>([])
  const events = ref<WsEvent[]>([])
  const queueDepth = ref(0)
  const wsConnected = ref(false)
  const loading = ref(false)
  const error = ref<string | null>(null)

  // ── Computed ──
  const activeNodes = computed(() => nodes.value.filter(n => n.active))
  const totalFindings = computed(() => findings.value.length)

  const findingsBySeverity = computed(() => {
    const counts = { critical: 0, high: 0, medium: 0, low: 0 }
    for (const f of findings.value) {
      if (f.severity in counts) counts[f.severity]++
    }
    return counts
  })

  // ── Actions ──
  async function fetchCluster() {
    loading.value = true
    error.value = null
    try {
      const data = await api.getCluster()
      nodes.value = data.nodes || []
      queueDepth.value = data.queue_depth || 0
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function fetchFindings(severity?: string, keyword?: string) {
    loading.value = true
    try {
      const data = await api.getFindings({ severity, keyword, limit: 200 })
      findings.value = data.findings || []
    } catch (e: any) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  function handleEvent(ev: WsEvent) {
    // Keep max 500 events in memory
    events.value.unshift(ev)
    if (events.value.length > 500) events.value.length = 500

    // Auto-update on specific events
    if (['finding_added', 'job_completed', 'job_failed'].includes(ev.type)) {
      fetchFindings()
      fetchCluster()
    }
    if (['connected', 'worker_heartbeat'].includes(ev.type)) {
      fetchCluster()
    }
  }

  let streamHandle: ReturnType<typeof createEventStream> | null = null

  function startEventStream() {
    streamHandle = createEventStream(handleEvent, (connected) => {
      wsConnected.value = connected
    })
  }

  function stopEventStream() {
    streamHandle?.stop()
  }

  return {
    nodes, findings, events, queueDepth, wsConnected, loading, error,
    activeNodes, totalFindings, findingsBySeverity,
    fetchCluster, fetchFindings, handleEvent, startEventStream, stopEventStream,
  }
})

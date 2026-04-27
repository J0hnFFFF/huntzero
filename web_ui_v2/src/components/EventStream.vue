<script setup lang="ts">
import type { WsEvent } from '../api/client'

defineProps<{ events: WsEvent[] }>()

const typeColors: Record<string, string> = {
  connected: 'var(--green)',
  finding_added: 'var(--red)',
  hypothesis_added: 'var(--purple)',
  hypothesis_updated: 'var(--purple)',
  task_added: 'var(--yellow)',
  task_updated: 'var(--yellow)',
  job_completed: 'var(--blue)',
  job_failed: 'var(--red)',
  sector_analysis_started: 'var(--cyan)',
  sector_analysis_completed: 'var(--green)',
  sector_analysis_failed: 'var(--red)',
  coordinator_started: 'var(--cyan)',
  coordinator_finished: 'var(--green)',
  cerebrum_round: 'var(--text-mid)',
  drone_completed: 'var(--green)',
  drone_timeout: 'var(--orange)',
}

function formatTs(ts?: number): string {
  if (!ts) return ''
  return new Date(ts * 1000).toLocaleTimeString('en-GB', { hour12: false })
}

function eventDetail(ev: WsEvent): string {
  const d = ev.data || {}
  if (ev.type === 'finding_added') return `${d.severity} — ${(d.title as string)?.slice(0, 80)}`
  if (ev.type === 'hypothesis_added') return `[${((d.confidence as number) * 100).toFixed(0)}%] ${(d.claim as string)?.slice(0, 80)}`
  if (ev.type === 'sector_analysis_started') return `▶ ${d.sector} — ${d.path}`
  if (ev.type === 'sector_analysis_completed') return `✓ ${d.sector} — ${d.findings} findings, ${(d.elapsed_seconds as number)?.toFixed(0)}s`
  if (ev.type === 'cerebrum_round') return `R${d.round} [${d.phase}]`
  if (ev.type === 'connected') return `WebSocket connected to ${(d.channel as string) || 'events'}`
  const firstVal = Object.values(d)[0]
  return typeof firstVal === 'string' ? firstVal.slice(0, 100) : ''
}
</script>

<template>
  <div class="event-stream">
    <div v-for="(ev, i) in events" :key="i" class="ev-row">
      <span class="ev-ts mono">{{ formatTs(ev.ts) }}</span>
      <span class="ev-type mono" :style="{ color: typeColors[ev.type] || 'var(--text-mid)' }">
        {{ ev.type }}
      </span>
      <span class="ev-detail">{{ eventDetail(ev) }}</span>
    </div>
    <div v-if="!events.length" class="empty">Waiting for events…</div>
  </div>
</template>

<style scoped>
.event-stream {
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.7;
  padding: 12px 0;
}
.ev-row {
  display: flex;
  gap: 12px;
  padding: 1px 16px;
  align-items: flex-start;
}
.ev-row:hover { background: var(--bg-hover); }
.ev-ts { color: var(--text-dim); flex-shrink: 0; font-size: 11px; min-width: 64px; }
.ev-type { font-weight: 700; flex-shrink: 0; min-width: 180px; font-size: 11.5px; }
.ev-detail { color: var(--text-mid); font-size: 11.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty { padding: 32px 16px; color: var(--text-dim); text-align: center; font-family: var(--font-sans); }
</style>

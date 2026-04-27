<script setup lang="ts">
import type { ClusterNode } from '../api/client'

defineProps<{ node: ClusterNode }>()

function timeAgo(ts: number): string {
  if (!ts) return '—'
  const s = Math.floor(Date.now() / 1000 - ts)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  return `${Math.floor(s / 3600)}h ago`
}
</script>

<template>
  <div class="node-card" :class="{ active: node.active, idle: !node.active }">
    <div class="node-bar" />
    <div class="node-head">
      <span class="node-id mono">{{ node.node_id?.slice(0, 8) }}</span>
      <span class="node-pill" :class="node.active ? 'pill-active' : 'pill-idle'">
        {{ node.active ? 'ACTIVE' : 'IDLE' }}
      </span>
    </div>
    <div v-if="node.target" class="node-target mono">{{ node.target }}</div>
    <div class="node-meta">
      <span>H: <b>{{ node.stats?.hypotheses || 0 }}</b></span>
      <span>F: <b>{{ node.stats?.findings || 0 }}</b></span>
      <span class="ts">{{ timeAgo(node.last_heartbeat) }}</span>
    </div>
  </div>
</template>

<style scoped>
.node-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: 14px;
  position: relative;
  overflow: hidden;
  transition: border-color .2s, transform .2s;
}
.node-card:hover {
  transform: translateY(-2px);
  border-color: var(--border-mid);
}
.node-card.active { border-color: rgba(88, 166, 255, .3); }

.node-bar {
  position: absolute; top: 0; left: 0; right: 0; height: 2px;
  background: var(--text-dim); transition: background .3s;
}
.active .node-bar { background: var(--blue); }

.node-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 8px;
}
.node-id { font-size: 12px; color: var(--text-mid); }

.node-pill {
  font-size: 10px; font-weight: 700; padding: 2px 8px;
  border-radius: 10px; letter-spacing: .5px;
}
.pill-active { background: var(--blue-dim); color: var(--blue); }
.pill-idle { background: var(--bg-panel); color: var(--text-dim); border: 1px solid var(--border); }

.node-target {
  font-size: 11px; color: var(--text); margin-bottom: 8px;
  word-break: break-all; line-height: 1.4;
}

.node-meta {
  display: flex; gap: 14px; font-size: 11px; color: var(--text-dim);
}
.node-meta b { color: var(--text-mid); font-weight: 600; }
.node-meta .ts { margin-left: auto; }
</style>

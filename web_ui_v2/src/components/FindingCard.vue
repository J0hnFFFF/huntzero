<script setup lang="ts">
import type { Finding } from '../api/client'

const props = defineProps<{ finding: Finding }>()
const emit = defineEmits<{ click: [finding: Finding] }>()

const sevColors: Record<string, string> = {
  critical: 'var(--red)',
  high: 'var(--orange)',
  medium: 'var(--yellow)',
  low: 'var(--blue)',
}
</script>

<template>
  <div class="finding-card" @click="emit('click', finding)">
    <div class="sev-bar" :style="{ background: sevColors[finding.severity] || 'var(--text-dim)' }" />
    <div class="card-body">
      <div class="card-head">
        <span class="sev-pill" :style="{
          background: sevColors[finding.severity] + '20',
          color: sevColors[finding.severity],
        }">
          {{ finding.severity?.toUpperCase() }}
        </span>
        <span class="finding-id mono">{{ finding.id }}</span>
      </div>
      <div class="finding-title">{{ finding.title }}</div>
      <div class="finding-desc">{{ finding.description?.slice(0, 180) }}</div>
      <div v-if="finding.evidence" class="finding-evidence mono">
        {{ finding.evidence?.slice(0, 120) }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.finding-card {
  position: relative;
  padding: 14px 16px 14px 20px;
  border-bottom: 1px solid var(--border-mid);
  cursor: pointer;
  transition: background .15s;
  animation: fade-in .3s ease-out;
}
.finding-card:hover { background: var(--bg-hover); }

.sev-bar {
  position: absolute; left: 0; top: 0; bottom: 0; width: 3px;
  border-radius: 0 2px 2px 0;
}

.card-head {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 6px;
}

.sev-pill {
  font-size: 10px; font-weight: 800; padding: 2px 8px;
  border-radius: 8px; letter-spacing: .5px;
}

.finding-id { font-size: 10px; color: var(--text-dim); }

.finding-title {
  font-size: 13px; font-weight: 600; color: var(--text);
  line-height: 1.4; margin-bottom: 4px;
}

.finding-desc {
  font-size: 12px; color: var(--text-mid); line-height: 1.4;
}

.finding-evidence {
  font-size: 11px; color: var(--text-dim); padding: 6px 8px;
  background: var(--bg-card); border-radius: 4px;
  margin-top: 8px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
</style>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useClusterStore } from '../stores/cluster'
import FindingCard from '../components/FindingCard.vue'
import type { Finding } from '../api/client'

const store = useClusterStore()
const severityFilter = ref('')
const keyword = ref('')
const selectedFinding = ref<Finding | null>(null)

const filtered = computed(() => {
  let list = store.findings
  if (severityFilter.value) {
    list = list.filter(f => f.severity === severityFilter.value)
  }
  if (keyword.value.trim()) {
    const kw = keyword.value.toLowerCase()
    list = list.filter(f =>
      f.title?.toLowerCase().includes(kw) ||
      f.description?.toLowerCase().includes(kw) ||
      f.evidence?.toLowerCase().includes(kw)
    )
  }
  return list
})

const sevColors: Record<string, string> = {
  critical: 'var(--red)', high: 'var(--orange)',
  medium: 'var(--yellow)', low: 'var(--blue)',
}
</script>

<template>
  <div class="findings-page">
    <!-- ── Filter Bar ── -->
    <div class="filter-bar">
      <div class="filter-group">
        <select v-model="severityFilter" class="filter-select mono">
          <option value="">All Severity</option>
          <option value="critical">Critical</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
        <input v-model="keyword" type="text" placeholder="Search findings…" class="filter-input" />
      </div>
      <div class="filter-stats">
        <span v-for="(count, sev) in store.findingsBySeverity" :key="sev"
          class="sev-count" :style="{ color: sevColors[sev] || 'var(--text-dim)' }">
          {{ sev }}: {{ count }}
        </span>
        <span class="total">Total: {{ store.totalFindings }}</span>
      </div>
    </div>

    <!-- ── Results ── -->
    <div class="results-grid">
      <div class="findings-list-panel">
        <FindingCard
          v-for="f in filtered"
          :key="f.id"
          :finding="f"
          @click="selectedFinding = f"
        />
        <div v-if="!filtered.length" class="empty">
          {{ store.findings.length ? 'No findings match your filter.' : 'No findings discovered yet.' }}
        </div>
      </div>

      <!-- ── Detail Panel ── -->
      <div v-if="selectedFinding" class="detail-panel">
        <div class="detail-header">
          <span class="sev-pill" :style="{
            background: sevColors[selectedFinding.severity] + '20',
            color: sevColors[selectedFinding.severity],
          }">
            {{ selectedFinding.severity?.toUpperCase() }}
          </span>
          <button class="close-btn" @click="selectedFinding = null">✕</button>
        </div>
        <h3 class="detail-title">{{ selectedFinding.title }}</h3>
        <p class="detail-id mono">{{ selectedFinding.id }} · hyp: {{ selectedFinding.hypothesis_id }}</p>

        <div class="detail-section">
          <div class="detail-label">Description</div>
          <div class="detail-text">{{ selectedFinding.description }}</div>
        </div>

        <div v-if="selectedFinding.evidence" class="detail-section">
          <div class="detail-label">Evidence</div>
          <pre class="evidence-block mono">{{ selectedFinding.evidence }}</pre>
        </div>
      </div>
      <div v-else class="detail-placeholder">
        <span>← Select a finding to view details</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.findings-page { display: flex; flex-direction: column; gap: 16px; height: calc(100vh - var(--header-h) - 48px); }

.filter-bar {
  display: flex; align-items: center; justify-content: space-between; gap: 16px;
  padding: 12px 18px;
  background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--radius-md);
}

.filter-group { display: flex; gap: 10px; }

.filter-select, .filter-input {
  background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius-sm);
  color: var(--text); font-size: 13px; padding: 7px 12px; outline: none;
  transition: border-color .2s;
}
.filter-select:focus, .filter-input:focus { border-color: var(--blue); }
.filter-input { width: 260px; }

.filter-stats { display: flex; gap: 16px; font-size: 11px; font-weight: 600; }
.sev-count { text-transform: uppercase; letter-spacing: .5px; }
.total { color: var(--text-mid); }

.results-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; flex: 1; min-height: 0; }

.findings-list-panel {
  background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--radius-lg);
  overflow-y: auto;
}

.detail-panel {
  background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--radius-lg);
  padding: 24px; overflow-y: auto; animation: fade-in .2s ease-out;
}

.detail-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; }

.sev-pill { font-size: 11px; font-weight: 800; padding: 3px 10px; border-radius: 8px; letter-spacing: .5px; }

.close-btn {
  background: none; border: none; color: var(--text-dim); cursor: pointer; font-size: 16px;
  padding: 4px 8px; border-radius: 4px; transition: background .15s;
}
.close-btn:hover { background: var(--bg-card); color: var(--text); }

.detail-title { font-size: 18px; font-weight: 700; margin-bottom: 6px; line-height: 1.4; }
.detail-id { font-size: 11px; color: var(--text-dim); margin-bottom: 20px; display: block; }

.detail-section { margin-bottom: 20px; }
.detail-label { font-size: 10px; font-weight: 700; letter-spacing: 1px; text-transform: uppercase; color: var(--text-dim); margin-bottom: 8px; }
.detail-text { font-size: 13px; color: var(--text-mid); line-height: 1.6; }

.evidence-block {
  background: var(--bg-void); padding: 14px; border-radius: var(--radius-sm);
  font-size: 12px; color: var(--text-mid); white-space: pre-wrap; word-break: break-all;
  max-height: 300px; overflow-y: auto; line-height: 1.5;
}

.detail-placeholder {
  display: flex; align-items: center; justify-content: center;
  background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--radius-lg);
  color: var(--text-dim); font-size: 14px;
}

.empty { padding: 48px; text-align: center; color: var(--text-dim); font-size: 14px; }
</style>

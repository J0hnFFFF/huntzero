<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type SubmitTarget, type BlackboardSnapshot } from '../api/client'

const jobIds = ref<string[]>([])
const selectedJob = ref<string | null>(null)
const snapshot = ref<BlackboardSnapshot | null>(null)
const loading = ref(false)
const submitLoading = ref(false)
const toast = ref({ msg: '', type: '', show: false })

// Submit form
const form = ref<SubmitTarget>({
  target: '',
  max_workers: 5,
  max_rounds: 30,
  max_tasks: 200,
  max_wall_time: 7200,
  stagnation_rounds: 3,
})

onMounted(fetchJobs)

async function fetchJobs() {
  try {
    const data = await api.getJobs()
    jobIds.value = data.job_ids || []
  } catch { /* cluster may be offline */ }
}

async function selectJob(id: string) {
  selectedJob.value = id
  loading.value = true
  try {
    snapshot.value = await api.getJobSnapshot(id)
  } catch (e: any) {
    showToast(`Failed to load: ${e.message}`, 'error')
  } finally {
    loading.value = false
  }
}

async function submitJob() {
  if (!form.value.target.trim()) return
  submitLoading.value = true
  try {
    const resp = await api.submitJobs([{ ...form.value }])
    showToast(resp.message || 'Submitted!', 'success')
    form.value.target = ''
    fetchJobs()
  } catch (e: any) {
    showToast(e.message, 'error')
  } finally {
    submitLoading.value = false
  }
}

function showToast(msg: string, type: string) {
  toast.value = { msg, type, show: true }
  setTimeout(() => { toast.value.show = false }, 3000)
}

const STATUS_ICON: Record<string, string> = {
  pending: '⏳', active: '🔵', suspected: '🟡', confirmed: '✅', discarded: '💀'
}

const SEV_COLORS: Record<string, string> = {
  critical: 'var(--red)', high: 'var(--orange)', medium: 'var(--yellow)', low: 'var(--blue)'
}
</script>

<template>
  <div class="jobs-page">
    <div class="jobs-grid">
      <!-- ── Left: Submit + Job List ── -->
      <div class="left-col">
        <!-- Submit Form -->
        <div class="panel">
          <div class="panel-head">SUBMIT TARGET</div>
          <form class="submit-form" @submit.prevent="submitJob">
            <div class="field">
              <label>Target URL / Path</label>
              <input v-model="form.target" type="text" placeholder="https://github.com/owner/repo" class="mono" />
            </div>
            <div class="params-grid">
              <div class="field">
                <label>Workers</label>
                <input v-model.number="form.max_workers" type="number" min="1" max="32" />
              </div>
              <div class="field">
                <label>Max Rounds</label>
                <input v-model.number="form.max_rounds" type="number" min="1" />
              </div>
              <div class="field">
                <label>Max Tasks</label>
                <input v-model.number="form.max_tasks" type="number" min="1" />
              </div>
              <div class="field">
                <label>Wall Time (s)</label>
                <input v-model.number="form.max_wall_time" type="number" min="60" />
              </div>
            </div>
            <button type="submit" class="btn-submit" :disabled="submitLoading || !form.target.trim()">
              {{ submitLoading ? 'Submitting…' : '🚀 Submit to Queue' }}
            </button>
          </form>
        </div>

        <!-- Job List -->
        <div class="panel">
          <div class="panel-head">
            <span>JOBS</span>
            <span class="badge mono">{{ jobIds.length }}</span>
          </div>
          <div class="job-list">
            <div v-for="id in jobIds" :key="id"
              class="job-item" :class="{ active: selectedJob === id }"
              @click="selectJob(id)">
              <span class="job-id mono">{{ id.slice(0, 12) }}</span>
              <span class="arrow">→</span>
            </div>
            <div v-if="!jobIds.length" class="empty">No jobs found. Submit a target above.</div>
          </div>
        </div>
      </div>

      <!-- ── Right: Snapshot Detail ── -->
      <div class="panel snapshot-panel">
        <div class="panel-head">
          <span>{{ selectedJob ? `JOB: ${selectedJob.slice(0, 12)}` : 'JOB DETAILS' }}</span>
        </div>
        <div v-if="loading" class="loading">Loading snapshot…</div>
        <div v-else-if="snapshot" class="snapshot-content">
          <!-- Stats -->
          <div class="snap-stats">
            <div class="snap-stat">
              <span class="val">{{ Object.keys(snapshot.hypotheses || {}).length }}</span>
              <span class="lbl">Hypotheses</span>
            </div>
            <div class="snap-stat">
              <span class="val" style="color:var(--red)">{{ (snapshot.findings || []).length }}</span>
              <span class="lbl">Findings</span>
            </div>
            <div class="snap-stat">
              <span class="val" style="color:var(--yellow)">{{ Object.keys(snapshot.tasks || {}).length }}</span>
              <span class="lbl">Tasks</span>
            </div>
          </div>

          <!-- Findings -->
          <div v-if="snapshot.findings?.length" class="snap-section">
            <div class="snap-section-title">🔴 Findings ({{ snapshot.findings.length }})</div>
            <div v-for="f in snapshot.findings" :key="f.id" class="snap-finding">
              <span class="sev-dot" :style="{ background: SEV_COLORS[f.severity] || 'var(--text-dim)' }" />
              <span class="sev-label" :style="{ color: SEV_COLORS[f.severity] }">{{ f.severity?.toUpperCase() }}</span>
              <span class="snap-finding-title">{{ f.title }}</span>
            </div>
          </div>

          <!-- Hypotheses -->
          <div class="snap-section">
            <div class="snap-section-title">💡 Hypotheses ({{ Object.keys(snapshot.hypotheses || {}).length }})</div>
            <div v-for="(h, hid) in snapshot.hypotheses" :key="hid" class="snap-hyp">
              <span class="hyp-icon">{{ STATUS_ICON[h.status] || '❓' }}</span>
              <span class="hyp-conf mono">{{ (h.confidence * 100).toFixed(0) }}%</span>
              <span class="hyp-desc">{{ h.description?.slice(0, 120) }}</span>
            </div>
          </div>
        </div>
        <div v-else class="empty">← Select a job to view its snapshot</div>
      </div>
    </div>

    <!-- Toast -->
    <div class="toast" :class="[toast.type, { show: toast.show }]">{{ toast.msg }}</div>
  </div>
</template>

<style scoped>
.jobs-page { height: calc(100vh - var(--header-h) - 48px); }

.jobs-grid { display: grid; grid-template-columns: 380px 1fr; gap: 16px; height: 100%; }

.left-col { display: flex; flex-direction: column; gap: 16px; overflow-y: auto; }

.panel {
  background: var(--bg-panel); border: 1px solid var(--border);
  border-radius: var(--radius-lg); overflow: hidden;
}
.panel-head {
  padding: 12px 18px; font-size: 10.5px; font-weight: 700; letter-spacing: 1.2px;
  text-transform: uppercase; color: var(--text-mid); border-bottom: 1px solid var(--border);
  display: flex; align-items: center; justify-content: space-between;
}
.badge { font-size: 10px; font-weight: 700; padding: 2px 8px; border-radius: 10px; background: var(--bg-card); color: var(--blue); border: 1px solid var(--border-mid); font-family: var(--font-mono); }

.submit-form { padding: 16px; display: flex; flex-direction: column; gap: 14px; }
.field label { font-size: 11px; font-weight: 600; color: var(--text-mid); letter-spacing: .5px; margin-bottom: 5px; display: block; }
.field input {
  width: 100%; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius-sm);
  color: var(--text); font-size: 13px; padding: 8px 12px; outline: none; transition: border-color .2s;
}
.field input:focus { border-color: var(--blue); box-shadow: 0 0 0 3px rgba(88,166,255,.12); }
.params-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }

.btn-submit {
  background: var(--blue); color: #000; border: none; cursor: pointer; font-family: var(--font-sans);
  font-weight: 700; font-size: 13px; padding: 10px 18px; border-radius: var(--radius-sm);
  box-shadow: 0 0 20px rgba(88,166,255,.2); transition: all .18s;
}
.btn-submit:hover:not(:disabled) { background: #79baff; transform: translateY(-1px); box-shadow: 0 4px 24px rgba(88,166,255,.35); }
.btn-submit:disabled { opacity: .4; cursor: not-allowed; }

.job-list { max-height: 300px; overflow-y: auto; }
.job-item {
  padding: 10px 18px; border-bottom: 1px solid var(--border-mid);
  display: flex; align-items: center; justify-content: space-between;
  cursor: pointer; transition: background .15s;
}
.job-item:hover { background: var(--bg-hover); }
.job-item.active { background: var(--bg-card); border-left: 3px solid var(--blue); }
.job-id { font-size: 13px; color: var(--blue); }
.arrow { color: var(--text-dim); font-size: 12px; }

.snapshot-panel { display: flex; flex-direction: column; }
.snapshot-content { overflow-y: auto; padding: 16px; flex: 1; }

.snap-stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 24px; }
.snap-stat { background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 14px; text-align: center; }
.snap-stat .val { font-size: 28px; font-weight: 800; font-family: var(--font-mono); display: block; }
.snap-stat .lbl { font-size: 10px; color: var(--text-dim); text-transform: uppercase; letter-spacing: .8px; font-weight: 600; }

.snap-section { margin-bottom: 20px; }
.snap-section-title { font-size: 12px; font-weight: 700; color: var(--text-mid); margin-bottom: 10px; }

.snap-finding { display: flex; align-items: center; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--border-mid); }
.sev-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.sev-label { font-size: 10px; font-weight: 800; letter-spacing: .5px; min-width: 56px; flex-shrink: 0; }
.snap-finding-title { font-size: 12px; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.snap-hyp { display: flex; align-items: flex-start; gap: 8px; padding: 6px 0; border-bottom: 1px solid var(--border-mid); font-size: 12px; }
.hyp-icon { flex-shrink: 0; }
.hyp-conf { color: var(--text-mid); font-size: 11px; min-width: 36px; flex-shrink: 0; }
.hyp-desc { color: var(--text-mid); line-height: 1.4; }

.loading, .empty { padding: 48px; text-align: center; color: var(--text-dim); font-size: 14px; }

.toast {
  position: fixed; bottom: 24px; right: 24px; background: var(--bg-card);
  border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 12px 20px;
  font-size: 13px; font-weight: 500; color: var(--text); box-shadow: 0 8px 32px rgba(0,0,0,.5);
  transform: translateY(16px); opacity: 0; pointer-events: none; transition: all .25s; z-index: 9999;
}
.toast.show { transform: translateY(0); opacity: 1; }
.toast.success { border-color: rgba(63,185,80,.4); color: var(--green); }
.toast.error { border-color: rgba(248,81,73,.4); color: var(--red); }
</style>

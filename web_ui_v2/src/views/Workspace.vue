<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api, type ProjectInfo, type ReportFile, type ReportContent } from '../api/client'

const projects = ref<ProjectInfo[]>([])
const loading = ref(false)
const selectedProject = ref<string | null>(null)
const reports = ref<ReportFile[]>([])
const reportMeta = ref<{ has_blackboard: boolean; has_audit_notes: boolean }>({ has_blackboard: false, has_audit_notes: false })
const reportContent = ref<ReportContent | null>(null)
const reportsLoading = ref(false)
const contentLoading = ref(false)
const deleteConfirm = ref<string | null>(null)
const toast = ref({ msg: '', type: '', show: false })
const search = ref('')
const sortBy = ref<'name' | 'date' | 'size'>('date')

onMounted(fetchProjects)

const filtered = computed(() => {
  let list = projects.value
  if (search.value.trim()) {
    const kw = search.value.toLowerCase()
    list = list.filter(p => p.name.toLowerCase().includes(kw))
  }
  if (sortBy.value === 'date') list = [...list].sort((a, b) => b.last_modified - a.last_modified)
  else if (sortBy.value === 'size') list = [...list].sort((a, b) => b.disk_size_mb - a.disk_size_mb)
  else list = [...list].sort((a, b) => a.name.localeCompare(b.name))
  return list
})

const totalDisk = computed(() => projects.value.reduce((s, p) => s + p.disk_size_mb, 0).toFixed(1))

async function fetchProjects() {
  loading.value = true
  try {
    const data = await api.getProjects()
    projects.value = data.projects || []
  } catch (e: any) {
    showToast(e.message, 'error')
  } finally {
    loading.value = false
  }
}

async function selectProject(name: string) {
  selectedProject.value = name
  reportContent.value = null
  reportsLoading.value = true
  try {
    const data = await api.getProjectReports(name)
    reports.value = data.reports || []
    reportMeta.value = data.meta
  } catch (e: any) {
    showToast(e.message, 'error')
  } finally {
    reportsLoading.value = false
  }
}

async function viewReport(filename: string) {
  if (!selectedProject.value) return
  contentLoading.value = true
  try {
    reportContent.value = await api.getReportContent(selectedProject.value, filename)
  } catch (e: any) {
    showToast(e.message, 'error')
  } finally {
    contentLoading.value = false
  }
}

async function confirmDelete(name: string) {
  deleteConfirm.value = name
}

async function doDelete() {
  if (!deleteConfirm.value) return
  const name = deleteConfirm.value
  deleteConfirm.value = null
  try {
    await api.deleteProject(name)
    showToast(`Deleted "${name}"`, 'success')
    if (selectedProject.value === name) {
      selectedProject.value = null
      reports.value = []
      reportContent.value = null
    }
    fetchProjects()
  } catch (e: any) {
    showToast(e.message, 'error')
  }
}

function timeAgo(ts: number): string {
  if (!ts) return '—'
  const s = Math.floor(Date.now() / 1000 - ts)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function showToast(msg: string, type: string) {
  toast.value = { msg, type, show: true }
  setTimeout(() => { toast.value.show = false }, 3000)
}
</script>

<template>
  <div class="workspace-page">
    <!-- ── Top Bar ── -->
    <div class="top-bar">
      <div class="top-left">
        <h2 class="page-title">📂 Workspace</h2>
        <span class="top-stat">{{ projects.length }} projects · {{ totalDisk }}MB</span>
      </div>
      <div class="top-right">
        <input v-model="search" type="text" placeholder="Search projects…" class="search-input" />
        <select v-model="sortBy" class="sort-select mono">
          <option value="date">Sort: Recent</option>
          <option value="name">Sort: Name</option>
          <option value="size">Sort: Size</option>
        </select>
      </div>
    </div>

    <!-- ── Main Grid ── -->
    <div class="main-grid">
      <!-- Left: Project List -->
      <div class="project-list-panel">
        <div v-if="loading" class="loading">Loading projects…</div>
        <div v-for="p in filtered" :key="p.name"
          class="project-card" :class="{ active: selectedProject === p.name }"
          @click="selectProject(p.name)">
          <div class="proj-head">
            <span class="proj-name">{{ p.name }}</span>
            <button class="del-btn" @click.stop="confirmDelete(p.name)" title="Delete">🗑</button>
          </div>
          <div class="proj-meta">
            <span>📄 {{ p.reports_count }} reports</span>
            <span>💾 {{ p.disk_size_mb }}MB</span>
            <span>🕐 {{ timeAgo(p.last_modified) }}</span>
          </div>
          <div class="proj-badges">
            <span v-if="p.has_blackboard" class="proj-badge bb">Blackboard</span>
            <span v-if="p.reports_count > 0" class="proj-badge report">Reports</span>
          </div>
        </div>
        <div v-if="!filtered.length && !loading" class="empty">No projects found.</div>
      </div>

      <!-- Middle: Reports List -->
      <div class="reports-panel">
        <div class="panel-head">
          <span>{{ selectedProject ? `${selectedProject} — Reports` : 'SELECT A PROJECT' }}</span>
        </div>
        <div v-if="reportsLoading" class="loading">Loading reports…</div>
        <div v-else-if="selectedProject">
          <div v-if="reportMeta.has_blackboard" class="report-item special" @click="viewReport('.blackboard.json')">
            <span class="report-icon">🧠</span>
            <span class="report-name">Blackboard Snapshot</span>
            <span class="report-type">JSON</span>
          </div>
          <div v-for="r in reports" :key="r.filename" class="report-item"
            @click="viewReport(r.filename)">
            <span class="report-icon">{{ r.type === 'md' ? '📝' : '📊' }}</span>
            <span class="report-name mono">{{ r.filename }}</span>
            <span class="report-size mono">{{ (r.size / 1024).toFixed(1) }}KB</span>
            <span class="report-time">{{ timeAgo(r.modified) }}</span>
          </div>
          <div v-if="!reports.length && !reportMeta.has_blackboard" class="empty">
            No reports for this project.
          </div>
        </div>
        <div v-else class="empty">← Select a project to view reports</div>
      </div>

      <!-- Right: Content Viewer -->
      <div class="content-panel">
        <div class="panel-head">
          <span>{{ reportContent ? reportContent.filename : 'REPORT VIEWER' }}</span>
          <span v-if="reportContent" class="badge mono">{{ (reportContent.size / 1024).toFixed(1) }}KB</span>
        </div>
        <div v-if="contentLoading" class="loading">Loading content…</div>
        <div v-else-if="reportContent" class="content-body">
          <pre v-if="reportContent.type === 'json'" class="content-pre mono">{{ reportContent.content }}</pre>
          <div v-else class="content-md" v-html="renderMd(reportContent.content)" />
        </div>
        <div v-else class="empty">← Select a report to view</div>
      </div>
    </div>

    <!-- Delete Confirm Modal -->
    <div class="modal-overlay" :class="{ open: deleteConfirm }" @click="deleteConfirm = null">
      <div class="modal" @click.stop>
        <h3>⚠️ Delete Project</h3>
        <p>Are you sure you want to delete <strong>{{ deleteConfirm }}</strong>?</p>
        <p class="modal-warn">This will permanently remove all source code, reports, and blackboard data.</p>
        <div class="modal-actions">
          <button class="btn-cancel" @click="deleteConfirm = null">Cancel</button>
          <button class="btn-delete" @click="doDelete">Delete</button>
        </div>
      </div>
    </div>

    <!-- Toast -->
    <div class="toast" :class="[toast.type, { show: toast.show }]">{{ toast.msg }}</div>
  </div>
</template>

<script lang="ts">
// Simple markdown-to-html (no dependency)
function renderMd(md: string): string {
  return md
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/^### (.+)$/gm, '<h3>$1</h3>')
    .replace(/^## (.+)$/gm, '<h2>$1</h2>')
    .replace(/^# (.+)$/gm, '<h1>$1</h1>')
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/^- (.+)$/gm, '<li>$1</li>')
    .replace(/\n{2,}/g, '<br><br>')
    .replace(/\n/g, '<br>')
}
export default { methods: { renderMd } }
</script>

<style scoped>
.workspace-page { display: flex; flex-direction: column; gap: 16px; height: calc(100vh - var(--header-h) - 48px); }

.top-bar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 18px; background: var(--bg-panel); border: 1px solid var(--border);
  border-radius: var(--radius-md);
}
.top-left { display: flex; align-items: center; gap: 16px; }
.page-title { font-size: 16px; font-weight: 700; }
.top-stat { font-size: 12px; color: var(--text-dim); }
.top-right { display: flex; gap: 10px; }
.search-input, .sort-select {
  background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--radius-sm);
  color: var(--text); font-size: 12px; padding: 6px 12px; outline: none;
}
.search-input { width: 200px; }
.search-input:focus { border-color: var(--blue); }

.main-grid { display: grid; grid-template-columns: 280px 300px 1fr; gap: 12px; flex: 1; min-height: 0; }

.project-list-panel, .reports-panel, .content-panel {
  background: var(--bg-panel); border: 1px solid var(--border);
  border-radius: var(--radius-lg); overflow-y: auto;
}

.panel-head {
  padding: 12px 16px; font-size: 10.5px; font-weight: 700; letter-spacing: 1px;
  text-transform: uppercase; color: var(--text-mid); border-bottom: 1px solid var(--border);
  display: flex; align-items: center; justify-content: space-between;
  position: sticky; top: 0; background: var(--bg-panel); z-index: 1;
}
.badge { font-size: 10px; padding: 2px 8px; border-radius: 10px; background: var(--bg-card); color: var(--blue); border: 1px solid var(--border-mid); }

.project-card {
  padding: 12px 14px; border-bottom: 1px solid var(--border-mid);
  cursor: pointer; transition: background .15s;
}
.project-card:hover { background: var(--bg-hover); }
.project-card.active { background: var(--bg-card); border-left: 3px solid var(--blue); }

.proj-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.proj-name { font-size: 13px; font-weight: 600; color: var(--text); }
.del-btn {
  background: none; border: none; cursor: pointer; font-size: 12px; opacity: .3;
  transition: opacity .15s; padding: 2px 4px;
}
.del-btn:hover { opacity: 1; }

.proj-meta { display: flex; gap: 12px; font-size: 11px; color: var(--text-dim); margin-bottom: 6px; }
.proj-badges { display: flex; gap: 6px; }
.proj-badge {
  font-size: 9px; font-weight: 700; padding: 1px 6px; border-radius: 6px;
  letter-spacing: .3px; text-transform: uppercase;
}
.proj-badge.bb { background: var(--purple-dim); color: var(--purple); }
.proj-badge.report { background: var(--green-dim); color: var(--green); }

.report-item {
  display: flex; align-items: center; gap: 10px; padding: 10px 14px;
  border-bottom: 1px solid var(--border-mid); cursor: pointer; transition: background .15s;
}
.report-item:hover { background: var(--bg-hover); }
.report-item.special { background: var(--purple-dim); }

.report-icon { font-size: 14px; flex-shrink: 0; }
.report-name { font-size: 12px; color: var(--text); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.report-size { font-size: 10px; color: var(--text-dim); flex-shrink: 0; }
.report-time { font-size: 10px; color: var(--text-dim); flex-shrink: 0; }
.report-type { font-size: 10px; color: var(--purple); font-weight: 600; }

.content-panel { display: flex; flex-direction: column; }
.content-body { flex: 1; overflow-y: auto; padding: 16px; }
.content-pre {
  font-size: 12px; line-height: 1.5; color: var(--text-mid);
  white-space: pre-wrap; word-break: break-all;
}
.content-md { font-size: 13px; line-height: 1.7; color: var(--text-mid); }
.content-md :deep(h1) { font-size: 20px; color: var(--text); margin: 16px 0 8px; }
.content-md :deep(h2) { font-size: 16px; color: var(--text); margin: 14px 0 6px; }
.content-md :deep(h3) { font-size: 14px; color: var(--text); margin: 12px 0 4px; }
.content-md :deep(code) { background: var(--bg-card); padding: 2px 6px; border-radius: 3px; font-size: 12px; color: var(--blue); }
.content-md :deep(strong) { color: var(--text); }

.loading, .empty { padding: 32px; text-align: center; color: var(--text-dim); font-size: 13px; }

/* Delete Modal */
.modal-overlay {
  position: fixed; inset: 0; background: rgba(1,4,9,.8); backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center; z-index: 999;
  opacity: 0; pointer-events: none; transition: opacity .2s;
}
.modal-overlay.open { opacity: 1; pointer-events: all; }
.modal {
  background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--radius-lg);
  padding: 24px; max-width: 420px; width: 90%;
}
.modal h3 { font-size: 16px; margin-bottom: 12px; }
.modal p { font-size: 13px; color: var(--text-mid); margin-bottom: 8px; }
.modal-warn { color: var(--red) !important; font-size: 12px !important; }
.modal-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 16px; }
.btn-cancel {
  background: var(--bg-panel); border: 1px solid var(--border); color: var(--text-mid);
  padding: 8px 16px; border-radius: var(--radius-sm); cursor: pointer; font-size: 13px;
}
.btn-delete {
  background: var(--red); border: none; color: #fff;
  padding: 8px 16px; border-radius: var(--radius-sm); cursor: pointer; font-size: 13px; font-weight: 600;
}

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

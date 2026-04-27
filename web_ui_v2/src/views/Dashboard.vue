<script setup lang="ts">
import { useClusterStore } from '../stores/cluster'
import StatCard from '../components/StatCard.vue'
import NodeCard from '../components/NodeCard.vue'
import FindingCard from '../components/FindingCard.vue'
import EventStream from '../components/EventStream.vue'

const store = useClusterStore()
</script>

<template>
  <div class="dashboard">
    <!-- ── Stats Row ── -->
    <div class="stats-row">
      <StatCard label="Nodes Online" :value="store.activeNodes.length" color="var(--blue)"
        :sub="`${store.nodes.length} total`" />
      <StatCard label="Queue Depth" :value="store.queueDepth" color="var(--yellow)" />
      <StatCard label="Findings" :value="store.totalFindings" color="var(--red)"
        :sub="`${store.findingsBySeverity.critical} critical`" />
      <StatCard label="Events" :value="store.events.length" color="var(--purple)"
        :sub="store.wsConnected ? 'streaming' : 'disconnected'" />
    </div>

    <!-- ── Main Grid ── -->
    <div class="main-grid">
      <!-- Left: Cluster Nodes + Recent Findings -->
      <div class="left-col">
        <section class="panel">
          <div class="panel-head">
            <span>CLUSTER NODES</span>
            <span class="badge mono">{{ store.nodes.length }}</span>
          </div>
          <div class="nodes-grid">
            <NodeCard v-for="n in store.nodes" :key="n.node_id" :node="n" />
            <div v-if="!store.nodes.length" class="empty-state">
              No nodes connected. Start a worker with<br>
              <code class="mono">docker compose up -d</code>
            </div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-head">
            <span>RECENT FINDINGS</span>
            <span class="badge mono">{{ store.findings.length }}</span>
          </div>
          <div class="findings-list">
            <FindingCard
              v-for="f in store.findings.slice(0, 8)"
              :key="f.id"
              :finding="f"
            />
            <div v-if="!store.findings.length" class="empty-state">
              No findings yet. Submit a target to start analysis.
            </div>
          </div>
        </section>
      </div>

      <!-- Right: Event Stream -->
      <section class="panel events-panel">
        <div class="panel-head">
          <span>LIVE EVENT STREAM</span>
          <span class="badge mono" :style="{ color: store.wsConnected ? 'var(--green)' : 'var(--red)' }">
            {{ store.wsConnected ? '● LIVE' : '○ OFF' }}
          </span>
        </div>
        <div class="events-scroll">
          <EventStream :events="store.events" />
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.dashboard { display: flex; flex-direction: column; gap: 20px; }

.stats-row {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 14px;
}

.main-grid {
  display: grid;
  grid-template-columns: 1fr 420px;
  gap: 16px;
  min-height: 0;
}

.left-col {
  display: flex; flex-direction: column; gap: 16px;
}

.panel {
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  overflow: hidden;
}

.panel-head {
  padding: 12px 18px;
  font-size: 10.5px; font-weight: 700; letter-spacing: 1.2px;
  text-transform: uppercase; color: var(--text-mid);
  border-bottom: 1px solid var(--border);
  display: flex; align-items: center; justify-content: space-between;
}

.badge {
  font-size: 10px; font-weight: 700; padding: 2px 8px;
  border-radius: 10px; background: var(--bg-card);
  color: var(--blue); border: 1px solid var(--border-mid);
}

.nodes-grid {
  padding: 14px;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 10px;
}

.findings-list { max-height: 400px; overflow-y: auto; }

.events-panel { display: flex; flex-direction: column; }
.events-scroll { flex: 1; overflow-y: auto; max-height: 700px; }

.empty-state {
  padding: 32px; text-align: center; color: var(--text-dim);
  font-size: 13px; line-height: 1.6;
}
.empty-state code {
  display: inline-block; margin-top: 8px; padding: 4px 10px;
  background: var(--bg-card); border-radius: 4px; font-size: 12px;
  color: var(--text-mid);
}

@media (max-width: 1100px) {
  .stats-row { grid-template-columns: repeat(2, 1fr); }
  .main-grid { grid-template-columns: 1fr; }
}
</style>

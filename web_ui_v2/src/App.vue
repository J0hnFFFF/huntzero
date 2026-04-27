<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useClusterStore } from './stores/cluster'
import StatusDot from './components/StatusDot.vue'

const store = useClusterStore()

onMounted(() => {
  store.fetchCluster()
  store.fetchFindings()
  store.startEventStream()
})

onUnmounted(() => {
  store.stopEventStream()
})

const navItems = [
  { path: '/', label: 'Dashboard', icon: '📊' },
  { path: '/findings', label: 'Findings', icon: '🔴' },
  { path: '/jobs', label: 'Jobs', icon: '📋' },
  { path: '/workspace', label: 'Workspace', icon: '📂' },
]
</script>

<template>
  <div class="app-shell">
    <!-- ── Header ── -->
    <header class="app-header">
      <div class="logo">
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none">
          <circle cx="12" cy="12" r="10" stroke="var(--blue)" stroke-width="1.5" opacity=".6"/>
          <circle cx="12" cy="12" r="4" fill="var(--blue)"/>
          <path d="M12 2v4M12 18v4M2 12h4M18 12h4" stroke="var(--blue)" stroke-width="1.5" stroke-linecap="round"/>
        </svg>
        <span>KIMISEC</span>
        <span class="version">CONSOLE</span>
      </div>

      <nav class="nav-tabs">
        <router-link
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="nav-tab"
          active-class="active"
        >
          <span class="nav-icon">{{ item.icon }}</span>
          {{ item.label }}
        </router-link>
      </nav>

      <div class="header-right">
        <div class="ws-status">
          <StatusDot :alive="store.wsConnected" />
          <span class="mono">{{ store.wsConnected ? 'LIVE' : 'OFFLINE' }}</span>
        </div>
      </div>
    </header>

    <!-- ── Main Content ── -->
    <main class="app-main">
      <router-view v-slot="{ Component }">
        <transition name="fade" mode="out-in">
          <component :is="Component" />
        </transition>
      </router-view>
    </main>
  </div>
</template>

<style scoped>
.app-shell {
  display: grid;
  grid-template-rows: var(--header-h) 1fr;
  height: 100vh;
  overflow: hidden;
}

.app-header {
  display: flex;
  align-items: center;
  padding: 0 24px;
  gap: 32px;
  border-bottom: 1px solid var(--border);
  background: rgba(1, 4, 9, .88);
  backdrop-filter: blur(20px);
  z-index: 100;
}

.logo {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 800;
  font-size: 14px;
  letter-spacing: 2px;
  color: var(--blue);
  text-shadow: 0 0 20px rgba(88, 166, 255, .3);
  flex-shrink: 0;
}

.logo .version {
  font-size: 10px;
  font-weight: 600;
  color: var(--text-dim);
  letter-spacing: 1.5px;
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
}

.nav-tabs {
  display: flex;
  gap: 4px;
  flex: 1;
}

.nav-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-mid);
  text-decoration: none;
  border-radius: var(--radius-sm);
  transition: all .15s;
}

.nav-tab:hover {
  color: var(--text);
  background: var(--bg-card);
}

.nav-tab.active {
  color: var(--text);
  background: var(--bg-card);
  box-shadow: inset 0 -2px 0 var(--blue);
}

.nav-icon { font-size: 15px; }

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-shrink: 0;
}

.ws-status {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-mid);
}

.app-main {
  overflow-y: auto;
  padding: 24px;
}
</style>

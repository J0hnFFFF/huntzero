import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory } from 'vue-router'
import App from './App.vue'
import './style.css'

import Dashboard from './views/Dashboard.vue'
import Findings from './views/Findings.vue'
import Jobs from './views/Jobs.vue'
import Workspace from './views/Workspace.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', name: 'dashboard', component: Dashboard },
    { path: '/findings', name: 'findings', component: Findings },
    { path: '/jobs', name: 'jobs', component: Jobs },
    { path: '/workspace', name: 'workspace', component: Workspace },
  ],
})

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')

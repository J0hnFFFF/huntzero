<script setup lang="ts">
defineProps<{
  label: string
  value: number | string
  color?: string
  sub?: string
}>()
</script>

<template>
  <div class="stat-card">
    <div class="stat-value" :style="color ? { color } : {}">{{ value }}</div>
    <div class="stat-label">{{ label }}</div>
    <div v-if="sub" class="stat-sub">{{ sub }}</div>
  </div>
</template>

<style scoped>
.stat-card {
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  transition: border-color .2s, transform .2s;
}
.stat-card:hover {
  border-color: var(--border-mid);
  transform: translateY(-2px);
}
.stat-value {
  font-size: 32px;
  font-weight: 800;
  line-height: 1;
  color: var(--text);
  font-family: var(--font-mono);
}
.stat-label {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .8px;
  text-transform: uppercase;
  color: var(--text-dim);
}
.stat-sub {
  font-size: 11px;
  color: var(--text-mid);
  font-family: var(--font-mono);
}
</style>

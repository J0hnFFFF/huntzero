<script setup lang="ts">
defineProps<{ alive: boolean }>()
</script>

<template>
  <span class="dot" :class="{ alive }" />
</template>

<style scoped>
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--text-dim);
  flex-shrink: 0;
  transition: background .3s, box-shadow .3s;
}
.dot.alive {
  background: var(--green);
  animation: pulse-green 2s infinite;
}
</style>

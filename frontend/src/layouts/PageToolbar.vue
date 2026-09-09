<template>
  <header class="page-toolbar">
    <div class="page-toolbar__inner">
      <h1>{{ title }}</h1>
      <div class="page-toolbar__actions"><slot /></div>
    </div>
  </header>
</template>
<script setup>defineProps({ title: { type: String, required: true } });</script>
<style scoped>
.page-toolbar { min-height: 60px; padding: 0 var(--xdev-content-padding); display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-4); border-bottom: 1px solid var(--xdev-border-subtle); background: var(--xdev-bg-panel); --wails-draggable: drag; }
.page-toolbar__inner { width: 100%; max-width: var(--xdev-content-max-width); display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-4); flex: 1; min-width: 0; }
h1 { margin: 0; color: var(--xdev-text-primary); font-size: clamp(16px, 1.4vw, 19px); font-weight: 650; letter-spacing: -0.01em; white-space: nowrap; }
.page-toolbar__actions { display: flex; align-items: center; gap: var(--xdev-space-3); flex-wrap: wrap; --wails-draggable: no-drag; }
@media (max-width: 799px) { .page-toolbar { padding: var(--xdev-space-3) var(--xdev-space-4); align-items: flex-start; flex-direction: column; } .page-toolbar__actions { width: 100%; } }
</style>

<template>
  <header class="page-toolbar">
    <h1>{{ title }}</h1>
    <div class="page-toolbar__actions"><slot /></div>
  </header>
</template>
<script setup>defineProps({ title: { type: String, required: true } });</script>
<style scoped>
.page-toolbar { min-height: 56px; padding: 0 var(--xdev-space-6); display: flex; align-items: center; justify-content: space-between; gap: var(--xdev-space-4); border-bottom: 1px solid var(--xdev-border-subtle); --wails-draggable: drag; }
h1 { margin: 0; color: var(--xdev-text-primary); font-size: var(--xdev-font-size-lg); }
.page-toolbar__actions { display: flex; align-items: center; gap: var(--xdev-space-3); flex-wrap: wrap; --wails-draggable: no-drag; }
@media (max-width: 799px) { .page-toolbar { padding: var(--xdev-space-3) var(--xdev-space-4); align-items: flex-start; flex-direction: column; } .page-toolbar__actions { width: 100%; } }
</style>

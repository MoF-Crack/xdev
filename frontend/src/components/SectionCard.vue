<template>
  <section class="section-card">
    <header v-if="title || description || $slots.title || $slots.description || $slots.actions" class="section-card__header">
      <div>
        <h2 v-if="title || $slots.title"><slot name="title">{{ title }}</slot></h2>
        <div v-if="description || $slots.description" class="section-card__description"><slot name="description">{{ description }}</slot></div>
      </div>
      <div v-if="$slots.actions" class="section-card__actions"><slot name="actions" /></div>
    </header>
    <div class="section-card__body"><slot /></div>
  </section>
</template>

<script setup>
defineProps({ title: { type: String, default: '' }, description: { type: String, default: '' } });
</script>

<style scoped>
.section-card { overflow: hidden; border: 1px solid var(--xdev-border-subtle); border-radius: var(--xdev-radius-lg); background: var(--xdev-bg-card); box-shadow: var(--xdev-shadow-sm); transition: border-color var(--xdev-motion-normal), box-shadow var(--xdev-motion-normal); }
.section-card:hover { border-color: var(--xdev-border-strong); box-shadow: var(--xdev-shadow-md); }
.section-card__header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--xdev-space-4); padding: var(--xdev-space-5) var(--xdev-space-6); border-bottom: 1px solid var(--xdev-border-subtle); }
h2 { margin: 0; font-size: var(--xdev-font-size-lg); color: var(--xdev-text-primary); }
.section-card__description { margin-top: var(--xdev-space-1); color: var(--xdev-text-muted); font-size: var(--xdev-font-size-sm); }
.section-card__actions { display: flex; gap: var(--xdev-space-2); }
.section-card__body { padding: var(--xdev-space-6); }
</style>

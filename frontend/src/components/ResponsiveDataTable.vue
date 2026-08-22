<template>
  <div class="responsive-data-table">
    <div v-if="narrow && $slots.card" class="responsive-data-table__cards">
      <slot v-for="(row, index) in (data || [])" name="card" :row="row" :index="index" :columns="validatedColumns" :key="getKey(row, index)" />
    </div>
    <slot v-else-if="$slots.default" :columns="validatedColumns" :data="data" />
    <table v-else class="responsive-data-table__table" data-testid="data-table">
      <thead>
        <tr>
          <th v-for="column in (validatedColumns || [])" :key="column.key">{{ column.title || column.key }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!(data && data.length)">
          <td :colspan="Math.max((validatedColumns || []).length, 1)" data-testid="column-keys">
            <slot name="empty">{{ (validatedColumns || []).map((column) => column.key).join(',') }}</slot>
          </td>
        </tr>
        <tr v-for="(row, index) in (data || [])" :key="getKey(row, index)">
          <td v-for="column in (validatedColumns || [])" :key="column.key">
            <slot :name="column.key" :row="row" :column="column">{{ row[column.key] }}</slot>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue';
defineOptions({ inheritAttrs: false });
const props = defineProps({ columns: { type: Array, required: true }, data: { type: Array, required: true }, rowKey: { type: [String, Function], default: 'id' }, breakpoint: { type: Number, default: 800 } });
const required = ['identity', 'status', 'actions'];
function validateColumns(columns) { const list = Array.isArray(columns) ? columns : []; if (!required.every((contract) => list.some((column) => column.contract === contract))) throw new Error('ResponsiveDataTable columns must preserve identity, status, and actions contracts'); }
validateColumns(props.columns);
const validatedColumns = shallowRef(props.columns);
watch(() => props.columns, (columns) => {
  validateColumns(columns);
  validatedColumns.value = columns;
});
const narrow = ref(false);
let media;
function update(event) { narrow.value = event.matches; }
function getKey(row, index) { return typeof props.rowKey === 'function' ? props.rowKey(row) : row[props.rowKey] ?? index; }
function removeMediaListener() { if (!media) return; media.removeEventListener ? media.removeEventListener('change', update) : media.removeListener?.(update); }
function createMediaListener() { removeMediaListener(); media = window.matchMedia?.(`(max-width: ${props.breakpoint - 1}px)`); if (!media) { narrow.value = false; return; } update(media); media.addEventListener ? media.addEventListener('change', update) : media.addListener?.(update); }
onMounted(createMediaListener);
watch(() => props.breakpoint, createMediaListener);
onBeforeUnmount(removeMediaListener);
</script>

<style scoped>
.responsive-data-table__cards { display: grid; gap: var(--xdev-space-3); }
.responsive-data-table__table { width: 100%; border-collapse: collapse; }
.responsive-data-table__table th, .responsive-data-table__table td { padding: 8px 10px; text-align: left; border-bottom: 1px solid var(--xdev-border-subtle); }
</style>

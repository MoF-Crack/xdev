<template>
  <div class="responsive-data-table">
    <div v-if="narrow && $slots.card" class="responsive-data-table__cards">
      <slot v-for="(row, index) in data" name="card" :row="row" :index="index" :columns="validatedColumns" :key="getKey(row, index)" />
    </div>
    <slot v-else-if="$slots.default" :columns="validatedColumns" :data="data" />
    <n-data-table v-else :columns="validatedColumns" :data="data" :row-key="getKey" v-bind="$attrs">
      <template v-for="(_, name) in $slots" #[name]="slotProps"><slot :name="name" v-bind="slotProps || {}" /></template>
    </n-data-table>
  </div>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue';
defineOptions({ inheritAttrs: false });
const props = defineProps({ columns: { type: Array, required: true }, data: { type: Array, required: true }, rowKey: { type: [String, Function], default: 'id' }, breakpoint: { type: Number, default: 800 } });
const required = ['identity', 'status', 'actions'];
function validateColumns(columns) { if (!required.every((contract) => columns.some((column) => column.contract === contract))) throw new Error('ResponsiveDataTable columns must preserve identity, status, and actions contracts'); }
validateColumns(props.columns);
const validatedColumns = shallowRef(props.columns);
watch(() => props.columns, (columns) => { validateColumns(columns); validatedColumns.value = columns; });
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
</style>

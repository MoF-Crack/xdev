<template>
  <n-modal
    :show="show"
    preset="dialog"
    :title="t('install.form_selector_title')"
    :positive-text="t('btn.install')"
    :negative-text="t('btn.cancel')"
    :positive-button-props="{ disabled: !selectedForm }"
    :mask-closable="false"
    :closable="false"
    @positive-click="confirm"
    @negative-click="cancel"
    @close="cancel"
  >
    <p class="form-selector__hint">{{ t('install.form_selector_hint', { name: displayName }) }}</p>
    <n-space vertical class="form-selector__options">
      <n-checkbox
        v-for="form in forms"
        :key="form"
        :checked="selectedForm === form"
        :aria-label="t(`form.${form}`)"
        @update:checked="checked => selectForm(form, checked)"
      >
        {{ t(`form.${form}`) }}
      </n-checkbox>
    </n-space>
  </n-modal>
</template>

<script setup>
import { computed, ref, watch } from 'vue';
import { NCheckbox, NModal, NSpace } from 'naive-ui';

const props = defineProps({
  show: Boolean,
  tool: { type: Object, default: null },
  t: { type: Function, required: true },
});
const emit = defineEmits(['update:show', 'confirm']);

const forms = computed(() => (Array.isArray(props.tool?.formFactor) ? props.tool.formFactor : [])
  .filter(Boolean)
  .map(form => String(form).toLowerCase()));
const displayName = computed(() => props.tool?.displayName || props.tool?.name || '');
const selectedForm = ref('');

function resetSelection() {
  selectedForm.value = forms.value[0] || '';
}

function selectForm(form, checked) {
  if (checked) selectedForm.value = form;
  else if (selectedForm.value === form) selectedForm.value = '';
}

function confirm() {
  if (!selectedForm.value) return false;
  emit('confirm', selectedForm.value);
  emit('update:show', false);
  return true;
}

function cancel() {
  emit('update:show', false);
}

watch(() => [props.show, props.tool?.name, forms.value.join('|')], ([show]) => {
  if (show) resetSelection();
}, { immediate: true });
</script>

<style scoped>
.form-selector__hint { margin: 0 0 var(--xdev-space-4); color: var(--xdev-text-muted); line-height: 1.5; }
.form-selector__options { align-items: flex-start; }
</style>

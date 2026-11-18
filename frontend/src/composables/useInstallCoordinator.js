import { computed, h, nextTick, shallowRef } from 'vue';

// One instance is shared by all installation entry points in AppContent.
// Ownership starts before preflight/confirmation and ends after terminal state.
// A batch owns one lease while its independent backend sessions run in parallel.
export function createInstallCoordinator() {
  const owner = shallowRef(null);
  function acquire(kind) {
    if (owner.value) return null;
    const token = Symbol(kind);
    owner.value = { token, kind };
    return {
      release() { if (owner.value?.token === token) owner.value = null; },
    };
  }
  return { acquire, busy: computed(() => owner.value !== null) };
}

// Resolve every Naive UI dismissal path exactly once. No caller leaves its
// ownership lease parked when the close button, Escape or mask dismisses it.
let confirmationCounter = 0;
export function confirmInstallAction(dialog, options) {
  const titleId = `install-confirmation-title-${++confirmationCounter}`;
  const nameDialog = async (element) => { await nextTick(); element?.closest?.('[role="dialog"]')?.setAttribute('aria-labelledby', titleId); };
  return new Promise((resolve) => {
    let settled = false;
    const finish = (answer) => { if (!settled) { settled = true; resolve(answer); } };
    dialog.warning({
      ...options,
      // Naive's imperative dialog does not link its visual title to role=dialog.
      // Name the dialog through the actual rendered title node, scoped by id.
      title: () => h('span', { id: titleId, ref: nameDialog }, typeof options.title === 'function' ? options.title() : options.title),
      onPositiveClick: () => finish(true),
      onNegativeClick: () => finish(false),
      onClose: () => finish(false),
      onEsc: () => finish(false),
      onMaskClick: () => finish(false),
    });
  });
}

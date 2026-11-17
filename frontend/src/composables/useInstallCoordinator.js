import { computed, shallowRef } from 'vue';

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
export function confirmInstallAction(dialog, options) {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (answer) => { if (!settled) { settled = true; resolve(answer); } };
    dialog.warning({
      ...options,
      onPositiveClick: () => finish(true),
      onNegativeClick: () => finish(false),
      onClose: () => finish(false),
      onEsc: () => finish(false),
      onMaskClick: () => finish(false),
    });
  });
}

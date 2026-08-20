import { ref } from 'vue';

export function useNotifications() {
  const notifications = ref([]);

  function addNotification(title, type = 'info') {
    notifications.value.unshift({
      id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      title,
      type,
      time: new Date().toLocaleTimeString(),
    });
    if (notifications.value.length > 50) {
      notifications.value = notifications.value.slice(0, 50);
    }
  }

  function clearNotifications() {
    notifications.value = [];
  }

  function dismissNotification(id) {
    notifications.value = notifications.value.filter((item) => item.id !== id);
  }

  return { notifications, addNotification, clearNotifications, dismissNotification };
}

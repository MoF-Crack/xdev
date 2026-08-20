import { describe, expect, it } from 'vitest';
import { useNotifications } from '../../src/composables/useNotifications.js';

describe('useNotifications', () => {
  it('adds, dismisses, and clears notifications', () => {
    const { notifications, addNotification, dismissNotification, clearNotifications } = useNotifications();
    addNotification('嗅探完成', 'success');
    expect(notifications.value).toHaveLength(1);
    expect(notifications.value[0].title).toBe('嗅探完成');
    expect(notifications.value[0].id).toBeTruthy();
    dismissNotification(notifications.value[0].id);
    expect(notifications.value).toHaveLength(0);
    addNotification('a');
    addNotification('b');
    clearNotifications();
    expect(notifications.value).toHaveLength(0);
  });

  it('caps the list at 50', () => {
    const { notifications, addNotification } = useNotifications();
    for (let i = 0; i < 55; i += 1) addNotification(`n${i}`);
    expect(notifications.value).toHaveLength(50);
    expect(notifications.value[0].title).toBe('n54');
  });
});

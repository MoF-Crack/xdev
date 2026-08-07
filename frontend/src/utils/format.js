export function formatBytes(bytes = 0) {
  const value = Number(bytes) || 0;
  if (value <= 0) return '0 B';

  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${parseFloat((value / (1024 ** index)).toFixed(1))} ${units[index]}`;
}

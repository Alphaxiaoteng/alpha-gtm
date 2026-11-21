// 趋势雷达共享的只读格式化函数：纯函数，无 React 依赖，可被编排层与子组件同时引用。
export function compactNumber(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '—';
  if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  return value.toLocaleString('zh-CN');
}

export function formatTime(value?: string): string {
  if (!value) return '尚未监测';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '时间待验证';
  return new Intl.DateTimeFormat('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

// 播放器时间：秒 → mm:ss。非法/未知值显示 --:--，不伪造 0:00。
export function formatAudioTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '--:--';
  const total = Math.floor(seconds);
  const minutes = Math.floor(total / 60);
  const secs = total % 60;
  return `${minutes}:${secs.toString().padStart(2, '0')}`;
}

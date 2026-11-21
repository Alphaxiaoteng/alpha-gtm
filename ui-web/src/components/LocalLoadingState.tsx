type LocalLoadingStateProps = {
  label: string;
  detail?: string;
  compact?: boolean;
  className?: string;
  testID?: string;
};

export function LocalLoadingState({
  label,
  detail = '只读取并恢复这台设备上的数据',
  compact = false,
  className = '',
  testID,
}: LocalLoadingStateProps) {
  return (
    <div
      data-testid={testID || 'local-loading-state'}
      role="status"
      aria-live="polite"
      aria-busy="true"
      aria-label={`${label}。${detail}`}
      className={`local-loading-state ${compact ? 'local-loading-state-compact' : ''} ${className}`}
    >
      <span className="local-loading-mark" aria-hidden="true">
        <span />
        <span />
        <span />
      </span>
      <span className="min-w-0">
        <span className="block text-xs font-semibold text-gray-800">{label}</span>
        <span className="mt-1 block text-[10px] leading-4 text-gray-500">{detail}</span>
      </span>
    </div>
  );
}

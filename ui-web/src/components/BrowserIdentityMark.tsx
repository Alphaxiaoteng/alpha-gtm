import type { EgoIdentity } from '../api';

type BrowserIdentityMarkProps = {
  identity: EgoIdentity;
  index: number;
  className?: string;
  inverted?: boolean;
};

export function BrowserIdentityMark({ identity, index, className = 'h-9 w-9', inverted = false }: BrowserIdentityMarkProps) {
  return (
    <span
      data-testid={`browser-identity-mark-${identity.id}`}
      role="img"
      aria-label={`${identity.name} 浏览器标识`}
      title={identity.name}
      className={`relative inline-flex shrink-0 items-center justify-center ${className}`}
    >
      {identity.avatar_url ? (
        <img src={identity.avatar_url} alt="" className={`h-full w-full rounded-full border object-cover ${inverted ? 'border-white' : 'border-brand-border'}`} draggable={false} />
      ) : (
        <span className={`flex h-full w-full items-center justify-center rounded-full border text-xs font-bold ${inverted ? 'border-white bg-white text-black' : 'border-brand-border bg-black text-white'}`}>
          {index + 1}
        </span>
      )}
    </span>
  );
}

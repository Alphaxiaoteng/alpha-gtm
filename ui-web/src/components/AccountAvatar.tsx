import React from 'react';
import { api } from '../api';
import { PlatformIcon } from './PlatformIcon';

type AccountAvatarProps = {
  src?: string;
  alt: string;
  platform: string;
  size?: 'sm' | 'md' | 'lg';
  className?: string;
  testId?: string;
};

const avatarSizeClass = {
  sm: 'h-8 w-8',
  md: 'h-9 w-9',
  lg: 'h-10 w-10',
} as const;
const avatarRetryLimit = 2;

function isManagedAvatarSource(src: string): boolean {
  try {
    const parsed = new URL(src, window.location.origin);
	return parsed.origin === window.location.origin && /^\/api\/(?:accounts|account-assets)\/\d+\/avatar$/.test(parsed.pathname);
  } catch {
    return false;
  }
}

export const AccountAvatar: React.FC<AccountAvatarProps> = ({
  src,
  alt,
  platform,
  size = 'lg',
  className = '',
  testId,
}) => {
  const [objectURL, setObjectURL] = React.useState('');
  const [failed, setFailed] = React.useState(false);
  const [retryVersion, setRetryVersion] = React.useState(0);
  const retryCount = React.useRef(0);
  const retryTimer = React.useRef<number | undefined>(undefined);
  const managed = Boolean(src && isManagedAvatarSource(src));

  const scheduleRetry = React.useCallback(() => {
    if (retryCount.current >= avatarRetryLimit) {
      setFailed(true);
      return;
    }
    retryCount.current += 1;
    setFailed(true);
    window.clearTimeout(retryTimer.current);
    retryTimer.current = window.setTimeout(() => {
      setFailed(false);
      setRetryVersion((current) => current + 1);
    }, 600 * retryCount.current);
  }, []);

  React.useEffect(() => {
    retryCount.current = 0;
    setRetryVersion(0);
    window.clearTimeout(retryTimer.current);
    return () => window.clearTimeout(retryTimer.current);
  }, [src]);

  React.useEffect(() => {
    let active = true;
    let createdURL = '';
    setObjectURL('');
    setFailed(false);
    if (src && managed) {
      void api.getAccountAvatarBlob(src).then((blob) => {
        if (!active) return;
        createdURL = URL.createObjectURL(blob);
        retryCount.current = 0;
        setObjectURL(createdURL);
      }).catch(() => {
        if (active) scheduleRetry();
      });
    }
    return () => {
      active = false;
      if (createdURL) URL.revokeObjectURL(createdURL);
    };
  }, [managed, retryVersion, scheduleRetry, src]);

  const displaySource = failed ? '' : managed ? objectURL : src || '';
	const hasPortrait = Boolean(displaySource);
  return (
    <span
      role="img"
      aria-label={alt}
      data-testid={testId}
	  className={`relative inline-flex shrink-0 items-center justify-center overflow-hidden ${hasPortrait ? 'rounded-full bg-brand-bg' : 'bg-transparent'} ${avatarSizeClass[size]} ${className}`}
    >
      {displaySource ? (
        <img
          key={retryVersion}
          src={displaySource}
          alt=""
          aria-hidden="true"
          className="block h-full w-full object-cover object-center"
          referrerPolicy="no-referrer"
          loading="lazy"
          decoding="async"
          draggable={false}
          onLoad={() => { retryCount.current = 0; }}
          onError={scheduleRetry}
        />
      ) : (
		<span aria-hidden="true" className="flex h-full w-full items-center justify-center">
		  <PlatformIcon platform={platform} className="h-full w-full" />
        </span>
      )}
    </span>
  );
};

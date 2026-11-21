import React from 'react';
import { Store } from 'lucide-react';

interface PlatformIconProps {
  platform: string;
  className?: string;
  title?: string;
}

// Bundled application artwork for every built-in platform. These are local
// files, so account management remains usable without a third-party icon CDN.
const officialAppIcon: Record<string, string> = {
  instagram: '/platforms/instagram.png',
  facebook: '/platforms/facebook.png',
  xhs: '/platforms/xhs.png',
  douyin: '/platforms/douyin.png',
  wechat_video: '/platforms/wechat_video.png',
  kuaishou: '/platforms/kuaishou.png',
  wechat_mp: '/platforms/wechat_mp.png',
  youtube: '/platforms/youtube.png',
  tiktok: '/platforms/tiktok.png',
  x: '/platforms/x.png',
  toutiao: '/platforms/toutiao.png',
  omtencent: '/platforms/omtencent.png',
  weishi: '/platforms/weishi.png',
  bilibili: '/platforms/bilibili.png',
  acfun: '/platforms/acfun.png',
  baijiahao: '/platforms/baijiahao.png',
  sina: '/platforms/sina.png',
  jianshu: '/platforms/jianshu.png',
  zhihu: '/platforms/zhihu.png',
  juejin: '/platforms/juejin.png',
  csdn: '/platforms/csdn.png',
  pdd: '/platforms/pdd.png',
  douyin_shop: '/platforms/douyin_shop.png',
  taobao: '/platforms/taobao.png',
  tiktok_shop: '/platforms/tiktok_shop.png',
  shopee: '/platforms/shopee.png',
  shopify: '/platforms/shopify.png',
  amazon_seller: '/platforms/amazon_seller.png',
  qianchuan: '/platforms/qianchuan.png',
  jd_shop: '/platforms/jd_shop.png',
  pinduoduo_shop: '/platforms/pinduoduo_shop.png',
  xhs_shop: '/platforms/xhs_shop.png',
  kuaishou_shop: '/platforms/kuaishou_shop.png',
  wechat_shop: '/platforms/wechat_shop.png',
  alibaba_1688: '/platforms/alibaba_1688.png',
  aliexpress: '/platforms/aliexpress.png',
  xianyu: '/platforms/xianyu.png',
};

const platformLabels: Record<string, string> = {
  qianchuan: '巨量千川',
  jd_shop: '京东店铺',
  pinduoduo_shop: '拼多多店铺',
  xhs_shop: '小红书店铺',
  kuaishou_shop: '快手小店',
  wechat_shop: '微信小店',
  alibaba_1688: '1688',
  aliexpress: 'AliExpress',
  xianyu: '闲鱼',
};

export const PlatformIcon: React.FC<PlatformIconProps> = ({
  platform,
  className = 'w-4 h-4',
  title,
}) => {
  const code = normalizePlatform(platform);
  const label = title || platformLabels[code] || code;
  const source = officialAppIcon[code];
  const [failedSources, setFailedSources] = React.useState<Set<string>>(() => new Set());
  const sourceFailed = source ? failedSources.has(source) : false;
  const rememberFailure = (failedSource: string) => {
    setFailedSources((current) => {
      const next = new Set(current);
      next.add(failedSource);
      return next;
    });
  };

  if (code === 'meta') {
    return (
      <span
        className={`relative inline-flex shrink-0 items-center justify-center ${className}`}
        title={label}
        role="img"
        aria-label={label}
        data-testid="platform-icon-meta"
		data-platform={code}
      >
        <img src={officialAppIcon.facebook} alt="" className="absolute left-0 top-0 h-[72%] w-[72%] rounded-[24%] object-cover ring-1 ring-white" draggable={false} onError={() => rememberFailure(officialAppIcon.facebook)} />
        <img src={officialAppIcon.instagram} alt="" className="absolute bottom-0 right-0 h-[72%] w-[72%] rounded-[24%] object-cover ring-1 ring-white" draggable={false} onError={() => rememberFailure(officialAppIcon.instagram)} />
      </span>
    );
  }

  return (
    <span
	  data-platform={code}
	  className={`inline-flex shrink-0 items-center justify-center bg-transparent ${className}`}
      title={label}
      role="img"
      aria-label={label}
    >
      {source && !sourceFailed ? (
        <img src={source} alt="" className="h-full w-full object-contain" draggable={false} onError={() => rememberFailure(source)} />
      ) : source ? (
        <Store
          className="h-full w-full text-gray-500"
          data-testid={`platform-icon-load-fallback-${code}`}
          aria-hidden="true"
        />
      ) : (
        // `custom` has no external brand. Keep it visibly distinct without
        // pretending it is a supported platform's official mark.
        <svg viewBox="0 0 24 24" className="h-full w-full bg-slate-600 p-[18%] text-white" aria-hidden="true">
          <path fill="currentColor" d="M4 5h16v3H4zm0 5.5h16v3H4zM4 16h16v3H4zM7 3h3v7H7zm7 4h3v7h-3zM9 14h3v7H9z" />
        </svg>
      )}
    </span>
  );
};

function normalizePlatform(platform: string): string {
  const code = (platform || '').toLowerCase().trim().replace(/[\s-]+/g, '_');
  if (code === 'twitter' || code === 'x_twitter') return 'x';
  if (code === 'weixin' || code === 'wechat_official' || code === 'mp' || code === 'wechat') return 'wechat_mp';
  if (code === 'channels' || code === 'shipinhao' || code === 'weixin_channels') return 'wechat_video';
  if (code === 'redbook' || code === 'xiaohongshu' || code === 'red') return 'xhs';
  if (code === 'kwai') return 'kuaishou';
  if (code === 'ig') return 'instagram';
  if (code === 'fb') return 'facebook';
  if (code === 'yt' || code === 'ytb') return 'youtube';
  return code;
}

export default PlatformIcon;

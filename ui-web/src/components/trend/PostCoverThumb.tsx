import React, { useEffect, useState } from 'react';
import { Check, ExternalLink, Film } from 'lucide-react';
import { formatAudioTime } from './format';

// 抖音部分黑底字幕视频的静态封面是纯黑帧（源端 cover/origin/dynamic 四个变体
// 全黑，并非下载失败）。检测到整帧接近全黑时，叠一层作品标题字幕，让黑屏
// 变成有信息量的卡片，并明确标注原因，避免被误认为「封面没加载到」。
const PostCoverThumb: React.FC<{
  src?: string;
  fallbackURL?: string;
  title: string;
  hasLocalCover: boolean;
  hasURL?: boolean;
  durationMs?: number;
  onOpen?: () => void;
}> = ({ src, fallbackURL, title, hasLocalCover, hasURL, durationMs, onOpen }) => {
  const [darkFrame, setDarkFrame] = useState(false);
  const [currentSrc, setCurrentSrc] = useState(src ?? '');

  // src 变化时（切换博主 / 重抓）重置检测与回退链。
  useEffect(() => {
    setCurrentSrc(src ?? '');
    setDarkFrame(false);
  }, [src]);

  const inspectBrightness = (img: HTMLImageElement) => {
    try {
      const canvas = document.createElement('canvas');
      canvas.width = 16;
      canvas.height = 16;
      const ctx = canvas.getContext('2d');
      if (!ctx) return;
      ctx.drawImage(img, 0, 0, 16, 16);
      const { data } = ctx.getImageData(0, 0, 16, 16);
      let max = 0;
      for (let i = 0; i < data.length; i += 4) {
        max = Math.max(max, data[i], data[i + 1], data[i + 2]);
      }
      // 整帧最亮像素仍低于 18 → 判定为黑帧。
      setDarkFrame(max < 18);
    } catch {
      // 跨域图片会污染 canvas，无法采样；保持原样，不误判。
    }
  };

  return (
    <div
      className="relative aspect-[9/16] w-full overflow-hidden bg-black"
      onClick={onOpen}
    >
      {currentSrc ? (
        <img
          src={currentSrc}
          alt={title}
          loading="lazy"
          className="h-full w-full object-contain transition duration-300 group-hover:scale-[1.02]"
          onLoad={(e) => inspectBrightness(e.currentTarget)}
          onError={() => {
            if (fallbackURL && currentSrc !== fallbackURL) {
              setCurrentSrc(fallbackURL);
              setDarkFrame(false);
            }
          }}
        />
      ) : (
        <div className="flex h-full w-full items-center justify-center text-white/40">
          <Film className="h-6 w-6" />
        </div>
      )}

      {/* 黑帧兜底字幕卡 */}
      {darkFrame && (
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 px-3 text-center">
          <p className="line-clamp-2 text-sm font-bold leading-snug text-amber-300 drop-shadow-[0_1px_2px_rgba(0,0,0,0.9)]">
            {title || '（无标题）'}
          </p>
          <span className="rounded bg-black/70 px-1.5 py-0.5 text-[9px] font-medium text-white/70">
            源封面为黑帧
          </span>
        </div>
      )}

      {hasLocalCover && (
        <span className="absolute left-1.5 top-1.5 inline-flex items-center gap-0.5 rounded bg-black/60 px-1.5 py-0.5 text-[9px] font-semibold text-emerald-300 backdrop-blur-xs">
          <Check className="h-2.5 w-2.5" />
          本地封面
        </span>
      )}
      {durationMs && durationMs > 0 ? (
        <span className="absolute bottom-1.5 right-1.5 rounded bg-black/75 px-1 py-0.5 font-mono text-[9px] font-medium text-white/90 backdrop-blur-xs">
          {formatAudioTime(durationMs / 1000)}
        </span>
      ) : null}
      {hasURL && (
        <span className="absolute right-1.5 top-1.5 hidden rounded bg-black/60 p-1 text-white backdrop-blur-xs group-hover:block" title="在平台打开原视频">
          <ExternalLink className="h-3 w-3" />
        </span>
      )}
    </div>
  );
};

export default PostCoverThumb;

import { FileText, Film, Image as ImageIcon } from 'lucide-react';
import type { ContentItem } from '../api';

export function ContentCover({ content, className = '', controls = false, domain = 'media' }: { content: ContentItem; className?: string; controls?: boolean; domain?: 'media' | 'shop' }) {
  const src = content.cover_media_id ? `/api/${domain === 'shop' ? 'shop/media' : 'media'}/${content.cover_media_id}/content` : '';
  const isVideo = content.cover_kind === 'video' || content.cover_mime_type?.startsWith('video/');
  const isImage = content.cover_kind === 'image' || content.cover_mime_type?.startsWith('image/');

  if (src && isVideo) {
    return <div className={`relative overflow-hidden bg-gray-950 ${className}`}>
      <video
        src={`${src}#t=0.1`}
        muted
        playsInline
        controls={controls}
        preload="metadata"
        onLoadedMetadata={(event) => {
          if (event.currentTarget.duration > 0.1) event.currentTarget.currentTime = 0.1;
        }}
        className="h-full w-full object-cover"
        aria-label={`${content.title || '视频内容'}首帧`}
      />
      <span className="absolute bottom-2 right-2 rounded-full bg-black/65 p-1.5 text-white"><Film className="h-3.5 w-3.5" /></span>
    </div>;
  }
  if (src && isImage) {
    return <div className={`overflow-hidden bg-gray-100 ${className}`}><img src={src} alt={`${content.title || '图文内容'}封面`} loading="lazy" className="h-full w-full object-cover" /></div>;
  }
  return <div className={`flex flex-col items-center justify-center bg-gradient-to-br from-brand-accent/10 via-brand-bg to-brand-accent/5 text-brand-accent ${className}`}>
    {content.type === 'article' ? <FileText className="h-8 w-8" /> : content.type === 'video' ? <Film className="h-8 w-8" /> : <ImageIcon className="h-8 w-8" />}
    <span className="mt-2 max-w-[80%] truncate text-xs font-semibold">{content.type === 'article' ? 'Markdown 文章' : content.type === 'video' ? '等待视频封面' : '等待图片封面'}</span>
  </div>;
}

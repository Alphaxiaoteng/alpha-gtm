type AlphaPixelMarkProps = {
  className?: string;
  decorative?: boolean;
};

const PIXEL_ALPHA = [
  '00011110000',
  '00110001100',
  '01100000110',
  '11000000110',
  '11001111110',
  '11011000110',
  '01110000110',
  '00111111100',
  '00000000110',
  '00000001100',
];

/** Black-and-white pixel alpha mark used by the single Alpha Agent. */
export function AlphaPixelMark({ className = 'h-8 w-8', decorative = false }: AlphaPixelMarkProps) {
  return (
    <span
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : 'Alpha Agent'}
      aria-hidden={decorative ? true : undefined}
      className={`inline-flex shrink-0 items-center justify-center overflow-hidden rounded-[22%] border border-black bg-black text-white ${className}`}
    >
      <svg viewBox="0 0 39 36" className="h-full w-full" fill="none" aria-hidden="true" focusable="false" shapeRendering="crispEdges">
        {PIXEL_ALPHA.flatMap((row, rowIndex) => [...row].map((pixel, columnIndex) => pixel === '1' ? (
          <rect key={`${rowIndex}-${columnIndex}`} x={3 + columnIndex * 3} y={3 + rowIndex * 3} width="3" height="3" fill="currentColor" />
        ) : null))}
      </svg>
    </span>
  );
}

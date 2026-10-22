import { useEffect, useRef } from 'react';

interface VideoSurfaceProps {
  stream: MediaStream | null;
  muted?: boolean;
  mirrored?: boolean;
  className?: string;
  label?: string;
}

/**
 * Binds a MediaStream to a <video> element.
 *
 * `srcObject` is assigned imperatively because React cannot express it in JSX,
 * and `play()` is retried because some browsers reject autoplay until the
 * element is visible.
 */
export function VideoSurface({ stream, muted, mirrored, className, label }: VideoSurfaceProps) {
  const ref = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    if (element.srcObject !== stream) {
      element.srcObject = stream;
    }
    if (stream) {
      void element.play().catch(() => {
        // Autoplay policies may require an extra user gesture; the poster stays.
      });
    }
  }, [stream]);

  return (
    <video
      ref={ref}
      className={[className, mirrored ? 'is-mirrored' : ''].filter(Boolean).join(' ')}
      autoPlay
      playsInline
      muted={muted}
      aria-label={label}
    />
  );
}

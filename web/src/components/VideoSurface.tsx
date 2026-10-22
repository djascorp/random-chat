import { useEffect, useRef } from 'react';

interface VideoSurfaceProps {
  stream: MediaStream | null;
  muted?: boolean;
  mirrored?: boolean;
  className?: string;
  label?: string;
  /** 0–1 output level for this element (applies to unmuted remote audio). */
  volume?: number;
  /** Escape hatch for imperative APIs (fullscreen, Picture-in-Picture). */
  elementRef?: (element: HTMLVideoElement | null) => void;
}

/**
 * Binds a MediaStream to a <video> element.
 *
 * `srcObject` is assigned imperatively because React cannot express it in JSX,
 * and `play()` is retried because some browsers reject autoplay until the
 * element is visible.
 */
export function VideoSurface({ stream, muted, mirrored, className, label, volume, elementRef }: VideoSurfaceProps) {
  const ref = useRef<HTMLVideoElement | null>(null);

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

  useEffect(() => {
    const element = ref.current;
    if (!element || volume === undefined) return;
    const clamped = Math.min(1, Math.max(0, volume));
    element.volume = clamped;
    element.muted = Boolean(muted) || clamped === 0;
  }, [volume, muted]);

  return (
    <video
      ref={(element) => {
        ref.current = element;
        elementRef?.(element);
      }}
      className={[className, mirrored ? 'is-mirrored' : ''].filter(Boolean).join(' ')}
      autoPlay
      playsInline
      muted={muted}
      aria-label={label}
    />
  );
}

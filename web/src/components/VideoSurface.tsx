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
  /** Reports the decoded frame size so the surface can be shaped to it. */
  onIntrinsicSize?: (width: number, height: number) => void;
}

/**
 * Binds a MediaStream to a <video> element.
 *
 * `srcObject` is assigned imperatively because React cannot express it in JSX,
 * and `play()` is retried because some browsers reject autoplay until the
 * element is visible.
 */
export function VideoSurface({
  stream,
  muted,
  mirrored,
  className,
  label,
  volume,
  elementRef,
  onIntrinsicSize,
}: VideoSurfaceProps) {
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

  // The element is the only reliable source for the frame's aspect ratio: a
  // track's reported settings can lag or stay empty on remote streams, while
  // videoWidth/videoHeight always describe what is actually on screen. The
  // resize event fires when a camera rotates or changes resolution mid-call.
  useEffect(() => {
    const element = ref.current;
    if (!element || !onIntrinsicSize) return;
    const report = () => {
      if (element.videoWidth > 0 && element.videoHeight > 0) {
        onIntrinsicSize(element.videoWidth, element.videoHeight);
      }
    };
    report();
    element.addEventListener('loadedmetadata', report);
    element.addEventListener('resize', report);
    return () => {
      element.removeEventListener('loadedmetadata', report);
      element.removeEventListener('resize', report);
    };
  }, [stream, onIntrinsicSize]);

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

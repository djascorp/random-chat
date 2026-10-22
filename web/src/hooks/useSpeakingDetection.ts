import { useEffect, useRef, useState } from 'react';

const RMS_THRESHOLD = 0.045;
const POLL_MS = 150;

export interface SpeakingEntry {
  /** Stable identifier ('local' or a peer id). */
  key: string;
  stream: MediaStream | null;
}

/**
 * Flags which streams are currently producing sound, sampled from WebAudio
 * analysers. The AudioContext is created lazily — starting a call is already a
 * user gesture, so autoplay policy never blocks it. Levels are polled on a
 * short interval and the state object only changes when a flag actually flips,
 * so steady audio causes no re-renders. Analysers are rebuilt whenever the
 * entry list changes (peer joins/leaves, device switch).
 *
 * Known limitation: an analyser also hears the room through the speakers (no
 * echo cancellation at this level), so two rings can pulse together on loud
 * output devices. The threshold keeps false positives low.
 */
export function useSpeakingDetection(entries: SpeakingEntry[], active: boolean): Record<string, boolean> {
  const [speaking, setSpeaking] = useState<Record<string, boolean>>({});
  const ctxRef = useRef<AudioContext | null>(null);
  const analysersRef = useRef<Map<string, AnalyserNode>>(new Map());

  const signature = entries
    .map((entry) => `${entry.key}:${entry.stream ? entry.stream.id : 'none'}`)
    .join('|');

  useEffect(() => {
    if (!active || entries.length === 0) {
      setSpeaking((prev) => (Object.keys(prev).length === 0 ? prev : {}));
      return;
    }

    const Ctx =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!Ctx) return;
    if (!ctxRef.current) ctxRef.current = new Ctx();
    const ctx = ctxRef.current;
    void ctx.resume().catch(() => {});

    analysersRef.current.forEach((node) => node.disconnect());
    analysersRef.current.clear();
    for (const entry of entries) {
      if (!entry.stream || entry.stream.getAudioTracks().length === 0) continue;
      const source = ctx.createMediaStreamSource(entry.stream);
      const analyser = ctx.createAnalyser();
      analyser.fftSize = 512;
      source.connect(analyser);
      analysersRef.current.set(entry.key, analyser);
    }

    const buffer = new Uint8Array(256);
    const rms = (analyser?: AnalyserNode) => {
      if (!analyser) return 0;
      analyser.getByteTimeDomainData(buffer);
      let sum = 0;
      for (let i = 0; i < buffer.length; i += 1) {
        const v = (buffer[i] - 128) / 128;
        sum += v * v;
      }
      return Math.sqrt(sum / buffer.length);
    };

    const interval = window.setInterval(() => {
      setSpeaking((prev) => {
        let changed = false;
        const next: Record<string, boolean> = {};
        for (const [key, analyser] of analysersRef.current) {
          const value = rms(analyser) > RMS_THRESHOLD;
          next[key] = value;
          if (prev[key] !== value) changed = true;
        }
        return changed ? next : prev;
      });
    }, POLL_MS);

    return () => {
      window.clearInterval(interval);
      analysersRef.current.forEach((node) => node.disconnect());
      analysersRef.current.clear();
    };
    // `signature` summarises the entry list without re-running on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [signature, active]);

  useEffect(() => {
    return () => {
      void ctxRef.current?.close().catch(() => {});
      ctxRef.current = null;
    };
  }, []);

  return speaking;
}

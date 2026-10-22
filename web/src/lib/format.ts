/** Formats a duration in seconds as m:ss or h:mm:ss. */
export function formatDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  const pad = (value: number) => value.toString().padStart(2, '0');
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(rest)}` : `${minutes}:${pad(rest)}`;
}

/** Formats a timestamp as a short local time. */
export function formatClock(timestamp: number): string {
  return new Date(timestamp).toLocaleTimeString('fr-FR', {
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** Human readable byte size. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} o`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} Ko`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} Mo`;
}

/** Picks a friendly label for a camera without leaking the device id. */
export function deviceLabel(device: MediaDeviceInfo, index: number, kind: 'camera' | 'microphone'): string {
  if (device.label) return device.label;
  return kind === 'camera' ? `Caméra ${index + 1}` : `Micro ${index + 1}`;
}

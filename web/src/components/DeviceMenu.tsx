import { useEffect, useRef, useState } from 'react';
import { FlipCameraIcon, MicIcon, SlidersIcon, VideoIcon } from './Icons';
import { deviceLabel } from '../lib/format';

interface DeviceMenuProps {
  cameras: MediaDeviceInfo[];
  microphones: MediaDeviceInfo[];
  selectedCamera: string | null;
  selectedMicrophone: string | null;
  onSelectCamera: (deviceId: string) => void;
  onSelectMicrophone: (deviceId: string) => void;
}

/** Popover listing the available cameras and microphones. */
export function DeviceMenu({
  cameras,
  microphones,
  selectedCamera,
  selectedMicrophone,
  onSelectCamera,
  onSelectMicrophone,
}: DeviceMenuProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  const hasDevices = cameras.length > 0 || microphones.length > 0;

  return (
    <div className="device-menu" ref={containerRef}>
      <button
        type="button"
        className={`ctl ${open ? 'ctl--active' : ''}`}
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Périphériques"
      >
        <SlidersIcon size={20} />
        <span className="ctl__label">Périphériques</span>
      </button>

      {open ? (
        <div className="popover" role="menu">
          <div className="popover__section">
            <p className="popover__title">
              <VideoIcon size={15} /> Caméra
            </p>
            {cameras.length === 0 ? (
              <p className="popover__empty">Aucune caméra détectée</p>
            ) : (
              cameras.map((camera, index) => (
                <button
                  key={camera.deviceId}
                  type="button"
                  role="menuitemradio"
                  aria-checked={camera.deviceId === selectedCamera}
                  className={`popover__item ${camera.deviceId === selectedCamera ? 'is-selected' : ''}`}
                  onClick={() => {
                    onSelectCamera(camera.deviceId);
                    setOpen(false);
                  }}
                >
                  {deviceLabel(camera, index, 'camera')}
                </button>
              ))
            )}
          </div>

          <div className="popover__section">
            <p className="popover__title">
              <MicIcon size={15} /> Microphone
            </p>
            {microphones.length === 0 ? (
              <p className="popover__empty">Aucun micro détecté</p>
            ) : (
              microphones.map((microphone, index) => (
                <button
                  key={microphone.deviceId}
                  type="button"
                  role="menuitemradio"
                  aria-checked={microphone.deviceId === selectedMicrophone}
                  className={`popover__item ${microphone.deviceId === selectedMicrophone ? 'is-selected' : ''}`}
                  onClick={() => {
                    onSelectMicrophone(microphone.deviceId);
                    setOpen(false);
                  }}
                >
                  {deviceLabel(microphone, index, 'microphone')}
                </button>
              ))
            )}
          </div>

          <p className="popover__hint">
            <FlipCameraIcon size={14} />
            {hasDevices
              ? 'Le changement de périphérique est appliqué à chaud, sans interrompre l’appel.'
              : 'Autorisez la caméra pour voir la liste des périphériques.'}
          </p>
        </div>
      ) : null}
    </div>
  );
}

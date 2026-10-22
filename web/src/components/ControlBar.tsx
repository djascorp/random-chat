import { useLayoutEffect, useRef, useState } from 'react';
import type { SessionMode } from '../types';
import { ChatIcon, MicIcon, MicOffIcon, PhoneOffIcon, SkipIcon, UserPlusIcon, VideoIcon, VideoOffIcon, VolumeIcon, VolumeOffIcon } from './Icons';
import { DeviceMenu } from './DeviceMenu';
import { SearchPrefsMenu } from './SearchPrefs';
import { SessionMenu } from './SessionMenu';

interface ControlBarProps {
  micEnabled: boolean;
  camEnabled: boolean;
  inCall: boolean;
  isGroup: boolean;
  canAdd: boolean;
  chatOpen: boolean;
  unread: number;
  cameras: MediaDeviceInfo[];
  microphones: MediaDeviceInfo[];
  selectedCamera: string | null;
  selectedMicrophone: string | null;
  /** Remote audio level, 0–1. */
  volume: number;
  /** True while the WhatsApp-style idle fade has kicked in. */
  dimmed: boolean;
  searchMode: SessionMode;
  searchMax: number;
  onSearchPrefs: (mode: SessionMode, max: number) => void;
  onToggleMic: () => void;
  onToggleCam: () => void;
  onNext: () => void;
  onStop: () => void;
  onAdd: () => void;
  onToggleChat: () => void;
  onReport: () => void;
  onBlock: () => void;
  onSelectCamera: (deviceId: string) => void;
  onSelectMicrophone: (deviceId: string) => void;
  onVolumeChange: (volume: number) => void;
}

export function ControlBar({
  micEnabled,
  camEnabled,
  inCall,
  isGroup,
  canAdd,
  chatOpen,
  unread,
  cameras,
  microphones,
  selectedCamera,
  selectedMicrophone,
  volume,
  dimmed,
  searchMode,
  searchMax,
  onSearchPrefs,
  onToggleMic,
  onToggleCam,
  onNext,
  onStop,
  onAdd,
  onToggleChat,
  onReport,
  onBlock,
  onSelectCamera,
  onSelectMicrophone,
  onVolumeChange,
}: ControlBarProps) {
  const barRef = useRef<HTMLDivElement>(null);
  const [volumeOpen, setVolumeOpen] = useState(false);

  // Publishes the bar's real height as --controls-height so the stage reserves
  // exactly the right amount of room. Hard-coding that reserve broke as soon as
  // the bar wrapped onto a second row on narrower screens.
  useLayoutEffect(() => {
    const element = barRef.current;
    if (!element) return;

    const publish = () => {
      document.documentElement.style.setProperty('--controls-height', `${element.offsetHeight}px`);
    };
    publish();

    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', publish);
      return () => {
        window.removeEventListener('resize', publish);
        document.documentElement.style.removeProperty('--controls-height');
      };
    }

    const observer = new ResizeObserver(publish);
    observer.observe(element);
    return () => {
      observer.disconnect();
      document.documentElement.style.removeProperty('--controls-height');
    };
  }, []);

  return (
    <div className={`controls ${dimmed ? "controls--dim" : ""}`} ref={barRef}>
      <div className="controls__group">
        <button
          type="button"
          className={`ctl ${micEnabled ? '' : 'ctl--off'}`}
          onClick={onToggleMic}
          aria-pressed={!micEnabled}
          aria-label={micEnabled ? 'Couper le micro (M)' : 'Activer le micro (M)'}
          title={micEnabled ? 'Couper le micro (M)' : 'Activer le micro (M)'}
        >
          {micEnabled ? <MicIcon size={20} /> : <MicOffIcon size={20} />}
          <span className="ctl__label">{micEnabled ? 'Micro' : 'Micro coupé'}</span>
        </button>

        <button
          type="button"
          className={`ctl ${camEnabled ? '' : 'ctl--off'}`}
          onClick={onToggleCam}
          aria-pressed={!camEnabled}
          aria-label={camEnabled ? 'Couper la caméra (C)' : 'Activer la caméra (C)'}
          title={camEnabled ? 'Couper la caméra (C)' : 'Activer la caméra (C)'}
        >
          {camEnabled ? <VideoIcon size={20} /> : <VideoOffIcon size={20} />}
          <span className="ctl__label">{camEnabled ? 'Caméra' : 'Caméra coupée'}</span>
        </button>

        <DeviceMenu
          cameras={cameras}
          microphones={microphones}
          selectedCamera={selectedCamera}
          selectedMicrophone={selectedMicrophone}
          onSelectCamera={onSelectCamera}
          onSelectMicrophone={onSelectMicrophone}
        />

        <SearchPrefsMenu mode={searchMode} max={searchMax} onChange={onSearchPrefs} />
      </div>

      <div className="controls__group controls__group--primary">
        <button
          type="button"
          className="ctl ctl--primary"
          onClick={onNext}
          aria-label="Passer à l’interlocuteur suivant (N)"
          title="Passer au suivant (N)"
        >
          <SkipIcon size={22} />
          <span className="ctl__label">Suivant</span>
        </button>
        {isGroup ? (
          <button
            type="button"
            className="ctl"
            onClick={onAdd}
            disabled={!canAdd}
            aria-label={canAdd ? 'Ajouter un participant (A)' : 'Conférence complète'}
            title={canAdd ? 'Ajouter un participant (A)' : 'Conférence complète'}
          >
            <UserPlusIcon size={20} />
            <span className="ctl__label">Ajouter</span>
          </button>
        ) : null}
        <button
          type="button"
          className="ctl ctl--danger"
          onClick={onStop}
          aria-label="Quitter la conversation (Q)"
          title="Quitter la conversation (Q)"
        >
          <PhoneOffIcon size={20} />
          <span className="ctl__label">Quitter</span>
        </button>
      </div>

      <div className="controls__group">
        <div className="vol">
          <button
            type="button"
            className={`ctl ${volume === 0 ? 'ctl--muted' : ''}`}
            onClick={() => setVolumeOpen((open) => !open)}
            aria-expanded={volumeOpen}
            aria-label="Volume de l’appelant"
            title="Volume de l’appelant"
          >
            {volume === 0 ? <VolumeOffIcon size={20} /> : <VolumeIcon size={20} />}
            <span className="ctl__label">Volume</span>
          </button>
          {volumeOpen ? (
            <div className="vol__pop" role="group" aria-label="Réglage du volume de l’appelant">
              <span className="vol__icon">
                {volume === 0 ? <VolumeOffIcon size={16} /> : <VolumeIcon size={16} />}
              </span>
              <input
                type="range"
                min={0}
                max={100}
                step={1}
                value={Math.round(Math.min(1, Math.max(0, volume)) * 100)}
                onChange={(event) => onVolumeChange(Number(event.target.value) / 100)}
                aria-label="Niveau du volume"
              />
              <span className="vol__value">{Math.round(Math.min(1, Math.max(0, volume)) * 100)}%</span>
            </div>
          ) : null}
        </div>

        <button
          type="button"
          className={`ctl ${chatOpen ? 'ctl--active' : ''}`}
          onClick={onToggleChat}
          aria-label="Ouvrir le chat (T)"
          title="Ouvrir le chat (T)"
        >
          <ChatIcon size={20} />
          {unread > 0 ? <span className="ctl__badge">{unread > 9 ? '9+' : unread}</span> : null}
          <span className="ctl__label">Chat</span>
        </button>

        {isGroup ? null : <SessionMenu inCall={inCall} onReport={onReport} onBlock={onBlock} />}
      </div>
    </div>
  );
}

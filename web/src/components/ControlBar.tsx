import { useLayoutEffect, useRef, useState } from 'react';
import type { SessionMode } from '../types';
import { ChatIcon, LinkIcon, MicIcon, MicOffIcon, PhoneOffIcon, SkipIcon, UserPlusIcon, VideoIcon, VideoOffIcon, VolumeIcon, VolumeOffIcon } from './Icons';
import { DeviceMenu } from './DeviceMenu';
import { SearchPrefsMenu } from './SearchPrefs';
import { SessionMenu } from './SessionMenu';
import { useT } from '../i18n';

interface ControlBarProps {
  micEnabled: boolean;
  camEnabled: boolean;
  inCall: boolean;
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
  sessionMax: number;
  onSearchPrefs: (mode: SessionMode, max: number) => void;
  onToggleMic: () => void;
  onToggleCam: () => void;
  onNext: () => void;
  onStop: () => void;
  onAdd: () => void;
  onInvite: () => void;
  onUpdateCap: (max: number) => void;
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
  sessionMax,
  onSearchPrefs,
  onToggleMic,
  onToggleCam,
  onNext,
  onStop,
  onAdd,
  onInvite,
  onUpdateCap,
  onToggleChat,
  onReport,
  onBlock,
  onSelectCamera,
  onSelectMicrophone,
  onVolumeChange,
}: ControlBarProps) {
  const t = useT();
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
          aria-label={micEnabled ? t('ctl.mic.off') : t('ctl.mic.on')}
          title={micEnabled ? t('ctl.mic.off') : t('ctl.mic.on')}
        >
          {micEnabled ? <MicIcon size={20} /> : <MicOffIcon size={20} />}
          <span className="ctl__label">{micEnabled ? t('devices.microphone') : t('stage.cameraOff')}</span>
        </button>

        <button
          type="button"
          className={`ctl ${camEnabled ? '' : 'ctl--off'}`}
          onClick={onToggleCam}
          aria-pressed={!camEnabled}
          aria-label={camEnabled ? t('ctl.cam.off') : t('ctl.cam.on')}
          title={camEnabled ? t('ctl.cam.off') : t('ctl.cam.on')}
        >
          {camEnabled ? <VideoIcon size={20} /> : <VideoOffIcon size={20} />}
          <span className="ctl__label">{camEnabled ? t('devices.camera') : t('stage.cameraOff')}</span>
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
          aria-label={t('ctl.next')}
          title={t("ctl.next")}
        >
          <SkipIcon size={22} />
          <span className="ctl__label">{t('stage.caller')}</span>
        </button>
        <button
          type="button"
          className="ctl"
          onClick={onAdd}
          disabled={!canAdd}
          aria-label={canAdd ? t('ctl.add') : t('ctl.add.full')}
          title={canAdd ? t('ctl.add') : t('ctl.add.full')}
        >
          <UserPlusIcon size={20} />
          <span className="ctl__label">{t('ctl.add').split(' ')[0]}</span>
        </button>
        <button
          type="button"
          className="ctl"
          onClick={onInvite}
          aria-label={t('ctl.invite')}
          title={t("ctl.invite")}
        >
          <LinkIcon size={20} />
          <span className="ctl__label">{t('ctl.invite').split(' ')[0]}</span>
        </button>
        <button
          type="button"
          className="ctl ctl--danger"
          onClick={onStop}
          aria-label={t('ctl.stop')}
          title={t("ctl.stop")}
        >
          <PhoneOffIcon size={20} />
          <span className="ctl__label">{t('stage.error.back')}</span>
        </button>
      </div>

      <div className="controls__group">
        <div className="vol">
          <button
            type="button"
            className={`ctl ${volume === 0 ? 'ctl--muted' : ''}`}
            onClick={() => setVolumeOpen((open) => !open)}
            aria-expanded={volumeOpen}
            aria-label={t('ctl.volume')}
            title={t("ctl.volume")}
          >
            {volume === 0 ? <VolumeOffIcon size={20} /> : <VolumeIcon size={20} />}
            <span className="ctl__label">{t('ctl.volume')}</span>
          </button>
          {volumeOpen ? (
            <div className="vol__pop" role="group" aria-label={t("ctl.volume")}>
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
                aria-label={t("ctl.volume.level")}
              />
              <span className="vol__value">{Math.round(Math.min(1, Math.max(0, volume)) * 100)}%</span>
            </div>
          ) : null}
        </div>

        <button
          type="button"
          className={`ctl ${chatOpen ? 'ctl--active' : ''}`}
          onClick={onToggleChat}
          aria-label={t('ctl.chat')}
          title={t("ctl.chat")}
        >
          <ChatIcon size={20} />
          {unread > 0 ? <span className="ctl__badge">{unread > 9 ? '9+' : unread}</span> : null}
          <span className="ctl__label">{t('chat.title')}</span>
        </button>

        <SessionMenu
          inCall={inCall}
          sessionMax={sessionMax}
          onReport={onReport}
          onBlock={onBlock}
          onInvite={onInvite}
          onUpdateCap={onUpdateCap}
        />
      </div>
    </div>
  );
}

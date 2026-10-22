import { useCallback, useEffect, useRef, useState } from 'react';
import { useRandomChat } from './hooks/useRandomChat';
import type { SessionMode } from './types';
import { Landing } from './components/Landing';
import { CallStage } from './components/CallStage';
import { ControlBar } from './components/ControlBar';
import { ChatPanel } from './components/ChatPanel';
import { ReportDialog } from './components/ReportDialog';
import { ConsentGate } from './components/ConsentGate';
import { Toasts } from './components/Toasts';
import { SparkIcon } from './components/Icons';

const DEFAULT_MAX_RUNES = 1000;
const VOLUME_STORAGE_KEY = 'random-chat.volume';
const CONTROLS_IDLE_MS = 4000;

function readVolume(): number {
  try {
    const raw = window.localStorage.getItem(VOLUME_STORAGE_KEY);
    const parsed = raw === null ? NaN : Number(raw);
    return Number.isFinite(parsed) ? Math.min(1, Math.max(0, parsed)) : 1;
  } catch {
    return 1;
  }
}

export default function App() {
  const chat = useRandomChat();
  const [reportOpen, setReportOpen] = useState(false);
  const [reportTarget, setReportTarget] = useState<string | null>(null);
  const [volume, setVolume] = useState<number>(readVolume);
  const [controlsVisible, setControlsVisible] = useState(true);
  const idleTimerRef = useRef<number | null>(null);

  const active = chat.status !== 'idle';
  const inCall =
    chat.status === 'connected' || chat.status === 'connecting' || chat.status === 'reconnecting';
  const showControls = active && chat.status !== 'error';
  const controlsDimmed = inCall && !controlsVisible;
  const isGroup = chat.sessionMode === 'group';
  const membersCount = chat.members.length;
  const canAdd =
    isGroup && inCall && (chat.sessionMax === 0 || membersCount < chat.sessionMax);

  const maxRunes = chat.hello?.limits.max_chat_runes ?? DEFAULT_MAX_RUNES;
  const turnConfigured =
    chat.hello?.ice_servers.some((server) => {
      const urls = Array.isArray(server.urls) ? server.urls : [server.urls];
      return urls.some((url) => url.startsWith('turn:') || url.startsWith('turns:'));
    }) ?? false;

  const handleRetry = useCallback(() => {
    chat.dismissError();
    void chat.start(chat.searchMode, chat.searchMax);
  }, [chat]);

  const handleBack = useCallback(() => {
    chat.stop();
  }, [chat]);

  const handleStart = useCallback(
    (mode: SessionMode, max: number) => {
      void chat.start(mode, max);
    },
    [chat],
  );

  // Tiles in a conference act on one peer; the solo Plus menu acts on the
  // single other participant by default.
  const reportTargets = chat.remotePeers.map((peer, index) => ({
    id: peer.id,
    label: `Participant ${index + 2}`,
  }));

  const reportPeerIds = chat.remotePeers.map((peer) => peer.id).join(',');
  const openReport = useCallback(
    (peerId?: string) => {
      setReportTarget(peerId ?? reportPeerIds.split(',')[0] ?? null);
      setReportOpen(true);
    },
    [reportPeerIds],
  );

  const handleReportPeer = useCallback(
    (peerId: string) => {
      openReport(peerId);
    },
    [openReport],
  );

  const handleBlockPeer = useCallback(
    (peerId: string) => {
      chat.blockPeer(peerId);
    },
    [chat],
  );

  // WhatsApp behaviour: during a live call the bar fades away after a few
  // idle seconds and comes back on any input. Never hidden while a panel or
  // dialog is open, while searching, or on error screens.
  const dimActive = inCall && !chat.chatOpen && !reportOpen;
  const bumpControls = useCallback(() => {
    setControlsVisible(true);
    if (idleTimerRef.current !== null) window.clearTimeout(idleTimerRef.current);
    idleTimerRef.current = window.setTimeout(() => setControlsVisible(false), CONTROLS_IDLE_MS);
  }, []);

  useEffect(() => {
    if (!dimActive) {
      setControlsVisible((prev) => (prev ? prev : true));
      if (idleTimerRef.current !== null) {
        window.clearTimeout(idleTimerRef.current);
        idleTimerRef.current = null;
      }
      return;
    }
    idleTimerRef.current = window.setTimeout(() => setControlsVisible(false), CONTROLS_IDLE_MS);
    const events = ['pointermove', 'pointerdown', 'keydown', 'touchstart'] as const;
    events.forEach((name) => window.addEventListener(name, bumpControls, { passive: true }));
    return () => {
      if (idleTimerRef.current !== null) window.clearTimeout(idleTimerRef.current);
      events.forEach((name) => window.removeEventListener(name, bumpControls));
    };
  }, [dimActive, bumpControls]);

  const handleVolumeChange = useCallback((next: number) => {
    setVolume(next);
    try {
      window.localStorage.setItem(VOLUME_STORAGE_KEY, String(next));
    } catch {
      /* private mode — level just won't persist */
    }
  }, []);

  // Keyboard shortcuts for the in-call bar. Ignored while typing, while the
  // consent gate is showing, or before a session exists.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.tagName === 'SELECT' ||
          target.isContentEditable)
      ) {
        return;
      }
      if (!chat.consentAccepted || chat.status === 'idle' || chat.status === 'error') return;
      switch (event.key.toLowerCase()) {
        case 'n':
          event.preventDefault();
          chat.next();
          break;
        case 'q':
          event.preventDefault();
          chat.stop();
          break;
        case 'a':
          event.preventDefault();
          chat.addParticipant();
          break;
        case 'm':
          event.preventDefault();
          chat.toggleMic();
          break;
        case 'c':
          event.preventDefault();
          chat.toggleCam();
          break;
        case 't':
          event.preventDefault();
          chat.setChatOpen(!chat.chatOpen);
          break;
        case 'escape':
          if (reportOpen) setReportOpen(false);
          else if (chat.chatOpen) chat.setChatOpen(false);
          break;
        default:
          break;
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [chat, reportOpen]);

  return (
    <div className="app">
      <div className="aurora" aria-hidden="true">
        <span className="orb orb--one" />
        <span className="orb orb--two" />
        <span className="orb orb--three" />
        <span className="grain" />
      </div>

      <header className="topbar">
        <div className="brand">
          <span className="brand__mark">
            <SparkIcon size={18} />
          </span>
          <span className="brand__name">
            Random<span className="brand__accent">Chat</span>
          </span>
        </div>
        <span className="topbar__meta">
          {chat.publicStats ? (
            <>
              <span className="dot dot--live" />
              {chat.publicStats.online} en ligne
            </>
          ) : (
            'hors ligne'
          )}
        </span>
      </header>

      <main className={`shell ${active ? 'shell--stage' : ''}`}>
        {active ? (
          <CallStage
            status={chat.status}
            statusDetail={chat.statusDetail}
            remotePeers={chat.remotePeers}
            localStream={chat.localStream}
            camEnabled={chat.camEnabled}
            micEnabled={chat.micEnabled}
            sessionSeconds={chat.sessionSeconds}
            quality={chat.quality}
            sessionMode={chat.sessionMode}
            sessionMax={chat.sessionMax}
            membersCount={membersCount}
            error={chat.error}
            volume={volume}
            dimmed={controlsDimmed}
            onRetry={handleRetry}
            onBack={handleBack}
            onReportPeer={handleReportPeer}
            onBlockPeer={handleBlockPeer}
          />
        ) : (
          <Landing
            online={chat.publicStats?.online ?? null}
            activeSessions={chat.publicStats?.active_sessions ?? null}
            matchesTotal={chat.publicStats?.matches_total ?? null}
            turnConfigured={turnConfigured}
            onStart={handleStart}
            busy={false}
          />
        )}
      </main>

      {showControls ? (
        <ControlBar
          micEnabled={chat.micEnabled}
          camEnabled={chat.camEnabled}
          inCall={inCall}
          isGroup={isGroup}
          canAdd={canAdd}
          chatOpen={chat.chatOpen}
          unread={chat.unread}
          cameras={chat.devices.cameras}
          microphones={chat.devices.microphones}
          selectedCamera={chat.selectedCamera}
          selectedMicrophone={chat.selectedMicrophone}
          volume={volume}
          dimmed={controlsDimmed}
          searchMode={chat.searchMode}
          searchMax={chat.searchMax}
          onSearchPrefs={chat.changeSearchPrefs}
          onToggleMic={chat.toggleMic}
          onToggleCam={chat.toggleCam}
          onNext={chat.next}
          onStop={chat.stop}
          onAdd={chat.addParticipant}
          onToggleChat={() => chat.setChatOpen(!chat.chatOpen)}
          onReport={() => openReport()}
          onBlock={() => chat.blockPeer(reportTargets[0]?.id)}
          onSelectCamera={(deviceId) => void chat.switchCamera(deviceId)}
          onSelectMicrophone={(deviceId) => void chat.switchMicrophone(deviceId)}
          onVolumeChange={handleVolumeChange}
        />
      ) : null}

      <ChatPanel
        open={chat.chatOpen}
        messages={chat.messages}
        connected={inCall}
        status={chat.status}
        maxRunes={maxRunes}
        onClose={() => chat.setChatOpen(false)}
        onSend={chat.sendMessage}
      />

      <ReportDialog
        open={reportOpen}
        targets={reportTargets}
        initialTargetId={reportTarget}
        onClose={() => setReportOpen(false)}
        onSubmit={(category, reason, block, targetId) =>
          chat.submitReport(category, reason, block, targetId || undefined)
        }
      />

      {!chat.consentAccepted ? <ConsentGate onAccept={chat.acceptConsent} /> : null}

      <Toasts toasts={chat.toasts} onDismiss={chat.dismissToast} />
    </div>
  );
}

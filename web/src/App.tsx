import { useCallback, useEffect, useRef, useState } from 'react';
import { useRandomChat } from './hooks/useRandomChat';
import type { SessionMode } from './types';
import { Landing } from './components/Landing';
import { JoinScreen } from './components/JoinScreen';
import { ShareDialog } from './components/ShareDialog';
import { CallStage } from './components/CallStage';
import { ControlBar } from './components/ControlBar';
import { ChatPanel } from './components/ChatPanel';
import { ReportDialog } from './components/ReportDialog';
import { ConsentGate } from './components/ConsentGate';
import { Toasts } from './components/Toasts';
import { SparkIcon } from './components/Icons';
import { LangProvider, LangSwitcher } from './i18n';

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

/** Reads /j/<token> once at mount; anything else means the home screen. */
function readInviteToken(): string | null {
  try {
    const match = window.location.pathname.match(/^\/j\/([A-Za-z0-9-]{8,64})\/?$/);
    return match ? match[1] : null;
  } catch {
    return null;
  }
}

interface SearchPreset {
  mode: SessionMode;
  max: number;
  /** True when the URL carried at least one valid preset parameter. */
  preset: boolean;
}

/**
 * Reads ?mode=&max= once at mount. Invalid values are silently ignored in
 * favour of safe defaults, so a crafted link can never break the form.
 */
function readSearchPreset(): SearchPreset {
  const fallback: SearchPreset = { mode: 'solo', max: 0, preset: false };
  try {
    const params = new URLSearchParams(window.location.search);
    if (!params.has('mode') && !params.has('max')) return fallback;
    let mode: SessionMode = 'solo';
    let max = 0;
    let valid = false;
    const rawMode = (params.get('mode') ?? '').toLowerCase();
    if (rawMode === 'solo' || rawMode === 'group') {
      mode = rawMode;
      valid = true;
    }
    const rawMax = (params.get('max') ?? '').trim();
    if (rawMax !== '') {
      if (!/^\d{1,3}$/.test(rawMax)) return { mode: 'solo', max: 0, preset: false };
      const parsed = Number.parseInt(rawMax, 10);
      if (parsed !== 0 && (parsed < 2 || parsed > 99)) return { mode: 'solo', max: 0, preset: false };
      max = parsed;
      valid = true;
    }
    if (mode === 'solo') max = 0;
    return valid ? { mode, max, preset: true } : fallback;
  } catch {
    return fallback;
  }
}

export default function App() {
  const chat = useRandomChat();
  const [reportOpen, setReportOpen] = useState(false);
  const [reportTarget, setReportTarget] = useState<string | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  // /j/<token> opens the invitation screen; ?mode=&max= presets the search.
  // Both are read once at mount: the app never reads the URL afterwards.
  const [joinToken, setJoinToken] = useState<string | null>(readInviteToken);
  const [preset] = useState(readSearchPreset);
  const [volume, setVolume] = useState<number>(readVolume);
  const [controlsVisible, setControlsVisible] = useState(true);
  const idleTimerRef = useRef<number | null>(null);

  const active = chat.status !== 'idle';
  const inCall =
    chat.status === 'connected' || chat.status === 'connecting' || chat.status === 'reconnecting';
  const showControls = active && chat.status !== 'error';
  const controlsDimmed = inCall && !controlsVisible;
  const membersCount = chat.members.length;
  // Ajouter works from any live session: a solo call is promoted on the way.
  const canAdd = inCall && (chat.sessionMax === 0 || membersCount < chat.sessionMax);
  const inSession = membersCount > 0;

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

  const handleJoinByToken = useCallback(
    (token: string) => {
      void chat.joinByToken(token);
    },
    [chat],
  );

  // Leaving the invitation screen returns to a clean home URL.
  const handleJoinBack = useCallback(() => {
    chat.stop();
    chat.dismissJoinError();
    setJoinToken(null);
    try {
      window.history.replaceState(null, '', '/');
    } catch {
      /* private mode or embedded frame — the token just stays in the URL */
    }
  }, [chat]);

  const openShare = useCallback(() => {
    if (chat.members.length === 0) return;
    setShareOpen(true);
  }, [chat.members.length]);

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
  const dimActive = inCall && !chat.chatOpen && !reportOpen && !shareOpen;
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
        case 'i':
          event.preventDefault();
          openShare();
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
  }, [chat, reportOpen, openShare]);

  return (
    <LangProvider>
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
        <LangSwitcher />
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
        ) : joinToken ? (
          <JoinScreen
            token={joinToken}
            status={chat.status}
            joinError={chat.joinError}
            onJoin={handleJoinByToken}
            onBack={handleJoinBack}
          />
        ) : (
          <Landing
            online={chat.publicStats?.online ?? null}
            activeSessions={chat.publicStats?.active_sessions ?? null}
            matchesTotal={chat.publicStats?.matches_total ?? null}
            turnConfigured={turnConfigured}
            initialMode={preset.mode}
            initialMax={preset.max}
            preset={preset.preset}
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
          sessionMax={chat.sessionMax}
          onSearchPrefs={chat.changeSearchPrefs}
          onToggleMic={chat.toggleMic}
          onToggleCam={chat.toggleCam}
          onNext={chat.next}
          onStop={chat.stop}
          onAdd={chat.addParticipant}
          onInvite={openShare}
          onUpdateCap={chat.updateSessionCap}
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

      <ShareDialog
        open={shareOpen && inSession}
        invite={chat.invite}
        revoked={chat.inviteRevoked}
        sessionMax={chat.sessionMax}
        onRequest={(max, rotate) => chat.createInvite(max, rotate)}
        onRevoke={chat.revokeInvite}
        onClose={() => setShareOpen(false)}
      />

      {!chat.consentAccepted ? <ConsentGate onAccept={chat.acceptConsent} /> : null}

      <Toasts toasts={chat.toasts} onDismiss={chat.dismissToast} />
    </div>
    </LangProvider>
  );
}

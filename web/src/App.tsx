import { useCallback, useEffect, useState } from 'react';
import { useRandomChat } from './hooks/useRandomChat';
import { Landing } from './components/Landing';
import { CallStage } from './components/CallStage';
import { ControlBar } from './components/ControlBar';
import { ChatPanel } from './components/ChatPanel';
import { ReportDialog } from './components/ReportDialog';
import { ConsentGate } from './components/ConsentGate';
import { Toasts } from './components/Toasts';
import { SparkIcon } from './components/Icons';

const DEFAULT_MAX_RUNES = 1000;

export default function App() {
  const chat = useRandomChat();
  const [reportOpen, setReportOpen] = useState(false);

  const active = chat.status !== 'idle';
  const inCall =
    chat.status === 'connected' || chat.status === 'connecting' || chat.status === 'reconnecting';
  const showControls = active && chat.status !== 'error';

  const maxRunes = chat.hello?.limits.max_chat_runes ?? DEFAULT_MAX_RUNES;
  const turnConfigured =
    chat.hello?.ice_servers.some((server) => {
      const urls = Array.isArray(server.urls) ? server.urls : [server.urls];
      return urls.some((url) => url.startsWith('turn:') || url.startsWith('turns:'));
    }) ?? false;

  const handleRetry = useCallback(() => {
    chat.dismissError();
    void chat.start();
  }, [chat]);

  const handleBack = useCallback(() => {
    chat.stop();
  }, [chat]);

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
            remoteStream={chat.remoteStream}
            localStream={chat.localStream}
            camEnabled={chat.camEnabled}
            micEnabled={chat.micEnabled}
            sessionSeconds={chat.sessionSeconds}
            quality={chat.quality}
            error={chat.error}
            onRetry={handleRetry}
            onBack={handleBack}
          />
        ) : (
          <Landing
            online={chat.publicStats?.online ?? null}
            activeSessions={chat.publicStats?.active_sessions ?? null}
            matchesTotal={chat.publicStats?.matches_total ?? null}
            turnConfigured={turnConfigured}
            onStart={() => void chat.start()}
            busy={false}
          />
        )}
      </main>

      {showControls ? (
        <ControlBar
          micEnabled={chat.micEnabled}
          camEnabled={chat.camEnabled}
          inCall={inCall}
          chatOpen={chat.chatOpen}
          unread={chat.unread}
          cameras={chat.devices.cameras}
          microphones={chat.devices.microphones}
          selectedCamera={chat.selectedCamera}
          selectedMicrophone={chat.selectedMicrophone}
          onToggleMic={chat.toggleMic}
          onToggleCam={chat.toggleCam}
          onNext={chat.next}
          onStop={chat.stop}
          onToggleChat={() => chat.setChatOpen(!chat.chatOpen)}
          onReport={() => setReportOpen(true)}
          onBlock={chat.blockPeer}
          onSelectCamera={(deviceId) => void chat.switchCamera(deviceId)}
          onSelectMicrophone={(deviceId) => void chat.switchMicrophone(deviceId)}
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
        onClose={() => setReportOpen(false)}
        onSubmit={chat.submitReport}
      />

      {!chat.consentAccepted ? <ConsentGate onAccept={chat.acceptConsent} /> : null}

      <Toasts toasts={chat.toasts} onDismiss={chat.dismissToast} />
    </div>
  );
}

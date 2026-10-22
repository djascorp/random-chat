import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CallStatus, PeerStats, SessionMode } from '../types';
import type { RemotePeer } from '../hooks/useRandomChat';
import { VideoSurface } from './VideoSurface';
import {
  AlertIcon,
  BlockIcon,
  FlagIcon,
  MicOffIcon,
  PipIcon,
  UsersIcon,
  VideoOffIcon,
} from './Icons';
import { describeConnection } from '../lib/webrtc';
import { formatDuration } from '../lib/format';
import { useSpeakingDetection } from '../hooks/useSpeakingDetection';

interface CallStageProps {
  status: CallStatus;
  statusDetail: string;
  remotePeers: RemotePeer[];
  localStream: MediaStream | null;
  camEnabled: boolean;
  micEnabled: boolean;
  sessionSeconds: number;
  quality: PeerStats | null;
  sessionMode: SessionMode;
  sessionMax: number;
  membersCount: number;
  error: string | null;
  /** Remote audio level, 0–1 (applies to every peer surface). */
  volume: number;
  /** True while the WhatsApp-style idle fade has kicked in. */
  dimmed: boolean;
  onRetry: () => void;
  onBack: () => void;
  onReportPeer: (peerId: string) => void;
  onBlockPeer: (peerId: string) => void;
}

const SEARCHING: CallStatus[] = ['waiting', 'ended'];
const NEGOTIATING: CallStatus[] = ['connecting', 'reconnecting'];

const PIP_STORAGE_KEY = 'random-chat.pip-pos';
const PIP_MARGIN = 8;

interface PipPos {
  right: number;
  top: number;
}

function readPipPos(): PipPos | null {
  try {
    const raw = window.localStorage.getItem(PIP_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as PipPos;
    if (
      typeof parsed?.right !== 'number' ||
      typeof parsed?.top !== 'number' ||
      !Number.isFinite(parsed.right) ||
      !Number.isFinite(parsed.top)
    ) {
      return null;
    }
    return { right: Math.max(PIP_MARGIN, parsed.right), top: Math.max(PIP_MARGIN, parsed.top) };
  } catch {
    return null;
  }
}

interface DragState {
  pointerId: number;
  startX: number;
  startY: number;
  startRight: number;
  startTop: number;
  moved: boolean;
}

export function CallStage({
  status,
  statusDetail,
  remotePeers,
  localStream,
  camEnabled,
  micEnabled,
  sessionSeconds,
  quality,
  sessionMode,
  sessionMax,
  membersCount,
  error,
  volume,
  dimmed,
  onRetry,
  onBack,
  onReportPeer,
  onBlockPeer,
}: CallStageProps) {
  // Solo: swap exchanges the main frame with the self view. Group: pin lifts
  // one participant to the main frame with the rest in a side strip.
  const [swapped, setSwapped] = useState(false);
  const [pinnedId, setPinnedId] = useState<string | null>(null);
  const [pipPos, setPipPos] = useState<PipPos | null>(() => readPipPos());
  const stageRef = useRef<HTMLDivElement>(null);
  const pipRef = useRef<HTMLDivElement>(null);
  const mainVideoRef = useRef<HTMLVideoElement | null>(null);
  const dragRef = useRef<DragState | null>(null);

  const connection = describeConnection(quality);
  const inCall = status === 'connected' || status === 'reconnecting';
  const showLocal = Boolean(localStream);
  const requestingMedia = status === 'requesting-media';
  const isGroup = sessionMode === 'group';
  // Alone in a conference: the call lives, the room waits for Ajouter.
  const lone = isGroup && inCall && remotePeers.length === 0;

  const peerLabel = useCallback(
    (peer: RemotePeer) => `Participant ${remotePeers.indexOf(peer) + 2}`,
    [remotePeers],
  );

  const pinnedPeer = useMemo(
    () => (pinnedId ? remotePeers.find((peer) => peer.id === pinnedId) ?? null : null),
    [pinnedId, remotePeers],
  );
  // A pinned participant who leaves drops the spotlight back to the grid.
  useEffect(() => {
    if (pinnedId && !pinnedPeer) setPinnedId(null);
  }, [pinnedId, pinnedPeer]);

  const showGrid = isGroup && remotePeers.length > 0 && !pinnedPeer;
  const stripPeers = pinnedPeer ? remotePeers.filter((peer) => peer.id !== pinnedPeer.id) : [];
  // Solo tap-to-swap only makes sense with a live peer.
  const swap = !isGroup && swapped && inCall && Boolean(remotePeers[0]?.stream ?? false);
  const mainPeer = swap ? null : pinnedPeer;
  const mainSoloStream = swap ? localStream : (remotePeers[0]?.stream ?? null);
  const mainPeerStream = mainPeer?.stream ?? null;
  const hasMainVideo = mainPeer ? Boolean(mainPeerStream) : Boolean(mainSoloStream);

  const speakingEntries = useMemo(
    () => [{ key: 'local', stream: localStream }, ...remotePeers.map((peer) => ({ key: peer.id, stream: peer.stream }))],
    [localStream, remotePeers],
  );
  const speaking = useSpeakingDetection(speakingEntries, inCall && Boolean(localStream));
  // The ring follows the stream, wherever it is displayed.
  const mainSpeaking = mainPeer
    ? Boolean(speaking[mainPeer.id])
    : swap
      ? Boolean(speaking.local)
      : Boolean(speaking[remotePeers[0]?.id ?? '']);
  const pipSpeaking = swap ? Boolean(speaking[remotePeers[0]?.id ?? '']) : Boolean(speaking.local);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => {});
      return;
    }
    void stageRef.current?.requestFullscreen?.().catch(() => {});
  }, []);

  // F = fullscreen, S = swap main/self view (solo only). Same typing guard as
  // the App-level shortcuts so keys never fire while the chat input has focus.
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
      const key = event.key.toLowerCase();
      if (key === 'f') {
        event.preventDefault();
        toggleFullscreen();
      } else if (key === 's' && !isGroup && inCall) {
        event.preventDefault();
        setSwapped((prev) => !prev);
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [toggleFullscreen, isGroup, inCall]);

  // --- Draggable, tappable self view (drag to move; solo: tap to swap) ---
  const onPipPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    const stage = stageRef.current;
    const pip = pipRef.current;
    if (!stage || !pip || !event.isPrimary) return;
    const sRect = stage.getBoundingClientRect();
    const pRect = pip.getBoundingClientRect();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      startRight: sRect.right - pRect.right,
      startTop: pRect.top - sRect.top,
      moved: false,
    };
  };

  const onPipPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    const stage = stageRef.current;
    const pip = pipRef.current;
    if (!drag || drag.pointerId !== event.pointerId || !stage || !pip) return;
    if (Math.abs(event.clientX - drag.startX) + Math.abs(event.clientY - drag.startY) > 6) {
      drag.moved = true;
    }
    if (!drag.moved) return;
    const sRect = stage.getBoundingClientRect();
    const pRect = pip.getBoundingClientRect();
    const right = drag.startRight + (drag.startX - event.clientX);
    const top = drag.startTop + (event.clientY - drag.startY);
    setPipPos({
      right: Math.min(Math.max(PIP_MARGIN, right), Math.max(PIP_MARGIN, sRect.width - pRect.width - PIP_MARGIN)),
      top: Math.min(Math.max(PIP_MARGIN, top), Math.max(PIP_MARGIN, sRect.height - pRect.height - PIP_MARGIN)),
    });
  };

  const onPipPointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    dragRef.current = null;
    if (!drag || drag.pointerId !== event.pointerId) return;
    if (!drag.moved && !isGroup && inCall) {
      // Tap: bring the small frame to the foreground (WhatsApp behaviour).
      setSwapped((prev) => !prev);
      return;
    }
    setPipPos((pos) => {
      if (pos) {
        try {
          window.localStorage.setItem(PIP_STORAGE_KEY, JSON.stringify(pos));
        } catch {
          /* private mode — position just won't persist */
        }
      }
      return pos;
    });
  };

  const canBrowserPip =
    typeof document !== 'undefined' && 'pictureInPictureEnabled' in document && document.pictureInPictureEnabled;
  const toggleBrowserPip = () => {
    const element = mainVideoRef.current;
    if (!element) return;
    if (document.pictureInPictureElement) {
      void document.exitPictureInPicture().catch(() => {});
      return;
    }
    void element.requestPictureInPicture?.().catch(() => {
      // Feature-detected above; failures here are browser quirks — ignore.
    });
  };

  const scrimTitle = requestingMedia
    ? 'Activation de la caméra…'
    : lone
      ? 'En attente de participants…'
      : SEARCHING.includes(status)
        ? 'Recherche d’un interlocuteur…'
        : 'Connexion en cours…';
  const qualityHint =
    quality?.candidateType === 'relay'
      ? 'Appel relayé via TURN : la qualité peut être réduite sur ce réseau'
      : quality?.rttMs !== null && quality?.rttMs !== undefined
        ? `Aller-retour réseau : ${quality.rttMs} ms`
        : 'Mesure de la qualité de connexion en cours';

  if (status === 'error') {
    return (
      <div className="stage stage--message">
        <div className="stage__card">
          <span className="stage__card-icon stage__card-icon--bad">
            <AlertIcon size={26} />
          </span>
          <h2>La session s’est arrêtée</h2>
          <p>{error ?? 'Une erreur inattendue est survenue.'}</p>
          <div className="stage__card-actions">
            <button type="button" className="btn btn--primary" onClick={onRetry}>
              Réessayer
            </button>
            <button type="button" className="btn btn--ghost" onClick={onBack}>
              Retour à l’accueil
            </button>
          </div>
        </div>
      </div>
    );
  }

  const mainSurface = mainPeer ? (
    <div className={`stage__pinned ${mainSpeaking ? 'is-speaking' : ''}`}>
      {mainPeerStream ? (
        <VideoSurface
          stream={mainPeerStream}
          className="stage__video"
          volume={volume}
          elementRef={(element) => {
            mainVideoRef.current = element;
          }}
          label={`${peerLabel(mainPeer)} en grand`}
        />
      ) : (
        <div className="stage__off">
          <UsersIcon size={30} />
          <span>Connexion…</span>
        </div>
      )}
      <button
        type="button"
        className="stage__unpin"
        onClick={() => setPinnedId(null)}
        title="Revenir à la grille"
      >
        ← Grille
      </button>
      <span className="stage__local-badge">{peerLabel(mainPeer)}</span>
    </div>
  ) : swap ? (
    camEnabled ? (
      <VideoSurface
        stream={mainSoloStream}
        className="stage__video"
        muted
        mirrored
        elementRef={(element) => {
          mainVideoRef.current = element;
        }}
        label="Votre caméra en grand"
      />
    ) : (
      <div className="stage__off" role="img" aria-label="Votre caméra est coupée">
        <VideoOffIcon size={34} />
        <span>Votre caméra est coupée</span>
      </div>
    )
  ) : (
    <VideoSurface
      stream={inCall && !lone ? mainSoloStream : null}
      className="stage__video"
      volume={volume}
      elementRef={(element) => {
        mainVideoRef.current = element;
      }}
      label="Flux vidéo de votre interlocuteur"
    />
  );

  const renderTileActions = (peer: RemotePeer) => (
    <span className="tile__actions">
      <button
        type="button"
        className="tile__btn"
        onClick={(event) => {
          event.stopPropagation();
          onReportPeer(peer.id);
        }}
        title={`Signaler ${peerLabel(peer)}`}
        aria-label={`Signaler ${peerLabel(peer)}`}
      >
        <FlagIcon size={15} />
      </button>
      <button
        type="button"
        className="tile__btn tile__btn--danger"
        onClick={(event) => {
          event.stopPropagation();
          onBlockPeer(peer.id);
        }}
        title={`Bloquer ${peerLabel(peer)}`}
        aria-label={`Bloquer ${peerLabel(peer)}`}
      >
        <BlockIcon size={15} />
      </button>
    </span>
  );

  return (
    <div className={`stage ${dimmed ? 'stage--dim' : ''}`}>
      <div
        ref={stageRef}
        className={`stage__remote ${inCall ? 'is-live' : ''} ${!showGrid && mainSpeaking ? 'is-speaking' : ''}`}
        onDoubleClick={inCall ? toggleFullscreen : undefined}
        title={inCall ? 'Double-clic : plein écran · F plein écran · S échanger les vues' : undefined}
      >
        {showGrid ? (
          <div className="stage__videos" role="group" aria-label="Participants de la conférence">
            {remotePeers.map((peer, index) => (
              <div
                className="tile"
                key={peer.id}
                onClick={() => setPinnedId(peer.id)}
                role="button"
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    setPinnedId(peer.id);
                  }
                }}
                aria-label={`Agrandir ${peerLabel(peer)}`}
                title="Cliquer pour agrandir"
              >
                {peer.stream ? (
                  <VideoSurface
                    stream={peer.stream}
                    className={`tile__video ${speaking[peer.id] ? 'is-speaking-glow' : ''}`}
                    volume={volume}
                    label={`Participant ${index + 2}`}
                  />
                ) : (
                  <div className="tile__waiting">
                    <UsersIcon size={22} />
                    <span>Connexion…</span>
                  </div>
                )}
                <span className={`tile__label ${speaking[peer.id] ? 'is-speaking-text' : ''}`}>
                  Participant {index + 2}
                </span>
                {renderTileActions(peer)}
              </div>
            ))}
          </div>
        ) : (
          mainSurface
        )}

        {pinnedPeer && stripPeers.length > 0 ? (
          <div className="stage__strip" role="group" aria-label="Autres participants">
            {stripPeers.map((peer) => (
              <div
                className="tile tile--mini"
                key={peer.id}
                onClick={() => setPinnedId(peer.id)}
                role="button"
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault();
                    setPinnedId(peer.id);
                  }
                }}
                aria-label={`Agrandir ${peerLabel(peer)}`}
                title={`Agrandir ${peerLabel(peer)}`}
              >
                {peer.stream ? (
                  <VideoSurface
                    stream={peer.stream}
                    className="tile__video"
                    volume={volume}
                    label={peerLabel(peer)}
                  />
                ) : (
                  <div className="tile__waiting">
                    <UsersIcon size={16} />
                  </div>
                )}
                <span className="tile__label">{peerLabel(peer)}</span>
              </div>
            ))}
          </div>
        ) : null}

        {!inCall || lone ? (
          <div className="stage__scrim">
            <div className="radar" aria-hidden="true">
              <span className="radar__ring" />
              <span className="radar__ring" />
              <span className="radar__ring" />
              <span className="radar__dot">
                <UsersIcon size={26} />
              </span>
            </div>
            <h2 className="stage__scrim-title">{scrimTitle}</h2>
            <p className="stage__scrim-text">
              {requestingMedia
                ? 'Autorisez l’accès dans votre navigateur : votre image apparaît ci-dessous avant la mise en relation.'
                : lone
                  ? 'Utilisez le bouton Ajouter pour faire entrer quelqu’un, ou attendez qu’on vous rejoigne.'
                  : statusDetail || 'Vous serez mis en relation dès qu’une personne sera disponible.'}
            </p>
            {NEGOTIATING.includes(status) ? (
              <p className="stage__scrim-note">
                Le son et l’image peuvent mettre quelques secondes à apparaître.
              </p>
            ) : null}
          </div>
        ) : null}

        {inCall && !lone && !showGrid && !mainPeer && !swap && !mainSoloStream ? (
          <div className="stage__scrim stage__scrim--soft">
            <span className="stage__scrim-avatar" aria-hidden="true">
              <UsersIcon size={34} />
            </span>
            <p className="stage__scrim-text">En attente du flux vidéo…</p>
          </div>
        ) : null}

        {/* The HUD and the self view live inside the video frame so they stay
            anchored to it: the frame is capped in width on large screens, and
            positioning them against the stage pushed the self view outside it. */}
        <div className="stage__hud-scrim" aria-hidden="true" />
        <div className="stage__hud">
          {status === 'reconnecting' ? (
            <span className="pill pill--warn" title="La connexion est instable, reprise en cours">
              <span className="dot dot--pulse" />
              Reconnexion…
            </span>
          ) : (
            <span className={`pill ${inCall ? 'pill--ok' : 'pill--busy'}`}>
              <span className={`dot ${inCall ? 'dot--ok' : 'dot--pulse'}`} />
              {inCall
                ? isGroup
                  ? 'En conférence'
                  : 'En conversation'
                : requestingMedia
                  ? 'Caméra'
                  : SEARCHING.includes(status)
                    ? 'Recherche'
                    : 'Connexion'}
            </span>
          )}
          {inCall ? (
            <>
              <span className="pill pill--mono" title="Durée de la session en cours">
                {formatDuration(sessionSeconds)}
              </span>
              {isGroup ? (
                <span
                  className="pill pill--mono"
                  title={
                    sessionMax > 0
                      ? `Conférence : ${membersCount} participant${membersCount > 1 ? 's' : ''} sur ${sessionMax} maximum`
                      : `Conférence : ${membersCount} participant${membersCount > 1 ? 's' : ''}, sans limite`
                  }
                >
                  {membersCount}{sessionMax > 0 ? `/${sessionMax}` : ''}
                </span>
              ) : null}
              <span className={`pill pill--${connection.tone}`} title={qualityHint}>
                {connection.label}
              </span>
            </>
          ) : null}
          {quality?.bitrateKbps !== null &&
          quality?.bitrateKbps !== undefined &&
          inCall &&
          !isGroup ? (
            <span className="pill pill--mono" title="Débit vidéo reçu">
              {quality.bitrateKbps} kbps
            </span>
          ) : null}
          {canBrowserPip && inCall && !lone && (showGrid || hasMainVideo) ? (
            <button
              type="button"
              className="pill pill--action"
              onClick={toggleBrowserPip}
              title="Afficher la vidéo en surimpression (Picture-in-Picture)"
              aria-label="Afficher la vidéo en surimpression"
            >
              <PipIcon size={14} />
              Surimpression
            </button>
          ) : null}
        </div>

        {showLocal ? (
          <div
            ref={pipRef}
            className={`stage__local ${pipSpeaking ? 'is-speaking' : ''}`}
            style={pipPos ? { right: pipPos.right, top: pipPos.top } : undefined}
            onPointerDown={onPipPointerDown}
            onPointerMove={onPipPointerMove}
            onPointerUp={onPipPointerUp}
            onDoubleClick={(event) => event.stopPropagation()}
            role="button"
            tabIndex={0}
            aria-label={isGroup ? 'Déplacer votre caméra' : 'Appuyez pour échanger avec la grande vue'}
            title={isGroup ? 'Glissez pour déplacer' : 'Appuyez pour agrandir · glissez pour déplacer'}
          >
            {swap ? (
              remotePeers[0]?.stream ? (
                <VideoSurface
                  stream={remotePeers[0].stream}
                  className="stage__local-video"
                  volume={volume}
                  label="Flux de l’appelant"
                />
              ) : (
                <div className="stage__local-off">
                  <UsersIcon size={22} />
                  <span>Flux en attente…</span>
                </div>
              )
            ) : camEnabled ? (
              <VideoSurface
                stream={localStream}
                className="stage__local-video"
                muted
                mirrored
                label="Votre caméra (miniature)"
              />
            ) : (
              <div className="stage__local-off">
                <VideoOffIcon size={22} />
                <span>Caméra coupée</span>
              </div>
            )}
            <span className="stage__local-badge">
              {swap ? null : !micEnabled ? <MicOffIcon size={14} /> : null}
              {swap ? 'Appelant' : 'Vous'}
            </span>
          </div>
        ) : null}
      </div>
    </div>
  );
}

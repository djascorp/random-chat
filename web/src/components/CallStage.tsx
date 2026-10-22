import type { CallStatus, PeerStats, SessionMode } from '../types';
import type { RemotePeer } from '../hooks/useRandomChat';
import { VideoSurface } from './VideoSurface';
import { AlertIcon, BlockIcon, FlagIcon, MicOffIcon, UsersIcon, VideoOffIcon } from './Icons';
import { describeConnection } from '../lib/webrtc';
import { formatDuration } from '../lib/format';

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
  onRetry: () => void;
  onBack: () => void;
  onReportPeer: (peerId: string) => void;
  onBlockPeer: (peerId: string) => void;
}

const SEARCHING: CallStatus[] = ['waiting', 'ended'];
const NEGOTIATING: CallStatus[] = ['connecting', 'reconnecting'];

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
  onRetry,
  onBack,
  onReportPeer,
  onBlockPeer,
}: CallStageProps) {
  const connection = describeConnection(quality);
  const inCall = status === 'connected' || status === 'reconnecting';
  const showLocal = Boolean(localStream);
  const requestingMedia = status === 'requesting-media';
  const isGroup = sessionMode === 'group';
  // Alone in a conference: the call lives, the room waits for Ajouter.
  const lone = isGroup && inCall && remotePeers.length === 0;
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

  const showGrid = isGroup && remotePeers.length > 0;

  return (
    <div className="stage">
      <div className={`stage__remote ${inCall ? 'is-live' : ''}`}>
        {showGrid ? (
          <div className="stage__videos" role="group" aria-label="Participants de la conférence">
            {remotePeers.map((peer, index) => (
              <div className="tile" key={peer.id}>
                {peer.stream ? (
                  <VideoSurface
                    stream={peer.stream}
                    className="tile__video"
                    label={`Participant ${index + 2}`}
                  />
                ) : (
                  <div className="tile__waiting">
                    <UsersIcon size={22} />
                    <span>Connexion…</span>
                  </div>
                )}
                <span className="tile__label">Participant {index + 2}</span>
                <span className="tile__actions">
                  <button
                    type="button"
                    className="tile__btn"
                    onClick={() => onReportPeer(peer.id)}
                    title={`Signaler le participant ${index + 2}`}
                    aria-label={`Signaler le participant ${index + 2}`}
                  >
                    <FlagIcon size={15} />
                  </button>
                  <button
                    type="button"
                    className="tile__btn tile__btn--danger"
                    onClick={() => onBlockPeer(peer.id)}
                    title={`Bloquer le participant ${index + 2}`}
                    aria-label={`Bloquer le participant ${index + 2}`}
                  >
                    <BlockIcon size={15} />
                  </button>
                </span>
              </div>
            ))}
          </div>
        ) : (
          <VideoSurface
            stream={inCall && !lone ? (remotePeers[0]?.stream ?? null) : null}
            className="stage__video"
            label="Flux vidéo de votre interlocuteur"
          />
        )}

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

        {inCall && !lone && !showGrid && !remotePeers[0]?.stream ? (
          <div className="stage__scrim stage__scrim--soft">
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
        </div>

        {showLocal ? (
          <div className="stage__local">
            {camEnabled ? (
              <VideoSurface stream={localStream} className="stage__local-video" muted mirrored label="Votre caméra" />
            ) : (
              <div className="stage__local-off">
                <VideoOffIcon size={22} />
                <span>Caméra coupée</span>
              </div>
            )}
            <span className="stage__local-badge">
              {micEnabled ? null : <MicOffIcon size={14} />}
              Vous
            </span>
          </div>
        ) : null}
      </div>
    </div>
  );
}

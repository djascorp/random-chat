import type { CallStatus, PeerStats } from '../types';
import { VideoSurface } from './VideoSurface';
import { AlertIcon, MicOffIcon, UsersIcon, VideoOffIcon } from './Icons';
import { describeConnection } from '../lib/webrtc';
import { formatDuration } from '../lib/format';

interface CallStageProps {
  status: CallStatus;
  statusDetail: string;
  remoteStream: MediaStream | null;
  localStream: MediaStream | null;
  camEnabled: boolean;
  micEnabled: boolean;
  sessionSeconds: number;
  quality: PeerStats | null;
  error: string | null;
  onRetry: () => void;
  onBack: () => void;
}

// 'ended' only ever shows while the automatic requeue is already running, so it
// reads as a search rather than a negotiation.
const SEARCHING: CallStatus[] = ['waiting', 'ended'];
const NEGOTIATING: CallStatus[] = ['connecting', 'reconnecting'];

export function CallStage({
  status,
  statusDetail,
  remoteStream,
  localStream,
  camEnabled,
  micEnabled,
  sessionSeconds,
  quality,
  error,
  onRetry,
  onBack,
}: CallStageProps) {
  const connection = describeConnection(quality);
  const remoteActive = status === 'connected' || status === 'reconnecting';
  const showLocal = Boolean(localStream);
  const requestingMedia = status === 'requesting-media';
  const scrimTitle = requestingMedia
    ? 'Activation de la caméra…'
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

  return (
    <div className="stage">
      <div className={`stage__remote ${remoteActive ? 'is-live' : ''}`}>
        <VideoSurface
          stream={remoteActive ? remoteStream : null}
          className="stage__video"
          label="Flux vidéo de votre interlocuteur"
        />

        {!remoteActive ? (
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
                : statusDetail || 'Vous serez mis en relation dès qu’une personne sera disponible.'}
            </p>
            {NEGOTIATING.includes(status) ? (
              <p className="stage__scrim-note">
                Le son et l’image peuvent mettre quelques secondes à apparaître.
              </p>
            ) : null}
          </div>
        ) : null}

        {remoteActive && !remoteStream ? (
          <div className="stage__scrim stage__scrim--soft">
            <p className="stage__scrim-text">En attente du flux vidéo…</p>
          </div>
        ) : null}

        {/* The HUD and the self view live inside the video frame so they stay
            anchored to it: the frame is capped in width on large screens, and
            positioning them against the stage pushed the self view outside it. */}
        {/* Top shade so the pills stay readable over bright video. */}
        <div className="stage__hud-scrim" aria-hidden="true" />
        <div className="stage__hud">
          {status === 'reconnecting' ? (
            <span className="pill pill--warn" title="La connexion est instable, reprise en cours">
              <span className="dot dot--pulse" />
              Reconnexion…
            </span>
          ) : (
            <span className={`pill ${remoteActive ? 'pill--ok' : 'pill--busy'}`}>
              <span className={`dot ${remoteActive ? 'dot--ok' : 'dot--pulse'}`} />
              {remoteActive
                ? 'En conversation'
                : requestingMedia
                  ? 'Caméra'
                  : SEARCHING.includes(status)
                    ? 'Recherche'
                    : 'Connexion'}
            </span>
          )}
          {remoteActive ? (
            <>
              <span className="pill pill--mono" title="Durée de la session en cours">
                {formatDuration(sessionSeconds)}
              </span>
              <span className={`pill pill--${connection.tone}`} title={qualityHint}>
                {connection.label}
              </span>
            </>
          ) : null}
          {quality?.bitrateKbps !== null &&
          quality?.bitrateKbps !== undefined &&
          remoteActive ? (
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

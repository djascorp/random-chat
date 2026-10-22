import { useState } from 'react';
import type { SessionMode } from '../types';
import { BoltIcon, ChatIcon, GlobeIcon, ShieldIcon, SkipIcon, UsersIcon } from './Icons';

interface LandingProps {
  online: number | null;
  activeSessions: number | null;
  matchesTotal: number | null;
  turnConfigured: boolean;
  onStart: (mode: SessionMode, max: number) => void;
  busy: boolean;
}

const STEPS = [
  {
    icon: <BoltIcon size={22} />,
    title: 'Autorisez la caméra',
    text: 'Un clic, aucune inscription. Votre navigateur demande l’accès au micro et à la caméra.',
  },
  {
    icon: <UsersIcon size={22} />,
    title: 'On cherche pour vous',
    text: 'Vous êtes placé dans la file d’attente et mis en relation avec une personne disponible.',
  },
  {
    icon: <SkipIcon size={22} />,
    title: 'Suivant quand vous voulez',
    text: 'La conversation ne vous plaît pas ? Un clic ferme la session et relance la recherche.',
  },
];

export function Landing({ online, activeSessions, matchesTotal, turnConfigured, onStart, busy }: LandingProps) {
  const [mode, setMode] = useState<SessionMode>('solo');
  const [max, setMax] = useState('');

  const parsedMax = max.trim() === '' ? 0 : Number.parseInt(max, 10);
  const maxValid = max.trim() === '' || (Number.isInteger(parsedMax) && parsedMax >= 2 && parsedMax <= 99);

  return (
    <div className="landing">
      <section className="hero">
        <span className="hero__badge">
          <span className="dot dot--live" />
          {online === null ? 'Connexion…' : `${online} personne${online > 1 ? 's' : ''} en ligne`}
        </span>

        <h1 className="hero__title">
          Parlez à un inconnu,
          <span className="hero__title-soft">en vidéo, maintenant.</span>
        </h1>

        <p className="hero__lead">
          Un clic, une conversation en direct avec une personne tirée au hasard. Pas de compte, pas
          de profil, pas d’historique public. Vous gardez la main : micro, caméra et bouton
          « suivant » à tout moment.
        </p>

        <div className="mode-cards" role="radiogroup" aria-label="Type de conversation">
          <button
            type="button"
            role="radio"
            aria-checked={mode === 'solo'}
            className={`mode-card ${mode === 'solo' ? 'is-selected' : ''}`}
            onClick={() => setMode('solo')}
          >
            <UsersIcon size={22} />
            <span className="mode-card__title">Solo</span>
            <span className="mode-card__text">Tête-à-tête classique, comme avant.</span>
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={mode === 'group'}
            className={`mode-card ${mode === 'group' ? 'is-selected' : ''}`}
            onClick={() => setMode('group')}
          >
            <UsersIcon size={22} />
            <span className="mode-card__title">Conférence</span>
            <span className="mode-card__text">Plusieurs participants, bouton Ajouter en cours d’appel.</span>
          </button>
        </div>

        {mode === 'group' ? (
          <label className="max-field">
            <span className="max-field__label">Participants maximum (vide = sans limite)</span>
            <input
              className="max-field__input"
              type="number"
              inputMode="numeric"
              min={2}
              max={99}
              step={1}
              placeholder="Sans limite"
              value={max}
              aria-invalid={!maxValid}
              onChange={(event) => setMax(event.target.value)}
            />
            {!maxValid ? (
              <span className="max-field__error">Choisissez un nombre entre 2 et 99.</span>
            ) : null}
          </label>
        ) : null}

        <div className="hero__actions">
          <button
            type="button"
            className="btn btn--primary btn--lg"
            onClick={() => onStart(mode, maxValid ? parsedMax || 0 : 0)}
            disabled={busy || !maxValid}
          >
            <span className="btn__pulse" aria-hidden="true" />
            {busy
              ? 'Préparation…'
              : mode === 'group'
                ? maxValid && parsedMax > 0
                  ? `Rejoindre une conférence (max ${parsedMax})`
                  : 'Rejoindre une conférence'
                : 'Démarrer une conversation'}
          </button>
          <span className="hero__hint">
            {turnConfigured
              ? 'Relais TURN configuré pour les réseaux restrictifs'
              : 'Fonctionne sur les réseaux qui autorisent les connexions directes'}
          </span>
        </div>

        <dl className="metrics">
          <div className="metric">
            <dt>En ligne</dt>
            <dd>{online ?? '—'}</dd>
          </div>
          <div className="metric">
            <dt>Conversations actives</dt>
            <dd>{activeSessions ?? '—'}</dd>
          </div>
          <div className="metric">
            <dt>Mises en relation</dt>
            <dd>{matchesTotal ?? '—'}</dd>
          </div>
        </dl>
      </section>

      <section className="steps" aria-label="Comment ça marche">
        {STEPS.map((step, index) => (
          <article className="step" key={step.title}>
            <span className="step__index">{String(index + 1).padStart(2, '0')}</span>
            <span className="step__icon">{step.icon}</span>
            <h2 className="step__title">{step.title}</h2>
            <p className="step__text">{step.text}</p>
          </article>
        ))}
      </section>

      <section className="safety" aria-label="Sécurité et modération">
        <div className="safety__card">
          <ShieldIcon size={22} />
          <div>
            <h2>Signalement et blocage</h2>
            <p>
              Signalez un comportement inapproprié en un clic. Le blocage empêche toute nouvelle mise
              en relation avec la même personne.
            </p>
          </div>
        </div>
        <div className="safety__card">
          <ChatIcon size={22} />
          <div>
            <h2>Chat texte de secours</h2>
            <p>Si le son ne passe pas, écrivez : le chat reste disponible pendant toute la session.</p>
          </div>
        </div>
        <div className="safety__card">
          <GlobeIcon size={22} />
          <div>
            <h2>Appel pair à pair</h2>
            <p>
              Le navigateur négocie la connexion directement avec l’autre participant. Le serveur ne
              voit que la signalisation, jamais votre flux vidéo.
            </p>
          </div>
        </div>
      </section>
    </div>
  );
}

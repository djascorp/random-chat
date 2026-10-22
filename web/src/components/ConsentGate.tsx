import { useState } from 'react';
import { ShieldIcon } from './Icons';

interface ConsentGateProps {
  onAccept: () => void;
}

/**
 * Age and terms gate shown before any camera access.
 *
 * Asking for the camera before the visitor has agreed to the rules would be
 * both hostile and, in several jurisdictions, unlawful for a service that can
 * put a minor in front of a stranger.
 */
export function ConsentGate({ onAccept }: ConsentGateProps) {
  const [adult, setAdult] = useState(false);
  const [terms, setTerms] = useState(false);
  const ready = adult && terms;

  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-labelledby="consent-title">
      <div className="modal modal--consent">
        <div className="modal__icon">
          <ShieldIcon size={26} />
        </div>
        <h2 id="consent-title" className="modal__title">
          Avant de commencer
        </h2>
        <p className="modal__lead">
          Cet espace met en relation des personnes qui ne se connaissent pas, en direct et sans
          modération automatique du contenu. Merci de confirmer les points suivants.
        </p>

        <label className="check">
          <input type="checkbox" checked={adult} onChange={(event) => setAdult(event.target.checked)} />
          <span>J’ai 18 ans ou plus.</span>
        </label>

        <label className="check">
          <input type="checkbox" checked={terms} onChange={(event) => setTerms(event.target.checked)} />
          <span>
            J’accepte les règles : pas de nudité, pas de harcèlement, pas de contenu illégal, et
            j’accepte que tout signalement soit enregistré.
          </span>
        </label>

        <button type="button" className="btn btn--primary btn--block" disabled={!ready} onClick={onAccept}>
          J’ai compris, continuer
        </button>

        <p className="modal__fineprint">
          Vous pouvez signaler ou bloquer un interlocuteur à tout moment. Les signalements sont
          conservés pour permettre leur traitement.
        </p>
      </div>
    </div>
  );
}

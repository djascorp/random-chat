import { useT } from '../i18n';
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
  const t = useT();
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
          {t('consent.title')}
        </h2>
        <p className="modal__lead">
          {t('consent.text')}
        </p>

        <label className="check">
          <input type="checkbox" checked={adult} onChange={(event) => setAdult(event.target.checked)} />
          <span>{t('consent.adult')}</span>
        </label>

        <label className="check">
          <input type="checkbox" checked={terms} onChange={(event) => setTerms(event.target.checked)} />
          <span>
            {t('consent.terms')}
          </span>
        </label>

        <button type="button" className="btn btn--primary btn--block" disabled={!ready} onClick={onAccept}>
          {t('consent.cta')}
        </button>

        <p className="modal__fineprint">
          Vous pouvez signaler ou bloquer un interlocuteur à tout moment. Les signalements sont
          conservés pour permettre leur traitement.
        </p>
      </div>
    </div>
  );
}

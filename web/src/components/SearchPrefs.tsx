import { useEffect, useRef, useState } from 'react';
import type { SessionMode } from '../types';
import { UsersIcon } from './Icons';

/** Parses the member-cap field: empty means no constraint, otherwise 2-99. */
export function parseMaxParticipants(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === '') return 0;
  if (!/^\d{1,3}$/.test(trimmed)) return null;
  const value = Number.parseInt(trimmed, 10);
  if (!Number.isInteger(value) || value < 2 || value > 99) return null;
  return value;
}

interface SearchPrefsProps {
  mode: SessionMode;
  maxText: string;
  onModeChange: (mode: SessionMode) => void;
  onMaxChange: (text: string) => void;
}

/** Solo vs conference choice with the optional member cap. */
export function SearchPrefs({ mode, maxText, onModeChange, onMaxChange }: SearchPrefsProps) {
  const value = parseMaxParticipants(maxText) ?? 0;

  /* Touch stepper instead of a typeable field: 0 means "no limit", and the
     buttons keep the value inside 2-99 so the caller never sees an invalid
     state. */
  const stepMax = (direction: -1 | 1) => {
    const next =
      direction === 1
        ? value === 0
          ? 2
          : Math.min(99, value + 1)
        : value <= 2
          ? 0
          : value - 1;
    onMaxChange(next === 0 ? '' : String(next));
  };

  return (
    <>
      <div className="mode-cards" role="radiogroup" aria-label="Type de conversation">
        <button
          type="button"
          role="radio"
          aria-checked={mode === 'solo'}
          className={`mode-card ${mode === 'solo' ? 'is-selected' : ''}`}
          onClick={() => onModeChange('solo')}
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
          onClick={() => onModeChange('group')}
        >
          <UsersIcon size={22} />
          <span className="mode-card__title">Conférence</span>
          <span className="mode-card__text">Plusieurs participants, bouton Ajouter en cours d’appel.</span>
        </button>
      </div>

      {mode === 'group' ? (
        <div className="max-field" role="group" aria-label="Participants maximum">
          <span className="max-field__label">Participants maximum</span>
          <div className="stepper">
            <button
              type="button"
              className="stepper__btn"
              onClick={() => stepMax(-1)}
              disabled={value === 0}
              aria-label="Réduire le nombre maximum de participants"
            >
              −
            </button>
            <span className="stepper__value" aria-live="polite">
              {value === 0 ? 'Sans limite' : `${value} max`}
            </span>
            <button
              type="button"
              className="stepper__btn"
              onClick={() => stepMax(1)}
              disabled={value === 99}
              aria-label="Augmenter le nombre maximum de participants"
            >
              +
            </button>
          </div>
        </div>
      ) : null}
    </>
  );
}

interface SearchPrefsMenuProps {
  mode: SessionMode;
  max: number;
  onChange: (mode: SessionMode, max: number) => void;
}

/**
 * In-call search settings. Whatever is selected here is what the next
 * Suivant searches for — no need to leave the call to switch shape.
 */
export function SearchPrefsMenu({ mode, max, onChange }: SearchPrefsMenuProps) {
  const [open, setOpen] = useState(false);
  const [maxText, setMaxText] = useState(max === 0 ? '' : String(max));
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!containerRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open ]);

  const parsed = parseMaxParticipants(maxText);
  const handleMaxText = (text: string) => {
    setMaxText(text);
    const value = parseMaxParticipants(text);
    if (value !== null) onChange(mode, value);
  };

  return (
    <div className="device-menu" ref={containerRef}>
      <button
        type="button"
        className={`ctl ${open ? 'ctl--active' : ''}`}
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Type de recherche : ${mode === 'group' ? 'conférence' : 'solo'}. Appliqué au prochain Suivant.`}
        title="Type de recherche (appliqué au Suivant)"
      >
        <UsersIcon size={20} />
        <span className="ctl__label">{mode === 'group' ? (max > 0 ? `Conf. ${max}` : 'Conf.') : 'Solo'}</span>
      </button>

      {open ? (
        <div className="popover popover--wide" role="menu" aria-label="Type de recherche">
          <SearchPrefs
            mode={mode}
            maxText={maxText}
            onModeChange={(next) => onChange(next, parsed ?? 0)}
            onMaxChange={handleMaxText}
          />
          <p className="popover__hint">Pris en compte dès le prochain Suivant.</p>
        </div>
      ) : null}
    </div>
  );
}

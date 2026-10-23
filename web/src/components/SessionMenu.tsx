import { useT } from '../i18n';
import { useEffect, useRef, useState } from 'react';
import { BlockIcon, DotsIcon, FlagIcon, LinkIcon } from './Icons';

interface SessionMenuProps {
  inCall: boolean;
  sessionMax: number;
  onReport: () => void;
  onBlock: () => void;
  onInvite: () => void;
  onUpdateCap: (max: number) => void;
}

/**
 * Secondary session actions. Reporting and blocking leave the main bar so the
 * destructive "leave" action no longer sits next to them, and blocking asks
 * for a second tap because it can never be undone. The invitation link and
 * the participant limit live here too: every member may use them, since
 * anonymous rooms have no owner.
 */
export function SessionMenu({ inCall, sessionMax, onReport, onBlock, onInvite, onUpdateCap }: SessionMenuProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const [confirmingBlock, setConfirmingBlock] = useState(false);
  const [pendingMax, setPendingMax] = useState(sessionMax === 0 ? '' : String(sessionMax));

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

  useEffect(() => {
    if (!open) {
      setConfirmingBlock(false);
      return;
    }
    setPendingMax(sessionMax === 0 ? '' : String(sessionMax));
  }, [open, sessionMax ]);

  const parsedMax = parseCap(pendingMax);
  const capDirty = parsedMax !== null && parsedMax !== sessionMax;

  const stepMax = (direction: -1 | 1) => {
    const value = parsedMax ?? sessionMax;
    const next =
      direction === 1
        ? value === 0
          ? 2
          : Math.min(99, value + 1)
        : value <= 2
          ? 0
          : value - 1;
    setPendingMax(next === 0 ? '' : String(next));
  };

  return (
    <div className="device-menu device-menu--more" ref={containerRef}>
      <button
        type="button"
        className={`ctl ${open ? 'ctl--active' : ''}`}
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("session.more")}
        title={t("session.moreHint")}
      >
        <DotsIcon size={20} />
        <span className="ctl__label">Plus</span>
      </button>

      {open ? (
        <div className="popover popover--wide" role="menu" aria-label="Actions de session">
          <button
            type="button"
            role="menuitem"
            className="popover__item"
            disabled={!inCall}
            onClick={() => {
              setOpen(false);
              onInvite();
            }}
          >
            <span className="popover__item-icon">
              <LinkIcon size={16} />
            </span>
            {t('session.invite')}
          </button>

          <div className="popover__section">
            <p className="popover__title">{t('session.cap')}</p>
            <div className="stepper">
              <button
                type="button"
                className="stepper__btn"
                onClick={() => stepMax(-1)}
                disabled={!inCall || (parsedMax ?? sessionMax) === 0}
                aria-label={t("prefs.max.decrease")}
              >
                −
              </button>
              <span className="stepper__value" aria-live="polite">
                {(parsedMax ?? sessionMax) === 0
                  ? t('prefs.max.unlimited')
                  : t('prefs.max.short', { n: parsedMax ?? sessionMax })}
              </span>
              <button
                type="button"
                className="stepper__btn"
                onClick={() => stepMax(1)}
                disabled={!inCall || (parsedMax ?? sessionMax) === 99}
                aria-label={t("prefs.max.increase")}
              >
                +
              </button>
              <button
                type="button"
                className="btn btn--ghost"
                disabled={!inCall || !capDirty}
                onClick={() => {
                  if (parsedMax !== null) {
                    onUpdateCap(parsedMax);
                    setOpen(false);
                  }
                }}
              >
                {t('session.cap.apply')}
              </button>
            </div>
            <p className="popover__hint">{t('session.cap.hint')}</p>
          </div>

          <button
            type="button"
            role="menuitem"
            className="popover__item"
            disabled={!inCall}
            onClick={() => {
              setOpen(false);
              onReport();
            }}
          >
            <span className="popover__item-icon">
              <FlagIcon size={16} />
            </span>
            Signaler l’interlocuteur
          </button>
          <button
            type="button"
            role="menuitem"
            className={`popover__item popover__item--danger ${confirmingBlock ? 'is-selected' : ''}`}
            disabled={!inCall}
            onClick={() => {
              if (!confirmingBlock) {
                setConfirmingBlock(true);
                return;
              }
              setOpen(false);
              onBlock();
            }}
          >
            <span className="popover__item-icon">
              <BlockIcon size={16} />
            </span>
            {confirmingBlock ? t('session.blockConfirm') : t('session.block')}
          </button>
          <p className="popover__hint">
            Le blocage est définitif : vous ne serez plus jamais mis en relation avec cette
            personne.
          </p>
        </div>
      ) : null}
    </div>
  );
}

/** Empty means 0 (no limit), otherwise an integer in 2–99; null is invalid. */
function parseCap(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === '') return 0;
  if (!/^\d{1,3}$/.test(trimmed)) return null;
  const value = Number.parseInt(trimmed, 10);
  if (!Number.isInteger(value) || value < 2 || value > 99) return null;
  return value;
}

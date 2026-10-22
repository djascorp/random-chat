import { useEffect, useRef, useState } from 'react';
import { BlockIcon, DotsIcon, FlagIcon } from './Icons';

interface SessionMenuProps {
  inCall: boolean;
  onReport: () => void;
  onBlock: () => void;
}

/**
 * Secondary session actions. Reporting and blocking leave the main bar so the
 * destructive "leave" action no longer sits next to them, and blocking asks
 * for a second tap because it can never be undone.
 */
export function SessionMenu({ inCall, onReport, onBlock }: SessionMenuProps) {
  const [open, setOpen] = useState(false);
  const [confirmingBlock, setConfirmingBlock] = useState(false);
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
    if (!open) setConfirmingBlock(false);
  }, [open ]);

  return (
    <div className="device-menu" ref={containerRef}>
      <button
        type="button"
        className={`ctl ${open ? 'ctl--active' : ''}`}
        onClick={() => setOpen((value) => !value)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Plus d’actions de session"
        title="Signaler, bloquer…"
      >
        <DotsIcon size={20} />
        <span className="ctl__label">Plus</span>
      </button>

      {open ? (
        <div className="popover" role="menu" aria-label="Actions de session">
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
            {confirmingBlock ? 'Confirmer le blocage définitif' : 'Bloquer l’interlocuteur'}
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

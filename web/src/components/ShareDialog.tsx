import { useT } from '../i18n';
import { useEffect, useRef, useState } from 'react';
import type { InviteCreated } from '../types';
import { CloseIcon, LinkIcon } from './Icons';

interface ShareDialogProps {
  open: boolean;
  invite: InviteCreated | null;
  revoked: boolean;
  sessionMax: number;
  onRequest: (max?: number, rotate?: boolean) => void;
  onRevoke: () => void;
  onClose: () => void;
}

/**
 * The single invitation link of the current session. The dialog asks for the
 * link on open; copy (with a clipboard fallback), mobile share, revoke and
 * regenerate act on it. An optional cap is applied through (re)generation,
 * because the ceiling lives on the session, not on the link.
 */
export function ShareDialog({ open, invite, revoked, sessionMax, onRequest, onRevoke, onClose }: ShareDialogProps) {
  const t = useT();
  const [pendingMax, setPendingMax] = useState('');
  const [copied, setCopied] = useState(false);
  const [working, setWorking] = useState(false);
  const requestedRef = useRef(false);
  const copyTimerRef = useRef<number | null>(null);

  // One request per opening; a fresh token arriving ends the working state.
  useEffect(() => {
    if (!open) {
      requestedRef.current = false;
      setCopied(false);
      setWorking(false);
      return;
    }
    setPendingMax(sessionMax === 0 ? '' : String(sessionMax));
    if (!requestedRef.current && !invite && !revoked) {
      requestedRef.current = true;
      onRequest();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open ]);

  useEffect(() => {
    if (invite) setWorking(false);
  }, [invite]);

  // A cap change (ours or another member's) refreshes the stepper baseline so
  // the Apply button only shows while the draft differs from the live limit.
  useEffect(() => {
    if (open) setPendingMax(sessionMax === 0 ? '' : String(sessionMax));
  }, [open, sessionMax]);

  useEffect(() => {
    return () => {
      if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    };
  }, []);

  if (!open) return null;

  const url = invite ? `${window.location.origin}/j/${invite.token}` : '';
  const parsedMax = parseCap(pendingMax);
  const capDirty = parsedMax !== null && parsedMax !== sessionMax;
  const canShare = typeof navigator !== 'undefined' && 'share' in navigator;

  const copy = async () => {
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
    } catch {
      // Clipboard API unavailable (permissions, old browser): select & copy.
      const area = document.createElement('textarea');
      area.value = url;
      area.setAttribute('readonly', '');
      area.style.position = 'fixed';
      area.style.opacity = '0';
      document.body.appendChild(area);
      area.select();
      try {
        document.execCommand('copy');
      } catch {
        /* the user can still select the field manually */
      }
      document.body.removeChild(area);
    }
    setCopied(true);
    if (copyTimerRef.current !== null) window.clearTimeout(copyTimerRef.current);
    copyTimerRef.current = window.setTimeout(() => setCopied(false), 2000);
  };

  const share = async () => {
    if (!url) return;
    try {
      await navigator.share({ title: 'RandomChat', url });
    } catch {
      // Dismissed or unsupported data: staying on copy is the fallback.
    }
  };

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
    <div className="overlay" role="dialog" aria-modal="true" aria-labelledby="share-title">
      <div className="modal">
        <div className="modal__head">
          <div className="modal__icon">
            <LinkIcon size={22} />
          </div>
          <div>
            <h2 id="share-title" className="modal__title">
              {t('share.title')}
            </h2>
            <p className="modal__lead">{t('share.lead')}</p>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label={t("report.close")}>
            <CloseIcon size={18} />
          </button>
        </div>

        {revoked || (!invite && requestedRef.current) ? (
          <p className="modal__lead" aria-live="polite">
            {revoked ? t('share.dead') : t('share.generating')}
          </p>
        ) : invite ? (
          <div className="field">
            <span className="field__label">{t('share.title')}</span>
            <div className="share-row">
              <input
                className="field__input share-row__input"
                readOnly
                value={url}
                onFocus={(event) => event.currentTarget.select()}
                aria-label={t("share.title")}
              />
              <button type="button" className="btn btn--primary" onClick={() => void copy()}>
                {copied ? t('share.copied') : t('share.copy')}
              </button>
              {canShare ? (
                <button type="button" className="btn btn--ghost" onClick={() => void share()}>
                  {t('share.share')}
                </button>
              ) : null}
            </div>
          </div>
        ) : null}

        <div className="field">
          <span className="field__label">{t('share.capLabel')}</span>
          <div className="stepper">
            <button
              type="button"
              className="stepper__btn"
              onClick={() => stepMax(-1)}
              disabled={!invite || (parsedMax ?? sessionMax) === 0}
              aria-label={t("prefs.max.decrease")}
            >
              −
            </button>
            <span className="stepper__value" aria-live="polite">
              {(parsedMax ?? sessionMax) === 0 ? t('prefs.max.unlimited') : t('prefs.max.short', { n: parsedMax ?? sessionMax })}
            </span>
            <button
              type="button"
              className="stepper__btn"
              onClick={() => stepMax(1)}
              disabled={!invite || (parsedMax ?? sessionMax) === 99}
              aria-label={t("prefs.max.increase")}
            >
              +
            </button>
          </div>
          <p className="popover__hint">{t('share.capHint')}</p>
        </div>

        <div className="modal__actions">
          <button
            type="button"
            className="btn btn--ghost"
            disabled={!invite || revoked}
            onClick={onRevoke}
          >
            {t('share.revoke')}
          </button>
          <button
            type="button"
            className="btn btn--ghost"
            disabled={!capDirty}
            onClick={() => {
              if (parsedMax !== null) onRequest(parsedMax, false);
            }}
          >
            {t('session.cap.apply')}
          </button>
          <button
            type="button"
            className="btn btn--primary"
            disabled={working && !invite}
            onClick={() => {
              setWorking(true);
              onRequest(parsedMax ?? undefined, true);
            }}
          >
            {t('share.regenerate')}
          </button>
        </div>

        <p className="popover__hint">{t('share.hint')}</p>
      </div>
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

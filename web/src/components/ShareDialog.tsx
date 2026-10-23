import { useT } from '../i18n';
import { useEffect, useRef, useState } from 'react';
import type { InviteCreated } from '../types';
import { AlertIcon, CheckIcon, CloseIcon, LinkIcon, RefreshIcon } from './Icons';

interface ShareDialogProps {
  open: boolean;
  invite: InviteCreated | null;
  revoked: boolean;
  sessionMax: number;
  onRequest: (max?: number, rotate?: boolean) => void;
  onRevoke: () => void;
  onClose: () => void;
}

/** One in-flight server round trip; every mutating action disables the rest. */
type Pending = 'create' | 'rotate' | 'apply' | 'revoke' | null;
/** Transient confirmation shown after an action completes (or times out). */
type Notice = 'rotated' | 'applied' | 'timeout' | null;

const REQUEST_TIMEOUT_MS = 12_000;
const NOTICE_TTL_MS = 6_000;

/**
 * The single invitation link of the current session. The dialog asks for the
 * link on open; copy (with a clipboard fallback), mobile share, revoke and
 * regenerate act on it. An optional cap is applied through (re)generation,
 * because the ceiling lives on the session, not on the link.
 *
 * Every action reports its own state: the pending one shows a spinner and
 * disables the others, a new link flashes the field with a confirmation, and
 * a silent server is reported instead of leaving the dialog stuck.
 */
export function ShareDialog({ open, invite, revoked, sessionMax, onRequest, onRevoke, onClose }: ShareDialogProps) {
  const t = useT();
  const [pendingMax, setPendingMax] = useState('');
  const [copied, setCopied] = useState(false);
  const [pending, setPending] = useState<Pending>(null);
  const [notice, setNotice] = useState<Notice>(null);
  const requestedRef = useRef(false);
  const seenInviteRef = useRef<InviteCreated | null>(null);
  const appliedMaxRef = useRef<number | null>(null);
  const copyTimerRef = useRef<number | null>(null);

  // One request per opening; it resolves when the link arrives.
  useEffect(() => {
    if (!open) {
      requestedRef.current = false;
      seenInviteRef.current = null;
      appliedMaxRef.current = null;
      setCopied(false);
      setPending(null);
      setNotice(null);
      return;
    }
    setPendingMax(sessionMax === 0 ? '' : String(sessionMax));
    if (!requestedRef.current && !invite && !revoked) {
      requestedRef.current = true;
      setPending('create');
      onRequest();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Link arrival: the first one resolves the initial request, a different
  // token resolves a regeneration. Same-token answers (cap changes) are
  // handled by the sessionMax effect below.
  useEffect(() => {
    if (!open) return;
    if (!invite) {
      seenInviteRef.current = null;
      return;
    }
    const previous = seenInviteRef.current;
    seenInviteRef.current = invite;
    if (previous === null) {
      // No link was visible (first open, or after a revoke): either request
      // or regeneration is satisfied by this first token.
      if (pending === 'create') setPending(null);
      if (pending === 'rotate') {
        setCopied(false);
        setPending(null);
        setNotice('rotated');
      }
      return;
    }
    if (previous.token !== invite.token && pending === 'rotate') {
      setCopied(false);
      setPending(null);
      setNotice('rotated');
    }
  }, [open, invite, pending]);

  // The server echoes the new ceiling through session.updated.
  useEffect(() => {
    if (pending !== 'apply') return;
    if (sessionMax === appliedMaxRef.current) {
      setPending(null);
      setNotice('applied');
    }
  }, [pending, sessionMax]);

  // Revocation is confirmed by the server event that flips `revoked`.
  useEffect(() => {
    if (revoked && pending === 'revoke') {
      setPending(null);
      setNotice(null);
    }
  }, [revoked, pending]);

  // A silent server must not leave a button spinning forever.
  useEffect(() => {
    if (!open || pending === null || pending === 'create') return;
    const timer = window.setTimeout(() => {
      setPending(null);
      setNotice('timeout');
    }, REQUEST_TIMEOUT_MS);
    return () => window.clearTimeout(timer);
  }, [open, pending]);

  // Confirmations are transient; they fade instead of stacking on the cookie.
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), NOTICE_TTL_MS);
    return () => window.clearTimeout(timer);
  }, [notice]);

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

  const regenerate = () => {
    if (pending !== null || (!invite && !revoked)) return;
    setPending('rotate');
    setNotice(null);
    onRequest(parsedMax ?? undefined, true);
  };

  const applyCap = () => {
    if (pending !== null || parsedMax === null) return;
    appliedMaxRef.current = parsedMax;
    setPending('apply');
    setNotice(null);
    onRequest(parsedMax, false);
  };

  const revoke = () => {
    if (pending !== null || !invite) return;
    setPending('revoke');
    setNotice(null);
    onRevoke();
  };

  // One status channel for the whole dialog: what is running, what just
  // happened, or why nothing happened.
  const status =
    pending === 'create'
      ? { tone: 'pending' as const, text: t('share.generating'), hint: t('share.generatingHint') }
      : pending === 'rotate'
        ? { tone: 'pending' as const, text: t('share.regenerating') }
        : pending === 'apply'
          ? { tone: 'pending' as const, text: t('share.applying') }
          : pending === 'revoke'
            ? { tone: 'pending' as const, text: t('share.revoking') }
            : notice === 'rotated'
              ? { tone: 'success' as const, text: t('share.rotated') }
              : notice === 'applied'
                ? { tone: 'success' as const, text: t('share.capUpdated') }
                : notice === 'timeout'
                  ? { tone: 'error' as const, text: t('share.timeout') }
                  : revoked
                    ? { tone: 'error' as const, text: t('share.dead') }
                    : null;

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

        {status ? (
          <div
            className={`share-status share-status--${status.tone}`}
            role="status"
            aria-live="polite"
          >
            {status.tone === 'pending' ? (
              <span className="spinner" aria-hidden="true" />
            ) : status.tone === 'success' ? (
              <CheckIcon size={16} />
            ) : (
              <AlertIcon size={16} />
            )}
            <div className="share-status__body">
              <span>{status.text}</span>
              {'hint' in status && status.hint ? (
                <span className="share-status__hint">{status.hint}</span>
              ) : null}
            </div>
          </div>
        ) : null}

        {invite ? (
          <div className={`field share-link${notice === 'rotated' ? ' is-flash' : ''}`}>
            <span className="field__label">{t('share.linkLabel')}</span>
            <div className={`share-row${pending === 'rotate' ? ' is-stale' : ''}`} aria-busy={pending === 'rotate'}>
              <input
                className="field__input share-row__input"
                readOnly
                value={url}
                onFocus={(event) => event.currentTarget.select()}
                aria-label={t('share.linkLabel')}
              />
              <button
                type="button"
                className="btn btn--primary"
                disabled={pending === 'rotate'}
                onClick={() => void copy()}
              >
                {copied ? t('share.copied') : t('share.copy')}
              </button>
              {canShare ? (
                <button
                  type="button"
                  className="btn btn--ghost"
                  disabled={pending === 'rotate'}
                  onClick={() => void share()}
                >
                  {t('share.share')}
                </button>
              ) : null}
            </div>
            <div className="share-link__foot">
              <button
                type="button"
                className="share-regenerate"
                disabled={pending !== null || revoked}
                onClick={regenerate}
              >
                <RefreshIcon
                  size={14}
                  className={pending === 'rotate' ? 'is-spinning' : undefined}
                />
                {pending === 'rotate' ? t('share.regenerating') : t('share.regenerate')}
              </button>
              <span className="share-note">{t('share.regenerateHint')}</span>
            </div>
          </div>
        ) : null}

        <div className="field">
          <span className="field__label">{t('session.cap')}</span>
          <div className="share-cap">
            <div className="stepper">
              <button
                type="button"
                className="stepper__btn"
                onClick={() => stepMax(-1)}
                disabled={!invite || pending !== null || (parsedMax ?? sessionMax) === 0}
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
                disabled={!invite || pending !== null || (parsedMax ?? sessionMax) === 99}
                aria-label={t("prefs.max.increase")}
              >
                +
              </button>
            </div>
            <button
              type="button"
              className="btn btn--ghost share-cap__apply"
              disabled={!invite || !capDirty || pending !== null}
              onClick={applyCap}
            >
              {t('session.cap.apply')}
            </button>
          </div>
          <p className="share-note">{t('session.cap.hint')}</p>
        </div>

        <div className="modal__actions share-actions">
          {revoked ? (
            <button
              type="button"
              className="btn btn--primary"
              disabled={pending !== null}
              onClick={regenerate}
            >
              <RefreshIcon size={16} />
              {t('share.newLink')}
            </button>
          ) : (
            <button
              type="button"
              className="btn btn--ghost share-revoke"
              disabled={!invite || pending !== null}
              onClick={revoke}
            >
              {t('share.revoke')}
            </button>
          )}
          <button type="button" className="btn btn--ghost" onClick={onClose}>
            {t('share.close')}
          </button>
        </div>

        <p className="share-note share-note--foot">{t('share.hint')}</p>
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

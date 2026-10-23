import { useT } from '../i18n';
import { useEffect, useState } from 'react';
import type { CallStatus, InvitePreview } from '../types';

interface JoinScreenProps {
  token: string;
  status: CallStatus;
  joinError: string | null;
  onJoin: (token: string) => void;
  onBack: () => void;
}

type PreviewState = { kind: 'loading' } | { kind: 'ready'; preview: InvitePreview } | { kind: 'dead' };

/**
 * The invitation landing: what the link opens. A public preview (type,
 * headcount, limit — never identifiers) first, then consent and camera
 * through the shared join path. A dead link explains itself with a way home.
 */
export function JoinScreen({ token, status, joinError, onJoin, onBack }: JoinScreenProps) {
  const t = useT();
  const [state, setState] = useState<PreviewState>({ kind: 'loading' });

  useEffect(() => {
    let cancelled = false;
    setState({ kind: 'loading' });
    const load = async () => {
      try {
        const response = await fetch(`/api/invites/${encodeURIComponent(token)}`, {
          headers: { Accept: 'application/json' },
        });
        if (cancelled) return;
        if (!response.ok) {
          setState({ kind: 'dead' });
          return;
        }
        const preview = (await response.json()) as InvitePreview;
        if (cancelled) return;
        setState({ kind: 'ready', preview });
      } catch {
        if (!cancelled) setState({ kind: 'dead' });
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [token]);

  const busy = status === 'requesting-media' || status === 'waiting';

  return (
    <div className="landing">
      <section className="hero">
        <span className="hero__badge">
          <span className="dot dot--live" />
          {t('join.title')}
        </span>

        {state.kind === 'loading' ? (
          <p className="hero__lead" aria-live="polite">
            {t('join.loading')}
          </p>
        ) : state.kind === 'dead' ? (
          <>
            <h1 className="hero__title">{t('join.dead.title')}</h1>
            <p className="hero__lead">{t('join.dead.body')}</p>
            <div className="hero__actions">
              <button type="button" className="btn btn--primary btn--lg" onClick={onBack}>
                {t('join.back')}
              </button>
            </div>
          </>
        ) : (
          <>
            <h1 className="hero__title">
              {state.preview.mode === 'group' ? t('join.group') : t('join.solo')}
            </h1>
            <p className="hero__lead">
              {t('join.members', { n: state.preview.members })} ·{' '}
              {state.preview.max_participants === 0
                ? t('join.maxUnlimited')
                : t('join.max', { n: state.preview.max_participants })}
              {state.preview.members <= 1
                ? ` · ${t('join.hostWaiting')}`
                : state.preview.mode === 'solo'
                  ? ` · ${t('join.solo.hint')}`
                  : ''}
            </p>

            {joinError ? (
              <p className="hero__lead" role="alert">
                {joinError}
              </p>
            ) : null}

            <div className="hero__actions">
              <button
                type="button"
                className="btn btn--primary btn--lg"
                disabled={busy}
                onClick={() => onJoin(token)}
              >
                <span className="btn__pulse" aria-hidden="true" />
                {busy ? t('invite.joining') : t('join.cta')}
              </button>
              <span className="hero__hint">{t('stage.cameraPromptHint')}</span>
            </div>

            <button type="button" className="btn btn--ghost" onClick={onBack}>
              {t('join.back')}
            </button>
          </>
        )}
      </section>
    </div>
  );
}

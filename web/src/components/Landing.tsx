import { useState } from 'react';
import type { SessionMode } from '../types';
import { useT } from '../i18n';
import { BoltIcon, ChatIcon, GlobeIcon, ShieldIcon, SkipIcon, UsersIcon } from './Icons';
import { SearchPrefs, parseMaxParticipants } from './SearchPrefs';

interface LandingProps {
  online: number | null;
  activeSessions: number | null;
  matchesTotal: number | null;
  turnConfigured: boolean;
  initialMode: SessionMode;
  initialMax: number;
  preset: boolean;
  onStart: (mode: SessionMode, max: number) => void;
  onInvite: () => void;
  busy: boolean;
}

export function Landing({
  online,
  activeSessions,
  matchesTotal,
  turnConfigured,
  initialMode,
  initialMax,
  preset,
  onStart,
  onInvite,
  busy,
}: LandingProps) {
  const t = useT();
  const [mode, setMode] = useState<SessionMode>(initialMode);
  const [max, setMax] = useState(initialMax === 0 ? '' : String(initialMax));

  const steps = [
    { icon: <BoltIcon size={22} />, title: t('steps.1.title'), text: t('steps.1.text') },
    { icon: <UsersIcon size={22} />, title: t('steps.2.title'), text: t('steps.2.text') },
    { icon: <SkipIcon size={22} />, title: t('steps.3.title'), text: t('steps.3.text') },
  ];

  const parsed = parseMaxParticipants(max);
  const maxValid = parsed !== null;
  const maxValue = parsed ?? 0;

  return (
    <div className="landing">
      <section className="hero">
        <span className="hero__badge">
          <span className="dot dot--live" />
          {online === null ? t('landing.badge.connecting') : t('topbar.online', { n: online })}
        </span>

        <h1 className="hero__title">
          {t('landing.title')}
          <span className="hero__title-soft">{t('landing.title.soft')}</span>
        </h1>

        <p className="hero__lead">{t('landing.lead')}</p>

        <SearchPrefs mode={mode} maxText={max} onModeChange={setMode} onMaxChange={setMax} />

        <div className="hero__actions">
          <button
            type="button"
            className="btn btn--primary btn--lg"
            onClick={() => onStart(mode, maxValue)}
            disabled={busy || !maxValid}
          >
            <span className="btn__pulse" aria-hidden="true" />
            {busy
              ? t('landing.cta.preparing')
              : mode === 'group'
                ? maxValue > 0
                  ? t('landing.cta.groupMax', { n: maxValue })
                  : t('landing.cta.group')
                : t('landing.cta.solo')}
          </button>
          <button type="button" className="btn btn--ghost btn--lg" onClick={onInvite} disabled={busy}>
            {t('landing.invite')}
          </button>
          <span className="hero__hint">
            {preset ? `${t('landing.preset')} ` : null}
            {turnConfigured ? t('landing.hint.turn') : t('landing.hint.p2p')}
          </span>
        </div>

        <dl className="metrics">
          <div className="metric">
            <dt>{t('metric.online')}</dt>
            <dd>{online ?? '—'}</dd>
          </div>
          <div className="metric">
            <dt>{t('metric.active')}</dt>
            <dd>{activeSessions ?? '—'}</dd>
          </div>
          <div className="metric">
            <dt>{t('metric.matches')}</dt>
            <dd>{matchesTotal ?? '—'}</dd>
          </div>
        </dl>
      </section>

      <section className="steps" aria-label={t('steps.title')}>
        {steps.map((step, index) => (
          <article className="step" key={step.title}>
            <span className="step__index">{String(index + 1).padStart(2, '0')}</span>
            <span className="step__icon">{step.icon}</span>
            <h2 className="step__title">{step.title}</h2>
            <p className="step__text">{step.text}</p>
          </article>
        ))}
      </section>

      <section className="safety" aria-label={t('safety.title')}>
        <div className="safety__card">
          <ShieldIcon size={22} />
          <div>
            <h2>{t('safety.report.title')}</h2>
            <p>{t('safety.report.text')}</p>
          </div>
        </div>
        <div className="safety__card">
          <ChatIcon size={22} />
          <div>
            <h2>{t('safety.chat.title')}</h2>
            <p>{t('safety.chat.text')}</p>
          </div>
        </div>
        <div className="safety__card">
          <GlobeIcon size={22} />
          <div>
            <h2>{t('safety.p2p.title')}</h2>
            <p>{t('safety.p2p.text')}</p>
          </div>
        </div>
      </section>
    </div>
  );
}

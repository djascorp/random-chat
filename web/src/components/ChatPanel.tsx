import { getT, useT } from '../i18n';
import { useEffect, useRef, useState } from 'react';
import type { CallStatus, ChatMessage } from '../types';
import { CloseIcon, SendIcon } from './Icons';
import { formatClock } from '../lib/format';

interface ChatPanelProps {
  open: boolean;
  messages: ChatMessage[];
  connected: boolean;
  status: CallStatus;
  maxRunes: number;
  onClose: () => void;
  onSend: (text: string) => void;
}

function sessionBanner(status: CallStatus): string | null {
  const t = getT();
  switch (status) {
    case 'requesting-media':
      return t('stage.cameraPrompt');
    case 'waiting':
    case 'ended':
      return t('stage.searching');
    case 'connecting':
    case 'reconnecting':
      return t('stage.connectingPeer');
    default:
      return null;
  }
}

export function ChatPanel({ open, messages, connected, status, maxRunes, onClose, onSend }: ChatPanelProps) {
  const t = useT();
  const [draft, setDraft] = useState('');
  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [messages, open]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  const submit = () => {
    if (!draft.trim()) return;
    onSend(draft);
    setDraft('');
  };

  const remaining = maxRunes - draft.length;
  const banner = connected ? null : sessionBanner(status);

  return (
    <aside className={`chat ${open ? 'is-open' : ''}`} aria-hidden={!open} aria-label={t("chat.panel")}>
      <header className="chat__header">
        <div>
          <h2 className="chat__title">{t("chat.title")}</h2>
          <p className="chat__subtitle">
            {connected ? t('chat.connected') : t('chat.standby')}
          </p>
        </div>
        <button type="button" className="icon-btn" onClick={onClose} aria-label={t("chat.close")}>
          <CloseIcon size={18} />
        </button>
      </header>

      <div className="chat__list" ref={listRef} aria-live="polite" aria-label={t("chat.messages")}>
        {banner ? (
          <p className="chat__banner" role="status">
            {banner}
          </p>
        ) : null}
        {messages.length === 0 && !banner ? (
          <p className="chat__empty">
            {t('chat.empty')}
          </p>
        ) : (
          messages.map((message) =>
            message.from === 'system' ? (
              <p className="chat__system" key={message.id}>
                {message.text}
              </p>
            ) : (
              <div
                className={`bubble ${message.from === 'me' ? 'bubble--me' : 'bubble--peer'} ${
                  message.pending ? 'is-pending' : ''
                }`}
                key={message.id}
              >
                <p className="bubble__text">{message.text}</p>
                <span className="bubble__meta">
                  {formatClock(message.at)}
                  {message.from === 'me' ? (message.pending ? t('chat.pending') : t('chat.sent')) : ''}
                </span>
              </div>
            ),
          )
        )}
      </div>

      <form
        className="chat__composer"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <textarea
          ref={inputRef}
          className="chat__input"
          placeholder={
            connected ? t('chat.placeholder.live') : t('chat.placeholder.standby')
          }
          value={draft}
          rows={1}
          maxLength={maxRunes}
          disabled={!connected}
          aria-label={t("chat.input")}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault();
              submit();
            }
          }}
        />
        <button
          type="submit"
          className="icon-btn icon-btn--primary"
          disabled={!draft.trim() || !connected}
          aria-label="Envoyer"
        >
          <SendIcon size={18} />
        </button>
        {remaining < 100 ? <span className="chat__counter">{remaining}</span> : null}
      </form>
    </aside>
  );
}

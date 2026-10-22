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
  switch (status) {
    case 'requesting-media':
      return 'Activation de la caméra…';
    case 'waiting':
    case 'ended':
      return 'Recherche d’un interlocuteur…';
    case 'connecting':
    case 'reconnecting':
      return 'Connexion à l’interlocuteur…';
    default:
      return null;
  }
}

export function ChatPanel({ open, messages, connected, status, maxRunes, onClose, onSend }: ChatPanelProps) {
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
    <aside className={`chat ${open ? 'is-open' : ''}`} aria-hidden={!open} aria-label="Chat texte">
      <header className="chat__header">
        <div>
          <h2 className="chat__title">Chat</h2>
          <p className="chat__subtitle">
            {connected ? 'Connecté à l’interlocuteur' : 'Disponible pendant la conversation'}
          </p>
        </div>
        <button type="button" className="icon-btn" onClick={onClose} aria-label="Fermer le chat">
          <CloseIcon size={18} />
        </button>
      </header>

      <div className="chat__list" ref={listRef} aria-live="polite" aria-label="Messages">
        {banner ? (
          <p className="chat__banner" role="status">
            {banner}
          </p>
        ) : null}
        {messages.length === 0 && !banner ? (
          <p className="chat__empty">
            Aucun message. Le chat est utile si le son ou l’image ne passent pas correctement.
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
                  {message.from === 'me' ? (message.pending ? ' · envoi…' : ' · envoyé') : ''}
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
            connected ? 'Écrivez un message…' : 'Le chat sera disponible pendant la conversation'
          }
          value={draft}
          rows={1}
          maxLength={maxRunes}
          disabled={!connected}
          aria-label="Votre message"
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

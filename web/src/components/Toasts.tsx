import type { Toast } from '../types';
import { AlertIcon, CloseIcon } from './Icons';

interface ToastsProps {
  toasts: Toast[];
  onDismiss: (id: string) => void;
}

export function Toasts({ toasts, onDismiss }: ToastsProps) {
  if (toasts.length === 0) return null;
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((toast) => (
        <div key={toast.id} className={`toast toast--${toast.tone}`}>
          <span className="toast__icon">
            <AlertIcon size={18} />
          </span>
          <div className="toast__body">
            <p className="toast__title">{toast.title}</p>
            {toast.description ? <p className="toast__text">{toast.description}</p> : null}
          </div>
          <button
            type="button"
            className="icon-btn icon-btn--ghost"
            onClick={() => onDismiss(toast.id)}
            aria-label="Fermer la notification"
          >
            <CloseIcon size={16} />
          </button>
        </div>
      ))}
    </div>
  );
}

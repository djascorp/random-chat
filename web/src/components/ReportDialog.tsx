import { useState } from 'react';
import { REPORT_CATEGORIES } from '../types';
import { CloseIcon, ShieldIcon } from './Icons';

interface ReportDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (category: string, reason: string, block: boolean) => void;
}

export function ReportDialog({ open, onClose, onSubmit }: ReportDialogProps) {
  const [category, setCategory] = useState<string>('harassment');
  const [reason, setReason] = useState('');
  const [block, setBlock] = useState(true);

  if (!open) return null;

  return (
    <div className="overlay" role="dialog" aria-modal="true" aria-labelledby="report-title">
      <div className="modal">
        <div className="modal__head">
          <div className="modal__icon">
            <ShieldIcon size={22} />
          </div>
          <div>
            <h2 id="report-title" className="modal__title">
              Signaler cet interlocuteur
            </h2>
            <p className="modal__lead">
              La session sera fermée et le signalement transmis à la modération.
            </p>
          </div>
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Fermer">
            <CloseIcon size={18} />
          </button>
        </div>

        <div className="field">
          <span className="field__label">Motif principal</span>
          <div className="chips">
            {REPORT_CATEGORIES.map((option) => (
              <button
                key={option.value}
                type="button"
                className={`chip ${category === option.value ? 'is-selected' : ''}`}
                onClick={() => setCategory(option.value)}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>

        <label className="field">
          <span className="field__label">Précisions (optionnel)</span>
          <textarea
            className="field__input"
            rows={3}
            maxLength={500}
            value={reason}
            placeholder="Décrivez brièvement ce qui s’est passé"
            onChange={(event) => setReason(event.target.value)}
          />
        </label>

        <label className="check">
          <input type="checkbox" checked={block} onChange={(event) => setBlock(event.target.checked)} />
          <span>Bloquer cette personne pour ne plus jamais lui être associé</span>
        </label>

        <div className="modal__actions">
          <button type="button" className="btn btn--ghost" onClick={onClose}>
            Annuler
          </button>
          <button
            type="button"
            className="btn btn--danger"
            onClick={() => {
              onSubmit(category, reason, block);
              onClose();
            }}
          >
            Envoyer le signalement
          </button>
        </div>
      </div>
    </div>
  );
}

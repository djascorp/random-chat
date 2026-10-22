import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './styles/global.css';
import './styles/components.css';

const container = document.getElementById('root');
if (!container) {
  throw new Error('élément racine #root introuvable');
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import App from './App.tsx';
import { installClientDiagnostics } from './clientDiagnostics';
import { ClientErrorBoundary } from './components/ClientErrorBoundary';
import { applySkin, getStoredSkin } from './themes';

// Skin owns both color and font; apply once before first paint.
applySkin(getStoredSkin());
installClientDiagnostics();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ClientErrorBoundary>
      <App />
    </ClientErrorBoundary>
  </StrictMode>,
);

import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App.jsx';
import { DialogProvider } from './components/ui/dialog.jsx';
import { ToastProvider } from './components/ui/toast.jsx';
import './styles/app.css';

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <DialogProvider>
      <ToastProvider>
        <App />
      </ToastProvider>
    </DialogProvider>
  </React.StrictMode>,
);

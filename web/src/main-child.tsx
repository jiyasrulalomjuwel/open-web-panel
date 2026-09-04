import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import AppChild from './AppChild';
import { ErrorBoundary } from './components/ErrorBoundary';
import { setTokenPrefix, setRefreshPath } from './lib/api';
import './index.css';

setTokenPrefix('owp_child_');
setRefreshPath('/child/auth/refresh');

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <AppChild />
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>
);

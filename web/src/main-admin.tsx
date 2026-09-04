import React from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import AppAdmin from './AppAdmin';
import { ErrorBoundary } from './components/ErrorBoundary';
import { setTokenPrefix, setRefreshPath } from './lib/api';
import './index.css';

setTokenPrefix('owp_admin_');
setRefreshPath('/auth/refresh');

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <AppAdmin />
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>
);

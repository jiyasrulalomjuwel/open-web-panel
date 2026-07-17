import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'framer-motion';
import { Login } from './pages/Login';
import { FileManager } from './pages/FileManager';
import { Databases } from './pages/Databases';
import { Domains } from './pages/Domains';
import { Tickets } from './pages/Tickets';
import { ChildBandwidth } from './pages/Bandwidth';
import { CMSInstaller } from './pages/CMSInstaller';
import { SSLCertificates } from './pages/SSLCertificates';
import { Emails } from './pages/Emails';
import { Webmail } from './pages/Webmail';
import { FTPManager } from './pages/FTPManager';
import { ChildDashboard } from './pages/ChildDashboard';
import { ChildNotifications } from './pages/ChildNotifications';
import { ChildLayout } from './components/ChildLayout';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ToastProvider } from './components/ToastProvider';
import { Redirects } from './pages/Redirects';
import { HotlinkProtection } from './pages/HotlinkProtection';
import { Stats } from './pages/Stats';
import { ErrorManager } from './pages/ErrorManager';
import { ChildPhpVersion } from './pages/ChildPhpVersion';
import { getAccessToken } from './lib/api';

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const token = getAccessToken();
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

function PageTransition({ children }: { children: React.ReactNode }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, y: -8 }}
      transition={{ duration: 0.2 }}
    >
      {children}
    </motion.div>
  );
}

export default function AppChild() {
  const location = useLocation();
  return (
    <ErrorBoundary>
      <ToastProvider>
        <AnimatePresence mode="wait">
          <Routes location={location} key={location.pathname}>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<ProtectedRoute><ChildLayout /></ProtectedRoute>}>
              <Route index element={<Navigate to="/child/dashboard" replace />} />
              <Route path="dashboard" element={<Navigate to="/child/dashboard" replace />} />
            </Route>
            <Route path="/child" element={<ProtectedRoute><ChildLayout /></ProtectedRoute>}>
              <Route index element={<Navigate to="/child/dashboard" replace />} />
              <Route path="dashboard" element={<PageTransition><ChildDashboard /></PageTransition>} />
              <Route path="files" element={<PageTransition><FileManager /></PageTransition>} />
              <Route path="databases" element={<PageTransition><Databases /></PageTransition>} />
              <Route path="domains" element={<PageTransition><Domains /></PageTransition>} />
              <Route path="ftp" element={<PageTransition><FTPManager /></PageTransition>} />
              <Route path="tickets" element={<PageTransition><Tickets /></PageTransition>} />
              <Route path="bandwidth" element={<PageTransition><ChildBandwidth /></PageTransition>} />
              <Route path="cms" element={<PageTransition><CMSInstaller /></PageTransition>} />
              <Route path="ssl" element={<PageTransition><SSLCertificates /></PageTransition>} />
              <Route path="emails" element={<PageTransition><Emails /></PageTransition>} />
              <Route path="webmail" element={<PageTransition><Webmail /></PageTransition>} />
              <Route path="redirects" element={<PageTransition><Redirects /></PageTransition>} />
              <Route path="hotlink" element={<PageTransition><HotlinkProtection /></PageTransition>} />
              <Route path="stats" element={<PageTransition><Stats /></PageTransition>} />
              <Route path="errors" element={<PageTransition><ErrorManager /></PageTransition>} />
              <Route path="notifications" element={<PageTransition><ChildNotifications /></PageTransition>} />
              <Route path="php-version" element={<PageTransition><ChildPhpVersion /></PageTransition>} />
            </Route>
          </Routes>
        </AnimatePresence>
      </ToastProvider>
    </ErrorBoundary>
  );
}

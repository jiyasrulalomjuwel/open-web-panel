import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'framer-motion';
import { Login } from './pages/Login';
import { Dashboard } from './pages/Dashboard';
import { Accounts } from './pages/Accounts';
import { Packages } from './pages/Packages';
import { Settings } from './pages/Settings';
import { FileManager } from './pages/FileManager';
import { Databases } from './pages/Databases';
import { Domains } from './pages/Domains';
import { Tickets, TicketManagement } from './pages/Tickets';
import { Bandwidth, ChildBandwidth } from './pages/Bandwidth';
import { CMSInstaller } from './pages/CMSInstaller';
import { SSLCertificates } from './pages/SSLCertificates';
import { Submissions } from './pages/Submissions';
import { AdminNotifications } from './pages/AdminNotifications';
import { Emails } from './pages/Emails';
import { Webmail } from './pages/Webmail';
import { FTPManager } from './pages/FTPManager';
import { ChildDashboard } from './pages/ChildDashboard';
import { ChildNotifications } from './pages/ChildNotifications';
import { Layout } from './components/Layout';
import { ChildLayout } from './components/ChildLayout';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ToastProvider } from './components/ToastProvider';
import { Redirects } from './pages/Redirects';
import { HotlinkProtection } from './pages/HotlinkProtection';
import { Stats } from './pages/Stats';
import { ErrorManager } from './pages/ErrorManager';
import { Onboarding } from './pages/Onboarding';
import { IpManagement } from './pages/IpManagement';
import { AccessLog } from './pages/AccessLog';
import { PhpVersions } from './pages/PhpVersions';
import { ChildPhpVersion } from './pages/ChildPhpVersion';

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const token = localStorage.getItem('owp_access_token');
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

function usePortRedirect() {
  const port = window.location.port;
  const isAdminPort = port === '2086';
  const isChildPort = port === '2082';
  return { isAdminPort, isChildPort };
}

function AdminGuard({ children }: { children: React.ReactNode }) {
  const { isChildPort } = usePortRedirect();
  const loc = window.location.pathname;
  // On child port, redirect admin routes to child dashboard
  if (isChildPort && !loc.startsWith('/child')) {
    return <Navigate to="/child/dashboard" replace />;
  }
  return <>{children}</>;
}

function ChildGuard({ children }: { children: React.ReactNode }) {
  const { isAdminPort } = usePortRedirect();
  const loc = window.location.pathname;
  // On admin port, redirect child routes to admin dashboard
  if (isAdminPort && loc.startsWith('/child')) {
    return <Navigate to="/" replace />;
  }
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

export default function App() {
  const location = useLocation();
  return (
    <ErrorBoundary>
      <ToastProvider>
        <AnimatePresence mode="wait">
          <Routes location={location} key={location.pathname}>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<ProtectedRoute><AdminGuard><Layout /></AdminGuard></ProtectedRoute>}>
              <Route index element={<PageTransition><Dashboard /></PageTransition>} />
              <Route path="accounts" element={<PageTransition><Accounts /></PageTransition>} />
              <Route path="packages" element={<PageTransition><Packages /></PageTransition>} />
              <Route path="settings" element={<PageTransition><Settings /></PageTransition>} />
              <Route path="tickets" element={<PageTransition><TicketManagement /></PageTransition>} />
              <Route path="bandwidth" element={<PageTransition><Bandwidth /></PageTransition>} />
              <Route path="submissions" element={<PageTransition><Submissions /></PageTransition>} />
              <Route path="notifications" element={<PageTransition><AdminNotifications /></PageTransition>} />
              <Route path="ip-management" element={<PageTransition><IpManagement /></PageTransition>} />
              <Route path="access-log" element={<PageTransition><AccessLog /></PageTransition>} />
              <Route path="php-versions" element={<PageTransition><PhpVersions /></PageTransition>} />
            </Route>
            <Route path="/child" element={<ProtectedRoute><ChildGuard><ChildLayout /></ChildGuard></ProtectedRoute>}>
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
            <Route path="/onboarding" element={<ProtectedRoute><PageTransition><Onboarding /></PageTransition></ProtectedRoute>} />
          </Routes>
        </AnimatePresence>
      </ToastProvider>
    </ErrorBoundary>
  );
}

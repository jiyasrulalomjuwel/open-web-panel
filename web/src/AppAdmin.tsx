import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'framer-motion';
import { Login } from './pages/Login';
import { Dashboard } from './pages/Dashboard';
import { Accounts } from './pages/Accounts';
import { Packages } from './pages/Packages';
import { Settings } from './pages/Settings';
import { Tickets, TicketManagement } from './pages/Tickets';
import { Bandwidth } from './pages/Bandwidth';
import { Submissions } from './pages/Submissions';
import { AdminNotifications } from './pages/AdminNotifications';
import { Layout } from './components/Layout';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ToastProvider } from './components/ToastProvider';
import { IpManagement } from './pages/IpManagement';
import { AccessLog } from './pages/AccessLog';
import { PhpVersions } from './pages/PhpVersions';
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

export default function AppAdmin() {
  const location = useLocation();
  return (
    <ErrorBoundary>
      <ToastProvider>
        <AnimatePresence mode="wait">
          <Routes location={location} key={location.pathname}>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<ProtectedRoute><Layout /></ProtectedRoute>}>
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
          </Routes>
        </AnimatePresence>
      </ToastProvider>
    </ErrorBoundary>
  );
}

import { useState } from 'react';
import { Outlet, NavLink, useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { FolderOpen, Database, LayoutDashboard, Globe, LifeBuoy, BarChart3, Globe2, Shield, LogOut, ChevronLeft, Menu, Mail, Inbox, Sun, Moon, Server } from 'lucide-react';
import { clearTokens, getUser } from '../lib/api';

const nav = [
  { to: '/child/dashboard', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/child/files', icon: FolderOpen, label: 'File Manager' },
  { to: '/child/databases', icon: Database, label: 'Databases' },
  { to: '/child/domains', icon: Globe, label: 'Domains' },
  { to: '/child/ftp', icon: Server, label: 'FTP Manager' },
  { to: '/child/cms', icon: Globe2, label: 'CMS Installer' },
  { to: '/child/ssl', icon: Shield, label: 'SSL Certs' },
  { to: '/child/emails', icon: Mail, label: 'Emails' },
  { to: '/child/webmail', icon: Inbox, label: 'Webmail' },
  { to: '/child/bandwidth', icon: BarChart3, label: 'Bandwidth' },
  { to: '/child/tickets', icon: LifeBuoy, label: 'Support' },
];

export function ChildLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [darkMode, setDarkMode] = useState(document.documentElement.classList.contains('dark'));
  const navigate = useNavigate();
  const user = getUser();

  const toggleDark = () => {
    const next = !darkMode;
    setDarkMode(next);
    document.documentElement.classList.toggle('dark', next);
  };

  const handleLogout = () => {
    clearTokens();
    navigate('/login');
  };

  return (
    <div className="flex h-screen bg-gray-50 dark:bg-gray-950">
      {/* Mobile overlay */}
      <AnimatePresence>
        {mobileOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 z-40 bg-black/40 lg:hidden"
            onClick={() => setMobileOpen(false)}
          />
        )}
      </AnimatePresence>

      {/* Sidebar */}
      <motion.aside
        animate={{ width: collapsed ? 64 : 240 }}
        transition={{ duration: 0.2, ease: 'easeInOut' }}
        className={`fixed inset-y-0 left-0 z-50 flex flex-col bg-emerald-900 dark:bg-gray-900 text-gray-200 lg:static
          ${collapsed ? 'w-16' : 'w-60'}
          ${mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}`}
      >
        <div className="flex h-14 items-center justify-between px-3 border-b border-emerald-700 dark:border-gray-700 shrink-0">
          {!collapsed && (
            <motion.span
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="font-semibold text-sm tracking-wide"
            >
              OWP · Site Panel
            </motion.span>
          )}
          <div className="flex items-center gap-1">
            <button
              onClick={toggleDark}
              className="p-1.5 rounded-md hover:bg-emerald-700 dark:hover:bg-gray-700 text-emerald-300 dark:text-gray-400"
              title={darkMode ? 'Light mode' : 'Dark mode'}
            >
              {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
            <button
              onClick={() => { setCollapsed(!collapsed); setMobileOpen(false); }}
              className="p-1.5 rounded-md hover:bg-emerald-700 dark:hover:bg-gray-700 text-emerald-300 dark:text-gray-400"
            >
              <ChevronLeft className={`h-4 w-4 transition-transform ${collapsed ? 'rotate-180' : ''}`} />
            </button>
          </div>
        </div>

        <nav className="flex-1 py-3 space-y-1 px-2 overflow-hidden">
          {nav.map(({ to, icon: Icon, label }, i) => (
            <motion.div
              key={to}
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.05 }}
            >
              <NavLink
                key={to}
                to={to}
                onClick={() => setMobileOpen(false)}
                className={({ isActive }) =>
                  `flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors ${
                    isActive
                      ? 'bg-emerald-600 text-white'
                      : 'text-emerald-100 hover:bg-emerald-800 hover:text-white'
                  }`
                }
              >
                <Icon className="h-4 w-4 shrink-0" />
                {!collapsed && <motion.span initial={{ opacity: 0 }} animate={{ opacity: 1 }}>{label}</motion.span>}
              </NavLink>
            </motion.div>
          ))}
        </nav>

        <div className="border-t border-emerald-700 dark:border-gray-700 p-3 space-y-2 shrink-0">
          <button
            onClick={toggleDark}
            className="flex items-center gap-2 text-xs text-emerald-300 dark:text-gray-400 hover:text-white transition-colors w-full px-1 py-1"
          >
            {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            {!collapsed && (darkMode ? 'Light Mode' : 'Dark Mode')}
          </button>
          {!collapsed && (
            <p className="text-xs text-emerald-300 dark:text-gray-400 mb-2 truncate">{user?.username}</p>
          )}
          <button
            onClick={handleLogout}
            className="flex items-center gap-2 text-xs text-emerald-300 dark:text-gray-400 hover:text-white transition-colors w-full"
          >
            <LogOut className="h-4 w-4" />
            {!collapsed && 'Logout'}
          </button>
        </div>
      </motion.aside>

      <div className="flex-1 flex flex-col min-w-0">
        <header className="h-14 flex items-center justify-between px-4 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 shrink-0">
          <button className="lg:hidden p-1.5 rounded-md hover:bg-gray-100 dark:hover:bg-gray-800" onClick={() => setMobileOpen(true)}>
            <Menu className="h-5 w-5 text-gray-600 dark:text-gray-400" />
          </button>
          <div className="flex items-center gap-3 ml-auto">
            <span className="text-xs text-gray-400 dark:text-gray-500 hidden sm:inline">Child Panel</span>
            <div className="h-7 w-7 rounded-full bg-emerald-600 flex items-center justify-center text-white text-xs font-medium">
              {user?.username?.[0]?.toUpperCase() || 'U'}
            </div>
          </div>
        </header>

        <motion.main
          className="flex-1 overflow-auto p-4 lg:p-6"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.2 }}
        >
          <Outlet />
        </motion.main>
      </div>
    </div>
  );
}

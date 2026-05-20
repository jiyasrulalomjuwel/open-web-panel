import { useState } from 'react';
import { Outlet, NavLink, useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { LayoutDashboard, Users, Package, Server, Settings2, MessageSquare, BarChart3, Inbox, LogOut, ChevronLeft, Menu, Sun, Moon } from 'lucide-react';
import { clearTokens, getUser } from '../lib/api';

const nav = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
  { to: '/accounts', icon: Users, label: 'Accounts' },
  { to: '/packages', icon: Package, label: 'Packages' },
  { to: '/bandwidth', icon: BarChart3, label: 'Bandwidth' },
  { to: '/submissions', icon: Inbox, label: 'Submissions' },
  { to: '/settings', icon: Settings2, label: 'Settings' },
  { to: '/tickets', icon: MessageSquare, label: 'Tickets' },
];

export function Layout() {
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

  const sidebarVariants = {
    expanded: { width: 240 },
    collapsed: { width: 64 },
  };

  return (
    <div className="flex h-screen bg-gray-50 dark:bg-gray-950">
      {mobileOpen && (
        <motion.div
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          className="fixed inset-0 z-40 bg-black/40 lg:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      <motion.aside
        variants={sidebarVariants}
        animate={collapsed ? 'collapsed' : 'expanded'}
        transition={{ duration: 0.2, ease: 'easeInOut' }}
        className={`fixed inset-y-0 left-0 z-50 flex flex-col bg-gray-900 dark:bg-gray-900 text-gray-200 lg:static
          ${mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}`}
      >
        <div className="flex h-14 items-center justify-between px-3 border-b border-gray-700 shrink-0">
          {!collapsed && (
            <motion.span
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="font-semibold text-sm tracking-wide"
            >
              OpenWebPanel
            </motion.span>
          )}
          <button
            onClick={() => { setCollapsed(!collapsed); setMobileOpen(false); }}
            className="p-1.5 rounded-md hover:bg-gray-700 text-gray-400 ml-auto"
          >
            <ChevronLeft className={`h-4 w-4 transition-transform ${collapsed ? 'rotate-180' : ''}`} />
          </button>
        </div>

        <nav className="flex-1 py-3 space-y-1 px-2 overflow-hidden">
          {nav.map(({ to, icon: Icon, label, end }, i) => (
            <motion.div
              key={to}
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.05 }}
            >
              <NavLink
                to={to}
                end={end}
                onClick={() => setMobileOpen(false)}
                className={({ isActive }) =>
                  `flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors ${
                    isActive
                      ? 'bg-blue-600 text-white'
                      : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                  }`
                }
              >
                <Icon className="h-4 w-4 shrink-0" />
                {!collapsed && <motion.span initial={{ opacity: 0 }} animate={{ opacity: 1 }}>{label}</motion.span>}
              </NavLink>
            </motion.div>
          ))}
        </nav>

        <div className="border-t border-gray-700 p-3 space-y-2">
          <button
            onClick={toggleDark}
            className="flex items-center gap-2 text-xs text-gray-400 hover:text-white transition-colors w-full px-1 py-1"
          >
            {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            {!collapsed && (darkMode ? 'Light Mode' : 'Dark Mode')}
          </button>
          {!collapsed && (
            <p className="text-xs text-gray-400 truncate">{user?.username} ({user?.role})</p>
          )}
          <button
            onClick={handleLogout}
            className="flex items-center gap-2 text-xs text-gray-400 hover:text-white transition-colors w-full px-1 py-1"
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
            <span className="text-xs text-gray-400 dark:text-gray-500 hidden sm:inline">Parent Panel</span>
            <div className="h-7 w-7 rounded-full bg-blue-600 flex items-center justify-center text-white text-xs font-medium">
              {user?.username?.[0]?.toUpperCase() || 'A'}
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

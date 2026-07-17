import { useState, useRef, useEffect } from 'react';
import { Outlet, NavLink, useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { LayoutDashboard, Users, Package, Settings2, MessageSquare, BarChart3, Inbox, LogOut, ChevronLeft, Menu, Sun, Moon, Bell, Shield, FileText, Code2, User, ChevronDown } from 'lucide-react';
import { clearTokens, getUser } from '../lib/api';

const nav = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
  { to: '/accounts', icon: Users, label: 'Accounts' },
  { to: '/packages', icon: Package, label: 'Packages' },
  { to: '/bandwidth', icon: BarChart3, label: 'Bandwidth' },
  { to: '/submissions', icon: Inbox, label: 'Submissions' },
  { to: '/settings', icon: Settings2, label: 'Settings' },
  { to: '/tickets', icon: MessageSquare, label: 'Tickets' },
  { to: '/notifications', icon: Bell, label: 'Notifications' },
  { to: '/ip-management', icon: Shield, label: 'IP Management' },
  { to: '/access-log', icon: FileText, label: 'Access Log' },
  { to: '/php-versions', icon: Code2, label: 'PHP Versions' },
];

export function Layout() {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [darkMode, setDarkMode] = useState(document.documentElement.classList.contains('dark'));
  const [profileOpen, setProfileOpen] = useState(false);
  const profileRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const user = getUser();

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (profileRef.current && !profileRef.current.contains(e.target as Node)) {
        setProfileOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

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

        <nav className="flex-1 overflow-y-auto py-3 space-y-1 px-2">
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
      </motion.aside>

      <div className="flex-1 flex flex-col min-w-0">
        <header className="h-14 flex items-center justify-between px-4 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 shrink-0">
          <button className="lg:hidden p-1.5 rounded-md hover:bg-gray-100 dark:hover:bg-gray-800" onClick={() => setMobileOpen(true)}>
            <Menu className="h-5 w-5 text-gray-600 dark:text-gray-400" />
          </button>
          <div className="flex items-center gap-3 ml-auto">
            <span className="text-xs text-gray-400 dark:text-gray-500 hidden sm:inline">Parent Panel</span>
            <div className="relative" ref={profileRef}>
              <button
                onClick={() => setProfileOpen(o => !o)}
                className="flex items-center gap-2 p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
              >
                <div className="h-7 w-7 rounded-full bg-blue-600 flex items-center justify-center text-white text-xs font-medium">
                  {user?.username?.[0]?.toUpperCase() || 'A'}
                </div>
                <ChevronDown className={`h-3.5 w-3.5 text-gray-400 transition-transform duration-150 ${profileOpen ? 'rotate-180' : ''}`} />
              </button>
              <AnimatePresence>
                {profileOpen && (
                  <motion.div
                    initial={{ opacity: 0, y: -8, scale: 0.96 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: -8, scale: 0.96 }}
                    transition={{ duration: 0.12 }}
                    className="absolute right-0 mt-2 w-56 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-lg overflow-hidden z-50"
                  >
                    <div className="px-4 py-3 border-b border-gray-100 dark:border-gray-700">
                      <p className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{user?.username || 'Admin'}</p>
                      <p className="text-xs text-gray-500 dark:text-gray-400 truncate capitalize">{user?.role || 'admin'}</p>
                    </div>
                    <div className="py-1">
                      <button
                        onClick={() => { toggleDark(); setProfileOpen(false); }}
                        className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors"
                      >
                        {darkMode ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
                        {darkMode ? 'Light Mode' : 'Dark Mode'}
                      </button>
                      <button
                        onClick={() => { setProfileOpen(false); navigate('/settings'); }}
                        className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors"
                      >
                        <User className="h-4 w-4" />
                        Admin Profile
                      </button>
                      <hr className="my-1 border-gray-100 dark:border-gray-700" />
                      <button
                        onClick={() => { handleLogout(); setProfileOpen(false); }}
                        className="flex items-center gap-3 w-full px-4 py-2.5 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
                      >
                        <LogOut className="h-4 w-4" />
                        Logout
                      </button>
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          </div>
        </header>

        <main className="flex-1 overflow-y-auto p-4 lg:p-6">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.2 }}
          >
            <Outlet />
          </motion.div>
        </main>
      </div>
    </div>
  );
}

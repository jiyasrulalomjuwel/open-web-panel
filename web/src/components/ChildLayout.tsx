import { useState, useCallback } from 'react';
import { Outlet, NavLink, useNavigate, useLocation } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { FolderOpen, Database, LayoutDashboard, Globe, LifeBuoy, BarChart3, Globe2, Shield, LogOut, ChevronLeft, Menu, Mail, Inbox, Sun, Moon, Server, ChevronDown, ChevronRight, Gauge, HardDrive, ArrowLeftRight, Ban, BarChartHorizontal, Bug } from 'lucide-react';
import { clearTokens, getUser } from '../lib/api';

type NavGroup = {
  label: string;
  icon: any;
  children: { to: string; label: string }[];
};

const groups: NavGroup[] = [
  {
    label: 'Web',
    icon: Globe,
    children: [
      { to: '/child/domains', label: 'Domains' },
      { to: '/child/cms', label: 'CMS Installer' },
      { to: '/child/ssl', label: 'SSL Certs' },
      { to: '/child/redirects', label: 'Redirects' },
      { to: '/child/hotlink', label: 'Hotlink Protection' },
      { to: '/child/stats', label: 'Stats' },
      { to: '/child/errors', label: 'Error Manager' },
    ],
  },
  {
    label: 'Files',
    icon: FolderOpen,
    children: [
      { to: '/child/files', label: 'File Manager' },
      { to: '/child/ftp', label: 'FTP Manager' },
    ],
  },
  {
    label: 'Email',
    icon: Mail,
    children: [
      { to: '/child/emails', label: 'Emails' },
      { to: '/child/webmail', label: 'Webmail' },
    ],
  },
  {
    label: 'System',
    icon: HardDrive,
    children: [
      { to: '/child/bandwidth', label: 'Bandwidth' },
      { to: '/child/tickets', label: 'Support' },
    ],
  },
];

const standalone = [
  { to: '/child/dashboard', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/child/databases', icon: Database, label: 'Databases' },
];

export function ChildLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [darkMode, setDarkMode] = useState(document.documentElement.classList.contains('dark'));
  const [openGroups, setOpenGroups] = useState<Set<string>>(() => {
    const saved = localStorage.getItem('owp_child_nav_groups');
    return saved ? new Set(JSON.parse(saved)) : new Set();
  });
  const navigate = useNavigate();
  const location = useLocation();
  const user = getUser();

  const toggleGroup = useCallback((label: string) => {
    setOpenGroups(prev => {
      const next = new Set(prev);
      if (next.has(label)) next.delete(label);
      else next.add(label);
      localStorage.setItem('owp_child_nav_groups', JSON.stringify([...next]));
      return next;
    });
  }, []);

  const isChildActive = useCallback((paths: string[]) => {
    return paths.some(p => location.pathname === p || location.pathname.startsWith(p + '/'));
  }, [location.pathname]);

  const toggleDark = () => {
    const next = !darkMode;
    setDarkMode(next);
    document.documentElement.classList.toggle('dark', next);
  };

  const handleLogout = () => {
    clearTokens();
    navigate('/login');
  };

  const closeMobile = () => setMobileOpen(false);

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors ${
      isActive
        ? 'bg-emerald-600 text-white'
        : 'text-emerald-100 hover:bg-emerald-800 hover:text-white'
    }`;

  const subLinkClass = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-3 px-3 py-1.5 rounded-md text-sm transition-colors ${
      isActive
        ? 'bg-emerald-600 text-white'
        : 'text-emerald-200 hover:bg-emerald-800 hover:text-white'
    }`;

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

        <nav className="flex-1 overflow-y-auto py-3 space-y-1 px-2 scrollbar-thin scrollbar-thumb-emerald-700 scrollbar-track-transparent">
          {/* Standalone items */}
          {standalone.map(({ to, icon: Icon, label }) => (
            <NavLink
              key={to}
              to={to}
              onClick={closeMobile}
              className={linkClass}
            >
              <Icon className="h-4 w-4 shrink-0" />
              {!collapsed && <span>{label}</span>}
            </NavLink>
          ))}

          {/* Grouped items */}
          {groups.map(g => {
            const groupActive = isChildActive(g.children.map(c => c.to));
            const isOpen = openGroups.has(g.label);
            return (
              <div key={g.label}>
                {collapsed ? (
                  <div className="flex items-center gap-3 px-3 py-2 rounded-md text-sm text-emerald-100">
                    <g.icon className="h-4 w-4 shrink-0" />
                  </div>
                ) : (
                  <button
                    onClick={() => toggleGroup(g.label)}
                    className={`flex items-center gap-3 w-full px-3 py-2 rounded-md text-sm transition-colors ${
                      groupActive
                        ? 'bg-emerald-700/60 text-white'
                        : 'text-emerald-100 hover:bg-emerald-800 hover:text-white'
                    }`}
                  >
                    <g.icon className="h-4 w-4 shrink-0" />
                    <span className="flex-1 text-left">{g.label}</span>
                    {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                  </button>
                )}
                <AnimatePresence initial={false}>
                  {!collapsed && isOpen && (
                    <motion.div
                      key={g.label}
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: 'auto', opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.15 }}
                      className="ml-2 mt-0.5 space-y-0.5 border-l border-emerald-700/50 pl-2 overflow-hidden"
                    >
                      {g.children.map(c => (
                        <NavLink
                          key={c.to}
                          to={c.to}
                          onClick={closeMobile}
                          className={subLinkClass}
                        >
                          <span className="h-1 w-1 rounded-full bg-emerald-400 shrink-0" />
                          <span>{c.label}</span>
                        </NavLink>
                      ))}
                    </motion.div>
                  )}
                </AnimatePresence>
              </div>
            );
          })}
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

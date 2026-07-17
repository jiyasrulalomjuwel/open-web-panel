import { useState, useCallback, useEffect, useRef } from 'react';
import { Outlet, NavLink, useNavigate, useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'framer-motion';
import {
  LayoutDashboard, Database, Globe, FolderOpen, Mail, HardDrive,
  LogOut, Menu, ChevronDown, X, Bell, Search, CheckCheck, ExternalLink,
  Clock, Server, Info
} from 'lucide-react';
import { AccountDetails } from './AccountDetails';
import {
  clearTokens, getUser, getChildNotifications,
  getUnreadNotificationCount, markNotificationRead, markAllNotificationsRead,
} from '../lib/api';

const groups = [
  {
    label: 'Web', icon: Globe,
    children: [
      { to: '/child/domains', label: 'Domains' },
      { to: '/child/cms', label: 'CMS Installer' },
      { to: '/child/ssl', label: 'SSL Certificates' },
      { to: '/child/redirects', label: 'Redirects' },
      { to: '/child/hotlink', label: 'Hotlink Protection' },
      { to: '/child/stats', label: 'Stats' },
      { to: '/child/errors', label: 'Error Handling' },
      { to: '/child/php-version', label: 'PHP Version' },
    ],
  },
  {
    label: 'Files', icon: FolderOpen,
    children: [
      { to: '/child/files', label: 'File Manager' },
      { to: '/child/ftp', label: 'FTP Manager' },
    ],
  },
  {
    label: 'Email', icon: Mail,
    children: [
      { to: '/child/emails', label: 'Emails' },
      { to: '/child/webmail', label: 'Webmail' },
    ],
  },
  {
    label: 'System', icon: Server,
    children: [
      { to: '/child/bandwidth', label: 'Bandwidth' },
      { to: '/child/tickets', label: 'Support' },
    ],
  },
];

const topLinks = [
  { to: '/child/dashboard', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/child/databases', icon: Database, label: 'Databases' },
];

const pageTitles: Record<string, string> = {
  '/child/dashboard': 'Dashboard',
  '/child/databases': 'Databases',
  '/child/domains': 'Domains',
  '/child/cms': 'CMS Installer',
  '/child/ssl': 'SSL Certificates',
  '/child/redirects': 'Redirects',
  '/child/hotlink': 'Hotlink Protection',
  '/child/stats': 'Stats',
  '/child/errors': 'Error Code Handling',
  '/child/php-version': 'PHP Version',
  '/child/files': 'File Manager',
  '/child/ftp': 'FTP Manager',
  '/child/emails': 'Emails',
  '/child/webmail': 'Webmail',
  '/child/bandwidth': 'Bandwidth',
  '/child/tickets': 'Support',
  '/child/notifications': 'Notifications',
};

export function ChildLayout() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [openGroups, setOpenGroups] = useState<Set<string>>(() => {
    const saved = localStorage.getItem('owp_child_nav_groups');
    return saved ? new Set(JSON.parse(saved)) : new Set();
  });
  const [accountDetailsOpen, setAccountDetailsOpen] = useState(false);
  const [notifs, setNotifs] = useState<any[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [notifOpen, setNotifOpen] = useState(false);
  const [selectedNotif, setSelectedNotif] = useState<any>(null);
  const notifRef = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();
  const location = useLocation();
  const user = getUser();

  const fetchNotifs = useCallback(async () => {
    try {
      const [n, c] = await Promise.all([
        getChildNotifications() as Promise<any[]>,
        getUnreadNotificationCount() as Promise<{ count: number }>,
      ]);
      setNotifs(Array.isArray(n) ? n : []);
      setUnreadCount(c?.count ?? 0);
    } catch {}
  }, []);

  useEffect(() => {
    fetchNotifs();
    const iv = setInterval(fetchNotifs, 30000);
    return () => clearInterval(iv);
  }, [fetchNotifs]);

  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (notifRef.current && !notifRef.current.contains(e.target as Node)) {
        setNotifOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  const handleMarkRead = async (id: number) => {
    try {
      await markNotificationRead(id);
      setNotifs(prev => prev.map(n => n.id === id ? { ...n, is_read: true } : n));
      setUnreadCount(c => Math.max(0, c - 1));
    } catch {}
  };

  const handleMarkAllRead = async () => {
    try {
      await markAllNotificationsRead();
      setNotifs(prev => prev.map(n => ({ ...n, is_read: true })));
      setUnreadCount(0);
    } catch {}
  };

  const toggleGroup = useCallback((label: string) => {
    setOpenGroups(prev => {
      const next = new Set(prev);
      if (next.has(label)) next.delete(label);
      else next.add(label);
      localStorage.setItem('owp_child_nav_groups', JSON.stringify([...next]));
      return next;
    });
  }, []);

  const isGroupActive = useCallback((paths: string[]) => {
    return paths.some(p => location.pathname === p || location.pathname.startsWith(p + '/'));
  }, [location.pathname]);

  const handleLogout = () => {
    clearTokens();
    navigate('/login');
  };

  const isActive = (to: string) =>
    location.pathname === to || location.pathname.startsWith(to + '/');

  const currentTitle = pageTitles[location.pathname] || 'Dashboard';

  const sidebarLink = (to: string, label: string, icon: any) => (
    <NavLink
      key={to}
      to={to}
      onClick={() => setMobileOpen(false)}
      className={({ isActive: act }) =>
        `flex items-center gap-3 px-3 py-2.5 rounded-xl text-sm font-medium transition-all duration-150 ${
          act
            ? 'bg-gray-100 text-gray-900'
            : 'text-gray-500 hover:text-gray-700 hover:bg-gray-50'
        }`
      }
    >
      <div className="w-5 h-5 flex items-center justify-center shrink-0">
        {icon}
      </div>
      <span>{label}</span>
    </NavLink>
  );

  const userInitial = user?.username?.[0]?.toUpperCase() || 'U';

  return (
    <div className="flex h-screen bg-surface overflow-hidden">
      <AnimatePresence>
        {mobileOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-40 bg-black/30"
            onClick={() => setMobileOpen(false)}
          />
        )}
      </AnimatePresence>

      <aside className={`fixed inset-y-0 left-0 z-50 flex flex-col bg-white border-r border-border-subtle w-60
        ${mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
        transition-transform duration-200 ease-in-out`}
      >
        <div className="flex h-[72px] items-center px-5 shrink-0 border-b border-border-subtle">
          <div className="flex items-center gap-3">
            <div className="w-8 h-8 rounded-xl bg-gradient-to-br from-[#2563EB] to-[#3B82F6] flex items-center justify-center">
              <span className="text-white text-xs font-bold">OW</span>
            </div>
            <div>
              <span className="text-sm font-semibold text-gray-900">Site Panel</span>
              <p className="text-[11px] text-gray-400 leading-none mt-0.5">Account Management</p>
            </div>
          </div>
        </div>

        <nav className="flex-1 overflow-y-auto py-4 px-3 space-y-0.5">
          {topLinks.map(({ to, icon: Icon, label }) =>
            sidebarLink(to, label, <Icon className="w-5 h-5" strokeWidth={1.5} />)
          )}

          <div className="my-4 border-t border-border-subtle" />

          {groups.map(g => {
            const groupActive = isGroupActive(g.children.map(c => c.to));
            const isOpen = openGroups.has(g.label);
            return (
              <div key={g.label}>
                <button
                  onClick={() => toggleGroup(g.label)}
                  className={`flex items-center gap-3 w-full px-3 py-2.5 rounded-xl text-sm font-medium transition-all duration-150 ${
                    groupActive
                      ? 'text-gray-900 bg-gray-50'
                      : 'text-gray-500 hover:text-gray-700 hover:bg-gray-50'
                  }`}
                >
                  <div className="w-5 h-5 flex items-center justify-center shrink-0">
                    <g.icon className="w-5 h-5" strokeWidth={1.5} />
                  </div>
                  <span className="flex-1 text-left">{g.label}</span>
                  <motion.div
                    animate={{ rotate: isOpen ? 180 : 0 }}
                    transition={{ duration: 0.15 }}
                  >
                    <ChevronDown className="w-3.5 h-3.5 text-gray-400" strokeWidth={2} />
                  </motion.div>
                </button>
                <AnimatePresence initial={false}>
                  {isOpen && (
                    <motion.div
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: 'auto', opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.15, ease: 'easeInOut' }}
                      className="ml-2 mt-0.5 space-y-0.5 pl-3 border-l border-border-subtle overflow-hidden"
                    >
                      {g.children.map(c => (
                        <NavLink
                          key={c.to}
                          to={c.to}
                          onClick={() => setMobileOpen(false)}
                          className={({ isActive: act }) =>
                            `flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-all duration-150 ${
                              act
                                ? 'text-gray-900 bg-gray-100 font-medium'
                                : 'text-gray-500 hover:text-gray-700 hover:bg-gray-50'
                            }`
                          }
                        >
                          <span className="w-1 h-1 rounded-full bg-gray-300 shrink-0" />
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

        <div className="border-t border-border-subtle p-3 shrink-0">
          <div className="flex items-center gap-3 px-2 py-2 mb-1">
            <div className="w-9 h-9 rounded-xl bg-gradient-to-br from-[#2563EB] to-[#3B82F6] flex items-center justify-center text-white text-xs font-bold shrink-0">
              {userInitial}
            </div>
            <div className="min-w-0 flex-1">
              <p className="text-sm font-semibold text-gray-900 truncate">{user?.username || 'User'}</p>
              <p className="text-[11px] text-gray-400 truncate">Account Owner</p>
            </div>
          </div>
          <button
            onClick={handleLogout}
            className="flex items-center gap-2 w-full px-3 py-2.5 rounded-xl text-sm text-gray-500 hover:text-red-600 hover:bg-red-50 transition-all duration-150"
          >
            <LogOut className="w-4 h-4" strokeWidth={1.5} />
            <span>Logout</span>
          </button>
        </div>
      </aside>

      <div className="flex-1 flex flex-col min-w-0 lg:ml-60">
        <header className="h-[72px] flex items-center justify-between px-6 lg:px-8 bg-white border-b border-border-subtle shrink-0 sticky top-0 z-30">
          <div className="flex items-center gap-4">
            <button
              className="lg:hidden p-2 rounded-xl hover:bg-gray-100 transition-colors"
              onClick={() => setMobileOpen(true)}
            >
              <Menu className="w-5 h-5 text-gray-500" />
            </button>
            <div className="flex items-center gap-3 text-sm">
              <span className="hidden sm:inline text-gray-400">Child Panel</span>
              <span className="hidden sm:inline text-gray-300">/</span>
              <span className="text-gray-900 font-semibold">{currentTitle}</span>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="hidden md:flex relative">
              <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" strokeWidth={2} />
              <input
                type="text"
                placeholder="Search here..."
                className="w-60 h-10 pl-10 pr-4 rounded-xl border border-border-subtle bg-white text-sm text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-[#2563EB] focus:border-transparent transition-all duration-150"
              />
            </div>

            <button
              className="lg:hidden p-2.5 rounded-xl hover:bg-gray-100 transition-colors"
              onClick={() => setAccountDetailsOpen(true)}
              title="Account Details"
            >
              <Info className="w-5 h-5 text-gray-500" strokeWidth={1.5} />
            </button>

            <div className="relative" ref={notifRef}>
              <button
                onClick={() => setNotifOpen(o => !o)}
                className="p-2.5 rounded-xl hover:bg-gray-100 transition-colors relative"
              >
                <Bell className="w-5 h-5 text-gray-500" strokeWidth={1.5} />
                {unreadCount > 0 && (
                  <span className="absolute -top-0.5 -right-0.5 w-[18px] h-[18px] rounded-full bg-[#2563EB] text-[10px] font-bold text-white flex items-center justify-center ring-2 ring-white">
                    {unreadCount > 9 ? '9+' : unreadCount}
                  </span>
                )}
              </button>

              <AnimatePresence>
                {notifOpen && (
                  <motion.div
                    initial={{ opacity: 0, y: -8, scale: 0.96 }}
                    animate={{ opacity: 1, y: 0, scale: 1 }}
                    exit={{ opacity: 0, y: -8, scale: 0.96 }}
                    transition={{ duration: 0.12 }}
                    className="absolute right-0 mt-2 w-80 sm:w-96 bg-white border border-border-subtle rounded-card shadow-dropdown overflow-hidden z-50"
                  >
                    <div className="flex items-center justify-between px-5 py-4 border-b border-border-subtle">
                      <div className="flex items-center gap-2.5">
                        <Bell className="w-4 h-4 text-gray-900" strokeWidth={1.5} />
                        <span className="text-sm font-semibold text-gray-900">Notifications</span>
                        {unreadCount > 0 && (
                          <span className="text-[11px] font-medium text-gray-500 bg-gray-100 px-2 py-0.5 rounded-full">{unreadCount} new</span>
                        )}
                      </div>
                      {unreadCount > 0 && (
                        <button onClick={handleMarkAllRead} className="text-xs font-medium text-gray-500 hover:text-gray-700 flex items-center gap-1 px-2 py-1 rounded-lg hover:bg-gray-100 transition-colors">
                          <CheckCheck className="w-3.5 h-3.5" /> Mark all read
                        </button>
                      )}
                    </div>
                    <div className="max-h-80 overflow-y-auto">
                      {notifs.length === 0 ? (
                        <div className="px-5 py-12 text-center">
                          <Bell className="w-8 h-8 mx-auto mb-3 text-gray-200" strokeWidth={1} />
                          <p className="text-sm font-medium text-gray-500">No notifications</p>
                          <p className="text-xs text-gray-400 mt-1">You're all caught up!</p>
                        </div>
                      ) : (
                        notifs.map(n => (
                          <button
                            key={n.id}
                            onClick={() => {
                              setNotifOpen(false);
                              if (!n.is_read) handleMarkRead(n.id);
                              setSelectedNotif(n);
                            }}
                            className={`w-full text-left px-5 py-3.5 border-b border-border-subtle hover:bg-gray-50 transition-colors ${!n.is_read ? 'bg-gray-50/50' : ''}`}
                          >
                            <div className="flex items-start gap-3">
                              {!n.is_read && <span className="w-2 h-2 rounded-full bg-[#2563EB] shrink-0 mt-1.5" />}
                              <div className={`min-w-0 flex-1 ${n.is_read ? 'ml-5' : ''}`}>
                                <p className={`text-sm ${!n.is_read ? 'font-semibold' : 'font-medium'} text-gray-900 truncate`}>
                                  {n.title}
                                </p>
                                <p className="text-xs text-gray-500 mt-0.5 line-clamp-2 leading-relaxed">{n.message}</p>
                                <div className="flex items-center gap-1 mt-1.5">
                                  <Clock className="w-3 h-3 text-gray-400" />
                                  <span className="text-[11px] text-gray-400">
                                    {new Date(n.created_at + 'Z').toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                                  </span>
                                </div>
                              </div>
                            </div>
                          </button>
                        ))
                      )}
                    </div>
                    <button
                      onClick={() => { setNotifOpen(false); navigate('/child/notifications'); }}
                      className="w-full flex items-center justify-center gap-1.5 px-5 py-3 text-xs font-medium text-gray-500 hover:text-gray-700 hover:bg-gray-50 border-t border-border-subtle transition-colors"
                    >
                      <ExternalLink className="w-3 h-3" />
                      View All Notifications
                    </button>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>

            <div className="w-9 h-9 rounded-xl bg-gradient-to-br from-[#2563EB] to-[#3B82F6] flex items-center justify-center text-white text-xs font-bold shrink-0">
              {userInitial}
            </div>
          </div>
        </header>

        <div className="flex-1 flex overflow-hidden">
          <main className="flex-1 overflow-auto p-6 lg:p-8">
            <Outlet />
          </main>

          <aside className="hidden lg:block w-64 shrink-0 border-l border-border-subtle overflow-auto p-5">
            <div className="sticky top-0">
              <AccountDetails />
            </div>
          </aside>
        </div>
      </div>

      <AnimatePresence>
        {accountDetailsOpen && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.15 }}
              className="fixed inset-0 z-40 bg-black/30"
              onClick={() => setAccountDetailsOpen(false)}
            />
            <motion.div
              initial={{ x: '100%' }}
              animate={{ x: 0 }}
              exit={{ x: '100%' }}
              transition={{ type: 'tween', duration: 0.2, ease: 'easeInOut' }}
              className="fixed right-0 top-0 bottom-0 z-50 w-64 bg-white border-l border-border-subtle shadow-xl flex flex-col"
            >
              <div className="flex items-center justify-between px-5 py-4 border-b border-border-subtle shrink-0">
                <h2 className="text-xs font-semibold text-gray-900 uppercase tracking-wide">Account Details</h2>
                <button
                  onClick={() => setAccountDetailsOpen(false)}
                  className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-400 hover:text-gray-600 transition-colors"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>
              <div className="flex-1 overflow-auto p-5">
                <AccountDetails />
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {selectedNotif && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.12 }}
              className="absolute inset-0 bg-black/30"
              onClick={() => setSelectedNotif(null)}
            />
            <motion.div
              initial={{ opacity: 0, scale: 0.97 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.97 }}
              transition={{ duration: 0.15 }}
              className="relative w-full max-w-lg bg-white rounded-card shadow-dropdown border border-border-subtle overflow-hidden"
            >
              <div className="flex items-center justify-between px-6 py-4 border-b border-border-subtle">
                <span className="text-sm font-semibold text-gray-900">{selectedNotif.title}</span>
                <button
                  onClick={() => setSelectedNotif(null)}
                  className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-400 hover:text-gray-600 transition-colors"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>
              <div className="px-6 py-5 max-h-[70vh] overflow-y-auto">
                <div className="flex items-center gap-2 text-xs text-gray-400 mb-4 bg-gray-50 rounded-lg px-3 py-2">
                  <Clock className="w-3 h-3" />
                  {new Date(selectedNotif.created_at + 'Z').toLocaleDateString('en-US', {
                    month: 'long', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit'
                  })}
                </div>
                <p className="text-sm text-gray-700 leading-relaxed whitespace-pre-wrap">
                  {selectedNotif.message}
                </p>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
}

import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { Bell, Clock, ArrowLeft, CheckCheck, X, ExternalLink, AlertCircle } from 'lucide-react';
import { getChildNotifications, markNotificationRead, markAllNotificationsRead } from '../lib/api';

interface Notification {
  id: number;
  title: string;
  message: string;
  created_at: string;
  is_read: boolean;
}

export function ChildNotifications() {
  const [notifs, setNotifs] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [detailNotif, setDetailNotif] = useState<Notification | null>(null);
  const navigate = useNavigate();

  const fetchNotifs = useCallback(async () => {
    try {
      const n = await getChildNotifications() as Notification[];
      setNotifs(Array.isArray(n) ? n : []);
    } catch (e: any) { console.error('Fetch notifs:', e); }
    setLoading(false);
  }, []);

  useEffect(() => { fetchNotifs(); }, [fetchNotifs]);

  const handleMarkRead = async (id: number) => {
    try {
      await markNotificationRead(id);
      setNotifs(prev => prev.map(n => n.id === id ? { ...n, is_read: true } : n));
    } catch (e: any) { console.error('Mark read:', e); }
  };

  const handleMarkAllRead = async () => {
    try {
      await markAllNotificationsRead();
      setNotifs(prev => prev.map(n => ({ ...n, is_read: true })));
    } catch (e: any) { console.error('Mark all read:', e); }
  };

  const unreadCount = notifs.filter(n => !n.is_read).length;

  const fmtDate = (d: string) => {
    const dt = new Date(d + 'Z');
    return dt.toLocaleDateString('en-US', {
      month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit'
    });
  };

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto space-y-6 animate-pulse">
        <div className="flex items-center gap-4">
          <div className="w-9 h-9 bg-gray-100 rounded-xl" />
          <div>
            <div className="h-7 w-40 bg-gray-100 rounded-lg" />
            <div className="h-4 w-24 bg-gray-50 rounded mt-2" />
          </div>
        </div>
        {[...Array(4)].map((_, i) => (
          <div key={i} className="bg-white rounded-card border border-border-subtle p-5 space-y-3">
            <div className="h-4 w-1/3 bg-gray-100 rounded" />
            <div className="h-3 w-2/3 bg-gray-50 rounded" />
            <div className="h-3 w-1/4 bg-gray-50 rounded" />
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="max-w-3xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <button
            onClick={() => navigate('/child/dashboard')}
            className="p-2 rounded-xl hover:bg-gray-100 text-gray-400 hover:text-gray-600 transition-colors"
          >
            <ArrowLeft className="w-4 h-4" strokeWidth={1.5} />
          </button>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-[32px] font-bold text-gray-900 tracking-tight">Notifications</h1>
              {unreadCount > 0 && (
                <span className="text-xs font-semibold text-gray-500 bg-gray-100 px-2.5 py-1 rounded-full">
                  {unreadCount} unread
                </span>
              )}
            </div>
            <p className="text-sm text-gray-500 mt-0.5">
              {notifs.length} total {notifs.length === 1 ? 'notification' : 'notifications'}
            </p>
          </div>
        </div>
        {unreadCount > 0 && (
          <button
            onClick={handleMarkAllRead}
            className="text-xs font-medium text-gray-500 hover:text-gray-700 flex items-center gap-1.5 px-3.5 py-2 rounded-xl bg-gray-50 hover:bg-gray-100 transition-all"
          >
            <CheckCheck className="w-3.5 h-3.5" />
            Mark All Read
          </button>
        )}
      </div>

      {notifs.length === 0 ? (
        <div className="bg-white rounded-card border border-border-subtle shadow-soft flex flex-col items-center py-16 text-center">
          <div className="w-14 h-14 rounded-2xl bg-gray-50 flex items-center justify-center mb-4">
            <Bell className="w-7 h-7 text-gray-300" strokeWidth={1} />
          </div>
          <p className="text-base font-semibold text-gray-900">All caught up!</p>
          <p className="text-sm text-gray-500 mt-1">You have no notifications at this time.</p>
        </div>
      ) : (
        <div className="space-y-3">
          {notifs.map((n, idx) => (
            <motion.div
              key={n.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: idx * 0.03, duration: 0.2 }}
            >
              <div
                className={`bg-white rounded-card border shadow-soft overflow-hidden transition-all duration-150 ${
                  !n.is_read ? 'border-[#2563EB]/20' : 'border-border-subtle'
                } ${expandedId === n.id ? 'shadow-card-hover' : 'hover:shadow-card-hover'}`}
              >
                <button
                  onClick={() => {
                    if (expandedId === n.id) {
                      setExpandedId(null);
                    } else {
                      setExpandedId(n.id);
                      if (!n.is_read) handleMarkRead(n.id);
                    }
                  }}
                  className="w-full text-left px-6 py-4 flex items-start justify-between gap-3"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2.5 mb-0.5">
                      {!n.is_read && (
                        <span className="w-2 h-2 rounded-full bg-[#2563EB] shrink-0" />
                      )}
                      <h3 className={`text-sm ${!n.is_read ? 'font-semibold' : 'font-medium'} text-gray-900`}>
                        {n.title}
                      </h3>
                    </div>
                    <div className="flex items-center gap-2 mt-1.5">
                      <Clock className="w-3 h-3 text-gray-400" />
                      <span className="text-xs text-gray-400">{fmtDate(n.created_at)}</span>
                    </div>
                    <p className={`text-sm text-gray-600 mt-2 leading-relaxed ${
                      expandedId === n.id ? '' : 'line-clamp-2'
                    }`}>
                      {n.message}
                    </p>
                  </div>
                  <div className="flex items-start gap-1 shrink-0 mt-0.5">
                    {!n.is_read && (
                      <span
                        onClick={e => { e.stopPropagation(); handleMarkRead(n.id); }}
                        className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-400 hover:text-gray-600 transition-colors"
                      >
                        <CheckCheck className="w-3.5 h-3.5" />
                      </span>
                    )}
                    <span
                      onClick={e => { e.stopPropagation(); setDetailNotif(n); }}
                      className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-400 hover:text-gray-600 transition-colors"
                    >
                      <ExternalLink className="w-3.5 h-3.5" />
                    </span>
                  </div>
                </button>
              </div>
            </motion.div>
          ))}
        </div>
      )}

      <AnimatePresence>
        {detailNotif && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.12 }}
              className="absolute inset-0 bg-black/30"
            />
            <motion.div
              initial={{ opacity: 0, scale: 0.97 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.97 }}
              transition={{ duration: 0.15 }}
              className="relative w-full max-w-lg bg-white rounded-card shadow-dropdown border border-border-subtle overflow-hidden"
            >
              <div className="flex items-center justify-between px-6 py-4 border-b border-border-subtle">
                <div className="flex items-center gap-2.5">
                  <div className="w-8 h-8 rounded-lg bg-gray-50 flex items-center justify-center">
                    <Bell className="w-4 h-4 text-gray-500" strokeWidth={1.5} />
                  </div>
                  <span className="text-sm font-semibold text-gray-900">{detailNotif.title}</span>
                </div>
                <button
                  onClick={() => setDetailNotif(null)}
                  className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-400 hover:text-gray-600 transition-colors"
                  aria-label="Close"
                >
                  <X className="w-5 h-5" />
                </button>
              </div>
              <div className="px-6 py-5 max-h-[70vh] overflow-y-auto">
                <div className="flex items-center gap-2 text-xs text-gray-400 mb-4 bg-gray-50 rounded-lg px-3.5 py-2.5">
                  <Clock className="w-3.5 h-3.5 text-gray-400" />
                  {fmtDate(detailNotif.created_at)}
                  {!detailNotif.is_read && (
                    <span className="ml-auto text-gray-500 font-medium flex items-center gap-1">
                      <span className="w-1.5 h-1.5 rounded-full bg-[#2563EB]" />
                      Unread
                    </span>
                  )}
                </div>
                <p className="text-sm text-gray-700 leading-relaxed whitespace-pre-wrap">
                  {detailNotif.message}
                </p>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
}

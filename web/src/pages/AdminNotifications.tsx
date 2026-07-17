import { useEffect, useState, useCallback } from 'react';
import { Bell, Plus, Trash2, Send, ChevronDown, Check, X } from 'lucide-react';
import { getAdminNotifications, createAdminNotification, deleteAdminNotification, getAccounts } from '../lib/api';
import Card from '../components/ui/Card';
import { Button } from '../components/ui/Button';

interface Notification {
  id: number;
  account_id: number;
  title: string;
  message: string;
  created_at: string;
}

interface Account {
  id: number;
  username: string;
  domain: string;
}

export function AdminNotifications() {
  const [notifs, setNotifs] = useState<Notification[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [sending, setSending] = useState(false);
  const [form, setForm] = useState({ title: '', message: '', scope: 'all', account_id: '' });

  const fetchAll = useCallback(async () => {
    try {
      const [n, a] = await Promise.all([
        getAdminNotifications() as Promise<Notification[]>,
        getAccounts() as Promise<Account[]>,
      ]);
      setNotifs(Array.isArray(n) ? n : []);
      setAccounts(Array.isArray(a) ? a : []);
    } catch {}
    setLoading(false);
  }, []);

  useEffect(() => { fetchAll(); }, [fetchAll]);

  const handleSend = async () => {
    if (!form.title.trim() || !form.message.trim()) return;
    setSending(true);
    try {
      const payload: { title: string; message: string; account_id?: number } = {
        title: form.title.trim(),
        message: form.message.trim(),
      };
      if (form.scope === 'single' && form.account_id) {
        payload.account_id = parseInt(form.account_id);
      }
      await createAdminNotification(payload);
      setForm({ title: '', message: '', scope: 'all', account_id: '' });
      setShowForm(false);
      await fetchAll();
    } catch {}
    setSending(false);
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteAdminNotification(id);
      setNotifs(prev => prev.filter(n => n.id !== id));
    } catch {}
  };

  const fmtDate = (d: string) => {
    const dt = new Date(d + 'Z');
    return dt.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric', hour: '2-digit', minute: '2-digit' });
  };

  const scopeInfo = (n: Notification) => {
    if (n.account_id === 0 || n.account_id === null) return 'All Accounts';
    const acct = accounts.find(a => a.id === n.account_id);
    return acct ? `${acct.username} (${acct.domain})` : `Account #${n.account_id}`;
  };

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto space-y-4">
        <div className="h-8 w-48 bg-gray-100 rounded animate-pulse" />
        <div className="h-20 bg-gray-100 rounded-xl animate-pulse" />
      </div>
    );
  }

  return (
    <div className="max-w-3xl mx-auto space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-gray-900">Notifications</h1>
          <p className="text-sm text-gray-500 mt-0.5">Send and manage announcements to child panels</p>
        </div>
        <Button onClick={() => setShowForm(o => !o)}>
          <Plus className="h-4 w-4 mr-1.5" />
          New Notification
        </Button>
      </div>

      {showForm && (
        <Card>
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-sm font-semibold text-gray-900">Compose Notification</h2>
            <button onClick={() => setShowForm(false)} className="p-1 rounded hover:bg-gray-100 text-gray-400">
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="space-y-3">
            <div>
              <label className="text-xs font-medium text-gray-500 block mb-1">Scope</label>
              <div className="flex gap-3">
                <label className="flex items-center gap-1.5 text-sm text-gray-700 cursor-pointer">
                  <input
                    type="radio"
                    name="scope"
                    value="all"
                    checked={form.scope === 'all'}
                    onChange={() => setForm(f => ({ ...f, scope: 'all' }))}
                    className="accent-purple-600"
                  />
                  All Accounts
                </label>
                <label className="flex items-center gap-1.5 text-sm text-gray-700 cursor-pointer">
                  <input
                    type="radio"
                    name="scope"
                    value="single"
                    checked={form.scope === 'single'}
                    onChange={() => setForm(f => ({ ...f, scope: 'single' }))}
                    className="accent-purple-600"
                  />
                  Specific Account
                </label>
              </div>
            </div>
            {form.scope === 'single' && (
              <div>
                <label className="text-xs font-medium text-gray-500 block mb-1">Account</label>
                <select
                  value={form.account_id}
                  onChange={e => setForm(f => ({ ...f, account_id: e.target.value }))}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent bg-white"
                >
                  <option value="">Select an account...</option>
                  {accounts.map(a => (
                    <option key={a.id} value={a.id}>{a.username} ({a.domain})</option>
                  ))}
                </select>
              </div>
            )}
            <div>
              <label className="text-xs font-medium text-gray-500 block mb-1">Title</label>
              <input
                type="text"
                value={form.title}
                onChange={e => setForm(f => ({ ...f, title: e.target.value }))}
                placeholder="e.g. Server Maintenance"
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-gray-500 block mb-1">Message</label>
              <textarea
                value={form.message}
                onChange={e => setForm(f => ({ ...f, message: e.target.value }))}
                placeholder="Notification message..."
                rows={4}
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent resize-none"
              />
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setShowForm(false)}>Cancel</Button>
              <Button onClick={handleSend} disabled={!form.title.trim() || !form.message.trim() || sending}>
                <Send className="h-4 w-4 mr-1.5" />
                {sending ? 'Sending...' : 'Send Notification'}
              </Button>
            </div>
          </div>
        </Card>
      )}

      <div className="space-y-2">
        {notifs.length === 0 ? (
          <Card>
            <div className="flex flex-col items-center py-10 text-center">
              <Bell className="h-10 w-10 text-gray-200 mb-3" strokeWidth={1} />
              <p className="text-sm font-medium text-gray-500">No notifications sent</p>
              <p className="text-xs text-gray-400 mt-1">Click "New Notification" to create one</p>
            </div>
          </Card>
        ) : (
          notifs.map(n => (
            <div key={n.id} className="bg-white border border-gray-200 rounded-lg px-5 py-4 flex items-start justify-between gap-4 hover:border-gray-300 transition-colors">
              <div className="min-w-0">
                <div className="flex items-center gap-2 mb-1">
                  <span className={`inline-flex items-center gap-1 text-[11px] font-medium px-2 py-0.5 rounded-full ${
                    n.account_id === 0 || n.account_id === null
                      ? 'bg-purple-50 text-purple-700'
                      : 'bg-blue-50 text-blue-700'
                  }`}>
                    {n.account_id === 0 || n.account_id === null ? (
                      <><Check className="h-3 w-3" /> All Accounts</>
                    ) : (
                      <><User className="h-3 w-3" /> {scopeInfo(n)}</>
                    )}
                  </span>
                  <span className="text-[11px] text-gray-400">{fmtDate(n.created_at)}</span>
                </div>
                <h3 className="text-sm font-semibold text-gray-900">{n.title}</h3>
                <p className="text-sm text-gray-600 mt-0.5 whitespace-pre-wrap">{n.message}</p>
              </div>
              <button
                onClick={() => handleDelete(n.id)}
                className="p-1.5 rounded-lg hover:bg-red-50 text-gray-300 hover:text-red-500 transition-colors shrink-0"
                title="Delete notification"
              >
                <Trash2 className="h-4 w-4" strokeWidth={1.5} />
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function User({ className }: { className?: string }) {
  return (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={className}>
      <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
      <circle cx="12" cy="7" r="4" />
    </svg>
  );
}

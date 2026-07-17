import { useEffect, useState, useCallback, useRef } from 'react';
import { motion } from 'framer-motion';
import { getChildAccount, getTickets, createTicket, getTicketMessages, replyTicket, updateTicketStatus, getAdminTickets, getAdminTicketMessages, replyAdminTicket, deleteAdminTicket, updateAdminTicketStatus } from '../lib/api';
import { LifeBuoy, Plus, X, Send, MessageSquare, Clock, CheckCircle, AlertTriangle, Loader2 } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }
const ensureArray = (v: any): any[] => Array.isArray(v) ? v : [];


type Ticket = {
  id: number;
  account_id: number;
  username?: string;
  subject: string;
  status: string;
  created_at: string;
  updated_at: string;
  message_count: number;
};

type Message = {
  id: number;
  sender_type: string;
  sender_id: number;
  sender_name?: string;
  message: string;
  created_at: string;
};

export function Tickets() {
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [subject, setSubject] = useState('');
  const [msg, setMsg] = useState('');
  const [creating, setCreating] = useState(false);
  const [selectedTicket, setSelectedTicket] = useState<number | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [msgsLoading, setMsgsLoading] = useState(false);
  const [replyMsg, setReplyMsg] = useState('');
  const [sending, setSending] = useState(false);
  const [account, setAccount] = useState<any>(null);
  const inflightTicketId = useRef<number | null>(null);

  useEffect(() => {
    getChildAccount().then(setAccount).catch((e: any) => console.error('Load account:', e));
  }, []);

  const loadTickets = useCallback(() => {
    setLoading(true);
    getTickets({ limit: 100 }).then(d => { setTickets(ensureArray(d?.tickets || d)); }).catch((e: any) => console.error('Load tickets:', e)).finally(() => setLoading(false));
  }, []);

  useEffect(() => { loadTickets(); }, [loadTickets]);

  const loadMessages = async (id: number) => {
    setSelectedTicket(id);
    setMsgsLoading(true);
    inflightTicketId.current = id;
    try {
      const res = await getTicketMessages(id, { limit: 500 });
      if (inflightTicketId.current === id) {
        setMessages(ensureArray(res?.messages || res));
      }
    } catch (e: any) {
      console.error('Load messages:', e);
      if (inflightTicketId.current === id) setMessages([]);
    }
    finally {
      if (inflightTicketId.current === id) setMsgsLoading(false);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    try {
      await createTicket({ subject, message: msg });
      setShowCreate(false); setSubject(''); setMsg('');
      setSuccess('Ticket created');
      loadTickets();
    } catch (err: any) { setError(err?.error || 'Failed to create ticket'); }
    finally { setCreating(false); }
  };

  const handleReply = async () => {
    if (!replyMsg || !selectedTicket) return;
    setSending(true);
    try {
      await replyTicket(selectedTicket, replyMsg);
      setReplyMsg('');
      setSuccess('Reply sent');
      loadMessages(selectedTicket);
    } catch (err: any) { setError(err?.error || 'Failed to send'); }
    finally { setSending(false); }
  };

  const statusBadge = (s: string) => {
    const colors: Record<string, string> = { open: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/50 dark:text-yellow-300', closed: 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400', replied: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300', pending: 'bg-purple-100 text-purple-700 dark:bg-purple-900/50 dark:text-purple-300' };
    return <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[s] || 'bg-gray-100 text-gray-500'}`}>{s}</span>;
  };

  if (loading) return (
    <div className="space-y-4">
      <div className="h-6 bg-gray-200 dark:bg-gray-700 rounded w-40 animate-pulse" />
      {[1,2].map(i => <Card key={i}><div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 animate-pulse" /></Card>)}
    </div>
  );

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Support Tickets</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{tickets.length} ticket(s)</p>
        </div>
        <Button onClick={() => setShowCreate(true)}>
          <Plus className="h-4 w-4" /> New Ticket
        </Button>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          <AlertTriangle className="h-4 w-4 shrink-0" /> {error}
          <button onClick={() => setError('')} className="ml-auto text-red-400 hover:text-red-600"><X className="h-4 w-4" /></button>
        </div>
      )}
      {success && (
        <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">
          <span>{success}</span>
          <button onClick={() => setSuccess('')} className="ml-auto text-emerald-400 hover:text-emerald-600"><X className="h-4 w-4" /></button>
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Ticket list */}
        <div className="lg:col-span-1 space-y-2">
          {!Array.isArray(tickets) || tickets.length === 0 ? (
            <EmptyState title="No tickets yet" />
          ) : tickets.map((t, i) => (
            <motion.div
              key={t.id}
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.03 }}
            >
              <div onClick={() => loadMessages(t.id)}
                className={`bg-white dark:bg-gray-800 rounded-xl border p-4 cursor-pointer transition-all ${selectedTicket === t.id ? 'ring-2 ring-emerald-500 border-emerald-500' : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'}`}>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{t.subject}</span>
                  {statusBadge(t.status)}
                </div>
                <div className="flex items-center gap-3 text-xs text-gray-400 dark:text-gray-500">
                  <span className="flex items-center gap-1"><MessageSquare className="h-3 w-3" />{t.message_count}</span>
                  <span className="flex items-center gap-1"><Clock className="h-3 w-3" />{new Date(t.updated_at).toLocaleDateString()}</span>
                </div>
              </div>
            </motion.div>
          ))}
        </div>

        {/* Messages */}
        <div className="lg:col-span-2">
          {selectedTicket ? (
            <Card padding={false}>
              <div className="flex flex-col h-[500px]">
                <div className="flex-1 overflow-y-auto p-4 space-y-3">
                  {msgsLoading ? (
                    <div className="flex items-center justify-center h-full"><Spinner /></div>
                  ) : !Array.isArray(messages) || messages.length === 0 ? (
                    <div className="text-center py-12 text-gray-400 dark:text-gray-500 text-sm">No messages</div>
                  ) : messages.map(m => (
                    <div key={m.id} className={`flex ${m.sender_type === 'admin' ? 'justify-start' : 'justify-end'}`}>
                      <div className={`max-w-[80%] rounded-lg px-4 py-2 text-sm ${m.sender_type === 'admin' ? 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200' : 'bg-emerald-600 text-white'}`}>
                        <div className="text-xs opacity-70 mb-1">{m.sender_name || (m.sender_type === 'admin' ? 'Staff' : account?.username || 'You')}</div>
                        <div className="whitespace-pre-wrap">{m.message}</div>
                        <div className="text-xs opacity-50 mt-1">{new Date(m.created_at).toLocaleString()}</div>
                      </div>
                    </div>
                  ))}
                </div>
                <div className="border-t border-gray-200 dark:border-gray-700 p-3 flex gap-2">
                  <input type="text" value={replyMsg} onChange={e => setReplyMsg(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleReply(); } }}
                    placeholder="Type your reply..." className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500" />
                  <Button onClick={handleReply} disabled={sending || !replyMsg} loading={sending}>
                    <Send className="h-4 w-4" /> Send
                  </Button>
                </div>
              </div>
            </Card>
          ) : (
            <Card className="text-center py-12">
              <MessageSquare className="h-10 w-10 text-gray-200 dark:text-gray-600 mx-auto mb-3" />
              <p className="text-gray-400 dark:text-gray-500 text-sm">Select a ticket to view messages</p>
            </Card>
          )}
        </div>
      </div>

      {/* Create ticket modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-lg mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">New Support Ticket</h3>
              <button onClick={() => setShowCreate(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Subject</label>
                <input type="text" value={subject} onChange={e => setSubject(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
                  placeholder="Brief description of your issue" required />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Message</label>
                <textarea value={msg} onChange={e => setMsg(e.target.value)} rows={5}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-emerald-500"
                  placeholder="Describe your issue in detail..." required />
              </div>
              <Button type="submit" disabled={creating || !subject || !msg} className="w-full" loading={creating}>
                Submit Ticket
              </Button>
            </form>
          </div>
        </div>
      )}
    </motion.div>
  );
}

// Parent Panel Ticket Management
export function TicketManagement() {
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [selectedTicket, setSelectedTicket] = useState<number | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [msgsLoading, setMsgsLoading] = useState(false);
  const [replyMsg, setReplyMsg] = useState('');
  const [sending, setSending] = useState(false);
  const [filter, setFilter] = useState('all');
  const [confirmDelete, setConfirmDelete] = useState<number | null>(null);
  const inflightTicketId = useRef<number | null>(null);

  const loadTickets = useCallback(() => {
    setLoading(true);
    getAdminTickets({ limit: 200 }).then(d => { setTickets(ensureArray(d?.tickets || d)); }).catch((e: any) => console.error('Load admin tickets:', e)).finally(() => setLoading(false));
  }, []);

  useEffect(() => { loadTickets(); }, [loadTickets]);

  const loadMessages = async (id: number) => {
    setSelectedTicket(id);
    setMsgsLoading(true);
    inflightTicketId.current = id;
    try {
      const res = await getAdminTicketMessages(id, { limit: 200 });
      if (inflightTicketId.current === id) {
        setMessages(ensureArray(res?.messages || res));
      }
    } catch (e: any) {
      console.error('Load admin messages:', e);
      if (inflightTicketId.current === id) setMessages([]);
    }
    finally {
      if (inflightTicketId.current === id) setMsgsLoading(false);
    }
  };

  const handleReply = async () => {
    if (!replyMsg || !selectedTicket) return;
    setSending(true);
    try {
      await replyAdminTicket(selectedTicket, replyMsg);
      setReplyMsg('');
      setSuccess('Reply sent');
      loadMessages(selectedTicket);
      loadTickets();
    } catch (err: any) { setError(err?.error || 'Failed to send'); }
    finally { setSending(false); }
  };

  const handleStatus = async (id: number, status: string) => {
    try {
      await updateAdminTicketStatus(id, status);
      setSuccess(`Ticket ${status}`);
      loadTickets();
      if (selectedTicket === id) loadMessages(id);
    } catch (err: any) { setError(err?.error || 'Failed'); }
  };

  const handleDelete = async (id: number) => {
    setConfirmDelete(id);
  };

  const doDelete = async () => {
    if (!confirmDelete) return;
    try {
      await deleteAdminTicket(confirmDelete);
      setSuccess('Ticket deleted');
      if (selectedTicket === confirmDelete) setSelectedTicket(null);
      setConfirmDelete(null);
      loadTickets();
    } catch (err: any) { setError(err?.error || 'Failed'); }
  };

  const statusBadge = (s: string) => {
    const colors: Record<string, string> = { open: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/50 dark:text-yellow-300', closed: 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400', replied: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300', pending: 'bg-purple-100 text-purple-700 dark:bg-purple-900/50 dark:text-purple-300' };
    return <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${colors[s] || 'bg-gray-100 text-gray-500'}`}>{s}</span>;
  };

  const filtered = filter === 'all' ? tickets : tickets.filter(t => t.status === filter);

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Ticket Management</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{tickets.length} total tickets</p>
        </div>
        <div className="flex gap-1 bg-gray-100 dark:bg-gray-800 rounded-lg p-1">
          {['all', 'open', 'replied', 'pending', 'closed'].map(f => (
            <button key={f} onClick={() => setFilter(f)}
              className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${filter === f ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm' : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'}`}>
              {f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}
            </button>
          ))}
        </div>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          <AlertTriangle className="h-4 w-4 shrink-0" /> {error}
          <button onClick={() => setError('')} className="ml-auto text-red-400 hover:text-red-600"><X className="h-4 w-4" /></button>
        </div>
      )}
      {success && (
        <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">
          <span>{success}</span>
          <button onClick={() => setSuccess('')} className="ml-auto text-emerald-400 hover:text-emerald-600"><X className="h-4 w-4" /></button>
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Ticket list */}
        <div className="lg:col-span-1 space-y-2">
          {loading ? [1,2].map(i => (
            <Card key={i}>
              <div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-3/4 mb-2 animate-pulse" />
              <div className="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2 animate-pulse" />
            </Card>
          )) : !Array.isArray(filtered) || filtered.length === 0 ? (
            <EmptyState title="No tickets" />
          ) : filtered.map((t, i) => (
            <motion.div
              key={t.id}
              initial={{ opacity: 0, x: -10 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.03 }}
            >
              <div onClick={() => loadMessages(t.id)}
                className={`bg-white dark:bg-gray-800 rounded-xl border p-4 cursor-pointer transition-all ${selectedTicket === t.id ? 'ring-2 ring-blue-500 border-blue-500' : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'}`}>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate mr-2">{t.subject}</span>
                  {statusBadge(t.status)}
                </div>
                <div className="flex items-center gap-3 text-xs text-gray-400 dark:text-gray-500">
                  <span>{t.username || '#' + t.account_id}</span>
                  <span className="flex items-center gap-1"><MessageSquare className="h-3 w-3" />{t.message_count}</span>
                  <span className="flex items-center gap-1"><Clock className="h-3 w-3" />{new Date(t.updated_at).toLocaleDateString()}</span>
                </div>
              </div>
            </motion.div>
          ))}
        </div>

        {/* Messages */}
        <div className="lg:col-span-2">
          {selectedTicket ? (
            <Card padding={false}>
              <div className="flex flex-col h-[550px]">
                <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
                  <span className="font-medium text-sm text-gray-900 dark:text-gray-100">
                    Ticket #{selectedTicket} — {statusBadge(tickets.find(t => t.id === selectedTicket)?.status || '')}
                  </span>
                  <div className="flex items-center gap-1">
                    <Button variant="secondary" size="sm" onClick={() => handleStatus(selectedTicket, 'closed')}>Close</Button>
                    <Button variant="danger" size="sm" onClick={() => handleDelete(selectedTicket)}>Delete</Button>
                  </div>
                </div>
                <div className="flex-1 overflow-y-auto p-4 space-y-3">
                  {msgsLoading ? (
                    <div className="flex items-center justify-center h-full"><Spinner /></div>
                  ) : !Array.isArray(messages) || messages.length === 0 ? (
                    <div className="text-center py-12 text-gray-400 dark:text-gray-500 text-sm">No messages</div>
                  ) : messages.map(m => (
                    <div key={m.id} className={`flex ${m.sender_type === 'user' ? 'justify-start' : 'justify-end'}`}>
                      <div className={`max-w-[80%] rounded-lg px-4 py-2 text-sm ${m.sender_type === 'user' ? 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200' : 'bg-blue-600 text-white'}`}>
                        <div className="text-xs opacity-70 mb-1">{m.sender_name || (m.sender_type === 'user' ? 'User' : 'Staff')}</div>
                        <div className="whitespace-pre-wrap">{m.message}</div>
                        <div className="text-xs opacity-50 mt-1">{new Date(m.created_at).toLocaleString()}</div>
                      </div>
                    </div>
                  ))}
                </div>
                <div className="border-t border-gray-200 dark:border-gray-700 p-3 flex gap-2">
                  <input type="text" value={replyMsg} onChange={e => setReplyMsg(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleReply(); } }}
                    placeholder="Type your reply..." className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                  <Button onClick={handleReply} disabled={sending || !replyMsg} loading={sending}>
                    <Send className="h-4 w-4" /> Send
                  </Button>
                </div>
              </div>
            </Card>
          ) : (
            <Card className="text-center py-12">
              <MessageSquare className="h-10 w-10 text-gray-200 dark:text-gray-600 mx-auto mb-3" />
              <p className="text-gray-400 dark:text-gray-500 text-sm">Select a ticket to view and reply</p>
            </Card>
          )}
        </div>
      </div>

      {/* Delete confirmation modal */}
      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Delete Ticket</h3>
              <button onClick={() => setConfirmDelete(null)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-5">Delete this ticket and all messages? This cannot be undone.</p>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(null)}>Cancel</Button>
              <Button variant="danger" onClick={doDelete}>Delete</Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

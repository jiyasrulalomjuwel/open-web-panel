import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Mail, Inbox, Send, Trash2, AlertTriangle, Loader2, ChevronLeft, ChevronRight, Eye, EyeOff, RefreshCw, X, Paperclip, Clock, Star, Archive, FileText, Search, Plus } from 'lucide-react';
import { getEmails, readMessage, sendEmail, deleteMessage } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import EmptyState from '../components/ui/EmptyState';
import Spinner from '../components/ui/Spinner';

const BASE = '/api/v1';
async function req(method: string, path: string, body?: any): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('owp_access_token');
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(`${BASE}${path}`, { method, headers, body: body ? JSON.stringify(body) : undefined });
  if (!res.ok) { const e = await res.json().catch(() => ({ error: res.statusText })); throw e; }
  return res.json();
}

type EmailAccount = {
  id: number; email: string; domain_name: string; send_limit: number; send_used: number; send_reset_date: string;
};

type EmailMessage = {
  id: number; folder: string; from: string; to: string;
  subject: string; seen: boolean; flags: string; received_at: string;
};

type EmailDetail = {
  id: number; folder: string; from: string; to: string;
  subject: string; body_text: string; body_html: string; flags: string; received_at: string;
};

const FOLDERS = [
  { key: 'INBOX', label: 'Inbox', icon: Inbox },
  { key: 'Sent', label: 'Sent', icon: Send },
  { key: 'Trash', label: 'Trash', icon: Trash2 },
];

export function Webmail() {
  const [accounts, setAccounts] = useState<EmailAccount[]>([]);
  const [selectedAcct, setSelectedAcct] = useState<number | null>(null);
  const [folder, setFolder] = useState('INBOX');
  const [messages, setMessages] = useState<EmailMessage[]>([]);
  const [selectedMsg, setSelectedMsg] = useState<number | null>(null);
  const [msgDetail, setMsgDetail] = useState<EmailDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMsgs, setLoadingMsgs] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  // Compose
  const [showCompose, setShowCompose] = useState(false);
  const [composeTo, setComposeTo] = useState('');
  const [composeSubj, setComposeSubj] = useState('');
  const [composeBody, setComposeBody] = useState('');
  const [sending, setSending] = useState(false);

  const loadAccounts = useCallback(async () => {
    setLoading(true);
    try {
      const accts = await getEmails();
      setAccounts(accts || []);
      if (accts?.length > 0 && !selectedAcct) {
        setSelectedAcct(accts[0].id);
      }
    } catch {}
    finally { setLoading(false); }
  }, []);

  useEffect(() => { loadAccounts(); }, [loadAccounts]);

  const loadMessages = useCallback(async (acctId: number, fld: string) => {
    setLoadingMsgs(true);
    setSelectedMsg(null);
    setMsgDetail(null);
    try {
      const msgs = await req('GET', `/child/emails/${acctId}/inbox?folder=${fld}`);
      setMessages(msgs || []);
    } catch { setMessages([]); }
    finally { setLoadingMsgs(false); }
  }, []);

  useEffect(() => {
    if (selectedAcct) {
      loadMessages(selectedAcct, folder);
    }
  }, [selectedAcct, folder, loadMessages]);

  const openMessage = async (msgId: number) => {
    if (!selectedAcct) return;
    setSelectedMsg(msgId);
    try {
      const detail = await readMessage(selectedAcct, msgId);
      setMsgDetail(detail);
      // Refresh message list to update seen status
      loadMessages(selectedAcct, folder);
    } catch { setError('Failed to load message'); }
  };

  const handleDeleteMsg = async (msgId: number) => {
    if (!selectedAcct) return;
    try {
      await deleteMessage(selectedAcct, msgId);
      setSuccess('Message deleted');
      if (selectedMsg === msgId) { setSelectedMsg(null); setMsgDetail(null); }
      loadMessages(selectedAcct, folder);
    } catch (err: any) { setError(err?.error || 'Delete failed'); }
  };

  const handleSend = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedAcct || !composeTo || !composeSubj) return;
    setSending(true);
    setError('');
    try {
      await sendEmail(selectedAcct, {
        to: composeTo, subject: composeSubj, body: composeBody
      });
      setShowCompose(false);
      setComposeTo('');
      setComposeSubj('');
      setComposeBody('');
      setSuccess('Email sent!');
      loadMessages(selectedAcct, folder);
    } catch (err: any) { setError(err?.error || 'Send failed'); }
    finally { setSending(false); }
  };

  const selectedAcctData = accounts.find(a => a.id === selectedAcct);

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-4"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Webmail</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {selectedAcctData?.email || 'Select an email account'}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {accounts.length > 1 && (
            <select value={selectedAcct || ''} onChange={e => setSelectedAcct(Number(e.target.value))}
              className="px-3 py-1.5 border border-gray-200 dark:border-gray-700 rounded-lg text-xs bg-white dark:bg-gray-800 text-gray-600 dark:text-gray-400">
              {accounts.map(a => <option key={a.id} value={a.id}>{a.email}</option>)}
            </select>
          )}
          <button onClick={() => { if (selectedAcct) loadMessages(selectedAcct, folder); }}
            className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700">
            <RefreshCw className="h-4 w-4" />
          </button>
          <Button variant="primary" onClick={() => setShowCompose(true)} disabled={!selectedAcct}>
            <Plus className="h-4 w-4" /> Compose
          </Button>
        </div>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {loading ? (
        <div className="text-center py-16"><Spinner /></div>
      ) : accounts.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Mail className="h-10 w-10 text-gray-400" />}
            title="No email accounts"
            message="Create an email account first to use webmail."
          />
        </Card>
      ) : (
        <Card className="!p-0 overflow-hidden">
          <div className="flex h-[600px]">
            {/* Folders + Message List */}
            <div className="w-80 border-r border-gray-200 dark:border-gray-700 flex flex-col">
              {/* Folders */}
              <div className="flex border-b border-gray-200 dark:border-gray-700">
                {FOLDERS.map(f => (
                  <button key={f.key} onClick={() => setFolder(f.key)}
                    className={`flex-1 flex items-center justify-center gap-1.5 py-3 text-xs font-medium transition-colors ${
                      folder === f.key ? 'bg-gray-50 dark:bg-gray-700 text-gray-900 dark:text-gray-100 border-b-2 border-gray-900 dark:border-gray-100' : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                    }`}>
                    <f.icon className="h-3.5 w-3.5" /> {f.label}
                  </button>
                ))}
              </div>

              {/* Message list */}
              <div className="flex-1 overflow-y-auto">
                {loadingMsgs ? (
                  <div className="flex items-center justify-center h-32"><Spinner /></div>
                ) : messages.length === 0 ? (
                  <div className="text-center py-12 text-gray-400 dark:text-gray-500 text-sm">No messages</div>
                ) : messages.map(m => (
                  <motion.div
                    key={m.id}
                    initial={{ opacity: 0, x: -5 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.15 }}
                  >
                    <div onClick={() => openMessage(m.id)}
                      className={`px-4 py-3 border-b border-gray-100 dark:border-gray-700/50 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/30 transition-colors ${
                        selectedMsg === m.id ? 'bg-blue-50 dark:bg-blue-900/20' : ''
                      } ${!m.seen ? 'font-semibold bg-gray-50 dark:bg-gray-700/40' : ''}`}>
                      <div className="flex items-center justify-between mb-0.5">
                        <span className={`text-sm truncate ${!m.seen ? 'text-gray-900 dark:text-gray-100' : 'text-gray-600 dark:text-gray-400'}`}>
                          {m.from || m.to}
                        </span>
                        <span className="text-xs text-gray-400 dark:text-gray-500 shrink-0 ml-2">
                          {new Date(m.received_at).toLocaleDateString()}
                        </span>
                      </div>
                      <div className={`text-xs truncate ${!m.seen ? 'text-gray-700 dark:text-gray-300' : 'text-gray-500 dark:text-gray-400'}`}>
                        {m.subject || '(no subject)'}
                      </div>
                    </div>
                  </motion.div>
                ))}
              </div>
            </div>

            {/* Message viewer */}
            <div className="flex-1 flex flex-col overflow-hidden">
              {msgDetail ? (
                <div className="flex-1 overflow-y-auto">
                  <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                    <div className="flex items-start justify-between mb-2">
                      <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">{msgDetail.subject || '(no subject)'}</h2>
                      <button onClick={() => handleDeleteMsg(msgDetail.id)} className="p-1.5 text-gray-400 hover:text-red-600 rounded hover:bg-red-50 dark:hover:bg-red-900/30">
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                    <div className="text-xs text-gray-500 dark:text-gray-400 space-y-0.5">
                      <div><span className="text-gray-400 dark:text-gray-500">From:</span> {msgDetail.from}</div>
                      <div><span className="text-gray-400 dark:text-gray-500">To:</span> {msgDetail.to}</div>
                      <div><span className="text-gray-400 dark:text-gray-500">Date:</span> {new Date(msgDetail.received_at).toLocaleString()}</div>
                    </div>
                  </div>
                  <div className="px-6 py-4 whitespace-pre-wrap text-sm text-gray-700 dark:text-gray-300 leading-relaxed">
                    {msgDetail.body_text || '(no content)'}
                    {msgDetail.body_html && !msgDetail.body_text && (
                      <div dangerouslySetInnerHTML={{ __html: msgDetail.body_html }} />
                    )}
                  </div>
                </div>
              ) : (
                <div className="flex-1 flex items-center justify-center text-gray-400 dark:text-gray-500">
                  <div className="text-center">
                    <Mail className="h-12 w-12 mx-auto mb-3 text-gray-200 dark:text-gray-600" />
                    <p className="text-sm">Select a message to read</p>
                  </div>
                </div>
              )}

              {/* Send limit info */}
              {selectedAcctData && (
                <div className="px-4 py-2 bg-gray-50 dark:bg-gray-700/50 border-t border-gray-200 dark:border-gray-700 text-xs text-gray-500 dark:text-gray-400 flex items-center gap-4">
                  <span className="flex items-center gap-1"><Send className="h-3 w-3" /> Sent today: {selectedAcctData.send_used}/{selectedAcctData.send_limit}</span>
                  <span className="flex items-center gap-1"><Inbox className="h-3 w-3" /> {messages.filter(m => !m.seen).length} unread</span>
                </div>
              )}
            </div>
          </div>
        </Card>
      )}

      {/* Compose Modal */}
      {showCompose && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-12 bg-black/40" onClick={() => setShowCompose(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-lg mx-4 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Compose Email</h3>
              <button onClick={() => setShowCompose(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"><X className="h-5 w-5" /></button>
            </div>
            <form onSubmit={handleSend} className="p-5 space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">From</label>
                <input type="text" value={selectedAcctData?.email || ''} disabled
                  className="w-full px-3 py-2 border border-gray-200 dark:border-gray-600 rounded-lg text-sm bg-gray-50 dark:bg-gray-700 text-gray-500 dark:text-gray-400" />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">To</label>
                <input type="email" value={composeTo} onChange={e => setComposeTo(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="recipient@example.com" required />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Subject</label>
                <input type="text" value={composeSubj} onChange={e => setComposeSubj(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="Subject" required />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Message</label>
                <textarea value={composeBody} onChange={e => setComposeBody(e.target.value)} rows={8}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-none"
                  placeholder="Write your message..." />
              </div>
              <div className="flex items-center justify-end">
                <Button type="submit" disabled={sending || !composeTo || !composeSubj} loading={sending}>
                  <Send className="h-4 w-4" /> Send Email
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </motion.div>
  );
}

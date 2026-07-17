import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Mail, Plus, X, AlertTriangle, Loader2, Trash2, Copy, ExternalLink, RefreshCw, Eye, EyeOff, Settings, ChevronRight, Server, CheckCircle } from 'lucide-react';
import { getEmails, createEmail, updateEmail, deleteEmail, getDomains, getEmailCount } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

const BASE = '/api/v1';
async function req(method: string, path: string, body?: any): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = localStorage.getItem('owp_access_token');
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(`${BASE}${path}`, { method, headers, body: body ? JSON.stringify(body) : undefined });
  if (!res.ok) { const e = await res.json().catch(() => ({ error: res.statusText })); throw e; }
  return res.json();
}
function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type EmailAccount = {
  id: number; account_id: number; domain_id: number;
  email: string; forward_to: string; quota_mb: number;
  send_limit: number; send_used: number; send_reset_date: string;
  status: string; created_at: string; domain_name: string;
};

type DNSConfig = {
  domain: string; mx_record: string; spf_record: string; dkim_record: string; server_ip: string; mail_host: string;
};

function passwordStrength(pw: string): { label: string; color: string; score: number } {
  let score = 0;
  if (pw.length >= 8) score += 25;
  if (pw.length >= 12) score += 15;
  if (/[a-z]/.test(pw)) score += 15;
  if (/[A-Z]/.test(pw)) score += 15;
  if (/[0-9]/.test(pw)) score += 15;
  if (/[^a-zA-Z0-9]/.test(pw)) score += 15;
  if (score >= 90) return { label: 'Very Strong', color: 'bg-emerald-500', score };
  if (score >= 70) return { label: 'Strong', color: 'bg-blue-500', score };
  if (score >= 50) return { label: 'Medium', color: 'bg-yellow-500', score };
  return { label: 'Weak', color: 'bg-red-500', score };
}

export function Emails() {
  const [accounts, setAccounts] = useState<EmailAccount[]>([]);
  const [dnsConfigs, setDnsConfigs] = useState<DNSConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [showForward, setShowForward] = useState<{ id: number; current: string } | null>(null);
  const [showPassword, setShowPassword] = useState<{ id: number } | null>(null);
  const [showDNS, setShowDNS] = useState(false);

  // Create form
  const [domainId, setDomainId] = useState(0);
  const [localPart, setLocalPart] = useState('');
  const [password, setPassword] = useState('');
  const [showPw, setShowPw] = useState(false);
  const [creating, setCreating] = useState(false);
  const [domains, setDomains] = useState<any[]>([]);

  // Forward form
  const [forwardTo, setForwardTo] = useState('');
  const [savingForward, setSavingForward] = useState(false);

  // Password change form
  const [newPassword, setNewPassword] = useState('');
  const [showNewPw, setShowNewPw] = useState(false);
  const [changingPw, setChangingPw] = useState(false);
  const [deleting, setDeleting] = useState<number | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      getEmails(),
      req('GET', '/child/emails/dns'),
      getDomains(),
    ]).then(([e, d, doms]) => {
      setAccounts(e || []);
      setDnsConfigs(d || []);
      setDomains(doms || []);
    }).catch((e: any) => console.error('Load emails:', e)).finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!domainId || !localPart || !password) return;
    setCreating(true);
    setError('');
    try {
      await createEmail({ domain_id: domainId, local_part: localPart, password });
      setShowCreate(false);
      setLocalPart('');
      setPassword('');
      setDomainId(0);
      setSuccess(`Email account ${localPart}@... created`);
      load();
    } catch (err: any) { setError(err?.error || 'Failed to create email'); }
    finally { setCreating(false); }
  };

  const handleSaveForward = async () => {
    if (!showForward) return;
    setSavingForward(true);
    try {
      await updateEmail(showForward.id, { forward_to: forwardTo });
      setShowForward(null);
      setSuccess('Forwarding updated');
      load();
    } catch (err: any) { setError(err?.error || 'Failed to update'); }
    finally { setSavingForward(false); }
  };

  const handleChangePassword = async () => {
    if (!showPassword || !newPassword) return;
    setChangingPw(true);
    try {
      await updateEmail(showPassword.id, { password: newPassword });
      setShowPassword(null);
      setNewPassword('');
      setSuccess('Password changed');
    } catch (err: any) { setError(err?.error || 'Failed to change password'); }
    finally { setChangingPw(false); }
  };

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this email account? All messages will be lost.')) return;
    setDeleting(id);
    try {
      await deleteEmail(id);
      setSuccess('Email account deleted');
      load();
    } catch (err: any) { setError(err?.error || 'Failed to delete'); }
    finally { setDeleting(null); }
  };

  const genPassword = () => {
    const c = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
    let p = '';
    for (let i = 0; i < 16; i++) p += c[Math.floor(Math.random() * c.length)];
    setPassword(p);
  };

  const copyText = (text: string) => {
    navigator.clipboard.writeText(text).catch((e: any) => console.error('Copy:', e));
    setSuccess('Copied to clipboard');
  };

  const totalEmails = accounts.length;
  const totalSendsToday = accounts.reduce((sum, a) => sum + a.send_used, 0);

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      {/* Port 25 Warning */}
      <Card className="!p-4 bg-amber-50 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800">
        <div className="flex items-start gap-3">
          <AlertTriangle className="h-5 w-5 text-amber-500 dark:text-amber-400 shrink-0 mt-0.5" />
          <div>
            <h3 className="font-medium text-amber-800 dark:text-amber-200 text-sm">Email Deliverability Notice</h3>
            <p className="text-xs text-amber-700 dark:text-amber-300 mt-1 leading-relaxed">
              <strong>Incoming mail:</strong> The SMTP server is running. Make sure your domain's MX record points to
              <code className="text-amber-800 dark:text-amber-200 bg-amber-100 dark:bg-amber-900/40 px-1 rounded">mail.yourdomain.com</code> with an A record to this server's IP.
            </p>
            <p className="text-xs text-amber-700 dark:text-amber-300 mt-1 leading-relaxed">
              <strong>Outgoing mail:</strong> If your VPS provider blocks port 25, configure an SMTP relay in
              Admin Settings. Your daily sending limit is <strong>25 emails/account</strong> (configurable by admin).
            </p>
          </div>
        </div>
      </Card>

      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Email Accounts</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{totalEmails} account(s) · {totalSendsToday} sent today</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm" onClick={() => { setShowDNS(true); }}>
            <Server className="h-3.5 w-3.5" /> DNS
          </Button>
          <Button variant="primary" onClick={() => { setShowCreate(true); setError(''); }}>
            <Plus className="h-4 w-4" /> Create Email
          </Button>
        </div>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {/* Account list */}
      {loading ? (
        <div className="space-y-3">{[1,2].map(i => <Card key={i}><Skeleton lines={2} /></Card>)}</div>
      ) : accounts.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Mail className="h-10 w-10 text-gray-400" />}
            title="No email accounts yet"
            message="Create an email account to start sending and receiving emails."
            actionLabel="Create Email Account"
            onAction={() => setShowCreate(true)}
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {accounts.map((a, i) => (
            <motion.div
              key={a.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.04 }}
            >
              <Card>
                <div className="flex items-start justify-between">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <Mail className="h-5 w-5 text-blue-500 shrink-0" />
                      <h3 className="font-medium text-gray-900 dark:text-gray-100 truncate">{a.email}</h3>
                      <Badge variant={a.status === 'active' ? 'success' : 'neutral'}>{a.status}</Badge>
                    </div>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400 mt-1">
                      {a.forward_to && <span className="flex items-center gap-1"><ChevronRight className="h-3 w-3" /> Forward: {a.forward_to}</span>}
                      <span>Quota: {a.quota_mb} MB</span>
                      <span>Send: {a.send_used}/{a.send_limit} today</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0 ml-2">
                    <button onClick={() => { setShowForward({ id: a.id, current: a.forward_to }); setForwardTo(a.forward_to); }} className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30" title="Set Forwarding">
                      <Settings className="h-4 w-4" />
                    </button>
                    <button onClick={() => { setShowPassword({ id: a.id }); setNewPassword(''); }} className="p-1.5 text-gray-400 hover:text-amber-600 rounded-lg hover:bg-amber-50 dark:hover:bg-amber-900/30" title="Change Password">
                      <Eye className="h-4 w-4" />
                    </button>
                    <button onClick={() => handleDelete(a.id)} disabled={deleting === a.id}
                      className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 disabled:opacity-50">
                      {deleting === a.id ? <Spinner /> : <Trash2 className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}

      {/* Create Email Modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Create Email Account</h3>
              <button onClick={() => setShowCreate(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"><X className="h-5 w-5" /></button>
            </div>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Domain</label>
                <select value={domainId} onChange={e => setDomainId(Number(e.target.value))} required
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                  <option value={0}>Select domain...</option>
                  {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
                </select>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Email Address</label>
                <div className="flex items-center gap-2">
                  <input type="text" value={localPart} onChange={e => setLocalPart(e.target.value.replace(/[^a-zA-Z0-9._\-]/g, '').toLowerCase())}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                    placeholder="username" required />
                  <span className="text-sm text-gray-400 dark:text-gray-500">@{domains.find(d => d.id === domainId)?.domain || 'domain'}</span>
                </div>
              </div>
              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">Password</label>
                  <button type="button" onClick={genPassword} className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium">Generate</button>
                </div>
                <div className="relative">
                  <input type={showPw ? 'text' : 'password'} value={password} onChange={e => setPassword(e.target.value)}
                    className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                    placeholder="Enter password" required />
                  <button type="button" onClick={() => setShowPw(!showPw)} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
                    {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
                {password && (
                  <div className="mt-1.5">
                    <div className="h-1.5 bg-gray-200 dark:bg-gray-600 rounded-full overflow-hidden">
                      <div className={`h-full ${passwordStrength(password).color}`} style={{ width: `${passwordStrength(password).score}%` }} />
                    </div>
                    <span className={`text-xs font-medium mt-0.5 ${passwordStrength(password).score >= 70 ? 'text-emerald-600 dark:text-emerald-400' : passwordStrength(password).score >= 50 ? 'text-yellow-600 dark:text-yellow-400' : 'text-red-600 dark:text-red-400'}`}>
                      {passwordStrength(password).label}
                    </span>
                  </div>
                )}
              </div>
              <Button type="submit" disabled={creating || !domainId || !localPart || !password} loading={creating} className="w-full">
                Create Account
              </Button>
            </form>
          </div>
        </div>
      )}

      {/* Forwarding Modal */}
      {showForward && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Email Forwarding</h3>
              <button onClick={() => setShowForward(null)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Forward to Email</label>
                <input type="email" value={forwardTo} onChange={e => setForwardTo(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100" placeholder="user@anotherdomain.com" />
                <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">Leave empty to disable forwarding</p>
              </div>
              <div className="flex items-center justify-end gap-2">
                <Button variant="ghost" onClick={() => setShowForward(null)}>Cancel</Button>
                <Button variant="primary" onClick={handleSaveForward} disabled={savingForward} loading={savingForward}>
                  Save
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Change Password Modal */}
      {showPassword && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Change Password</h3>
              <button onClick={() => setShowPassword(null)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <div className="space-y-4">
              <div className="relative">
                <input type={showNewPw ? 'text' : 'password'} value={newPassword} onChange={e => setNewPassword(e.target.value)}
                  className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100" placeholder="New password" />
                <button type="button" onClick={() => setShowNewPw(!showNewPw)} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
                  {showNewPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
              <div className="flex items-center justify-end gap-2">
                <Button variant="ghost" onClick={() => setShowPassword(null)}>Cancel</Button>
                <Button variant="primary" onClick={handleChangePassword} disabled={changingPw || !newPassword} loading={changingPw}>
                  Change
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* DNS Modal */}
      {showDNS && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-12 bg-black/40 overflow-y-auto">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-lg mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <div>
                <h3 className="font-semibold text-gray-900 dark:text-gray-100">DNS Configuration</h3>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Add these DNS records to your domain for email delivery</p>
              </div>
              <button onClick={() => setShowDNS(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"><X className="h-5 w-5" /></button>
            </div>
            {dnsConfigs.length === 0 ? (
              <p className="text-sm text-gray-400 dark:text-gray-500 text-center py-8">No domains found. Add a domain first.</p>
            ) : (
              <div className="space-y-4">
                {dnsConfigs.map(cfg => (
                  <div key={cfg.domain} className="border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                    <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">{cfg.domain}</h4>
                    <div className="space-y-3">
                      <div>
                        <label className="text-xs font-medium text-gray-500 dark:text-gray-400">A Record (Mail Server)</label>
                        <div className="flex items-center gap-2 mt-0.5">
                          <code className="flex-1 px-2 py-1.5 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded text-xs font-mono text-gray-700 dark:text-gray-300">{cfg.mail_host}</code>
                          <span className="text-xs text-gray-400 dark:text-gray-500">→</span>
                          <code className="px-2 py-1.5 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded text-xs font-mono text-gray-700 dark:text-gray-300">{cfg.server_ip}</code>
                          <button onClick={() => copyText(`${cfg.mail_host} A ${cfg.server_ip}`)} className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"><Copy className="h-3.5 w-3.5" /></button>
                        </div>
                        <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">Type: A | Points mail.{cfg.domain} to your server IP</p>
                      </div>
                      <div>
                        <label className="text-xs font-medium text-gray-500 dark:text-gray-400">MX Record</label>
                        <div className="flex items-center gap-2 mt-0.5">
                          <code className="flex-1 px-2 py-1.5 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded text-xs font-mono text-gray-700 dark:text-gray-300">{cfg.mx_record}</code>
                          <button onClick={() => copyText(cfg.mx_record)} className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"><Copy className="h-3.5 w-3.5" /></button>
                        </div>
                        <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">Type: MX | Priority: 10 | Points to: mail.{cfg.domain}</p>
                      </div>
                      <div>
                        <label className="text-xs font-medium text-gray-500 dark:text-gray-400">SPF Record</label>
                        <div className="flex items-center gap-2 mt-0.5">
                          <code className="flex-1 px-2 py-1.5 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded text-xs font-mono text-gray-700 dark:text-gray-300">{cfg.spf_record}</code>
                          <button onClick={() => copyText(cfg.spf_record)} className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"><Copy className="h-3.5 w-3.5" /></button>
                        </div>
                        <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">Type: TXT | Authorizes your server to send emails</p>
                      </div>
                      <div>
                        <label className="text-xs font-medium text-gray-500 dark:text-gray-400">DKIM Record</label>
                        <div className="flex items-center gap-2 mt-0.5">
                          <code className="flex-1 px-2 py-1.5 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded text-xs font-mono text-gray-500 dark:text-gray-400">{cfg.dkim_record}</code>
                        </div>
                        <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">Enhances email security and deliverability</p>
                      </div>
                    </div>
                  </div>
                ))}
                <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                  <p className="text-xs text-blue-700 dark:text-blue-300">
                    <strong>Required DNS records for mail:</strong> (1) <strong>A record</strong> for <code className="text-blue-800 dark:text-blue-200 bg-blue-100 dark:bg-blue-900/40 px-1 rounded">mail.yourdomain.com</code> pointing to your server IP,
                    (2) <strong>MX record</strong> pointing your domain to <code className="text-blue-800 dark:text-blue-200 bg-blue-100 dark:bg-blue-900/40 px-1 rounded">mail.yourdomain.com</code> (priority 10),
                    (3) <strong>SPF TXT record</strong> authorizing your server to send. DNS changes may take up to 24-48 hours to propagate.
                  </p>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </motion.div>
  );
}

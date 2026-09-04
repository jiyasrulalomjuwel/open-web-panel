import { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { ArrowLeft, Ban, CheckCircle, Trash2, ExternalLink, Loader2, X, AlertTriangle, Server, Globe, Mail, HardDrive, MemoryStick, TrendingUp, Database, KeyRound, Shield } from 'lucide-react';
import { getAccount, getAccountResources, getAccountActivity, getAccountResourceLimits, suspendAccount, unsuspendAccount, terminateAccount, purgeAccount, loginAsChild, setTokensFor } from '../lib/api';
import { LimitsForm, PackageSection, EditInfoForm, PasswordForm } from '../components/AccountForms';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';

const statusColors: Record<string, string> = {
  active: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  suspended: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  pending: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
  terminated: 'bg-red-100 text-red-700 dark:bg-red-900/50 dark:text-red-300',
};

const TABS = [
  { key: 'overview', label: 'Overview' },
  { key: 'limits', label: 'Limits & Access' },
  { key: 'package', label: 'Package' },
  { key: 'security', label: 'Login & Security' },
  { key: 'resources', label: 'Resources' },
] as const;

function UsageBar({ used, limit, unit }: { used: number; limit: number; unit: string }) {
  const pct = !limit ? 0 : Math.min(100, Math.round((used / limit) * 100));
  return (
    <div>
      <div className="flex justify-between text-xs text-gray-500 dark:text-gray-400 mb-1">
        <span>{used} {unit} used</span>
        <span>{!limit ? '∞' : `${limit} ${unit}`}</span>
      </div>
      <div className="h-2 rounded-full bg-gray-100 dark:bg-gray-700 overflow-hidden">
        <div className={`h-full rounded-full ${pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-blue-500'}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

function ResourceTable({ title, rows, columns }: { title: string; rows: any[]; columns: Array<{ key: string; label: string }> }) {
  return (
    <div>
      <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">{title} <span className="text-gray-400 font-normal">({rows.length})</span></h3>
      {rows.length === 0 ? (
        <p className="text-xs text-gray-400 dark:text-gray-500 mb-4">None</p>
      ) : (
        <div className="overflow-x-auto mb-4 border border-gray-200 dark:border-gray-700 rounded-lg">
          <table className="w-full text-xs">
            <thead>
              <tr className="bg-gray-50 dark:bg-gray-800 text-left text-gray-500 dark:text-gray-400">
                {columns.map((c) => <th key={c.key} className="px-3 py-2 font-medium">{c.label}</th>)}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, i) => (
                <tr key={i} className="border-t border-gray-100 dark:border-gray-700/50 text-gray-700 dark:text-gray-300">
                  {columns.map((c) => <td key={c.key} className="px-3 py-2 truncate max-w-[220px]">{String(r[c.key] ?? '')}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function AccountDetails() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const accountId = Number(id);
  const [account, setAccount] = useState<any | null>(null);
  const [limits, setLimits] = useState<any | null>(null);
  const [resources, setResources] = useState<any | null>(null);
  const [activity, setActivity] = useState<any[]>([]);
  const [tab, setTab] = useState<string>('overview');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [actionError, setActionError] = useState('');
  const [acting, setActing] = useState(false);
  const [loginAsLoading, setLoginAsLoading] = useState(false);
  const [showSuspend, setShowSuspend] = useState(false);
  const [suspendReason, setSuspendReason] = useState('');

  const readOnly = account?.status === 'terminated';

  const load = useCallback(() => {
    if (!accountId) return;
    setLoading(true);
    setError('');
    Promise.all([
      getAccount(accountId),
      getAccountResourceLimits(accountId).catch(() => null),
      getAccountResources(accountId).catch(() => null),
      getAccountActivity(accountId).catch(() => []),
    ]).then(([a, lim, res, act]) => {
      setAccount(a);
      setLimits(lim);
      setResources(res);
      setActivity(Array.isArray(act) ? act : []);
    }).catch((e: any) => setError(e?.message || e?.error || 'Failed to load account'))
      .finally(() => setLoading(false));
  }, [accountId]);

  useEffect(() => { load(); }, [load]);

  const doSuspend = async () => {
    setActing(true);
    setActionError('');
    try {
      await suspendAccount(accountId, suspendReason || undefined);
      setShowSuspend(false);
      setSuspendReason('');
      load();
    } catch (err: any) {
      setActionError(err?.message || err?.error || 'Failed to suspend account');
    } finally {
      setActing(false);
    }
  };

  const doUnsuspend = async () => {
    setActing(true);
    setActionError('');
    try {
      await unsuspendAccount(accountId);
      load();
    } catch (err: any) {
      setActionError(err?.message || err?.error || 'Failed to unsuspend account');
    } finally {
      setActing(false);
    }
  };

  const doTerminate = async () => {
    if (!confirm(`Terminate account "${account?.username}"? This action cannot be undone.`)) return;
    setActing(true);
    setActionError('');
    try {
      await terminateAccount(accountId);
      load();
    } catch (err: any) {
      setActionError(err?.message || err?.error || 'Failed to terminate account');
    } finally {
      setActing(false);
    }
  };

  const [showDelete, setShowDelete] = useState(false);
  const doPurge = async () => {
    setActing(true);
    setActionError('');
    try {
      await purgeAccount(accountId);
      navigate('/accounts');
    } catch (err: any) {
      setActionError(err?.message || err?.error || 'Failed to delete account');
      setShowDelete(false);
    } finally {
      setActing(false);
    }
  };

  const doLoginAs = async () => {
    setLoginAsLoading(true);
    setActionError('');
    try {
      const tokens = await loginAsChild(accountId);
      setTokensFor('owp_child_', tokens);
      const port = window.location.port;
      let childPort = '2082';
      if (port === '9000') childPort = '9001';
      else if (port === '2086') childPort = '2082';
      else if (port === '9001') childPort = '9001';
      else if (port === '2082') childPort = '2082';
      window.open(`${window.location.protocol}//${window.location.hostname}:${childPort}/`, '_blank');
    } catch (err: any) {
      setActionError(err?.message || err?.error || 'Failed to open the Site Panel');
    } finally {
      setLoginAsLoading(false);
    }
  };

  if (loading) {
    return <div className="flex items-center justify-center py-16 text-gray-400"><Loader2 className="h-5 w-5 animate-spin mr-2" /> Loading account...</div>;
  }
  if (error || !account) {
    return (
      <div className="space-y-4">
        <Link to="/accounts" className="inline-flex items-center gap-1.5 text-sm text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"><ArrowLeft className="h-4 w-4" /> Back to Accounts</Link>
        <div className="p-4 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          {error || 'Account not found'}
          <button onClick={() => navigate('/accounts')} className="ml-3 underline">Back to list</button>
        </div>
      </div>
    );
  }

  const eff = limits?.effective || {};
  const features = eff.features || {};

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <Link to="/accounts" className="inline-flex items-center gap-1.5 text-sm text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"><ArrowLeft className="h-4 w-4" /> Back to Accounts</Link>

      {/* Header */}
      <Card>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="w-11 h-11 rounded-xl bg-gradient-to-br from-[#2563EB] to-[#3B82F6] flex items-center justify-center text-white font-bold">
              {account.username?.[0]?.toUpperCase() || 'U'}
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">{account.username}</h1>
                <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${statusColors[account.status] || 'bg-gray-100 text-gray-600'}`}>{account.status}</span>
              </div>
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">{account.email} · {account.domain} · {account.package_name}</p>
            </div>
          </div>
          {!readOnly && (
            <div className="flex items-center gap-2 flex-wrap">
              {account.status === 'active' && (
                <Button variant="secondary" size="sm" onClick={() => setShowSuspend(true)} disabled={acting}>
                  <Ban className="h-3.5 w-3.5" /> Suspend
                </Button>
              )}
              {account.status === 'suspended' && (
                <Button variant="secondary" size="sm" onClick={doUnsuspend} disabled={acting}>
                  <CheckCircle className="h-3.5 w-3.5" /> Unsuspend
                </Button>
              )}
              {account.status === 'active' && (
                <Button variant="secondary" size="sm" onClick={doLoginAs} disabled={loginAsLoading}>
                  {loginAsLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ExternalLink className="h-3.5 w-3.5" />} Site Panel
                </Button>
              )}
              <Button variant="danger" size="sm" onClick={doTerminate} disabled={acting}>
                <Trash2 className="h-3.5 w-3.5" /> Terminate
              </Button>
              <Button variant="danger" size="sm" onClick={() => setShowDelete(true)} disabled={acting}>
                <X className="h-3.5 w-3.5" /> Delete
              </Button>
            </div>
          )}
          {readOnly && (
            <div className="flex items-center gap-2 flex-wrap">
              <Button variant="danger" size="sm" onClick={() => setShowDelete(true)} disabled={acting}>
                <X className="h-3.5 w-3.5" /> Delete permanently
              </Button>
            </div>
          )}
        </div>
        {account.status === 'suspended' && account.suspended_reason && (
          <div className="mt-4 flex items-center gap-2 p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-sm text-amber-700 dark:text-amber-300">
            <AlertTriangle className="h-4 w-4 shrink-0" /> Suspended: {account.suspended_reason}
          </div>
        )}
        {account.status === 'suspended' && account.purge_in_days >= 0 && (
          <div className="mt-4 flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
            <Trash2 className="h-4 w-4 shrink-0" />
            {account.purge_in_days === 0
              ? 'This account will be permanently deleted very soon. Unsuspend it to keep it.'
              : `This account will be permanently deleted in ${account.purge_in_days} day${account.purge_in_days === 1 ? '' : 's'}${account.purge_at ? ` (on ${account.purge_at} UTC)` : ''}. Unsuspend it to keep it.`}
          </div>
        )}
        {readOnly && (
          <div className="mt-4 p-3 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg text-sm text-gray-500 dark:text-gray-400">
            This account is terminated — history below is read-only.
          </div>
        )}
        {actionError && (
          <div className="mt-4 flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
            <AlertTriangle className="h-4 w-4" />{actionError}
            <button onClick={() => setActionError('')} className="ml-auto"><X className="h-4 w-4" /></button>
          </div>
        )}
      </Card>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700 overflow-x-auto">
        {TABS.map((t) => (
          <button key={t.key} onClick={() => setTab(t.key)}
            className={`px-4 py-2.5 text-sm font-medium whitespace-nowrap transition-colors ${
              tab === t.key
                ? 'text-gray-900 dark:text-gray-100 border-b-2 border-gray-900 dark:border-gray-100'
                : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
            }`}>
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'overview' && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Account Info</h3>
            <div className="space-y-2 text-sm">
              {[
                [Server, 'Username', account.username],
                [Mail, 'Email', account.email],
                [Globe, 'Domain', account.domain],
                [Server, 'Home', account.home_dir],
                [Globe, 'IP', account.ip_address || '—'],
                [Database, 'Package', account.package_name],
                [Shield, 'Created', (account.created_at || '').slice(0, 19).replace('T', ' ')],
              ].map(([Icon, label, value]: any, i: number) => (
                <div key={i} className="flex items-center gap-2 text-gray-600 dark:text-gray-400">
                  <Icon className="h-3.5 w-3.5 text-gray-400 shrink-0" />
                  <span className="w-20 text-gray-400">{label}</span>
                  <span className="text-gray-900 dark:text-gray-100 truncate">{String(value ?? '')}</span>
                </div>
              ))}
            </div>
          </Card>
          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Usage</h3>
            <div className="space-y-4">
              <div className="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400"><HardDrive className="h-3.5 w-3.5" /> Disk</div>
              <UsageBar used={account.disk_used_mb || 0} limit={eff.disk_mb || 0} unit="MB" />
              <div className="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400"><MemoryStick className="h-3.5 w-3.5" /> RAM</div>
              <UsageBar used={account.ram_used_mb || 0} limit={eff.ram_limit_mb || 0} unit="MB" />
              <div className="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400"><TrendingUp className="h-3.5 w-3.5" /> Bandwidth</div>
              <UsageBar used={Math.round(account.bandwidth_used_mb || 0)} limit={eff.bandwidth_mb || 0} unit="MB" />
            </div>
          </Card>
          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Feature Access</h3>
            <div className="flex flex-wrap gap-1.5">
              {['files', 'emails', 'ftp', 'db', 'backups', 'cron'].map((f) => (
                <Badge key={f} variant={features[f] === false ? 'neutral' : 'success'}>{f}{features[f] === false ? ': blocked' : ''}</Badge>
              ))}
            </div>
          </Card>
          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Counts</h3>
            <div className="grid grid-cols-2 gap-2 text-sm text-gray-600 dark:text-gray-400">
              <span>Databases: <strong className="text-gray-900 dark:text-gray-100">{eff.max_databases ?? eff.max_db ?? '—'}</strong></span>
              <span>Emails: <strong className="text-gray-900 dark:text-gray-100">{eff.max_email ?? '—'}</strong></span>
              <span>FTP: <strong className="text-gray-900 dark:text-gray-100">{eff.max_ftp ?? '—'}</strong></span>
              <span>Domains: <strong className="text-gray-900 dark:text-gray-100">{eff.max_domains ?? '—'}</strong></span>
            </div>
          </Card>
        </div>
      )}

      {tab === 'limits' && (
        <Card>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-4">Limits & Access</h3>
          <LimitsForm accountId={accountId} readOnly={readOnly} onSaved={load} />
        </Card>
      )}

      {tab === 'package' && (
        <Card>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-1">Hosting Package</h3>
          <p className="text-xs text-gray-400 dark:text-gray-500 mb-4">Current: <strong>{account.package_name}</strong></p>
          <PackageSection account={account} readOnly={readOnly} onChanged={load} />
        </Card>
      )}

      {tab === 'security' && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-4 flex items-center gap-2"><Mail className="h-4 w-4" /> Contact Info</h3>
            <EditInfoForm account={account} readOnly={readOnly} onSaved={load} />
          </Card>
          {!readOnly && (
            <Card>
              <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-4 flex items-center gap-2"><KeyRound className="h-4 w-4" /> Reset Password</h3>
              <PasswordForm accountId={accountId} />
            </Card>
          )}
          <Card className="lg:col-span-2">
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Recent Activity</h3>
            {activity.length === 0 ? (
              <p className="text-xs text-gray-400 dark:text-gray-500">No recorded admin actions for this account yet.</p>
            ) : (
              <div className="space-y-2 max-h-80 overflow-y-auto">
                {activity.map((a: any) => (
                  <div key={a.id} className="flex items-start gap-3 text-xs border-b border-gray-100 dark:border-gray-700/50 pb-2">
                    <Badge variant="info">{a.action}</Badge>
                    <span className="text-gray-500 dark:text-gray-400">by {a.actor_type} #{a.actor_id} · {a.ip_address}</span>
                    <span className="ml-auto text-gray-400 shrink-0">{String(a.created_at || '').slice(0, 19)}</span>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
      )}

      {tab === 'resources' && (
        <Card>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-4">Resources</h3>
          {!resources ? (
            <p className="text-xs text-gray-400 dark:text-gray-500">No resource data.</p>
          ) : (
            <div>
              <ResourceTable title="Domains" rows={resources.domains || []} columns={[{ key: 'domain', label: 'Domain' }, { key: 'type', label: 'Type' }, { key: 'doc_root', label: 'Doc Root' }]} />
              <ResourceTable title="Databases" rows={resources.databases || []} columns={[{ key: 'db_name', label: 'Database' }, { key: 'db_user', label: 'User' }, { key: 'host', label: 'Host' }]} />
              <ResourceTable title="Database Users" rows={resources.db_users || []} columns={[{ key: 'username', label: 'Username' }, { key: 'created_at', label: 'Created' }]} />
              <ResourceTable title="Email Accounts" rows={resources.emails || []} columns={[{ key: 'email', label: 'Email' }, { key: 'status', label: 'Status' }, { key: 'forward_to', label: 'Forward' }]} />
              <ResourceTable title="FTP Accounts" rows={resources.ftp || []} columns={[{ key: 'username', label: 'Username' }, { key: 'domain', label: 'Domain' }, { key: 'directory', label: 'Directory' }, { key: 'status', label: 'Status' }]} />
              <p className="text-xs text-gray-500 dark:text-gray-400">Cron jobs: <strong>{resources.cron_total ?? 0}</strong> · Backups: <strong>{resources.backup_total ?? 0}</strong> · Schedules: <strong>{resources.schedule_total ?? 0}</strong></p>
            </div>
          )}
        </Card>
      )}

      {/* Suspend with reason */}
      {showSuspend && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl">
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Suspend {account.username}?</h3>
            <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">The user loses panel access immediately (active sessions are revoked by the access gate).</p>
            <input type="text" value={suspendReason} onChange={(e) => setSuspendReason(e.target.value)} placeholder="Reason (shown to admins)"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 mb-4" />
            <div className="flex gap-3">
              <Button variant="danger" onClick={doSuspend} disabled={acting} loading={acting} className="flex-1">Suspend</Button>
              <Button variant="secondary" onClick={() => setShowSuspend(false)} className="flex-1">Cancel</Button>
            </div>
          </div>
        </div>
      )}

      {showDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl">
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Delete {account.username} permanently?</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">This destroys <strong>everything</strong> owned by this account and cannot be undone:</p>
            <ul className="text-xs text-gray-500 dark:text-gray-400 mb-4 space-y-1 list-disc pl-5">
              <li>Website files on disk</li>
              <li>MySQL databases and users</li>
              <li>Email accounts and messages</li>
              <li>FTP accounts, cron jobs, backups</li>
              <li>Domains, SSL certificates, redirects, error pages</li>
              <li>Tickets, tokens, stats and settings</li>
            </ul>
            <div className="flex gap-3">
              <Button variant="danger" onClick={doPurge} disabled={acting} loading={acting} className="flex-1">Delete everything</Button>
              <Button variant="secondary" onClick={() => setShowDelete(false)} className="flex-1">Cancel</Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

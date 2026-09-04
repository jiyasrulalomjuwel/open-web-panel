import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Plus, Trash2, X, Loader2, KeyRound, AlertTriangle, ToggleLeft, ToggleRight } from 'lucide-react';
import { getAdminTokens, createAdminToken, toggleAdminToken, deleteAdminToken } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import CopyButton from '../components/ui/CopyButton';

function CodeBlock({ code }: { code: string }) {
  return (
    <div className="relative bg-gray-900 dark:bg-black rounded-lg p-4 overflow-x-auto">
      <pre className="text-xs text-gray-100 whitespace-pre">{code}</pre>
      <div className="absolute top-2 right-2 bg-gray-800 rounded px-1">
        <CopyButton text={code} />
      </div>
    </div>
  );
}

export function ApiTokens() {
  const [tab, setTab] = useState<'tokens' | 'guide'>('tokens');
  const [tokens, setTokens] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState('');
  const [expiry, setExpiry] = useState('90');
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');
  const [newSecret, setNewSecret] = useState<string | null>(null);
  const [acting, setActing] = useState<number | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    getAdminTokens().then((d) => setTokens(d || [])).catch(() => {}).finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setCreateError('');
    try {
      const res = await createAdminToken({ name, expires_in_days: Number(expiry) || 0 });
      setNewSecret(res.token);
      setName('');
      setShowCreate(false);
      load();
    } catch (err: any) {
      setCreateError(err?.message || err?.error || 'Failed to create token');
    } finally {
      setCreating(false);
    }
  };

  const handleToggle = async (t: any) => {
    setActing(t.id);
    setError('');
    try {
      await toggleAdminToken(t.id, !t.enabled);
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to update token');
    } finally {
      setActing(null);
    }
  };

  const handleDelete = async (t: any) => {
    if (!confirm(`Revoke token "${t.name}" (${t.token_prefix})? Integrations using it will stop working immediately.`)) return;
    setActing(t.id);
    setError('');
    try {
      await deleteAdminToken(t.id);
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to revoke token');
    } finally {
      setActing(null);
    }
  };

  const host = typeof window !== 'undefined' ? window.location.hostname : 'your-server';
  const adminBase = `http://${host}:2086/api/v1`;
  const childBase = `http://${host}:2082/api/v1`;

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">API Tokens</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Long-lived keys for your own software — create accounts, suspend users and more without the UI.
        </p>
      </div>

      <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700">
        {([['tokens', 'Tokens'], ['guide', 'Integration Guide']] as const).map(([key, label]) => (
          <button key={key} onClick={() => setTab(key)}
            className={`px-4 py-2.5 text-sm font-medium transition-colors ${
              tab === key
                ? 'text-gray-900 dark:text-gray-100 border-b-2 border-gray-900 dark:border-gray-100'
                : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
            }`}>
            {label}
          </button>
        ))}
      </div>

      {tab === 'tokens' && (
        <div className="space-y-4">
          <div className="flex justify-end">
            <Button onClick={() => { setShowCreate(true); setNewSecret(null); }}>
              <Plus className="h-4 w-4" /> New Token
            </Button>
          </div>

          {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

          {newSecret && (
            <Card className="!border-emerald-300 dark:!border-emerald-700">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-1">Token created — copy it now</h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">This secret is shown exactly once. Only its hash is stored.</p>
              <div className="flex items-center gap-3 bg-gray-50 dark:bg-gray-800 rounded-lg px-3 py-2.5">
                <code className="text-xs text-gray-900 dark:text-gray-100 break-all flex-1">{newSecret}</code>
                <CopyButton text={newSecret} label="Copy token" />
              </div>
              <div className="mt-3">
                <Button variant="secondary" size="sm" onClick={() => setNewSecret(null)}>Done</Button>
              </div>
            </Card>
          )}

          {loading ? (
            <div className="flex items-center justify-center py-12 text-gray-400"><Loader2 className="h-5 w-5 animate-spin mr-2" /> Loading tokens...</div>
          ) : tokens.length === 0 ? (
            <Card>
              <EmptyState
                icon={<KeyRound className="h-10 w-10 text-gray-400" />}
                title="No API tokens yet"
                message="Create your first token to integrate your own software with OpenWebPanel."
                actionLabel="Create Token"
                onAction={() => setShowCreate(true)}
              />
            </Card>
          ) : (
            <div className="space-y-3">
              {tokens.map((t) => (
                <Card key={t.id}>
                  <div className="flex flex-wrap items-center gap-3">
                    <KeyRound className="h-5 w-5 text-gray-400 shrink-0" />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-gray-900 dark:text-gray-100 truncate">{t.name}</span>
                        <Badge variant={t.enabled ? 'success' : 'neutral'}>{t.enabled ? 'active' : 'disabled'}</Badge>
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                        <code>{t.token_prefix}</code> · owner {t.owner || '—'} · last used {t.last_used_at || 'never'} · expires {t.expires_at || 'never'}
                      </div>
                    </div>
                    <button onClick={() => handleToggle(t)} disabled={acting === t.id} title={t.enabled ? 'Disable' : 'Enable'}
                      className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30 disabled:opacity-50">
                      {t.enabled ? <ToggleRight className="h-5 w-5" /> : <ToggleLeft className="h-5 w-5" />}
                    </button>
                    <button onClick={() => handleDelete(t)} disabled={acting === t.id} title="Revoke"
                      className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 disabled:opacity-50">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </Card>
              ))}
            </div>
          )}

          {showCreate && (
            <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
              <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl">
                <div className="flex items-center justify-between mb-5">
                  <h3 className="font-semibold text-gray-900 dark:text-gray-100">New API Token</h3>
                  <button onClick={() => setShowCreate(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
                </div>
                {createError && <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{createError}</div>}
                <form onSubmit={handleCreate} className="space-y-4">
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Name</label>
                    <input type="text" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. billing-integration"
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" required maxLength={64} />
                    <p className="text-xs text-gray-400 mt-1">Admin tokens inherit your full admin access.</p>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Expires after (days, 0 = never)</label>
                    <input type="number" min={0} max={3650} value={expiry} onChange={(e) => setExpiry(e.target.value)}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                  </div>
                  <Button type="submit" disabled={creating} loading={creating} className="w-full">Create Token</Button>
                </form>
              </div>
            </div>
          )}
        </div>
      )}

      {tab === 'guide' && (
        <div className="space-y-4">
          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">1 · Base URLs & auth</h3>
            <div className="text-xs text-gray-600 dark:text-gray-400 space-y-1 mb-3">
              <p>Admin API (accounts, packages, tokens): <code>{adminBase}</code></p>
              <p>Child API (files, databases, emails, …): <code>{childBase}</code></p>
              <p>Send the token on every request: <code>Authorization: Bearer owp_…</code> — it replaces the login JWT.</p>
            </div>
            <CodeBlock code={`curl -s ${adminBase}/accounts \\\n  -H "Authorization: Bearer $OWP_TOKEN" | head -c 300`} />
          </Card>

          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">2 · Create an account</h3>
            <CodeBlock code={`curl -s -X POST ${adminBase}/accounts \\\n  -H "Authorization: Bearer $OWP_TOKEN" -H 'Content-Type: application/json' \\\n  -d '{"username":"client1","domain":"client1.com","email":"owner@client1.com","password":"ChangeMe123!","package_id":1}'`} />
          </Card>

          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">3 · Suspend / unsuspend / limits</h3>
            <CodeBlock code={`# Suspend (reason is optional)\ncurl -s -X POST ${adminBase}/accounts/1/suspend \\\n  -H "Authorization: Bearer $OWP_TOKEN" -H 'Content-Type: application/json' \\\n  -d '{"reason":"overdue invoice"}'\n\n# Unsuspend\ncurl -s -X POST ${adminBase}/accounts/1/unsuspend \\\n  -H "Authorization: Bearer $OWP_TOKEN"\n\n# Block emails + files for one user (-1 = package default, 0 = blocked, 1 = allowed)\ncurl -s -X PUT ${adminBase}/accounts/1/resource-limits \\\n  -H "Authorization: Bearer $OWP_TOKEN" -H 'Content-Type: application/json' \\\n  -d '{"cpu_limit":0,"disk_mb":0,"bandwidth_mb":0,"ram_limit_mb":0,"max_db":0,"max_email":0,"max_ftp":0,"max_domains":0,"max_subdomains":0,"ssh_access":-1,"upload_limit_mb":0,"feature_emails":0,"feature_files":0}'`} />
          </Card>

          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">4 · Read a user&apos;s files (account token)</h3>
            <div className="text-xs text-gray-600 dark:text-gray-400 mb-3">
              Account tokens are scoped to one hosting account and obey the same feature gates as the panel. Create them from the user&apos;s Site Panel; use the admin API above for admin work.
            </div>
            <CodeBlock code={`curl -s "${childBase}/child/files/list?path=/" \\\n  -H "Authorization: Bearer $OWP_USER_TOKEN"`} />
          </Card>

          <Card>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-2">5 · Safety rules</h3>
            <ul className="text-xs text-gray-600 dark:text-gray-400 space-y-1.5 list-disc pl-5">
              <li>Secrets are shown <strong>once</strong> at creation — only a hash is stored. Lost it? Revoke and create a new one.</li>
              <li>Set an expiry (e.g. 90 days) and rotate: create replacement → deploy → revoke the old one.</li>
              <li>Disable instead of delete to pause an integration without losing its prefix/last-used history.</li>
              <li>Suspended users stay blocked for token calls too; disabled features return <code>403</code>.</li>
              <li>Bad/expired/disabled token → <code>401</code>. Wrong panel (admin token on child API) → <code>403 invalid scope</code>.</li>
            </ul>
          </Card>
        </div>
      )}
    </motion.div>
  );
}

import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { getAccounts, getAccountResourceLimits, setAccountResourceLimits } from '../lib/api';
import { Search, RefreshCw, X, AlertTriangle, Loader2, Check, Ban, Minus, RotateCcw } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Pagination from '../components/ui/Pagination';

const FEATURES: Array<{ key: string; label: string }> = [
  { key: 'files', label: 'Files' },
  { key: 'emails', label: 'Emails' },
  { key: 'ftp', label: 'FTP' },
  { key: 'db', label: 'Databases' },
  { key: 'backups', label: 'Backups' },
  { key: 'cron', label: 'Cron' },
];

// Tri-state: -1 inherit package, 1 allowed, 0 blocked. Click cycles allow -> blocked -> inherit.
function nextState(v: number) {
  if (v === 1) return 0;
  if (v === 0) return -1;
  return 1;
}

type RowState = {
  account: any;
  overrides: Record<string, number>;
  effective: Record<string, boolean>;
  pkg: Record<string, boolean>;
  dirty: boolean;
  saving: boolean;
};

export function AccessControl() {
  const [rows, setRows] = useState<RowState[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [error, setError] = useState('');
  const [page, setPage] = useState(1);
  const limit = 10;

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const accs = (await getAccounts(statusFilter || undefined)) || [];
      const settled = await Promise.all(
        accs.map(async (a: any) => {
          try {
            const lim = await getAccountResourceLimits(a.id);
            const ov = lim?.overrides || {};
            const eff = lim?.effective?.features || {};
            const pkg = lim?.package || {};
            const tri = (v: any) => (v === 0 || v === 1 ? v : -1);
            return {
              account: a,
              overrides: {
                files: tri(ov.feature_files),
                emails: tri(ov.feature_emails),
                ftp: tri(ov.feature_ftp),
                db: tri(ov.feature_db),
                backups: tri(ov.feature_backups),
                cron: tri(ov.feature_cron),
              },
              effective: {
                files: eff.files !== false,
                emails: eff.emails !== false,
                ftp: eff.ftp !== false,
                db: eff.db !== false,
                backups: eff.backups !== false,
                cron: eff.cron !== false,
              },
              pkg: {
                files: pkg.files_enabled !== false,
                emails: pkg.emails_enabled !== false,
                ftp: pkg.ftp_enabled !== false,
                db: pkg.db_enabled !== false,
                backups: pkg.backups_enabled !== false,
                cron: pkg.cron_enabled !== false,
              },
              dirty: false,
              saving: false,
            } as RowState;
          } catch {
            return null;
          }
        })
      );
      setRows(settled.filter(Boolean) as RowState[]);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to load access matrix');
    } finally {
      setLoading(false);
    }
  }, [statusFilter]);

  useEffect(() => { load(); }, [load]);

  const setCell = (id: number, feature: string, value: number) => {
    setRows((prev) =>
      prev.map((r) => {
        if (r.account.id !== id) return r;
        const overrides = { ...r.overrides, [feature]: value };
        const effective = {
          ...r.effective,
          [feature]: value === -1 ? r.pkg[feature] !== false : value === 1,
        };
        return { ...r, overrides, effective, dirty: true };
      })
    );
  };

  const saveRow = async (id: number) => {
    const row = rows.find((r) => r.account.id === id);
    if (!row) return;
    setRows((prev) => prev.map((r) => (r.account.id === id ? { ...r, saving: true } : r)));
    setError('');
    try {
      await setAccountResourceLimits(id, {
        cpu_limit: 0, disk_mb: 0, bandwidth_mb: 0, ram_limit_mb: 0,
        max_db: 0, max_email: 0, max_ftp: 0, max_domains: 0, max_subdomains: 0,
        ssh_access: -1, upload_limit_mb: 0,
        feature_files: row.overrides.files,
        feature_emails: row.overrides.emails,
        feature_ftp: row.overrides.ftp,
        feature_db: row.overrides.db,
        feature_backups: row.overrides.backups,
        feature_cron: row.overrides.cron,
      });
      setRows((prev) => prev.map((r) => (r.account.id === id ? { ...r, dirty: false, saving: false } : r)));
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to save');
      setRows((prev) => prev.map((r) => (r.account.id === id ? { ...r, saving: false } : r)));
    }
  };

  const blockAll = (id: number) => {
    FEATURES.forEach((f) => setCell(id, f.key, 0));
  };

  const resetRow = (id: number) => {
    FEATURES.forEach((f) => setCell(id, f.key, -1));
  };

  const filtered = rows.filter(
    (r) =>
      !search ||
      (r.account.username || '').includes(search) ||
      (r.account.domain || '').includes(search) ||
      (r.account.email || '').includes(search)
  );
  const totalPages = Math.max(1, Math.ceil(filtered.length / limit));
  const paginated = filtered.slice((page - 1) * limit, page * limit);

  const cellContent = (row: RowState, f: string) => {
    const ov = row.overrides[f];
    const allowed = row.effective[f];
    if (ov === 1)
      return { cls: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300', icon: <Check className="h-3.5 w-3.5" />, title: 'Allowed (override) — click to block' };
    if (ov === 0)
      return { cls: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300', icon: <Ban className="h-3.5 w-3.5" />, title: 'Blocked (override) — click for package default' };
    return {
      cls: allowed
        ? 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400'
        : 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300',
      icon: <Minus className="h-3.5 w-3.5" />,
      title: `Package default (${allowed ? 'allowed' : 'blocked'}) — click to ${allowed ? 'block' : 'allow'}`,
    };
  };

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Access Control</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Click any cell to cycle allow → block → package default. Legend: <Check className="inline h-3 w-3" /> allowed · <Ban className="inline h-3 w-3" /> blocked · <Minus className="inline h-3 w-3" /> package default
          </p>
        </div>
        <button onClick={load} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700" title="Refresh">
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          <AlertTriangle className="h-4 w-4" />{error}
          <button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button>
        </div>
      )}

      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            placeholder="Search users..."
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1); }}
            className="w-full pl-9 pr-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        <select
          value={statusFilter}
          onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}
          className="px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">All statuses</option>
          <option value="active">Active</option>
          <option value="suspended">Suspended</option>
          <option value="pending">Pending</option>
          <option value="terminated">Terminated</option>
        </select>
      </div>

      <Card padding={false}>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-gray-200 dark:border-gray-700 text-left">
                <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-400 min-w-[180px]">User</th>
                {FEATURES.map((f) => (
                  <th key={f.key} className="px-2 py-3 font-medium text-gray-600 dark:text-gray-400 text-center">{f.label}</th>
                ))}
                <th className="px-4 py-3 font-medium text-gray-600 dark:text-gray-400 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr><td colSpan={8} className="px-4 py-10 text-center text-gray-400"><Loader2 className="h-5 w-5 animate-spin inline mr-2" />Loading access matrix...</td></tr>
              ) : paginated.length === 0 ? (
                <tr><td colSpan={8} className="px-4 py-10 text-center text-gray-400">No accounts found</td></tr>
              ) : (
                paginated.map((row) => (
                  <tr key={row.account.id} className="border-b border-gray-100 dark:border-gray-700/50 last:border-0 hover:bg-gray-50/60 dark:hover:bg-gray-700/20">
                    <td className="px-4 py-2.5">
                      <div className="font-medium text-gray-900 dark:text-gray-100">{row.account.username}</div>
                      <div className="text-xs text-gray-400">{row.account.domain} · {row.account.package_name}</div>
                    </td>
                    {FEATURES.map((f) => {
                      const c = cellContent(row, f.key);
                      const terminated = row.account.status === 'terminated';
                      return (
                        <td key={f.key} className="px-2 py-2.5 text-center">
                          <button
                            disabled={terminated}
                            onClick={() => setCell(row.account.id, f.key, nextState(row.overrides[f.key]))}
                            title={terminated ? 'Terminated account' : c.title}
                            className={`inline-flex items-center justify-center w-9 h-9 rounded-xl transition-all hover:scale-105 disabled:opacity-40 ${c.cls}`}
                          >
                            {c.icon}
                          </button>
                        </td>
                      );
                    })}
                    <td className="px-4 py-2.5 text-right whitespace-nowrap">
                      {row.dirty ? (
                        <Button size="sm" onClick={() => saveRow(row.account.id)} disabled={row.saving} loading={row.saving}>
                          Save
                        </Button>
                      ) : (
                        <div className="inline-flex gap-1">
                          <button onClick={() => blockAll(row.account.id)} title="Block all six features"
                            className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30">
                            <Ban className="h-4 w-4" />
                          </button>
                          <button onClick={() => resetRow(row.account.id)} title="Reset all to package defaults"
                            className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30">
                            <RotateCcw className="h-4 w-4" />
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </Card>

      <Pagination page={page} totalPages={totalPages} total={filtered.length} limit={limit} onPageChange={setPage} />
    </motion.div>
  );
}

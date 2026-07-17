import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { getAccounts, createAccount, suspendAccount, unsuspendAccount, terminateAccount, getPackages, getAccountUploadLimit, setAccountUploadLimit, getAccountRamLimit, setAccountRamLimit } from '../lib/api';
import { Plus, Search, Ban, CheckCircle, Trash2, RefreshCw, Server, Globe, Mail, X, HardDrive, TrendingUp, Database, Users as UsersIcon, Loader2, Upload, MemoryStick } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import DataTable, { Column } from '../components/ui/DataTable';
import Pagination from '../components/ui/Pagination';

const statusColors: Record<string, string> = {
  active: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  suspended: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  pending: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
  terminated: 'bg-red-100 text-red-700 dark:bg-red-900/50 dark:text-red-300',
};

export function Accounts() {
  const [accounts, setAccounts] = useState<any[]>([]);
  const [packages, setPackages] = useState<any[]>([]);
  const [filter, setFilter] = useState('');
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [sortKey, setSortKey] = useState('');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
  const [page, setPage] = useState(1);
  const limit = 10;

  const [actionLoading, setActionLoading] = useState<Record<number, string>>({});

  const load = useCallback(() => {
    getAccounts(filter || undefined).then((d) => setAccounts(d || [])).catch(() => {});
  }, [filter]);

  useEffect(() => { load(); getPackages().then((d) => setPackages(d || [])).catch(() => {}); }, [load]);

  const [actionError, setActionError] = useState('');
  const [form, setForm] = useState({ username: '', domain: '', email: '', password: '', package_id: 1 });
  const [createError, setCreateError] = useState('');
  const [creating, setCreating] = useState(false);

  // Upload limit state
  const [limitAccount, setLimitAccount] = useState<any | null>(null);
  const [limitValue, setLimitValue] = useState('');
  const [limitCurrent, setLimitCurrent] = useState('');
  const [limitSaving, setLimitSaving] = useState(false);
  const [limitError, setLimitError] = useState('');

  // RAM limit state
  const [ramLimitAccount, setRamLimitAccount] = useState<any | null>(null);
  const [ramLimitValue, setRamLimitValue] = useState('');
  const [ramLimitSaving, setRamLimitSaving] = useState(false);
  const [ramLimitError, setRamLimitError] = useState('');

  const openLimitModal = async (account: any) => {
    setLimitAccount(account);
    setLimitValue('');
    setLimitCurrent('Loading...');
    setLimitError('');
    try {
      const data = await getAccountUploadLimit(account.id);
      setLimitValue(String(data.current_limit_mb));
      setLimitCurrent(String(data.current_limit_mb) + ' MB');
    } catch {
      setLimitCurrent('Error loading');
    }
  };

  const [ramLimitPackageDefault, setRamLimitPackageDefault] = useState('');

  const openRamLimitModal = async (account: any) => {
    setRamLimitAccount(account);
    setRamLimitValue('');
    setRamLimitPackageDefault('');
    setRamLimitError('');
    try {
      const data = await getAccountRamLimit(account.id);
      setRamLimitValue(String(data.current_limit_mb));
      setRamLimitPackageDefault(data.package_limit_mb === 0 ? 'Unlimited' : data.package_limit_mb + ' MB');
    } catch {
      setRamLimitError('Error loading current limit');
    }
  };

  const handleSaveLimit = async () => {
    if (!limitAccount) return;
    const mb = parseInt(limitValue);
    if (isNaN(mb) || mb < 0) {
      setLimitError('Enter a valid number (0 or greater)');
      return;
    }
    setLimitSaving(true);
    setLimitError('');
    try {
      await setAccountUploadLimit(limitAccount.id, mb);
      setLimitAccount(null);
    } catch (err: any) {
      setLimitError(err?.error || 'Failed to save limit');
    } finally {
      setLimitSaving(false);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreateError('');
    setCreating(true);
    try {
      await createAccount(form);
      setShowCreate(false);
      setForm({ username: '', domain: '', email: '', password: '', package_id: 1 });
      load();
    } catch (err: any) {
      setCreateError(err?.error || 'Failed to create account');
    } finally {
      setCreating(false);
    }
  };

  const handleSuspend = async (id: number) => {
    try {
      await suspendAccount(id);
      setActionError('');
    } catch (err: any) {
      setActionError(err?.error || 'Failed to suspend account');
    }
    load();
  };
  const handleUnsuspend = async (id: number) => {
    try {
      await unsuspendAccount(id);
      setActionError('');
    } catch (err: any) {
      setActionError(err?.error || 'Failed to unsuspend account');
    }
    load();
  };
  const handleTerminate = async (id: number) => {
    if (!confirm('Terminate this account? This action cannot be undone.')) return;
    try {
      await terminateAccount(id);
      setActionError('');
    } catch (err: any) {
      setActionError(err?.error || 'Failed to terminate account');
    }
    load();
  };

  const filtered = accounts.filter((a) =>
    !search || a.username.includes(search) || a.domain.includes(search) || a.email.includes(search)
  );

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDir(prev => prev === 'asc' ? 'desc' : 'asc');
    } else {
      setSortKey(key);
      setSortDir('asc');
    }
  };

  const sorted = [...filtered].sort((a, b) => {
    if (!sortKey) return 0;
    const aVal = a[sortKey];
    const bVal = b[sortKey];
    if (typeof aVal === 'string') {
      return sortDir === 'asc' ? aVal.localeCompare(bVal) : bVal.localeCompare(aVal);
    }
    return sortDir === 'asc' ? (aVal - bVal) : (bVal - aVal);
  });

  const totalPages = Math.ceil(sorted.length / limit);
  const paginated = sorted.slice((page - 1) * limit, page * limit);

  const columns: Column<any>[] = [
    { key: 'username', label: 'Account', sortable: true, render: (a) => (
      <div className="flex items-center gap-2">
        <Server className="h-4 w-4 text-gray-400" />
        <div>
          <div className="font-medium text-gray-900 dark:text-gray-100">{a.username}</div>
          <div className="text-xs text-gray-400">{a.email}</div>
        </div>
      </div>
    )},
    { key: 'domain', label: 'Domain', sortable: true, render: (a) => (
      <div className="flex items-center gap-1.5">
        <Globe className="h-3 w-3 text-gray-400" />
        <span className="text-gray-700 dark:text-gray-300">{a.domain}</span>
      </div>
    )},
    { key: 'package_name', label: 'Package', sortable: true, className: 'hidden md:table-cell', render: (a) => <span className="text-gray-500">{a.package_name}</span> },
    { key: 'status', label: 'Status', sortable: true, render: (a) => (
      <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${statusColors[a.status] || 'bg-gray-100 text-gray-600'}`}>
        {a.status}
      </span>
    )},
    { key: 'disk_used_mb', label: 'Disk', sortable: true, className: 'hidden lg:table-cell', render: (a) => <span className="text-gray-500">{a.disk_used_mb} MB</span> },
    { key: 'ram_used_mb', label: 'RAM', sortable: true, className: 'hidden lg:table-cell', render: (a) => {
      const limit = a.ram_limit_mb;
      return <span className="text-gray-500">{a.ram_used_mb} MB / {!limit ? '∞' : limit + ' MB'}</span>;
    } },
    { key: 'actions', label: 'Actions', className: 'text-right', render: (a) => (
      <div className="flex items-center justify-end gap-1">
        {a.status === 'active' && (
          <button onClick={(e) => { e.stopPropagation(); handleSuspend(a.id); }} className="p-1.5 text-amber-500 hover:bg-amber-50 dark:hover:bg-amber-900/30 rounded" title="Suspend">
            <Ban className="h-4 w-4" />
          </button>
        )}
        {a.status === 'suspended' && (
          <button onClick={(e) => { e.stopPropagation(); handleUnsuspend(a.id); }} className="p-1.5 text-emerald-500 hover:bg-emerald-50 dark:hover:bg-emerald-900/30 rounded" title="Unsuspend">
            <CheckCircle className="h-4 w-4" />
          </button>
        )}
        {a.status !== 'terminated' && (
          <button onClick={(e) => { e.stopPropagation(); openLimitModal(a); }} className="p-1.5 text-blue-500 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded" title="Upload Limit">
            <Upload className="h-4 w-4" />
          </button>
        )}
        {a.status !== 'terminated' && (
          <button onClick={(e) => { e.stopPropagation(); openRamLimitModal(a); }} className="p-1.5 text-purple-500 hover:bg-purple-50 dark:hover:bg-purple-900/30 rounded" title="RAM Limit">
            <MemoryStick className="h-4 w-4" />
          </button>
        )}
        {a.status !== 'terminated' && (
          <button onClick={(e) => { e.stopPropagation(); handleTerminate(a.id); }} className="p-1.5 text-red-500 hover:bg-red-50 dark:hover:bg-red-900/30 rounded" title="Terminate">
            <Trash2 className="h-4 w-4" />
          </button>
        )}
      </div>
    )},
  ];

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Accounts</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{accounts.length} hosting accounts</p>
        </div>
        <Button onClick={() => setShowCreate(true)}>
          <Plus className="h-4 w-4" /> Create Account
        </Button>
      </div>

      {actionError && (
        <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          {actionError}
          <button onClick={() => setActionError('')} className="ml-auto"><X className="h-4 w-4" /></button>
        </div>
      )}

      {/* Filters */}
      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            placeholder="Search accounts..."
            value={search}
            onChange={(e) => { setSearch(e.target.value); setPage(1); }}
            className="w-full pl-9 pr-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        <select
          value={filter}
          onChange={(e) => { setFilter(e.target.value); setPage(1); }}
          className="px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">All statuses</option>
          <option value="active">Active</option>
          <option value="suspended">Suspended</option>
          <option value="pending">Pending</option>
          <option value="terminated">Terminated</option>
        </select>
        <button onClick={load} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700">
          <RefreshCw className="h-4 w-4" />
        </button>
      </div>

      {/* Table */}
      <Card padding={false}>
        <DataTable
          columns={columns}
          data={paginated}
          sortKey={sortKey}
          sortDir={sortDir}
          onSort={handleSort}
          emptyMessage="No accounts found"
          keyExtractor={(a) => a.id}
        />
      </Card>

      <Pagination
        page={page}
        totalPages={totalPages}
        total={sorted.length}
        limit={limit}
        onPageChange={setPage}
      />

      {/* Create modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-10 pb-10 overflow-auto">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-2xl mx-4 p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Create Hosting Account</h3>
              <button onClick={() => setShowCreate(false)} className="p-1 text-gray-400 hover:text-gray-600">
                <X className="h-5 w-5" />
              </button>
            </div>
            {createError && (
              <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{createError}</div>
            )}
            <form onSubmit={handleCreate}>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                {/* Left column: account info */}
                <div className="space-y-3">
                  <h4 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">Account Information</h4>
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username</label>
                    <input type="text" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" placeholder="siteuser" required />
                    <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">System username for this account</p>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Domain</label>
                    <input type="text" value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" placeholder="example.com" required />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Email</label>
                    <input type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" placeholder="user@example.com" required />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Password</label>
                    <input type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" placeholder="Min. 8 characters" required minLength={8} />
                  </div>
                </div>

                {/* Right column: package & limits */}
                <div className="space-y-3">
                  <h4 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">Resource Limits</h4>
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Hosting Package</label>
                    <select value={form.package_id} onChange={(e) => setForm({ ...form, package_id: +e.target.value })}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                      {packages.map((p: any) => (
                        <option key={p.id} value={p.id}>{p.name}</option>
                      ))}
                    </select>
                  </div>

                  {/* Selected package limits */}
                  {(() => {
                    const selected = packages.find((p: any) => p.id === form.package_id);
                    if (!selected) return null;
                    const limits = [
                      { label: 'Disk Space', value: selected.disk_mb >= 1024 ? `${(selected.disk_mb / 1024).toFixed(1)} GB` : `${selected.disk_mb} MB`, icon: HardDrive },
                      { label: 'Bandwidth', value: selected.bandwidth_mb >= 1024 ? `${(selected.bandwidth_mb / 1024).toFixed(1)} GB` : `${selected.bandwidth_mb} MB`, icon: TrendingUp },
                      { label: 'Max Databases', value: selected.max_db, icon: Database },
                      { label: 'Max Email Accounts', value: selected.max_email, icon: Mail },
                      { label: 'Max FTP Accounts', value: selected.max_ftp, icon: UsersIcon },
                      { label: 'Max Domains', value: selected.max_domains, icon: Globe },
                      { label: 'Max Subdomains', value: selected.max_subdomains, icon: Globe },
                      { label: 'SSH Access', value: selected.ssh_access ? 'Yes' : 'No', icon: Server },
                    ];
                    return (
                      <div className="bg-gray-50 dark:bg-gray-700/50 rounded-lg p-3 space-y-2">
                        <p className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-2">Package &quot;{selected.name}&quot; includes:</p>
                        {limits.map((l) => (
                          <div key={l.label} className="flex items-center justify-between text-xs">
                            <div className="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
                              <l.icon className="h-3 w-3" />
                              {l.label}
                            </div>
                            <span className="font-medium text-gray-700 dark:text-gray-300">{l.value}</span>
                          </div>
                        ))}
                      </div>
                    );
                  })()}
                </div>
              </div>

              <Button type="submit" disabled={creating} className="w-full mt-6" loading={creating}>
                Create Account
              </Button>
            </form>
          </div>
        </div>
      )}

      {/* RAM Limit Modal */}
      {ramLimitAccount && (
        <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-20 pb-10 overflow-auto">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">
                RAM Limit — {ramLimitAccount.username}
              </h3>
              <button onClick={() => setRamLimitAccount(null)} className="p-1 text-gray-400 hover:text-gray-600">
                <X className="h-5 w-5" />
              </button>
            </div>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
              Current usage: <span className="font-medium text-gray-700 dark:text-gray-300">{ramLimitAccount.ram_used_mb} MB</span>
            </p>
            <p className="text-xs text-gray-400 dark:text-gray-500 mb-4">
              Package default: <span className="font-medium text-gray-600 dark:text-gray-300">{ramLimitPackageDefault || 'Loading...'}</span>
            </p>
            {ramLimitError && (
              <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{ramLimitError}</div>
            )}
            <div className="mb-4">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">RAM Limit (MB)</label>
              <input type="number" min="0" value={ramLimitValue} onChange={(e) => setRamLimitValue(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-purple-500"
                placeholder="Enter limit in MB (0 = use package default)" />
              <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">Set to 0 to use the package default limit</p>
            </div>
            <div className="flex gap-3">
              <Button onClick={async () => {
                const mb = parseInt(ramLimitValue);
                if (isNaN(mb) || mb < 0) { setRamLimitError('Enter a valid number (0 or greater)'); return; }
                setRamLimitSaving(true); setRamLimitError('');
                try {
                  await setAccountRamLimit(ramLimitAccount.id, mb);
                  setRamLimitAccount(null);
                } catch (err: any) {
                  setRamLimitError(err?.error || 'Failed to save');
                } finally { setRamLimitSaving(false); }
              }} disabled={ramLimitSaving} loading={ramLimitSaving} className="flex-1">
                Save
              </Button>
              <Button variant="secondary" onClick={() => setRamLimitAccount(null)} className="flex-1">
                Cancel
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* Upload Limit Modal */}
      {limitAccount && (
        <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/40 pt-20 pb-10 overflow-auto">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">
                Upload Limit — {limitAccount.username}
              </h3>
              <button onClick={() => setLimitAccount(null)} className="p-1 text-gray-400 hover:text-gray-600">
                <X className="h-5 w-5" />
              </button>
            </div>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
              Current limit: <span className="font-medium text-gray-700 dark:text-gray-300">{limitCurrent}</span>
            </p>
            {limitError && (
              <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{limitError}</div>
            )}
            <div className="mb-4">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Upload Limit (MB)</label>
              <input type="number" min="0" value={limitValue} onChange={(e) => setLimitValue(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="Enter limit in MB" />
              <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">Set to 0 to use the default limit</p>
            </div>
            <div className="flex gap-3">
              <Button onClick={handleSaveLimit} disabled={limitSaving} loading={limitSaving} className="flex-1">
                Save
              </Button>
              <Button variant="secondary" onClick={() => setLimitAccount(null)} className="flex-1">
                Cancel
              </Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

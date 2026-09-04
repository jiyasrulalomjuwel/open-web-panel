import { useEffect, useState, useCallback } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { motion } from 'framer-motion';
import { getAccounts, createAccount, getPackages } from '../lib/api';
import { Plus, Search, RefreshCw, Server, Globe, X, HardDrive, TrendingUp, Database, Mail, Users as UsersIcon, ChevronRight } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import DataTable, { Column } from '../components/ui/DataTable';
import Pagination from '../components/ui/Pagination';

const statusColors: Record<string, string> = {
  active: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  suspended: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  pending: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
  terminated: 'bg-red-100 text-red-700 dark:bg-red-900/50 dark:text-red-300',
};

export function Accounts() {
  const navigate = useNavigate();
  const [accounts, setAccounts] = useState<any[]>([]);
  const [packages, setPackages] = useState<any[]>([]);
  const [filter, setFilter] = useState('');
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [sortKey, setSortKey] = useState('');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
  const [page, setPage] = useState(1);
  const limit = 10;

  const load = useCallback(() => {
    getAccounts(filter || undefined).then((d) => setAccounts(d || [])).catch(() => {});
  }, [filter]);

  useEffect(() => { load(); getPackages().then((d) => setPackages(d || [])).catch(() => {}); }, [load]);

  const [form, setForm] = useState({ username: '', domain: '', email: '', password: '', package_id: 1 });
  const [createError, setCreateError] = useState('');
  const [creating, setCreating] = useState(false);

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
      setCreateError(err?.message || err?.error || 'Failed to create account');
    } finally {
      setCreating(false);
    }
  };

  const filtered = accounts.filter((a) =>
    !search || (a.username || '').includes(search) || (a.domain || '').includes(search) || (a.email || '').includes(search)
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

  useEffect(() => {
    setPage((p) => Math.min(p, Math.max(1, totalPages)));
  }, [totalPages]);

  const paginated = sorted.slice((page - 1) * limit, page * limit);

  const columns: Column<any>[] = [
    { key: 'username', label: 'Account', sortable: true, render: (a) => (
      <Link to={`/accounts/${a.id}`} onClick={(e) => e.stopPropagation()} className="flex items-center gap-2 group">
        <Server className="h-4 w-4 text-gray-400" />
        <div>
          <div className="font-medium text-gray-900 dark:text-gray-100 group-hover:text-blue-600 dark:group-hover:text-blue-400">{a.username}</div>
          <div className="text-xs text-gray-400">{a.email}</div>
        </div>
      </Link>
    )},
    { key: 'domain', label: 'Domain', sortable: true, render: (a) => (
      <div className="flex items-center gap-1.5">
        <Globe className="h-3 w-3 text-gray-400" />
        <span className="text-gray-700 dark:text-gray-300">{a.domain}</span>
      </div>
    )},
    { key: 'package_name', label: 'Package', sortable: true, className: 'hidden md:table-cell', render: (a) => <span className="text-gray-500">{a.package_name}</span> },
    { key: 'status', label: 'Status', sortable: true, render: (a) => (
      <span className="flex flex-col items-start gap-1">
        <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${statusColors[a.status] || 'bg-gray-100 text-gray-600'}`}>
          {a.status}
        </span>
        {a.status === 'suspended' && a.purge_in_days >= 0 && (
          <span className="inline-flex px-2 py-0.5 rounded-full text-[11px] font-medium bg-red-50 text-red-600 dark:bg-red-900/30 dark:text-red-300" title={a.purge_at ? `Permanently deleted on ${a.purge_at} UTC` : 'Auto-delete scheduled'}>
            {a.purge_in_days === 0 ? 'deleted soon' : `${a.purge_in_days}d left`}
          </span>
        )}
      </span>
    )},
    { key: 'disk_used_mb', label: 'Disk', sortable: true, className: 'hidden lg:table-cell', render: (a) => <span className="text-gray-500">{a.disk_used_mb} MB</span> },
    { key: 'ram_used_mb', label: 'RAM', sortable: true, className: 'hidden lg:table-cell', render: (a) => {
      const ramLimit = a.ram_limit_mb;
      return <span className="text-gray-500">{a.ram_used_mb} MB / {!ramLimit ? '∞' : ramLimit + ' MB'}</span>;
    } },
    { key: 'actions', label: '', className: 'text-right', render: (a) => (
      <Link to={`/accounts/${a.id}`} onClick={(e) => e.stopPropagation()}
        className="inline-flex items-center gap-1 p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded hover:bg-blue-50 dark:hover:bg-blue-900/30 text-xs font-medium" title="Open details">
        Details <ChevronRight className="h-4 w-4" />
      </Link>
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
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{accounts.length} hosting accounts · click a row to manage</p>
        </div>
        <Button onClick={() => setShowCreate(true)}>
          <Plus className="h-4 w-4" /> Create Account
        </Button>
      </div>

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
          onRowClick={(a) => navigate(`/accounts/${a.id}`)}
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
    </motion.div>
  );
}

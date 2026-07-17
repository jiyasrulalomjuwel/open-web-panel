import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Inbox, X, Trash2, AlertTriangle, Loader2, Search } from 'lucide-react';
import { getSubmissions, deleteSubmission } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import DataTable, { Column } from '../components/ui/DataTable';
import Pagination from '../components/ui/Pagination';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type Submission = { id: number; account_id: number; form_type: string; metadata: string; ip_address: string; created_at: string };

export function Submissions() {
  const [subs, setSubs] = useState<Submission[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [filter, setFilter] = useState('');
  const [page, setPage] = useState(1);
  const [sortKey, setSortKey] = useState('');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
  const limit = 10;

  const load = useCallback(() => {
    setLoading(true);
    getSubmissions().then(d => setSubs(d || [])).catch(() => {}).finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleDelete = async (id: number) => {
    try { await deleteSubmission(id); setSuccess('Deleted'); load(); }
    catch (err: any) { setError(err?.error || 'Failed'); }
  };

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc');
    } else {
      setSortKey(key);
      setSortDir('asc');
    }
    setPage(1);
  };

  const filtered = filter
    ? subs.filter(s => s.form_type.toLowerCase().includes(filter.toLowerCase()) || s.metadata.toLowerCase().includes(filter.toLowerCase()))
    : subs;

  const sorted = [...filtered].sort((a, b) => {
    if (!sortKey) return 0;
    const aVal = (a as any)[sortKey] ?? '';
    const bVal = (b as any)[sortKey] ?? '';
    const cmp = typeof aVal === 'number' ? aVal - bVal : String(aVal).localeCompare(String(bVal));
    return sortDir === 'asc' ? cmp : -cmp;
  });

  const totalPages = Math.ceil(sorted.length / limit);
  const paged = sorted.slice((page - 1) * limit, page * limit);

  const tryParse = (s: string) => {
    try { return JSON.stringify(JSON.parse(s), null, 2); } catch { return s; }
  };

  const columns: Column<Submission>[] = [
    {
      key: 'form_type',
      label: 'Form Type',
      sortable: true,
      render: (s) => <Badge variant="info">{s.form_type}</Badge>,
    },
    { key: 'id', label: 'ID', sortable: true },
    {
      key: 'account_id',
      label: 'Account',
      sortable: true,
      render: (s) => s.account_id > 0 ? `#${s.account_id}` : '-',
    },
    {
      key: 'ip_address',
      label: 'IP',
      sortable: true,
      render: (s) => s.ip_address || '-',
    },
    {
      key: 'metadata',
      label: 'Data',
      render: (s) => (
        <pre className="text-xs text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900 rounded p-1.5 overflow-x-auto max-h-20 font-mono leading-tight">
          {tryParse(s.metadata)}
        </pre>
      ),
    },
    {
      key: 'created_at',
      label: 'Created',
      sortable: true,
      render: (s) => new Date(s.created_at).toLocaleString(),
    },
    {
      key: 'actions',
      label: '',
      render: (s) => (
        <button onClick={() => handleDelete(s.id)} className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30">
          <Trash2 className="h-4 w-4" />
        </button>
      ),
    },
  ];

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Form Submissions</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{subs.length} submission(s)</p>
        </div>
        <div className="relative">
          <Search className="h-4 w-4 absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500" />
          <input type="text" value={filter} onChange={e => { setFilter(e.target.value); setPage(1); }}
            className="pl-9 pr-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 w-48"
            placeholder="Filter..." />
        </div>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      <Card>
        {loading ? (
          <Skeleton lines={5} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={<Inbox className="h-10 w-10 text-gray-400" />}
            title={filter ? 'No submissions match your filter' : 'No submissions'}
            message={filter ? 'Try a different search term.' : 'Form submissions from your websites will appear here.'}
          />
        ) : (
          <>
            <DataTable
              columns={columns}
              data={paged}
              sortKey={sortKey}
              sortDir={sortDir}
              onSort={handleSort}
              keyExtractor={(s) => s.id}
              emptyMessage="No submissions found"
            />
            <Pagination
              page={page}
              totalPages={totalPages}
              total={sorted.length}
              limit={limit}
              onPageChange={setPage}
            />
          </>
        )}
      </Card>
    </motion.div>
  );
}

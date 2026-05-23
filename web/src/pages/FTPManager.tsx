import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Plus, X, AlertTriangle, Loader2, Trash2, Edit3, Eye, EyeOff, Server } from 'lucide-react';
import { getFTPAccounts, createFTPAccount, updateFTPAccount, deleteFTPAccount, getDomains } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type FTPAccount = {
  id: number; username: string; domain: string;
  directory: string; quota_mb: number; status: string; created_at: string;
};

export function FTPManager() {
  const [accounts, setAccounts] = useState<FTPAccount[]>([]);
  const [domains, setDomains] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [showEdit, setShowEdit] = useState<FTPAccount | null>(null);
  const [deleting, setDeleting] = useState<number | null>(null);

  // Create form
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPw, setShowPw] = useState(false);
  const [domainId, setDomainId] = useState(0);
  const [directory, setDirectory] = useState('');
  const [quotaMb, setQuotaMb] = useState(100);
  const [creating, setCreating] = useState(false);

  // Edit form
  const [editPassword, setEditPassword] = useState('');
  const [showEditPw, setShowEditPw] = useState(false);
  const [editDirectory, setEditDirectory] = useState('');
  const [editQuotaMb, setEditQuotaMb] = useState(100);
  const [editStatus, setEditStatus] = useState('active');
  const [savingEdit, setSavingEdit] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      getFTPAccounts(),
      getDomains()
    ]).then(([a, d]) => { setAccounts(a || []); setDomains(d || []); }).catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const resetCreate = () => {
    setUsername(''); setPassword(''); setDomainId(0);
    setDirectory(''); setQuotaMb(100); setShowPw(false);
  };

  const handleCreate = async () => {
    if (!username || !password || !domainId) return;
    setCreating(true);
    try {
      const domain = domains.find(d => d.id === domainId);
      const dir = directory || ('/' + username);
      await createFTPAccount({ username, password, domain: domain?.domain || '', directory: dir, quota_mb: quotaMb });
      setShowCreate(false);
      resetCreate();
      setSuccess('FTP account created');
      load();
    } catch (err: any) { setError(err?.error || 'Create failed'); }
    finally { setCreating(false); }
  };

  const openEdit = (a: FTPAccount) => {
    setShowEdit(a);
    setEditPassword('');
    setEditDirectory(a.directory);
    setEditQuotaMb(a.quota_mb);
    setEditStatus(a.status);
    setShowEditPw(false);
    setError('');
  };

  const handleEdit = async () => {
    if (!showEdit) return;
    setSavingEdit(true);
    try {
      const data: any = {};
      if (editPassword) data.password = editPassword;
      if (editDirectory !== showEdit.directory) data.directory = editDirectory;
      if (editQuotaMb !== showEdit.quota_mb) data.quota_mb = editQuotaMb;
      if (editStatus !== showEdit.status) data.status = editStatus;
      await updateFTPAccount(showEdit.id, data);
      setShowEdit(null);
      setSuccess('FTP account updated');
      load();
    } catch (err: any) { setError(err?.error || 'Update failed'); }
    finally { setSavingEdit(false); }
  };

  const handleDelete = async (id: number) => {
    setDeleting(id);
    try { await deleteFTPAccount(id); setSuccess('FTP account deleted'); load(); }
    catch (err: any) { setError(err?.error || 'Delete failed'); }
    finally { setDeleting(null); }
  };

  const statusBadge = (s: string) => {
    const map: Record<string, 'success' | 'error' | 'warning'> = { active: 'success', suspended: 'error', inactive: 'warning' };
    return <Badge variant={map[s] || 'neutral'}>{s}</Badge>;
  };

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">FTP Manager</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{accounts.length} account(s)</p>
        </div>
        <Button variant="primary" onClick={() => { setShowCreate(true); setError(''); resetCreate(); }}>
          <Plus className="h-4 w-4" /> Create FTP Account
        </Button>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {loading ? (
        <Card><Skeleton lines={3} /></Card>
      ) : accounts.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Server className="h-10 w-10 text-gray-400" />}
            title="No FTP accounts"
            message="Create an FTP account to access your files via FTP clients."
            actionLabel="Create FTP Account"
            onAction={() => { setShowCreate(true); setError(''); resetCreate(); }}
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
                      <Server className="h-5 w-5 text-blue-500 shrink-0" />
                      <h3 className="font-medium text-gray-900 dark:text-gray-100 truncate">{a.username}</h3>
                      {statusBadge(a.status)}
                    </div>
                    <div className="text-xs text-gray-500 dark:text-gray-400 space-y-0.5 mt-2">
                      <p>Domain: {a.domain}</p>
                      <p>Directory: {a.directory}</p>
                      <p>Quota: {a.quota_mb > 0 ? a.quota_mb + ' MB' : 'Unlimited'}</p>
                      <p>Created: {a.created_at}</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 ml-3 shrink-0">
                    <button onClick={() => openEdit(a)} className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30">
                      <Edit3 className="h-4 w-4" />
                    </button>
                    <button onClick={() => handleDelete(a.id)} disabled={deleting === a.id} className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 disabled:opacity-40">
                      {deleting === a.id ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}

      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowCreate(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-5">Create FTP Account</h3>
            <div className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Domain</label>
                <select value={domainId} onChange={e => setDomainId(Number(e.target.value))}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                  <option value={0}>Choose a domain...</option>
                  {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
                </select>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username</label>
                  <input value={username} onChange={e => setUsername(e.target.value)} placeholder="ftpuser"
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Password</label>
                  <div className="relative">
                    <input type={showPw ? 'text' : 'password'} value={password} onChange={e => setPassword(e.target.value)} placeholder="Min 6 chars"
                      className="w-full px-3 py-2 pr-8 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                    <button onClick={() => setShowPw(!showPw)} className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                      {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                </div>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                  Directory <span className="text-gray-400 font-normal">(leave empty for /username)</span>
                </label>
                <input value={directory} onChange={e => setDirectory(e.target.value)} placeholder={'/' + (username || 'username')}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Quota (MB) — 0 = unlimited</label>
                <input type="number" min={0} max={99999} value={quotaMb} onChange={e => setQuotaMb(Number(e.target.value))}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
              </div>
              <Button variant="primary" className="w-full" onClick={handleCreate} disabled={creating || !username || !password || !domainId || password.length < 6} loading={creating}>
                Create Account
              </Button>
            </div>
          </div>
        </div>
      )}

      {showEdit && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowEdit(null)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-5">Edit FTP Account: {showEdit.username}</h3>
            <div className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">New Password <span className="text-gray-400 font-normal">(leave empty to keep current)</span></label>
                <div className="relative">
                  <input type={showEditPw ? 'text' : 'password'} value={editPassword} onChange={e => setEditPassword(e.target.value)} placeholder="Min 6 chars"
                    className="w-full px-3 py-2 pr-8 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                  <button onClick={() => setShowEditPw(!showEditPw)} className="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                    {showEditPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Directory</label>
                <input value={editDirectory} onChange={e => setEditDirectory(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Quota (MB)</label>
                  <input type="number" min={0} max={99999} value={editQuotaMb} onChange={e => setEditQuotaMb(Number(e.target.value))}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Status</label>
                  <select value={editStatus} onChange={e => setEditStatus(e.target.value)}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                    <option value="active">Active</option>
                    <option value="suspended">Suspended</option>
                    <option value="inactive">Inactive</option>
                  </select>
                </div>
              </div>
              <Button variant="primary" className="w-full" onClick={handleEdit} disabled={savingEdit} loading={savingEdit}>
                Save Changes
              </Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

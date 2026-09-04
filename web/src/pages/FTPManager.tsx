import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Plus, X, AlertTriangle, Loader2, Trash2, Edit3, Eye, EyeOff, Server } from 'lucide-react';
import { getFTPAccounts, createFTPAccount, updateFTPAccount, deleteFTPAccount, getDomains } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Modal from '../components/ui/Modal';
import ConfirmDialog from '../components/ui/ConfirmDialog';

function ErrorBanner({ msg, onDismiss }: { msg: string; onDismiss: () => void }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"
    >
      <AlertTriangle className="h-4 w-4 shrink-0" />
      <span className="flex-1">{msg}</span>
      <button onClick={onDismiss} className="text-red-400 hover:text-red-600 p-0.5"><X className="h-4 w-4" /></button>
    </motion.div>
  );
}

function SuccessBanner({ msg, onDismiss }: { msg: string; onDismiss: () => void }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300"
    >
      {msg}
      <button onClick={onDismiss} className="ml-auto text-emerald-400 hover:text-emerald-600 p-0.5"><X className="h-4 w-4" /></button>
    </motion.div>
  );
}

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
  const [confirmDelete, setConfirmDelete] = useState<FTPAccount | null>(null);

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPw, setShowPw] = useState(false);
  const [domainId, setDomainId] = useState(0);
  const [directory, setDirectory] = useState('');
  const [quotaMb, setQuotaMb] = useState(100);
  const [creating, setCreating] = useState(false);

  const [editPassword, setEditPassword] = useState('');
  const [showEditPw, setShowEditPw] = useState(false);
  const [editDirectory, setEditDirectory] = useState('');
  const [editQuotaMb, setEditQuotaMb] = useState(100);
  const [editStatus, setEditStatus] = useState('active');
  const [savingEdit, setSavingEdit] = useState(false);

  const load = useCallback((showLoader = true) => {
    if (showLoader) setLoading(true);
    setError('');
    return Promise.all([
      getFTPAccounts(),
      getDomains()
    ]).then(([a, d]) => { setAccounts(a || []); setDomains(d || []); })
      .catch((err: any) => { console.error('Load FTP:', err); setError(err?.error || 'Failed to load FTP accounts'); })
      .finally(() => { if (showLoader) setLoading(false); });
  }, []);

  useEffect(() => { let mounted = true; load().then(() => { if (!mounted) return; }); return () => { mounted = false; }; }, [load]);

  const refresh = useCallback(() => load(false), [load]);

  const resetCreate = () => {
    setUsername(''); setPassword(''); setDomainId(0);
    setDirectory(''); setQuotaMb(100); setShowPw(false);
  };

  const handleCreate = async () => {
    if (!username || !password || !domainId) return;
    setCreating(true);
    try {
      const domain = domains.find(d => d.id === domainId);
      const dir = directory.trim() || username;
      await createFTPAccount({ username, password, domain: domain?.domain || '', directory: dir, quota_mb: quotaMb });
      setShowCreate(false);
      resetCreate();
      setSuccess('FTP account created');
      refresh();
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
      refresh();
    } catch (err: any) { setError(err?.error || 'Update failed'); }
    finally { setSavingEdit(false); }
  };

  const handleDelete = async (id: number) => {
    setDeleting(id);
    try { await deleteFTPAccount(id); setSuccess('FTP account deleted'); refresh(); }
    catch (err: any) { setError(err?.error || 'Delete failed'); }
    finally { setDeleting(null); }
  };

  const statusBadge = (s: string) => {
    const map: Record<string, 'success' | 'error' | 'warning' | 'neutral'> = {
      active: 'success', disabled: 'neutral', suspended: 'error', inactive: 'warning'
    };
    return <Badge variant={map[s] || 'neutral'}>{s}</Badge>;
  };

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="max-w-4xl mx-auto space-y-5">
      <div>
        <h1 className="text-lg font-semibold text-gray-900 dark:text-gray-100">FTP Manager</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">{loading ? '...' : `${accounts.length} account(s)`}</p>
      </div>

      {error && <ErrorBanner msg={error} onDismiss={() => setError('')} />}
      {success && <SuccessBanner msg={success} onDismiss={() => setSuccess('')} />}

      <div className="flex items-center justify-between">
        <div />
        <Button onClick={() => { setShowCreate(true); setError(''); resetCreate(); }} size="sm">
          <Plus className="h-4 w-4" /> Create FTP Account
        </Button>
      </div>

      {loading ? (
        <Card><div className="h-4 bg-gray-100 rounded w-1/3 mb-3" /><div className="h-3 bg-gray-50 rounded w-1/2" /></Card>
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
        <div className="space-y-2">
          {accounts.map((a, i) => (
            <motion.div
              key={a.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.03 }}
            >
              <Card hover>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 min-w-0 flex-1">
                    <div className="p-2 rounded-md bg-blue-50 dark:bg-blue-900/30 shrink-0">
                      <Server className="h-4 w-4 text-blue-500" strokeWidth={1.5} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <h3 className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{a.username}</h3>
                        {statusBadge(a.status)}
                      </div>
                      <div className="flex items-center gap-3 mt-0.5 text-xs text-gray-400 dark:text-gray-500">
                        <span>{a.domain}</span>
                        <span className="text-gray-300 dark:text-gray-600">|</span>
                        <span className="truncate max-w-[200px]">{a.directory}</span>
                        <span className="text-gray-300 dark:text-gray-600">|</span>
                        <span>{a.quota_mb > 0 ? a.quota_mb + ' MB' : 'Unlimited'}</span>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <button onClick={() => openEdit(a)} className="p-1.5 text-gray-400 hover:text-blue-600 rounded-md hover:bg-blue-50 dark:hover:bg-blue-900/30 transition-colors" title="Edit">
                      <Edit3 className="h-4 w-4" strokeWidth={1.5} />
                    </button>
                    <button onClick={() => setConfirmDelete(a)} disabled={deleting === a.id} className="p-1.5 text-gray-400 hover:text-red-600 rounded-md hover:bg-red-50 dark:hover:bg-red-900/30 transition-colors disabled:opacity-40" title="Delete">
                      {deleting === a.id ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" strokeWidth={1.5} />}
                    </button>
                  </div>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}

      {showCreate && (
        <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create FTP Account" size="sm">
          <div className="space-y-4">
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Domain</label>
              <select value={domainId} onChange={e => setDomainId(Number(e.target.value))}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all">
                <option value={0}>Choose a domain...</option>
                {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
              </select>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Username</label>
                <input value={username} onChange={e => setUsername(e.target.value)} placeholder="ftpuser"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Password</label>
                <div className="relative">
                  <input type={showPw ? 'text' : 'password'} value={password} onChange={e => setPassword(e.target.value)} placeholder="Min 6 chars"
                    className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
                  <button type="button" onClick={() => setShowPw(!showPw)} className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                    {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>
            </div>
            {password.length > 0 && password.length < 6 && (
              <p className="text-xs text-red-500">Password must be at least 6 characters</p>
            )}
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">
                Directory <span className="text-gray-400 dark:text-gray-500 font-normal">(leave empty for /username)</span>
              </label>
              <input value={directory} onChange={e => setDirectory(e.target.value)} placeholder={'/' + (username || 'username')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Quota (MB) &mdash; 0 = unlimited</label>
              <input type="number" min={0} max={99999} value={quotaMb} onChange={e => setQuotaMb(Number(e.target.value))}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
            </div>
            <Button variant="primary" className="w-full" onClick={handleCreate} disabled={creating || !username || !password || !domainId || password.length < 6} loading={creating}>
              Create Account
            </Button>
          </div>
        </Modal>
      )}

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={() => { const id = confirmDelete!.id; setConfirmDelete(null); handleDelete(id); }}
        title="Delete FTP Account"
        message={confirmDelete ? `Delete ${confirmDelete.username}? This action cannot be undone.` : ''}
        confirmLabel="Delete"
        variant="danger"
      />

      {showEdit && (
        <Modal isOpen={true} onClose={() => setShowEdit(null)} title={'Edit FTP Account: ' + showEdit.username} size="sm">
          <div className="space-y-4">
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">New Password <span className="text-gray-400 dark:text-gray-500 font-normal">(leave empty to keep current)</span></label>
              <div className="relative">
                <input type={showEditPw ? 'text' : 'password'} value={editPassword} onChange={e => setEditPassword(e.target.value)} placeholder="Min 6 chars"
                  className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
                <button type="button" onClick={() => setShowEditPw(!showEditPw)} className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                  {showEditPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
              {editPassword.length > 0 && editPassword.length < 6 && (
                <p className="text-xs text-red-500 mt-1">Password must be at least 6 characters</p>
              )}
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Directory</label>
              <input value={editDirectory} onChange={e => setEditDirectory(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Quota (MB)</label>
                <input type="number" min={0} max={99999} value={editQuotaMb} onChange={e => setEditQuotaMb(Number(e.target.value))}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all" />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Status</label>
                <select value={editStatus} onChange={e => setEditStatus(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all">
                  <option value="active">Active</option>
                  <option value="disabled">Disabled</option>
                  <option value="suspended">Suspended</option>
                  <option value="inactive">Inactive</option>
                </select>
              </div>
            </div>
            <Button variant="primary" className="w-full" onClick={handleEdit}
              disabled={savingEdit || (editPassword.length > 0 && editPassword.length < 6)} loading={savingEdit}>
              Save Changes
            </Button>
          </div>
        </Modal>
      )}
    </motion.div>
  );
}

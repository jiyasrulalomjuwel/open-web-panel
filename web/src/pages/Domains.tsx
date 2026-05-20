import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { getDomains, createDomain, deleteDomain } from '../lib/api';
import { Globe, Plus, Trash2, FolderOpen, ExternalLink, X, AlertTriangle, Loader2, ArrowRight } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';

const typeColors: Record<string, string> = {
  primary: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  addon: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
  parked: 'bg-purple-100 text-purple-700 dark:bg-purple-900/50 dark:text-purple-300',
  subdomain: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
};

export function Domains() {
  const [domains, setDomains] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const navigate = useNavigate();

  const load = useCallback(() => {
    setLoading(true);
    getDomains().then((d) => { setDomains(d || []); setError(''); }).catch(() => {}).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const [showAdd, setShowAdd] = useState(false);
  const [domain, setDomain] = useState('');
  const [dtype, setDtype] = useState('addon');
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<number | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{ id: number; name: string } | null>(null);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    setAdding(true);
    setError('');
    try {
      await createDomain(domain, dtype);
      setShowAdd(false);
      setDomain('');
      load();
    } catch (err: any) {
      setError(err?.error || 'Failed to add domain');
    } finally {
      setAdding(false);
    }
  };

  const handleDelete = async (id: number, name: string) => {
    setConfirmDelete({ id, name });
  };
  const confirmDeleteAction = async () => {
    if (!confirmDelete) return;
    setDeleting(confirmDelete.id);
    try {
      await deleteDomain(confirmDelete.id);
      setConfirmDelete(null);
      load();
    } catch (err: any) {
      setError(err?.error || 'Failed to delete domain');
    } finally {
      setDeleting(null);
    }
  };

  const openInFileManager = (docRoot: string) => {
    localStorage.setItem('owp_fm_path', docRoot);
    navigate('/child/files');
  };

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Domains</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {loading ? 'Loading...' : `${domains.length} domains configured`}
          </p>
        </div>
        <Button onClick={() => { setShowAdd(true); setError(''); }}>
          <Plus className="h-4 w-4" /> Add Domain
        </Button>
      </div>

      {error && (
        <motion.div
          initial={{ opacity: 0, y: -10 }}
          animate={{ opacity: 1, y: 0 }}
          className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300 animate-fade-in"
        >
          <AlertTriangle className="h-4 w-4 shrink-0" /> {error}
          <button onClick={() => setError('')} className="ml-auto text-red-400 hover:text-red-600"><X className="h-4 w-4" /></button>
        </motion.div>
      )}

      {/* Loading skeleton */}
      {loading && domains.length === 0 && (
        <div className="space-y-3">
          {[1, 2, 3].map((i) => (
            <Card key={i}>
              <div className="flex items-center gap-3">
                <div className="h-10 w-10 bg-gray-200 dark:bg-gray-700 rounded-lg animate-pulse" />
                <div className="flex-1">
                  <div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 mb-2 animate-pulse" />
                  <div className="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2 animate-pulse" />
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Domain list */}
      {!loading && (
        <div className="space-y-3">
          {domains.map((d, i) => (
            <motion.div
              key={d.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.05 }}
            >
              <Card hover>
                <div className="flex items-start justify-between">
                  <div className="flex items-start gap-3 flex-1 min-w-0">
                    <div className={`mt-0.5 p-2 rounded-lg ${d.type === 'primary' ? 'bg-emerald-50 dark:bg-emerald-900/30' : 'bg-blue-50 dark:bg-blue-900/30'}`}>
                      <Globe className={`h-5 w-5 ${d.type === 'primary' ? 'text-emerald-600 dark:text-emerald-400' : 'text-blue-600 dark:text-blue-400'}`} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <h3 className="font-medium text-gray-900 dark:text-gray-100 truncate">{d.domain}</h3>
                        <Badge variant={d.type === 'primary' ? 'success' : 'info'}>{d.type}</Badge>
                        {d.ssl_enabled && <Badge variant="success">SSL</Badge>}
                      </div>
                      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                        <div className="flex items-center gap-1">
                          <FolderOpen className="h-3 w-3 shrink-0" />
                          <span className="font-mono text-gray-600 dark:text-gray-400 truncate max-w-[200px]">~/{d.doc_root.split('/').slice(3).join('/') || d.doc_root}</span>
                        </div>
                        <button
                          onClick={() => openInFileManager(d.doc_root)}
                          className="flex items-center gap-1 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 transition-colors"
                        >
                          Browse files <ArrowRight className="h-3 w-3" />
                        </button>
                        <a
                          href={`http://${d.domain}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="flex items-center gap-1 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 transition-colors"
                        >
                          <ExternalLink className="h-3 w-3" /> Visit
                        </a>
                      </div>
                    </div>
                  </div>
                  {d.type !== 'primary' && (
                    <button
                      onClick={() => handleDelete(d.id, d.domain)}
                      disabled={deleting === d.id}
                      className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 transition-colors disabled:opacity-50 shrink-0"
                      title="Remove domain"
                    >
                      {deleting === d.id ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Trash2 className="h-4 w-4" />
                      )}
                    </button>
                  )}
                </div>
              </Card>
            </motion.div>
          ))}
          {domains.length === 0 && !error && (
            <Card className="text-center py-10">
              <Globe className="h-10 w-10 text-gray-200 dark:text-gray-600 mx-auto mb-3" />
              <p className="text-gray-400 dark:text-gray-500 text-sm">No domains configured</p>
              <button onClick={() => setShowAdd(true)} className="mt-3 text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium">
                Add your first domain
              </button>
            </Card>
          )}
        </div>
      )}

      {/* Add domain modal */}
      {showAdd && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowAdd(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Add Domain</h3>
              <button onClick={() => setShowAdd(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <form onSubmit={handleAdd} className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Domain Name</label>
                <input
                  type="text" autoFocus value={domain}
                  onChange={(e) => setDomain(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="example.com"
                  required
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Type</label>
                <select value={dtype} onChange={(e) => setDtype(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                  <option value="addon">Addon Domain</option>
                  <option value="subdomain">Subdomain</option>
                </select>
              </div>
              <p className="text-xs text-gray-400 dark:text-gray-500">
                {dtype === 'primary'
                  ? <>Document root: <code className="text-blue-600">~/public_html</code></>
                  : <>Document root: <code className="text-blue-600">~/{domain || 'domain.com'}</code></>
                }
              </p>
              <Button type="submit" disabled={adding || !domain} className="w-full" loading={adding}>
                Add Domain
              </Button>
            </form>
          </div>
        </div>
      )}

      {/* Delete confirmation modal */}
      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setConfirmDelete(null)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Remove Domain</h3>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-5">
              Remove &quot;{confirmDelete.name}&quot;? Files will not be deleted.
            </p>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(null)}>Cancel</Button>
              <Button variant="danger" onClick={confirmDeleteAction} disabled={deleting === confirmDelete.id} loading={deleting === confirmDelete.id}>
                Remove
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

import { useEffect, useState, useCallback, useRef } from 'react';
import { motion } from 'framer-motion';
import { AlertTriangle, X, Loader2, Trash2, Plus, FileCode, Eye, EyeOff, Globe, Bug } from 'lucide-react';
import { getRecentErrors, getCustomErrorPages, getCustomErrorContent, saveCustomErrorPage, deleteCustomErrorPage, getDomains } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type ErrorEntry = { domain: string; line: string; level: string; time: string };
type CustomPage = { id: number; domain_id: number; domain: string; error_code: number };

const levelColor: Record<string, string> = {
  error: 'text-red-600 dark:text-red-400',
  warn: 'text-yellow-600 dark:text-yellow-400',
  critical: 'text-red-700 dark:text-red-300',
  alert: 'text-orange-600 dark:text-orange-400',
  emergency: 'text-red-800 dark:text-red-200',
};

export function ErrorManager() {
  const [errors, setErrors] = useState<ErrorEntry[]>([]);
  const [pages, setPages] = useState<CustomPage[]>([]);
  const [domains, setDomains] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [tab, setTab] = useState<'logs' | 'custom'>('logs');

  // Custom page editor
  const [showEditor, setShowEditor] = useState(false);
  const [editId, setEditId] = useState<number | null>(null);
  const [domainId, setDomainId] = useState(0);
  const [errorCode, setErrorCode] = useState(404);
  const [content, setContent] = useState('');
  const [saving, setSaving] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      getRecentErrors(),
      getCustomErrorPages(),
      getDomains()
    ]).then(([e, p, d]) => { setErrors(e || []); setPages(p || []); setDomains(d || []); }).catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const openEditor = async (page?: CustomPage) => {
    if (page) {
      setEditId(page.id);
      setDomainId(page.domain_id);
      setErrorCode(page.error_code);
      try {
        const data = await getCustomErrorContent(page.id);
        setContent(data.content || '');
      } catch { setContent(''); }
    } else {
      setEditId(null);
      setDomainId(0);
      setErrorCode(404);
      setContent('<!DOCTYPE html><html><head><title>Error</title></head><body><h1>Oops!</h1><p>Something went wrong.</p></body></html>');
    }
    setShowEditor(true);
    setError('');
  };

  const handleSave = async () => {
    if (!domainId || !errorCode || !content.trim()) return;
    setSaving(true);
    try {
      const data: any = { domain_id: domainId, error_code: errorCode, content };
      if (editId) data.id = editId;
      await saveCustomErrorPage(data);
      setShowEditor(false);
      setSuccess('Custom error page saved');
      load();
    } catch (err: any) { setError(err?.error || 'Save failed'); }
    finally { setSaving(false); }
  };

  const handleDelete = async (id: number) => {
    try { await deleteCustomErrorPage(id); setSuccess('Error page removed'); load(); }
    catch (err: any) { setError(err?.error || 'Delete failed'); }
  };

  const levelBadge = (lvl: string) => {
    const map: Record<string, 'error' | 'warning' | 'neutral'> = {
      error: 'error', warn: 'warning', critical: 'error', alert: 'warning', emergency: 'error',
    };
    return <Badge variant={map[lvl] || 'neutral'}>{lvl}</Badge>;
  };

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Error Manager</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">View error logs and set custom error pages per domain</p>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700">
        <button onClick={() => setTab('logs')} className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === 'logs' ? 'border-blue-500 text-blue-600 dark:text-blue-400' : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'}`}>Error Logs</button>
        <button onClick={() => setTab('custom')} className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${tab === 'custom' ? 'border-blue-500 text-blue-600 dark:text-blue-400' : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'}`}>Custom Error Pages</button>
      </div>

      {loading ? (
        <Card><Skeleton lines={4} /></Card>
      ) : tab === 'logs' ? (
        errors.length === 0 ? (
          <Card><EmptyState icon={<Bug className="h-10 w-10 text-gray-400" />} title="No recent errors" message="Your websites are running without errors." /></Card>
        ) : (
          <Card>
            <div className="space-y-2 max-h-[600px] overflow-y-auto">
              {errors.map((e, i) => (
                <div key={i} className="p-2.5 bg-gray-50 dark:bg-gray-800/50 rounded-lg text-xs font-mono">
                  <div className="flex items-center gap-2 mb-1">
                    <Globe className="h-3 w-3 text-gray-400" />
                    <span className="text-gray-900 dark:text-gray-100 font-semibold">{e.domain}</span>
                    {levelBadge(e.level)}
                    <span className="text-gray-400 ml-auto">{e.time}</span>
                  </div>
                  <p className={`${levelColor[e.level] || 'text-gray-600 dark:text-gray-400'} break-all leading-relaxed`}>{e.line}</p>
                </div>
              ))}
            </div>
          </Card>
        )
      ) : (
        <>
          <div className="flex justify-end">
            <Button variant="primary" onClick={() => openEditor()}><Plus className="h-4 w-4" /> Add Custom Page</Button>
          </div>
          {pages.length === 0 ? (
            <Card><EmptyState icon={<FileCode className="h-10 w-10 text-gray-400" />} title="No custom error pages" message="Create custom error pages for your domains." actionLabel="Add Custom Page" onAction={() => openEditor()} /></Card>
          ) : (
            <div className="space-y-3">
              {pages.map((p, i) => (
                <motion.div key={p.id} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.04 }}>
                  <Card>
                    <div className="flex items-start justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-red-50 dark:bg-red-900/30"><FileCode className="h-5 w-5 text-red-500" /></div>
                        <div>
                          <h3 className="font-medium text-gray-900 dark:text-gray-100">{p.error_code} · {p.domain}</h3>
                          <p className="text-xs text-gray-500 dark:text-gray-400">HTTP {p.error_code} error page</p>
                        </div>
                      </div>
                      <div className="flex gap-1">
                        <button onClick={() => openEditor(p)} className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30"><Eye className="h-4 w-4" /></button>
                        <button onClick={() => handleDelete(p.id)} className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30"><Trash2 className="h-4 w-4" /></button>
                      </div>
                    </div>
                  </Card>
                </motion.div>
              ))}
            </div>
          )}
        </>
      )}

      {showEditor && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowEditor(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-2xl mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-5">{editId ? 'Edit' : 'Add'} Custom Error Page</h3>
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Domain</label>
                  <select value={domainId} onChange={e => setDomainId(Number(e.target.value))}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                    <option value={0}>Choose...</option>
                    {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
                  </select>
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">HTTP Error Code</label>
                  <select value={errorCode} onChange={e => setErrorCode(Number(e.target.value))}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                    {[400, 401, 403, 404, 405, 408, 429, 500, 502, 503, 504].map(code => (
                      <option key={code} value={code}>{code}</option>
                    ))}
                  </select>
                </div>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">HTML Content</label>
                <textarea value={content} onChange={e => setContent(e.target.value)} rows={12}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y" />
              </div>
              <Button variant="primary" className="w-full" onClick={handleSave} disabled={saving || !domainId || !errorCode || !content.trim()} loading={saving}>
                Save Error Page
              </Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

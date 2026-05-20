import { useState, useEffect } from 'react';
import { motion } from 'framer-motion';
import { Plus, Trash2, ExternalLink } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Modal from '../components/ui/Modal';
import Spinner from '../components/ui/Spinner';
import { useToast } from '../components/ToastProvider';
import { getDomains } from '../lib/api';

// Define local API calls for redirects since they're new
const API_BASE = '/api/v1/child';

async function request(url: string, opts?: any) {
  const token = localStorage.getItem('owp_access_token');
  const res = await fetch(API_BASE + url, {
    ...opts,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...opts?.headers },
    body: opts?.body ? JSON.stringify(opts.body) : undefined,
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

interface Redirect {
  id: number;
  domain_id: number;
  source_path: string;
  target_url: string;
  redirect_type: string;
  status: string;
}

export function Redirects() {
  const { toast } = useToast();
  const [redirects, setRedirects] = useState<Redirect[]>([]);
  const [domains, setDomains] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [form, setForm] = useState({ domain_id: 0, source_path: '/old-page', target_url: 'https://', redirect_type: '301' });
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    Promise.all([
      request('/redirects'),
      getDomains().catch(() => []),
    ]).then(([r, d]) => {
      setRedirects(Array.isArray(r) ? r : []);
      setDomains(Array.isArray(d) ? d : []);
    }).catch(() => toast('error', 'Failed to load redirects')).finally(() => setLoading(false));
  }, []);

  const createRedirect = async () => {
    if (!form.source_path || !form.target_url) { toast('error', 'Please fill all fields'); return; }
    setSaving(true);
    try {
      const res = await request('/redirects', { method: 'POST', body: form });
      setRedirects(prev => [...prev, res]);
      setShowModal(false);
      toast('success', 'Redirect created');
    } catch (e: any) {
      toast('error', e.message || 'Failed to create redirect');
    } finally { setSaving(false); }
  };

  const deleteRedirect = async (id: number) => {
    try {
      await request(`/redirects/${id}`, { method: 'DELETE' });
      setRedirects(prev => prev.filter(r => r.id !== id));
      toast('success', 'Redirect deleted');
    } catch { toast('error', 'Failed to delete'); }
  };

  if (loading) return <div className="flex justify-center py-16"><Spinner size={32} text="Loading redirects..." /></div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">Redirects</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Manage URL redirects for your domains</p>
        </div>
        <Button onClick={() => setShowModal(true)}><Plus size={18} />Add Redirect</Button>
      </div>

      {redirects.length === 0 ? (
        <EmptyState title="No redirects" message="Add your first URL redirect" actionLabel="Add Redirect" onAction={() => setShowModal(true)} />
      ) : (
        <Card>
          <div className="space-y-3">
            {redirects.map((r, i) => (
              <motion.div
                key={r.id} initial={{ opacity: 0, x: -10 }} animate={{ opacity: 1, x: 0 }} transition={{ delay: i * 0.05 }}
                className="flex items-center justify-between p-4 bg-gray-50 dark:bg-gray-700/50 rounded-lg"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 mb-1">
                    <Badge variant={r.redirect_type === '301' ? 'success' : 'info'}>{r.redirect_type}</Badge>
                    <Badge variant={r.status === 'active' ? 'success' : 'neutral'} dot>{r.status}</Badge>
                  </div>
                  <p className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{r.source_path}</p>
                  <p className="text-xs text-gray-500 dark:text-gray-400 truncate flex items-center gap-1">
                    → <ExternalLink size={12} /> {r.target_url}
                  </p>
                </div>
                <button onClick={() => deleteRedirect(r.id)} className="p-2 text-gray-400 hover:text-red-500 transition-colors">
                  <Trash2 size={16} />
                </button>
              </motion.div>
            ))}
          </div>
        </Card>
      )}

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Add Redirect" size="lg">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Domain</label>
            <select value={form.domain_id} onChange={e => setForm(p => ({ ...p, domain_id: +e.target.value }))}
              className="w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-3 py-2 text-sm">
              <option value={0}>Select domain</option>
              {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Source Path</label>
            <input value={form.source_path} onChange={e => setForm(p => ({ ...p, source_path: e.target.value }))}
              className="w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-3 py-2 text-sm" />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Target URL</label>
            <input value={form.target_url} onChange={e => setForm(p => ({ ...p, target_url: e.target.value }))}
              className="w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-3 py-2 text-sm" />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Type</label>
            <select value={form.redirect_type} onChange={e => setForm(p => ({ ...p, redirect_type: e.target.value }))}
              className="w-full rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-3 py-2 text-sm">
              <option value="301">301 — Permanent</option>
              <option value="302">302 — Temporary</option>
            </select>
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={createRedirect} loading={saving}>Create Redirect</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}

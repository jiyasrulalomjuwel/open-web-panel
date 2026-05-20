import { useState, useEffect } from 'react';
import { motion } from 'framer-motion';
import { Shield, Plus, X } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Spinner from '../components/ui/Spinner';
import { useToast } from '../components/ToastProvider';

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

export function HotlinkProtection() {
  const { toast } = useToast();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [domains, setDomains] = useState<string[]>(['']);
  const [hasSettings, setHasSettings] = useState(false);

  useEffect(() => {
    request('/hotlink').then(r => {
      if (r && r.id) {
        setEnabled(r.enabled === 1);
        setDomains(r.allowed_domains ? r.allowed_domains.split(',').filter(Boolean) : ['']);
        setHasSettings(true);
      }
    }).catch(() => {}).finally(() => setLoading(false));
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      await request('/hotlink', {
        method: 'POST',
        body: { enabled: enabled ? 1 : 0, allowed_domains: domains.filter(Boolean).join(',') }
      });
      toast('success', enabled ? 'Hotlink protection enabled' : 'Hotlink protection disabled');
      setHasSettings(true);
    } catch (e: any) {
      toast('error', e.message || 'Failed to save');
    } finally { setSaving(false); }
  };

  if (loading) return <div className="flex justify-center py-16"><Spinner size={32} text="Loading..." /></div>;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">Hotlink Protection</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Prevent other sites from using your bandwidth</p>
        </div>
      </div>

      <Card>
        <div className="space-y-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Shield size={24} className={enabled ? 'text-emerald-500' : 'text-gray-400'} />
              <div>
                <p className="font-medium text-gray-900 dark:text-gray-100">Protection Status</p>
                <p className="text-sm text-gray-500 dark:text-gray-400">{enabled ? 'Active — external hotlinks blocked' : 'Disabled'}</p>
              </div>
            </div>
            <label className="relative inline-flex items-center cursor-pointer">
              <input type="checkbox" checked={enabled} onChange={() => setEnabled(!enabled)} className="sr-only peer" />
              <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-blue-600"></div>
            </label>
          </div>

          {enabled && (
            <motion.div initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: 'auto' }} className="space-y-3 pt-4 border-t border-gray-200 dark:border-gray-700">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">Allowed Referrer Domains</label>
              <p className="text-xs text-gray-500 dark:text-gray-400">Add domains that may embed your files (e.g., your own domains)</p>
              {domains.map((d, i) => (
                <div key={i} className="flex gap-2">
                  <input value={d} onChange={e => { const nd = [...domains]; nd[i] = e.target.value; setDomains(nd); }}
                    placeholder="example.com" className="flex-1 rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-3 py-2 text-sm" />
                  {domains.length > 1 && (
                    <button onClick={() => setDomains(domains.filter((_, j) => j !== i))} className="p-2 text-gray-400 hover:text-red-500">
                      <X size={18} />
                    </button>
                  )}
                </div>
              ))}
              <button onClick={() => setDomains([...domains, ''])} className="text-sm text-blue-600 hover:text-blue-700 flex items-center gap-1">
                <Plus size={16} /> Add domain
              </button>
            </motion.div>
          )}

          <div className="flex justify-end pt-2">
            <Button onClick={save} loading={saving}>{hasSettings ? 'Update' : 'Save'} Settings</Button>
          </div>
        </div>
      </Card>
    </div>
  );
}

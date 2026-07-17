import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { getPackages, createPackage, updatePackage, deletePackage } from '../lib/api';
import { Plus, Pencil, Trash2, HardDrive, Database, Globe, Mail, RefreshCw, X, Loader2 } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';

export function Packages() {
  const [pkgs, setPkgs] = useState<any[]>([]);
  const load = useCallback(() => getPackages().then((d) => setPkgs(d || [])).catch(() => {}), []);
  useEffect(() => { load(); }, [load]);

  const [showModal, setShowModal] = useState<'create' | 'edit' | null>(null);
  const [editId, setEditId] = useState<number | null>(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const emptyForm = { name: '', disk_mb: 1000, bandwidth_mb: 10000, ram_limit_mb: 0, max_db: 5, max_email: 10, max_ftp: 5, max_domains: 3, max_subdomains: 10, ssh_access: false, backup_enabled: true };
  const [form, setForm] = useState(emptyForm);

  const openEdit = (p: any) => {
    setEditId(p.id);
    setForm({ name: p.name, disk_mb: p.disk_mb, bandwidth_mb: p.bandwidth_mb, ram_limit_mb: p.ram_limit_mb, max_db: p.max_db, max_email: p.max_email, max_ftp: p.max_ftp, max_domains: p.max_domains, max_subdomains: p.max_subdomains, ssh_access: p.ssh_access, backup_enabled: p.backup_enabled });
    setShowModal('edit');
  };

  const openCreate = () => {
    setEditId(null);
    setForm(emptyForm);
    setShowModal('create');
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSaving(true);
    try {
      if (showModal === 'edit' && editId) {
        await updatePackage(editId, form);
      } else {
        await createPackage(form);
      }
      setShowModal(null);
      load();
    } catch (err: any) {
      setError(err?.error || 'Save failed');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: number, name: string) => {
    if (!confirm(`Delete package "${name}"?`)) return;
    try {
      await deletePackage(id);
    } catch (err: any) {
      setError(err?.error || 'Failed to delete package');
    }
    load();
  };

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Packages</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{pkgs.length} hosting plan templates</p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" /> Create Package
        </Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {pkgs.map((p, i) => (
          <motion.div
            key={p.id}
            initial={{ opacity: 0, y: 15 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.05 }}
          >
            <Card hover>
              <div className="flex items-start justify-between mb-3">
                <div>
                  <h3 className="font-medium text-gray-900 dark:text-gray-100">{p.name}</h3>
                  {p.is_default ? <Badge variant="info">Default</Badge> : null}
                </div>
                <div className="flex gap-1">
                  <button onClick={() => openEdit(p)} className="p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded hover:bg-blue-50 dark:hover:bg-blue-900/30">
                    <Pencil className="h-4 w-4" />
                  </button>
                  {!p.is_default && (
                    <button onClick={() => handleDelete(p.id, p.name)} className="p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400 rounded hover:bg-red-50 dark:hover:bg-red-900/30">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2 text-sm">
                <div className="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
                  <HardDrive className="h-3.5 w-3.5" /> {p.disk_mb} MB
                </div>
                <div className="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
                  <Database className="h-3.5 w-3.5" /> {p.max_db} DBs
                </div>
                <div className="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
                  <Globe className="h-3.5 w-3.5" /> {p.max_domains} domains
                </div>
                <div className="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
                  <Mail className="h-3.5 w-3.5" /> {p.max_email} emails
                </div>
              </div>
              <div className="mt-3 flex flex-wrap gap-1.5">
                <span className="text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 px-2 py-0.5 rounded">{p.max_ftp} FTP</span>
                <span className="text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 px-2 py-0.5 rounded">{p.max_subdomains} subdomains</span>
                <span className="text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 px-2 py-0.5 rounded">{p.ram_limit_mb === 0 ? '∞ RAM' : p.ram_limit_mb + ' MB RAM'}</span>
                <span className={`text-xs px-2 py-0.5 rounded ${p.ssh_access ? 'bg-emerald-50 dark:bg-emerald-900/50 text-emerald-600 dark:text-emerald-300' : 'bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500'}`}>
                  {p.ssh_access ? 'SSH' : 'No SSH'}
                </span>
                <span className={`text-xs px-2 py-0.5 rounded ${p.backup_enabled ? 'bg-emerald-50 dark:bg-emerald-900/50 text-emerald-600 dark:text-emerald-300' : 'bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500'}`}>
                  {p.backup_enabled ? 'Backups' : 'No backups'}
                </span>
              </div>
            </Card>
          </motion.div>
        ))}
        {pkgs.length === 0 && (
          <div className="col-span-full text-center py-12 text-gray-400 dark:text-gray-500">No packages configured</div>
        )}
      </div>

      {/* Create/Edit modal */}
      {showModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-lg mx-4 p-6 shadow-xl max-h-[90vh] overflow-auto" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">{showModal === 'create' ? 'Create Package' : 'Edit Package'}</h3>
              <button onClick={() => setShowModal(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            {error && <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error}</div>}
            <form onSubmit={handleSave} className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Package Name</label>
                <input type="text" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" required />
              </div>
              <div className="grid grid-cols-2 gap-3">
                {[['Disk (MB)', 'disk_mb'], ['Bandwidth (MB)', 'bandwidth_mb'], ['RAM Limit (MB)', 'ram_limit_mb'], ['Max Databases', 'max_db'], ['Max Emails', 'max_email'], ['Max FTP', 'max_ftp'], ['Max Domains', 'max_domains'], ['Max Subdomains', 'max_subdomains']].map(([label, key]) => (
                  <div key={key}>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">{label}</label>
                    <input type="number" value={(form as any)[key]} onChange={(e) => setForm({ ...form, [key]: +e.target.value })}
                      className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
                  </div>
                ))}
              </div>
              <div className="flex items-center gap-6">
                <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                  <input type="checkbox" checked={form.ssh_access} onChange={(e) => setForm({ ...form, ssh_access: e.target.checked })}
                    className="rounded border-gray-300 dark:border-gray-600" /> SSH Access
                </label>
                <label className="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                  <input type="checkbox" checked={form.backup_enabled} onChange={(e) => setForm({ ...form, backup_enabled: e.target.checked })}
                    className="rounded border-gray-300 dark:border-gray-600" /> Backups Enabled
                </label>
              </div>
              <Button type="submit" disabled={saving} className="w-full" loading={saving}>
                {showModal === 'create' ? 'Create Package' : 'Save Changes'}
              </Button>
            </form>
          </div>
        </div>
      )}
    </motion.div>
  );
}

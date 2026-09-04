import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { Clock, Plus, X, Loader2, AlertTriangle, Trash2, Pencil, Play, Pause } from 'lucide-react';
import { getCronJobs, createCronJob, updateCronJob, deleteCronJob, toggleCronJob, getCronPresets } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

type Job = { id: number; command: string; schedule: string; description: string; enabled: boolean; last_run_at: string; created_at: string };

export function CronJobs() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [presets, setPresets] = useState<Array<{ label: string; schedule: string }>>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<Job | null>(null);
  const [command, setCommand] = useState('');
  const [schedule, setSchedule] = useState('');
  const [description, setDescription] = useState('');
  const [saving, setSaving] = useState(false);
  const [acting, setActing] = useState<number | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([getCronJobs().catch(() => []), getCronPresets().catch(() => [])])
      .then(([j, p]) => { setJobs(j || []); setPresets(p || []); })
      .catch((e: any) => setError(e?.message || e?.error || 'Failed to load cron jobs'))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const openCreate = () => {
    setEditing(null);
    setCommand('');
    setSchedule('');
    setDescription('');
    setError('');
    setShowForm(true);
  };

  const openEdit = (j: Job) => {
    setEditing(j);
    setCommand(j.command);
    setSchedule(j.schedule);
    setDescription(j.description || '');
    setError('');
    setShowForm(true);
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!command.trim() || !schedule.trim()) return;
    setSaving(true);
    setError('');
    try {
      if (editing) {
        await updateCronJob(editing.id, { command, schedule, description });
      } else {
        await createCronJob({ command, schedule, description });
      }
      setShowForm(false);
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to save cron job');
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = async (j: Job) => {
    setActing(j.id);
    try {
      await toggleCronJob(j.id);
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to toggle job');
    } finally {
      setActing(null);
    }
  };

  const handleDelete = async (j: Job) => {
    if (!confirm(`Delete cron job "${j.schedule} ${j.command}"?`)) return;
    setActing(j.id);
    try {
      await deleteCronJob(j.id);
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to delete job');
    } finally {
      setActing(null);
    }
  };

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Cron Jobs</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{jobs.length}/20 jobs · run inside your container</p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4" /> New Cron Job
        </Button>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {loading ? (
        <Card><Skeleton lines={3} /></Card>
      ) : jobs.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Clock className="h-10 w-10 text-gray-400" />}
            title="No cron jobs yet"
            message="Schedule commands to run automatically, e.g. Laravel schedulers, backup scripts, queue workers."
            actionLabel="Create Cron Job"
            onAction={openCreate}
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {jobs.map((j, i) => (
            <motion.div key={j.id} initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.04 }}>
              <Card>
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <code className="text-sm font-mono font-medium text-gray-900 dark:text-gray-100 break-all">{j.command}</code>
                      <Badge variant={j.enabled ? 'success' : 'neutral'}>{j.enabled ? 'enabled' : 'disabled'}</Badge>
                    </div>
                    <p className="text-xs text-gray-500 dark:text-gray-400 font-mono">{j.schedule}</p>
                    {j.description && <p className="text-xs text-gray-400 mt-0.5">{j.description}</p>}
                    {j.last_run_at && <p className="text-[11px] text-gray-400 mt-0.5">Last run: {j.last_run_at}</p>}
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <button onClick={() => handleToggle(j)} disabled={acting === j.id} title={j.enabled ? 'Disable' : 'Enable'}
                      className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30 disabled:opacity-50">
                      {j.enabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
                    </button>
                    <button onClick={() => openEdit(j)} title="Edit"
                      className="p-1.5 text-gray-400 hover:text-amber-600 rounded-lg hover:bg-amber-50 dark:hover:bg-amber-900/30">
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button onClick={() => handleDelete(j)} disabled={acting === j.id} title="Delete"
                      className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 disabled:opacity-50">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </div>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}

      {showForm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl">
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">{editing ? 'Edit Cron Job' : 'New Cron Job'}</h3>
              <button onClick={() => setShowForm(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <form onSubmit={handleSave} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Command</label>
                <input type="text" value={command} onChange={(e) => setCommand(e.target.value)} placeholder="php /home/user/public_html/artisan schedule:run"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm font-mono dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" required maxLength={1000} />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Schedule (min hour day month weekday)</label>
                <input type="text" value={schedule} onChange={(e) => setSchedule(e.target.value)} placeholder="*/5 * * * *"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm font-mono dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" required />
                {presets.length > 0 && (
                  <div className="flex flex-wrap gap-1.5 mt-2">
                    {presets.map((p) => (
                      <button key={p.schedule} type="button" onClick={() => setSchedule(p.schedule)} title={p.schedule}
                        className="text-[11px] px-2 py-1 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-blue-100 dark:hover:bg-blue-900/40 hover:text-blue-700 dark:hover:text-blue-300 transition-colors">
                        {p.label}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Description (optional)</label>
                <input type="text" value={description} onChange={(e) => setDescription(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" maxLength={255} />
              </div>
              <Button type="submit" disabled={saving} loading={saving} className="w-full">
                {editing ? 'Save Changes' : 'Create Job'}
              </Button>
            </form>
          </div>
        </div>
      )}
    </motion.div>
  );
}

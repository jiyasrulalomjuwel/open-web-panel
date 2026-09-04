import { useEffect, useState, useCallback, useMemo } from 'react';
import {
  getBackups, getBackupSummary, createBackup, getBackupStatus, deleteBackup,
  restoreBackup, downloadBackup, getBackupSchedules, createBackupSchedule,
  updateBackupSchedule, deleteBackupSchedule, getDiskUsage, getChildAccount,
} from '../lib/api';
import {
  HardDrive, Plus, Trash2, Download, RotateCcw, Loader2, Database,
  FolderOpen, Layers, CalendarClock, Clock, AlertTriangle, X, CalendarDays,
  History, PlayCircle, Pencil, Info, CheckCircle2, XCircle,
} from 'lucide-react';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Card from '../components/ui/Card';
import EmptyState from '../components/ui/EmptyState';
import Modal from '../components/ui/Modal';
import ProgressBar from '../components/ui/ProgressBar';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

function ErrorBanner({ msg, onDismiss }: { msg: string; onDismiss: () => void }) {
  return (
    <div className="flex items-center gap-2 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
      <AlertTriangle className="h-4 w-4 shrink-0" />
      <span className="flex-1">{msg}</span>
      <button onClick={onDismiss} className="text-red-400 hover:text-red-600 p-0.5" aria-label="Dismiss">
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}

function humanSize(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(n >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

const typeMeta: Record<string, { label: string; icon: any; desc: string; color: string }> = {
  full: { label: 'Whole File Backup', icon: FolderOpen, desc: 'All website files in your home directory', color: 'text-blue-600 bg-blue-50' },
  files: { label: 'Whole File Backup', icon: FolderOpen, desc: 'All website files in your home directory', color: 'text-amber-600 bg-amber-50' },
  database: { label: 'Database Only', icon: Database, desc: 'Your MySQL databases', color: 'text-emerald-600 bg-emerald-50' },
};

type Tab = 'create' | 'schedule' | 'timeline';

const tabs: { key: Tab; label: string; icon: any }[] = [
  { key: 'create', label: 'Create Backup', icon: PlayCircle },
  { key: 'schedule', label: 'Automatic Backup', icon: CalendarClock },
  { key: 'timeline', label: 'Timeline', icon: History },
];

function SegmentedTabs({ value, onChange }: { value: Tab; onChange: (t: Tab) => void }) {
  return (
    <div className="inline-flex items-center gap-1 p-1 bg-gray-100 rounded-lg">
      {tabs.map(({ key, label, icon: Icon }) => (
        <button
          key={key}
          onClick={() => onChange(key)}
          className={`flex items-center gap-1.5 px-3.5 py-1.5 text-sm font-medium rounded-md transition-all ${
            value === key ? 'bg-white text-blue-700 shadow-sm' : 'text-gray-500 hover:text-gray-700'
          }`}
        >
          <Icon className="h-4 w-4" strokeWidth={1.5} /> {label}
        </button>
      ))}
    </div>
  );
}

function ToggleSwitch({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex items-center h-6 w-11 rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 ${
        checked ? 'bg-blue-600' : 'bg-gray-200'
      } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
    >
      <span
        className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform ${
          checked ? 'translate-x-[22px]' : 'translate-x-0.5'
        }`}
      />
    </button>
  );
}

export function Backups() {
  const [tab, setTab] = useState<Tab>('create');
  const [error, setError] = useState('');
  const [summary, setSummary] = useState<any>(null);
  const [backups, setBackups] = useState<any[]>([]);
  const [schedules, setSchedules] = useState<any[]>([]);
  const [storage, setStorage] = useState<{ size_mb: number; human: string } | null>(null);
  const [diskLimit, setDiskLimit] = useState<number>(0);
  const [polling, setPolling] = useState(false);

  const loadAll = useCallback(async () => {
    const [b, s, sum] = await Promise.all([
      getBackups().catch(() => null),
      getBackupSchedules().catch(() => null),
      getBackupSummary().catch(() => null),
    ]);
    if (b) {
      setBackups(b);
      const inFlight = b.some((x: any) => ['pending', 'running', 'restoring'].includes(x.status));
      setPolling(inFlight);
    }
    if (s) setSchedules(s);
    if (sum) setSummary(sum);
  }, []);

  useEffect(() => { loadAll(); }, [loadAll]);

  useEffect(() => {
    getDiskUsage().then((d) => setStorage(d)).catch(() => {});
    getChildAccount().then((a) => setDiskLimit(a?.disk_limit_mb || 0)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!polling) return;
    const iv = setInterval(loadAll, 3000);
    return () => clearInterval(iv);
  }, [polling, loadAll]);

  const hasRunning = useMemo(
    () => backups.some((b) => ['pending', 'running', 'restoring'].includes(b.status)),
    [backups],
  );

  return (
    <div className="max-w-5xl mx-auto space-y-5">
      <div>
        <h1 className="text-lg font-semibold text-gray-900">Backups</h1>
        <p className="text-sm text-gray-500 mt-0.5">
          Create, schedule, download and restore backups of your files and databases
        </p>
      </div>

      {error && <ErrorBanner msg={error} onDismiss={() => setError('')} />}

      <SummaryCards summary={summary} storage={storage} diskLimit={diskLimit} />

      <SegmentedTabs value={tab} onChange={(t) => { setTab(t); setError(''); }} />

      {tab === 'create' && (
        <CreateTab onError={setError} onChanged={loadAll} running={hasRunning} />
      )}
      {tab === 'schedule' && (
        <ScheduleTab onError={setError} schedules={schedules} onChanged={loadAll} />
      )}
      {tab === 'timeline' && (
        <TimelineTab onError={setError} backups={backups} running={hasRunning} onChanged={loadAll} />
      )}
    </div>
  );
}

/* ── Summary cards with progress bars ── */
function SummaryCards({ summary, storage, diskLimit }: { summary: any; storage: any; diskLimit: number }) {
  const usedMB = storage?.size_mb || 0;
  const pct = diskLimit > 0 ? Math.min(100, Math.round((usedMB / diskLimit) * 100)) : 0;
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
      <Card className="!p-4">
        <div className="flex items-center gap-2 text-gray-500 text-xs font-medium mb-1.5">
          <HardDrive className="h-4 w-4" /> Total Backups
        </div>
        <div className="text-xl font-semibold text-gray-900">{summary?.total_backups ?? 0}</div>
        <div className="text-[11px] text-gray-400 mt-0.5">
          {summary?.schedule_count ? `${summary.schedule_count} schedule(s)` : 'No schedules yet'}
        </div>
      </Card>
      <Card className="!p-4">
        <div className="flex items-center gap-2 text-gray-500 text-xs font-medium mb-1.5">
          <Layers className="h-4 w-4" /> Backup Storage
        </div>
        <div className="text-xl font-semibold text-gray-900">{humanSize(summary?.total_size || 0)}</div>
        <div className="text-[11px] text-gray-400 mt-1">
          Stored in <span className="font-medium text-gray-500">~/backups</span> and included in your account&apos;s total disk usage.
        </div>
        <ProgressBar value={pct} className="mt-2" variant="blue" />
        <div className="text-[11px] text-gray-400 mt-1">
          Account disk usage (incl. backups): {pct}% of {humanSize(diskLimit * 1024 * 1024)}
        </div>
      </Card>
      <Card className="!p-4">
        <div className="flex items-center gap-2 text-gray-500 text-xs font-medium mb-1.5">
          <CalendarClock className="h-4 w-4" /> Automatic Backups
        </div>
        <div className="text-xl font-semibold text-gray-900">{summary?.enabled_schedules ?? 0}</div>
        <div className="text-[11px] text-gray-400 mt-0.5">active schedule(s)</div>
      </Card>
      <Card className="!p-4">
        <div className="flex items-center gap-2 text-gray-500 text-xs font-medium mb-1.5">
          <Clock className="h-4 w-4" /> Last Backup
        </div>
        {summary?.last_backup_at ? (
          <>
            <div className="text-sm font-semibold text-gray-900 truncate">{summary.last_backup_at}</div>
            <Badge variant={summary.last_status === 'completed' ? 'success' : 'warning'} dot className="mt-1.5">
              {summary.last_status || 'unknown'}
            </Badge>
          </>
        ) : (
          <div className="text-sm text-gray-400">No backups yet</div>
        )}
      </Card>
    </div>
  );
}

/* ── Create tab ── */
function CreateTab({ onError, onChanged, running }: { onError: (m: string) => void; onChanged: () => void; running: boolean }) {
  const [backupType, setBackupType] = useState<'files' | 'database'>('files');
  const [notes, setNotes] = useState('');
  const [creating, setCreating] = useState(false);
  const [activeId, setActiveId] = useState<number | null>(null);
  const [progress, setProgress] = useState<{ status: string; progress: number; message: string } | null>(null);

  useEffect(() => {
    if (activeId == null) return;
    const iv = setInterval(async () => {
      try {
        const s = await getBackupStatus(activeId);
        setProgress(s);
        if (s.status === 'completed' || s.status === 'failed') {
          clearInterval(iv);
          setActiveId(null);
          setProgress(null);
          onChanged();
        }
      } catch { clearInterval(iv); setActiveId(null); setProgress(null); }
    }, 1000);
    return () => clearInterval(iv);
  }, [activeId, onChanged]);

  const handleCreate = async () => {
    setCreating(true);
    try {
      const res = await createBackup(backupType, notes);
      setActiveId(res.id);
      setProgress({ status: 'running', progress: 5, message: 'Starting…' });
      onChanged();
    } catch (e: any) {
      onError(e?.message || 'Failed to create backup');
    } finally {
      setCreating(false);
    }
  };

  const busy = running || creating;

  return (
    <div className="space-y-4">
      <Card>
        <div className="mb-4">
          <h3 className="text-sm font-semibold text-gray-900">Choose what to back up</h3>
          <p className="text-xs text-gray-500 mt-0.5">
            A whole-file backup archives everything in your home directory. A database backup archives your MySQL databases only.
          </p>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          {(['files', 'database'] as const).map((t) => {
            const meta = typeMeta[t];
            const Icon = meta.icon;
            const active = backupType === t;
            return (
              <button
                key={t}
                onClick={() => setBackupType(t)}
                className={`text-left p-4 rounded-xl border-2 transition-all ${
                  active
                    ? 'border-blue-500 bg-blue-50/50 ring-1 ring-blue-200'
                    : 'border-gray-200 hover:border-gray-300 bg-white'
                }`}
              >
                <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${meta.color} mb-2.5`}>
                  <Icon className="h-5 w-5" strokeWidth={1.5} />
                </div>
                <div className="text-sm font-semibold text-gray-900">{meta.label}</div>
                <div className="text-xs text-gray-500 mt-0.5">{meta.desc}</div>
              </button>
            );
          })}
        </div>

        <div className="mt-4">
          <label className="block text-xs font-medium text-gray-600 mb-1.5">Notes (optional)</label>
          <input
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            maxLength={500}
            placeholder="e.g. before updating the site theme"
            className="w-full px-3.5 py-2.5 rounded-lg border border-gray-200 text-sm text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          />
        </div>

        <div className="mt-4 flex items-center gap-3">
          <Button onClick={handleCreate} loading={busy} disabled={busy}>
            <HardDrive className="h-4 w-4" /> Create Backup
          </Button>
          {busy && <span className="text-xs text-gray-500">A backup is already in progress…</span>}
        </div>
      </Card>

      {progress && (
        <Card>
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2 text-sm font-medium text-gray-900">
              <Spinner /> Backup #{activeId} — {progress.status}
            </div>
            <span className="text-xs font-semibold text-blue-600">{progress.progress}%</span>
          </div>
          <ProgressBar value={progress.progress} variant={progress.status === 'failed' ? 'danger' : 'blue'} size="md" />
          <p className="text-xs text-gray-500 mt-2">{progress.message}</p>
        </Card>
      )}
    </div>
  );
}

/* ── Schedule tab ── */
function ScheduleTab({ onError, schedules, onChanged }: { onError: (m: string) => void; schedules: any[]; onChanged: () => void }) {
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<any | null>(null);
  const [form, setForm] = useState({
    name: 'Daily Backup',
    backup_type: 'files' as 'files' | 'database',
    frequency: 'daily' as 'daily' | 'weekly' | 'monthly',
    time: '02:00',
    day_of_week: 1,
    day_of_month: 1,
    retention: 7,
  });
  const [saving, setSaving] = useState(false);

  const openNew = () => {
    setEditing(null);
    setForm({ name: 'Daily Backup', backup_type: 'files', frequency: 'daily', time: '02:00', day_of_week: 1, day_of_month: 1, retention: 7 });
    setModalOpen(true);
  };

  const openEdit = (s: any) => {
    setEditing(s);
    const btype = s.backup_type === 'full' ? 'files' : s.backup_type;
    setForm({
      name: s.name, backup_type: btype, frequency: s.frequency, time: s.time,
      day_of_week: s.day_of_week ?? 1, day_of_month: s.day_of_month ?? 1, retention: s.retention ?? 7,
    });
    setModalOpen(true);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      if (editing) {
        await updateBackupSchedule(editing.id, form);
      } else {
        await createBackupSchedule(form);
      }
      setModalOpen(false);
      onChanged();
    } catch (e: any) {
      onError(e?.message || 'Failed to save schedule');
    } finally {
      setSaving(false);
    }
  };

  const toggleEnabled = async (s: any) => {
    try {
      await updateBackupSchedule(s.id, { enabled: !s.enabled });
      onChanged();
    } catch (e: any) {
      onError(e?.message || 'Failed to update schedule');
    }
  };

  const remove = async (s: any) => {
    if (!window.confirm(`Delete schedule "${s.name}"? Automatic backups for it will stop.`)) return;
    try {
      await deleteBackupSchedule(s.id);
      onChanged();
    } catch (e: any) {
      onError(e?.message || 'Failed to delete schedule');
    }
  };

  const freqLabel = (s: any) =>
    s.frequency === 'daily' ? `Every day at ${s.time}`
      : s.frequency === 'weekly' ? `Weekly on ${['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'][s.day_of_week ?? 1]} at ${s.time}`
      : `Monthly on day ${s.day_of_month ?? 1} at ${s.time}`;

  return (
    <Card>
      <div className="flex items-center justify-between mb-4">
        <div>
          <h3 className="text-sm font-semibold text-gray-900">Automatic Backups</h3>
          <p className="text-xs text-gray-500 mt-0.5">
            Run backups on a schedule. Old backups beyond the retention count are removed automatically.
          </p>
        </div>
        <Button size="sm" onClick={openNew}><Plus className="h-4 w-4" /> New Schedule</Button>
      </div>

      {schedules.length === 0 ? (
        <EmptyState
          icon={<CalendarClock className="h-5 w-5 text-gray-400" />}
          title="No automatic backups configured"
          message="Create a schedule to automatically back up your files and databases."
          actionLabel="Create Schedule"
          onAction={openNew}
        />
      ) : (
        <div className="space-y-3">
          {schedules.map((s) => {
            const meta = typeMeta[s.backup_type] || typeMeta.full;
            const Icon = meta.icon;
            return (
              <div key={s.id} className="flex items-center gap-4 p-4 border border-gray-200 rounded-xl hover:border-gray-300 transition-colors">
                <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${meta.color} shrink-0`}>
                  <Icon className="h-4 w-4" strokeWidth={1.5} />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-gray-900 truncate">{s.name}</span>
                    <Badge variant="info">{meta.label}</Badge>
                    {s.last_status && (
                      <Badge variant={s.last_status === 'completed' ? 'success' : s.last_status === 'failed' ? 'danger' : 'default'}>
                        last: {s.last_status}
                      </Badge>
                    )}
                  </div>
                  <div className="text-xs text-gray-500 mt-1">{freqLabel(s)} · keep {s.retention} backup(s)</div>
                  <div className="text-[11px] text-gray-400 mt-0.5">
                    {s.enabled
                      ? <>Next run: <span className="text-gray-600">{s.next_run_at || 'soon'}</span></>
                      : <span className="text-gray-400">Paused</span>}
                  </div>
                </div>
                <ToggleSwitch checked={!!s.enabled} onChange={() => toggleEnabled(s)} />
                <button onClick={() => openEdit(s)} className="p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded-md transition-colors" title="Edit">
                  <Pencil className="h-4 w-4" />
                </button>
                <button onClick={() => remove(s)} className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded-md transition-colors" title="Delete">
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            );
          })}
        </div>
      )}

      <Modal isOpen={modalOpen} onClose={() => setModalOpen(false)} title={editing ? 'Edit Schedule' : 'New Backup Schedule'}>
        <div className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-600 mb-1.5">Schedule name</label>
            <input
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              className="w-full px-3.5 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1.5">Backup type</label>
              <select
                value={form.backup_type}
                onChange={(e) => setForm({ ...form, backup_type: e.target.value as any })}
                className="w-full px-3 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="files">Whole File Backup</option>
                <option value="database">Database Only</option>
              </select>
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1.5">Frequency</label>
              <select
                value={form.frequency}
                onChange={(e) => setForm({ ...form, frequency: e.target.value as any })}
                className="w-full px-3 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="daily">Daily</option>
                <option value="weekly">Weekly</option>
                <option value="monthly">Monthly</option>
              </select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1.5">Time (24h)</label>
              <input
                type="time"
                value={form.time}
                onChange={(e) => setForm({ ...form, time: e.target.value })}
                className="w-full px-3 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            {form.frequency === 'weekly' && (
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1.5">Day of week</label>
                <select
                  value={form.day_of_week}
                  onChange={(e) => setForm({ ...form, day_of_week: Number(e.target.value) })}
                  className="w-full px-3 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  {['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'].map((d, i) => (
                    <option key={d} value={i}>{d}</option>
                  ))}
                </select>
              </div>
            )}
            {form.frequency === 'monthly' && (
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1.5">Day of month</label>
                <input
                  type="number"
                  min={1}
                  max={28}
                  value={form.day_of_month}
                  onChange={(e) => setForm({ ...form, day_of_month: Number(e.target.value) })}
                  className="w-full px-3 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
            )}
            <div>
              <label className="block text-xs font-medium text-gray-600 mb-1.5">Keep last</label>
              <input
                type="number"
                min={1}
                max={90}
                value={form.retention}
                onChange={(e) => setForm({ ...form, retention: Number(e.target.value) })}
                className="w-full px-3 py-2.5 rounded-lg border border-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <p className="text-[11px] text-gray-400 mt-1">Older backups are deleted automatically</p>
            </div>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="secondary" onClick={() => setModalOpen(false)}>Cancel</Button>
            <Button onClick={handleSave} loading={saving}>
              {editing ? 'Save Changes' : 'Create Schedule'}
            </Button>
          </div>
        </div>
      </Modal>
    </Card>
  );
}

/* ── Timeline tab ── */
function TimelineTab({ onError, backups, running, onChanged }: { onError: (m: string) => void; backups: any[]; running: boolean; onChanged: () => void }) {
  const [restoreTarget, setRestoreTarget] = useState<any | null>(null);
  const [restoreFiles, setRestoreFiles] = useState(true);
  const [restoreDb, setRestoreDb] = useState(true);
  const [restoring, setRestoring] = useState(false);

  const openRestore = (b: any) => {
    setRestoreTarget(b);
    setRestoreFiles(b.type !== 'database');
    setRestoreDb(b.type !== 'files');
    setRestoring(false);
  };

  const handleRestore = async () => {
    if (!restoreTarget) return;
    setRestoring(true);
    try {
      await restoreBackup(restoreTarget.id, restoreFiles, restoreDb);
      setRestoreTarget(null);
      onChanged();
    } catch (e: any) {
      onError(e?.message || 'Failed to start restore');
      setRestoring(false);
    }
  };

  const handleDownload = async (b: any) => {
    try {
      await downloadBackup(b.id, `backup_${b.created_at.replace(/[: ]/g, '_')}.tar.gz`);
    } catch (e: any) {
      onError(e?.message || 'Download failed');
    }
  };

  const handleDelete = async (b: any) => {
    if (!window.confirm(`Delete backup #${b.id} from ${b.created_at}? The archive will be permanently removed.`)) return;
    try {
      await deleteBackup(b.id);
      onChanged();
    } catch (e: any) {
      onError(e?.message || 'Failed to delete backup');
    }
  };

  const statusBadge = (s: string) => {
    if (s === 'completed') return <Badge variant="success" dot>Completed</Badge>;
    if (s === 'failed') return <Badge variant="danger" dot>Failed</Badge>;
    if (s === 'restoring') return <Badge variant="warning" dot>Restoring…</Badge>;
    return <Badge variant="info" dot>In progress</Badge>;
  };

  return (
    <Card>
      <div className="mb-4">
        <h3 className="text-sm font-semibold text-gray-900">Backup Timeline</h3>
        <p className="text-xs text-gray-500 mt-0.5">
          Every backup you create or that runs automatically appears here. Download completed backups or restore from them.
        </p>
        <p className="text-[11px] text-gray-400 mt-1">
          Backup files are ordinary files stored in your <span className="font-medium text-gray-500">~/backups</span> folder — they count toward your disk usage and can be managed like any other file in the File Manager.
        </p>
      </div>

      {backups.length === 0 ? (
        <EmptyState
          icon={<History className="h-5 w-5 text-gray-400" />}
          title="No backups yet"
          message="Create your first backup or set up automatic backups to see a timeline here."
        />
      ) : (
        <div className="relative pl-6">
          <div className="absolute left-[7px] top-2 bottom-2 w-px bg-gray-200" />
          <div className="space-y-4">
            {backups.map((b) => {
              const meta = typeMeta[b.type] || typeMeta.full;
              const Icon = meta.icon;
              const inFlight = ['pending', 'running', 'restoring'].includes(b.status);
              return (
                <div key={b.id} className="relative">
                  <span
                    className={`absolute -left-6 top-1.5 w-3.5 h-3.5 rounded-full border-2 border-white ${
                      b.status === 'completed' ? 'bg-emerald-500'
                        : b.status === 'failed' ? 'bg-red-500'
                        : 'bg-blue-500 animate-pulse'
                    }`}
                  />
                  <div className="border border-gray-200 rounded-xl p-4 hover:border-gray-300 transition-colors">
                    <div className="flex items-start gap-3">
                      <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${meta.color} shrink-0`}>
                        <Icon className="h-4 w-4" strokeWidth={1.5} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-semibold text-gray-900">Backup #{b.id}</span>
                          <Badge variant="info">{meta.label}</Badge>
                          <Badge variant={b.trigger === 'schedule' ? 'neutral' : 'default'}>
                            {b.trigger === 'schedule' ? 'Automatic' : 'Manual'}
                          </Badge>
                          {statusBadge(b.status)}
                        </div>
                        <div className="text-xs text-gray-500 mt-1">
                          {b.created_at} · {humanSize(b.file_size)}
                        </div>
                        {b.notes && <div className="text-xs text-gray-400 mt-0.5 italic">{b.notes}</div>}

                        {inFlight && (
                          <div className="mt-2">
                            <div className="flex items-center justify-between text-xs mb-1">
                              <span className="text-gray-500">{b.message || 'Working…'}</span>
                              <span className="font-semibold text-blue-600">{b.progress}%</span>
                            </div>
                            <ProgressBar value={b.progress} variant={b.status === 'restoring' ? 'amber' : 'blue'} size="md" />
                          </div>
                        )}

                        {!inFlight && (
                          <div className="flex items-center gap-2 mt-2">
                            {b.status === 'completed' && (
                              <>
                                <Button size="sm" variant="outline" onClick={() => handleDownload(b)}>
                                  <Download className="h-3.5 w-3.5" /> Download
                                </Button>
                                <Button size="sm" variant="secondary" onClick={() => openRestore(b)}>
                                  <RotateCcw className="h-3.5 w-3.5" /> Restore
                                </Button>
                              </>
                            )}
                            {b.status === 'failed' && (
                              <span className="text-xs text-gray-400">The backup could not be completed. Delete it and try again.</span>
                            )}
                            <button
                              onClick={() => handleDelete(b)}
                              className="ml-auto p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded-md transition-colors"
                              title="Delete"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      <Modal isOpen={!!restoreTarget} onClose={() => setRestoreTarget(null)} title={`Restore backup #${restoreTarget?.id}`}>
        {restoreTarget && (
          <div className="space-y-4">
            <div className="flex items-start gap-2 p-3 bg-amber-50 border border-amber-200 rounded-lg text-xs text-amber-800">
              <Info className="h-4 w-4 shrink-0 mt-0.5" />
              <span>
                Restoring will <strong>overwrite your current data</strong> with the contents of this backup
                from {restoreTarget.created_at}. This cannot be undone.
              </span>
            </div>

            {restoreTarget.type !== 'database' && (
              <label className={`flex items-center gap-3 p-3 border rounded-xl cursor-pointer transition-colors ${restoreFiles ? 'border-blue-500 bg-blue-50/40' : 'border-gray-200'}`}>
                <input type="checkbox" checked={restoreFiles} onChange={(e) => setRestoreFiles(e.target.checked)} className="h-4 w-4 accent-blue-600" />
                <FolderOpen className="h-4 w-4 text-amber-600" />
                <span className="text-sm text-gray-800">Restore website files</span>
              </label>
            )}
            {restoreTarget.type !== 'files' && (
              <label className={`flex items-center gap-3 p-3 border rounded-xl cursor-pointer transition-colors ${restoreDb ? 'border-blue-500 bg-blue-50/40' : 'border-gray-200'}`}>
                <input type="checkbox" checked={restoreDb} onChange={(e) => setRestoreDb(e.target.checked)} className="h-4 w-4 accent-blue-600" />
                <Database className="h-4 w-4 text-emerald-600" />
                <span className="text-sm text-gray-800">Restore databases</span>
              </label>
            )}

            <div className="flex justify-end gap-2 pt-1">
              <Button variant="secondary" onClick={() => setRestoreTarget(null)}>Cancel</Button>
              <Button variant="danger" onClick={handleRestore} loading={restoring} disabled={!restoreFiles && !restoreDb}>
                <RotateCcw className="h-4 w-4" /> Start Restore
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </Card>
  );
}

import { useEffect, useState } from 'react';
import { Cpu, MemoryStick, HardDrive, TrendingUp, Upload, Database, Mail, Globe, Shield, Users as UsersIcon, Loader2 } from 'lucide-react';
import { getAccountResourceLimits, setAccountResourceLimits, getPackages, changeAccountPackage, updateAccount, resetAccountPassword } from '../lib/api';
import Button from './ui/Button';

const FEATURE_GATES: Array<[string, string]> = [
  ['Files Access', 'feature_files'],
  ['Emails Access', 'feature_emails'],
  ['FTP Access', 'feature_ftp'],
  ['Databases Access', 'feature_db'],
  ['Backups Access', 'feature_backups'],
  ['Cron Access', 'feature_cron'],
];

// ---------- Limits + feature access (quotas, SSH, 6 gates) ----------
export function LimitsForm({ accountId, readOnly, onSaved }: { accountId: number; readOnly?: boolean; onSaved?: () => void }) {
  const [data, setData] = useState<any | null>(null);
  const [form, setForm] = useState<any | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    setLoading(true);
    getAccountResourceLimits(accountId).then((d) => {
      setData(d);
      const eff = d.effective || {};
      const ov = d.overrides || {};
      const tri = (v: any) => (v === 0 || v === 1 ? v : -1);
      setForm({
        cpu_limit: eff.cpu_limit || '',
        disk_mb: eff.disk_mb || '',
        bandwidth_mb: eff.bandwidth_mb || '',
        ram_limit_mb: eff.ram_limit_mb || '',
        upload_limit_mb: eff.upload_limit_mb || '',
        max_db: eff.max_db || '',
        max_email: eff.max_email || '',
        max_ftp: eff.max_ftp || '',
        max_domains: eff.max_domains || '',
        max_subdomains: eff.max_subdomains || '',
        ssh_access: eff.ssh_access,
        feature_files: tri(ov.feature_files),
        feature_emails: tri(ov.feature_emails),
        feature_ftp: tri(ov.feature_ftp),
        feature_db: tri(ov.feature_db),
        feature_backups: tri(ov.feature_backups),
        feature_cron: tri(ov.feature_cron),
      });
    }).catch((e: any) => setError(e?.message || e?.error || 'Failed to load limits'))
      .finally(() => setLoading(false));
  }, [accountId]);

  const save = async () => {
    if (!form) return;
    const num = (v: any) => v === '' || v === null || v === undefined ? 0 : Number(v);
    setSaving(true);
    setError('');
    try {
      await setAccountResourceLimits(accountId, {
        cpu_limit: num(form.cpu_limit),
        disk_mb: num(form.disk_mb),
        bandwidth_mb: num(form.bandwidth_mb),
        ram_limit_mb: num(form.ram_limit_mb),
        upload_limit_mb: num(form.upload_limit_mb),
        max_db: num(form.max_db),
        max_email: num(form.max_email),
        max_ftp: num(form.max_ftp),
        max_domains: num(form.max_domains),
        max_subdomains: num(form.max_subdomains),
        ssh_access: form.ssh_access === 1 ? 1 : (form.ssh_access === 0 ? 0 : -1),
        feature_files: +form.feature_files,
        feature_emails: +form.feature_emails,
        feature_ftp: +form.feature_ftp,
        feature_db: +form.feature_db,
        feature_backups: +form.feature_backups,
        feature_cron: +form.feature_cron,
      });
      onSaved?.();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to save limits');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <div className="flex items-center justify-center py-8 text-gray-400"><Loader2 className="h-5 w-5 animate-spin mr-2" /> Loading limits...</div>;
  }
  if (!form) {
    return <div className="p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error || 'Failed to load limits'}</div>;
  }
  const pkg = data?.package || {};
  const fields = [
    { key: 'cpu_limit', label: 'CPU Cores', icon: Cpu },
    { key: 'ram_limit_mb', label: 'RAM Limit (MB)', icon: MemoryStick },
    { key: 'disk_mb', label: 'Disk (MB)', icon: HardDrive },
    { key: 'bandwidth_mb', label: 'Bandwidth (MB)', icon: TrendingUp },
    { key: 'upload_limit_mb', label: 'Upload Limit (MB)', icon: Upload },
    { key: 'max_db', label: 'Max Databases', icon: Database },
    { key: 'max_email', label: 'Max Email Accounts', icon: Mail },
    { key: 'max_ftp', label: 'Max FTP Accounts', icon: UsersIcon },
    { key: 'max_domains', label: 'Max Domains', icon: Globe },
    { key: 'max_subdomains', label: 'Max Subdomains', icon: Globe },
  ];
  const inputCls = "w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-60";
  return (
    <div>
      {error && <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error}</div>}
      <p className="text-xs text-gray-400 dark:text-gray-500 mb-4">
        Package default: {pkg.disk_mb} MB disk · {pkg.bandwidth_mb} MB bandwidth · {pkg.ram_limit_mb} MB RAM ·
        {pkg.cpu_limit} CPU · {pkg.max_db} DBs · {pkg.max_email} emails · {pkg.max_ftp} FTP ·
        {pkg.max_domains} domains · {pkg.max_subdomains} subdomains · SSH {pkg.ssh_access ? 'on' : 'off'}.
        Leave a field at 0 to keep the package default.
      </p>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {fields.map((f) => (
          <div key={f.key}>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1 flex items-center gap-1.5">
              <f.icon className="h-3 w-3" /> {f.label}
            </label>
            <input type="number" min="0" step={f.key === 'cpu_limit' ? '0.1' : '1'} value={form[f.key]} disabled={readOnly}
              onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
              className={inputCls} placeholder="0 = package default" />
          </div>
        ))}
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1 flex items-center gap-1.5">
            <Shield className="h-3 w-3" /> SSH Access
          </label>
          <select value={String(form.ssh_access)} disabled={readOnly}
            onChange={(e) => setForm({ ...form, ssh_access: +e.target.value })} className={inputCls}>
            <option value="-1">Use package default</option>
            <option value="1">Enabled</option>
            <option value="0">Disabled</option>
          </select>
        </div>
        {FEATURE_GATES.map(([label, key]) => (
          <div key={key}>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1 flex items-center gap-1.5">
              <Shield className="h-3 w-3" /> {label}
            </label>
            <select value={String(form[key])} disabled={readOnly}
              onChange={(e) => setForm({ ...form, [key]: +e.target.value })} className={inputCls}>
              <option value="-1">Use package default</option>
              <option value="1">Allowed</option>
              <option value="0">Blocked</option>
            </select>
          </div>
        ))}
      </div>
      {!readOnly && (
        <div className="mt-6">
          <Button onClick={save} disabled={saving} loading={saving}>Save Limits</Button>
        </div>
      )}
    </div>
  );
}

// ---------- Package change ----------
export function PackageSection({ account, readOnly, onChanged }: { account: any; readOnly?: boolean; onChanged?: () => void }) {
  const [packages, setPackages] = useState<any[]>([]);
  const [selection, setSelection] = useState<number>(account?.package_id || 0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');

  useEffect(() => {
    getPackages().then((d) => setPackages(d || [])).catch(() => {});
    setSelection(account?.package_id || 0);
  }, [account?.package_id]);

  const save = async () => {
    if (!selection) { setError('Select a package'); return; }
    setSaving(true);
    setError('');
    setDone('');
    try {
      await changeAccountPackage(account.id, selection);
      setDone('Package changed. The container will be re-provisioned with the new limits.');
      onChanged?.();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to change package');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-md">
      {error && <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error}</div>}
      {done && <div className="mb-4 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{done}</div>}
      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Hosting Package</label>
      <select value={selection} disabled={readOnly} onChange={(e) => setSelection(+e.target.value)}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 mb-2 disabled:opacity-60">
        {packages.map((p: any) => (
          <option key={p.id} value={p.id}>{p.name}</option>
        ))}
      </select>
      {(() => {
        const selected = packages.find((p: any) => p.id === selection);
        if (!selected) return null;
        return (
          <p className="text-xs text-gray-400 dark:text-gray-500 mb-4">
            {selected.disk_mb} MB disk · {selected.bandwidth_mb} MB bandwidth · {selected.ram_limit_mb} MB RAM ·
            {selected.max_db} DBs · {selected.max_email} emails · {selected.max_ftp} FTP ·
            {selected.max_domains} domains · {selected.max_subdomains} subdomains
          </p>
        );
      })()}
      {!readOnly && (
        <Button onClick={save} disabled={saving} loading={saving}>Change Package</Button>
      )}
    </div>
  );
}

// ---------- Edit contact info ----------

// ---------- Edit contact info ----------
export function EditInfoForm({ account, readOnly, onSaved }: { account: any; readOnly?: boolean; onSaved?: () => void }) {
  const [email, setEmail] = useState(account?.email || '');
  const [domain, setDomain] = useState(account?.domain || '');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');

  useEffect(() => {
    setEmail(account?.email || '');
    setDomain(account?.domain || '');
  }, [account?.email, account?.domain]);

  const save = async () => {
    setSaving(true);
    setError('');
    setDone('');
    try {
      await updateAccount(account.id, { email, domain });
      setDone('Account info updated.');
      onSaved?.();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to update account');
    } finally {
      setSaving(false);
    }
  };

  const inputCls = "w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-60";
  return (
    <div className="max-w-md space-y-4">
      {error && <div className="p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error}</div>}
      {done && <div className="p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{done}</div>}
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Email</label>
        <input type="email" value={email} disabled={readOnly} onChange={(e) => setEmail(e.target.value)} className={inputCls} />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Primary Domain</label>
        <input type="text" value={domain} disabled={readOnly} onChange={(e) => setDomain(e.target.value)} className={inputCls} />
        <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
          Changing the domain rewrites the primary nginx vhost and updates the domains table.
        </p>
      </div>
      {!readOnly && (
        <Button onClick={save} disabled={saving} loading={saving}>Save Changes</Button>
      )}
    </div>
  );
}

// ---------- Reset password ----------
export function PasswordForm({ accountId }: { accountId: number }) {
  const [value, setValue] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');

  const save = async () => {
    if (value.length < 8) { setError('Password must be at least 8 characters'); return; }
    setSaving(true);
    setError('');
    setDone('');
    try {
      await resetAccountPassword(accountId, value);
      setValue('');
      setDone('Password reset. The user will need to log in with the new password.');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to reset password');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-md">
      {error && <div className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error}</div>}
      {done && <div className="mb-4 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{done}</div>}
      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">New Password</label>
      <input type="password" value={value} onChange={(e) => setValue(e.target.value)}
        className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
        placeholder="Min. 8 characters" />
      <div className="mt-4">
        <Button onClick={save} disabled={saving} loading={saving}>Reset Password</Button>
      </div>
    </div>
  );
}

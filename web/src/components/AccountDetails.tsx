import { useEffect, useState } from 'react';
import { getChildAccount } from '../lib/api';
import { HardDrive, AlertCircle } from 'lucide-react';

function fmt(mb: number) {
  if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB';
  return mb.toFixed(0) + ' MB';
}

export function AccountDetails() {
  const [account, setAccount] = useState<any>(null);
  const [updatedAt, setUpdatedAt] = useState<number | null>(null);
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    let live = true;
    let iv: ReturnType<typeof setInterval> | null = null;
    const fetch = () => {
      getChildAccount()
        .then((a) => {
          if (!live) return;
          setAccount(a);
          setUpdatedAt(Date.now());
        })
        .catch(() => null);
    };
    fetch();
    // Refresh every 15s so counts and usage track reality; the tick below
    // re-renders the "updated Xs ago" label.
    iv = setInterval(fetch, 15_000);
    const tick = setInterval(() => { if (live) setNow(Date.now()); }, 5_000);
    const onFocus = () => fetch();
    window.addEventListener('focus', onFocus);
    return () => {
      live = false;
      if (iv) clearInterval(iv);
      clearInterval(tick);
      window.removeEventListener('focus', onFocus);
    };
  }, []);

  if (!account) return null;

  const age = updatedAt == null ? null : Math.max(0, Math.round((now - updatedAt) / 1000));
  const ageLabel = age == null ? '' : age < 5 ? 'just now' : age < 60 ? `${age}s ago` : `${Math.floor(age / 60)}m ago`;

  const rl = account?.ram_limit_mb || 0;
  const ru = account?.ram_used_mb || 0;
  const dl = account?.disk_limit_mb || 0;
  const du = account?.disk_used_mb || 0;
  const bl = account?.bandwidth_limit_mb || 0;
  const bu = account?.bandwidth_used_mb || 0;

  const usageResources = [
    { label: 'Domains', used: account.total_domains ?? 0, max: account.max_domains ?? 0 },
    { label: 'Databases', used: account.databases_used ?? 0, max: account.max_databases ?? 0 },
    { label: 'Email', used: account.emails_used ?? 0, max: account.max_email ?? 0 },
    { label: 'FTP', used: account.ftp_used ?? 0, max: account.max_ftp ?? 0 },
    { label: 'Disk', used: du, max: dl, format: true },
    { label: 'Bandwidth', used: bu, max: bl, format: true },
    { label: 'RAM', used: ru, max: rl, format: true },
  ];

  const resourceWarning = account?.resource_warning || '';

  return (
    <>
      {resourceWarning && (
        <div className="flex items-start gap-3 bg-amber-50 border border-amber-200 rounded-card p-4 mb-4">
          <AlertCircle className="w-5 h-5 text-amber-500 shrink-0 mt-0.5" strokeWidth={1.5} />
          <div>
            <p className="text-sm font-semibold text-amber-800">Resource Usage Warning</p>
            <p className="text-xs text-amber-700 mt-1 leading-relaxed">{resourceWarning}</p>
          </div>
        </div>
      )}
    <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
      <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
      <div className="px-4 py-3 border-b border-border-subtle flex items-center gap-2">
        <HardDrive className="w-3.5 h-3.5 text-[#2563EB]" strokeWidth={2} />
        <h2 className="text-[11px] font-semibold text-gray-900 uppercase tracking-wide">Account Details</h2>
        {ageLabel && <span className="ml-auto text-[10px] text-gray-400">Updated {ageLabel}</span>}
      </div>
      <div className="divide-y divide-border-subtle">
        {usageResources.map(({ label, used, max, format }) => {
          const pct = max > 0 ? Math.min(Math.round((used / max) * 100), 100) : 0;
          const barColor = pct >= 90 ? 'bg-[#EF4444]' : pct >= 70 ? 'bg-[#F59E0B]' : 'bg-[#2563EB]';
          return (
            <div key={label} className="px-4 py-2.5">
              <div className="flex items-center justify-between">
                <span className="text-[11px] text-gray-500">{label}</span>
                <span className="text-[11px] font-semibold text-gray-900">
                  {format ? fmt(used) : used}{' '}
                  <span className="text-gray-400 font-normal">/ {max === 0 ? '∞' : format ? fmt(max) : max}</span>
                </span>
              </div>
              {max > 0 && (
                <div className="w-full h-1.5 bg-gray-100 rounded-full overflow-hidden mt-1.5">
                  <div
                    className={`h-full rounded-full transition-all duration-500 ${barColor}`}
                    style={{ width: `${pct}%` }}
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
      <div className="border-t border-border-subtle bg-gray-50/50 px-4 py-2.5 space-y-1.5">
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-gray-500">Package</span>
          <span className="text-[11px] font-semibold text-gray-900 truncate max-w-[100px] text-right">{account?.package_name || '—'}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-gray-500">Server IP</span>
          <span className="text-[11px] font-semibold text-gray-900">{account?.shared_ip || '—'}</span>
        </div>
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-gray-500">Domain</span>
          <span className="text-[11px] font-semibold text-gray-900 truncate max-w-[100px] text-right">{account?.domain || '—'}</span>
        </div>
      </div>
    </div>
    </>
  );
}

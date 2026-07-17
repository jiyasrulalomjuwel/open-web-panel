import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getDiskUsage, getChildAccount, getAvailablePhpVersions, getCurrentPhpVersion, selectPhpVersion } from '../lib/api';
import {
  Globe, Database, FolderOpen, Mail, HardDrive, Activity,
  Layers, Share2, Code2, CheckCircle, AlertCircle, ChevronRight,
  ArrowRight, Shield, Server, TrendingUp, MemoryStick
} from 'lucide-react';
import Button from '../components/ui/Button';

function fmt(mb: number) {
  if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB';
  return mb.toFixed(0) + ' MB';
}

export function ChildDashboard() {
  const [disk, setDisk] = useState<any>(null);
  const [account, setAccount] = useState<any>(null);
  const [phpVersions, setPhpVersions] = useState<any[]>([]);
  const [currentPhp, setCurrentPhp] = useState<any>(null);
  const [selectedPhpId, setSelectedPhpId] = useState<number>(0);
  const [switchingPhp, setSwitchingPhp] = useState(false);
  const [phpMessage, setPhpMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();

  useEffect(() => {
    let mounted = true;
    Promise.all([
      getDiskUsage().catch((e: any) => { console.error('Load disk:', e); return null; }),
      getChildAccount().catch((e: any) => { console.error('Load account:', e); return null; }),
      getAvailablePhpVersions().catch((e: any) => { console.error('Load PHP:', e); return []; }),
      getCurrentPhpVersion().catch((e: any) => { console.error('Load current PHP:', e); return null; }),
    ]).then(([d, a, pv, cur]) => {
      if (!mounted) return;
      if (d) setDisk(d);
      if (a) setAccount(a);
      const versions = Array.isArray(pv) ? pv : [];
      setPhpVersions(versions);
      setCurrentPhp(cur || null);
      if (cur?.id > 0) setSelectedPhpId(cur.id);
      else if (versions.length > 0) setSelectedPhpId(versions[0].id);
    }).finally(() => { if (mounted) setLoading(false); });
    return () => { mounted = false; };
  }, []);

  const handlePhpSwitch = async () => {
    if (!selectedPhpId) return;
    setSwitchingPhp(true);
    setPhpMessage(null);
    try {
      const res = await selectPhpVersion(selectedPhpId);
      setPhpMessage({ type: 'success', text: `Switched to PHP ${res.version}` });
      setCurrentPhp((prev: any) => ({ ...(prev || {}), id: selectedPhpId, version: res.version }));
    } catch (err: any) {
      setPhpMessage({ type: 'error', text: err?.error || 'Switch failed' });
    } finally {
      setSwitchingPhp(false);
    }
  };

  const du = disk?.size_mb || 0;
  const dh = disk?.human || '0 B';
  const dl = account?.disk_limit_mb || 0;
  const bu = account?.bandwidth_used_mb || 0;
  const bl = account?.bandwidth_limit_mb || 0;
  const ru = account?.ram_used_mb || 0;
  const rl = account?.ram_limit_mb || 0;
  const ramWarning = account?.ram_warning || '';

  const diskPct = dl > 0 ? Math.round((du / dl) * 100) : 0;
  const bwPct = bl > 0 ? Math.round((bu / bl) * 100) : 0;
  const ramPct = rl > 0 ? Math.round((ru / rl) * 100) : 0;

  const stats = [
    { label: 'Total Domains', value: account?.total_domains ?? 0, badge: null },
    { label: 'Addon Domains', value: account?.addon_domains ?? 0, badge: null },
    { label: 'Subdomains', value: account?.subdomains ?? 0, badge: null },
    { label: 'Databases', value: account?.databases_used ?? 0, badge: null },
    { label: 'Email Accounts', value: account?.emails_used ?? 0, badge: null },
  ];

  const quickActions = [
    { to: '/child/domains', label: 'Domains', icon: Globe },
    { to: '/child/databases', label: 'Databases', icon: Database },
    { to: '/child/files', label: 'File Manager', icon: FolderOpen },
    { to: '/child/emails', label: 'Email', icon: Mail },
    { to: '/child/ssl', label: 'SSL', icon: Shield },
    { to: '/child/ftp', label: 'FTP', icon: Server },
  ];

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto space-y-6 animate-pulse">
        <div className="h-8 w-48 bg-gray-100 rounded-lg" />
        <div className="grid grid-cols-3 gap-5">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="bg-white rounded-card border border-border-subtle p-6 space-y-3">
              <div className="h-3 w-20 bg-gray-100 rounded" />
              <div className="h-8 w-16 bg-gray-100 rounded" />
              <div className="h-5 w-12 bg-gray-50 rounded-full" />
            </div>
          ))}
        </div>
        <div className="grid grid-cols-2 gap-5">
          {[...Array(2)].map((_, i) => (
            <div key={i} className="bg-white rounded-card border border-border-subtle p-6 space-y-3">
              <div className="h-4 w-24 bg-gray-100 rounded" />
              <div className="h-2 w-full bg-gray-100 rounded-full" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto space-y-8">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-[32px] font-bold text-gray-900 tracking-tight">
            Welcome back, {account?.username || 'User'}
          </h1>
          <p className="text-base text-gray-500 mt-1.5">
            {account?.domain || 'No domain'} &middot; {account?.shared_ip || 'Detecting...'}
          </p>
        </div>
      </div>

      {ramWarning && (
        <div className="flex items-start gap-3 bg-amber-50 border border-amber-200 rounded-card p-4">
          <AlertCircle className="w-5 h-5 text-amber-500 shrink-0 mt-0.5" strokeWidth={1.5} />
          <div>
            <p className="text-sm font-semibold text-amber-800">RAM Usage Warning</p>
            <p className="text-xs text-amber-700 mt-1 leading-relaxed">{ramWarning}</p>
          </div>
        </div>
      )}

      <div className="space-y-8">
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-5">
            {stats.map(({ label, value }) => (
              <div key={label} className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200">
                <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
                <div className="p-6">
                  <p className="text-xs font-medium text-gray-400 uppercase tracking-wide">{label}</p>
                  <p className="text-[34px] font-bold text-gray-900 leading-none mt-2">{value}</p>
                </div>
              </div>
            ))}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200">
              <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
              <div className="p-6">
                <div className="flex items-center gap-3 mb-5">
                  <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
                    <HardDrive className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
                  </div>
                  <div>
                    <p className="text-[13px] font-semibold text-gray-900">Disk Usage</p>
                    <p className="text-xs text-gray-400 mt-0.5">{dh} of {dl === 0 ? 'Unlimited' : fmt(dl)} used</p>
                  </div>
                </div>
                <div className="w-full h-2.5 bg-gray-100 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all duration-700 ease-out ${
                      diskPct >= 90 ? 'bg-[#EF4444]' : diskPct >= 70 ? 'bg-[#F59E0B]' : 'bg-[#2563EB]'
                    }`}
                    style={{ width: `${Math.min(diskPct, 100)}%` }}
                  />
                </div>
                <div className="flex items-center justify-between mt-2.5">
                  <span className="text-xs text-gray-400">{diskPct}% used</span>
                  <span className="text-xs text-gray-400">{dl === 0 ? 'Unlimited' : fmt(dl)} limit</span>
                </div>
              </div>
            </div>

            <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200">
              <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
              <div className="p-6">
                <div className="flex items-center gap-3 mb-5">
                  <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
                    <Activity className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
                  </div>
                  <div>
                    <p className="text-[13px] font-semibold text-gray-900">Bandwidth</p>
                    <p className="text-xs text-gray-400 mt-0.5">{fmt(bu)} of {bl === 0 ? 'Unlimited' : fmt(bl)} used</p>
                  </div>
                </div>
                <div className="w-full h-2.5 bg-gray-100 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all duration-700 ease-out ${
                      bwPct >= 90 ? 'bg-[#EF4444]' : bwPct >= 70 ? 'bg-[#F59E0B]' : 'bg-[#2563EB]'
                    }`}
                    style={{ width: `${Math.min(bwPct, 100)}%` }}
                  />
                </div>
                <div className="flex items-center justify-between mt-2.5">
                  <span className="text-xs text-gray-400">{bwPct}% used</span>
                  <span className="text-xs text-gray-400">{bl === 0 ? 'Unlimited' : fmt(bl)} limit</span>
                </div>
              </div>
            </div>

            <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200">
              <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
              <div className="p-6">
                <div className="flex items-center gap-3 mb-5">
                  <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
                    <MemoryStick className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
                  </div>
                  <div>
                    <p className="text-[13px] font-semibold text-gray-900">RAM Usage</p>
                    <p className="text-xs text-gray-400 mt-0.5">{fmt(ru)} of {rl === 0 ? 'Unlimited' : fmt(rl)} used</p>
                  </div>
                </div>
                <div className="w-full h-2.5 bg-gray-100 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full transition-all duration-700 ease-out ${
                      ramPct >= 90 ? 'bg-[#EF4444]' : ramPct >= 70 ? 'bg-[#F59E0B]' : 'bg-[#2563EB]'
                    }`}
                    style={{ width: `${Math.min(ramPct, 100)}%` }}
                  />
                </div>
                <div className="flex items-center justify-between mt-2.5">
                  <span className="text-xs text-gray-400">{ramPct}% used</span>
                  <span className="text-xs text-gray-400">{rl === 0 ? 'Unlimited' : fmt(rl)} limit</span>
                </div>
              </div>
            </div>
          </div>

          <div>
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-xs font-medium text-gray-400 uppercase tracking-wide">Quick Actions</h2>
              <button
                onClick={() => navigate('/child/domains')}
                className="text-xs font-medium text-gray-500 hover:text-gray-700 flex items-center gap-1 transition-colors"
              >
                View All <ArrowRight className="w-3 h-3" />
              </button>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-5">
              {quickActions.map(({ to, label, icon: Icon }) => (
                <button
                  key={to}
                  onClick={() => navigate(to)}
                  className="group flex flex-col items-center gap-3 bg-white rounded-card border border-border-subtle shadow-soft p-6 hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200"
                >
                  <div className="w-11 h-11 rounded-xl bg-gray-50 flex items-center justify-center group-hover:bg-[#2563EB]/10 transition-colors duration-200">
                    <Icon className="w-5 h-5 text-gray-500 group-hover:text-[#2563EB] transition-colors duration-200" strokeWidth={1.5} />
                  </div>
                  <span className="text-[13px] font-semibold text-gray-900 group-hover:text-[#2563EB] transition-colors duration-200">{label}</span>
                </button>
              ))}
            </div>
          </div>

          <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
            <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
            <div className="p-6">
            <div className="flex items-center gap-4 mb-6">
              <Code2 className="w-5 h-5 text-gray-400" strokeWidth={1.5} />
              <div>
                <div className="flex items-center gap-3">
                  <p className="text-[13px] font-semibold text-gray-900">PHP Version</p>
                  <span className={`inline-flex items-center gap-1.5 px-3 py-0.5 rounded-full text-xs font-medium ${
                    currentPhp?.version
                      ? 'bg-[#EAF8EE] text-[#16A34A]'
                      : 'bg-gray-100 text-gray-500'
                  }`}>
                    {currentPhp?.version ? `PHP ${currentPhp.version}` : 'Default'}
                  </span>
                </div>
                <p className="text-xs text-gray-400 mt-0.5">
                  {currentPhp?.version ? `${currentPhp.version} is currently active` : 'Default server PHP'}
                </p>
              </div>
            </div>

            {phpMessage && (
              <div className={`mb-4 flex items-center gap-2.5 text-sm rounded-xl px-4 py-3 ${
                phpMessage.type === 'success'
                  ? 'bg-[#EAF8EE] text-[#16A34A]'
                  : 'bg-[#FDECEC] text-[#DC2626]'
              }`}>
                {phpMessage.type === 'success'
                  ? <CheckCircle className="w-4 h-4 shrink-0" />
                  : <AlertCircle className="w-4 h-4 shrink-0" />
                }
                {phpMessage.text}
              </div>
            )}

            {phpVersions.length > 0 ? (
              <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-3">
                <div className="flex-1 relative">
                  <select
                    value={selectedPhpId}
                    onChange={e => setSelectedPhpId(Number(e.target.value))}
                    className="w-full h-11 px-4 rounded-xl border border-border-subtle bg-white text-sm text-gray-900 focus:outline-none focus:ring-2 focus:ring-[#2563EB] focus:border-transparent appearance-none cursor-pointer"
                  >
                    {phpVersions.map(v => (
                      <option key={v.id} value={v.id}>PHP {v.version}</option>
                    ))}
                  </select>
                  <ChevronRight className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400 rotate-90 pointer-events-none" />
                </div>
                {selectedPhpId !== (currentPhp?.id || 0) && (
                  <Button size="sm" onClick={handlePhpSwitch} loading={switchingPhp}>
                    Apply Changes
                  </Button>
                )}
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-gray-400 bg-gray-50 rounded-xl px-4 py-3">
                <AlertCircle className="w-4 h-4 shrink-0" />
                No PHP versions available. Contact your administrator.
              </div>
            )}

            {phpVersions.length > 0 && selectedPhpId !== (currentPhp?.id || 0) && (
              <p className="mt-3 text-xs text-gray-400 flex items-center gap-1.5">
                <AlertCircle className="w-3 h-3" />
                Click "Apply Changes" to switch to the selected version
              </p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

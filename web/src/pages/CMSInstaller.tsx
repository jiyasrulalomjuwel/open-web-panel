import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { getCMSInstalls, getCMSInstall, checkSSLDomain, getCMSVersions, installCMS, deleteCMSInstall } from '../lib/api';
import { Globe, Plus, X, ExternalLink, AlertTriangle, Loader2, Trash2, CheckCircle, Clock, Eye, EyeOff, Copy, Shield, Unlock, Server, ChevronLeft, Database, Code } from 'lucide-react';
import { getAccessToken } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Modal from '../components/ui/Modal';

function req(method: string, path: string, body?: any): Promise<any> {
  const token = (() => {
    try { return getAccessToken(); } catch { return null; }
  })();
  return (window as any).apiRequest ? (window as any).apiRequest(method, path, body) :
    fetch(`/api/v1${path}`, {
      method,
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
      body: body ? JSON.stringify(body) : undefined,
    }).then(r => { if (!r.ok) throw r; return r.json(); });
}

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type Install = {
  id: number; domain_id: number; domain: string; cms_type: string;
  version: string; install_url: string; admin_url: string;
  admin_user: string; admin_email: string;
  status: string; created_at: string;
};

type Version = { version: string; download: string };

function passwordStrength(pw: string): { label: string; color: string; score: number } {
  let score = 0;
  if (pw.length >= 8) score += 25;
  if (pw.length >= 12) score += 15;
  if (/[a-z]/.test(pw)) score += 15;
  if (/[A-Z]/.test(pw)) score += 15;
  if (/[0-9]/.test(pw)) score += 15;
  if (/[^a-zA-Z0-9]/.test(pw)) score += 15;
  if (score >= 90) return { label: 'Very Strong', color: 'bg-emerald-500', score };
  if (score >= 70) return { label: 'Strong', color: 'bg-blue-500', score };
  if (score >= 50) return { label: 'Medium', color: 'bg-yellow-500', score };
  return { label: 'Weak', color: 'bg-red-500', score };
}

const STATUS_LABELS: Record<string, string> = {
  downloading: 'Downloading WordPress...',
  extracting: 'Extracting files...',
  configuring: 'Configuring database...',
  installed: 'Installed',
  failed: 'Failed',
};

const STATUS_ICONS: Record<string, any> = {
  downloading: Loader2,
  extracting: Loader2,
  configuring: Loader2,
  installed: CheckCircle,
  failed: AlertTriangle,
};

function WordPressLogo({ className }: { className?: string }) {
  return (
    <img
      src="https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcR4QwYmd-eH-ejLtvnpN4903KHQFjnO0wq2zNTdJ3HFvyNsSR1jwtolZQMc&s=10"
      alt="WordPress"
      className={className}
      style={{ objectFit: 'contain' }}
    />
  );
}

type Service = {
  id: string;
  name: string;
  description: string;
  icon: React.ReactNode;
  color: string;
};

const SERVICES: Service[] = [
  {
    id: 'wordpress',
    name: 'WordPress',
    description: "The world's most popular content management system. Start your blog or website in minutes.",
    icon: <WordPressLogo className="w-12 h-12" />,
    color: 'from-blue-500 to-cyan-500',
  },
];

export function CMSInstaller() {
  const [installs, setInstalls] = useState<Install[]>([]);
  const [domains, setDomains] = useState<any[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');

  const [selectedService, setSelectedService] = useState<string | null>(null);
  const [showInstall, setShowInstall] = useState(false);

  const [protocol, setProtocol] = useState('http');
  const [domainId, setDomainId] = useState(0);
  const [domainHasSSL, setDomainHasSSL] = useState(false);
  const [checkingSSL, setCheckingSSL] = useState(false);
  const [installSubdir, setInstallSubdir] = useState('');
  const [selectedVersion, setSelectedVersion] = useState('');
  const [siteName, setSiteName] = useState('');
  const [adminUser, setAdminUser] = useState('');
  const [adminPass, setAdminPass] = useState('');
  const [showAdminPass, setShowAdminPass] = useState(false);
  const [adminEmail, setAdminEmail] = useState('');
  const [installing, setInstalling] = useState(false);

  const [pollingId, setPollingId] = useState<number | null>(null);
  const [pollStatus, setPollStatus] = useState<string | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      getCMSInstalls(),
      req('GET', '/child/domains'),
      getCMSVersions(),
    ]).then(([c, d, v]) => {
      setInstalls(c || []);
      setDomains(d || []);
      setVersions(v || []);
      if (v?.length > 0 && !selectedVersion) setSelectedVersion(v[0].version);
    }).catch((e: any) => console.error('Load CMS:', e)).finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    const domain = domains.find(d => d.id === domainId);
    if (!domain) {
      setDomainHasSSL(false);
      return;
    }
    setCheckingSSL(true);
    checkSSLDomain(domain.domain)
      .then(r => setDomainHasSSL(r.has_ssl))
      .catch((e: any) => { console.error('SSL check:', e); setDomainHasSSL(false); })
      .finally(() => setCheckingSSL(false));
  }, [domainId, domains]);

  useEffect(() => {
    if (installs.some(i => i.status === 'downloading' || i.status === 'extracting' || i.status === 'configuring')) {
      const iv = setInterval(() => {
        getCMSInstalls().then(setInstalls).catch((e: any) => console.error('Poll installs:', e));
      }, 2000);
      return () => clearInterval(iv);
    }
  }, [installs]);

  useEffect(() => {
    if (!pollingId) return;
    const iv = setInterval(async () => {
      try {
        const res = await getCMSInstall(pollingId);
        setPollStatus(res.status);
        if (res.status === 'installed' || res.status === 'failed') {
          clearInterval(iv);
          setPollingId(null);
          load();
        }
      } catch (e: any) { console.error('Poll install:', e); clearInterval(iv); setPollingId(null); }
    }, 1500);
    return () => clearInterval(iv);
  }, [pollingId]);

  const selectedDomain = domains.find(d => d.id === domainId);
  const installUrl = `${protocol}://${selectedDomain?.domain || '...'}${installSubdir ? '/' + installSubdir : ''}`;

  const resetForm = () => {
    setProtocol('http');
    setDomainId(0);
    setDomainHasSSL(false);
    setInstallSubdir('');
    setSelectedVersion(versions[0]?.version || '');
    setSiteName('');
    setAdminUser('');
    setAdminPass('');
    setAdminEmail('');
  };

  const handleInstall = async () => {
    if (!domainId) return;
    setInstalling(true);
    setError('');
    try {
      const domain = domains.find(d => d.id === domainId);
      const res = await installCMS({
        domain_id: domainId,
        domain: domain?.domain || '',
        cms_type: selectedService || 'wordpress',
        version: selectedVersion,
        protocol,
        install_subdir: installSubdir,
        site_name: siteName || domain?.domain || 'My Blog',
        admin_user: adminUser || 'admin',
        admin_password: adminPass,
        admin_email: adminEmail,
      });
      setShowInstall(false);
      setPollingId(res.id);
      setPollStatus('downloading');
      setSuccess('WordPress installation started');
      resetForm();
    } catch (err: any) { setError(err?.error || 'Install failed'); }
    finally { setInstalling(false); }
  };

  const [confirmDelete, setConfirmDelete] = useState<{ id: number; domain: string } | null>(null);

  const confirmDeleteAction = async () => {
    if (!confirmDelete) return;
    try { await deleteCMSInstall(confirmDelete.id); load(); }
    catch (err: any) { setError(err?.error || 'Delete failed'); }
    finally { setConfirmDelete(null); }
  };

  const genPassword = () => {
    const c = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*';
    let p = '';
    for (let i = 0; i < 16; i++) p += c[Math.floor(Math.random() * c.length)];
    setAdminPass(p);
  };

  const selectService = (serviceId: string) => {
    setSelectedService(serviceId);
    setShowInstall(true);
    setError('');
    resetForm();
  };

  const renderProgressOverlay = () => {
    if (!pollingId || !pollStatus || pollStatus === 'installed' || pollStatus === 'failed') return null;

    const statusText = STATUS_LABELS[pollStatus] || pollStatus;
    const StatusIcon = STATUS_ICONS[pollStatus] || Loader2;

    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
        <div className="bg-white dark:bg-gray-800 rounded-xl p-8 max-w-sm mx-4 shadow-xl text-center">
          <StatusIcon className={`h-10 w-10 mx-auto mb-4 ${pollStatus === 'failed' ? 'text-red-500' : 'text-blue-500 animate-spin'}`} />
          <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-1">Installing WordPress</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mb-3">{statusText}</p>
          <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
            <div className="bg-blue-500 h-2 rounded-full animate-pulse" style={{
              width:
                pollStatus === 'downloading' ? '30%' :
                pollStatus === 'extracting' ? '60%' :
                pollStatus === 'configuring' ? '85%' : '0%'
            }} />
          </div>
          <p className="text-xs text-gray-400 dark:text-gray-500 mt-3">Please wait, this may take a few minutes...</p>
        </div>
      </div>
    );
  };

  const statusBadge = (s: string) => {
    const colors: Record<string, string> = {
      installed: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
      downloading: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
      extracting: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
      configuring: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
      failed: 'bg-red-100 text-red-700 dark:bg-red-900/50 dark:text-red-300',
    };
    const Icon = STATUS_ICONS[s];
    return (
      <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${colors[s] || 'bg-gray-100 text-gray-500'}`}>
        {Icon && <Icon className={`h-3 w-3 ${s !== 'installed' && s !== 'failed' ? 'animate-spin' : ''}`} />}
        {STATUS_LABELS[s] || s}
      </span>
    );
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).catch((e: any) => console.error('Copy:', e));
  };

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      {renderProgressOverlay()}

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">CMS Installer</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {selectedService ? `Configure ${SERVICES.find(s => s.id === selectedService)?.name || selectedService}` : `${installs.length} installation(s)`}
          </p>
        </div>
        {!selectedService && (
          <Button onClick={() => selectService('wordpress')}>
            <Plus className="h-4 w-4" /> New Installation
          </Button>
        )}
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4 shrink-0" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {!selectedService && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {SERVICES.map((service, idx) => (
            <motion.div
              key={service.id}
              initial={{ opacity: 0, y: 20 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: idx * 0.08 }}
            >
              <button type="button" onClick={() => selectService(service.id)} className="w-full text-left">
                <Card hover className="cursor-pointer group">
                  <div className="flex flex-col items-center text-center p-4">
                    <div className={`w-20 h-20 rounded-2xl bg-gradient-to-br ${service.color} flex items-center justify-center mb-4 shadow-lg group-hover:scale-105 transition-transform`}>
                      {service.icon}
                    </div>
                    <h3 className="font-semibold text-gray-900 dark:text-gray-100 text-lg">{service.name}</h3>
                    <p className="text-sm text-gray-500 dark:text-gray-400 mt-1.5 leading-relaxed">{service.description}</p>
                    <Button size="sm" className="mt-4">
                      <Plus className="h-3.5 w-3.5" /> Install {service.name}
                    </Button>
                  </div>
                </Card>
              </button>
            </motion.div>
          ))}
        </div>
      )}

      {loading ? (
        <div className="space-y-3">{[1,2].map(i => <Card key={i}><div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 animate-pulse" /></Card>)}</div>
      ) : installs.length === 0 ? (
        <EmptyState
          icon={<Globe size={28} className="text-gray-400" />}
          title="No CMS installations yet"
          message="Choose a service above to install"
        />
      ) : (
        <div>
          <h2 className="text-sm font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-3">Your Installations</h2>
          <div className="space-y-3">
            {installs.map((i, idx) => (
              <motion.div
                key={i.id}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: idx * 0.05 }}
              >
                <Card hover>
                  <div className="flex items-start justify-between">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <Globe className="h-5 w-5 text-blue-500 shrink-0" />
                        <h3 className="font-medium text-gray-900 dark:text-gray-100 truncate">WordPress {i.version || ''}</h3>
                        {statusBadge(i.status)}
                      </div>
                      <p className="text-sm text-gray-500 dark:text-gray-400 truncate">Domain: {i.domain}{i.install_url ? ` — ${i.install_url}` : ''}</p>
                      {i.status === 'installed' && (
                        <div className="mt-2 space-y-1">
                          <div className="flex items-center gap-2 text-xs">
                            <span className="text-gray-400 dark:text-gray-500">Admin:</span>
                            <span className="text-gray-700 dark:text-gray-300 font-mono">{i.admin_user}</span>
                            <button onClick={() => copyToClipboard(i.admin_user)} className="text-gray-400 hover:text-gray-600"><Copy className="h-3 w-3" /></button>
                          </div>
                          {i.admin_email && (
                            <div className="flex items-center gap-2 text-xs">
                              <span className="text-gray-400 dark:text-gray-500">Email:</span>
                              <span className="text-gray-700 dark:text-gray-300">{i.admin_email}</span>
                            </div>
                          )}
                          <div className="flex items-center gap-3 text-xs">
                            {i.install_url && (
                              <a href={i.install_url} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 flex items-center gap-1">
                                Visit Site <ExternalLink className="h-3 w-3" />
                              </a>
                            )}
                            {i.admin_url && (
                              <a href={i.admin_url} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 flex items-center gap-1">
                                WP Admin <ExternalLink className="h-3 w-3" />
                              </a>
                            )}
                          </div>
                        </div>
                      )}
                      <p className="text-xs text-gray-400 dark:text-gray-500 mt-1 flex items-center gap-1"><Clock className="h-3 w-3" /> {new Date(i.created_at).toLocaleString()}</p>
                    </div>
                    <button onClick={() => setConfirmDelete({ id: i.id, domain: i.domain })} className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 shrink-0 ml-2"><Trash2 className="h-4 w-4" /></button>
                  </div>
                </Card>
              </motion.div>
            ))}
          </div>
        </div>
      )}

      <Modal isOpen={showInstall} onClose={() => setShowInstall(false)} title={`Install ${SERVICES.find(s => s.id === selectedService)?.name || 'CMS'}`} size="lg">
        <p className="text-xs text-gray-500 dark:text-gray-400 -mt-3 mb-4">Configure your site in seconds</p>
        <div className="space-y-5">
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Domain</label>
            <select value={domainId} onChange={e => { setDomainId(Number(e.target.value)); setProtocol('http'); }}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
              <option value={0}>Select a domain...</option>
              {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
            </select>
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Protocol</label>
            <div className="flex gap-2">
              <button type="button" onClick={() => setProtocol('http')}
                className={`flex-1 py-2.5 rounded-lg text-sm font-medium transition-all flex items-center justify-center gap-2 ${
                  protocol === 'http'
                    ? 'bg-blue-600 text-white shadow-sm'
                    : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                }`}>
                <Unlock className="h-4 w-4" />
                HTTP
              </button>
              <button type="button" onClick={() => domainHasSSL && setProtocol('https')}
                className={`flex-1 py-2.5 rounded-lg text-sm font-medium transition-all flex items-center justify-center gap-2 ${
                  !domainHasSSL
                    ? 'bg-gray-50 dark:bg-gray-700/50 text-gray-300 dark:text-gray-600 cursor-not-allowed'
                    : protocol === 'https'
                      ? 'bg-blue-600 text-white shadow-sm'
                      : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                }`}
                title={domainHasSSL ? '' : 'SSL certificate required'}>
                <Shield className="h-4 w-4" />
                HTTPS
                {checkingSSL && <Spinner />}
                {!domainHasSSL && !checkingSSL && domainId !== 0 && <span className="ml-1 text-[10px] text-gray-400 font-normal">(SSL needed)</span>}
              </button>
            </div>
            {protocol === 'https' && domainHasSSL && (
              <p className="text-xs text-emerald-600 dark:text-emerald-400 mt-1 flex items-center gap-1">
                <Shield className="h-3 w-3" /> SSL certificate active
              </p>
            )}
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Version</label>
            <select value={selectedVersion} onChange={e => setSelectedVersion(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
              {versions.length === 0 && <option value="">Loading...</option>}
              {versions.map(v => (
                <option key={v.version} value={v.version}>{v.version}</option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Install in subdirectory <span className="text-gray-400 font-normal">(optional)</span></label>
            <input type="text" value={installSubdir} onChange={e => setInstallSubdir(e.target.value.replace(/[^a-zA-Z0-9_\-]/g, ''))}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="Leave empty for root installation" />
            {selectedDomain && (
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Will be available at: <span className="font-mono text-blue-600 dark:text-blue-400">{installUrl}</span>
              </p>
            )}
          </div>

          <hr className="border-gray-200 dark:border-gray-700" />

          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Site Name</label>
            <input type="text" value={siteName} onChange={e => setSiteName(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder={selectedDomain?.domain || 'My Blog'} />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Admin Username</label>
            <input type="text" value={adminUser} onChange={e => setAdminUser(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="admin" />
          </div>

          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Admin Email</label>
            <input type="email" value={adminEmail} onChange={e => setAdminEmail(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              placeholder="admin@example.com" />
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400">Admin Password</label>
              <button onClick={genPassword} className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 font-medium">Generate</button>
            </div>
            <div className="relative">
              <input type={showAdminPass ? 'text' : 'password'} value={adminPass} onChange={e => setAdminPass(e.target.value)}
                className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="Enter a strong password" />
              <button type="button" onClick={() => setShowAdminPass(!showAdminPass)}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                {showAdminPass ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
            {adminPass && (
              <div className="mt-1.5">
                <div className="flex items-center gap-2">
                  <div className="flex-1 h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                    <div className={`h-full ${passwordStrength(adminPass).color} transition-all`} style={{ width: `${passwordStrength(adminPass).score}%` }} />
                  </div>
                  <span className={`text-xs font-medium ${
                    passwordStrength(adminPass).score >= 70 ? 'text-emerald-600 dark:text-emerald-400' :
                    passwordStrength(adminPass).score >= 50 ? 'text-yellow-600 dark:text-yellow-400' : 'text-red-600 dark:text-red-400'
                  }`}>
                    {passwordStrength(adminPass).label}
                  </span>
                </div>
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center justify-end gap-2">
          <Button variant="secondary" onClick={() => setShowInstall(false)}>Cancel</Button>
          <Button onClick={handleInstall} disabled={installing || !domainId || !adminEmail} loading={installing}>
            Install {SERVICES.find(s => s.id === selectedService)?.name || 'CMS'}
          </Button>
        </div>
      </Modal>

      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Delete CMS Install?</h3>
            <p className="text-sm text-gray-600 dark:text-gray-300 mb-5">
              This will permanently remove the CMS and its files for <span className="font-medium">{confirmDelete.domain}</span>. This action cannot be undone.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setConfirmDelete(null)}>Cancel</Button>
              <Button variant="danger" onClick={confirmDeleteAction}>Delete</Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

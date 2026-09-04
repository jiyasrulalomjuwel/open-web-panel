import { useEffect, useState, useCallback, useRef } from 'react';
import { useLocation } from 'react-router-dom';
import { motion } from 'framer-motion';
import { Globe, Plus, X, Shield, AlertTriangle, Loader2, Trash2, CheckCircle, FileText, Lock, LockOpen } from 'lucide-react';
import { getSSLCerts, getDomains, issueSSLCert, deleteSSLCert, installCustomSSLCert, setDomainForceHTTPS } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type Cert = { id: number; domain_id: number; domain: string; issuer: string; expires_at: string; auto_renew: boolean; status: string; last_error?: string; created_at: string };

export function SSLCertificates() {
  const [certs, setCerts] = useState<Cert[]>([]);
  const [domains, setDomains] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [showIssue, setShowIssue] = useState(false);
  const [domainId, setDomainId] = useState(0);
  const [issuing, setIssuing] = useState(false);
  const [deleting, setDeleting] = useState<number | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{ id: number; domain: string } | null>(null);
  const [showCustom, setShowCustom] = useState(false);
  const [customDomainId, setCustomDomainId] = useState(0);
  const [certificate, setCertificate] = useState('');
  const [privateKey, setPrivateKey] = useState('');
  const [installing, setInstalling] = useState(false);
  const [togglingHTTPS, setTogglingHTTPS] = useState<number | null>(null);

  const forceFor = (domainId: number) => domains.find((d: any) => d.id === domainId)?.force_https === true;

  const handleToggleHTTPS = async (c: Cert) => {
    setTogglingHTTPS(c.id);
    setError('');
    try {
      await setDomainForceHTTPS(c.domain_id, !forceFor(c.domain_id));
      load();
    } catch (err: any) {
      setError(err?.error || err?.message || 'Failed to update HTTPS redirect');
    } finally {
      setTogglingHTTPS(null);
    }
  };

  const location = useLocation();
  const handledPrefill = useRef(false);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      getSSLCerts(),
      getDomains()
    ]).then(([c, d]) => { setCerts(c || []); setDomains(d || []); }).catch((e: any) => console.error('Load certs:', e)).finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  // Auto-open Issue SSL modal when navigated from Domains with a prefilled domain
  useEffect(() => {
    const state = location.state as { prefilledDomain?: number } | null;
    if (state?.prefilledDomain && domains.length > 0 && !handledPrefill.current) {
      handledPrefill.current = true;
      setDomainId(state.prefilledDomain);
      setShowIssue(true);
    }
  }, [location.state, domains]);

  // Poll issuing certs: only run a 3s timer while any cert is `issuing`.
  // Timer setup/teardown is driven by the `issuing` flag, not by the full
  // `certs` array, so polling doesn't recreate itself on every response.
  const anyIssuing = certs.some((c) => c.status === 'issuing');
  useEffect(() => {
    if (!anyIssuing) return;
    const iv = setInterval(() => {
      getSSLCerts().then(setCerts).catch((e: any) => console.error('Poll certs:', e));
    }, 3000);
    return () => clearInterval(iv);
  }, [anyIssuing]);


  const handleIssue = async () => {
    if (!domainId) return;
    setIssuing(true);
    try {
      const domain = domains.find(d => d.id === domainId);
      await issueSSLCert({ domain_id: domainId, domain: domain?.domain || '' });
      setShowIssue(false);
      setSuccess('SSL certificate issuance started');
      load();
    } catch (err: any) { setError(err?.error || 'Failed'); }
    finally { setIssuing(false); }
  };

  const confirmDeleteAction = async () => {
    if (!confirmDelete) return;
    setDeleting(confirmDelete.id);
    try { await deleteSSLCert(confirmDelete.id); load(); }
    catch (err: any) { setError(err?.error || 'Delete failed'); }
    finally { setDeleting(null); setConfirmDelete(null); }
  };

  const handleCustomInstall = async () => {
    if (!customDomainId || !certificate.trim() || !privateKey.trim()) return;
    setInstalling(true);
    try {
      const domain = domains.find(d => d.id === customDomainId);
      await installCustomSSLCert({ domain_id: customDomainId, certificate: certificate.trim(), private_key: privateKey.trim() });
      setShowCustom(false);
      setCertificate('');
      setPrivateKey('');
      setCustomDomainId(0);
      setSuccess('Custom SSL certificate installed for ' + (domain?.domain || ''));
      load();
    } catch (err: any) { setError(err?.error || 'Install failed'); }
    finally { setInstalling(false); }
  };

  const statusBadge = (s: string) => {
    const map: Record<string, 'success' | 'info' | 'error' | 'warning' | 'neutral'> = {
      issued: 'success',
      issuing: 'info',
      failed: 'error',
      pending: 'warning',
      'self-signed': 'warning',
    };
    return <Badge variant={map[s] || 'neutral'}>{s === 'self-signed' ? 'self-signed (untrusted)' : s}</Badge>;
  };

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">SSL Certificates</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{certs.length} certificate(s)</p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => { setShowCustom(true); setError(''); setCertificate(''); setPrivateKey(''); setCustomDomainId(0); }}>
            <FileText className="h-4 w-4" /> Custom SSL
          </Button>
          <Button variant="primary" onClick={() => { setShowIssue(true); setError(''); }}>
            <Plus className="h-4 w-4" /> Issue SSL
          </Button>
        </div>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}
      {success && <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{success}<button onClick={() => setSuccess('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      {loading ? (
        <Card><Skeleton lines={3} /></Card>
      ) : certs.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Shield className="h-10 w-10 text-gray-400" />}
            title="No SSL certificates issued"
            message="Secure your domains with free Let's Encrypt SSL certificates."
            actionLabel="Issue Certificate"
            onAction={() => setShowIssue(true)}
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {certs.map((c, i) => (
            <motion.div
              key={c.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.04 }}
            >
              <Card>
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <Shield className={`h-5 w-5 ${c.status === 'issued' ? 'text-emerald-500' : 'text-gray-300 dark:text-gray-500'}`} />
                      <h3 className="font-medium text-gray-900 dark:text-gray-100">{c.domain}</h3>
                      {statusBadge(c.status)}
                    </div>
                    <div className="text-xs text-gray-500 dark:text-gray-400 space-y-0.5 mt-2">
                      {c.issuer && <p>Issuer: {c.issuer}</p>}
                      {c.expires_at && <p>Expires: {new Date(c.expires_at).toLocaleDateString()}</p>}
                      <p>Auto-renew: {c.auto_renew ? 'Yes' : 'No'}</p>
                      <p>Created: {new Date(c.created_at).toLocaleString()}</p>
                    </div>
                    {c.status === 'self-signed' && (
                      <div className="mt-2 p-2.5 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-xs text-amber-700 dark:text-amber-300">
                        <strong>Browsers will not trust this.</strong> Let&apos;s Encrypt failed
                        {c.last_error ? `: ${c.last_error}` : '. Check that the domain points to this server and port 80 is reachable, then delete this entry and issue again.'}
                      </div>
                    )}
                    {c.status === 'failed' && c.last_error && (
                      <div className="mt-2 p-2.5 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-xs text-red-600 dark:text-red-300">
                        {c.last_error}
                      </div>
                    )}
                  </div>
                  <div className="flex items-center gap-1 shrink-0 ml-2">
                    {(c.status === 'issued' || c.status === 'self-signed') && (
                      <button onClick={() => handleToggleHTTPS(c)} disabled={togglingHTTPS === c.id}
                        className={`p-1.5 rounded-lg transition-colors ${forceFor(c.domain_id) ? 'text-emerald-600 hover:text-emerald-700 hover:bg-emerald-50 dark:hover:bg-emerald-900/30' : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700'}`}
                        title={forceFor(c.domain_id) ? 'HTTPS redirect on — click to allow HTTP' : 'HTTPS redirect off — click to force HTTPS'}>
                        {togglingHTTPS === c.id ? <Loader2 className="h-4 w-4 animate-spin" /> : forceFor(c.domain_id) ? <Lock className="h-4 w-4" /> : <LockOpen className="h-4 w-4" />}
                      </button>
                    )}
                    <button onClick={() => setConfirmDelete({ id: c.id, domain: c.domain })} disabled={deleting === c.id} className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30 disabled:opacity-40">{deleting === c.id ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}</button>
                  </div>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}

      {showIssue && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Issue SSL Certificate</h3>
              <button onClick={() => setShowIssue(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Select Domain</label>
                <select value={domainId} onChange={e => setDomainId(Number(e.target.value))}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                  <option value={0}>Choose a domain...</option>
                  {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
                </select>
              </div>
              <p className="text-xs text-gray-400 dark:text-gray-500">A Let's Encrypt SSL certificate will be issued. The domain must be publicly accessible on port 80 for domain validation.</p>
              <Button variant="primary" className="w-full" onClick={handleIssue} disabled={issuing || !domainId} loading={issuing}>
                Issue Certificate
              </Button>
            </div>
          </div>
        </div>
      )}

      {showCustom && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-lg mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Install Custom SSL Certificate</h3>
              <button onClick={() => setShowCustom(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Select Domain</label>
                <select value={customDomainId} onChange={e => setCustomDomainId(Number(e.target.value))}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                  <option value={0}>Choose a domain...</option>
                  {domains.map(d => <option key={d.id} value={d.id}>{d.domain}</option>)}
                </select>
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Certificate (PEM)</label>
                <textarea value={certificate} onChange={e => setCertificate(e.target.value)} rows={6} placeholder="-----BEGIN CERTIFICATE-----\n..."
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y" />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Private Key (PEM)</label>
                <textarea value={privateKey} onChange={e => setPrivateKey(e.target.value)} rows={6} placeholder="-----BEGIN RSA PRIVATE KEY-----\n..."
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y" />
              </div>
              <p className="text-xs text-gray-400 dark:text-gray-500">Paste your certificate and private key in PEM format. The certificate must cover the selected domain. If you have intermediate certificates, include them after the domain certificate.</p>
              <Button variant="primary" className="w-full" onClick={handleCustomInstall} disabled={installing || !customDomainId || !certificate.trim() || !privateKey.trim()} loading={installing}>
                Install Certificate
              </Button>
            </div>
          </div>
        </div>
      )}

      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Delete SSL Certificate?</h3>
            <p className="text-sm text-gray-600 dark:text-gray-300 mb-5">
              This will revoke the certificate for <span className="font-medium">{confirmDelete.domain}</span>. Traffic will fall back to HTTP. This action cannot be undone.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setConfirmDelete(null)} disabled={deleting !== null}>Cancel</Button>
              <Button variant="danger" onClick={confirmDeleteAction} loading={deleting !== null}>Delete</Button>
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

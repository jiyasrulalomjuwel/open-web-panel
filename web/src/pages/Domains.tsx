import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { getDomains, createDomain, deleteDomain, getChildAccount, issueSSLCert, setDomainForceHTTPS } from '../lib/api';
import { Globe, Plus, Trash2, ExternalLink, ArrowRight, AlertTriangle, ShieldOff, ShieldCheck, Loader2, CheckCircle, Lock, LockOpen } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import EmptyState from '../components/ui/EmptyState';

const sslBadge: Record<string, { variant: 'success' | 'info' | 'error' | 'warning' | 'neutral'; label: string }> = {
  issued:  { variant: 'success', label: 'SSL' },
  issuing: { variant: 'info',    label: 'Issuing…' },
  'self-signed': { variant: 'warning', label: 'Self-signed' },
  expired: { variant: 'warning', label: 'Expired' },
  failed:  { variant: 'error',   label: 'Failed' },
  none:    { variant: 'neutral', label: 'No SSL' },
};

// Converts an absolute doc_root (e.g. /opt/openwebpanel/homes/user/example.com)
// into the home-relative path the File Manager expects (e.g. example.com).
function toHomeRelative(docRoot: string, homeDir: string): string {
  if (!docRoot) return '/';
  if (homeDir && docRoot.startsWith(homeDir)) {
    const rel = docRoot.slice(homeDir.length).replace(/^\/+/, '');
    return rel ? '/' + rel : '/';
  }
  return docRoot;
}

export function Domains() {
  const [domains, setDomains] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [homeDir, setHomeDir] = useState('');
  const navigate = useNavigate();

  useEffect(() => {
    getChildAccount().then((a: any) => setHomeDir(a?.home_dir || '')).catch(() => {});
  }, []);

  const load = useCallback(() => {
    setLoading(true);
    getDomains().then((d) => { setDomains(d || []); setError(''); }).catch((e: any) => console.error('Load domains:', e)).finally(() => setLoading(false));
  }, []);
  useEffect(() => { let mounted = true; load(); return () => { mounted = false; }; }, [load]);

  // Poll only when domains have SSL in issuing state
  const domainsIssuing = domains.filter(d => d.ssl_status === 'issuing');
  useEffect(() => {
    if (domainsIssuing.length === 0) return;
    const iv = setInterval(() => {
      getDomains().then((d) => setDomains(d || [])).catch((e: any) => console.error('Poll domains:', e));
    }, 30_000);
    return () => clearInterval(iv);
  }, [domainsIssuing.length]);

  const domainsNoSSL = domains.filter(d => d.ssl_status !== 'issued');

  const [showAdd, setShowAdd] = useState(false);
  const [domain, setDomain] = useState('');
  const [dtype, setDtype] = useState('addon');
  const [adding, setAdding] = useState(false);
  const [deleting, setDeleting] = useState<number | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{ id: number; name: string } | null>(null);
  const [togglingHTTPS, setTogglingHTTPS] = useState<number | null>(null);
  // Post-add success state: offers one-click SSL without leaving the page.
  const [added, setAdded] = useState<{ id: number; domain: string } | null>(null);
  const [issuingSSL, setIssuingSSL] = useState(false);

  const closeAdd = () => {
    setShowAdd(false);
    setAdded(null);
    setDomain('');
  };

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    setAdding(true);
    setError('');
    try {
      const res = await createDomain(domain, dtype);
      setAdded({ id: res.id, domain: res.domain || domain });
      setDomain('');
      load();
    } catch (err: any) {
      setError(err?.error || 'Failed to add domain');
    } finally {
      setAdding(false);
    }
  };

  const handleSecureAdded = async () => {
    if (!added) return;
    setIssuingSSL(true);
    setError('');
    try {
      await issueSSLCert({ domain_id: added.id, domain: added.domain });
      closeAdd();
      load();
    } catch (err: any) {
      setError(err?.error || 'Failed to start SSL issuance');
    } finally {
      setIssuingSSL(false);
    }
  };

  const confirmDeleteAction = async () => {
    if (!confirmDelete) return;
    setDeleting(confirmDelete.id);
    try {
      await deleteDomain(confirmDelete.id);
      setConfirmDelete(null);
      load();
    } catch (err: any) {
      setError(err?.error || 'Failed to delete domain');
    } finally {
      setDeleting(null);
    }
  };

  const handleToggleHTTPS = async (d: any) => {
    setTogglingHTTPS(d.id);
    setError('');
    try {
      await setDomainForceHTTPS(d.id, !d.force_https);
      load();
    } catch (err: any) {
      setError(err?.error || err?.message || 'Failed to update HTTPS redirect');
    } finally {
      setTogglingHTTPS(null);
    }
  };

  const openInFileManager = (docRoot: string) => {
    localStorage.setItem('owp_fm_path', toHomeRelative(docRoot, homeDir));
    navigate('/child/files');
  };

  return (
    <div className="max-w-4xl mx-auto space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-gray-900">Domains</h1>
          <p className="text-sm text-gray-500 mt-0.5">
            {loading ? 'Loading...' : `${domains.length} domains configured`}
          </p>
        </div>
        <Button onClick={() => { setShowAdd(true); setError(''); }}>
          <Plus className="h-4 w-4" /> Add Domain
        </Button>
      </div>

      {/* SSL warning banner */}
      {domainsNoSSL.length > 0 && !loading && (
        <motion.div
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          className="flex items-start gap-3 p-4 bg-amber-50 border border-amber-200 rounded-md text-sm"
        >
          <ShieldOff className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
          <div className="flex-1">
            <p className="font-medium text-amber-800">
              {domainsNoSSL.length} domain{domainsNoSSL.length > 1 ? 's' : ''} without SSL protection
            </p>
            <p className="text-amber-600 mt-0.5">
              Your visitors may see a &quot;Not Secure&quot; warning. Issue free Let&apos;s Encrypt certificates for each domain.
            </p>
          </div>
        </motion.div>
      )}

      {/* Error */}
      {error && (
        <motion.div
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          className="flex items-center gap-2 p-3 bg-red-50 border border-red-200 rounded-md text-sm text-red-700"
        >
          <AlertTriangle className="h-4 w-4 shrink-0" />
          <span className="flex-1">{error}</span>
          <button onClick={() => setError('')} className="text-red-400 hover:text-red-600"><span className="text-lg leading-none">&times;</span></button>
        </motion.div>
      )}

      {/* Loading */}
      {loading && domains.length === 0 && (
        <div className="space-y-3">
          {[1, 2, 3].map((i) => (
            <Card key={i}>
              <div className="flex items-center gap-3">
                <div className="h-10 w-10 bg-gray-100 rounded-md animate-pulse" />
                <div className="flex-1 space-y-2">
                  <div className="h-4 bg-gray-100 rounded w-1/3 animate-pulse" />
                  <div className="h-3 bg-gray-50 rounded w-1/2 animate-pulse" />
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Domain list */}
      {!loading && (
        <div className="space-y-2">
          {domains.map((d, i) => {
            const sb = sslBadge[d.ssl_status] || sslBadge.none;
            return (
            <motion.div
              key={d.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.03 }}
            >
              <Card hover>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 min-w-0 flex-1">
                    <div className="p-2 rounded-md bg-gray-50 shrink-0">
                      <Globe className="h-4 w-4 text-gray-500" strokeWidth={1.5} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium text-gray-900">{d.domain}</span>
                        <Badge variant={d.type === 'primary' ? 'success' : 'info'}>{d.type}</Badge>
                        <Badge variant={sb.variant}>{sb.label}</Badge>
                      </div>
                      <div className="flex items-center gap-3 mt-1 text-xs text-gray-400">
                        <span className="font-mono truncate max-w-[180px]">~{toHomeRelative(d.doc_root, homeDir) || '/'}</span>
                        <button onClick={() => openInFileManager(d.doc_root)} className="text-blue-600 hover:text-blue-700 shrink-0 flex items-center gap-1">
                          Browse <ArrowRight className="h-3 w-3" />
                        </button>
                        <a href={`http://${d.domain}`} target="_blank" rel="noopener noreferrer" className="text-blue-600 hover:text-blue-700 shrink-0 flex items-center gap-1">
                          <ExternalLink className="h-3 w-3" /> Visit
                        </a>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0 ml-2">
                    {d.ssl_status !== 'issued' && d.ssl_status !== 'self-signed' && (
                      <button
                        onClick={() => navigate('/child/ssl', { state: { prefilledDomain: d.id } })}
                        disabled={d.ssl_status === 'issuing'}
                        className="p-1.5 text-amber-600 hover:text-amber-700 rounded-md hover:bg-amber-50 transition-colors"
                        title={d.ssl_status === 'issuing' ? 'Certificate being issued…' : 'Issue SSL certificate'}
                      >
                        {d.ssl_status === 'issuing'
                          ? <Loader2 className="h-4 w-4 animate-spin" />
                          : <ShieldOff className="h-4 w-4" />}
                      </button>
                    )}
                    {(d.ssl_status === 'issued' || d.ssl_status === 'self-signed') && (
                      <button
                        onClick={() => handleToggleHTTPS(d)}
                        disabled={togglingHTTPS === d.id}
                        className={`p-1.5 rounded-md transition-colors ${d.force_https ? 'text-emerald-600 hover:text-emerald-700 hover:bg-emerald-50' : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100'}`}
                        title={d.force_https ? 'HTTPS redirect on — click to allow HTTP' : 'HTTPS redirect off — click to force HTTPS'}
                      >
                        {togglingHTTPS === d.id
                          ? <Loader2 className="h-4 w-4 animate-spin" />
                          : d.force_https ? <Lock className="h-4 w-4" /> : <LockOpen className="h-4 w-4" />}
                      </button>
                    )}
                    {d.type !== 'primary' && (
                      <button
                        onClick={() => setConfirmDelete({ id: d.id, name: d.domain })}
                        disabled={deleting === d.id}
                        className="p-1.5 text-gray-400 hover:text-red-600 rounded-md hover:bg-red-50 transition-colors"
                        title="Remove domain"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    )}
                  </div>
                </div>
              </Card>
            </motion.div>
          );})}
          {domains.length === 0 && (
            <Card>
              <EmptyState
                icon={<Globe className="h-5 w-5 text-gray-400" />}
                title="No domains configured"
                message="Add your first domain to get started"
                actionLabel="Add Domain"
                onAction={() => setShowAdd(true)}
              />
            </Card>
          )}
        </div>
      )}

      {/* Add domain modal */}
      <Modal isOpen={showAdd} onClose={closeAdd} title={added ? 'Domain Added' : 'Add Domain'} size="sm">
        {added ? (
          <div className="space-y-4">
            <div className="flex items-start gap-3 p-3 bg-emerald-50 border border-emerald-200 rounded-md">
              <CheckCircle className="h-5 w-5 text-emerald-500 shrink-0 mt-0.5" />
              <div>
                <p className="text-sm font-medium text-emerald-800">{added.domain} is live on HTTP</p>
                <p className="text-xs text-emerald-600 mt-0.5">Files are served from your home directory. Visitors will see “Not Secure” until HTTPS is enabled.</p>
              </div>
            </div>
            <p className="text-xs text-gray-400 bg-gray-50 rounded-md p-2.5">
              Free Let&apos;s Encrypt certificate — requires the domain to point at this server and port 80 to be reachable.
            </p>
            <Button onClick={handleSecureAdded} disabled={issuingSSL} loading={issuingSSL} className="w-full">
              <ShieldCheck className="h-4 w-4" /> Secure with Free SSL
            </Button>
            <Button variant="ghost" onClick={closeAdd} className="w-full">
              Done
            </Button>
          </div>
        ) : (
        <form onSubmit={handleAdd} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Domain Name</label>
            <input
              type="text" autoFocus value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all placeholder:text-gray-400"
              placeholder="example.com"
              required
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Type</label>
            <select value={dtype} onChange={(e) => setDtype(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all">
              <option value="addon">Addon Domain</option>
              <option value="subdomain">Subdomain</option>
            </select>
          </div>
          <p className="text-xs text-gray-400 bg-gray-50 rounded-md p-2.5">
            Document root: <code className="text-blue-600 font-mono text-[11px]">~/{domain || 'domain.com'}</code>
          </p>
          <Button type="submit" disabled={adding || !domain} className="w-full" loading={adding}>
            Add Domain
          </Button>
        </form>
        )}
      </Modal>

      {/* Delete confirmation */}
      <Modal isOpen={!!confirmDelete} onClose={() => setConfirmDelete(null)} title="Remove Domain" size="sm">
        <p className="text-sm text-gray-500 mb-5">
          Remove &quot;{confirmDelete?.name}&quot;? Files will not be deleted.
        </p>
        <div className="flex items-center justify-end gap-2">
          <Button variant="ghost" onClick={() => setConfirmDelete(null)}>Cancel</Button>
          <Button variant="danger" onClick={confirmDeleteAction} disabled={deleting === confirmDelete?.id} loading={deleting === confirmDelete?.id}>
            Remove
          </Button>
        </div>
      </Modal>
    </div>
  );
}

import { useEffect, useState, useCallback, useRef } from 'react';
import { motion } from 'framer-motion';
import { Plus, X, Loader2, Server, AlertTriangle, RefreshCw, Wrench, RotateCcw, Trash2, Activity, FileText } from 'lucide-react';
import { getK8sStatus, getK8sJoinCommand, installWorkerDeps, getWorkerInstallLog, getWorkerHealth, recheckWorkerHealth, fixWorkerHealth, drainK8sNode, deleteK8sNode } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import CopyButton from '../components/ui/CopyButton';

export function Nodes() {
  const [k8s, setK8s] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showJoin, setShowJoin] = useState(false);
  const [joinCommand, setJoinCommand] = useState<string | null>(null);
  const [joinError, setJoinError] = useState('');
  const [joinLoading, setJoinLoading] = useState(false);
  const [installing, setInstalling] = useState<string | null>(null);
  const [logNode, setLogNode] = useState<string | null>(null);
  const [logLines, setLogLines] = useState('');
  const logBoxRef = useRef<HTMLDivElement>(null);
  const [healthOpen, setHealthOpen] = useState<string | null>(null);
  const [health, setHealth] = useState<Record<string, any[]>>({});
  const [healthLoading, setHealthLoading] = useState<string | null>(null);
  const [fixing, setFixing] = useState<string | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<any | null>(null);
  const [removing, setRemoving] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    getK8sStatus().then((k) => setK8s(k)).catch(() => setK8s(null)).finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
    const iv = setInterval(load, 60_000);
    return () => clearInterval(iv);
  }, [load]);

  const openJoin = async () => {
    setShowJoin(true);
    setJoinCommand(null);
    setJoinError('');
    setJoinLoading(true);
    try {
      const res = await getK8sJoinCommand();
      setJoinCommand(res.command);
    } catch (err: any) {
      setJoinError(err?.message || err?.error || 'Could not build the join command');
    } finally {
      setJoinLoading(false);
    }
  };

  const closeLogs = useCallback(() => {
    setLogNode(null);
    setLogLines('');
  }, []);

  const fetchLogTail = useCallback(async (name: string) => {
    try {
      const res = await getWorkerInstallLog(name, 300);
      if (res?.lines) setLogLines(res.lines);
      else if (res?.error && !logLines) setLogLines(`(waiting for installer output… ${res.error})`);
    } catch {
      /* keep old lines while polling */
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!logNode) return;
    fetchLogTail(logNode);
    const iv = setInterval(() => {
      fetchLogTail(logNode);
      load();
    }, 2000);
    return () => clearInterval(iv);
  }, [logNode, fetchLogTail, load]);

  useEffect(() => {
    logBoxRef.current?.scrollTo({ top: logBoxRef.current.scrollHeight });
  }, [logLines]);

  const handleInstall = async (name: string) => {
    setInstalling(name);
    setError('');
    try {
      await installWorkerDeps(name);
      setLogNode(name);
      setLogLines('');
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to start installer');
    } finally {
      setInstalling(null);
    }
  };

  const toggleHealth = async (name: string) => {
    if (healthOpen === name) {
      setHealthOpen(null);
      return;
    }
    setHealthOpen(name);
    if (!health[name]) {
      setHealthLoading(name);
      try {
        const res = await getWorkerHealth(name);
        setHealth((h) => ({ ...h, [name]: res?.checks || [] }));
      } catch {
        /* show empty */
      } finally {
        setHealthLoading(null);
      }
    }
  };

  const handleRecheck = async (name: string) => {
    setHealthLoading(name);
    setError('');
    try {
      const res = await recheckWorkerHealth(name);
      setHealth((h) => ({ ...h, [name]: res?.checks || [] }));
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Health check failed');
    } finally {
      setHealthLoading(null);
    }
  };

  const handleFix = async (name: string) => {
    setFixing(name);
    setError('');
    try {
      const res = await fixWorkerHealth(name);
      setHealth((h) => ({ ...h, [name]: res?.checks || [] }));
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Auto-fix failed');
    } finally {
      setFixing(null);
    }
  };

  const handleRemove = async () => {
    if (!confirmRemove) return;
    setRemoving(true);
    setError('');
    try {
      await deleteK8sNode(confirmRemove.name);
      setConfirmRemove(null);
      load();
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to remove node');
    } finally {
      setRemoving(false);
    }
  };

  const nodes: any[] = k8s?.nodes || [];

  // Smart units, MB/GB decimal like the rest of the panel.
  const fmtMem = (v: any): string => {
    if (v == null || v === '') return '—';
    const m = String(v).trim().match(/^([\d.]+)\s*(Ki|Mi|Gi|K|M|G|KB|MB|GB|B)?$/i);
    if (!m) return String(v);
    const n = parseFloat(m[1]);
    const u = (m[2] || 'B').toLowerCase();
    let mb: number;
    if (u === 'ki') mb = (n * 1024) / 1e6;
    else if (u === 'mi') mb = (n * 1048576) / 1e6;
    else if (u === 'gi') mb = (n * 1073741824) / 1e9 * 1000;
    else if (u === 'k' || u === 'kb') mb = (n * 1000) / 1e6;
    else if (u === 'm' || u === 'mb') mb = n;
    else if (u === 'g' || u === 'gb') mb = n * 1000;
    else mb = n / 1e6;
    if (mb >= 1000) return `${(mb / 1000).toFixed(1)} GB`;
    if (mb >= 1) return `${Math.round(mb)} MB`;
    return `${Math.round(mb * 1000)} KB`;
  };

  const fmtCPU = (v: any): string => {
    if (v == null || v === '') return '—';
    const m = String(v).trim().match(/^([\d.]+)\s*(m)?$/i);
    if (!m) return String(v);
    const millicores = m[2] ? parseFloat(m[1]) : parseFloat(m[1]) * 1000;
    if (millicores >= 1000) return `${parseFloat((millicores / 1000).toFixed(2))} CPU`;
    return `${Math.round(millicores)}m`;
  };

  const metricCell = (value: string, pct: any) => (
    <span className="text-xs text-gray-700 dark:text-gray-300 whitespace-nowrap">
      <span className="font-semibold text-gray-900 dark:text-gray-100">{value}</span>
      {pct != null && pct !== '' && <span className="text-gray-400"> ({pct}%)</span>}
    </span>
  );

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-5">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Cluster Nodes</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            K3s-powered cluster. Add workers when disk space runs out.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={load} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700" title="Refresh">
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <Button onClick={openJoin} disabled={!k8s?.active}>
            <Plus className="h-4 w-4" /> Add Node
          </Button>
        </div>
      </div>

      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}<button onClick={() => setError('')} className="ml-auto"><X className="h-4 w-4" /></button></div>}

      <Card>
        <div className="flex flex-wrap items-center gap-3">
          <div className={`p-2 rounded-lg ${k8s?.active ? 'bg-emerald-50 dark:bg-emerald-900/30' : 'bg-gray-100 dark:bg-gray-700'}`}>
            <Server className={`h-5 w-5 ${k8s?.active ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-400'}`} />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <span className="font-medium text-gray-900 dark:text-gray-100">Kubernetes (K3s)</span>
              <Badge variant={k8s?.active ? 'success' : 'neutral'}>
                {loading ? 'checking…' : k8s?.active ? 'enabled' : 'not enabled'}
              </Badge>
            </div>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
              {!k8s || k8s.code === 'no_kubeconfig'
                ? 'Run deploy/k3s/install-k3s-server.sh on the host to enable clustering.'
                : k8s.active
                  ? `${k8s.ready_count ?? 0}/${k8s.node_count ?? 0} nodes ready${k8s.server_version ? ` · server ${k8s.server_version}` : ''}`
                  : k8s.error || 'Cluster unreachable.'}
            </p>
          </div>
        </div>
      </Card>

      {loading ? (
        <div className="flex items-center justify-center py-12 text-gray-400"><Loader2 className="h-5 w-5 animate-spin mr-2" /> Loading nodes...</div>
      ) : nodes.length === 0 ? (
        <Card>
          <EmptyState
            icon={<Server className="h-10 w-10 text-gray-400" />}
            title="No Kubernetes nodes visible"
            message={k8s?.active ? 'The API is reachable but reports no nodes.' : 'Enable K3s first, then add worker nodes.'}
          />
        </Card>
      ) : (
        <Card padding={false}>
          <div className="hidden md:grid grid-cols-[minmax(0,2fr)_repeat(4,minmax(96px,auto))_auto] items-center gap-3 px-4 py-2.5 border-b border-gray-200 dark:border-gray-700 text-[11px] font-medium text-gray-400 uppercase tracking-wide">
            <span>Node</span>
            <span className="text-right">CPU</span>
            <span className="text-right">Memory</span>
            <span className="text-right">Disk</span>
            <span className="text-right">Pods</span>
            <span className="text-right">Actions</span>
          </div>
          <div className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {nodes.map((n: any, i: number) => (
            <motion.div key={n.name} initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: Math.min(i * 0.03, 0.3) }}>
              <div className="grid grid-cols-1 md:grid-cols-[minmax(0,2fr)_repeat(4,minmax(96px,auto))_auto] items-center gap-2 md:gap-3 px-4 py-3 hover:bg-gray-50/70 dark:hover:bg-gray-700/20 transition-colors">
                <div className="flex items-center gap-2 min-w-0">
                  <span className={`w-2 h-2 rounded-full shrink-0 ${n.ready ? 'bg-emerald-500' : 'bg-red-500'}`} title={n.ready ? 'ready' : 'not ready'} />
                  <div className="min-w-0">
                    <div className="flex items-center gap-1.5 flex-wrap">
                      <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate font-mono">{n.name}</span>
                      <Badge variant={n.role === 'master' ? 'warning' : 'info'}>{n.role === 'master' ? 'Master' : 'Worker'}</Badge>
                      {n.version_ok === false && <Badge variant="warning">skew</Badge>}
                    </div>
                    <div className="text-[11px] text-gray-400 font-mono truncate">
                      {n.version || 'unknown'} · {n.deps?.state === 'ready' ? 'deps ready' : n.deps?.state === 'installing' ? 'deps installing…' : n.deps?.state === 'failed' ? 'deps failed' : 'deps missing'}
                    </div>
                  </div>
                </div>
                <div className="md:text-right">{metricCell(fmtCPU(n.cpu_usage || n.cpu), n.cpu_pct)}</div>
                <div className="md:text-right">{metricCell(fmtMem(n.mem_usage || n.memory), n.mem_pct)}</div>
                <div className="md:text-right">{metricCell(n.disk_used ? fmtMem(n.disk_used) : '—', n.disk_pct)}</div>
                <div className="md:text-right"><span className="text-xs text-gray-700 dark:text-gray-300"><span className="font-semibold text-gray-900 dark:text-gray-100">{n.pods ?? '—'}</span><span className="text-gray-400">/{n.pods_cap ?? '—'}</span></span></div>
                <div className="flex items-center md:justify-end gap-1">
                  <button onClick={() => toggleHealth(n.name)} title="Health details"
                    className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30">
                    <Activity className="h-4 w-4" />
                  </button>
                  {(n.deps?.state === 'missing' || !n.deps || n.deps?.state === 'failed') && (
                    <button onClick={() => handleInstall(n.name)} disabled={installing === n.name}
                      title={n.deps?.state === 'failed' ? 'Retry install' : 'Install dependencies'}
                      className="p-1.5 text-gray-400 hover:text-emerald-600 rounded-lg hover:bg-emerald-50 dark:hover:bg-emerald-900/30 disabled:opacity-50">
                      {installing === n.name ? <Loader2 className="h-4 w-4 animate-spin" /> : n.deps?.state === 'failed' ? <RotateCcw className="h-4 w-4" /> : <Wrench className="h-4 w-4" />}
                    </button>
                  )}
                  {n.deps?.job && (
                    <button onClick={() => setLogNode(n.name)} title="Install log"
                      className="p-1.5 text-gray-400 hover:text-blue-600 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30">
                      <FileText className="h-4 w-4" />
                    </button>
                  )}
                  <button onClick={() => setConfirmRemove(n)} title="Remove node"
                    className="p-1.5 text-gray-400 hover:text-red-600 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
              {healthOpen === n.name && (
                <div className="px-4 pb-3">
                  <div className="border border-gray-200 dark:border-gray-700 rounded-lg divide-y divide-gray-100 dark:divide-gray-700/50 bg-gray-50/50 dark:bg-gray-800/40">
                    {(health[n.name] || []).map((c: any, i: number) => (
                      <div key={i} className="flex items-start gap-2 px-3 py-1.5">
                        <span className={`mt-1 w-2 h-2 rounded-full shrink-0 ${
                          c.status === 'ok' ? 'bg-emerald-500' : c.status === 'skip' ? 'bg-gray-300 dark:bg-gray-600' : 'bg-red-500'
                        }`} />
                        <div className="min-w-0">
                          <span className="text-xs font-mono font-medium text-gray-900 dark:text-gray-100">{c.name}</span>
                          {c.detail && <span className="text-xs text-gray-500 dark:text-gray-400"> — {c.detail}</span>}
                        </div>
                      </div>
                    ))}
                    {!(health[n.name] || []).length && !healthLoading && (
                      <p className="px-3 py-2.5 text-xs text-gray-400">No check results yet — the 5-minute scheduler fills this in, or press Re-check.</p>
                    )}
                  </div>
                  <div className="flex gap-2 mt-2">
                    <Button variant="secondary" size="sm" onClick={() => handleRecheck(n.name)} disabled={healthLoading === n.name} loading={healthLoading === n.name}>
                      Re-check
                    </Button>
                    <Button variant="secondary" size="sm" onClick={() => handleFix(n.name)} disabled={fixing === n.name} loading={fixing === n.name}>
                      <Wrench className="h-3.5 w-3.5" /> Fix issues
                    </Button>
                  </div>
                </div>
              )}
              {n.deps?.detail && (
                <p className="px-4 pb-2.5 text-xs text-red-500 dark:text-red-400">{n.deps.detail}</p>
              )}
            </motion.div>
          ))}
          </div>
        </Card>
      )}

      {confirmRemove && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md p-6 shadow-xl">
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Remove {confirmRemove.role === 'master' ? 'master' : 'worker'} “{confirmRemove.name}”?</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-1">
              Workloads drain automatically first. The last control-plane can never be removed.
            </p>
            <p className="text-xs text-amber-600 dark:text-amber-400 mb-5">
              {confirmRemove.role === 'master'
                ? 'Removing a master reduces cluster quorum — only proceed with 2+ control-planes.'
                : 'Sites scheduled here stop serving from this node after the drain.'}
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={() => setConfirmRemove(null)} disabled={removing}>Cancel</Button>
              <Button variant="danger" onClick={handleRemove} disabled={removing} loading={removing}>Drain & remove</Button>
            </div>
          </div>
        </div>
      )}

      {logNode && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-2xl shadow-xl flex flex-col max-h-[85vh]">
            <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
              <div>
                <h3 className="font-semibold text-gray-900 dark:text-gray-100">Installer log — {logNode}</h3>
                <p className="text-xs text-gray-400">Live refresh every 2s · auto-retries 3× on failure</p>
              </div>
              <button onClick={closeLogs} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <div ref={logBoxRef} className="flex-1 overflow-y-auto bg-gray-900 p-4 min-h-[300px]">
              <pre className="text-xs text-gray-100 whitespace-pre-wrap break-words font-mono">{logLines || 'Waiting for installer output…'}</pre>
            </div>
            <div className="px-5 py-3 border-t border-gray-200 dark:border-gray-700 flex justify-end gap-2">
              <Button variant="secondary" size="sm" onClick={() => { closeLogs(); load(); }}>Close</Button>
            </div>
          </div>
        </div>
      )}

      {showJoin && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-lg mx-4 p-6 shadow-xl">
            <div className="flex items-center justify-between mb-5">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Add Worker Node</h3>
              <button onClick={() => setShowJoin(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            {joinLoading ? (
              <div className="flex items-center justify-center py-8 text-gray-400"><Loader2 className="h-5 w-5 animate-spin mr-2" /> Building join command...</div>
            ) : joinError ? (
              <div className="p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{joinError}</div>
            ) : joinCommand ? (
              <div className="space-y-4">
                <p className="text-xs text-gray-500 dark:text-gray-400">Run this on the new server as root. The token is read from this host and the command is audit-logged.</p>
                <div className="relative bg-gray-900 rounded-lg p-4 overflow-x-auto">
                  <pre className="text-xs text-gray-100 whitespace-pre-wrap break-all pr-10">{joinCommand}</pre>
                  <div className="absolute top-2 right-2 bg-gray-800 rounded px-1">
                    <CopyButton text={joinCommand} />
                  </div>
                </div>
                <Button variant="secondary" onClick={() => { setShowJoin(false); load(); }} className="w-full">Done</Button>
              </div>
            ) : null}
          </div>
        </div>
      )}
    </motion.div>
  );
}

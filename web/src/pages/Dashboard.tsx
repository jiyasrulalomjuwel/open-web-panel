import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { getStatsOverview, getServerStatus, getAccounts, getK8sMetricsSummary } from '../lib/api';
import { Server, HardDrive, Cpu, Monitor, Globe, Clock, Activity, ArrowRight, RefreshCw, AlertTriangle, ChevronRight, Network } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import Card from '../components/ui/Card';
import Skeleton from '../components/ui/Skeleton';
import ProgressBar from '../components/ui/ProgressBar';

// Animated number that eases from 0 to the target whenever it changes.
function useCountUp(target: number, duration = 700) {
  const [value, setValue] = useState(0);
  useEffect(() => {
    let raf = 0;
    const start = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - start) / duration);
      setValue(target * (1 - Math.pow(1 - p, 3)));
      if (p < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [target, duration]);
  return value;
}

function formatUptime(hours: number) {
  if (!hours || hours < 0) return '—';
  const d = Math.floor(hours / 24);
  const h = Math.floor(hours % 24);
  if (d > 0) return `${d}d ${h}h`;
  return `${h}h`;
}

const container = { hidden: { opacity: 0 }, visible: { opacity: 1, transition: { staggerChildren: 0.07 } } };
const item = { hidden: { opacity: 0, y: 16 }, visible: { opacity: 1, y: 0, transition: { duration: 0.35, ease: 'easeOut' as const } } };

function StatTile({ value, label, cls }: { value: number; label: string; cls: string }) {
  const animated = useCountUp(value);
  return (
    <motion.div variants={item} whileHover={{ y: -3 }} transition={{ duration: 0.15 }}
      className={`${cls} rounded-lg p-3 text-center`}>
      <div className="text-2xl font-bold">{Math.round(animated)}</div>
      <div className="text-xs opacity-80">{label}</div>
    </motion.div>
  );
}

export function Dashboard() {
  const [stats, setStats] = useState<any>(null);
  const [server, setServer] = useState<any>(null);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [cluster, setCluster] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const navigate = useNavigate();

  const load = () => {
    setLoading(true);
    setError('');
    Promise.all([
      getStatsOverview(),
      getServerStatus(),
      getAccounts().then((d) => d || []),
      getK8sMetricsSummary().catch(() => null),
    ]).then(([s, srv, acc, cl]) => {
      setStats(s);
      setServer(srv);
      setAccounts(acc);
      setCluster(cl);
    }).catch((e: any) => setError(e?.message || e?.error || 'Failed to load dashboard'))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const activeCount = stats?.active_accounts ?? 0;
  const suspendedCount = stats?.suspended_accounts ?? 0;
  const pendingCount = stats?.pending_accounts ?? 0;
  const totalAccounts = activeCount + suspendedCount + pendingCount;

  // Newest first — the API returns insertion order, so sort client-side.
  const recentAccounts = [...accounts]
    .sort((a, b) => String(b.created_at || '').localeCompare(String(a.created_at || '')))
    .slice(0, 5);

  const cpuPct = Number(server?.cpu_percent ?? 0);
  const ramPct = server?.ram_total_mb > 0 ? (server.ram_used_mb / server.ram_total_mb) * 100 : 0;
  const diskPct = server?.disk_total_mb > 0 ? (server.disk_used_mb / server.disk_total_mb) * 100 : 0;

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-6"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Dashboard</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Server overview &amp; resource monitoring</p>
        </div>
        <button onClick={load} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700" title="Refresh">
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          <AlertTriangle className="h-4 w-4 shrink-0" />{error}
          <button onClick={load} className="ml-auto underline">Retry</button>
        </div>
      )}

      {loading ? (
        <div className="space-y-4">
          <Card><Skeleton lines={2} /></Card>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {[1, 2, 3].map((i) => <Card key={i}><Skeleton lines={3} /></Card>)}
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {[1, 2].map((i) => <Card key={i}><Skeleton lines={3} /></Card>)}
          </div>
        </div>
      ) : (
        <motion.div variants={container} initial="hidden" animate="visible" className="space-y-6">
          {/* Server info bar */}
          {server && (
            <motion.div variants={item}>
              <Card>
                <div className="flex flex-wrap items-center gap-x-6 gap-y-1.5 text-sm">
                  <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                    <Server className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                    <span className="text-gray-400 dark:text-gray-500">Hostname:</span>
                    <span className="font-medium text-gray-900 dark:text-gray-100">{server.hostname}</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                    <Monitor className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                    <span className="text-gray-400 dark:text-gray-500">OS:</span>
                    <span className="font-medium text-gray-900 dark:text-gray-100">{server.os}</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                    <Globe className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                    <span className="text-gray-400 dark:text-gray-500">Shared IP:</span>
                    <span className="font-medium text-blue-600 dark:text-blue-400">{server.shared_ip}</span>
                  </div>
                  <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                    <Clock className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                    <span className="text-gray-400 dark:text-gray-500">Uptime:</span>
                    <span className="font-medium text-gray-900 dark:text-gray-100">{formatUptime(server.uptime_hours)}</span>
                  </div>
                </div>
              </Card>
            </motion.div>
          )}

          {/* System resources */}
          {server && (
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <motion.div variants={item} whileHover={{ y: -3 }} transition={{ duration: 0.15 }}>
                <Card>
                  <div className="flex items-center gap-2 mb-4">
                    <div className="p-2 bg-blue-50 dark:bg-blue-900/30 rounded-lg">
                      <Cpu className="h-5 w-5 text-blue-600 dark:text-blue-400" />
                    </div>
                    <div>
                      <div className="font-medium text-gray-900 dark:text-gray-100">CPU Usage</div>
                      <div className="text-xs text-gray-400 dark:text-gray-500">Load: {server.load_1m} / {server.load_5m} / {server.load_15m}</div>
                    </div>
                  </div>
                  <ProgressBar value={cpuPct} variant="blue" />
                  <div className="mt-1 text-lg font-bold text-gray-900 dark:text-gray-100">{cpuPct.toFixed(1)}%</div>
                </Card>
              </motion.div>

              <motion.div variants={item} whileHover={{ y: -3 }} transition={{ duration: 0.15 }}>
                <Card>
                  <div className="flex items-center gap-2 mb-4">
                    <div className="p-2 bg-emerald-50 dark:bg-emerald-900/30 rounded-lg">
                      <Activity className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
                    </div>
                    <div>
                      <div className="font-medium text-gray-900 dark:text-gray-100">Memory</div>
                      <div className="text-xs text-gray-400 dark:text-gray-500">{(server.ram_used_mb / 1024).toFixed(1)} GB of {(server.ram_total_mb / 1024).toFixed(1)} GB</div>
                    </div>
                  </div>
                  <ProgressBar value={server.ram_used_mb} max={server.ram_total_mb} variant="emerald" />
                  <div className="mt-1 text-lg font-bold text-gray-900 dark:text-gray-100">{ramPct.toFixed(1)}%</div>
                </Card>
              </motion.div>

              <motion.div variants={item} whileHover={{ y: -3 }} transition={{ duration: 0.15 }}>
                <Card>
                  <div className="flex items-center gap-2 mb-4">
                    <div className="p-2 bg-orange-50 dark:bg-orange-900/30 rounded-lg">
                      <HardDrive className="h-5 w-5 text-orange-600 dark:text-orange-400" />
                    </div>
                    <div>
                      <div className="font-medium text-gray-900 dark:text-gray-100">Disk Space</div>
                      <div className="text-xs text-gray-400 dark:text-gray-500">{(server.disk_free_mb / 1024).toFixed(1)} GB free of {(server.disk_total_mb / 1024).toFixed(1)} GB</div>
                    </div>
                  </div>
                  <ProgressBar value={server.disk_used_mb} max={server.disk_total_mb} variant="amber" />
                  <div className="mt-1 text-lg font-bold text-gray-900 dark:text-gray-100">{diskPct.toFixed(1)}%</div>
                </Card>
              </motion.div>
            </div>
          )}

          {/* Account stats */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <motion.div variants={item}>
              <Card>
                <div className="flex items-center justify-between mb-4">
                  <h2 className="font-medium text-gray-900 dark:text-gray-100">Accounts Overview</h2>
                  <button onClick={() => navigate('/accounts')} className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 flex items-center gap-1">
                    Manage <ArrowRight className="h-3 w-3" />
                  </button>
                </div>
                <div className="grid grid-cols-3 gap-3">
                  <StatTile value={activeCount} label="Active" cls="bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300" />
                  <StatTile value={suspendedCount} label="Suspended" cls="bg-amber-50 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300" />
                  <StatTile value={pendingCount} label="Pending" cls="bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300" />
                </div>
                <div className="mt-3 text-xs text-gray-400 dark:text-gray-500">
                  {totalAccounts} total · {stats?.total_packages ?? 0} packages · {stats?.total_disk_used_mb != null ? (stats.total_disk_used_mb / 1024).toFixed(1) : '0.0'} GB account disk
                </div>
              </Card>
            </motion.div>

            {/* Recent accounts */}
            <motion.div variants={item}>
              <Card>
                <h2 className="font-medium text-gray-900 dark:text-gray-100 mb-3">Recent Accounts</h2>
                {recentAccounts.length === 0 ? (
                  <p className="text-sm text-gray-400 dark:text-gray-500 py-4 text-center">No accounts yet</p>
                ) : (
                  <div className="space-y-2">
                    {recentAccounts.map((a: any) => (
                      <button key={a.id} onClick={() => navigate(`/accounts/${a.id}`)}
                        className="w-full flex items-center justify-between py-1.5 px-2 -mx-2 rounded-lg border-b border-gray-50 dark:border-gray-700/50 last:border-0 hover:bg-gray-50 dark:hover:bg-gray-700/30 transition-colors text-left">
                        <div>
                          <div className="text-sm font-medium text-gray-700 dark:text-gray-300 flex items-center gap-1">
                            {a.username} <ChevronRight className="h-3 w-3 text-gray-300 dark:text-gray-600" />
                          </div>
                          <div className="text-xs text-gray-400 dark:text-gray-500">{a.domain}</div>
                        </div>
                        <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${
                          a.status === 'active' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' :
                          a.status === 'suspended' ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400'
                        }`}>{a.status}</span>
                      </button>
                    ))}
                  </div>
                )}
              </Card>
            </motion.div>
          </div>

          {/* Cluster totals (from SQLite metrics snapshots) */}
          {cluster && (cluster.totals?.nodes ?? 0) > 0 && (
            <motion.div variants={item}>
              <Card>
                <div className="flex items-center justify-between mb-4">
                  <h2 className="font-medium text-gray-900 dark:text-gray-100 flex items-center gap-2">
                    <Network className="h-4 w-4 text-gray-400" /> Cluster Resources
                  </h2>
                  <button onClick={() => navigate('/nodes')} className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 flex items-center gap-1">
                    Nodes <ArrowRight className="h-3 w-3" />
                  </button>
                </div>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                  <StatTile value={cluster.totals.nodes} label="Nodes" cls="bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300" />
                  <StatTile value={cluster.totals.pods} label="Pods" cls="bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300" />
                  <StatTile value={(cluster.nodes || []).filter((n: any) => n.role === 'master').length} label="Masters" cls="bg-amber-50 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300" />
                  <StatTile value={(cluster.nodes || []).filter((n: any) => n.role !== 'master').length} label="Workers" cls="bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300" />
                </div>
              </Card>
            </motion.div>
          )}
        </motion.div>
      )}
    </motion.div>
  );
}

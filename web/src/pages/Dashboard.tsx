import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { getStatsOverview, getServerStatus, getAccounts } from '../lib/api';
import { Server, HardDrive, Cpu, Monitor, Globe, Clock, Activity, Users, AlertTriangle, Package, TrendingUp, ArrowRight } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import Card from '../components/ui/Card';
import Skeleton from '../components/ui/Skeleton';
import ProgressBar from '../components/ui/ProgressBar';

export function Dashboard() {
  const [stats, setStats] = useState<any>(null);
  const [server, setServer] = useState<any>(null);
  const [accounts, setAccounts] = useState<any[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    getStatsOverview().then(setStats).catch(() => {});
    getServerStatus().then(setServer).catch(() => {});
    getAccounts().then((d) => setAccounts(d || [])).catch(() => {});
  }, []);

  const activeCount = stats?.active_accounts ?? 0;
  const suspendedCount = stats?.suspended_accounts ?? 0;
  const pendingCount = stats?.pending_accounts ?? 0;
  const totalAccounts = activeCount + suspendedCount + pendingCount;

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
      </div>

      {/* Server info bar */}
      {server && (
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
              <span className="font-medium text-gray-900 dark:text-gray-100">{server.uptime_hours}h</span>
            </div>
          </div>
        </Card>
      )}

      {/* System resources */}
      {server && (
        <motion.div
          className="grid grid-cols-1 md:grid-cols-3 gap-4"
          variants={{ hidden: { opacity: 0 }, visible: { opacity: 1, transition: { staggerChildren: 0.08 } } }}
          initial="hidden"
          animate="visible"
        >
          <motion.div variants={{ hidden: { opacity: 0, y: 15 }, visible: { opacity: 1, y: 0 } }}>
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
              <ProgressBar value={server.cpu_percent} variant="blue" />
              <div className="mt-1 text-lg font-bold text-gray-900 dark:text-gray-100">{server.cpu_percent}%</div>
            </Card>
          </motion.div>

          <motion.div variants={{ hidden: { opacity: 0, y: 15 }, visible: { opacity: 1, y: 0 } }}>
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
              <div className="mt-1 text-lg font-bold text-gray-900 dark:text-gray-100">{server.ram_total_mb > 0 ? ((server.ram_used_mb / server.ram_total_mb) * 100).toFixed(1) : 0}%</div>
            </Card>
          </motion.div>

          <motion.div variants={{ hidden: { opacity: 0, y: 15 }, visible: { opacity: 1, y: 0 } }}>
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
              <div className="mt-1 text-lg font-bold text-gray-900 dark:text-gray-100">{server.disk_total_mb > 0 ? ((server.disk_used_mb / server.disk_total_mb) * 100).toFixed(1) : 0}%</div>
            </Card>
          </motion.div>
        </motion.div>
      )}

      {/* Account stats */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <div className="flex items-center justify-between mb-4">
            <h2 className="font-medium text-gray-900 dark:text-gray-100">Accounts Overview</h2>
            <button onClick={() => navigate('/accounts')} className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 flex items-center gap-1">
              Manage <ArrowRight className="h-3 w-3" />
            </button>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <div className="bg-emerald-50 dark:bg-emerald-900/30 rounded-lg p-3 text-center">
              <div className="text-2xl font-bold text-emerald-700 dark:text-emerald-300">{activeCount}</div>
              <div className="text-xs text-emerald-600 dark:text-emerald-400">Active</div>
            </div>
            <div className="bg-amber-50 dark:bg-amber-900/30 rounded-lg p-3 text-center">
              <div className="text-2xl font-bold text-amber-700 dark:text-amber-300">{suspendedCount}</div>
              <div className="text-xs text-amber-600 dark:text-amber-400">Suspended</div>
            </div>
            <div className="bg-blue-50 dark:bg-blue-900/30 rounded-lg p-3 text-center">
              <div className="text-2xl font-bold text-blue-700 dark:text-blue-300">{pendingCount}</div>
              <div className="text-xs text-blue-600 dark:text-blue-400">Pending</div>
            </div>
          </div>
          <div className="mt-3 text-xs text-gray-400 dark:text-gray-500">
            {totalAccounts} total · {stats?.total_packages ?? 0} packages · {(stats?.total_disk_used_mb / 1024).toFixed(1)} GB account disk
          </div>
        </Card>

        {/* Recent accounts */}
        <Card>
          <h2 className="font-medium text-gray-900 dark:text-gray-100 mb-3">Recent Accounts</h2>
          {accounts.length === 0 ? (
            <p className="text-sm text-gray-400 dark:text-gray-500 py-4 text-center">No accounts yet</p>
          ) : (
            <div className="space-y-2">
              {accounts.slice(0, 5).map((a: any) => (
                <div key={a.id} className="flex items-center justify-between py-1.5 border-b border-gray-50 dark:border-gray-700/50 last:border-0">
                  <div>
                    <div className="text-sm font-medium text-gray-700 dark:text-gray-300">{a.username}</div>
                    <div className="text-xs text-gray-400 dark:text-gray-500">{a.domain}</div>
                  </div>
                  <span className={`inline-flex px-2 py-0.5 rounded-full text-xs font-medium ${
                    a.status === 'active' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' :
                    a.status === 'suspended' ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400'
                  }`}>{a.status}</span>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </motion.div>
  );
}

import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { getAdminBandwidthSummary, getAdminBandwidthAccounts, getBandwidth } from '../lib/api';
import { BarChart3, Activity, HardDrive, X, AlertTriangle, Loader2 } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

function fmtBytes(b: number): string {
  if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' GB';
  if (b >= 1048576) return (b / 1048576).toFixed(1) + ' MB';
  if (b >= 1024) return (b / 1024).toFixed(0) + ' KB';
  return b + ' B';
}

// Parent bandwidth overview
export function Bandwidth() {
  const [data, setData] = useState<any>(null);
  const [accounts, setAccounts] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([
      getAdminBandwidthSummary(),
      getAdminBandwidthAccounts()
    ]).then(([s, a]) => { setData(s); setAccounts(a || []); }).catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  if (loading) return <div className="h-32 bg-gray-200 dark:bg-gray-700 rounded-xl animate-pulse" />;

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Bandwidth Usage</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Total: {fmtBytes(data?.total_bytes || 0)}</p>
      </div>
      {error && <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border rounded-lg text-sm text-red-600 dark:text-red-300"><AlertTriangle className="h-4 w-4" />{error}</div>}

      {/* Daily chart */}
      <Card>
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center gap-2"><Activity className="h-4 w-4" /> Daily Usage (last 30 days)</h3>
        {data?.days?.length > 0 ? (
          <div className="space-y-1">
            {data.days.slice(0, 14).map((d: any) => (
              <div key={d.date} className="flex items-center gap-3 text-xs">
                <span className="w-24 text-gray-500 dark:text-gray-400 truncate">{d.date}</span>
                <div className="flex-1 bg-gray-100 dark:bg-gray-700 rounded-full h-3 overflow-hidden">
                  <div className="bg-blue-500 dark:bg-blue-400 h-full rounded-full" style={{ width: Math.min(100, (d.bytes_out || 0) / Math.max(...data.days.map((x: any) => x.bytes_out || 0)) * 100) + '%' }} />
                </div>
                <span className="w-20 text-right text-gray-600 dark:text-gray-400">{fmtBytes(d.bytes_out)}</span>
              </div>
            ))}
          </div>
        ) : <p className="text-gray-400 dark:text-gray-500 text-sm">No bandwidth data yet</p>}
      </Card>

      {/* Per-account table */}
      <Card>
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-4 flex items-center gap-2"><HardDrive className="h-4 w-4" /> Per Account</h3>
        <table className="w-full text-sm">
          <thead><tr className="border-b border-gray-100 dark:border-gray-700 text-left text-gray-500 dark:text-gray-400 text-xs">
            <th className="px-4 py-2">Account</th><th className="px-4 py-2">Usage</th><th className="px-4 py-2">Used (MB)</th>
          </tr></thead>
          <tbody>
            {accounts.map((a: any, i: number) => (
              <motion.tr
                key={a.id}
                initial={{ opacity: 0, y: 5 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.03 }}
                className="border-b border-gray-50 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-700/30"
              >
                <td className="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">{a.username}</td>
                <td className="px-4 py-2 text-gray-600 dark:text-gray-400">{fmtBytes(a.bytes)}</td>
                <td className="px-4 py-2 text-gray-600 dark:text-gray-400">{a.used_mb} MB</td>
              </motion.tr>
            ))}
            {accounts.length === 0 && <tr><td colSpan={3} className="px-4 py-6 text-center text-gray-400 dark:text-gray-500">No accounts</td></tr>}
          </tbody>
        </table>
      </Card>
    </motion.div>
  );
}

// Child bandwidth usage
export function ChildBandwidth() {
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    getBandwidth().then(d => setData(d)).catch(() => {}).finally(() => setLoading(false));
  }, []);

  if (loading) return <div className="h-32 bg-gray-200 dark:bg-gray-700 rounded-xl animate-pulse" />;

  const pct = Math.min(100, Math.round(data?.usage_percent || 0));

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Bandwidth</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{fmtBytes(data?.total_bytes || 0)} used</p>
      </div>

      {/* Usage gauge */}
      <Card>
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3">Usage vs Limit</h3>
        <div className="flex items-center gap-4">
          <div className="relative w-20 h-20">
            <svg className="w-20 h-20 -rotate-90" viewBox="0 0 36 36">
              <circle cx="18" cy="18" r="15.5" fill="none" stroke="#e5e7eb" strokeWidth="3" />
              <circle cx="18" cy="18" r="15.5" fill="none" stroke={pct > 80 ? '#ef4444' : '#3b82f6'} strokeWidth="3"
                strokeDasharray={`${pct * 0.966} 96.6`} strokeLinecap="round" />
            </svg>
            <span className="absolute inset-0 flex items-center justify-center text-sm font-bold text-gray-900 dark:text-gray-100">{pct}%</span>
          </div>
          <div className="text-sm text-gray-600 dark:text-gray-400">
            <p>Used: <strong className="text-gray-900 dark:text-gray-100">{fmtBytes(data?.total_bytes || 0)}</strong></p>
            <p>Limit: <strong className="text-gray-900 dark:text-gray-100">{data?.limit_mb || 0} MB</strong></p>
          </div>
        </div>
      </Card>

      {/* Daily breakdown */}
      <Card>
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center gap-2"><BarChart3 className="h-4 w-4" /> Daily Breakdown</h3>
        {data?.days?.length > 0 ? (
          <table className="w-full text-sm">
            <thead><tr className="border-b border-gray-100 dark:border-gray-700 text-left text-gray-500 dark:text-gray-400 text-xs">
              <th className="pb-2">Date</th><th className="pb-2">In</th><th className="pb-2">Out</th><th className="pb-2">Total</th>
            </tr></thead>
            <tbody>
              {data.days.map((d: any, i: number) => (
                <motion.tr
                  key={d.date}
                  initial={{ opacity: 0, y: 3 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: i * 0.02 }}
                  className="border-b border-gray-50 dark:border-gray-700/50"
                >
                  <td className="py-1.5 text-gray-900 dark:text-gray-100">{d.date}</td>
                  <td className="py-1.5 text-gray-600 dark:text-gray-400">{fmtBytes(d.bytes_in)}</td>
                  <td className="py-1.5 text-gray-600 dark:text-gray-400">{fmtBytes(d.bytes_out)}</td>
                  <td className="py-1.5 text-gray-600 dark:text-gray-400">{fmtBytes((d.bytes_in||0) + (d.bytes_out||0))}</td>
                </motion.tr>
              ))}
            </tbody>
          </table>
        ) : <p className="text-gray-400 dark:text-gray-500 text-sm">No data yet</p>}
      </Card>
    </motion.div>
  );
}

import { useState, useEffect } from 'react';
import { motion } from 'framer-motion';
import { LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { Users, HardDrive, Eye, Clock } from 'lucide-react';
import Card from '../components/ui/Card';
import Spinner from '../components/ui/Spinner';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';

const API_BASE = '/api/v1';

async function request(url: string, opts?: any) {
  const token = localStorage.getItem('owp_access_token');
  const res = await fetch(API_BASE + url, {
    ...opts,
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...opts?.headers },
    body: opts?.body ? JSON.stringify(opts.body) : undefined,
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

interface StatsData {
  total_visitors: number;
  total_bandwidth_bytes: number;
  bandwidth_over_time: Array<{ date: string; bytes: number }>;
  top_pages: Array<{ path: string; hits: number }>;
  recent_hits: Array<{ timestamp: string; ip: string; path: string; status: number; bytes: number }>;
}

function fmtBytes(b: number): string {
  if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' GB';
  if (b >= 1048576) return (b / 1048576).toFixed(1) + ' MB';
  if (b >= 1024) return (b / 1024).toFixed(0) + ' KB';
  return b + ' B';
}

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: { staggerChildren: 0.1 },
  },
};

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

export function Stats() {
  const [data, setData] = useState<StatsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    fetch('/api/v1/child/stats', {
      headers: { Authorization: `Bearer ${localStorage.getItem('owp_access_token')}` },
    })
      .then(r => r.ok ? r.json() : Promise.reject())
      .then(d => setData(d))
      .catch(() => setError('Failed to load stats'))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner size={32} text="Loading stats..." />
      </div>
    );
  }

  if (error) {
    return (
      <EmptyState title="Unable to load stats" message={error} />
    );
  }

  if (!data) return null;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Website Statistics</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Traffic and bandwidth analytics</p>
      </div>

      <motion.div
        variants={containerVariants}
        initial="hidden"
        animate="visible"
        className="grid grid-cols-1 md:grid-cols-2 gap-4"
      >
        <motion.div variants={itemVariants}>
          <Card>
            <div className="flex items-center gap-3">
              <div className="p-2.5 bg-blue-50 dark:bg-blue-900/30 rounded-lg">
                <Users className="h-5 w-5 text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{data.total_visitors.toLocaleString()}</p>
                <p className="text-xs text-gray-500 dark:text-gray-400">Total Visitors</p>
              </div>
            </div>
          </Card>
        </motion.div>

        <motion.div variants={itemVariants}>
          <Card>
            <div className="flex items-center gap-3">
              <div className="p-2.5 bg-emerald-50 dark:bg-emerald-900/30 rounded-lg">
                <HardDrive className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
              </div>
              <div>
                <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtBytes(data.total_bandwidth_bytes)}</p>
                <p className="text-xs text-gray-500 dark:text-gray-400">Total Bandwidth</p>
              </div>
            </div>
          </Card>
        </motion.div>
      </motion.div>

      {/* Bandwidth over time chart */}
      {data.bandwidth_over_time?.length > 0 && (
        <Card>
          <h3 className="font-medium text-gray-900 dark:text-gray-100 mb-4">Bandwidth Over Time</h3>
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data.bandwidth_over_time}>
                <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="#9ca3af" />
                <YAxis tickFormatter={(v) => fmtBytes(v)} tick={{ fontSize: 12 }} stroke="#9ca3af" />
                <Tooltip
                  contentStyle={{ borderRadius: '8px', border: '1px solid #e5e7eb' }}
                  formatter={(value: any) => [fmtBytes(Number(value) || 0), 'Bandwidth']}
                />
                <Line type="monotone" dataKey="bytes" stroke="#3b82f6" strokeWidth={2} dot={{ r: 3 }} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Card>
      )}

      {/* Top pages chart + table */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {data.top_pages?.length > 0 && (
          <Card>
            <h3 className="font-medium text-gray-900 dark:text-gray-100 mb-4">Top Pages</h3>
            <div className="h-64">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={data.top_pages.slice(0, 10)} layout="vertical">
                  <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                  <XAxis type="number" tick={{ fontSize: 12 }} stroke="#9ca3af" />
                  <YAxis dataKey="path" type="category" width={150} tick={{ fontSize: 11 }} stroke="#9ca3af" />
                  <Tooltip
                    contentStyle={{ borderRadius: '8px', border: '1px solid #e5e7eb' }}
                  />
                  <Bar dataKey="hits" fill="#3b82f6" radius={[0, 4, 4, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </Card>
        )}

        {/* Recent hits */}
        {data.recent_hits?.length > 0 && (
          <Card>
            <h3 className="font-medium text-gray-900 dark:text-gray-100 mb-4">Recent Hits</h3>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700">
                    <th className="text-left py-2 px-2 text-xs font-medium text-gray-500 dark:text-gray-400">Time</th>
                    <th className="text-left py-2 px-2 text-xs font-medium text-gray-500 dark:text-gray-400">IP</th>
                    <th className="text-left py-2 px-2 text-xs font-medium text-gray-500 dark:text-gray-400">Path</th>
                    <th className="text-left py-2 px-2 text-xs font-medium text-gray-500 dark:text-gray-400">Status</th>
                    <th className="text-right py-2 px-2 text-xs font-medium text-gray-500 dark:text-gray-400">Bytes</th>
                  </tr>
                </thead>
                <tbody>
                  {data.recent_hits.slice(0, 20).map((hit, i) => (
                    <motion.tr
                      key={i}
                      initial={{ opacity: 0, x: -5 }}
                      animate={{ opacity: 1, x: 0 }}
                      transition={{ delay: i * 0.02 }}
                      className="border-b border-gray-50 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-700/30"
                    >
                      <td className="py-2 px-2 text-xs text-gray-500 dark:text-gray-400 whitespace-nowrap">{new Date(hit.timestamp).toLocaleTimeString()}</td>
                      <td className="py-2 px-2 text-xs font-mono text-gray-600 dark:text-gray-300">{hit.ip}</td>
                      <td className="py-2 px-2 text-xs text-gray-600 dark:text-gray-300 truncate max-w-[120px]">{hit.path}</td>
                      <td className="py-2 px-2">
                        <Badge variant={hit.status < 400 ? 'success' : hit.status < 500 ? 'warning' : 'error'}>{hit.status}</Badge>
                      </td>
                      <td className="py-2 px-2 text-xs text-gray-500 dark:text-gray-400 text-right">{fmtBytes(hit.bytes)}</td>
                    </motion.tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}

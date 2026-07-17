import { useState, useEffect } from 'react';
import { Shield, RefreshCw, Search, LogIn, XCircle, CheckCircle, Clock } from 'lucide-react';
import { motion } from 'framer-motion';

const BASE = '/api/v1';

function getToken(): string | null {
  return localStorage.getItem('owp_access_token');
}

async function apiRequest(method: string, path: string, body?: any): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;
  const res = await fetch(`${BASE}${path}`, { method, headers, body: body ? JSON.stringify(body) : undefined });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw err;
  }
  return res.json();
}

interface LoginAttempt {
  id: number;
  username: string;
  ip_address: string;
  success: boolean;
  user_agent: string;
  created_at: string;
}

interface AccessLogCounts {
  total_attempts: number;
  blocked_ips: number;
}

export function AccessLog() {
  const [attempts, setAttempts] = useState<LoginAttempt[]>([]);
  const [counts, setCounts] = useState<AccessLogCounts>({ total_attempts: 0, blocked_ips: 0 });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [searchTerm, setSearchTerm] = useState('');
  const [filter, setFilter] = useState<'all' | 'success' | 'failed'>('all');

  const fetchData = async () => {
    setLoading(true);
    setError('');
    try {
      const [attemptsData, countsData] = await Promise.all([
        apiRequest('GET', '/security/access-log?limit=200'),
        apiRequest('GET', '/security/access-log/count'),
      ]);
      setAttempts(attemptsData || []);
      setCounts(countsData || { total_attempts: 0, blocked_ips: 0 });
    } catch (err: any) {
      setError(err?.error || 'Failed to load access log');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const filteredAttempts = attempts.filter(a => {
    const matchesSearch = 
      a.username.toLowerCase().includes(searchTerm.toLowerCase()) ||
      a.ip_address.toLowerCase().includes(searchTerm.toLowerCase());
    const matchesFilter = 
      filter === 'all' || 
      (filter === 'success' && a.success) || 
      (filter === 'failed' && !a.success);
    return matchesSearch && matchesFilter;
  });

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Access Log</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Monitor login attempts and security events
          </p>
        </div>
        <button
          onClick={fetchData}
          className="flex items-center gap-2 px-3 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
        >
          <RefreshCw className="h-4 w-4" /> Refresh
        </button>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-blue-100 dark:bg-blue-900/30 rounded-lg">
              <LogIn className="h-5 w-5 text-blue-600 dark:text-blue-400" />
            </div>
            <div>
              <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{counts.total_attempts}</p>
              <p className="text-xs text-gray-500 dark:text-gray-400">Total Login Attempts</p>
            </div>
          </div>
        </div>
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-red-100 dark:bg-red-900/30 rounded-lg">
              <Shield className="h-5 w-5 text-red-600 dark:text-red-400" />
            </div>
            <div>
              <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{counts.blocked_ips}</p>
              <p className="text-xs text-gray-500 dark:text-gray-400">Blocked IPs</p>
            </div>
          </div>
        </div>
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-green-100 dark:bg-green-900/30 rounded-lg">
              <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
            </div>
            <div>
              <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">
                {attempts.filter(a => a.success).length}
              </p>
              <p className="text-xs text-gray-500 dark:text-gray-400">Successful Logins</p>
            </div>
          </div>
        </div>
      </div>

      {error && (
        <motion.div initial={{ opacity: 0, y: -10 }} animate={{ opacity: 1, y: 0 }}
          className="p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          {error}
        </motion.div>
      )}

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            placeholder="Search by username or IP..."
            className="w-full pl-10 pr-4 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>
        <div className="flex gap-2">
          {(['all', 'success', 'failed'] as const).map((f) => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={`px-3 py-2 text-sm font-medium rounded-lg transition-colors ${
                filter === f
                  ? 'bg-blue-600 text-white'
                  : 'bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-300 border border-gray-300 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700'
              }`}
            >
              {f === 'all' ? 'All' : f === 'success' ? 'Successful' : 'Failed'}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <div className="animate-spin h-8 w-8 border-4 border-blue-500 border-t-transparent rounded-full" />
        </div>
      ) : filteredAttempts.length === 0 ? (
        <div className="text-center py-12">
          <Shield className="h-12 w-12 text-gray-300 dark:text-gray-600 mx-auto mb-3" />
          <p className="text-sm text-gray-500 dark:text-gray-400">
            {searchTerm ? 'No login attempts match your search' : 'No login attempts recorded yet'}
          </p>
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Status</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Username</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">IP Address</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Time</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400 hidden lg:table-cell">User Agent</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                {filteredAttempts.map((a) => (
                  <motion.tr
                    key={a.id}
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    className="hover:bg-gray-50 dark:hover:bg-gray-750"
                  >
                    <td className="px-4 py-3">
                      {a.success ? (
                        <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
                          <CheckCircle className="h-4 w-4" />
                          <span className="text-xs font-medium">Success</span>
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
                          <XCircle className="h-4 w-4" />
                          <span className="text-xs font-medium">Failed</span>
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 font-medium text-gray-900 dark:text-gray-100">{a.username}</td>
                    <td className="px-4 py-3 font-mono text-sm text-gray-600 dark:text-gray-400">{a.ip_address}</td>
                    <td className="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {new Date(a.created_at + 'Z').toLocaleString()}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-400 dark:text-gray-500 max-w-[200px] truncate hidden lg:table-cell">
                      {a.user_agent || '-'}
                    </td>
                  </motion.tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </motion.div>
  );
}

import { useState, useEffect } from 'react';
import { Shield, ShieldOff, RefreshCw, Search, Trash2, ExternalLink, Clock, AlertTriangle } from 'lucide-react';
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

interface BlockedIP {
  id: number;
  ip_address: string;
  reason: string;
  blocked_by: string;
  failed_attempts: number;
  created_at: string;
  updated_at: string;
}

export function IpManagement() {
  const [blockedIPs, setBlockedIPs] = useState<BlockedIP[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [searchTerm, setSearchTerm] = useState('');

  const fetchBlockedIPs = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await apiRequest('GET', '/security/ips');
      setBlockedIPs(data || []);
    } catch (err: any) {
      setError(err?.error || 'Failed to load blocked IPs');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchBlockedIPs(); }, []);

  const unblockIP = async (id: number) => {
    setError('');
    setSuccess('');
    try {
      const result = await apiRequest('POST', `/security/ips/${id}/unblock`);
      setSuccess(`IP ${result.ip_address} has been unblocked successfully.`);
      fetchBlockedIPs();
    } catch (err: any) {
      setError(err?.error || 'Failed to unblock IP');
    }
  };

  const filteredIPs = blockedIPs.filter(ip =>
    ip.ip_address.toLowerCase().includes(searchTerm.toLowerCase())
  );

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-semibold text-gray-900 dark:text-gray-100">IP Management</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Manage blocked IP addresses and security rules
          </p>
        </div>
        <button
          onClick={fetchBlockedIPs}
          className="flex items-center gap-2 px-3 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
        >
          <RefreshCw className="h-4 w-4" /> Refresh
        </button>
      </div>

      {error && (
        <motion.div initial={{ opacity: 0, y: -10 }} animate={{ opacity: 1, y: 0 }}
          className="p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          {error}
        </motion.div>
      )}

      {success && (
        <motion.div initial={{ opacity: 0, y: -10 }} animate={{ opacity: 1, y: 0 }}
          className="p-3 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-lg text-sm text-green-600 dark:text-green-300">
          {success}
        </motion.div>
      )}

      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
        <input
          type="text"
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          placeholder="Search IP addresses..."
          className="w-full pl-10 pr-4 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <div className="animate-spin h-8 w-8 border-4 border-blue-500 border-t-transparent rounded-full" />
        </div>
      ) : filteredIPs.length === 0 ? (
        <div className="text-center py-12">
          <Shield className="h-12 w-12 text-gray-300 dark:text-gray-600 mx-auto mb-3" />
          <p className="text-sm text-gray-500 dark:text-gray-400">
            {searchTerm ? 'No blocked IPs match your search' : 'No IP addresses are currently blocked'}
          </p>
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900">
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">IP Address</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Failed Attempts</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Reason</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Blocked By</th>
                  <th className="text-left px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Blocked At</th>
                  <th className="text-right px-4 py-3 font-medium text-gray-600 dark:text-gray-400">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                {filteredIPs.map((ip) => (
                  <motion.tr
                    key={ip.id}
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    className="hover:bg-gray-50 dark:hover:bg-gray-750"
                  >
                    <td className="px-4 py-3 font-mono text-sm text-gray-900 dark:text-gray-100">{ip.ip_address}</td>
                    <td className="px-4 py-3">
                      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300">
                        <AlertTriangle className="h-3 w-3" />
                        {ip.failed_attempts}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-600 dark:text-gray-400 max-w-xs truncate">{ip.reason}</td>
                    <td className="px-4 py-3 text-sm text-gray-600 dark:text-gray-400">{ip.blocked_by}</td>
                    <td className="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {new Date(ip.created_at + 'Z').toLocaleString()}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={() => unblockIP(ip.id)}
                        className="inline-flex items-center gap-1 px-3 py-1.5 text-xs font-medium text-white bg-green-600 hover:bg-green-700 rounded-md transition-colors"
                      >
                        <ShieldOff className="h-3 w-3" /> Unblock
                      </button>
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

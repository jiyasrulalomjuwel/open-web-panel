import { useState, useEffect, useMemo } from 'react';
import { motion } from 'framer-motion';
import {
  LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as ReTooltip,
  ResponsiveContainer, PieChart, Pie, Cell, AreaChart, Area
} from 'recharts';
import {
  Users, HardDrive, Activity, Globe, MousePointerClick,
  ExternalLink, Monitor, Smartphone, Chrome, TrendingUp,
  CalendarDays, ArrowUpRight, AlertCircle, Fingerprint
} from 'lucide-react';

const API_BASE = '/api/v1';

function fmtBytes(b: number): string {
  if (b >= 1073741824) return (b / 1073741824).toFixed(2) + ' GB';
  if (b >= 1048576) return (b / 1048576).toFixed(1) + ' MB';
  if (b >= 1024) return (b / 1024).toFixed(0) + ' KB';
  return b + ' B';
}

function fmtNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
  return n.toLocaleString();
}

function parseUA(ua: string) {
  const lower = ua.toLowerCase();
  let browser = 'Other';
  let os = 'Other';
  let device = 'Desktop';

  if (lower.includes('chrome') && !lower.includes('edg')) browser = 'Chrome';
  else if (lower.includes('firefox')) browser = 'Firefox';
  else if (lower.includes('safari') && !lower.includes('chrome')) browser = 'Safari';
  else if (lower.includes('edg')) browser = 'Edge';
  else if (lower.includes('opera') || lower.includes('opr')) browser = 'Opera';

  if (lower.includes('windows')) os = 'Windows';
  else if (lower.includes('mac os')) os = 'macOS';
  else if (lower.includes('linux') && !lower.includes('android')) os = 'Linux';
  else if (lower.includes('android')) os = 'Android';
  else if (lower.includes('iphone') || lower.includes('ipad')) os = 'iOS';

  if (lower.includes('iphone') || lower.includes('android') && lower.includes('mobile')) device = 'Mobile';
  else if (lower.includes('ipad') || lower.includes('tablet')) device = 'Tablet';

  return { browser, os, device };
}

function extractDomain(url: string): string {
  if (!url || url === '-' || url === '') return 'Direct';
  try {
    const u = new URL(url);
    return u.hostname.replace(/^www\./, '');
  } catch {
    const cleaned = url.replace(/^https?:\/\//, '').replace(/^www\./, '').split('/')[0];
    return cleaned || 'Direct';
  }
}

function formatDate(d: string) {
  if (!d) return '';
  const date = new Date(d + 'T00:00:00');
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

const CHART_COLORS = ['#2563EB', '#3B82F6', '#60A5FA', '#93C5FD', '#1D4ED8', '#1E40AF', '#DBEAFE'];
const PIE_COLORS = ['#2563EB', '#3B82F6', '#60A5FA', '#93C5FD', '#BFDBFE', '#1D4ED8', '#1E40AF', '#DBEAFE', '#EFF6FF'];

interface StatsData {
  total_visitors: number;
  total_bandwidth_bytes: number;
  top_pages: Array<{ path: string; hits: number; bytes: number }>;
  top_referrers: Array<{ referer: string; hits: number }>;
  traffic_by_day: Array<{ date: string; hits: number; visits: number }>;
  recent_hits: Array<{
    timestamp: string; ip: string; path: string;
    status: number; bytes: number; referer: string; user_agent: string;
  }>;
}

const itemVariants = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0 },
};

function ChartTooltip({ active, payload, label, formatter }: any) {
  if (!active || !payload?.length) return null;
  return (
    <div className="bg-white border border-border-subtle rounded-xl shadow-dropdown px-4 py-3 text-xs">
      <p className="text-gray-400 mb-1">{label}</p>
      {payload.map((p: any, i: number) => (
        <p key={i} className="font-semibold text-gray-900">
          {formatter ? formatter(p.value) : p.value} {p.name}
        </p>
      ))}
    </div>
  );
}

function CustomPieTooltip({ active, payload }: any) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload;
  return (
    <div className="bg-white border border-border-subtle rounded-xl shadow-dropdown px-4 py-3 text-xs">
      <p className="font-semibold text-gray-900">{d.name}</p>
      <p className="text-gray-500 mt-0.5">{d.value} visits ({d.pct}%)</p>
    </div>
  );
}

export function Stats() {
  const [data, setData] = useState<StatsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const fetchStats = async () => {
    setLoading(true);
    setError('');
    try {
      const token = localStorage.getItem('owp_access_token');
      const res = await fetch('/api/v1/child/stats', {
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!res.ok) throw new Error('Failed to load');
      const d = await res.json();
      setData(d);
    } catch (e: any) {
      console.error('Stats fetch:', e);
      setError('Failed to load stats data');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    let mounted = true;
    fetchStats().then(() => { if (!mounted) return; });
    const iv = setInterval(() => { if (mounted) fetchStats(); }, 30000);
    return () => { mounted = false; clearInterval(iv); };
  }, []);

  const { browserStats, osStats, deviceStats, topRefs, totalHits } = useMemo(() => {
    if (!data?.recent_hits) return { browserStats: [], osStats: [], deviceStats: [], topRefs: [], totalHits: 0 };

    const browsers: Record<string, number> = {};
    const oss: Record<string, number> = {};
    const devices: Record<string, number> = {};
    let hits = 0;

    data.recent_hits.forEach(h => {
      if (h.user_agent && h.user_agent !== '-') {
        const parsed = parseUA(h.user_agent);
        browsers[parsed.browser] = (browsers[parsed.browser] || 0) + 1;
        oss[parsed.os] = (oss[parsed.os] || 0) + 1;
        devices[parsed.device] = (devices[parsed.device] || 0) + 1;
      }
      hits++;
    });

    const total = Object.values(browsers).reduce((a, b) => a + b, 0) || 1;

    const toPct = (map: Record<string, number>) =>
      Object.entries(map)
        .map(([name, value]) => ({ name, value, pct: Math.round((value / total) * 100) }))
        .sort((a, b) => b.value - a.value);

    const refs = (data.top_referrers || []).map(r => ({ name: r.referer, value: r.hits }));

    return {
      browserStats: toPct(browsers),
      osStats: toPct(oss),
      deviceStats: toPct(devices),
      topRefs: refs,
      totalHits: hits,
    };
  }, [data]);

  if (loading && !data) {
    return (
      <div className="max-w-7xl mx-auto space-y-6 animate-pulse">
        <div className="h-8 w-48 bg-gray-100 rounded-lg" />
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5">
          {[...Array(4)].map((_, i) => (
            <div key={i} className="bg-white rounded-card border border-border-subtle p-6 space-y-3">
              <div className="h-3 w-20 bg-gray-100 rounded" />
              <div className="h-8 w-24 bg-gray-100 rounded" />
              <div className="h-3 w-16 bg-gray-50 rounded" />
            </div>
          ))}
        </div>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 bg-white rounded-card border border-border-subtle p-6 h-80" />
          <div className="bg-white rounded-card border border-border-subtle p-6 h-80" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="max-w-7xl mx-auto py-16">
        <div className="bg-white rounded-card border border-border-subtle shadow-soft p-12 text-center">
          <AlertCircle className="w-12 h-12 text-gray-300 mx-auto mb-4" strokeWidth={1} />
          <h2 className="text-lg font-semibold text-gray-900 mb-2">Unable to Load Statistics</h2>
          <p className="text-sm text-gray-500 mb-6">{error}</p>
          <button
            onClick={fetchStats}
            className="px-5 py-2.5 bg-[#2563EB] text-white text-sm font-medium rounded-xl hover:bg-[#1D4ED8] transition-colors"
          >
            Try Again
          </button>
        </div>
      </div>
    );
  }

  if (!data) return null;

  const bwFormatted = fmtBytes(data.total_bandwidth_bytes);
  const totalBytes = data.total_bandwidth_bytes;
  const avgBytes = totalHits > 0 ? totalBytes / totalHits : 0;

  const trafficData = (data.traffic_by_day || []).map(d => ({
    ...d,
    dateLabel: formatDate(d.date),
  }));

  const topPages = (data.top_pages || []).slice(0, 8);

  return (
    <div className="max-w-7xl mx-auto space-y-8">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-[32px] font-bold text-gray-900 tracking-tight">Statistics</h1>
          <p className="text-base text-gray-500 mt-1.5">Website traffic and visitor analytics</p>
        </div>
        <button
          onClick={fetchStats}
          className="px-4 py-2.5 bg-white border border-border-subtle text-sm font-medium text-gray-600 rounded-xl hover:bg-gray-50 hover:text-gray-900 transition-all duration-150 flex items-center gap-2"
        >
          <TrendingUp className="w-4 h-4" strokeWidth={1.5} />
          Refresh
        </button>
      </div>

      {/* Summary Cards */}
      <motion.div
        initial="hidden"
        animate="visible"
        className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-5"
      >
        {[
          { label: 'Total Visitors', value: fmtNum(data.total_visitors), icon: Users, color: '#2563EB', bg: 'bg-[#2563EB]/10' },
          { label: 'Total Bandwidth', value: bwFormatted, icon: HardDrive, color: '#059669', bg: 'bg-[#059669]/10' },
          { label: 'Total Hits', value: fmtNum(totalHits), icon: MousePointerClick, color: '#D97706', bg: 'bg-[#D97706]/10' },
          { label: 'Avg Response', value: fmtBytes(Math.round(avgBytes)), icon: Activity, color: '#7C3AED', bg: 'bg-[#7C3AED]/10' },
        ].map((stat, i) => (
          <motion.div
            key={stat.label}
            variants={itemVariants}
            transition={{ delay: i * 0.05 }}
            className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden hover:shadow-card-hover hover:-translate-y-0.5 transition-all duration-200"
          >
            <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
            <div className="p-6">
              <div className="flex items-center justify-between mb-3">
                <p className="text-xs font-medium text-gray-400 uppercase tracking-wide">{stat.label}</p>
                <div className={`w-8 h-8 rounded-lg ${stat.bg} flex items-center justify-center`}>
                  <stat.icon className="w-4 h-4" style={{ color: stat.color }} strokeWidth={1.5} />
                </div>
              </div>
              <p className="text-2xl font-bold text-gray-900">{stat.value}</p>
            </div>
          </motion.div>
        ))}
      </motion.div>

      {/* Traffic Trend + World Map */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
          <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
          <div className="p-6">
            <div className="flex items-center justify-between mb-6">
              <div className="flex items-center gap-3">
                <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
                  <Activity className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
                </div>
                <div>
                  <p className="text-sm font-semibold text-gray-900">Traffic Trend</p>
                  <p className="text-xs text-gray-400 mt-0.5">Daily hits over time</p>
                </div>
              </div>
              <CalendarDays className="w-4 h-4 text-gray-400" strokeWidth={1.5} />
            </div>
            {trafficData.length > 0 ? (
              <div className="h-72">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={trafficData}>
                    <defs>
                      <linearGradient id="colorHits" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#2563EB" stopOpacity={0.15} />
                        <stop offset="95%" stopColor="#2563EB" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="#ECEEF4" />
                    <XAxis dataKey="dateLabel" tick={{ fontSize: 11, fill: '#9CA3AF' }} />
                    <YAxis tick={{ fontSize: 11, fill: '#9CA3AF' }} />
                    <ReTooltip content={<ChartTooltip formatter={(v: number) => v.toLocaleString()} />} />
                    <Area
                      type="monotone" dataKey="hits" stroke="#2563EB" strokeWidth={2}
                      fill="url(#colorHits)" dot={false} activeDot={{ r: 4, fill: '#2563EB' }}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-72 flex items-center justify-center text-sm text-gray-400">
                No traffic data available yet
              </div>
            )}
          </div>
        </div>

        {/* World Map Panel */}
        <WorldMapCard recentHits={data.recent_hits || []} />
      </div>

      {/* Top Pages + Top Referrers */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
          <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
          <div className="p-6">
            <div className="flex items-center gap-3 mb-6">
              <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
                <ExternalLink className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-900">Top Pages</p>
                <p className="text-xs text-gray-400 mt-0.5">Most visited pages</p>
              </div>
            </div>
            {topPages.length > 0 ? (
              <div className="h-72">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={topPages} layout="vertical" margin={{ left: 0, right: 20 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#ECEEF4" horizontal={false} />
                    <XAxis type="number" tick={{ fontSize: 11, fill: '#9CA3AF' }} />
                    <YAxis
                      type="category" dataKey="path" width={140}
                      tick={{ fontSize: 10, fill: '#6B7280' }}
                      tickFormatter={(v: string) => v.length > 18 ? v.slice(0, 18) + '...' : v}
                    />
                    <ReTooltip content={<ChartTooltip />} />
                    <Bar dataKey="hits" fill="#2563EB" radius={[0, 4, 4, 0]} barSize={20} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-72 flex items-center justify-center text-sm text-gray-400">
                No page data available yet
              </div>
            )}
          </div>
        </div>

        <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
          <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
          <div className="p-6">
            <div className="flex items-center gap-3 mb-6">
              <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
                <ArrowUpRight className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-900">Top Referrers</p>
                <p className="text-xs text-gray-400 mt-0.5">Traffic sources</p>
              </div>
            </div>
            {topRefs.length > 0 ? (
              <div className="h-72">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={topRefs} layout="vertical" margin={{ left: 0, right: 20 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#ECEEF4" horizontal={false} />
                    <XAxis type="number" tick={{ fontSize: 11, fill: '#9CA3AF' }} />
                    <YAxis
                      type="category" dataKey="name" width={120}
                      tick={{ fontSize: 10, fill: '#6B7280' }}
                      tickFormatter={(v: string) => v.length > 18 ? v.slice(0, 18) + '...' : v}
                    />
                    <ReTooltip content={<ChartTooltip />} />
                    <Bar dataKey="value" fill="#3B82F6" radius={[0, 4, 4, 0]} barSize={20} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-72 flex items-center justify-center text-sm text-gray-400">
                No referrer data available yet
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Browser / OS / Device Distribution */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <PieCard title="Browser" data={browserStats} icon={Chrome} />
        <PieCard title="Operating System" data={osStats} icon={Monitor} />
        <PieCard title="Device" data={deviceStats} icon={Smartphone} />
      </div>

      {/* Recent Hits */}
      <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
        <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
        <div className="p-6">
          <div className="flex items-center gap-3 mb-6">
            <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
              <Fingerprint className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
            </div>
            <div>
              <p className="text-sm font-semibold text-gray-900">Recent Hits</p>
              <p className="text-xs text-gray-400 mt-0.5">Latest visitor activity</p>
            </div>
          </div>
          {data.recent_hits && data.recent_hits.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-border-subtle">
                    <th className="text-left py-3 px-3 text-xs font-medium text-gray-400 uppercase tracking-wider">Time</th>
                    <th className="text-left py-3 px-3 text-xs font-medium text-gray-400 uppercase tracking-wider">IP</th>
                    <th className="text-left py-3 px-3 text-xs font-medium text-gray-400 uppercase tracking-wider">Path</th>
                    <th className="text-left py-3 px-3 text-xs font-medium text-gray-400 uppercase tracking-wider">Status</th>
                    <th className="text-left py-3 px-3 text-xs font-medium text-gray-400 uppercase tracking-wider">Referer</th>
                    <th className="text-right py-3 px-3 text-xs font-medium text-gray-400 uppercase tracking-wider">Bytes</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {data.recent_hits.slice(0, 30).map((hit, i) => (
                    <motion.tr
                      key={i}
                      initial={{ opacity: 0, x: -5 }}
                      animate={{ opacity: 1, x: 0 }}
                      transition={{ delay: i * 0.01 }}
                      className="hover:bg-gray-50 transition-colors"
                    >
                      <td className="py-3 px-3 text-xs text-gray-500 whitespace-nowrap">
                        {new Date(hit.timestamp).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                      </td>
                      <td className="py-3 px-3">
                        <span className="text-xs font-mono text-gray-600 bg-gray-50 px-2 py-0.5 rounded-md">{hit.ip}</span>
                      </td>
                      <td className="py-3 px-3 text-xs text-gray-700 max-w-[160px] truncate">{hit.path}</td>
                      <td className="py-3 px-3">
                        <span className={`inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium ${
                          hit.status < 300 ? 'bg-[#EAF8EE] text-[#16A34A]' :
                          hit.status < 400 ? 'bg-[#EFF6FF] text-[#2563EB]' :
                          hit.status < 500 ? 'bg-[#FEF3C7] text-[#D97706]' :
                          'bg-[#FDECEC] text-[#DC2626]'
                        }`}>
                          {hit.status}
                        </span>
                      </td>
                      <td className="py-3 px-3 text-xs text-gray-500 max-w-[120px] truncate">
                        {hit.referer && hit.referer !== '-' ? extractDomain(hit.referer) : '—'}
                      </td>
                      <td className="py-3 px-3 text-xs text-gray-500 text-right whitespace-nowrap">{fmtBytes(hit.bytes)}</td>
                    </motion.tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="py-12 text-center">
              <Activity className="w-8 h-8 mx-auto mb-3 text-gray-200" strokeWidth={1} />
              <p className="text-sm font-medium text-gray-500">No recent hits</p>
              <p className="text-xs text-gray-400 mt-1">Visitor data will appear once your site receives traffic</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function PieCard({ title, data, icon: Icon }: { title: string; data: Array<{ name: string; value: number; pct: number }>; icon: any }) {
  const total = data.reduce((a, b) => a + b.value, 0);
  return (
    <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
      <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
      <div className="p-6">
        <div className="flex items-center gap-3 mb-5">
          <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
            <Icon className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
          </div>
          <div>
            <p className="text-sm font-semibold text-gray-900">{title}</p>
            <p className="text-xs text-gray-400 mt-0.5">{total} visitors</p>
          </div>
        </div>
        {data.length > 0 ? (
          <div className="flex items-center gap-6">
            <div className="w-28 h-28 shrink-0">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={data} cx="50%" cy="50%" innerRadius={26} outerRadius={42}
                    dataKey="value" strokeWidth={0}
                  >
                    {data.map((_, i) => (
                      <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />
                    ))}
                  </Pie>
                  <ReTooltip content={<CustomPieTooltip />} />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="flex-1 space-y-1.5">
              {data.slice(0, 5).map((item) => (
                <div key={item.name} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: PIE_COLORS[data.indexOf(item) % PIE_COLORS.length] }} />
                    <span className="text-xs text-gray-500">{item.name}</span>
                  </div>
                  <span className="text-xs font-semibold text-gray-900">{item.pct}%</span>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <div className="h-28 flex items-center justify-center text-sm text-gray-400">No data</div>
        )}
      </div>
    </div>
  );
}

function WorldMapCard({ recentHits }: { recentHits: Array<{ ip: string }> }) {
  const [geoData, setGeoData] = useState<Array<{ country: string; count: number }>>([]);
  const [loadingGeo, setLoadingGeo] = useState(false);

  useEffect(() => {
    let mounted = true;
    const ips = [...new Set(recentHits.map(h => h.ip))].filter(Boolean).slice(0, 50);
    if (ips.length === 0) { setLoadingGeo(false); return; }

    setLoadingGeo(true);
    fetch('http://ip-api.com/batch?fields=country,query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(ips),
    })
      .then(r => r.json())
      .then((results: Array<{ country?: string; query?: string }>) => {
        if (!mounted) return;
        const counts: Record<string, number> = {};
        (results || []).forEach(r => {
          if (r.country && r.country !== '-') {
            counts[r.country] = (counts[r.country] || 0) + 1;
          }
        });
        const sorted = Object.entries(counts)
          .map(([country, count]) => ({ country, count }))
          .sort((a, b) => b.count - a.count);
        setGeoData(sorted);
      })
      .catch((e: any) => console.error('Geo fetch:', e))
      .finally(() => { if (mounted) setLoadingGeo(false); });
    return () => { mounted = false; };
  }, [recentHits]);

  const total = geoData.reduce((a, b) => a + b.count, 0);
  const maxCount = Math.max(...geoData.map(d => d.count), 1);

  const continentCoords: Record<string, { cx: number; cy: number }> = {
    'North America': { cx: 160, cy: 110 },
    'South America': { cx: 175, cy: 230 },
    'Europe': { cx: 320, cy: 100 },
    'Africa': { cx: 330, cy: 210 },
    'Asia': { cx: 430, cy: 130 },
    'Australia': { cx: 480, cy: 270 },
  };

  const geoByContinent: Record<string, { country: string; count: number }[]> = {
    'North America': [],
    'South America': [],
    'Europe': [],
    'Africa': [],
    'Asia': [],
    'Australia': [],
  };

  const continentMap: Record<string, string> = {
    'United States': 'North America', 'Canada': 'North America', 'Mexico': 'North America',
    'Brazil': 'South America', 'Argentina': 'South America', 'Colombia': 'South America',
    'United Kingdom': 'Europe', 'Germany': 'Europe', 'France': 'Europe', 'Spain': 'Europe', 'Italy': 'Europe', 'Netherlands': 'Europe',
    'Nigeria': 'Africa', 'South Africa': 'Africa', 'Egypt': 'Africa', 'Kenya': 'Africa',
    'China': 'Asia', 'India': 'Asia', 'Japan': 'Asia', 'South Korea': 'Asia', 'Singapore': 'Asia', 'Russia': 'Asia',
    'Australia': 'Australia', 'New Zealand': 'Australia',
  };

  geoData.forEach(g => {
    const continent = continentMap[g.country] || 'Asia';
    if (geoByContinent[continent]) {
      geoByContinent[continent].push(g);
    }
  });

  const continentData = Object.entries(continentCoords).map(([name, coord]) => {
    const countries = geoByContinent[name] || [];
    const visitors = countries.reduce((a, b) => a + b.count, 0);
    return { name, ...coord, visitors, countries };
  }).filter(c => c.visitors > 0);

  return (
    <div className="bg-white rounded-card border border-border-subtle shadow-soft overflow-hidden">
      <div className="h-1 bg-gradient-to-r from-[#2563EB] to-[#3B82F6]" />
      <div className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="w-9 h-9 rounded-xl bg-[#2563EB]/10 flex items-center justify-center">
            <Globe className="w-5 h-5 text-[#2563EB]" strokeWidth={1.5} />
          </div>
          <div>
            <p className="text-sm font-semibold text-gray-900">Visitor Geography</p>
            <p className="text-xs text-gray-400 mt-0.5">
              {loadingGeo ? 'Looking up locations...' : `${total} visitors from ${geoData.length} countries`}
            </p>
          </div>
        </div>

        {geoData.length > 0 ? (
          <>
            <svg viewBox="0 0 560 320" className="w-full h-auto" xmlns="http://www.w3.org/2000/svg">
              <rect width="560" height="320" fill="#F7F8FC" rx={12} />

              <g fill="none" stroke="#D1D5DB" strokeWidth={0.5}>
                <path d="M30 180 Q60 140 90 120 Q120 100 140 80 L160 60 Q180 50 200 70 L220 80 Q240 90 260 80 L280 60 Q300 50 320 70 L340 90 Q360 100 380 110 Q400 120 420 130 Q440 140 460 135 L480 130 Q500 125 520 140 L530 160 Q535 180 530 200 L520 220 Q510 240 500 260 L490 270 Q480 280 470 260 L460 240 Q450 230 440 240 L420 250 Q400 260 380 240 L360 220 Q340 210 320 220 L300 230 Q280 240 260 250 L240 260 Q220 255 200 245 L180 235 Q160 225 140 230 L120 235 Q100 230 90 220 L80 210 Q70 200 60 195 Z" />
                <path d="M140 60 Q160 40 180 55 Q200 70 190 90 Q180 80 160 75 Z" fill="#D1D5DB" />
                <path d="M370 50 Q390 35 410 40 Q430 45 440 60 Q435 55 420 50 Q400 48 380 55 Z" fill="#D1D5DB" />
              </g>

              {continentData.map(cont => {
                const intensity = cont.visitors / maxCount;
                const r = 12 + intensity * 20;
                const opacity = 0.3 + intensity * 0.5;
                return (
                  <g key={cont.name}>
                    <circle cx={cont.cx} cy={cont.cy} r={r} fill="#2563EB" opacity={opacity} className="cursor-pointer hover:opacity-80 transition-opacity" />
                    <circle cx={cont.cx} cy={cont.cy} r={4} fill="#2563EB" opacity={0.9} />
                    <title>{`${cont.name}\n${cont.visitors} visitors\n${cont.countries.map(c => `${c.country}: ${c.count}`).join('\n')}`}</title>
                  </g>
                );
              })}
            </svg>

            <div className="mt-4 space-y-1.5">
              {geoData.slice(0, 8).map(geo => {
                const pct = Math.round((geo.count / total) * 100);
                const barWidth = Math.max(pct, 2);
                return (
                  <div key={geo.country} className="flex items-center gap-3">
                    <span className="text-xs text-gray-500 w-24 truncate">{geo.country}</span>
                    <div className="flex-1 h-2 bg-gray-100 rounded-full overflow-hidden">
                      <div
                        className="h-full rounded-full bg-[#2563EB] transition-all duration-500"
                        style={{ width: `${barWidth}%` }}
                      />
                    </div>
                    <span className="text-xs font-semibold text-gray-900 w-10 text-right">{geo.count}</span>
                  </div>
                );
              })}
            </div>
          </>
        ) : (
          <div className="py-10 text-center">
            <Globe className="w-8 h-8 mx-auto mb-3 text-gray-200" strokeWidth={1} />
            <p className="text-sm font-medium text-gray-500">No geographic data</p>
            <p className="text-xs text-gray-400 mt-1">Visitor locations will appear once your site receives traffic</p>
          </div>
        )}
      </div>
    </div>
  );
}

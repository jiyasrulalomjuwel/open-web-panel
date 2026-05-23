import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { getDiskUsage, getDatabases, getChildAccount, getEmailCount, getRecentErrors } from '../lib/api';
import {
  HardDrive, Database, Globe, Mail, FolderOpen, Server, Activity,
  Bug, AlertTriangle, Users, ArrowUpRight, ShieldCheck, Monitor, Wrench,
  Gauge, Box
} from 'lucide-react';
import Card from '../components/ui/Card';
import Badge from '../components/ui/Badge';
import ProgressBar from '../components/ui/ProgressBar';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';

function pct(val: number, max: number) {
  if (!max) return 0;
  return Math.min(Math.round((val / max) * 100), 100);
}

function fmt(mb: number) {
  if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB';
  return mb.toFixed(0) + ' MB';
}

function barVariant(val: number, max: number) {
  if (!max) return 'blue' as const;
  const r = val / max;
  if (r >= 0.9) return 'red' as const;
  if (r >= 0.7) return 'amber' as const;
  return 'blue' as const;
}

const iconMap: Record<string, { bg: string; color: string }> = {
  blue: 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400',
  violet: 'bg-violet-50 dark:bg-violet-900/30 text-violet-600 dark:text-violet-400',
  rose: 'bg-rose-50 dark:bg-rose-900/30 text-rose-600 dark:text-rose-400',
  amber: 'bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400',
};

const container = {
  hidden: { opacity: 0 },
  show: {
    opacity: 1,
    transition: { staggerChildren: 0.06 },
  },
};

const item = {
  hidden: { opacity: 0, y: 16 },
  show: { opacity: 1, y: 0, transition: { duration: 0.4 } },
};

export function ChildDashboard() {
  const [disk, setDisk] = useState<any>(null);
  const [dbs, setDbs] = useState<any[]>([]);
  const [account, setAccount] = useState<any>(null);
  const [emailCount, setEmailCount] = useState(0);
  const [recentErrors, setRecentErrors] = useState<any[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    getDiskUsage().then(setDisk).catch(() => {});
    getDatabases().then((d) => setDbs(d || [])).catch(() => {});
    getChildAccount().then(setAccount).catch(() => {});
    getEmailCount().then((c: any) => setEmailCount(c?.count ?? c ?? 0)).catch(() => {});
    getRecentErrors().then((e: any) => setRecentErrors(e || [])).catch(() => {});
  }, []);

  const du = disk?.size_mb || 0;
  const dl = account?.disk_limit_mb || 1000;
  const bu = account?.bandwidth_used_mb || 0;
  const bl = account?.bandwidth_limit_mb || 0;

  if (!account) {
    return (
      <div className="space-y-6">
        <div className="h-36 rounded-2xl bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-800 dark:to-gray-900 animate-pulse" />
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5">
              <Skeleton className="h-4 w-1/3 mb-4" />
              <Skeleton className="h-2 w-full mb-2" />
              <Skeleton className="h-2 w-3/4" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  return (
    <motion.div variants={container} initial="hidden" animate="show" className="space-y-6">
      {/* Hero banner */}
      <motion.div variants={item}>
        <div className="relative rounded-2xl bg-gradient-to-br from-blue-600 via-indigo-600 to-violet-700 p-6 md:p-8 text-white shadow-lg overflow-hidden">
          <div className="absolute top-0 right-0 w-64 h-64 bg-white/5 rounded-full -translate-y-1/2 translate-x-1/2" />
          <div className="absolute bottom-0 left-0 w-48 h-48 bg-white/5 rounded-full translate-y-1/2 -translate-x-1/2" />
          <div className="relative flex items-start justify-between gap-4">
            <div className="space-y-1">
              <p className="text-white/60 text-xs font-medium uppercase tracking-wider">Welcome back</p>
              <h1 className="text-2xl md:text-3xl font-bold tracking-tight">{account.username}</h1>
              <p className="text-white/70 text-sm">{account.domain}</p>
            </div>
            <div className="hidden sm:flex flex-wrap items-center gap-2">
              <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-white/10 text-white/80">
                <Server className="h-3.5 w-3.5" />
                {account.shared_ip}
              </span>
              <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-white/10 text-white/80">
                <Wrench className="h-3.5 w-3.5" />
                {account.package_name}
              </span>
              <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-blue-400/20 text-blue-200">
                <span className="w-1.5 h-1.5 rounded-full bg-blue-300" />
                {account.status}
              </span>
            </div>
          </div>
          <div className="flex sm:hidden flex-wrap items-center gap-2 mt-4">
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-medium bg-white/10 text-white/80">
              <Server className="h-3 w-3" />
              {account.shared_ip}
            </span>
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-medium bg-white/10 text-white/80">
              <Wrench className="h-3 w-3" />
              {account.package_name}
            </span>
            <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-medium bg-blue-400/20 text-blue-200">
              <span className="w-1.5 h-1.5 rounded-full bg-blue-300" />
              {account.status}
            </span>
          </div>
        </div>
      </motion.div>

      {/* Quick actions */}
      <motion.div variants={item} className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {[
          { to: '/child/domains', label: 'Domains', icon: Globe, color: 'blue' },
          { to: '/child/files', label: 'File Manager', icon: FolderOpen, color: 'violet' },
          { to: '/child/databases', label: 'Databases', icon: Database, color: 'rose' },
          { to: '/child/emails', label: 'Email', icon: Mail, color: 'amber' },
        ].map(({ to, label, icon: Icon, color }) => (
          <button key={to} onClick={() => navigate(to)}
            className="group flex items-center gap-3 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-4 hover:shadow-md hover:border-gray-300 dark:hover:border-gray-600 transition-all duration-200">
            <div className={`p-2.5 rounded-lg ${iconMap[color].bg} transition-transform duration-200 group-hover:scale-110`}>
              <Icon className={`h-4 w-4 ${iconMap[color].color}`} />
            </div>
            <span className="text-sm font-semibold text-gray-700 dark:text-gray-200">{label}</span>
          </button>
        ))}
      </motion.div>

      {/* Resource cards */}
      <motion.div variants={item} className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <Card hover>
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <div className={`p-2 rounded-lg ${iconMap.blue.bg}`}>
                <HardDrive className={`h-5 w-5 ${iconMap.blue.color}`} />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-800 dark:text-gray-200">Disk Usage</p>
                <p className="text-xs text-gray-400">{fmt(du)} / {dl === 0 ? '∞' : fmt(dl)}</p>
              </div>
            </div>
            <span className="text-xl font-bold text-gray-900 dark:text-gray-100">
              {dl > 0 ? pct(du, dl) : '∞'}<span className="text-sm font-normal text-gray-400">%</span>
            </span>
          </div>
          {dl > 0 ? (
            <ProgressBar value={du} max={dl} variant={barVariant(du, dl)} size="sm" />
          ) : (
            <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full" />
          )}
        </Card>

        <Card hover>
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <div className={`p-2 rounded-lg ${iconMap.violet.bg}`}>
                <Activity className={`h-5 w-5 ${iconMap.violet.color}`} />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-800 dark:text-gray-200">Bandwidth</p>
                <p className="text-xs text-gray-400">{fmt(bu)} / {bl === 0 ? '∞' : fmt(bl)}</p>
              </div>
            </div>
            <span className="text-xl font-bold text-gray-900 dark:text-gray-100">
              {bl > 0 ? pct(bu, bl) : '∞'}<span className="text-sm font-normal text-gray-400">%</span>
            </span>
          </div>
          {bl > 0 ? (
            <ProgressBar value={bu} max={bl} variant={barVariant(bu, bl)} size="sm" />
          ) : (
            <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full" />
          )}
        </Card>

        <Card hover>
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <div className={`p-2 rounded-lg ${iconMap.rose.bg}`}>
                <Database className={`h-5 w-5 ${iconMap.rose.color}`} />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-800 dark:text-gray-200">Databases</p>
                <p className="text-xs text-gray-400">{dbs.length} used{account.max_databases ? ` · ${account.max_databases} max` : ''}</p>
              </div>
            </div>
            <span className="text-xl font-bold text-gray-900 dark:text-gray-100">{dbs.length}</span>
          </div>
          {account.max_databases > 0 ? (
            <ProgressBar value={dbs.length} max={account.max_databases} variant="blue" size="sm" />
          ) : (
            <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full" />
          )}
        </Card>

        <Card hover>
          <div className="flex items-center justify-between mb-4">
            <div className="flex items-center gap-3">
              <div className={`p-2 rounded-lg ${iconMap.amber.bg}`}>
                <Mail className={`h-5 w-5 ${iconMap.amber.color}`} />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-800 dark:text-gray-200">Email Accounts</p>
                <p className="text-xs text-gray-400">{emailCount} used{account.max_email ? ` · ${account.max_email} max` : ''}</p>
              </div>
            </div>
            <span className="text-xl font-bold text-gray-900 dark:text-gray-100">{emailCount}</span>
          </div>
          {account.max_email > 0 ? (
            <ProgressBar value={emailCount} max={account.max_email} variant="amber" size="sm" />
          ) : (
            <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full" />
          )}
        </Card>
      </motion.div>

      {/* Bottom section */}
      <motion.div variants={item} className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Recent errors */}
        <div className="lg:col-span-2 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 flex items-center gap-2">
              <Bug className="h-4 w-4 text-rose-500" />
              Recent Errors
            </h3>
            {recentErrors.length > 0 && (
              <button onClick={() => navigate('/child/errors')}
                className="text-xs font-medium text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 flex items-center gap-1 transition-colors">
                View All <ArrowUpRight className="h-3 w-3" />
              </button>
            )}
          </div>
          <Card padding={false}>
            {recentErrors.length > 0 ? (
              <div className="divide-y divide-gray-100 dark:divide-gray-700/50 max-h-[260px] overflow-y-auto">
                {recentErrors.slice(0, 5).map((e, i) => (
                  <div key={i} className="flex items-start gap-3 px-5 py-3 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors">
                    <div className="p-1.5 rounded-lg bg-red-50 dark:bg-red-900/30 mt-0.5 shrink-0">
                      <AlertTriangle className="h-3 w-3 text-red-500" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 mb-0.5">
                        <span className="text-xs font-semibold text-gray-900 dark:text-gray-100">{e.domain}</span>
                        <Badge variant="error" className="text-[10px] px-1.5 py-0">{e.level}</Badge>
                      </div>
                      <p className="text-xs font-mono text-gray-500 dark:text-gray-400 truncate">{e.line}</p>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState
                icon={<ShieldCheck className="h-6 w-6 text-blue-500" />}
                title="No recent errors"
                message="Your websites are running without any issues."
              />
            )}
          </Card>
        </div>

        {/* Account details */}
        <div className="space-y-3">
          <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 flex items-center gap-2">
            <Users className="h-4 w-4 text-blue-500" />
            Account Limits
          </h3>
          <Card className="divide-y divide-gray-100 dark:divide-gray-700/50 space-y-0">
            {[
              { label: 'Domains', value: account.max_domains, icon: Globe },
              { label: 'Subdomains', value: account.max_subdomains, icon: Globe },
              { label: 'FTP Accounts', value: account.max_ftp, icon: Server },
              { label: 'Databases', value: account.max_databases, icon: Database },
              { label: 'Email', value: account.max_email, icon: Mail },
            ].map(({ label, value, icon: Icon }) => (
              <div key={label} className="flex items-center justify-between py-2.5 first:pt-0 last:pb-0">
                <div className="flex items-center gap-2.5">
                  <div className="p-1.5 rounded-md bg-gray-50 dark:bg-gray-700/50">
                    <Icon className="h-3.5 w-3.5 text-gray-500 dark:text-gray-400" />
                  </div>
                  <span className="text-sm text-gray-600 dark:text-gray-400">{label}</span>
                </div>
                <span className="text-sm font-semibold text-gray-900 dark:text-gray-100">{!value ? '∞' : value}</span>
              </div>
            ))}
          </Card>

          {/* Quick stats mini-card */}
          <Card className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">Resources</span>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="text-center p-2 rounded-lg bg-blue-50 dark:bg-blue-900/20">
                <p className="text-lg font-bold text-blue-600 dark:text-blue-400">{account.max_domains || '∞'}</p>
                <p className="text-[10px] text-gray-500 dark:text-gray-400 uppercase">Domains</p>
              </div>
              <div className="text-center p-2 rounded-lg bg-violet-50 dark:bg-violet-900/20">
                <p className="text-lg font-bold text-violet-600 dark:text-violet-400">{account.max_databases || '∞'}</p>
                <p className="text-[10px] text-gray-500 dark:text-gray-400 uppercase">DBs</p>
              </div>
            </div>
          </Card>
        </div>
      </motion.div>
    </motion.div>
  );
}

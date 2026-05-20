import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { getDiskUsage, getDatabases, getChildAccount, getEmailCount } from '../lib/api';
import { HardDrive, Database, Globe, TrendingUp, Mail, FolderOpen, Server, Wrench } from 'lucide-react';
import Card from '../components/ui/Card';
import Spinner from '../components/ui/Spinner';
import Badge from '../components/ui/Badge';
import ProgressBar from '../components/ui/ProgressBar';
import Skeleton from '../components/ui/Skeleton';

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

export function ChildDashboard() {
  const [disk, setDisk] = useState<any>(null);
  const [dbs, setDbs] = useState<any[]>([]);
  const [account, setAccount] = useState<any>(null);
  const [emailCount, setEmailCount] = useState(0);

  useEffect(() => {
    getDiskUsage().then(setDisk).catch(() => {});
    getDatabases().then((d) => setDbs(d || [])).catch(() => {});
    getChildAccount().then(setAccount).catch(() => {});
    getEmailCount().then((c: any) => setEmailCount(c?.count ?? c ?? 0)).catch(() => {});
  }, []);

  const diskUsedMB = disk?.size_mb || 0;
  const diskLimitMB = account?.disk_limit_mb || 1000;

  return (
    <motion.div
      className="space-y-6"
      variants={containerVariants}
      initial="hidden"
      animate="visible"
    >
      <motion.div variants={itemVariants}>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Dashboard</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          {account ? `Welcome, ${account.username}` : 'Welcome to your hosting control panel'}
        </p>
      </motion.div>

      {/* Account info bar */}
      {account && (
        <motion.div variants={itemVariants}>
          <Card>
            <div className="flex flex-wrap items-center gap-x-6 gap-y-1.5 text-sm">
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <Globe className="h-4 w-4 text-gray-400" />
                <span className="text-gray-400">Domain:</span>
                <span className="font-medium text-gray-900 dark:text-gray-100">{account.domain}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <Server className="h-4 w-4 text-gray-400" />
                <span className="text-gray-400">Shared IP:</span>
                <span className="font-medium text-blue-600 dark:text-blue-400">{account.shared_ip}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <Wrench className="h-4 w-4 text-gray-400" />
                <span className="text-gray-400">Package:</span>
                <span className="font-medium text-gray-900 dark:text-gray-100">{account.package_name}</span>
              </div>
              <div className="flex items-center gap-1.5 text-gray-600 dark:text-gray-400">
                <FolderOpen className="h-4 w-4 text-gray-400" />
                <span className="text-gray-400">Home:</span>
                <span className="font-medium text-xs font-mono text-gray-900 dark:text-gray-100">{account.home_dir}</span>
              </div>
            </div>
          </Card>
        </motion.div>
      )}

      {/* Resource meters */}
      <motion.div variants={itemVariants} className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <Card>
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-blue-50 dark:bg-blue-900/30">
                <HardDrive className="h-4 w-4 text-blue-600 dark:text-blue-400" />
              </div>
              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Disk Space</span>
            </div>
            <span className="text-sm text-gray-500 dark:text-gray-400">
              {diskLimitMB === 0 ? '∞' : `${diskUsedMB} / ${diskLimitMB}`} MB
            </span>
          </div>
          {diskLimitMB > 0 ? (
            <ProgressBar value={diskUsedMB} max={diskLimitMB} variant={diskLimitMB > 0 && (diskUsedMB / diskLimitMB) > 0.9 ? 'amber' : 'blue'} />
          ) : (
            <p className="text-xs text-gray-400 dark:text-gray-500">Unlimited</p>
          )}
        </Card>

        <Card>
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <div className="p-1.5 rounded-lg bg-emerald-50 dark:bg-emerald-900/30">
                <TrendingUp className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              </div>
              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Bandwidth</span>
            </div>
            <span className="text-sm text-gray-500 dark:text-gray-400">
              {!account?.bandwidth_limit_mb ? '∞' : `${account?.bandwidth_used_mb || 0} / ${account?.bandwidth_limit_mb}`} MB
            </span>
          </div>
          {account?.bandwidth_limit_mb > 0 ? (
            <ProgressBar value={account?.bandwidth_used_mb || 0} max={account?.bandwidth_limit_mb} variant="emerald" />
          ) : (
            <p className="text-xs text-gray-400 dark:text-gray-500">Unlimited</p>
          )}
        </Card>

        {account && (
          <>
            <Card>
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                  <div className="p-1.5 rounded-lg bg-purple-50 dark:bg-purple-900/30">
                    <Database className="h-4 w-4 text-purple-600 dark:text-purple-400" />
                  </div>
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Databases</span>
                </div>
                <span className="text-sm text-gray-500 dark:text-gray-400">
                  {!account.max_databases ? '∞' : `${dbs.length} / ${account.max_databases}`}
                </span>
              </div>
              {account.max_databases > 0 ? (
                <ProgressBar value={dbs.length} max={account.max_databases} variant="blue" />
              ) : (
                <p className="text-xs text-gray-400 dark:text-gray-500">Unlimited</p>
              )}
            </Card>

            <Card>
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                  <div className="p-1.5 rounded-lg bg-amber-50 dark:bg-amber-900/30">
                    <Mail className="h-4 w-4 text-amber-600 dark:text-amber-400" />
                  </div>
                  <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Email Accounts</span>
                </div>
                <span className="text-sm text-gray-500 dark:text-gray-400">
                  {!account.max_email ? '∞' : `${emailCount} / ${account.max_email}`}
                </span>
              </div>
              {account.max_email > 0 ? (
                <ProgressBar value={emailCount} max={account.max_email} variant="amber" />
              ) : (
                <p className="text-xs text-gray-400 dark:text-gray-500">Unlimited</p>
              )}
            </Card>
          </>
        )}
      </motion.div>

      {/* Quick stats row */}
      {account && (
        <motion.div variants={itemVariants} className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {[
            { label: 'Domains', value: `${account.max_domains || 0} max`, icon: Globe, color: 'text-indigo-600', bg: 'bg-indigo-50' },
            { label: 'Subdomains', value: `${account.max_subdomains || 0} max`, icon: Globe, color: 'text-sky-600', bg: 'bg-sky-50' },
            { label: 'FTP Accounts', value: `${account.max_ftp || 0} max`, icon: Server, color: 'text-orange-600', bg: 'bg-orange-50' },
            { label: 'Status', value: account.status, icon: Wrench, color: 'text-emerald-600', bg: 'bg-emerald-50' },
          ].map(({ label, value, icon: Icon, color, bg }) => (
            <div key={label} className={`${bg} dark:opacity-90 rounded-xl p-3`}>
              <Icon className={`h-4 w-4 ${color} mb-1.5`} />
              <div className="text-lg font-bold text-gray-900 dark:text-gray-100 capitalize">{value}</div>
              <div className="text-xs text-gray-500 dark:text-gray-400">{label}</div>
            </div>
          ))}
        </motion.div>
      )}

      {!account && (
        <motion.div variants={itemVariants} className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {[1, 2, 3, 4].map((i) => (
            <Card key={i}>
              <Skeleton className="h-4 w-1/3 mb-3" />
              <Skeleton className="h-3 w-full mb-2" />
              <Skeleton className="h-2 w-full" />
            </Card>
          ))}
        </motion.div>
      )}
    </motion.div>
  );
}

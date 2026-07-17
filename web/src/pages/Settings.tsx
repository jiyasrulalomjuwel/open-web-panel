import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { getSettings, updateSettings } from '../lib/api';
import { Save, Settings2, Upload, Clock, Trash2, Loader2, Mail } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Skeleton from '../components/ui/Skeleton';

export function Settings() {
  const [cfg, setCfg] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    getSettings().then((d) => { setCfg(d || {}); }).catch((e: any) => console.error('Load settings:', e)).finally(() => setLoading(false));
  }, []);

  const handleChange = (key: string, val: string) => {
    setCfg((prev) => ({ ...prev, [key]: val }));
    setSaved(false);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await updateSettings(cfg);
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (e: any) { console.error('Save settings:', e); setSaved(false); }
    finally { setSaving(false); }
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-6 w-48" />
        {[1, 2, 3].map((i) => (
          <Card key={i}>
            <Skeleton className="h-4 w-1/3 mb-3" />
            <Skeleton className="h-8 w-full" />
          </Card>
        ))}
      </div>
    );
  }

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-6 max-w-2xl"
    >
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Settings</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Server-wide configuration</p>
        </div>
        <Button
          onClick={handleSave}
          disabled={saving}
          variant={saved ? 'primary' : 'primary'}
          loading={saving}
          className={saved ? '!bg-emerald-600' : ''}
        >
          <Save className="h-4 w-4" />
          {saved ? 'Saved!' : 'Save Changes'}
        </Button>
      </div>

      {/* Account Settings */}
      <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.05 }}>
        <Card>
          <div className="flex items-center gap-2 mb-4">
            <div className="p-1.5 bg-amber-50 dark:bg-amber-900/30 rounded-lg">
              <Clock className="h-4 w-4 text-amber-600 dark:text-amber-400" />
            </div>
            <h2 className="font-medium text-gray-900 dark:text-gray-100">Suspended Account Cleanup</h2>
          </div>
          <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
            Suspended accounts will be automatically removed after this many days. Set to 0 to disable auto-removal.
          </p>
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Auto-remove after (days)</label>
            <input
              type="number"
              value={cfg.suspend_auto_remove_days ?? '7'}
              onChange={(e) => handleChange('suspend_auto_remove_days', e.target.value)}
              className="w-32 px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
              min="0"
              max="365"
            />
            <span className="ml-2 text-xs text-gray-400 dark:text-gray-500">0 = never auto-remove</span>
          </div>
        </Card>
      </motion.div>

      {/* Upload Limits */}
      <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }}>
        <Card>
          <div className="flex items-center gap-2 mb-4">
            <div className="p-1.5 bg-blue-50 dark:bg-blue-900/30 rounded-lg">
              <Upload className="h-4 w-4 text-blue-600 dark:text-blue-400" />
            </div>
            <h2 className="font-medium text-gray-900 dark:text-gray-100">File Upload Limits</h2>
          </div>
          <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
            Default maximum file size for child panel uploads. Child users can request an increase up to the max limit.
          </p>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Default limit (MB)</label>
              <input
                type="number"
                value={cfg.default_upload_limit_mb ?? '2048'}
                onChange={(e) => handleChange('default_upload_limit_mb', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                min="1"
              />
              <span className="text-xs text-gray-400 dark:text-gray-500">Recommended: 2048 MB (2 GB)</span>
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Max limit (MB)</label>
              <input
                type="number"
                value={cfg.max_upload_limit_mb ?? '5120'}
                onChange={(e) => handleChange('max_upload_limit_mb', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                min="1"
              />
              <span className="text-xs text-gray-400 dark:text-gray-500">Maximum child users can request: 5120 MB (5 GB)</span>
            </div>
          </div>
        </Card>
      </motion.div>

      {/* General */}
      <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.15 }}>
        <Card>
          <div className="flex items-center gap-2 mb-4">
            <div className="p-1.5 bg-purple-50 dark:bg-purple-900/30 rounded-lg">
              <Settings2 className="h-4 w-4 text-purple-600 dark:text-purple-400" />
            </div>
            <h2 className="font-medium text-gray-900 dark:text-gray-100">General</h2>
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Site Name</label>
            <input
              type="text"
              value={cfg.site_name ?? 'OpenWebPanel'}
              onChange={(e) => handleChange('site_name', e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>
        </Card>
      </motion.div>

      {/* SMTP Relay */}
      <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.2 }}>
        <Card>
          <div className="flex items-center gap-2 mb-4">
            <div className="p-1.5 bg-emerald-50 dark:bg-emerald-900/30 rounded-lg">
              <Mail className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
            </div>
            <h2 className="font-medium text-gray-900 dark:text-gray-100">SMTP Relay (Outgoing Mail)</h2>
          </div>
          <p className="text-xs text-gray-400 dark:text-gray-500 mb-3">
            Most VPS providers block port 25. Configure an SMTP relay (SendGrid, Mailgun, Amazon SES, etc.) to send emails. If no relay is configured, the system will attempt direct delivery which may fail if port 25 is blocked.
          </p>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Relay Host</label>
              <input
                type="text"
                value={cfg.smtp_relay_host || ''}
                onChange={(e) => handleChange('smtp_relay_host', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="smtp.sendgrid.net"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Relay Port</label>
              <input
                type="number"
                value={cfg.smtp_relay_port ?? '587'}
                onChange={(e) => handleChange('smtp_relay_port', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="587"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username</label>
              <input
                type="text"
                value={cfg.smtp_relay_username || ''}
                onChange={(e) => handleChange('smtp_relay_username', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="apikey"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Password</label>
              <input
                type="password"
                value={cfg.smtp_relay_password || ''}
                onChange={(e) => handleChange('smtp_relay_password', e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder="SMTP password or API key"
              />
            </div>
          </div>
        </Card>
      </motion.div>
    </motion.div>
  );
}

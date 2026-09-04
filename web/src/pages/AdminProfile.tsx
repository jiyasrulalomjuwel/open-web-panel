import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { getMe, updateMe, getUser, setStoredUser } from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Skeleton from '../components/ui/Skeleton';

export function AdminProfile() {
  const [me, setMe] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [savingUser, setSavingUser] = useState(false);
  const [savingPass, setSavingPass] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');

  useEffect(() => {
    getMe().then((d) => {
      setMe(d);
      setUsername(d?.username || '');
    }).catch(() => {
      const u = getUser();
      if (u) {
        setMe(u);
        setUsername(u.username || '');
      }
    }).finally(() => setLoading(false));
  }, []);

  const saveUsername = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setDone('');
    setSavingUser(true);
    try {
      const updated = await updateMe({ username });
      setMe(updated);
      setStoredUser({ username: updated.username });
      setDone('Username updated.');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to update username');
    } finally {
      setSavingUser(false);
    }
  };

  const savePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setDone('');
    if (password.length < 8) {
      setError('Password must be at least 8 characters');
      return;
    }
    if (password !== confirm) {
      setError('Passwords do not match');
      return;
    }
    setSavingPass(true);
    try {
      await updateMe({ password });
      setPassword('');
      setConfirm('');
      setDone('Password updated. Use it on your next login.');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to update password');
    } finally {
      setSavingPass(false);
    }
  };

  const inputCls = "w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500";

  if (loading) {
    return <div className="space-y-4 max-w-xl"><Skeleton className="h-6 w-48" /><Card><Skeleton className="h-8 w-full" /></Card></div>;
  }

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-6 max-w-xl">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Admin Profile</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Signed in as <strong>{me?.username || 'admin'}</strong>{me?.role ? ` (${me.role})` : ''}
        </p>
      </div>

      {error && <div className="p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">{error}</div>}
      {done && <div className="p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">{done}</div>}

      <Card>
        <h2 className="font-medium text-gray-900 dark:text-gray-100 mb-4">Username</h2>
        <form onSubmit={saveUsername} className="flex gap-3">
          <input type="text" value={username} onChange={(e) => setUsername(e.target.value)}
            className={inputCls} minLength={3} maxLength={30} required />
          <Button type="submit" disabled={savingUser} loading={savingUser}>Save</Button>
        </form>
      </Card>

      <Card>
        <h2 className="font-medium text-gray-900 dark:text-gray-100 mb-4">Password</h2>
        <form onSubmit={savePassword} className="space-y-3">
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">New password</label>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              className={inputCls} placeholder="Min. 8 characters" autoComplete="new-password" />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Confirm new password</label>
            <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)}
              className={inputCls} placeholder="Repeat new password" autoComplete="new-password" />
          </div>
          <Button type="submit" disabled={savingPass} loading={savingPass}>Update password</Button>
        </form>
      </Card>
    </motion.div>
  );
}

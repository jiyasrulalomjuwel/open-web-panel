import { useState, FormEvent, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { motion, type Variants } from 'framer-motion';
import { login, loginChild, getUser } from '../lib/api';
import { Server, ArrowRight, Eye, EyeOff } from 'lucide-react';

function getPanelMode(): 'parent' | 'child' | 'both' {
  const port = window.location.port;
  if (port === '2086') return 'parent';
  if (port === '2082') return 'child';
  return 'both';
}

const fadeInUp: Variants = {
  hidden: { opacity: 0, y: 20 },
  visible: (i: number) => ({
    opacity: 1, y: 0,
    transition: { delay: i * 0.1, duration: 0.4, ease: 'easeOut' as const },
  }),
};

export function Login() {
  const panelMode = getPanelMode();
  const [mode, setMode] = useState<'parent' | 'child'>(panelMode === 'both' ? 'parent' : panelMode);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPw, setShowPw] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    if (panelMode !== 'both') setMode(panelMode);
  }, [panelMode]);

  useEffect(() => {
    const token = localStorage.getItem('owp_access_token');
    const user = getUser();
    if (token && user) {
      if (user.home_dir || user.role === 'account') {
        navigate('/child/dashboard', { replace: true });
      } else {
        navigate('/', { replace: true });
      }
    }
  }, [navigate]);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const fn = mode === 'parent' ? login : loginChild;
      await fn(username, password);
      navigate(mode === 'parent' ? '/' : '/child/dashboard');
    } catch (err: any) {
      setError(err?.error || 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex">
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.6 }}
        className="hidden lg:flex flex-col justify-center items-center w-1/2 bg-gray-900 p-12 relative overflow-hidden"
      >
        <div className="absolute inset-0 bg-gradient-to-br from-blue-600/20 to-emerald-600/20" />
        <div className="absolute top-20 left-20 w-64 h-64 bg-blue-500/10 rounded-full blur-3xl" />
        <div className="absolute bottom-20 right-20 w-96 h-96 bg-emerald-500/10 rounded-full blur-3xl" />
        <div className="relative text-center max-w-md">
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.2 }}
            className="inline-flex items-center gap-2 bg-white/10 rounded-full px-4 py-1.5 text-xs text-gray-300 mb-6"
          >
            <Server className="h-3 w-3" /> Open Source Hosting Control Panel
          </motion.div>
          <motion.h1
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.3 }}
            className="text-4xl font-bold text-white mb-3"
          >
            OpenWebPanel
          </motion.h1>
          <motion.p
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.4 }}
            className="text-gray-400 text-sm leading-relaxed"
          >
            A modern, two-tier web hosting control panel. Manage servers, accounts, and websites from a single dashboard.
          </motion.p>
        </div>
      </motion.div>

      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.4, delay: 0.2 }}
        className="flex-1 flex items-center justify-center bg-white dark:bg-gray-900 p-8"
      >
        <div className="w-full max-w-sm">
          {panelMode === 'both' && (
            <motion.div
              variants={fadeInUp}
              initial="hidden"
              animate="visible"
              custom={0}
              className="flex bg-gray-100 dark:bg-gray-800 rounded-lg p-1 mb-8"
            >
              {(['parent', 'child'] as const).map((m) => (
                <button
                  key={m}
                  onClick={() => { setMode(m); setError(''); }}
                  className={`flex-1 py-2 text-sm font-medium rounded-md transition-all ${
                    mode === m
                      ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
                      : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
                  }`}
                >
                  {m === 'parent' ? 'Admin Panel' : 'Site Panel'}
                </button>
              ))}
            </motion.div>
          )}

          {panelMode === 'parent' && (
            <motion.div
              variants={fadeInUp}
              initial="hidden"
              animate="visible"
              custom={0}
              className="mb-8"
            >
              <div className="inline-flex items-center gap-2 bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg px-3 py-1.5 text-xs text-blue-700 dark:text-blue-300">
                <Server className="h-3 w-3" /> Admin Panel — Port 2086
              </div>
            </motion.div>
          )}

          {panelMode === 'child' && (
            <motion.div
              variants={fadeInUp}
              initial="hidden"
              animate="visible"
              custom={0}
              className="mb-8"
            >
              <div className="inline-flex items-center gap-2 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg px-3 py-1.5 text-xs text-emerald-700 dark:text-emerald-300">
                <Server className="h-3 w-3" /> Site Panel — Port 2082
              </div>
            </motion.div>
          )}

          <motion.h2
            variants={fadeInUp}
            initial="hidden"
            animate="visible"
            custom={1}
            className="text-xl font-semibold text-gray-900 dark:text-gray-100 mb-1"
          >
            {mode === 'parent' ? 'Admin Login' : 'Site Owner Login'}
          </motion.h2>
          <motion.p
            variants={fadeInUp}
            initial="hidden"
            animate="visible"
            custom={2}
            className="text-sm text-gray-500 dark:text-gray-400 mb-6"
          >
            {mode === 'parent'
              ? 'Sign in to the parent control panel'
              : 'Sign in to manage your website'}
          </motion.p>

          {error && (
            <motion.div
              initial={{ opacity: 0, y: -10 }}
              animate={{ opacity: 1, y: 0 }}
              className="mb-4 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"
            >
              {error}
            </motion.div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <motion.div
              variants={fadeInUp}
              initial="hidden"
              animate="visible"
              custom={3}
            >
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username</label>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder={mode === 'parent' ? 'admin' : 'your-username'}
                autoFocus
              />
            </motion.div>
            <motion.div
              variants={fadeInUp}
              initial="hidden"
              animate="visible"
              custom={4}
            >
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Password</label>
              <div className="relative">
                <input
                  type={showPw ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  placeholder="Enter your password"
                />
                <button
                  type="button"
                  onClick={() => setShowPw(!showPw)}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </motion.div>
            <motion.button
              variants={fadeInUp}
              initial="hidden"
              animate="visible"
              custom={5}
              type="submit"
              disabled={loading || !username || !password}
              className="w-full flex items-center justify-center gap-2 py-2.5 rounded-lg text-sm font-medium text-white bg-gray-900 hover:bg-gray-800 dark:bg-blue-600 dark:hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {loading ? 'Signing in...' : 'Sign In'}
              {!loading && <ArrowRight className="h-4 w-4" />}
            </motion.button>
          </form>

          <motion.p
            variants={fadeInUp}
            initial="hidden"
            animate="visible"
            custom={6}
            className="mt-6 text-xs text-gray-400 dark:text-gray-500 text-center"
          >
            {mode === 'parent'
              ? 'Default: admin / admin123'
              : 'Use the credentials from your hosting account'}
          </motion.p>
        </div>
      </motion.div>
    </div>
  );
}

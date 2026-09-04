import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import { getAvailablePhpVersions, getCurrentPhpVersion, selectPhpVersion } from '../lib/api';
import { Code2, CheckCircle, AlertCircle, Loader2, RefreshCw, Server } from 'lucide-react';
import Button from '../components/ui/Button';

type PhpVersion = {
  id: number;
  version: string;
  socket_path: string;
  status: string;
};

export function ChildPhpVersion() {
  const [available, setAvailable] = useState<PhpVersion[]>([]);
  const [current, setCurrent] = useState<{ id: number; version: string; socket_path: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const [switching, setSwitching] = useState(false);
  const [selectedId, setSelectedId] = useState<number>(0);
  const [switchError, setSwitchError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [avail, cur] = await Promise.all([
        getAvailablePhpVersions().catch((e: any) => { console.error('Load available PHP:', e); return []; }),
        getCurrentPhpVersion().catch((e: any) => { console.error('Load current PHP:', e); return null; }),
      ]);
      setAvailable(Array.isArray(avail) ? avail : []);
      setCurrent(cur || null);
      if (cur?.id > 0) setSelectedId(cur.id);
      else if (Array.isArray(avail) && avail.length > 0) setSelectedId(avail[0].id);
    } catch (e: any) { console.error('Load PHP versions:', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { let mounted = true; load().then(() => { if (!mounted) return; }); return () => { mounted = false; }; }, [load]);

  const handleSwitch = async () => {
    if (!selectedId) return;
    setSwitching(true);
    setSwitchError('');
    try {
      const res = await selectPhpVersion(selectedId);
      setCurrent(prev => ({ ...(prev || { id: 0, version: '', socket_path: '' }), id: selectedId, version: res.version }));
    } catch (err: any) {
      setSwitchError(err?.error || 'Failed to switch PHP version.');
    } finally {
      setSwitching(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <div className="text-center">
          <Loader2 className="w-6 h-6 animate-spin text-gray-300 mx-auto mb-3" />
          <p className="text-sm text-gray-400">Loading PHP versions...</p>
        </div>
      </div>
    );
  }

  const hasChanges = selectedId !== (current?.id || 0);

  return (
    <div className="max-w-3xl mx-auto space-y-8">
      <div>
        <h1 className="text-[32px] font-bold text-gray-900 tracking-tight">PHP Version</h1>
        <p className="text-base text-gray-500 mt-1.5">
          Select and manage the PHP version for your website.
        </p>
      </div>

      <div className="bg-white rounded-card border border-border-subtle shadow-soft p-6">
        <div className="space-y-6">
          <div className="flex items-center gap-4 pb-5 border-b border-border-subtle">
            <Server className="w-5 h-5 text-gray-400 shrink-0" strokeWidth={1.5} />
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-semibold text-gray-900">Current PHP Version</p>
              <p className="text-xs text-gray-400 mt-0.5">
                {current?.version ? `PHP ${current.version} is currently active` : 'Default server PHP is being used'}
              </p>
            </div>
            <span className={`inline-flex items-center gap-1.5 px-3 py-0.5 rounded-full text-xs font-medium ${
              current?.version
                ? 'bg-[#EAF8EE] text-[#16A34A]'
                : 'bg-gray-100 text-gray-500'
            }`}>
              {current?.version || 'Default'}
            </span>
          </div>

          {switchError && (
            <div className="flex items-center gap-2.5 text-sm rounded-xl px-4 py-3 bg-[#FDECEC] text-[#DC2626]">
              <AlertCircle className="w-4 h-4 shrink-0" />
              {switchError}
            </div>
          )}

          <div>
            <label className="block text-[13px] font-semibold text-gray-900 mb-3">
              Select PHP Version
            </label>
            {available.length === 0 ? (
              <div className="bg-gray-50 rounded-xl p-8 text-center border border-dashed border-border-subtle">
                <Code2 className="w-8 h-8 text-gray-300 mx-auto mb-3" strokeWidth={1} />
                <p className="text-sm font-medium text-gray-500">No PHP versions available</p>
                <p className="text-xs text-gray-400 mt-1">Contact your administrator to install PHP versions.</p>
              </div>
            ) : (
              <div className="space-y-3">
                {available.map(v => (
                  <label
                    key={v.id}
                    className={`flex items-center gap-4 p-4 rounded-xl border-2 cursor-pointer transition-all duration-150 ${
                      selectedId === v.id
                        ? 'border-[#2563EB] bg-[#EFF6FF]'
                        : 'border-border-subtle hover:border-gray-300 hover:bg-gray-50'
                    }`}
                  >
                    <div className="relative flex items-center justify-center">
                      <input
                        type="radio"
                        name="php_version"
                        value={v.id}
                        checked={selectedId === v.id}
                        onChange={() => { setSelectedId(v.id); setSwitchError(''); }}
                        className="sr-only"
                      />
                      <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all ${
                        selectedId === v.id ? 'border-[#2563EB]' : 'border-gray-300'
                      }`}>
                        {selectedId === v.id && <div className="w-2.5 h-2.5 rounded-full bg-[#2563EB]" />}
                      </div>
                    </div>
                    <div className="flex-1 min-w-0">
                      <span className="text-sm font-semibold text-gray-900">PHP {v.version}</span>
                      <p className="text-xs text-gray-400 truncate mt-0.5 font-mono">{v.socket_path}</p>
                    </div>
                    {current?.id === v.id && (
                      <span className="inline-flex items-center gap-1.5 px-3 py-0.5 rounded-full text-xs font-medium bg-[#EAF8EE] text-[#16A34A]">
                        Active
                      </span>
                    )}
                  </label>
                ))}
              </div>
            )}
          </div>

          {hasChanges && selectedId > 0 && (
            <motion.div
              initial={{ opacity: 0, y: -8 }}
              animate={{ opacity: 1, y: 0 }}
            >
              <Button onClick={handleSwitch} loading={switching} className="w-full">
                <RefreshCw className="w-4 h-4" /> Apply PHP Version
              </Button>
            </motion.div>
          )}

          {!hasChanges && current?.version && selectedId > 0 && (
            <div className="flex items-center justify-center gap-2 text-sm text-gray-500 bg-[#EAF8EE] border border-[#22C55E]/20 rounded-xl px-4 py-3">
              <CheckCircle className="w-4 h-4 text-[#22C55E]" />
              PHP {current.version} is already active
            </div>
          )}
        </div>
      </div>

      <div className="bg-amber-50 border border-amber-200 rounded-card p-5">
        <div className="flex items-center gap-2 mb-2">
          <AlertCircle className="w-4 h-4 text-amber-600 shrink-0" />
          <p className="text-sm font-semibold text-amber-800">Important</p>
        </div>
        <p className="text-sm text-amber-700 leading-relaxed">
          Switching PHP versions will briefly reload your website's Nginx configuration.
          The process is fast and should not cause significant downtime.
        </p>
      </div>
    </div>
  );
}

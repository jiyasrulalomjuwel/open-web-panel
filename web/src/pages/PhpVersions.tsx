import { useEffect, useState, useCallback, useRef } from 'react';
import { motion } from 'framer-motion';
import { getPhpVersions, downloadPhpVersion, cancelDownloadPhpVersion, getDownloadStatuses, activatePhpVersion, deactivatePhpVersion, updatePhpVersionPackages, uninstallPhpVersion } from '../lib/api';
import { Download, Trash2, Play, Square, RefreshCw, Loader2, Globe, XCircle } from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';

type PhpVersion = {
  id: number;
  version: string;
  socket_path: string;
  status: 'not_installed' | 'downloaded' | 'activated';
  created_at: string;
  updated_at: string;
};

type DownloadStatus = {
  id: number;
  version: string;
  status: string;
  progress: string;
  error?: string;
};

const statusConfig: Record<string, { variant: 'danger' | 'warning' | 'success'; label: string }> = {
  not_installed: { variant: 'danger', label: 'Not Installed' },
  downloaded: { variant: 'warning', label: 'Downloaded' },
  activated: { variant: 'success', label: 'Activated' },
};

export function PhpVersions() {
  const [versions, setVersions] = useState<PhpVersion[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [actionId, setActionId] = useState<number | null>(null);
  const [downloads, setDownloads] = useState<Map<number, DownloadStatus>>(new Map());
  const pollRef = useRef<ReturnType<typeof setInterval>>();

  const load = useCallback(() => {
    setLoading(true);
    getPhpVersions().then(d => setVersions(d || [])).catch(() => {}).finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  // Poll download statuses when any are active
  const activeDownloads = Array.from(downloads.values()).filter(d => d.status === 'queued' || d.status === 'downloading');

  useEffect(() => {
    if (activeDownloads.length === 0) {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = undefined;
      }
      return;
    }

    const poll = async () => {
      try {
        const res = await getDownloadStatuses();
        const list: DownloadStatus[] = res?.downloads || [];
        const m = new Map<number, DownloadStatus>();
        let hasActive = false;
        for (const d of list) {
          m.set(d.id, d);
          if (d.status === 'queued' || d.status === 'downloading') hasActive = true;
        }
        setDownloads(m);
        if (!hasActive) {
          load(); // Refresh versions list when downloads finish
          if (pollRef.current) {
            clearInterval(pollRef.current);
            pollRef.current = undefined;
          }
        }
      } catch {
        // ignore poll errors
      }
    };

    poll(); // immediate
    pollRef.current = setInterval(poll, 2000);

    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [activeDownloads.length, load]);

  const handleAction = async (id: number, action: 'download' | 'activate' | 'deactivate' | 'update' | 'uninstall' | 'delete') => {
    setActionId(id);
    setError('');
    try {
      switch (action) {
        case 'download':
          await downloadPhpVersion(id);
          // Pre-fill pending state so user sees immediate feedback
          setDownloads(prev => {
            const m = new Map(prev);
            m.set(id, { id, version: '', status: 'queued', progress: 'Starting...' });
            return m;
          });
          break;
        case 'activate': await activatePhpVersion(id); break;
        case 'deactivate': await deactivatePhpVersion(id); break;
        case 'update': await updatePhpVersionPackages(id); break;
        case 'uninstall': await uninstallPhpVersion(id); break;
      }
      load();
    } catch (err: any) {
      setError(err?.error || `${action} failed`);
    } finally {
      setActionId(null);
    }
  };

  const handleCancel = async (id: number) => {
    try {
      await cancelDownloadPhpVersion(id);
      setDownloads(prev => {
        const m = new Map(prev);
        const d = m.get(id);
        if (d) { d.status = 'cancelled'; d.progress = 'Cancelled'; }
        return m;
      });
      load();
    } catch (err: any) {
      setError(err?.error || 'cancel failed');
    }
  };

  const dlFor = (id: number) => downloads.get(id);

  return (
    <div className="max-w-5xl mx-auto space-y-5">
      <div>
        <h1 className="text-lg font-semibold text-gray-900">PHP Version Management</h1>
        <p className="text-sm text-gray-500 mt-0.5">
          All supported PHP versions are listed below. Download from the official PHP repository, then activate to make them available to users.
        </p>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg px-4 py-3">
          {error}
        </div>
      )}

      <Card padding={false}>
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-5 w-5 animate-spin text-gray-400" />
          </div>
        ) : versions.length === 0 ? (
          <div className="text-center py-12">
            <p className="text-sm text-gray-500">No PHP versions found.</p>
            <p className="text-xs text-gray-400 mt-1">If this is a new installation, the default versions should be seeded automatically. Please restart the service.</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-gray-200">
                  <th className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider py-3 px-5">Version</th>
                  <th className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider py-3 px-5">Status</th>
                  <th className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider py-3 px-5">Progress</th>
                  <th className="text-right text-xs font-medium text-gray-500 uppercase tracking-wider py-3 px-5">Actions</th>
                </tr>
              </thead>
              <tbody>
                {versions.map((v, i) => {
                  const dl = dlFor(v.id);
                  const isActive = dl && (dl.status === 'queued' || dl.status === 'downloading');
                  return (
                    <motion.tr
                      key={v.id}
                      initial={{ opacity: 0, y: 8 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: i * 0.03 }}
                      className="border-b border-gray-100 hover:bg-gray-50 transition-colors"
                    >
                      <td className="py-3 px-5">
                        <span className="text-sm font-medium text-gray-900">PHP {v.version}</span>
                      </td>
                      <td className="py-3 px-5">
                        <Badge variant={dl?.status === 'failed' ? 'danger' : dl?.status === 'completed' ? 'success' : statusConfig[v.status]?.variant || 'neutral'} dot>
                          {dl?.status === 'completed' ? 'Downloaded' : dl?.status === 'failed' ? 'Failed' : dl?.status === 'cancelled' ? 'Cancelled' : statusConfig[v.status]?.label || v.status}
                        </Badge>
                      </td>
                      <td className="py-3 px-5">
                        {isActive ? (
                          <div className="flex items-center gap-2">
                            <Loader2 className="h-3.5 w-3.5 animate-spin text-purple-500 shrink-0" />
                            <span className="text-xs text-gray-600 truncate max-w-[200px]">{dl?.progress || 'Downloading...'}</span>
                          </div>
                        ) : dl?.status === 'failed' ? (
                          <span className="text-xs text-red-600 truncate max-w-[200px] block" title={dl?.error}>{dl?.error || 'Error'}</span>
                        ) : dl?.status === 'completed' ? (
                          <span className="text-xs text-green-600">Complete</span>
                        ) : dl?.status === 'cancelled' ? (
                          <span className="text-xs text-gray-400">Cancelled</span>
                        ) : (
                          <span className="text-xs text-gray-400">—</span>
                        )}
                      </td>
                      <td className="py-3 px-5 text-right">
                        <div className="flex items-center justify-end gap-2">
                          {isActive ? (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleCancel(v.id)}
                              title="Cancel download"
                            >
                              <XCircle className="h-3.5 w-3.5 text-red-500" /> Cancel
                            </Button>
                          ) : v.status === 'not_installed' ? (
                            <Button
                              variant="primary"
                              size="sm"
                              loading={actionId === v.id}
                              onClick={() => handleAction(v.id, 'download')}
                              title="Download and install packages from the official PHP repository"
                            >
                              <Download className="h-3.5 w-3.5" /> Download
                            </Button>
                          ) : v.status === 'downloaded' ? (
                            <>
                              <Button
                                variant="primary"
                                size="sm"
                                loading={actionId === v.id}
                                onClick={() => handleAction(v.id, 'activate')}
                                title="Make this version available to end users"
                              >
                                <Play className="h-3.5 w-3.5" /> Activate
                              </Button>
                              <Button
                                variant="danger"
                                size="sm"
                                loading={actionId === v.id}
                                onClick={() => handleAction(v.id, 'uninstall')}
                                title="Uninstall packages but keep the version entry"
                              >
                                <Trash2 className="h-3.5 w-3.5" /> Remove
                              </Button>
                            </>
                          ) : v.status === 'activated' ? (
                            <>
                              <Button
                                variant="secondary"
                                size="sm"
                                loading={actionId === v.id}
                                onClick={() => handleAction(v.id, 'update')}
                                title="Update PHP packages to the latest patch version"
                              >
                                <RefreshCw className="h-3.5 w-3.5" /> Update
                              </Button>
                              <Button
                                variant="secondary"
                                size="sm"
                                loading={actionId === v.id}
                                onClick={() => handleAction(v.id, 'deactivate')}
                                title="Hide this version from end users"
                              >
                                <Square className="h-3.5 w-3.5" /> Deactivate
                              </Button>
                              <Button
                                variant="danger"
                                size="sm"
                                loading={actionId === v.id}
                                onClick={() => handleAction(v.id, 'uninstall')}
                                title="Uninstall packages but keep the version entry"
                              >
                                <Trash2 className="h-3.5 w-3.5" /> Remove
                              </Button>
                            </>
                          ) : null}
                        </div>
                      </td>
                    </motion.tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4 text-sm text-blue-700">
        <p className="font-medium mb-2 flex items-center gap-1.5">
          <Globe className="h-4 w-4" /> How PHP Version Management Works
        </p>
        <ol className="list-decimal list-inside space-y-1 text-blue-600">
          <li>All supported PHP versions are pre-populated from the official PHP release repository.</li>
          <li>Click <strong>Download</strong> to download and install the PHP-FPM packages on your server.</li>
          <li>Each download runs independently with its own progress indicator and cancel button.</li>
          <li>After downloading, click <strong>Activate</strong> to make the version available to end users.</li>
          <li>Use <strong>Update</strong> to upgrade to the latest patch release of an activated version.</li>
          <li>Use <strong>Deactivate</strong> to hide a version from users while keeping it installed.</li>
          <li>Use <strong>Remove</strong> to uninstall the packages while keeping the version in the list for future re-download.</li>
        </ol>
      </div>
    </div>
  );
}

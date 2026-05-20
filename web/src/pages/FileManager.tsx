import { useEffect, useState, useCallback, useRef } from 'react';
import { useLocation } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  getFileList, readFile, writeFile, mkdir, renameFile, getDiskUsage,
  uploadFile, compressFiles, extractFile,
  getTrashList, restoreFromTrash, deletePermanently, emptyTrash,
} from '../lib/api';
import {
  Folder, File, FileText, Image, ChevronRight, Plus, Trash2, Pencil,
  FolderPlus, ArrowUp, HardDrive, X, Save, RefreshCw, Upload, FileCode,
  Package, PackageOpen, Loader2, Archive, RotateCcw, AlertTriangle,
  Film,
} from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import EmptyState from '../components/ui/EmptyState';

/* ── helpers ── */
function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type FileEntry = { name: string; type: 'file' | 'dir'; size: string; mod_time: string; perm: string };

function getIcon(entry: FileEntry) {
  if (entry.type === 'dir') return <Folder className="h-4 w-4 text-amber-500" />;
  const ext = entry.name.split('.').pop()?.toLowerCase() || '';
  if (['png','jpg','jpeg','gif','svg','webp','bmp','ico'].includes(ext)) return <Image className="h-4 w-4 text-purple-500" />;
  if (['mp4','webm','ogg','mov','avi','mkv','wmv'].includes(ext)) return <Film className="h-4 w-4 text-pink-500" />;
  if (['txt','md','json','yml','yaml','toml','xml','csv','log'].includes(ext)) return <FileText className="h-4 w-4 text-blue-500" />;
  if (['js','ts','jsx','tsx','go','py','rs','c','cpp','h','java','rb','php','css','html','sql','sh','bash'].includes(ext)) return <FileCode className="h-4 w-4 text-emerald-500" />;
  if (['zip','tar','gz','tgz','bz2','7z','rar'].includes(ext)) return <Package className="h-4 w-4 text-orange-500" />;
  return <File className="h-4 w-4 text-gray-400" />;
}

const mediaExt = new Set(['png','jpg','jpeg','gif','svg','webp','bmp','ico','mp4','webm','ogg','mov','avi','mkv','wmv']);

function getFileUrl(path: string): string {
  const token = localStorage.getItem('owp_access_token');
  return `/api/v1/child/files/download?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token || '')}`;
}

export function FileManager() {
  const [cwd, setCwd] = useState('/');
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [diskUsage, setDiskUsage] = useState<any>(null);
  const [loading, setLoading] = useState(false);
  const [view, setView] = useState<'files' | 'trash'>('files');
  const [error, setError] = useState('');
  const [successMsg, setSuccessMsg] = useState('');
  const [compressDialog, setCompressDialog] = useState(false);
  const [compressName, setCompressName] = useState('archive.zip');
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Media viewer
  const [viewerFile, setViewerFile] = useState<{ name: string; url: string; type: 'image' | 'video' } | null>(null);

  const loadDir = useCallback(async (path: string) => {
    setLoading(true);
    try {
      const data = await getFileList(path);
      if (data) { setCwd(data.path || '/'); setEntries(data.entries || []); }
      setSelected(new Set());
    } catch { /* ignore */ }
    finally { setLoading(false); }
  }, []);

  // Check for stored path from domains navigation
  const location = useLocation();
  useEffect(() => {
    const storedPath = localStorage.getItem('owp_fm_path');
    if (storedPath) { localStorage.removeItem('owp_fm_path'); loadDir(storedPath); }
    else loadDir(cwd);
    getDiskUsage().then(setDiskUsage).catch(() => {});
  }, [location.key]);

  const navigateUp = () => {
    if (cwd === '/') return;
    const parts = cwd.split('/').filter(Boolean);
    parts.pop();
    loadDir('/' + parts.join('/') || '/');
  };

  /* ── upload ── */
  const [uploading, setUploading] = useState(false);
  const [uploadPct, setUploadPct] = useState(0);
  const [uploadStatus, setUploadStatus] = useState<'uploading' | 'processing' | ''>('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    setUploading(true); setUploadPct(0); setUploadStatus('uploading');
    for (let i = 0; i < files.length; i++) {
      try {
        await uploadFile(cwd, files[i], (pct, phase) => {
          setUploadPct(pct);
          setUploadStatus(phase === 'processing' ? 'processing' : 'uploading');
        });
      } catch (err: any) {
        setError(err?.error || `Upload failed for ${files[i].name}`);
      }
    }
    setUploading(false);
    setUploadStatus('');
    if (fileInputRef.current) fileInputRef.current.value = '';
    loadDir(cwd);
  };

  /* ── compress ── */
  const handleCompress = async () => {
    if (selected.size === 0) { setError('Select files/folders first'); return; }
    setCompressDialog(true);
    setCompressName('archive.zip');
  };
  const doCompress = async () => {
    if (!compressName) return;
    try {
      await compressFiles(cwd, Array.from(selected), compressName);
      setCompressDialog(false);
      setSuccessMsg('Archive created');
      loadDir(cwd);
    } catch (err: any) {
      setError(err?.error || 'Compression failed');
    }
  };

  /* ── extract ── */
  const handleExtract = async (entryName: string) => {
    const path = cwd === '/' ? `/${entryName}` : `${cwd}/${entryName}`;
    await extractFile(path);
    loadDir(cwd);
  };

  /* ── new file / folder ── */
  const [showNew, setShowNew] = useState<'file' | 'dir' | null>(null);
  const [newName, setNewName] = useState('');
  const handleCreate = async () => {
    if (!newName) return;
    if (showNew === 'dir') await mkdir(cwd, newName);
    else await writeFile(cwd === '/' ? `/${newName}` : `${cwd}/${newName}`, '');
    setShowNew(null); setNewName(''); loadDir(cwd);
  };

  /* ── delete (move to trash) ── */
  const handleDelete = (name: string) => {
    setConfirmDelete(name);
  };
  const doDelete = async () => {
    if (!confirmDelete) return;
    const path = cwd === '/' ? `/${confirmDelete}` : `${cwd}/${confirmDelete}`;
    try {
      const { deleteFile } = await import('../lib/api');
      await deleteFile(path);
      setConfirmDelete(null);
      setSuccessMsg('Moved to trash');
      loadDir(cwd);
    } catch (err: any) {
      setError(err?.error || 'Delete failed');
    }
  };
  const handleDeleteSelected = async () => {
    if (selected.size === 0) return;
    if (!confirm(`Move ${selected.size} items to trash?`)) return;
    const { deleteFile } = await import('../lib/api');
    for (const name of selected) {
      const path = cwd === '/' ? `/${name}` : `${cwd}/${name}`;
      await deleteFile(path);
    }
    loadDir(cwd);
  };

  /* ── rename ── */
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameTo, setRenameTo] = useState('');
  const handleRename = async () => {
    if (!renaming || !renameTo) return;
    const oldPath = cwd === '/' ? `/${renaming}` : `${cwd}/${renaming}`;
    const newPath = cwd === '/' ? `/${renameTo}` : `${cwd}/${renameTo}`;
    await renameFile(oldPath, newPath);
    setRenaming(null); setRenameTo(''); loadDir(cwd);
  };

  /* ── text editor ── */
  const [editingFile, setEditingFile] = useState<{ path: string; content: string } | null>(null);
  const openEditor = async (name: string) => {
    const path = cwd === '/' ? `/${name}` : `${cwd}/${name}`;
    try {
      const data = await readFile(path);
      if (data.type === 'large') { setError('File too large (>512KB)'); return; }
      setEditingFile({ path, content: data.content });
    } catch { /* ignore */ }
  };
  const saveFile = async () => {
    if (!editingFile) return;
    await writeFile(editingFile.path, editingFile.content);
    setEditingFile(null); loadDir(cwd);
  };

  /* ── toggle selection ── */
  const toggleSelect = (name: string) => {
    setSelected(prev => { const next = new Set(prev); next.has(name) ? next.delete(name) : next.add(name); return next; });
  };

  /* ── breadcrumb ── */
  const crumbs = cwd.split('/').filter(Boolean).reduce<{ label: string; path: string }[]>((acc, part) => {
    return [...acc, { label: part, path: acc.length === 0 ? `/${part}` : `${acc[acc.length - 1].path}/${part}` }];
  }, []);

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-4"
    >
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">File Manager</h1>
          <div className="flex items-center gap-1.5 mt-1 text-xs text-gray-500 dark:text-gray-400">
            <HardDrive className="h-3 w-3" /> {diskUsage?.human || '...'}
          </div>
        </div>
        <div className="flex items-center gap-1">
          <button onClick={() => setView(v => v === 'files' ? 'trash' : 'files')}
            className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg transition-colors ${
              view === 'trash' ? 'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300' : 'bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 text-gray-500 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'
            }`}>
            {view === 'trash' ? <Folder className="h-3.5 w-3.5" /> : <Trash2 className="h-3.5 w-3.5" />}
            {view === 'trash' ? 'Files' : 'Trash'}
          </button>
          <button onClick={() => loadDir(cwd)} className="p-2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700">
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <div className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300">
          <AlertTriangle className="h-4 w-4 shrink-0" /> {error}
          <button onClick={() => setError('')} className="ml-auto text-red-400 hover:text-red-600"><X className="h-4 w-4" /></button>
        </div>
      )}
      {successMsg && (
        <div className="flex items-center gap-2 p-3 bg-emerald-50 dark:bg-emerald-900/30 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-600 dark:text-emerald-300">
          <span>{successMsg}</span>
          <button onClick={() => setSuccessMsg('')} className="ml-auto text-emerald-400 hover:text-emerald-600"><X className="h-4 w-4" /></button>
        </div>
      )}

      {view === 'trash' ? (
        <TrashView onBack={() => setView('files')} />
      ) : (
        <>
          {/* Toolbar */}
          <div className="flex items-center gap-1 flex-wrap">
            <button onClick={() => { setShowNew('dir'); setNewName(''); }} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700">
              <FolderPlus className="h-3.5 w-3.5" /> Folder
            </button>
            <button onClick={() => { setShowNew('file'); setNewName(''); }} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700">
              <Plus className="h-3.5 w-3.5" /> File
            </button>
            <button onClick={() => fileInputRef.current?.click()} disabled={uploading} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700">
              {uploading ? <Spinner /> : <Upload className="h-3.5 w-3.5" />}
              {uploading
                ? uploadStatus === 'processing'
                  ? `Processing ${uploadPct}%`
                  : `${uploadPct}%`
                : 'Upload'}
            </button>
            <input ref={fileInputRef} type="file" multiple onChange={handleUpload} className="hidden" />
            <button onClick={handleCompress} disabled={selected.size === 0} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-40">
              <Package className="h-3.5 w-3.5" /> Compress
            </button>
            <button onClick={navigateUp} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700">
              <ArrowUp className="h-3.5 w-3.5" /> Up
            </button>
            {selected.size > 0 && (
              <button onClick={handleDeleteSelected} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-800 rounded-lg hover:bg-red-100 dark:hover:bg-red-900/50">
                <Trash2 className="h-3.5 w-3.5" /> Delete ({selected.size})
              </button>
            )}
          </div>

          {/* New item inline form */}
          {showNew && (
            <div className="flex items-center gap-2 p-3 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
              <span className="text-xs text-gray-500 dark:text-gray-400">New {showNew}:</span>
              <input type="text" autoFocus value={newName}
                onChange={e => setNewName(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') handleCreate(); if (e.key === 'Escape') setShowNew(null); }}
                className="flex-1 px-2 py-1 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                placeholder={showNew === 'dir' ? 'folder-name' : 'file.txt'} />
              <button onClick={handleCreate} className="px-3 py-1 bg-gray-900 dark:bg-blue-600 text-white text-xs rounded hover:bg-gray-800 dark:hover:bg-blue-700">Create</button>
              <button onClick={() => setShowNew(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-4 w-4" /></button>
            </div>
          )}

          {/* Rename inline form */}
          {renaming && (
            <div className="flex items-center gap-2 p-3 bg-blue-50 dark:bg-blue-900/30 rounded-lg border border-blue-200 dark:border-blue-800">
              <span className="text-xs text-blue-600 dark:text-blue-300">Rename &quot;{renaming}&quot;:</span>
              <input type="text" autoFocus value={renameTo}
                onChange={e => setRenameTo(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') handleRename(); if (e.key === 'Escape') setRenaming(null); }}
                className="flex-1 px-2 py-1 border border-blue-300 dark:border-blue-600 bg-white dark:bg-gray-700 rounded text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
              <button onClick={handleRename} className="px-3 py-1 bg-blue-600 text-white text-xs rounded hover:bg-blue-700">Rename</button>
              <button onClick={() => setRenaming(null)} className="p-1 text-blue-400 hover:text-blue-600"><X className="h-4 w-4" /></button>
            </div>
          )}

          {/* Upload progress bar */}
          {uploading && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-xs font-medium text-blue-700 dark:text-blue-300">
                  {uploadStatus === 'processing' ? 'Processing on server...' : 'Uploading...'}
                </span>
                <span className="text-xs text-blue-600 dark:text-blue-400">
                  {uploadStatus === 'processing' ? 'Writing to disk...' : `${uploadPct}%`}
                </span>
              </div>
              <div className="w-full bg-blue-200 dark:bg-blue-800 rounded-full h-2 overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all duration-150 ${
                    uploadStatus === 'processing' ? 'bg-amber-500 animate-pulse' : 'bg-blue-600'
                  }`}
                  style={{ width: `${uploadPct}%` }}
                />
              </div>
            </div>
          )}

          {/* Breadcrumb */}
          <div className="flex items-center gap-1 text-sm bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 px-3 py-2 overflow-x-auto">
            <button onClick={() => loadDir('/')} className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 shrink-0">~</button>
            {crumbs.map(({ label, path }) => (
              <span key={path} className="flex items-center gap-1">
                <ChevronRight className="h-3 w-3 text-gray-300 dark:text-gray-600" />
                <button onClick={() => loadDir(path)} className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 truncate max-w-[150px]">{label}</button>
              </span>
            ))}
            <span className="flex items-center gap-1">
              <ChevronRight className="h-3 w-3 text-gray-300 dark:text-gray-600" />
              <span className="text-gray-400 dark:text-gray-500">{loading ? '...' : `${entries.length} items`}</span>
            </span>
          </div>

          {/* File list */}
          <Card padding={false}>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 w-8"></th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 w-8"></th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400">Name</th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 hidden sm:table-cell">Size</th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 hidden md:table-cell">Permissions</th>
                    <th className="text-right px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 w-28">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {cwd !== '/' && (
                    <tr className="border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-700/30 cursor-pointer" onClick={navigateUp}>
                      <td></td>
                      <td className="px-4 py-2"><Folder className="h-4 w-4 text-amber-500" /></td>
                      <td className="px-4 py-2 font-medium text-blue-600 dark:text-blue-400">..</td>
                      <td className="px-4 py-2 hidden sm:table-cell"></td>
                      <td className="px-4 py-2 hidden md:table-cell"></td>
                      <td></td>
                    </tr>
                  )}
                  {entries.map((e, i) => (
                    <motion.tr
                      key={e.name}
                      initial={{ opacity: 0, y: 5 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: i * 0.02 }}
                      className={`border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-700/30 ${selected.has(e.name) ? 'bg-blue-50 dark:bg-blue-900/20' : ''}`}>
                      <td className="px-4 py-2">
                        <input type="checkbox" checked={selected.has(e.name)} onChange={() => toggleSelect(e.name)}
                          className="rounded border-gray-300 dark:border-gray-600" />
                      </td>
                      <td className="px-4 py-2">{getIcon(e)}</td>
                      <td className="px-4 py-2">
                        {e.type === 'dir' ? (
                          <button onClick={(ev) => { ev.stopPropagation(); loadDir(cwd === '/' ? `/${e.name}` : `${cwd}/${e.name}`); }}
                            className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium">{e.name}</button>
                        ) : (
                          <button onClick={(ev) => {
                            ev.stopPropagation();
                            const ext = e.name.split('.').pop()?.toLowerCase() || '';
                            if (mediaExt.has(ext)) {
                              const url = getFileUrl(cwd === '/' ? `/${e.name}` : `${cwd}/${e.name}`);
                              setViewerFile({ name: e.name, url, type: ['mp4','webm','ogg','mov','avi','mkv','wmv'].includes(ext) ? 'video' : 'image' });
                            } else {
                              openEditor(e.name);
                            }
                          }}
                            className="text-gray-700 dark:text-gray-300 hover:text-blue-600 dark:hover:text-blue-400">{e.name}</button>
                        )}
                      </td>
                      <td className="px-4 py-2 text-gray-500 dark:text-gray-400 hidden sm:table-cell">{e.size || '—'}</td>
                      <td className="px-4 py-2 text-gray-400 dark:text-gray-500 text-xs font-mono hidden md:table-cell">{e.perm}</td>
                      <td className="px-4 py-2 text-right">
                        <div className="flex items-center justify-end gap-0.5">
                          {e.name.endsWith('.zip') && (
                            <button onClick={(ev) => { ev.stopPropagation(); handleExtract(e.name); }}
                              className="p-1 text-gray-400 hover:text-orange-600 dark:hover:text-orange-400 rounded" title="Extract">
                              <PackageOpen className="h-3.5 w-3.5" />
                            </button>
                          )}
                          <button onClick={(ev) => { ev.stopPropagation(); setRenaming(e.name); setRenameTo(e.name); }}
                            className="p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded" title="Rename">
                            <Pencil className="h-3.5 w-3.5" />
                          </button>
                          <button onClick={(ev) => { ev.stopPropagation(); handleDelete(e.name); }}
                            className="p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400 rounded" title="Move to Trash">
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </td>
                    </motion.tr>
                  ))}
                  {entries.length === 0 && !loading && (
                    <tr><td colSpan={6} className="px-4 py-12">
                      <EmptyState title="Directory empty" message="Upload files or create new items to get started" actionLabel="Upload files" onAction={() => fileInputRef.current?.click()} />
                    </td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </Card>
        </>
      )}

      {/* Compress dialog */}
      {compressDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setCompressDialog(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Compress to ZIP</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">{selected.size} item(s) selected</p>
            <div className="mb-4">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Archive name</label>
              <input type="text" value={compressName} onChange={e => setCompressName(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" />
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setCompressDialog(false)}>Cancel</Button>
              <Button onClick={doCompress}>Compress</Button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirmation modal */}
      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setConfirmDelete(null)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-2">Move to Trash</h3>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-5">Move &quot;{confirmDelete}&quot; to trash?</p>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(null)}>Cancel</Button>
              <Button variant="danger" onClick={doDelete}>Move to Trash</Button>
            </div>
          </div>
        </div>
      )}

      {/* Text editor modal */}
      {editingFile && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setEditingFile(null)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-3xl mx-4 max-h-[85vh] flex flex-col shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center gap-2"><FileCode className="h-4 w-4 text-gray-500 dark:text-gray-400" />
                <span className="font-medium text-sm text-gray-900 dark:text-gray-100 truncate max-w-md">{editingFile.path}</span></div>
              <button onClick={() => setEditingFile(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <textarea value={editingFile.content} onChange={e => setEditingFile({ ...editingFile, content: e.target.value })}
              className="flex-1 p-4 font-mono text-sm bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 focus:outline-none resize-none min-h-[300px]" spellCheck={false} />
            <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 rounded-b-xl">
              <Button variant="ghost" onClick={() => setEditingFile(null)}>Cancel</Button>
              <Button onClick={saveFile}><Save className="h-4 w-4" /> Save</Button>
            </div>
          </div>
        </div>
      )}

      {/* Media viewer modal */}
      {viewerFile && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80" onClick={() => setViewerFile(null)}>
          <div className="relative w-full h-full max-w-5xl max-h-[90vh] m-4 flex flex-col" onClick={e => e.stopPropagation()}>
            {/* Header */}
            <div className="flex items-center justify-between mb-2">
              <span className="text-sm text-white/80 truncate">{viewerFile.name}</span>
              <button onClick={() => setViewerFile(null)} className="p-1.5 text-white/60 hover:text-white rounded hover:bg-white/10">
                <X className="h-5 w-5" />
              </button>
            </div>
            {/* Content */}
            <div className="flex-1 flex items-center justify-center bg-black/40 rounded-xl overflow-hidden">
              {viewerFile.type === 'image' ? (
                <img src={viewerFile.url} alt={viewerFile.name}
                  className="max-w-full max-h-full object-contain" />
              ) : (
                <video src={viewerFile.url} controls autoPlay
                  className="max-w-full max-h-full"
                  style={{ maxWidth: '100%', maxHeight: '100%' }}>
                  Your browser does not support video playback.
                </video>
              )}
            </div>
          </div>
        </div>
      )}
    </motion.div>
  );
}

/* ── Trash View ── */
function TrashView({ onBack }: { onBack: () => void }) {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    getTrashList().then((d) => setItems(d || [])).catch(() => {}).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const handleRestore = async (id: number) => { await restoreFromTrash(id); load(); };
  const handlePermanentDelete = async (id: number) => {
    if (!confirm('Delete permanently? This cannot be undone.')) return;
    await deletePermanently(id); load();
  };
  const handleEmpty = async () => {
    if (!confirm('Empty entire trash? All items will be permanently deleted.')) return;
    await emptyTrash(); load();
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="font-medium text-gray-900 dark:text-gray-100">Trash</h3>
          <p className="text-xs text-gray-400 dark:text-gray-500">Items are automatically deleted after 30 days</p>
        </div>
        <Button onClick={handleEmpty} disabled={items.length === 0} variant="danger" size="sm">
          <Trash2 className="h-3.5 w-3.5" /> Empty Trash
        </Button>
      </div>

      {loading ? (
        <div className="space-y-3">
          {[1,2,3].map(i => (
            <Card key={i}>
              <div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 mb-2 animate-pulse" />
              <div className="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2 animate-pulse" />
            </Card>
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyState title="Trash is empty" />
      ) : (
        <div className="space-y-2">
          {items.map((item, i) => (
            <motion.div
              key={item.id}
              initial={{ opacity: 0, y: 5 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.03 }}
            >
              <Card>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="p-1.5 bg-gray-100 dark:bg-gray-700 rounded-lg">
                      {item.is_dir ? <Folder className="h-4 w-4 text-amber-500" /> : <File className="h-4 w-4 text-gray-400" />}
                    </div>
                    <div className="min-w-0">
                      <div className="text-sm text-gray-700 dark:text-gray-300 truncate">{item.original_path}</div>
                      <div className="text-xs text-gray-400">
                        {item.is_dir ? 'Directory' : `${(item.size_bytes / 1024).toFixed(1)} KB`} &middot; deleted {new Date(item.deleted_at).toLocaleDateString()}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0 ml-2">
                    <button onClick={() => handleRestore(item.id)} className="p-1.5 text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded" title="Restore">
                      <RotateCcw className="h-4 w-4" /></button>
                    <button onClick={() => handlePermanentDelete(item.id)} className="p-1.5 text-red-500 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 rounded" title="Delete permanently">
                      <Trash2 className="h-4 w-4" /></button>
                  </div>
                </div>
              </Card>
            </motion.div>
          ))}
        </div>
      )}
    </div>
  );
}

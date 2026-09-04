import { useEffect, useState, useCallback, useRef } from 'react';
import { useLocation } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  getFileList, readFile, writeFile, mkdir, renameFile, deleteFile, getDiskUsage,
  uploadFile, compressFiles, extractFile, moveFiles, copyFiles,
  getTrashList, restoreFromTrash, deletePermanently, emptyTrash,
  getFileStat, chmodFile, chownFile,
  searchFiles,
  resizeImage, rotateImage, cropImage,
  downloadMultipleFiles, getAccessToken,
} from '../lib/api';
import {
  Folder, File, FileText, Image, ChevronRight, Plus, Trash2, Pencil,
  FolderPlus, ArrowUp, HardDrive, X, Save, RefreshCw, Upload, FileCode,
  Package, PackageOpen, Loader2, Archive, RotateCcw, AlertTriangle,
  Film, Download, Move, Copy, Search, Link, Crop, Maximize,
  Shield, ShieldCheck, Users, UserCheck, Scissors,
  ClipboardList, ArrowUpDown, CopyCheck,
} from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import EmptyState from '../components/ui/EmptyState';

/* --- helpers --- */
function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

type FileEntry = { name: string; type: 'file' | 'dir'; size: string; mod_time: string; perm: string; owner?: string; group?: string };

function getIcon(entry: FileEntry) {
  if (entry.type === 'dir') return <Folder className="h-4 w-4 text-amber-500" />;
  const ext = entry.name.split('.').pop()?.toLowerCase() || '';
  if (['png','jpg','jpeg','gif','svg','webp','bmp','ico'].includes(ext)) return <Image className="h-4 w-4 text-blue-500" />;
  if (['mp4','webm','ogg','mov','avi','mkv','wmv'].includes(ext)) return <Film className="h-4 w-4 text-pink-500" />;
  if (['txt','md','json','yml','yaml','toml','xml','csv','log'].includes(ext)) return <FileText className="h-4 w-4 text-blue-500" />;
  if (['js','ts','jsx','tsx','go','py','rs','c','cpp','h','java','rb','php','css','html','sql','sh','bash'].includes(ext)) return <FileCode className="h-4 w-4 text-emerald-500" />;
  if (['zip','tar','gz','tgz','bz2','7z','rar'].includes(ext)) return <Package className="h-4 w-4 text-orange-500" />;
  return <File className="h-4 w-4 text-gray-400" />;
}

const mediaExt = new Set(['png','jpg','jpeg','gif','svg','webp','bmp','ico','mp4','webm','ogg','mov','avi','mkv','wmv']);

// Fetches a file with the Authorization header and returns an object URL so the
// JWT is never placed in the URL query (which would leak via Referer/logs).
async function loadFileUrl(path: string): Promise<string> {
  const token = getAccessToken();
  const res = await fetch(
    '/api/v1/child/files/download?path=' + encodeURIComponent(path),
    { headers: token ? { Authorization: `Bearer ${token}` } : undefined },
  );
  if (!res.ok) throw new Error('Failed to load file');
  return URL.createObjectURL(await res.blob());
}

async function downloadFile(path: string, name: string) {
  const token = getAccessToken();
  const res = await fetch(
    '/api/v1/child/files/download?path=' + encodeURIComponent(path),
    { headers: token ? { Authorization: `Bearer ${token}` } : undefined },
  );
  if (!res.ok) throw new Error('Download failed');
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/* --- syntax highlighting --- */
function highlightSyntax(code: string, filename: string): React.ReactNode[] {
  const ext = filename.split('.').pop()?.toLowerCase() || '';
  const lines = code.split('\n');
  return lines.map((line, i) => {
    let html = escapeHtml(line);
    if (['js','ts','jsx','tsx','mjs','cjs'].includes(ext)) {
      html = highlightJS(html);
    } else if (['py','python'].includes(ext)) {
      html = highlightPython(html);
    } else if (['go'].includes(ext)) {
      html = highlightGo(html);
    } else if (['php','phtml','php3','php4','php5','php7','phps'].includes(ext)) {
      html = highlightPHP(html);
    } else if (['html','htm','xhtml'].includes(ext)) {
      html = highlightHTML(html);
    } else if (['css','scss','sass','less'].includes(ext)) {
      html = highlightCSS(html);
    } else if (['json'].includes(ext)) {
      html = highlightJSON(html);
    } else if (['xml','svg','rss','atom','xsd','xslt'].includes(ext)) {
      html = highlightXML(html);
    } else if (['yaml','yml'].includes(ext)) {
      html = highlightYAML(html);
    } else if (['sql'].includes(ext)) {
      html = highlightSQL(html);
    } else if (['sh','bash','zsh','ksh'].includes(ext)) {
      html = highlightBash(html);
    } else if (['md','markdown'].includes(ext)) {
      html = highlightMarkdown(html);
    } else if (['java','c','cpp','h','hpp','cs','swift','kt','rs','rb','pl','lua','r'].includes(ext)) {
      html = highlightComments(html);
    }
    return <span key={i} dangerouslySetInnerHTML={{ __html: html + '\n' }} />;
  });
}
function escapeHtml(s: string): string {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}
function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
function wrapToken(text: string, color: string): string {
  return '<span style="color:' + color + '">' + text + '</span>';
}
function highlightJS(line: string): string {
  return line
    .replace(/(\/\/.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\/\*.*?\*\//g, wrapToken('$&', '#6a9955'))
    .replace(/\b(import|export|from|const|let|var|function|return|if|else|for|while|do|switch|case|break|continue|new|try|catch|finally|throw|async|await|class|extends|super|this|typeof|instanceof|in|of|yield|def)\b/g, wrapToken('$1', '#569cd6'))
    .replace(/\b(true|false|null|undefined|NaN)\b/g, wrapToken('$1', '#ce9178'))
    .replace(/\b(\d+\.?\d*)\b/g, wrapToken('$1', '#b5cea8'))
    .replace(/'[^']*'/g, wrapToken('$&', '#ce9178'))
    .replace(/"[^"]*"/g, wrapToken('$&', '#ce9178'))
    .replace(/`[^`]*`/g, wrapToken('$&', '#ce9178'));
}
function highlightPython(line: string): string {
  return line
    .replace(/(#.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\b(def|class|import|from|as|return|if|elif|else|for|while|try|except|finally|with|as|yield|lambda|pass|break|continue|raise|and|or|not|is|in|True|False|None|self|async|await)\b/g, wrapToken('$1', '#569cd6'))
    .replace(/\b(\d+\.?\d*)\b/g, wrapToken('$1', '#b5cea8'))
    .replace(/'[^']*'/g, wrapToken('$&', '#ce9178'))
    .replace(/"[^"]*"/g, wrapToken('$&', '#ce9178'))
    .replace(/'''[\s\S]*?'''/g, wrapToken('$&', '#6a9955'));
}
function highlightGo(line: string): string {
  return line
    .replace(/(\/\/.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\b(func|package|import|var|const|type|struct|interface|map|chan|go|defer|select|return|if|else|for|range|switch|case|default|break|continue|fallthrough|new|make|append|len|cap|copy|close|delete|panic|recover|true|false|nil)\b/g, wrapToken('$1', '#569cd6'))
    .replace(/\b(\d+\.?\d*)\b/g, wrapToken('$1', '#b5cea8'))
    .replace(/'[^']*'/g, wrapToken('$&', '#ce9178'))
    .replace(/"[^"]*"/g, wrapToken('$&', '#ce9178'))
    .replace(/`[^`]*`/g, wrapToken('$&', '#ce9178'));
}
function highlightPHP(line: string): string {
  return line
    .replace(/(\/\/.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/(#.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\/\*.*?\*\//g, wrapToken('$&', '#6a9955'))
    .replace(/\b(echo|print|return|if|else|elseif|for|foreach|while|switch|case|break|continue|function|class|new|public|private|protected|static|var|const|define|include|require|namespace|use|as|try|catch|throw|finally|abstract|interface|implements|extends|trait|final|self|\$this|true|false|null|array|isset|empty|die|exit|list)\b/g, wrapToken('$1', '#569cd6'))
    .replace(/\b(\d+\.?\d*)\b/g, wrapToken('$1', '#b5cea8'))
    .replace(/'.*?'/g, wrapToken('$&', '#ce9178'))
    .replace(/".*?"/g, wrapToken('$&', '#ce9178'))
    .replace(/\$\w+/g, wrapToken('$&', '#d4d169'));
}
function highlightHTML(line: string): string {
  return line
    .replace(/(&lt;!--[\s\S]*?--&gt;)/g, wrapToken('$1', '#6a9955'))
    .replace(/(&lt;\/?\w[\w-]*)[^&]*((\/?)&gt;)/g, (match: string, tag: string, rest: string) => {
      return wrapToken(tag, '#569cd6') + rest.replace(/(\w[\w-]*)(=)(&quot;[^&]*&quot;|'[^']*')/g, (m: string, attr: string, eq: string, val: string) => {
        return wrapToken(attr, '#9cdcfe') + eq + wrapToken(val, '#ce9178');
      });
    });
}
function highlightCSS(line: string): string {
  return line
    .replace(/\/\*.*?\*\//g, wrapToken('$&', '#6a9955'))
    .replace(/([\w-]+)\s*(?=:)/g, wrapToken('$1', '#9cdcfe'))
    .replace(/:\s*([^;{}]+)/g, (m: string, val: string) => ':' + wrapToken(val, '#ce9178'))
    .replace(/(#[0-9a-fA-F]{3,8})\b/g, wrapToken('$1', '#b5cea8'))
    .replace(/\b(\d+\.?\d*)(px|em|rem|%|vh|vw|pt|cm|mm)\b/g, wrapToken('$1$2', '#b5cea8'))
    .replace(/\.([\w-]+)/g, wrapToken('.$1', '#d4d169'))
    .replace(/#([\w-]+)/g, wrapToken('.#$1', '#d4d169'));
}
function highlightJSON(line: string): string {
  return line
    .replace(/"([^"]*)"/g, (m: string, key: string) => {
      if (/^\s*[{,\[]/.test(line) || /:\s*$/.test(line.split(key)[0] || '')) {
        return wrapToken('"', '#9cdcfe') + wrapToken(key, '#9cdcfe') + wrapToken('"', '#9cdcfe');
      }
      return wrapToken(m, '#ce9178');
    })
    .replace(/\b(true|false|null)\b/g, wrapToken('$1', '#569cd6'))
    .replace(/\b(\d+\.?\d*)\b/g, wrapToken('$1', '#b5cea8'));
}
function highlightXML(line: string): string {
  return line
    .replace(/(&lt;!--[\s\S]*?--&gt;)/g, wrapToken('$1', '#6a9955'))
    .replace(/(&lt;\/?\w[\w:-]*)/g, wrapToken('$1', '#569cd6'))
    .replace(/(\/?&gt;)/g, wrapToken('$1', '#569cd6'))
    .replace(/\s(\w[\w:-]*)=/g, (m: string) => wrapToken(m, '#9cdcfe'));
}
function highlightYAML(line: string): string {
  return line
    .replace(/(#.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/^(\s*)([\w.-]+)(:)/gm, (m: string, sp: string, key: string) => sp + wrapToken(key, '#9cdcfe') + ':')
    .replace(/:\s+(['\"]?.*['\"]?)$/g, (m: string) => wrapToken(m, '#ce9178'))
    .replace(/-\s+/g, wrapToken('- ', '#d4d169'))
    .replace(/\|\s*$/g, wrapToken('|', '#d4d169'));
}
function highlightSQL(line: string): string {
  return line
    .replace(/(--.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\b(SELECT|FROM|WHERE|INSERT|INTO|VALUES|UPDATE|SET|DELETE|CREATE|TABLE|ALTER|ADD|DROP|INDEX|JOIN|LEFT|RIGHT|INNER|OUTER|ON|AND|OR|NOT|IN|LIKE|BETWEEN|IS|NULL|AS|ORDER|BY|GROUP|HAVING|LIMIT|OFFSET|UNION|ALL|DISTINCT|COUNT|SUM|AVG|MIN|MAX|EXISTS|CASE|WHEN|THEN|ELSE|END|PRIMARY|KEY|FOREIGN|REFERENCES|CASCADE|INT|VARCHAR|TEXT|BOOLEAN|INTEGER|FLOAT|DOUBLE|DATE|TIMESTAMP|BIGINT|SMALLINT|CHAR|BLOB)\b/gi, wrapToken('$1', '#569cd6'))
    .replace(/'.*?'/g, wrapToken('$&', '#ce9178'))
    .replace(/\b(\d+)\b/g, wrapToken('$1', '#b5cea8'));
}
function highlightBash(line: string): string {
  return line
    .replace(/(#.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\b(if|then|else|elif|fi|for|while|do|done|case|esac|function|return|exit|export|local|source|\.|echo|printf|read|set|unset|declare|typeset)\b/g, wrapToken('$1', '#569cd6'))
    .replace(/'.*?'/g, wrapToken('$&', '#ce9178'))
    .replace(/".*?"/g, wrapToken('$&', '#ce9178'))
    .replace(/\b(\d+)\b/g, wrapToken('$1', '#b5cea8'));
}
function highlightMarkdown(line: string): string {
  return line
    .replace(/(#{1,6}\s+.*)/g, wrapToken('$1', '#569cd6'))
    .replace(/(\*\*.*?\*\*)/g, wrapToken('$1', '#ce9178'))
    .replace(/(`[^`]*`)/g, wrapToken('$1', '#b5cea8'))
    .replace(/^(\s*[-*]\s)/gm, wrapToken('$1', '#d4d169'));
}
function highlightComments(line: string): string {
  return line
    .replace(/(\/\/.*)/g, wrapToken('$1', '#6a9955'))
    .replace(/\/\*.*?\*\//g, wrapToken('$&', '#6a9955'))
    .replace(/'.*?'/g, wrapToken('$&', '#ce9178'))
    .replace(/".*?"/g, wrapToken('$&', '#ce9178'))
    .replace(/\b(\d+\.?\d*)\b/g, wrapToken('$1', '#b5cea8'));
}

export function FileManager() {
  const [cwd, setCwd] = useState('/');
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [diskUsage, setDiskUsage] = useState<any>(null);
  const [diskUsageError, setDiskUsageError] = useState(false);
  const diskUsageCache = useRef<{ data: any; time: number } | null>(null);
  const DISK_USAGE_TTL = 30000;
  const [loading, setLoading] = useState(false);
  const [view, setView] = useState<'files' | 'trash'>('files');
  const [error, setError] = useState('');
  const [successMsg, setSuccessMsg] = useState('');
  const [compressDialog, setCompressDialog] = useState(false);
  const [compressName, setCompressName] = useState('archive.zip');
  const [extractDialog, setExtractDialog] = useState<string | null>(null);
  const [extractDest, setExtractDest] = useState('');
  const [moveDialog, setMoveDialog] = useState(false);
  const [moveItems, setMoveItems] = useState<string[]>([]);
  const [moveDest, setMoveDest] = useState('');
  const [copyDialog, setCopyDialog] = useState(false);
  const [copyItems, setCopyItems] = useState<string[]>([]);
  const [copyDest, setCopyDest] = useState('');
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [confirmDeleteMulti, setConfirmDeleteMulti] = useState(false);
  const [overwriteConfirm, setOverwriteConfirm] = useState<{ action: 'move' | 'copy'; paths: string[]; dest: string; conflicts: string[] } | null>(null);

  // Search
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<any[]>([]);
  const [searching, setSearching] = useState(false);
  const [searchType, setSearchType] = useState<'filename' | 'content' | 'both'>('filename');

  // Permission management
  const [permDialog, setPermDialog] = useState<{ path: string; name: string; perm: string; owner: string; group: string } | null>(null);
  const [chmodOwner, setChmodOwner] = useState({ r: true, w: true, x: true });
  const [chmodGroup, setChmodGroup] = useState({ r: true, w: false, x: false });
  const [chmodOther, setChmodOther] = useState({ r: true, w: false, x: false });
  const [permOwner, setPermOwner] = useState('');
  const [permGroup, setPermGroup] = useState('');

  // Image manipulation
  const [imageDialog, setImageDialog] = useState<{ name: string; path: string } | null>(null);
  const [imagePreviewUrl, setImagePreviewUrl] = useState('');
  const [imageResizeW, setImageResizeW] = useState('');
  const [imageResizeH, setImageResizeH] = useState('');
  const [imageDegrees, setImageDegrees] = useState(90);
  const [imageCropX, setImageCropX] = useState('');
  const [imageCropY, setImageCropY] = useState('');
  const [imageCropW, setImageCropW] = useState('');
  const [imageCropH, setImageCropH] = useState('');
  const [imageProcessing, setImageProcessing] = useState(false);


  // Editor search/replace
  const [editorSearchOpen, setEditorSearchOpen] = useState(false);
  const [editorSearchQuery, setEditorSearchQuery] = useState('');
  const [editorReplace, setEditorReplace] = useState('');

  // Context menu
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; entry: FileEntry } | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const handleContextMenu = (e: React.MouseEvent, entry: FileEntry) => {
    e.preventDefault();
    e.stopPropagation();
    const menuW = 176;
    const menuH = 360;
    let x = e.clientX;
    let y = e.clientY;
    if (x + menuW > window.innerWidth - 8) {
      x = window.innerWidth - menuW - 8;
    }
    if (y + menuH > window.innerHeight - 8) {
      y = Math.max(8, e.clientY - menuH);
    }
    x = Math.max(8, x);
    y = Math.max(8, y);
    setContextMenu({ x, y, entry });
  };
  useEffect(() => {
    const close = () => setContextMenu(null);
    const handleEsc = (e: KeyboardEvent) => { if (e.key === 'Escape') setContextMenu(null); };
    if (contextMenu) {
      document.addEventListener('click', close);
      document.addEventListener('keydown', handleEsc);
    }
    return () => {
      document.removeEventListener('click', close);
      document.removeEventListener('keydown', handleEsc);
    };
  }, [contextMenu]);
  const menuAction = (fn: () => void) => { setContextMenu(null); fn(); };

  // Sorting
  const [sortBy, setSortBy] = useState<'name' | 'size' | 'time' | 'perm'>('name');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');

  const toggleSort = (field: typeof sortBy) => {
    if (sortBy === field) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc');
    } else {
      setSortBy(field);
      setSortDir('asc');
    }
  };

  const sortedEntries = [...entries].sort((a, b) => {
    const dirsFirst = (a.type === 'dir' ? 0 : 1) - (b.type === 'dir' ? 0 : 1);
    if (dirsFirst !== 0) return dirsFirst;

    let cmp = 0;
    switch (sortBy) {
      case 'name': cmp = a.name.localeCompare(b.name); break;
      case 'size': cmp = parseSize(a.size) - parseSize(b.size); break;
      case 'time': cmp = new Date(a.mod_time).getTime() - new Date(b.mod_time).getTime(); break;
      case 'perm': cmp = (a.perm || '').localeCompare(b.perm || ''); break;
    }
    return sortDir === 'asc' ? cmp : -cmp;
  });

  function parseSize(s: string): number {
    if (!s) return 0;
    const m = s.match(/^([\d.]+)\s*([KMGTP]?B?)?$/i);
    if (!m) return 0;
    const n = parseFloat(m[1]);
    const u = (m[2] || 'B').toUpperCase()[0];
    const units: Record<string, number> = { '': 1, 'B': 1, 'K': 1024, 'M': 1024 ** 2, 'G': 1024 ** 3, 'T': 1024 ** 4, 'P': 1024 ** 5 };
    return n * (units[u] || 1);
  }

  function SortIcon({ field }: { field: typeof sortBy }) {
    if (sortBy !== field) return <ArrowUpDown className="h-3 w-3 inline ml-1 opacity-30" />;
    return sortDir === 'asc'
      ? <span className="inline-flex ml-1 text-blue-500"><ArrowUpDown className="h-3 w-3" /></span>
      : <span className="inline-flex ml-1 text-blue-500"><ArrowUpDown className="h-3 w-3 rotate-180" /></span>;
  }

  // Clipboard for cut/copy/paste
  const [clipboard, setClipboard] = useState<{ items: string[]; mode: 'cut' | 'copy' } | null>(null);


  // Global Escape key --- closes all modals
  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      setContextMenu(null);
      closeViewer();
      setEditingFile(null);
      setCompressDialog(false);
      setExtractDialog(null);
      setExtractDest('');
      setMoveDialog(false);
      setMoveItems([]);
      setMoveDest('');
      setCopyDialog(false);
      setCopyItems([]);
      setCopyDest('');
      setOverwriteConfirm(null);
      setConfirmDelete(null);
      setConfirmDeleteMulti(false);
      setSearchOpen(false);
      setPermDialog(null);
      setImageDialog(null);
      setImagePreviewUrl('');
      setEditorSearchOpen(false);
    };
    document.addEventListener('keydown', handleEsc);
    return () => document.removeEventListener('keydown', handleEsc);
  }, []);

  // Media viewer
  const [viewerFile, setViewerFile] = useState<{ name: string; url: string; type: 'image' | 'video' } | null>(null);
  const dirReqSeq = useRef(0);
  const previewReqSeq = useRef(0);

  const closeViewer = useCallback(() => {
    setViewerFile((prev) => {
      if (prev?.url) URL.revokeObjectURL(prev.url);
      return null;
    });
  }, []);

  const loadDir = useCallback(async (path: string) => {
    const seq = ++dirReqSeq.current;
    setLoading(true);
    try {
      const data = await getFileList(path);
      if (seq !== dirReqSeq.current) return;
      if (data) { setCwd(data.path || '/'); setEntries(data.entries || []); }
      setSelected(new Set());
      setError('');
    } catch (err: any) {
      if (seq !== dirReqSeq.current) return;
      setError(err?.message || err?.error || 'Failed to load directory');
    }
    finally { if (seq === dirReqSeq.current) setLoading(false); }
  }, []);

  // Check for stored path from domains navigation
  const location = useLocation();
  useEffect(() => {
    let mounted = true;
    const storedPath = localStorage.getItem('owp_fm_path');
    if (storedPath) { localStorage.removeItem('owp_fm_path'); loadDir(storedPath).then(() => { if (!mounted) return; }); }
    else loadDir(cwd).then(() => { if (!mounted) return; });
    if (diskUsageCache.current && Date.now() - diskUsageCache.current.time < DISK_USAGE_TTL) {
      setDiskUsage(diskUsageCache.current.data);
      setDiskUsageError(false);
    } else {
      getDiskUsage()
        .then(data => {
          if (!mounted) return;
          setDiskUsage(data);
          setDiskUsageError(false);
          diskUsageCache.current = { data, time: Date.now() };
        })
        .catch((e: any) => {
          console.error('Disk usage:', e);
          if (!diskUsageCache.current) setDiskUsageError(true);
        });
    }
    return () => { mounted = false; };
  }, [location.key]);

  const navigateUp = () => {
    if (cwd === '/') return;
    const parts = cwd.split('/').filter(Boolean);
    parts.pop();
    loadDir('/' + parts.join('/') || '/');
  };

  /* --- upload --- */
  const [uploading, setUploading] = useState(false);
  const [uploadPct, setUploadPct] = useState(0);
  const [uploadStatus, setUploadStatus] = useState<'uploading' | 'processing' | ''>('');
  const fileInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);
  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    setUploading(true); setUploadPct(0); setUploadStatus('uploading');
    for (let i = 0; i < files.length; i++) {
      try {
        await uploadFile(cwd, files[i], (pct: number, phase: string) => {
          setUploadPct(pct);
          setUploadStatus(phase === 'processing' ? 'processing' : 'uploading');
        });
      } catch (err: any) {
        setError(err?.message || err?.error || 'Upload failed for ' + files[i].name);
      }
    }
    setUploading(false);
    setUploadStatus('');
    if (fileInputRef.current) fileInputRef.current.value = '';
    if (folderInputRef.current) folderInputRef.current.value = '';
    loadDir(cwd);
  };

  // Drag and drop upload
  const [dragOver, setDragOver] = useState(false);
  const dropRef = useRef<HTMLDivElement>(null);
  const handleDragOver = (e: React.DragEvent) => { e.preventDefault(); e.stopPropagation(); setDragOver(true); };
  const handleDragLeave = (e: React.DragEvent) => { e.preventDefault(); e.stopPropagation(); setDragOver(false); };
  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragOver(false);
    const files = e.dataTransfer.files;
    if (!files || files.length === 0) return;
    setUploading(true); setUploadPct(0); setUploadStatus('uploading');
    for (let i = 0; i < files.length; i++) {
      try {
        await uploadFile(cwd, files[i], (pct: number, phase: string) => {
          setUploadPct(pct);
          setUploadStatus(phase === 'processing' ? 'processing' : 'uploading');
        });
      } catch (err: any) {
        setError(err?.message || err?.error || 'Upload failed for ' + files[i].name);
      }
    }
    setUploading(false);
    setUploadStatus('');
    loadDir(cwd);
  };

  /* --- compress --- */
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
      setError(err?.message || err?.error || 'Compression failed');
    }
  };

  /* --- extract --- */
  const handleExtract = (entryName: string) => {
    setExtractDialog(entryName);
    setExtractDest(cwd);
  };
  const doExtract = async () => {
    if (!extractDialog) return;
    const path = cwd === '/' ? '/' + extractDialog : cwd + '/' + extractDialog;
    try {
      await extractFile(path, extractDest);
      setSuccessMsg('Archive extracted');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Extraction failed');
    }
    setExtractDialog(null);
    setExtractDest('');
    loadDir(cwd);
  };

  /* --- new file / folder --- */
  const [showNew, setShowNew] = useState<'file' | 'dir' | null>(null);
  const [newName, setNewName] = useState('');
  const handleCreate = async () => {
    if (!newName) return;
    try {
      if (showNew === 'dir') await mkdir(cwd, newName);
      else await writeFile(cwd === '/' ? '/' + newName : cwd + '/' + newName, '');
      setSuccessMsg(showNew === 'dir' ? 'Folder created' : 'File created');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Create failed');
    }
    setShowNew(null); setNewName(''); loadDir(cwd);
  };

  /* --- delete (move to trash) --- */
  const handleDelete = (name: string) => {
    setConfirmDelete(name);
  };
  const doDelete = async () => {
    if (!confirmDelete) return;
    const path = cwd === '/' ? '/' + confirmDelete : cwd + '/' + confirmDelete;
    try {
      await deleteFile(path);
      setConfirmDelete(null);
      setSuccessMsg('Moved to trash');
      setSelected(new Set());
      loadDir(cwd);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Delete failed');
    }
  };
  const handleDeleteSelected = () => {
    if (selected.size === 0) return;
    setConfirmDeleteMulti(true);
  };
  const doDeleteSelected = async () => {
    if (selected.size === 0) return;
    setConfirmDeleteMulti(false);
    const paths = Array.from(selected).map(name =>
      cwd === '/' ? '/' + name : cwd + '/' + name
    );
    const results = await Promise.allSettled(paths.map(p => deleteFile(p)));
    const failures = results.filter(r => r.status === 'rejected').length;
    if (failures > 0) {
      setError(failures + ' item(s) failed to delete');
    } else {
      setSuccessMsg('All items moved to trash');
    }
    setSelected(new Set());
    loadDir(cwd);
  };

  /* --- rename --- */
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameTo, setRenameTo] = useState('');
  const handleRename = async () => {
    if (!renaming || !renameTo) return;
    try {
      const oldPath = cwd === '/' ? '/' + renaming : cwd + '/' + renaming;
      const newPath = cwd === '/' ? '/' + renameTo : cwd + '/' + renameTo;
      await renameFile(oldPath, newPath);
      setSuccessMsg('Renamed');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Rename failed');
    }
    setRenaming(null); setRenameTo(''); loadDir(cwd);
  };

  /* --- text editor --- */
  const [editingFile, setEditingFile] = useState<{ path: string; content: string } | null>(null);
  const openEditor = async (name: string) => {
    const path = cwd === '/' ? '/' + name : cwd + '/' + name;
    try {
      const data = await readFile(path);
      if (data.type === 'large') { setError('File too large (>512KB)'); return; }
      setEditingFile({ path, content: data.content });
    } catch (e: any) {
      console.error('Read file:', e);
      setError('Failed to read file');
    }
  };
  const saveFile = async () => {
    if (!editingFile) return;
    try {
      await writeFile(editingFile.path, editingFile.content);
      setSuccessMsg('File saved');
    } catch (err: any) {
      setError(err?.message || err?.error || 'Save failed');
    }
    setEditingFile(null); loadDir(cwd);
  };

  /* --- move --- */
  const handleMove = () => {
    if (selected.size === 0) return;
    setMoveItems(Array.from(selected));
    setMoveDest(cwd);
    setMoveDialog(true);
  };
  const handleMoveSingle = (name: string) => {
    setMoveItems([name]);
    setMoveDest(cwd);
    setMoveDialog(true);
  };
  const doMove = async () => {
    if (moveItems.length === 0 || !moveDest) return;
    const paths = moveItems.map(name =>
      cwd === '/' ? '/' + name : cwd + '/' + name
    );
    try {
      const result = await moveFiles(paths, moveDest);
      if (result.status === 'partial') {
        const conflicts = result.failures.filter((f: string) => f.includes('destination already exists'));
        const otherFailures = result.failures.filter((f: string) => !f.includes('destination already exists'));
        if (conflicts.length > 0 && otherFailures.length === 0) {
          setMoveDialog(false);
          setOverwriteConfirm({ action: 'move', paths, dest: moveDest, conflicts });
        } else {
          setError(result.failures.join('; '));
        }
      } else {
        setSuccessMsg('Items moved');
        setMoveDialog(false);
        setMoveItems([]);
        setMoveDest('');
        loadDir(cwd);
      }
    } catch (err: any) {
      setError(err?.message || err?.error || 'Move failed');
    }
  };

  const doMoveConfirm = async () => {
    if (!overwriteConfirm) return;
    try {
      const result = await moveFiles(overwriteConfirm.paths, overwriteConfirm.dest, true);
      if (result.status === 'partial') {
        setError(result.failures.join('; '));
      } else {
        setSuccessMsg('Items moved');
        loadDir(cwd);
      }
    } catch (err: any) {
      setError(err?.message || err?.error || 'Move failed');
    }
    setOverwriteConfirm(null);
  };

  /* --- copy --- */
  const handleCopy = () => {
    if (selected.size === 0) return;
    setCopyItems(Array.from(selected));
    setCopyDest(cwd);
    setCopyDialog(true);
  };
  const handleCopySingle = (name: string) => {
    setCopyItems([name]);
    setCopyDest(cwd);
    setCopyDialog(true);
  };
  const doCopy = async () => {
    if (copyItems.length === 0 || !copyDest) return;
    const paths = copyItems.map(name =>
      cwd === '/' ? '/' + name : cwd + '/' + name
    );
    try {
      const result = await copyFiles(paths, copyDest);
      if (result.status === 'partial') {
        const conflicts = result.failures.filter((f: string) => f.includes('destination already exists'));
        const otherFailures = result.failures.filter((f: string) => !f.includes('destination already exists'));
        if (conflicts.length > 0 && otherFailures.length === 0) {
          setCopyDialog(false);
          setOverwriteConfirm({ action: 'copy', paths, dest: copyDest, conflicts });
        } else {
          setError(result.failures.join('; '));
        }
      } else {
        setSuccessMsg('Items copied');
        setCopyDialog(false);
        setCopyItems([]);
        setCopyDest('');
        loadDir(cwd);
      }
    } catch (err: any) {
      setError(err?.message || err?.error || 'Copy failed');
    }
  };

  const doCopyConfirm = async () => {
    if (!overwriteConfirm) return;
    try {
      const result = await copyFiles(overwriteConfirm.paths, overwriteConfirm.dest, true);
      if (result.status === 'partial') {
        setError(result.failures.join('; '));
      } else {
        setSuccessMsg('Items copied');
        loadDir(cwd);
      }
    } catch (err: any) {
      setError(err?.message || err?.error || 'Copy failed');
    }
    setOverwriteConfirm(null);
  };

  /* --- clipboard paste --- */
  const handlePaste = async () => {
    if (!clipboard || clipboard.items.length === 0) return;
    const paths = clipboard.items.map(name =>
      cwd === '/' ? '/' + name : cwd + '/' + name
    );
    try {
      if (clipboard.mode === 'cut') {
        const result = await moveFiles(paths, cwd);
        if (result.status === 'partial') {
          const conflicts = result.failures?.filter((f: string) => f.includes('destination already exists'));
          if (conflicts?.length > 0) {
            setOverwriteConfirm({ action: 'move', paths, dest: cwd, conflicts });
          } else {
            setError(result.failures?.join('; ') || 'Move failed');
          }
          return;
        }
        setSuccessMsg('Items pasted (moved)');
      } else {
        const result = await copyFiles(paths, cwd);
        if (result.status === 'partial') {
          const conflicts = result.failures?.filter((f: string) => f.includes('destination already exists'));
          if (conflicts?.length > 0) {
            setOverwriteConfirm({ action: 'copy', paths, dest: cwd, conflicts });
          } else {
            setError(result.failures?.join('; ') || 'Copy failed');
          }
          return;
        }
        setSuccessMsg('Items pasted (copied)');
      }
      setClipboard(null);
      loadDir(cwd);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Paste failed');
    }
  };

  // Keyboard shortcuts
  const keyboardShortcuts = useRef<{ [key: string]: () => void }>({});
  keyboardShortcuts.current = {
    'ctrl+a': () => {
      if (view !== 'files') return;
      if (selected.size === entries.length) setSelected(new Set());
      else setSelected(new Set(entries.map(e => e.name)));
    },
    'ctrl+f': () => {
      if (view !== 'files') return;
      setSearchOpen(o => !o);
    },
    'ctrl+c': () => {
      if (selected.size === 0 || view !== 'files') return;
      setClipboard({ items: Array.from(selected), mode: 'copy' });
      setSuccessMsg(selected.size + ' item(s) copied to clipboard');
    },
    'ctrl+x': () => {
      if (selected.size === 0 || view !== 'files') return;
      setClipboard({ items: Array.from(selected), mode: 'cut' });
      setSuccessMsg(selected.size + ' item(s) cut to clipboard');
    },
    'ctrl+v': () => {
      if (!clipboard || view !== 'files') return;
      handlePaste();
    },
    'delete': () => {
      if (view !== 'files') return;
      if (selected.size > 0) {
        handleDeleteSelected();
      }
    },
    'f2': () => {
      if (view !== 'files' || selected.size !== 1) return;
      const name = Array.from(selected)[0];
      setRenaming(name);
      setRenameTo(name);
    },
  };

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const key = e.key.toLowerCase();
      const ctrl = e.ctrlKey || e.metaKey;
      let combo = '';
      if (ctrl && key === 'a') combo = 'ctrl+a';
      else if (ctrl && key === 'f') combo = 'ctrl+f';
      else if (ctrl && key === 'c' && !editingFile) combo = 'ctrl+c';
      else if (ctrl && key === 'x' && !editingFile) combo = 'ctrl+x';
      else if (ctrl && key === 'v' && !editingFile) combo = 'ctrl+v';
      else if (key === 'delete') combo = 'delete';
      else if (key === 'f2') combo = 'f2';
      if (combo && keyboardShortcuts.current[combo]) {
        const active = document.activeElement;
        const isInput = active?.tagName === 'INPUT' || active?.tagName === 'TEXTAREA' || active?.getAttribute('contenteditable') === 'true';
        if ((ctrl || key === 'f2') && isInput) return;
        if (key === 'delete' && isInput) return;
        e.preventDefault();
        keyboardShortcuts.current[combo]();
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [editingFile]);
  /* --- file search --- */
  const handleSearch = async () => {
    if (!searchQuery.trim()) return;
    setSearching(true);
    try {
      const data = await searchFiles(searchQuery, searchType, true);
      setSearchResults(data.results || []);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Search failed');
      setSearchResults([]);
    }
    setSearching(false);
  };

  /* --- permission management --- */
  const openPermDialog = async (name: string) => {
    const path = cwd === '/' ? '/' + name : cwd + '/' + name;
    try {
      const stat = await getFileStat(path);
      setPermDialog({ path, name, perm: stat.perm_octal, owner: stat.owner, group: stat.group });
      const m = parseInt(stat.perm_octal, 8);
      setChmodOwner({ r: !!(m & 0o400), w: !!(m & 0o200), x: !!(m & 0o100) });
      setChmodGroup({ r: !!(m & 0o040), w: !!(m & 0o020), x: !!(m & 0o010) });
      setChmodOther({ r: !!(m & 0o004), w: !!(m & 0o002), x: !!(m & 0o001) });
      setPermOwner(stat.owner);
      setPermGroup(stat.group);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Failed to get file info');
    }
  };
  const doChmod = async () => {
    if (!permDialog) return;
    const modeBits = (chmodOwner.r ? 0o400 : 0) | (chmodOwner.w ? 0o200 : 0) | (chmodOwner.x ? 0o100 : 0) |
      (chmodGroup.r ? 0o040 : 0) | (chmodGroup.w ? 0o020 : 0) | (chmodGroup.x ? 0o010 : 0) |
      (chmodOther.r ? 0o004 : 0) | (chmodOther.w ? 0o002 : 0) | (chmodOther.x ? 0o001 : 0);
    try {
      await chmodFile(permDialog.path, modeBits.toString(8));
      setSuccessMsg('Permissions updated');
      setPermDialog(null);
      loadDir(cwd);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Chmod failed');
    }
  };
  const doChown = async () => {
    if (!permDialog) return;
    try {
      await chownFile(permDialog.path, permOwner || undefined, permGroup || undefined);
      setSuccessMsg('Owner/group updated');
      setPermDialog(null);
      loadDir(cwd);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Chown failed');
    }
  };

  /* --- image manipulation --- */
  const openImageDialog = (name: string) => {
    const path = cwd === '/' ? '/' + name : cwd + '/' + name;
    setImageDialog({ name, path });
    setImagePreviewUrl('');
    setImageResizeW('');
    setImageResizeH('');
    setImageDegrees(90);
    const seq = ++previewReqSeq.current;
    loadFileUrl(path)
      .then((url) => { if (seq === previewReqSeq.current) setImagePreviewUrl(url); else URL.revokeObjectURL(url); })
      .catch(() => { if (seq === previewReqSeq.current) setImagePreviewUrl(''); });
  };
  const closeImageDialog = () => {
    previewReqSeq.current++;
    if (imagePreviewUrl) URL.revokeObjectURL(imagePreviewUrl);
    setImagePreviewUrl('');
    setImageDialog(null);
  };
  const doImageResize = async () => {
    if (!imageDialog) return;
    const w = parseInt(imageResizeW);
    const h = parseInt(imageResizeH);
    if (isNaN(w) || isNaN(h) || w <= 0 || h <= 0) { setError('Invalid dimensions'); return; }
    setImageProcessing(true);
    try {
await resizeImage(imageDialog.path, w, h);
      setSuccessMsg('Image resized');
      closeImageDialog();
      loadDir(cwd);
    } catch (err: any) { setError(err?.message || err?.error || 'Resize failed'); }
    setImageProcessing(false);
  };
  const doImageRotate = async () => {
    if (!imageDialog) return;
    setImageProcessing(true);
    try {
      await rotateImage(imageDialog.path, imageDegrees);
      setSuccessMsg('Image rotated');
      closeImageDialog();
      loadDir(cwd);
    } catch (err: any) { setError(err?.message || err?.error || 'Rotate failed'); }
    setImageProcessing(false);
  };
  const doImageCrop = async () => {
    if (!imageDialog) return;
    const x = parseInt(imageCropX), y = parseInt(imageCropY), w = parseInt(imageCropW), h = parseInt(imageCropH);
    if (isNaN(x) || isNaN(y) || isNaN(w) || isNaN(h) || w <= 0 || h <= 0) { setError('Invalid crop dimensions'); return; }
    setImageProcessing(true);
    try {
      await cropImage(imageDialog.path, x, y, w, h);
      setSuccessMsg('Image cropped');
      closeImageDialog();
      loadDir(cwd);
    } catch (err: any) { setError(err?.message || err?.error || 'Crop failed'); }
    setImageProcessing(false);
  };

  /* --- multi-file download --- */
  const handleDownloadSelected = async () => {
    if (selected.size === 0) return;
    const paths = Array.from(selected).map(name => cwd === '/' ? '/' + name : cwd + '/' + name);
    try {
      await downloadMultipleFiles(paths);
    } catch (err: any) {
      setError(err?.message || err?.error || 'Download failed');
    }
  };

  /* --- copy file path --- */
  const copyPath = (name: string) => {
    const path = cwd === '/' ? '/' + name : cwd + '/' + name;
    navigator.clipboard.writeText(path).then(() => {
      setSuccessMsg('Path copied: ' + path);
    }).catch(() => {
      setError('Failed to copy path');
    });
  };

  /* --- editor search/replace --- */
  const handleEditorSearch = () => {
    if (!editingFile || !editorSearchQuery) return;
    const content = editingFile.content;
    const idx = content.toLowerCase().indexOf(editorSearchQuery.toLowerCase());
    if (idx >= 0) {
      const textarea = document.querySelector('.editor-textarea') as HTMLTextAreaElement;
      if (textarea) {
        textarea.focus();
        textarea.setSelectionRange(idx, idx + editorSearchQuery.length);
      }
    } else {
      setError('No more matches found');
    }
  };
  const handleEditorReplace = () => {
    if (!editingFile || !editorSearchQuery) return;
    const content = editingFile.content;
    const idx = content.toLowerCase().indexOf(editorSearchQuery.toLowerCase());
    if (idx >= 0) {
      const newContent = content.substring(0, idx) + editorReplace + content.substring(idx + editorSearchQuery.length);
      setEditingFile({ ...editingFile, content: newContent });
    } else {
      setError('No matches found');
    }
  };
  const handleEditorReplaceAll = () => {
    if (!editingFile || !editorSearchQuery) return;
    const re = new RegExp(editorSearchQuery.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi');
    const newContent = editingFile.content.replace(re, editorReplace);
    setEditingFile({ ...editingFile, content: newContent });
    setSuccessMsg('Replaced all occurrences of "' + editorSearchQuery + '"');
  };

  /* --- toggle selection --- */
  const toggleSelect = (name: string) => {
    setSelected(prev => { const next = new Set(prev); next.has(name) ? next.delete(name) : next.add(name); return next; });
  };

  /* --- breadcrumb --- */
  const crumbs = cwd.split('/').filter(Boolean).reduce<{ label: string; path: string }[]>((acc, part) => {
    return [...acc, { label: part, path: acc.length === 0 ? '/' + part : acc[acc.length - 1].path + '/' + part }];
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
            <HardDrive className="h-3 w-3" /> {diskUsageError ? 'Error' : diskUsage?.human || '...'}
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
            <button onClick={() => folderInputRef.current?.click()} disabled={uploading} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700">
              <FolderPlus className="h-3.5 w-3.5" /> Folder Upload
            </button>
            <input ref={folderInputRef} type="file" {...{webkitdirectory: ""} as any} directory="" multiple onChange={handleUpload} className="hidden" />

            <span className="w-px h-5 bg-gray-200 dark:bg-gray-700 mx-1" />

            <button onClick={() => setSearchOpen(!searchOpen)} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700">
              <Search className="h-3.5 w-3.5" /> Search
            </button>


            <span className="w-px h-5 bg-gray-200 dark:bg-gray-700 mx-1" />

            <button onClick={() => {
              if (selected.size === entries.length) setSelected(new Set());
              else setSelected(new Set(entries.map(e => e.name)));
            }} disabled={entries.length === 0} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-40">
              {selected.size === entries.length && entries.length > 0 ? 'Deselect All' : 'Select All'}
            </button>

            <span className="w-px h-5 bg-gray-200 dark:bg-gray-700 mx-1" />

            <button onClick={handleCompress} disabled={selected.size === 0} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-40">
              <Package className="h-3.5 w-3.5" /> Compress
            </button>
            <button onClick={handleDownloadSelected} disabled={selected.size === 0} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-40">
              <Download className="h-3.5 w-3.5" /> Download
            </button>
            <button onClick={handleMove} disabled={selected.size === 0} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-40">
              <Move className="h-3.5 w-3.5" /> Move
            </button>
            <button onClick={handleCopy} disabled={selected.size === 0} className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-40">
              <Copy className="h-3.5 w-3.5" /> Copy
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

          {/* Selection counter */}
          {selected.size > 0 && (
            <div className="flex items-center gap-2 px-3 py-1.5 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg text-xs">
              <span className="text-blue-700 dark:text-blue-300">{selected.size} of {entries.length} items selected</span>
              <span className="w-px h-4 bg-blue-200 dark:bg-blue-700 mx-1" />
              {clipboard && (
                <span className="text-blue-600 dark:text-blue-400">
                  Clipboard: {clipboard.items.length} item(s) ({clipboard.mode === 'cut' ? 'cut' : 'copy'})
                </span>
              )}
              {clipboard && (
                <>
                  <span className="w-px h-4 bg-blue-200 dark:bg-blue-700 mx-1" />
                  <button onClick={() => setClipboard(null)} className="text-blue-500 hover:text-blue-700 dark:hover:text-blue-300 underline">Clear</button>
                </>
              )}
              <button onClick={() => setSelected(new Set())} className="ml-auto text-blue-500 hover:text-blue-700 dark:hover:text-blue-300 underline">Clear selection</button>
            </div>
          )}

          {/* Paste bar */}
          {clipboard && clipboard.items.length > 0 && (
            <div className="flex items-center gap-2 px-3 py-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg text-xs text-blue-700 dark:text-blue-300">
              <ClipboardList className="h-3.5 w-3.5" />
              <span>{clipboard.items.length} item(s) on clipboard ({clipboard.mode === 'cut' ? 'Cut' : 'Copy'}) -- paste into current directory</span>
              <button onClick={handlePaste} className="ml-auto px-2.5 py-1 bg-blue-600 text-white text-xs rounded hover:bg-blue-700">
                <CopyCheck className="h-3 w-3 inline mr-1" /> Paste here
              </button>
              <button onClick={() => setClipboard(null)} className="p-1 text-blue-400 hover:text-blue-600"><X className="h-3.5 w-3.5" /></button>
            </div>
          )}

          {/* Search bar */}
          {searchOpen && (
            <div className="p-3 bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 space-y-2">
              <div className="flex items-center gap-2">
                <input type="text" value={searchQuery} onChange={e => setSearchQuery(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') handleSearch(); }}
                  placeholder="Search by filename, content..."
                  className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500" autoFocus />
                <select value={searchType} onChange={e => setSearchType(e.target.value as any)}
                  className="px-2 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500">
                  <option value="filename">Filename</option>
                  <option value="content">Content</option>
                  <option value="both">Both</option>
                </select>
                <button onClick={handleSearch} disabled={searching || !searchQuery.trim()}
                  className="px-3 py-2 bg-blue-600 text-white text-sm rounded-lg hover:bg-blue-700 disabled:opacity-50">
                  {searching ? <Spinner /> : 'Search'}
                </button>
                <button onClick={() => { setSearchOpen(false); setSearchResults([]); }} className="p-2 text-gray-400 hover:text-gray-600"><X className="h-4 w-4" /></button>
              </div>
              {searchResults.length > 0 && (
                <div className="max-h-60 overflow-y-auto border border-gray-100 dark:border-gray-700 rounded-lg">
                  {searchResults.map((r, i) => (
                    <div key={i} onClick={() => loadDir('/' + r.path.split('/').slice(0, -1).join('/'))}
                      className="flex items-center gap-2 px-3 py-2 text-xs hover:bg-gray-50 dark:hover:bg-gray-700/50 cursor-pointer border-b border-gray-100 dark:border-gray-700/50 last:border-0">
                      {r.type === 'dir' ? <Folder className="h-3 w-3 text-amber-500 shrink-0" /> : <File className="h-3 w-3 text-blue-500 shrink-0" />}
                      <span className="text-gray-700 dark:text-gray-300 truncate">{r.path.replace(/^\//, '')}</span>
                      {r.match && <span className="text-gray-400 truncate ml-2 max-w-[200px]">&mdash; {r.match}</span>}
                      {r.line && <span className="text-gray-400 shrink-0">line {r.line}</span>}
                    </div>
                  ))}
                </div>
              )}
              {searchResults.length === 0 && searchQuery && !searching && (
                <p className="text-xs text-gray-400">No results found</p>
              )}
            </div>
          )}

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

          {/* File list with drag-drop support */}
          <Card padding={false}>
            <div ref={dropRef}
              className={`overflow-x-auto transition-colors ${dragOver ? 'bg-blue-50 dark:bg-blue-900/20 ring-2 ring-blue-400 ring-inset' : ''}`}
              onDragOver={handleDragOver}
              onDragLeave={handleDragLeave}
              onDrop={handleDrop}
            >
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 w-8">
                      <input type="checkbox"
                        checked={entries.length > 0 && selected.size === entries.length}
                        onChange={() => {
                          if (selected.size === entries.length) setSelected(new Set());
                          else setSelected(new Set(entries.map(e => e.name)));
                        }}
                        className="rounded border-gray-300 dark:border-gray-600" />
                    </th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 w-8"></th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 cursor-pointer select-none hover:text-gray-700 dark:hover:text-gray-300" onClick={() => toggleSort('name')}>
                      Name <SortIcon field="name" />
                    </th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 hidden sm:table-cell cursor-pointer select-none hover:text-gray-700 dark:hover:text-gray-300" onClick={() => toggleSort('size')}>
                      Size <SortIcon field="size" />
                    </th>
                    <th className="text-left px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 hidden md:table-cell cursor-pointer select-none hover:text-gray-700 dark:hover:text-gray-300" onClick={() => toggleSort('perm')}>
                      Permissions <SortIcon field="perm" />
                    </th>
                    <th className="text-right px-4 py-2.5 font-medium text-gray-500 dark:text-gray-400 w-36">Actions</th>
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
                  {sortedEntries.map((e, i) => (
                    <motion.tr
                      key={e.name}
                      initial={{ opacity: 0, y: 5 }}
                      animate={{ opacity: 1, y: 0 }}
                      transition={{ delay: i * 0.02 }}
                      onContextMenu={(ev) => handleContextMenu(ev, e)}
                      className={`border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-700/30 ${selected.has(e.name) ? 'bg-blue-50 dark:bg-blue-900/20' : ''}`}>
                      <td className="px-4 py-2">
                        <input type="checkbox" checked={selected.has(e.name)} onChange={() => toggleSelect(e.name)}
                          className="rounded border-gray-300 dark:border-gray-600" />
                      </td>
                      <td className="px-4 py-2">{getIcon(e)}</td>
                      <td className="px-4 py-2">
                        {e.type === 'dir' ? (
                          <button onClick={(ev) => { ev.stopPropagation(); loadDir(cwd === '/' ? '/' + e.name : cwd + '/' + e.name); }}
                            className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300 font-medium">{e.name}</button>
                        ) : (
                          <button onClick={(ev) => {
                            ev.stopPropagation();
                            const ext = e.name.split('.').pop()?.toLowerCase() || '';
                            if (mediaExt.has(ext)) {
                              loadFileUrl(cwd === '/' ? '/' + e.name : cwd + '/' + e.name)
                                .then((url) => {
                                  setViewerFile((prev) => {
                                    if (prev?.url && prev.url !== url) URL.revokeObjectURL(prev.url);
                                    return { name: e.name, url, type: ['mp4','webm','ogg','mov','avi','mkv','wmv'].includes(ext) ? 'video' : 'image' };
                                  });
                                })
                                .catch(() => openEditor(e.name));
                            } else {
                              openEditor(e.name);
                            }
                          }}
                            className="text-gray-700 dark:text-gray-300 hover:text-blue-600 dark:hover:text-blue-400">{e.name}</button>
                        )}
                      </td>
                      <td className="px-4 py-2 text-gray-500 dark:text-gray-400 hidden sm:table-cell">{e.size || '\u2014'}</td>
                      <td className="px-4 py-2 text-gray-400 dark:text-gray-500 text-xs font-mono hidden md:table-cell">
                        <button onClick={(ev) => { ev.stopPropagation(); openPermDialog(e.name); }}
                          className="hover:text-blue-600 dark:hover:text-blue-400 underline decoration-dotted">{e.perm}</button>
                      </td>
                      <td className="px-4 py-2 text-right">
                        <div className="flex items-center justify-end gap-0.5">
                          <button onClick={(ev) => { ev.stopPropagation(); copyPath(e.name); }}
                            className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded" title="Copy Path">
                            <Copy className="h-3.5 w-3.5" />
                          </button>
                          {e.name.endsWith('.zip') && (
                            <button onClick={(ev) => { ev.stopPropagation(); handleExtract(e.name); }}
                              className="p-1 text-gray-400 hover:text-orange-600 dark:hover:text-orange-400 rounded" title="Extract">
                              <PackageOpen className="h-3.5 w-3.5" />
                            </button>
                          )}
                          <button onClick={(ev) => { ev.stopPropagation(); openPermDialog(e.name); }}
                            className="p-1 text-gray-400 hover:text-green-600 dark:hover:text-green-400 rounded" title="Permissions">
                            <Shield className="h-3.5 w-3.5" />
                          </button>
                          {(e.name.match(/\.(png|jpg|jpeg|gif|svg|webp|bmp|ico)$/i)) && (
                            <button onClick={(ev) => { ev.stopPropagation(); openImageDialog(e.name); }}
                              className="p-1 text-gray-400 hover:text-pink-600 dark:hover:text-pink-400 rounded" title="Edit Image">
                              <Crop className="h-3.5 w-3.5" />
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
                    <tr><td colSpan={7} className="px-4 py-12">
                      <EmptyState title="Directory empty" message="Upload files or create new items to get started" actionLabel="Upload files" onAction={() => fileInputRef.current?.click()} />
                    </td></tr>
                  )}
                </tbody>
              </table>
              {dragOver && (
                <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
                  <div className="bg-white dark:bg-gray-800 rounded-xl px-6 py-4 shadow-lg border-2 border-dashed border-blue-400">
                    <Upload className="h-8 w-8 text-blue-500 mx-auto mb-1" />
                    <p className="text-sm font-medium text-gray-700 dark:text-gray-300">Drop files to upload</p>
                  </div>
                </div>
              )}
            </div>
          </Card>
        </>
      )}

      {/* Compress dialog */}
      {compressDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Compress to ZIP</h3>
              <button onClick={() => setCompressDialog(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
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

      {/* Extract dialog */}
      {extractDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Extract ZIP</h3>
              <button onClick={() => { setExtractDialog(null); setExtractDest(''); }} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">Extract &quot;{extractDialog}&quot; to:</p>
            <div className="mb-4">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Destination directory</label>
              <input type="text" value={extractDest} onChange={e => setExtractDest(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono"
                placeholder="/" />
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => { setExtractDialog(null); setExtractDest(''); }}>Cancel</Button>
              <Button onClick={doExtract}>Extract</Button>
            </div>
          </div>
        </div>
      )}

      {/* Move dialog */}
      {moveDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Move Items</h3>
              <button onClick={() => { setMoveDialog(false); setMoveItems([]); setMoveDest(''); }} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">{moveItems.length} item(s) to move</p>
            <div className="mb-4">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Destination directory</label>
              <input type="text" value={moveDest} onChange={e => setMoveDest(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono"
                placeholder="/" />
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => { setMoveDialog(false); setMoveItems([]); setMoveDest(''); }}>Cancel</Button>
              <Button onClick={doMove}>Move</Button>
            </div>
          </div>
        </div>
      )}

      {/* Copy dialog */}
      {copyDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Copy Items</h3>
              <button onClick={() => { setCopyDialog(false); setCopyItems([]); setCopyDest(''); }} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">{copyItems.length} item(s) to copy</p>
            <div className="mb-4">
              <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Destination directory</label>
              <input type="text" value={copyDest} onChange={e => setCopyDest(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono"
                placeholder="/" />
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => { setCopyDialog(false); setCopyItems([]); setCopyDest(''); }}>Cancel</Button>
              <Button onClick={doCopy}>Copy</Button>
            </div>
          </div>
        </div>
      )}

      {/* Overwrite confirmation dialog */}
      {overwriteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Confirm {overwriteConfirm.action === 'move' ? 'Move' : 'Copy'}</h3>
              <button onClick={() => setOverwriteConfirm(null)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-amber-600 dark:text-amber-400 mb-3 font-medium">A file or folder with the same name already exists at the destination and will be overwritten.</p>
            <div className="mb-4 p-3 bg-amber-50 dark:bg-amber-900/20 rounded-lg border border-amber-200 dark:border-amber-800 text-sm text-gray-700 dark:text-gray-300">
              <p className="text-xs font-medium text-amber-700 dark:text-amber-400 mb-1">Conflicting items:</p>
              {overwriteConfirm.conflicts.map((c, i) => (
                <p key={i} className="text-xs text-gray-600 dark:text-gray-400 truncate">{c.split(':')[0]}</p>
              ))}
            </div>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setOverwriteConfirm(null)}>Cancel</Button>
              <Button variant="danger" onClick={overwriteConfirm.action === 'move' ? doMoveConfirm : doCopyConfirm}>Yes, Overwrite</Button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirmation modal */}
      {confirmDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Move to Trash</h3>
              <button onClick={() => setConfirmDelete(null)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-5">Move &quot;{confirmDelete}&quot; to trash?</p>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDelete(null)}>Cancel</Button>
              <Button variant="danger" onClick={doDelete}>Move to Trash</Button>
            </div>
          </div>
        </div>
      )}
      {confirmDeleteMulti && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Move to Trash</h3>
              <button onClick={() => setConfirmDeleteMulti(false)} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300" aria-label="Close"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-5">Move {selected.size} item(s) to trash?</p>
            <div className="flex items-center justify-end gap-2">
              <Button variant="ghost" onClick={() => setConfirmDeleteMulti(false)}>Cancel</Button>
              <Button variant="danger" onClick={doDeleteSelected}>Move to Trash</Button>
            </div>
          </div>
        </div>
      )}

      {/* Code editor modal with syntax highlighting, line numbers, search/replace */}
      {editingFile && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-4xl mx-4 max-h-[90vh] flex flex-col shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center gap-2">
                <FileCode className="h-4 w-4 text-gray-500 dark:text-gray-400" />
                <span className="font-medium text-sm text-gray-900 dark:text-gray-100 truncate max-w-md">{editingFile.path}</span>
                <span className="text-xs text-gray-400">({editingFile.content.split('\n').length} lines)</span>
              </div>
              <div className="flex items-center gap-1">
                <button onClick={() => setEditorSearchOpen(!editorSearchOpen)}
                  className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700" title="Search/Replace">
                  <Search className="h-4 w-4" />
                </button>
                <button onClick={() => setEditingFile(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
              </div>
            </div>
            {editorSearchOpen && (
              <div className="flex items-center gap-2 px-5 py-2 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/30">
                <input type="text" value={editorSearchQuery} onChange={e => setEditorSearchQuery(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') handleEditorSearch(); }}
                  placeholder="Find..."
                  className="flex-1 px-3 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <input type="text" value={editorReplace} onChange={e => setEditorReplace(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') handleEditorReplace(); }}
                  placeholder="Replace..."
                  className="w-40 px-3 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <button onClick={handleEditorSearch}
                  className="px-2.5 py-1.5 bg-blue-600 text-white text-xs rounded hover:bg-blue-700">Find</button>
                <button onClick={handleEditorReplace}
                  className="px-2.5 py-1.5 bg-gray-600 text-white text-xs rounded hover:bg-gray-700">Replace</button>
                <button onClick={handleEditorReplaceAll}
                  className="px-2.5 py-1.5 bg-gray-600 text-white text-xs rounded hover:bg-gray-700">All</button>
                <button onClick={() => { setEditorSearchOpen(false); setEditorSearchQuery(''); setEditorReplace(''); }}
                  className="p-1 text-gray-400 hover:text-gray-600"><X className="h-3 w-3" /></button>
              </div>
            )}
            <div className="flex flex-1 overflow-hidden">
              <div className="select-none text-right px-3 py-4 font-mono text-sm leading-5 text-gray-400 dark:text-gray-600 bg-gray-50 dark:bg-gray-900/50 border-r border-gray-200 dark:border-gray-700 overflow-hidden min-w-[3rem]">
                {editingFile.content.split('\n').map((_, i) => (
                  <div key={i}>{i + 1}</div>
                ))}
              </div>
              <div className="relative flex-1">
                <textarea value={editingFile.content} onChange={e => setEditingFile({ ...editingFile, content: e.target.value })}
                  className="editor-textarea absolute inset-0 w-full h-full p-4 font-mono text-sm leading-5 bg-transparent text-transparent caret-gray-900 dark:caret-gray-100 focus:outline-none resize-none border-0 z-10"
                  spellCheck={false} wrap="off" />
                <pre className="p-4 font-mono text-sm leading-5 whitespace-pre-wrap break-all text-gray-900 dark:text-gray-100 pointer-events-none">
                  <code>{highlightSyntax(editingFile.content, editingFile.path)}</code>
                </pre>
              </div>
            </div>
            <div className="flex items-center justify-between px-5 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 rounded-b-xl">
              <span className="text-xs text-gray-400">{editingFile.path.split('.').pop()?.toUpperCase() || 'TXT'}</span>
              <div className="flex items-center gap-2">
                <Button variant="ghost" onClick={() => setEditingFile(null)}>Cancel</Button>
                <Button onClick={saveFile}><Save className="h-4 w-4" /> Save</Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Context menu */}
      {contextMenu && (
        <div
          ref={menuRef}
          className="fixed z-[100] w-44 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg py-1"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          onClick={(e) => e.stopPropagation()}
        >
          <button
            onClick={() => menuAction(() => openEditor(contextMenu.entry.name))}
            disabled={contextMenu.entry.type === 'dir'}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <FileCode className="h-3.5 w-3.5" /> Edit
          </button>
          <button
            onClick={() => menuAction(() => copyPath(contextMenu.entry.name))}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
          >
            <Copy className="h-3.5 w-3.5" /> Copy Path
          </button>
          <button
            onClick={() => menuAction(() => openPermDialog(contextMenu.entry.name))}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
          >
            <Shield className="h-3.5 w-3.5" /> Permissions
          </button>
          <button
            onClick={() => menuAction(() => { setRenaming(contextMenu.entry.name); setRenameTo(contextMenu.entry.name); })}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
          >
            <Pencil className="h-3.5 w-3.5" /> Rename
          </button>
          <button
            onClick={() => menuAction(() => {
              const path = cwd === '/' ? '/' + contextMenu.entry.name : cwd + '/' + contextMenu.entry.name;
              downloadFile(path, contextMenu.entry.name);
            })}
            disabled={contextMenu.entry.type === 'dir'}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300 disabled:opacity-30 disabled:cursor-not-allowed"
          >
            <Download className="h-3.5 w-3.5" /> Download
          </button>
          <button
            onClick={() => menuAction(() => handleMoveSingle(contextMenu.entry.name))}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
          >
            <Move className="h-3.5 w-3.5" /> Move
          </button>
          <button
            onClick={() => menuAction(() => handleCopySingle(contextMenu.entry.name))}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
          >
            <Copy className="h-3.5 w-3.5" /> Copy
          </button>
          {(contextMenu.entry.name.match(/\.(png|jpg|jpeg|gif|svg|webp|bmp|ico)$/i)) && (
            <button
              onClick={() => menuAction(() => openImageDialog(contextMenu.entry.name))}
              className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
            >
              <Crop className="h-3.5 w-3.5" /> Edit Image
            </button>
          )}
          <hr className="my-1 border-gray-100 dark:border-gray-700" />
          {contextMenu.entry.name.endsWith('.zip') && (
            <button
              onClick={() => menuAction(() => handleExtract(contextMenu.entry.name))}
              className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-gray-700 dark:text-gray-300 hover:bg-blue-50 dark:hover:bg-blue-900/20 hover:text-blue-700 dark:hover:text-blue-300"
            >
              <PackageOpen className="h-3.5 w-3.5" /> Extract
            </button>
          )}
          <button
            onClick={() => menuAction(() => handleDelete(contextMenu.entry.name))}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20"
          >
            <Trash2 className="h-3.5 w-3.5" /> Delete
          </button>
        </div>
      )}

      {/* Permission management dialog */}
      {permDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Permissions: {permDialog.name}</h3>
              <button onClick={() => setPermDialog(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <div className="mb-4">
              <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Change permissions</p>
              <div className="grid grid-cols-4 gap-2 text-xs">
                <div className="font-medium text-gray-500"> </div>
                <div className="text-center text-gray-500">Read</div>
                <div className="text-center text-gray-500">Write</div>
                <div className="text-center text-gray-500">Execute</div>
                <div className="font-medium text-gray-600 dark:text-gray-300">Owner</div>
                {['r','w','x'].map(p => (
                  <div key={'o'+p} className="flex justify-center">
                    <input type="checkbox" checked={chmodOwner[p as keyof typeof chmodOwner]}
                      onChange={() => setChmodOwner(prev => ({...prev, [p]: !prev[p as keyof typeof chmodOwner]}))}
                      className="rounded border-gray-300 dark:border-gray-600" />
                  </div>
                ))}
                <div className="font-medium text-gray-600 dark:text-gray-300">Group</div>
                {['r','w','x'].map(p => (
                  <div key={'g'+p} className="flex justify-center">
                    <input type="checkbox" checked={chmodGroup[p as keyof typeof chmodGroup]}
                      onChange={() => setChmodGroup(prev => ({...prev, [p]: !prev[p as keyof typeof chmodGroup]}))}
                      className="rounded border-gray-300 dark:border-gray-600" />
                  </div>
                ))}
                <div className="font-medium text-gray-600 dark:text-gray-300">Other</div>
                {['r','w','x'].map(p => (
                  <div key={'o'+p} className="flex justify-center">
                    <input type="checkbox" checked={chmodOther[p as keyof typeof chmodOther]}
                      onChange={() => setChmodOther(prev => ({...prev, [p]: !prev[p as keyof typeof chmodOther]}))}
                      className="rounded border-gray-300 dark:border-gray-600" />
                  </div>
                ))}
              </div>
              <div className="mt-2 text-xs text-gray-400">
                Octal: {((chmodOwner.r?4:0)+(chmodOwner.w?2:0)+(chmodOwner.x?1:0))}{(chmodGroup.r?4:0)+(chmodGroup.w?2:0)+(chmodGroup.x?1:0)}{(chmodOther.r?4:0)+(chmodOther.w?2:0)+(chmodOther.x?1:0)}
              </div>
            </div>
            <div className="flex items-center justify-end gap-2 mb-4">
              <Button onClick={doChmod}>Apply Permissions</Button>
            </div>
            <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
              <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Change owner/group</p>
              <div className="flex items-center gap-2 mb-3">
                <div className="flex-1">
                  <label className="block text-xs text-gray-500 mb-1">Owner</label>
                  <input type="text" value={permOwner} onChange={e => setPermOwner(e.target.value)}
                    className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                </div>
                <div className="flex-1">
                  <label className="block text-xs text-gray-500 mb-1">Group</label>
                  <input type="text" value={permGroup} onChange={e => setPermGroup(e.target.value)}
                    className="w-full px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                </div>
              </div>
              <div className="flex items-center justify-end">
                <Button onClick={doChown} variant="secondary" size="sm">Apply Owner/Group</Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Image manipulation dialog */}
      {imageDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-md mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-4">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100">Edit Image: {imageDialog.name}</h3>
              <button onClick={() => closeImageDialog()} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button>
            </div>
            <div className="mb-4 bg-gray-100 dark:bg-gray-700 rounded-lg overflow-hidden flex items-center justify-center h-40">
              <img src={imagePreviewUrl || undefined} alt={imageDialog.name}
                className="max-w-full max-h-full object-contain" />
            </div>
            <div className="mb-3">
              <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Resize</p>
              <div className="flex items-center gap-2">
                <input type="number" value={imageResizeW} onChange={e => setImageResizeW(e.target.value)} placeholder="Width"
                  className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <span className="text-gray-400">&times;</span>
                <input type="number" value={imageResizeH} onChange={e => setImageResizeH(e.target.value)} placeholder="Height"
                  className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <button onClick={doImageResize} disabled={imageProcessing}
                  className="px-2.5 py-1.5 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 disabled:opacity-50">
                  {imageProcessing ? <Spinner /> : 'Resize'}
                </button>
              </div>
            </div>
            <div className="mb-3">
              <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Rotate</p>
              <div className="flex items-center gap-2">
                <select value={imageDegrees} onChange={e => setImageDegrees(parseInt(e.target.value))}
                  className="px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500">
                  <option value={90}>90&deg;</option>
                  <option value={180}>180&deg;</option>
                  <option value={270}>270&deg;</option>
                </select>
                <button onClick={doImageRotate} disabled={imageProcessing}
                  className="px-2.5 py-1.5 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 disabled:opacity-50">
                  Rotate
                </button>
              </div>
            </div>
            <div>
              <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Crop</p>
              <div className="flex items-center gap-2">
                <input type="number" value={imageCropX} onChange={e => setImageCropX(e.target.value)} placeholder="X"
                  className="w-16 px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <input type="number" value={imageCropY} onChange={e => setImageCropY(e.target.value)} placeholder="Y"
                  className="w-16 px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <input type="number" value={imageCropW} onChange={e => setImageCropW(e.target.value)} placeholder="Width"
                  className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <input type="number" value={imageCropH} onChange={e => setImageCropH(e.target.value)} placeholder="Height"
                  className="flex-1 px-2 py-1.5 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-xs dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500" />
                <button onClick={doImageCrop} disabled={imageProcessing}
                  className="px-2.5 py-1.5 bg-blue-600 text-white text-xs rounded hover:bg-blue-700 disabled:opacity-50">
                  Crop
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Media viewer modal */}
      {viewerFile && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80">
          <div className="relative w-full h-full max-w-5xl max-h-[90vh] m-4 flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-2">
              <span className="text-sm text-white/80 truncate">{viewerFile.name}</span>
              <button onClick={closeViewer} className="p-1.5 text-white/60 hover:text-white rounded hover:bg-white/10">
                <X className="h-5 w-5" />
              </button>
            </div>
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

/* --- Trash View --- */
function TrashView({ onBack }: { onBack: () => void }) {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    getTrashList().then((d) => setItems(d || [])).catch((e: any) => console.error('Load trash:', e)).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const handleRestore = async (id: number) => {
    try { await restoreFromTrash(id); load(); } catch (e: any) { console.error('Restore:', e); }
  };
  const handlePermanentDelete = async (id: number) => {
    if (!confirm('Delete permanently? This cannot be undone.')) return;
    try { await deletePermanently(id); load(); } catch (e: any) { console.error('Permanent delete:', e); }
  };
  const handleEmpty = async () => {
    if (!confirm('Empty entire trash? All items will be permanently deleted.')) return;
    try { await emptyTrash(); load(); } catch (e: any) { console.error('Empty trash:', e); }
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

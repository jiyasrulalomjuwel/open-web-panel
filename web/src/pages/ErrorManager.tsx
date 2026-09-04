import { useEffect, useState, useCallback, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  AlertTriangle, X, Loader2, Trash2, Plus, FileCode, Eye, EyeOff,
  Globe, Bug, Check, ChevronDown, ChevronRight, Settings, RotateCcw,
  Download, Upload, ExternalLink, ToggleLeft, ToggleRight, FolderOpen,
  Search, RefreshCw, BarChart3, Clock, Link, FileText, Code, FileType,
  PanelRightOpen, PanelRightClose, Sparkles, ArrowRight, Save,
} from 'lucide-react';
import {
  getDomains, getFileList, getRecentErrors,
  getErrorPagesByDomain, getErrorPageContent, saveErrorPage,
  deleteErrorPage, toggleErrorPage, resetErrorPage,
  testErrorPage, getErrorPageStats, exportErrorPages, importErrorPages,
} from '../lib/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Skeleton from '../components/ui/Skeleton';
import { useToast } from '../components/ToastProvider';

const ERROR_CODES = [
  { code: 400, name: 'Bad Request', severity: 'warning' },
  { code: 401, name: 'Unauthorized', severity: 'warning' },
  { code: 403, name: 'Forbidden', severity: 'warning' },
  { code: 404, name: 'Not Found', severity: 'error' },
  { code: 405, name: 'Method Not Allowed', severity: 'warning' },
  { code: 408, name: 'Request Timeout', severity: 'warning' },
  { code: 410, name: 'Gone', severity: 'warning' },
  { code: 429, name: 'Too Many Requests', severity: 'warning' },
  { code: 500, name: 'Internal Server Error', severity: 'error' },
  { code: 502, name: 'Bad Gateway', severity: 'error' },
  { code: 503, name: 'Service Unavailable', severity: 'error' },
  { code: 504, name: 'Gateway Timeout', severity: 'error' },
];

const ACTION_LABELS: Record<string, string> = {
  custom_html: 'Custom HTML',
  file_manager: 'File Manager',
  internal_redirect: 'Internal Redirect',
  external_redirect: 'External URL',
  template: 'Template',
};

const ACTION_ICONS: Record<string, any> = {
  custom_html: Code,
  file_manager: FolderOpen,
  internal_redirect: ArrowRight,
  external_redirect: ExternalLink,
  template: Sparkles,
};

const SEVERITY_COLORS: Record<string, string> = {
  error: 'text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800',
  warning: 'text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800',
};

function timeAgo(t: string): string {
  if (!t) return 'Never';
  const diff = Date.now() - new Date(t + 'Z').getTime();
  if (diff < 60000) return 'Just now';
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
  return `${Math.floor(diff / 86400000)}d ago`;
}

function formatNum(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
  return String(n);
}

const defaultContent = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Error</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
           display: flex; align-items: center; justify-content: center;
           min-height: 100vh; background: #f8fafc; color: #1e293b; }
    .container { text-align: center; padding: 2rem; }
    .code { font-size: 6rem; font-weight: 800; color: #2563eb; line-height: 1; }
    .title { font-size: 1.5rem; margin: 1rem 0 0.5rem; color: #334155; }
    .desc { color: #64748b; margin-bottom: 2rem; }
    .btn { display: inline-block; padding: 0.75rem 1.5rem; background: #2563eb;
           color: #fff; border-radius: 0.5rem; text-decoration: none; font-weight: 500; }
    .btn:hover { background: #1d4ed8; }
  </style>
</head>
<body>
  <div class="container">
    <div class="code">{{ERROR_CODE}}</div>
    <h1 class="title">{{ERROR_NAME}}</h1>
    <p class="desc">Sorry, something went wrong. Please try again later.</p>
    <a href="/" class="btn">Go Home</a>
  </div>
</body>
</html>`;

interface ErrorPageRecord {
  id: number;
  domain_id: number;
  domain: string;
  error_code: number;
  content: string;
  action_type: string;
  action_value: string;
  enabled: boolean;
  hit_count: number;
  last_triggered_at: string;
  custom_headers: string;
  custom_footer: string;
  seo_noindex: boolean;
  seo_nofollow: boolean;
  seo_canonical: string;
  template: string;
  language: string;
  created_at: string;
  updated_at: string;
}

interface DomainRecord {
  id: number;
  domain: string;
  type: string;
}

interface FileEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified: string;
}

function getErrorMessage(err: any): string {
  if (typeof err === 'string') return err;
  if (err?.error) return err.error;
  if (err?.message) return err.message;
  return 'An unexpected error occurred';
}

export function ErrorManager() {
  const { toast } = useToast();
  const [domains, setDomains] = useState<DomainRecord[]>([]);
  const [selectedDomainId, setSelectedDomainId] = useState<number>(0);
  const [pages, setPages] = useState<ErrorPageRecord[]>([]);
  const [stats, setStats] = useState<any>(null);
  const [domainLoading, setDomainLoading] = useState(true);
  const [pageLoading, setPageLoading] = useState(false);
  const [actionLoading, setActionLoading] = useState<Set<number>>(new Set());
  const [filterMode, setFilterMode] = useState<'all' | 'configured'>('all');
  const [showLogs, setShowLogs] = useState(false);

  // Config panel
  const [configDomainId, setConfigDomainId] = useState<number>(0);
  const [configErrorCode, setConfigErrorCode] = useState<number>(0);
  const [configOpen, setConfigOpen] = useState(false);
  const [configRecord, setConfigRecord] = useState<ErrorPageRecord | null>(null);
  const [configSaving, setConfigSaving] = useState(false);

  // Form fields
  const [formActionType, setFormActionType] = useState('custom_html');
  const [formActionValue, setFormActionValue] = useState('');
  const [formContent, setFormContent] = useState('');
  const [formCustomHeaders, setFormCustomHeaders] = useState('');
  const [formCustomFooter, setFormCustomFooter] = useState('');
  const [formSeoNoindex, setFormSeoNoindex] = useState(false);
  const [formSeoNofollow, setFormSeoNofollow] = useState(false);
  const [formSeoCanonical, setFormSeoCanonical] = useState('');
  const [formTemplate, setFormTemplate] = useState('');
  const [formEnabled, setFormEnabled] = useState(true);
  const [formLanguage, setFormLanguage] = useState('en');

  // File browser
  const [fileBrowserOpen, setFileBrowserOpen] = useState(false);
  const [fileBrowserPath, setFileBrowserPath] = useState('/');
  const [fileBrowserEntries, setFileBrowserEntries] = useState<FileEntry[]>([]);
  const [fileBrowserLoading, setFileBrowserLoading] = useState(false);

  // Preview modal
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewContent, setPreviewContent] = useState('');

  // Import/Export
  const [importExportOpen, setImportExportOpen] = useState(false);
  const [importExportMode, setImportExportMode] = useState<'import' | 'export'>('export');
  const [importData, setImportData] = useState('');
  const [importing, setImporting] = useState(false);

  // Tabs for the config panel sections
  const [configTab, setConfigTab] = useState<'content' | 'seo' | 'headers' | 'stats'>('content');

  // Track if component is mounted for safe state updates
  const mountedRef = useRef(true);
  useEffect(() => { return () => { mountedRef.current = false; }; }, []);

  const loadData = useCallback(async () => {
    setDomainLoading(true);
    try {
      const d = await getDomains();
      if (mountedRef.current) setDomains(Array.isArray(d) ? d : []);
    } catch (err) {
      console.error('Failed to load domains:', err);
    }
    if (mountedRef.current) setDomainLoading(false);
  }, []);

  useEffect(() => { loadData(); }, [loadData]);

  const loadDomainData = useCallback(async (domainId: number) => {
    if (!domainId) return;
    setPageLoading(true);
    try {
      const [p, s] = await Promise.all([
        getErrorPagesByDomain(domainId),
        getErrorPageStats(domainId),
      ]);
      if (mountedRef.current) {
        setPages(Array.isArray(p) ? p : []);
        setStats(s || null);
      }
    } catch (err) {
      console.error('Failed to load domain error pages:', err);
      if (mountedRef.current) toast('error', getErrorMessage(err));
    }
    if (mountedRef.current) setPageLoading(false);
  }, [toast]);

  useEffect(() => {
    if (selectedDomainId > 0) loadDomainData(selectedDomainId);
    else { setPages([]); setStats(null); }
  }, [selectedDomainId, loadDomainData]);

  const getPageForCode = (code: number): ErrorPageRecord | undefined =>
    pages.find(p => p.error_code === code);

  const handleToggle = async (id: number, current: boolean) => {
    setActionLoading(prev => new Set(prev).add(id));
    try {
      await toggleErrorPage(id, !current);
      setPages(prev => prev.map(p => p.id === id ? { ...p, enabled: !current } : p));
      toast('success', `${!current ? 'Enabled' : 'Disabled'} error page`);
    } catch (err) {
      console.error('Toggle failed:', err);
      toast('error', getErrorMessage(err));
    }
    setActionLoading(prev => { const next = new Set(prev); next.delete(id); return next; });
  };

  const openConfig = async (code: number) => {
    const existing = getPageForCode(code);
    setConfigDomainId(selectedDomainId);
    setConfigErrorCode(code);

    if (existing) {
      try {
        const full = await getErrorPageContent(existing.id);
        setConfigRecord(full);
        setFormActionType(full.action_type || 'custom_html');
        setFormActionValue(full.action_value || '');
        setFormContent(full.content || '');
        setFormCustomHeaders(full.custom_headers || '');
        setFormCustomFooter(full.custom_footer || '');
        setFormSeoNoindex(full.seo_noindex || false);
        setFormSeoNofollow(full.seo_nofollow || false);
        setFormSeoCanonical(full.seo_canonical || '');
        setFormTemplate(full.template || '');
        setFormEnabled(full.enabled);
        setFormLanguage(full.language || 'en');
      } catch (err) {
        console.error('Failed to load error page content:', err);
        setDefaults(code); setConfigRecord(null);
      }
    } else {
      setDefaults(code);
      setConfigRecord(null);
    }
    setConfigTab('content');
    setConfigOpen(true);
  };

  const setDefaults = (code: number) => {
    const errDef = ERROR_CODES.find(e => e.code === code);
    setConfigRecord(null);
    setFormActionType('custom_html');
    setFormActionValue('');
    setFormContent(defaultContent.replace('{{ERROR_CODE}}', String(code)).replace('{{ERROR_NAME}}', errDef?.name || 'Error'));
    setFormCustomHeaders('');
    setFormCustomFooter('');
    setFormSeoNoindex(false);
    setFormSeoNofollow(false);
    setFormSeoCanonical('');
    setFormTemplate('');
    setFormEnabled(true);
    setFormLanguage('en');
  };

  const handleSaveConfig = async () => {
    setConfigSaving(true);
    try {
      const body: any = {
        domain_id: configDomainId,
        error_code: configErrorCode,
        action_type: formActionType,
        action_value: formActionValue,
        content: formContent,
        custom_headers: formCustomHeaders,
        custom_footer: formCustomFooter,
        seo_noindex: formSeoNoindex,
        seo_nofollow: formSeoNofollow,
        seo_canonical: formSeoCanonical,
        template: formTemplate,
        enabled: formEnabled,
        language: formLanguage,
      };
      if (configRecord?.id) body.id = configRecord.id;
      await saveErrorPage(body);
      toast('success', 'Error page saved');
      setConfigOpen(false);
      if (selectedDomainId > 0) loadDomainData(selectedDomainId);
    } catch (e: any) {
      console.error('Save failed:', e);
      toast('error', getErrorMessage(e));
    }
    setConfigSaving(false);
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteErrorPage(id);
      toast('success', 'Error page deleted');
      if (selectedDomainId > 0) loadDomainData(selectedDomainId);
    } catch (err) {
      console.error('Delete failed:', err);
      toast('error', getErrorMessage(err));
    }
  };

  const handleReset = async (id: number) => {
    try {
      await resetErrorPage(id);
      toast('success', 'Error page reset to default');
      if (selectedDomainId > 0) loadDomainData(selectedDomainId);
      if (configOpen) setConfigOpen(false);
    } catch (err) {
      console.error('Reset failed:', err);
      toast('error', getErrorMessage(err));
    }
  };

  const handleTest = async (id: number) => {
    try {
      const res = await testErrorPage(id);
      if (res.action_type === 'internal_redirect' || res.action_type === 'external_redirect') {
        toast('info', `Redirects to: ${res.content}`);
      } else {
        setPreviewContent(res.content);
        setPreviewOpen(true);
      }
    } catch (err) {
      console.error('Test failed:', err);
      toast('error', getErrorMessage(err));
    }
  };

  const handlePreview = () => {
    setPreviewContent(formContent);
    setPreviewOpen(true);
  };

  const openFileBrowser = async (path: string = '/') => {
    setFileBrowserPath(path);
    setFileBrowserLoading(true);
    setFileBrowserOpen(true);
    try {
      const list = await getFileList(path);
      setFileBrowserEntries(Array.isArray(list) ? list.filter((e: FileEntry) =>
        e.is_dir || e.name.endsWith('.html') || e.name.endsWith('.htm')
      ) : []);
    } catch (err) { console.error('File browser failed:', err); setFileBrowserEntries([]); }
    setFileBrowserLoading(false);
  };

  const selectFile = (entry: FileEntry) => {
    if (entry.is_dir) { openFileBrowser(entry.path); return; }
    setFormActionValue(entry.path);
    setFileBrowserOpen(false);
  };

  const handleExport = async () => {
    if (!selectedDomainId) { toast('error', 'Select a domain first'); return; }
    try {
      const data = await exportErrorPages(selectedDomainId);
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `error-pages-${domains.find(d => d.id === selectedDomainId)?.domain || 'export'}.json`;
      a.click();
      URL.revokeObjectURL(url);
      toast('success', 'Error pages exported');
    } catch (err) {
      console.error('Export failed:', err);
      toast('error', getErrorMessage(err));
    }
  };

  const handleImport = async () => {
    if (!selectedDomainId || !importData.trim()) { toast('error', 'Select domain and provide data'); return; }
    setImporting(true);
    try {
      const parsed = JSON.parse(importData);
      const pagesArray = parsed.pages || parsed;
      const res = await importErrorPages({ domain_id: selectedDomainId, pages: Array.isArray(pagesArray) ? pagesArray : [pagesArray] });
      toast('success', `Imported ${res.imported} of ${res.total} error pages`);
      setImportExportOpen(false);
      setImportData('');
      if (selectedDomainId > 0) loadDomainData(selectedDomainId);
    } catch (e: any) {
      console.error('Import failed:', e);
      toast('error', getErrorMessage(e));
    }
    setImporting(false);
  };

  const configuredCount = pages.filter(p => p.enabled).length;
  const totalHits = pages.reduce((sum, p) => sum + p.hit_count, 0);
  const newestTrigger = pages.reduce((latest, p) =>
    p.last_triggered_at && p.last_triggered_at > latest ? p.last_triggered_at : latest, '');

  const domain = domains.find(d => d.id === selectedDomainId);

  const displayCodes = filterMode === 'configured'
    ? ERROR_CODES.filter(ec => pages.some(p => p.error_code === ec.code))
    : ERROR_CODES;

  // Group error codes by severity for visual hierarchy
  const errorGroup = (c: number) => c >= 500 ? '5xx' : c >= 400 ? '4xx' : 'other';
  const groupedCodes: Record<string, typeof ERROR_CODES> = {};
  for (const ec of displayCodes) {
    const g = errorGroup(ec.code);
    if (!groupedCodes[g]) groupedCodes[g] = [];
    groupedCodes[g].push(ec);
  }

  return (
    <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">Error Code Handling</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Configure custom HTTP error pages per domain
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" size="sm" onClick={handleExport}>
            <Download className="h-4 w-4" /> Export
          </Button>
          <Button variant="secondary" size="sm" onClick={() => { setImportExportMode('import'); setImportExportOpen(true); }}>
            <Upload className="h-4 w-4" /> Import
          </Button>
          <Button
            variant={showLogs ? 'primary' : 'secondary'}
            size="sm"
            onClick={() => setShowLogs(!showLogs)}
          >
            <Bug className="h-4 w-4" /> {showLogs ? 'Error Config' : 'Error Logs'}
          </Button>
        </div>
      </div>

      {/* Domain Selector + Stats */}
      <Card>
        <div className="flex flex-col sm:flex-row gap-4">
          <div className="flex-1">
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Domain</label>
            <select
              value={selectedDomainId}
              onChange={e => setSelectedDomainId(Number(e.target.value))}
              className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 transition-shadow"
            >
              <option value={0}>Select a domain...</option>
              {domains.map(d => (
                <option key={d.id} value={d.id} className="font-mono">{d.domain}</option>
              ))}
            </select>
          </div>
          <div className="flex-1">
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Filter</label>
            <div className="flex gap-1.5">
              <button
                onClick={() => setFilterMode('all')}
                className={`flex-1 px-3 py-2.5 rounded-xl text-sm font-medium transition-all ${
                  filterMode === 'all'
                    ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 ring-1 ring-blue-200 dark:ring-blue-800'
                    : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
                }`}
              >All Codes <span className="text-xs ml-1 opacity-60">({ERROR_CODES.length})</span></button>
              <button
                onClick={() => setFilterMode('configured')}
                className={`flex-1 px-3 py-2.5 rounded-xl text-sm font-medium transition-all ${
                  filterMode === 'configured'
                    ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 ring-1 ring-blue-200 dark:ring-blue-800'
                    : 'bg-gray-50 dark:bg-gray-800 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
                }`}
              >Configured <span className="text-xs ml-1 opacity-60">({configuredCount})</span></button>
            </div>
          </div>
        </div>

        {selectedDomainId > 0 && (
          <div className="mt-4 pt-4 border-t border-gray-100 dark:border-gray-700">
            <div className="grid grid-cols-3 gap-4">
              <div className="text-center">
                <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{configuredCount}/{ERROR_CODES.length}</p>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">Enabled</p>
              </div>
              <div className="text-center border-x border-gray-100 dark:border-gray-700">
                <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{formatNum(totalHits)}</p>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">Total Hits</p>
              </div>
              <div className="text-center">
                <p className="text-2xl font-bold text-gray-900 dark:text-gray-100">{timeAgo(newestTrigger)}</p>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">Last Triggered</p>
              </div>
            </div>
          </div>
        )}
      </Card>

      {/* Error Configuration Grid */}
      {!showLogs ? (
        selectedDomainId === 0 ? (
          <Card>
            <EmptyState
              icon={<Globe className="h-12 w-12 text-gray-300" />}
              title="Select a domain to begin"
              message="Choose a domain above to view and configure its HTTP error pages"
            />
          </Card>
        ) : domainLoading || pageLoading ? (
          <Card><Skeleton lines={6} /></Card>
        ) : displayCodes.length === 0 ? (
          <Card>
            <EmptyState
              icon={<Settings className="h-12 w-12 text-gray-300" />}
              title="No error pages configured"
              message="Configure some error codes to see them here. Switch to 'All Codes' view to see all available error codes."
            />
          </Card>
        ) : (
          <div className="space-y-8">
            {Object.entries(groupedCodes).map(([groupName, codes]) => (
              <div key={groupName}>
                <div className="flex items-center gap-3 mb-4">
                  <div className={`h-2 w-2 rounded-full ${groupName === '5xx' ? 'bg-red-500' : 'bg-amber-500'}`} />
                  <span className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-widest">
                    {groupName === '5xx' ? 'Server Errors (5xx)' : 'Client Errors (4xx)'}
                  </span>
                  <div className="flex-1 border-t border-gray-100 dark:border-gray-700/50" />
                </div>
                <div className="space-y-3">
                  {codes.map((ec, idx) => {
                    const page = getPageForCode(ec.code);
                    const ActionIcon = page ? (ACTION_ICONS[page.action_type] || Code) : Code;

                    return (
                      <motion.button
                        key={ec.code}
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: idx * 0.04 }}
                        onClick={() => openConfig(ec.code)}
                        className={`w-full text-left rounded-xl border-2 transition-all duration-200 hover:shadow-lg ${
                          page?.enabled
                            ? 'border-blue-200 dark:border-blue-700 bg-white dark:bg-gray-800 hover:border-blue-300 dark:hover:border-blue-600'
                            : page && !page.enabled
                            ? 'border-gray-200 dark:border-gray-700 bg-gray-50/80 dark:bg-gray-800/40 opacity-80 hover:opacity-100'
                            : 'border-dashed border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800/20 hover:border-gray-300 dark:hover:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-800/40'
                        }`}
                      >
                        <div className="flex items-center gap-5 px-6 py-5">
                          {/* Error code badge */}
                          <div className={`shrink-0 w-14 h-14 rounded-2xl flex items-center justify-center text-xl font-extrabold tracking-tight ${
                            ec.severity === 'error'
                              ? 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400 ring-1 ring-red-200 dark:ring-red-800'
                              : 'bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400 ring-1 ring-amber-200 dark:ring-amber-800'
                          }`}>
                            {ec.code}
                          </div>

                          {/* Info section */}
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-3">
                              <h3 className="text-base font-semibold text-gray-900 dark:text-gray-100">{ec.name}</h3>
                              {page ? (
                                <span className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium ${
                                  page.enabled
                                    ? 'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-400 ring-1 ring-emerald-200 dark:ring-emerald-800'
                                    : 'bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400 ring-1 ring-gray-200 dark:ring-gray-600'
                                }`}>
                                  <span className={`w-1.5 h-1.5 rounded-full ${
                                    page.enabled ? 'bg-emerald-500' : 'bg-gray-400'
                                  }`} />
                                  {page.enabled ? 'Active' : 'Disabled'}
                                </span>
                              ) : (
                                <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium text-gray-400 dark:text-gray-500 bg-gray-50 dark:bg-gray-800 ring-1 ring-gray-200 dark:ring-gray-700">
                                  Not configured
                                </span>
                              )}
                            </div>

                            {page ? (
                              <div className="flex items-center gap-4 mt-2">
                                <div className="flex items-center gap-1.5 text-sm text-gray-500 dark:text-gray-400">
                                  <ActionIcon className="h-3.5 w-3.5" />
                                  <span>{ACTION_LABELS[page.action_type] || 'Custom HTML'}</span>
                                  {page.action_value && (
                                    <>
                                      <ArrowRight className="h-3 w-3 text-gray-300" />
                                      <span className="text-xs font-mono text-gray-400 truncate max-w-[200px]">{page.action_value}</span>
                                    </>
                                  )}
                                </div>
                              </div>
                            ) : (
                              <p className="text-sm text-gray-400 dark:text-gray-500 mt-1.5">
                                Click to configure a custom error page for this status code
                              </p>
                            )}
                          </div>

                          {/* Stats & Toggle */}
                          <div className="flex items-center gap-6 shrink-0">
                            <div className="flex flex-col items-end gap-1">
                              <div className="flex items-center gap-3 text-xs text-gray-400 dark:text-gray-500">
                                <span className="flex items-center gap-1.5">
                                  <BarChart3 className="h-3.5 w-3.5" />
                                  <span className="font-medium text-gray-500 dark:text-gray-400">{formatNum(page?.hit_count || 0)}</span>
                                  hits
                                </span>
                                <span className="flex items-center gap-1.5">
                                  <Clock className="h-3.5 w-3.5" />
                                  <span className="font-medium text-gray-500 dark:text-gray-400">{timeAgo(page?.last_triggered_at || '')}</span>
                                </span>
                              </div>
                            </div>

                            <div className="flex items-center gap-2 pl-4 border-l border-gray-200 dark:border-gray-700">
                              <button
                                onClick={e => { e.stopPropagation(); if (page) handleToggle(page.id, page.enabled); }}
                                className={`p-2 rounded-xl transition-all ${
                                  page?.enabled
                                    ? 'text-blue-500 bg-blue-50 dark:bg-blue-900/30 hover:bg-blue-100 dark:hover:bg-blue-900/50'
                                    : 'text-gray-300 dark:text-gray-600 hover:text-gray-400 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700'
                                }`}
                                title={page?.enabled ? 'Disable' : 'Enable'}
                              >
                                {actionLoading.has(page?.id ?? -1) ? (
                                  <Loader2 className="h-5 w-5 animate-spin" />
                                ) : page?.enabled ? (
                                  <ToggleRight className="h-5 w-5" />
                                ) : (
                                  <ToggleLeft className="h-5 w-5" />
                                )}
                              </button>
                              <ChevronRight className="h-4 w-4 text-gray-300 dark:text-gray-600" />
                            </div>
                          </div>
                        </div>
                      </motion.button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        )
      ) : (
        /* Legacy Error Logs */
        <ErrorLogsView />
      )}

      {/* Configuration Slide Panel */}
      <AnimatePresence>
        {configOpen && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 z-40 bg-black/30"
              onClick={() => setConfigOpen(false)}
            />
            <motion.div
              initial={{ x: '100%' }}
              animate={{ x: 0 }}
              exit={{ x: '100%' }}
              transition={{ type: 'tween', duration: 0.25, ease: 'easeInOut' }}
              className="fixed right-0 top-0 bottom-0 z-50 w-full max-w-2xl bg-white dark:bg-gray-800 shadow-2xl border-l border-gray-200 dark:border-gray-700 flex flex-col"
            >
              {/* Panel Header */}
              <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700 shrink-0">
                <div className="flex items-center gap-3">
                  <div className={`w-10 h-10 rounded-xl flex items-center justify-center text-lg font-bold ${
                    configErrorCode >= 500
                      ? 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-400'
                      : 'bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400'
                  }`}>
                    {configErrorCode}
                  </div>
                  <div>
                    <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                      {ERROR_CODES.find(e => e.code === configErrorCode)?.name || 'Unknown'}
                    </h2>
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      {domain?.domain} &middot; HTTP {configErrorCode}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-1">
                  {configRecord?.id && (
                    <>
                      <button
                        onClick={() => handleTest(configRecord!.id)}
                        className="p-2 text-gray-400 hover:text-blue-600 hover:bg-blue-50 dark:hover:bg-blue-900/30 rounded-lg transition-colors"
                        title="Test"
                      >
                        <Play className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => handleReset(configRecord!.id)}
                        className="p-2 text-gray-400 hover:text-amber-600 hover:bg-amber-50 dark:hover:bg-amber-900/30 rounded-lg transition-colors"
                        title="Reset to Default"
                      >
                        <RotateCcw className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(configRecord!.id)}
                        className="p-2 text-gray-400 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/30 rounded-lg transition-colors"
                        title="Delete"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </>
                  )}
                  <button
                    onClick={() => setConfigOpen(false)}
                    className="p-2 text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors"
                  >
                    <X className="h-5 w-5" />
                  </button>
                </div>
              </div>

              {/* Panel Body */}
              <div className="flex-1 overflow-y-auto">
                {/* Enable toggle strip */}
                <div className="flex items-center justify-between px-6 py-3 bg-gray-50 dark:bg-gray-700/50 border-b border-gray-100 dark:border-gray-700">
                  <div className="flex items-center gap-2">
                    {formEnabled ? (
                      <ToggleRight className="h-5 w-5 text-emerald-500" />
                    ) : (
                      <ToggleLeft className="h-5 w-5 text-gray-400" />
                    )}
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {formEnabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                  <button
                    onClick={() => setFormEnabled(!formEnabled)}
                    className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                      formEnabled ? 'bg-blue-500' : 'bg-gray-300 dark:bg-gray-600'
                    }`}
                  >
                    <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                      formEnabled ? 'translate-x-6' : 'translate-x-1'
                    }`} />
                  </button>
                </div>

                {/* Config Tabs */}
                <div className="flex border-b border-gray-200 dark:border-gray-700 px-6">
                  {[
                    { id: 'content', label: 'Content', icon: Code },
                    { id: 'seo', label: 'SEO', icon: Search },
                    { id: 'headers', label: 'Headers & Footer', icon: FileText },
                    { id: 'stats', label: 'Statistics', icon: BarChart3 },
                  ].map(tab => {
                    const Icon = tab.icon;
                    return (
                      <button
                        key={tab.id}
                        onClick={() => setConfigTab(tab.id as any)}
                        className={`flex items-center gap-1.5 px-4 py-3 text-sm font-medium border-b-2 transition-colors ${
                          configTab === tab.id
                            ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                            : 'border-transparent text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
                        }`}
                      >
                        <Icon className="h-4 w-4" />
                        {tab.label}
                      </button>
                    );
                  })}
                </div>

                <div className="p-6 space-y-5">
                  {/* Content Tab */}
                  {configTab === 'content' && (
                    <>
                      <div>
                        <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-2">
                          Action Type
                        </label>
                        <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                          {[
                            { value: 'custom_html', label: 'Custom HTML', icon: Code, desc: 'Write custom HTML content' },
                            { value: 'file_manager', label: 'File Manager', icon: FolderOpen, desc: 'Select an existing HTML file' },
                            { value: 'internal_redirect', label: 'Internal Redirect', icon: ArrowRight, desc: 'Redirect to a path' },
                            { value: 'external_redirect', label: 'External URL', icon: ExternalLink, desc: 'Redirect to an external URL' },
                            { value: 'template', label: 'Template', icon: Sparkles, desc: 'Use a predefined template' },
                          ].map(action => {
                            const Icon = action.icon;
                            const selected = formActionType === action.value;
                            return (
                              <button
                                key={action.value}
                                onClick={() => setFormActionType(action.value)}
                                className={`text-left p-3 rounded-xl border transition-all ${
                                  selected
                                    ? 'border-blue-300 dark:border-blue-600 bg-blue-50 dark:bg-blue-900/20 ring-1 ring-blue-200 dark:ring-blue-800'
                                    : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600 bg-white dark:bg-gray-800'
                                }`}
                              >
                                <Icon className={`h-5 w-5 mb-1.5 ${selected ? 'text-blue-500' : 'text-gray-400'}`} />
                                <p className="text-sm font-medium text-gray-900 dark:text-gray-100">{action.label}</p>
                                <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">{action.desc}</p>
                              </button>
                            );
                          })}
                        </div>
                      </div>

                      <div>
                        <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-2">
                          {formActionType === 'custom_html' && 'HTML Content'}
                          {formActionType === 'file_manager' && 'File Path'}
                          {formActionType === 'internal_redirect' && 'Internal Path'}
                          {formActionType === 'external_redirect' && 'External URL'}
                          {formActionType === 'template' && 'Template Content'}
                        </label>
                        {formActionType === 'custom_html' || formActionType === 'template' ? (
                          <div className="relative">
                            <textarea
                              value={formContent}
                              onChange={e => setFormContent(e.target.value)}
                              rows={14}
                              className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y leading-relaxed"
                              placeholder="<html>...</html>"
                            />
                            <div className="absolute right-2 bottom-2 flex gap-1">
                              <button
                                onClick={handlePreview}
                                className="p-1.5 bg-white dark:bg-gray-600 border border-gray-200 dark:border-gray-500 rounded-lg text-gray-400 hover:text-blue-500 transition-colors"
                                title="Preview"
                              >
                                <Eye className="h-3.5 w-3.5" />
                              </button>
                            </div>
                          </div>
                        ) : formActionType === 'file_manager' ? (
                          <div className="flex gap-2">
                            <input
                              value={formActionValue}
                              onChange={e => setFormActionValue(e.target.value)}
                              className="flex-1 px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                              placeholder="/path/to/error.html"
                            />
                            <Button variant="secondary" onClick={() => openFileBrowser('/')}>
                              <FolderOpen className="h-4 w-4" /> Browse
                            </Button>
                          </div>
                        ) : (
                          <input
                            value={formActionValue}
                            onChange={e => setFormActionValue(e.target.value)}
                            className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                            placeholder={formActionType === 'external_redirect' ? 'https://example.com/error-page' : '/custom-error-page'}
                          />
                        )}
                      </div>

                      <div className="flex items-center gap-2 p-3 bg-gray-50 dark:bg-gray-700/50 rounded-xl">
                        <Info className="h-4 w-4 text-blue-500 shrink-0" />
                        <p className="text-xs text-gray-500 dark:text-gray-400">
                          {formActionType === 'custom_html' && 'Write custom HTML that will be served as the error page. Use {{ERROR_CODE}} and {{ERROR_NAME}} as placeholders.'}
                          {formActionType === 'file_manager' && 'Select an existing HTML file from your file manager. The file content will be served as the error page.'}
                          {formActionType === 'internal_redirect' && 'Enter a path relative to your domain root. Users will be redirected to this path when this error occurs.'}
                          {formActionType === 'external_redirect' && 'Enter a full URL to redirect users to when this error occurs.'}
                          {formActionType === 'template' && 'Use predefined templates to quickly create error pages with a professional look.'}
                        </p>
                      </div>
                    </>
                  )}

                  {/* SEO Tab */}
                  {configTab === 'seo' && (
                    <>
                      <div className="space-y-4">
                        <div className="flex items-center justify-between p-4 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl">
                          <div>
                            <p className="text-sm font-medium text-gray-900 dark:text-gray-100">No Index</p>
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                              Add <code className="text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">X-Robots-Tag: noindex</code> header
                            </p>
                          </div>
                          <button
                            onClick={() => setFormSeoNoindex(!formSeoNoindex)}
                            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                              formSeoNoindex ? 'bg-amber-500' : 'bg-gray-300 dark:bg-gray-600'
                            }`}
                          >
                            <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                              formSeoNoindex ? 'translate-x-6' : 'translate-x-1'
                            }`} />
                          </button>
                        </div>

                        <div className="flex items-center justify-between p-4 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl">
                          <div>
                            <p className="text-sm font-medium text-gray-900 dark:text-gray-100">No Follow</p>
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                              Add <code className="text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">X-Robots-Tag: nofollow</code> header
                            </p>
                          </div>
                          <button
                            onClick={() => setFormSeoNofollow(!formSeoNofollow)}
                            className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                              formSeoNofollow ? 'bg-amber-500' : 'bg-gray-300 dark:bg-gray-600'
                            }`}
                          >
                            <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                              formSeoNofollow ? 'translate-x-6' : 'translate-x-1'
                            }`} />
                          </button>
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">
                            Canonical URL
                          </label>
                          <input
                            value={formSeoCanonical}
                            onChange={e => setFormSeoCanonical(e.target.value)}
                            placeholder="https://example.com/error-page"
                            className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                          />
                          <p className="text-xs text-gray-400 mt-1">
                            Helps search engines understand which URL should be indexed when this error page is served
                          </p>
                        </div>

                        <div className="p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-xl">
                          <div className="flex items-start gap-2">
                            <Search className="h-4 w-4 text-blue-500 mt-0.5" />
                            <div>
                              <p className="text-sm font-medium text-blue-800 dark:text-blue-300">SEO Recommendation</p>
                              <p className="text-xs text-blue-600 dark:text-blue-400 mt-1">
                                For 404 and 410 errors, enabling "No Index" is recommended to prevent
                                search engines from indexing these error pages. For soft 404s, consider
                                adding a canonical URL pointing to the original resource.
                              </p>
                            </div>
                          </div>
                        </div>
                      </div>
                    </>
                  )}

                  {/* Headers & Footer Tab */}
                  {configTab === 'headers' && (
                    <div className="space-y-4">
                      <div>
                        <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">
                          Custom Headers <span className="text-gray-300 font-normal">(HTML/JS injected before &lt;/head&gt;)</span>
                        </label>
                        <textarea
                          value={formCustomHeaders}
                          onChange={e => setFormCustomHeaders(e.target.value)}
                          rows={5}
                          placeholder="<!-- Google Analytics, custom CSS, etc. -->"
                          className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
                        />
                      </div>
                      <div>
                        <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">
                          Custom Footer <span className="text-gray-300 font-normal">(HTML/JS injected before &lt;/body&gt;)</span>
                        </label>
                        <textarea
                          value={formCustomFooter}
                          onChange={e => setFormCustomFooter(e.target.value)}
                          rows={5}
                          placeholder="<!-- Custom footer content -->"
                          className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
                        />
                      </div>
                      <div>
                        <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">
                          Language
                        </label>
                        <select
                          value={formLanguage}
                          onChange={e => setFormLanguage(e.target.value)}
                          className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                        >
                          <option value="en">English</option>
                          <option value="es">Spanish</option>
                          <option value="fr">French</option>
                          <option value="de">German</option>
                          <option value="it">Italian</option>
                          <option value="pt">Portuguese</option>
                          <option value="ru">Russian</option>
                          <option value="zh">Chinese</option>
                          <option value="ja">Japanese</option>
                          <option value="ar">Arabic</option>
                        </select>
                        <p className="text-xs text-gray-400 mt-1">
                          Language setting is for future multi-language support. Error pages can include language-specific content.
                        </p>
                      </div>
                    </div>
                  )}

                  {/* Statistics Tab */}
                  {configTab === 'stats' && (
                    <div className="space-y-4">
                      <div className="grid grid-cols-2 gap-4">
                        <div className="p-5 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl text-center">
                          <BarChart3 className="h-6 w-6 mx-auto mb-2 text-blue-500" />
                          <p className="text-3xl font-bold text-gray-900 dark:text-gray-100">
                            {formatNum(configRecord?.hit_count || 0)}
                          </p>
                          <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Total Hits</p>
                        </div>
                        <div className="p-5 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl text-center">
                          <Clock className="h-6 w-6 mx-auto mb-2 text-amber-500" />
                          <p className="text-lg font-bold text-gray-900 dark:text-gray-100 break-all">
                            {timeAgo(configRecord?.last_triggered_at || '')}
                          </p>
                          <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Last Triggered</p>
                        </div>
                      </div>
                      <div className="p-5 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl">
                        <div className="flex items-center justify-between">
                          <div>
                            <p className="text-sm font-medium text-gray-900 dark:text-gray-100">HTTP {configErrorCode} - {ERROR_CODES.find(e => e.code === configErrorCode)?.name}</p>
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              Domain: {domain?.domain || 'Unknown'}
                            </p>
                          </div>
                          <Badge variant={configRecord?.enabled ? 'success' : 'neutral'}>
                            {configRecord?.enabled ? 'Active' : 'Disabled'}
                          </Badge>
                        </div>
                      </div>
                      <div className="p-4 bg-gray-50 dark:bg-gray-700/50 rounded-xl">
                        <div className="flex items-start gap-2">
                          <Info className="h-4 w-4 text-blue-500 mt-0.5 shrink-0" />
                          <div>
                            <p className="text-xs font-medium text-gray-700 dark:text-gray-300">Error Hit Tracking</p>
                            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                              Hits are automatically recorded when the web server serves this error page.
                              The counter tracks how many times visitors have encountered this error.
                              Last triggered timestamp is updated with each hit.
                            </p>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              </div>

              {/* Panel Footer */}
              <div className="px-6 py-4 border-t border-gray-200 dark:border-gray-700 shrink-0 flex items-center justify-between gap-3 bg-gray-50 dark:bg-gray-800/50">
                <div className="flex items-center gap-2">
                  {configRecord?.id && (
                    <Button variant="secondary" size="sm" onClick={() => handleTest(configRecord.id)}>
                      <Play className="h-4 w-4" /> Test
                    </Button>
                  )}
                  <Button variant="secondary" size="sm" onClick={handlePreview}>
                    <Eye className="h-4 w-4" /> Preview
                  </Button>
                </div>
                <div className="flex items-center gap-2">
                  <Button variant="secondary" size="sm" onClick={() => setConfigOpen(false)}>
                    Cancel
                  </Button>
                  <Button variant="primary" size="sm" onClick={handleSaveConfig} loading={configSaving}>
                    <Save className="h-4 w-4" /> Save Changes
                  </Button>
                </div>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>

      {/* Preview Modal */}
      <AnimatePresence>
        {previewOpen && (
          <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-black/40">
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              className="bg-white dark:bg-gray-800 rounded-2xl w-full max-w-4xl max-h-[85vh] overflow-hidden shadow-2xl border border-gray-200 dark:border-gray-700"
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                <h3 className="font-semibold text-gray-900 dark:text-gray-100">Error Page Preview</h3>
                <button onClick={() => setPreviewOpen(false)} className="p-1.5 text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors">
                  <X className="h-5 w-5" />
                </button>
              </div>
              <div className="overflow-auto p-6 bg-gray-100 dark:bg-gray-900">
                <div className="bg-white dark:bg-gray-800 rounded-xl shadow-sm border border-gray-200 dark:border-gray-700 overflow-hidden">
                  <div className="flex items-center gap-1.5 px-4 py-2 bg-gray-50 dark:bg-gray-700 border-b border-gray-200 dark:border-gray-600">
                    <div className="w-3 h-3 rounded-full bg-red-400" />
                    <div className="w-3 h-3 rounded-full bg-amber-400" />
                    <div className="w-3 h-3 rounded-full bg-emerald-400" />
                    <span className="text-xs text-gray-400 ml-2 font-mono">Preview</span>
                  </div>
                  <iframe
                    srcDoc={previewContent}
                    className="w-full h-[60vh] bg-white"
                    title="Error page preview"
                    sandbox="allow-scripts"
                  />
                </div>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      {/* File Browser Modal */}
      <AnimatePresence>
        {fileBrowserOpen && (
          <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-black/40">
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              className="bg-white dark:bg-gray-800 rounded-2xl w-full max-w-lg max-h-[70vh] overflow-hidden shadow-2xl border border-gray-200 dark:border-gray-700"
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
                <div className="flex items-center gap-2">
                  <FolderOpen className="h-5 w-5 text-blue-500" />
                  <h3 className="font-semibold text-gray-900 dark:text-gray-100">Select HTML File</h3>
                </div>
                <button onClick={() => setFileBrowserOpen(false)} className="p-1.5 text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors">
                  <X className="h-5 w-5" />
                </button>
              </div>
              <div className="px-5 py-3 border-b border-gray-100 dark:border-gray-700 text-xs text-gray-500 font-mono">
                {fileBrowserPath}
              </div>
              <div className="overflow-y-auto max-h-[40vh] p-2">
                {fileBrowserLoading ? (
                  <div className="flex justify-center py-8"><Loader2 className="h-6 w-6 animate-spin text-gray-400" /></div>
                ) : fileBrowserEntries.length === 0 ? (
                  <div className="py-8 text-center text-sm text-gray-400">No HTML files found in this directory</div>
                ) : (
                  <div className="space-y-0.5">
                    {fileBrowserPath !== '/' && (
                      <button
                        onClick={() => openFileBrowser(fileBrowserPath.split('/').slice(0, -1).join('/') || '/')}
                        className="w-full flex items-center gap-3 px-3 py-2.5 rounded-xl hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors text-left"
                      >
                        <FolderOpen className="h-4 w-4 text-amber-500" />
                        <span className="text-sm text-gray-600 dark:text-gray-400">..</span>
                      </button>
                    )}
                    {fileBrowserEntries.map(entry => (
                      <button
                        key={entry.path}
                        onClick={() => selectFile(entry)}
                        className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-xl hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors text-left ${
                          !entry.is_dir ? 'hover:text-blue-600 dark:hover:text-blue-400' : ''
                        }`}
                      >
                        {entry.is_dir ? (
                          <FolderOpen className="h-4 w-4 text-amber-500 shrink-0" />
                        ) : (
                          <FileType className="h-4 w-4 text-blue-500 shrink-0" />
                        )}
                        <span className="text-sm text-gray-700 dark:text-gray-300 truncate">{entry.name}</span>
                        {!entry.is_dir && (
                          <span className="text-xs text-gray-400 ml-auto shrink-0">Select</span>
                        )}
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <div className="px-5 py-3 border-t border-gray-200 dark:border-gray-700 flex justify-end">
                <Button variant="secondary" size="sm" onClick={() => setFileBrowserOpen(false)}>Cancel</Button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      {/* Import/Export Modal */}
      <AnimatePresence>
        {importExportOpen && importExportMode === 'import' && (
          <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-black/40">
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              className="bg-white dark:bg-gray-800 rounded-2xl w-full max-w-lg shadow-2xl border border-gray-200 dark:border-gray-700"
              onClick={e => e.stopPropagation()}
            >
              <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
                <h3 className="font-semibold text-gray-900 dark:text-gray-100">Import Error Pages</h3>
                <button onClick={() => { setImportExportOpen(false); setImportData(''); }} className="p-1.5 text-gray-400 hover:text-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors">
                  <X className="h-5 w-5" />
                </button>
              </div>
              <div className="p-5 space-y-4">
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  Paste the JSON export data below to import error page configurations for <strong className="text-gray-900 dark:text-gray-100">{domain?.domain || 'selected domain'}</strong>.
                </p>
                <textarea
                  value={importData}
                  onChange={e => setImportData(e.target.value)}
                  rows={10}
                  placeholder='{"pages": [{"error_code": 404, "content": "...", ...}]}'
                  className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-600 rounded-xl text-sm font-mono bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
                />
              </div>
              <div className="px-5 py-4 border-t border-gray-200 dark:border-gray-700 flex justify-end gap-2">
                <Button variant="secondary" size="sm" onClick={() => { setImportExportOpen(false); setImportData(''); }}>Cancel</Button>
                <Button variant="primary" size="sm" onClick={handleImport} loading={importing} disabled={!importData.trim()}>
                  <Upload className="h-4 w-4" /> Import
                </Button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}

// Error Logs Sub-View
function ErrorLogsView() {
  const [errors, setErrors] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    getRecentErrors().then(e => { setErrors(e || []); }).catch((err) => { console.error('Failed to load error logs:', err); }).finally(() => setLoading(false));
  }, []);

  const levelBadge = (lvl: string) => {
    const map: Record<string, 'error' | 'warning' | 'neutral'> = {
      error: 'error', warn: 'warning', critical: 'error', alert: 'warning', emergency: 'error',
    };
    return <Badge variant={map[lvl] || 'neutral'}>{lvl}</Badge>;
  };

  const levelColor: Record<string, string> = {
    error: 'text-red-600 dark:text-red-400',
    warn: 'text-yellow-600 dark:text-yellow-400',
    critical: 'text-red-700 dark:text-red-300',
    alert: 'text-orange-600 dark:text-orange-400',
    emergency: 'text-red-800 dark:text-red-200',
  };

  if (loading) return <Card><Skeleton lines={4} /></Card>;

  return errors.length === 0 ? (
    <Card><EmptyState icon={<Bug className="h-10 w-10 text-gray-400" />} title="No recent errors" message="Your websites are running without errors." /></Card>
  ) : (
    <Card>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Recent Error Logs</h3>
        <RefreshCw className="h-4 w-4 text-gray-400" />
      </div>
      <div className="space-y-2 max-h-[600px] overflow-y-auto">
        {errors.map((e, i) => (
          <div key={i} className="p-2.5 bg-gray-50 dark:bg-gray-800/50 rounded-lg text-xs font-mono">
            <div className="flex items-center gap-2 mb-1">
              <Globe className="h-3 w-3 text-gray-400" />
              <span className="text-gray-900 dark:text-gray-100 font-semibold">{e.domain}</span>
              {levelBadge(e.level)}
              <span className="text-gray-400 ml-auto">{e.time}</span>
            </div>
            <p className={`${levelColor[e.level] || 'text-gray-600 dark:text-gray-400'} break-all leading-relaxed`}>{e.line}</p>
          </div>
        ))}
      </div>
    </Card>
  );
}

function Play(props: any) { return <svg {...props} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="5 3 19 12 5 21 5 3" /></svg>; }
function Info(props: any) { return <svg {...props} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>; }

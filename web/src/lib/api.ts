export interface Account { id: number; username: string; domain: string; email: string; package_name: string; status: string; home_dir: string; disk_used_mb: number; bandwidth_used_mb: number; created_at: string; }
export interface Package { id: number; name: string; disk_mb: number; bandwidth_mb: number; max_db: number; max_email: number; max_ftp: number; max_domains: number; max_subdomains: number; ssh_access: number; backup_enabled: number; files_enabled?: boolean; emails_enabled?: boolean; ftp_enabled?: boolean; db_enabled?: boolean; cron_enabled?: boolean; is_default: number; }
export type FeatureName = 'files' | 'emails' | 'ftp' | 'db' | 'backups' | 'cron';
export type FeatureMap = Partial<Record<FeatureName, boolean>>;
export interface Domain { id: number; domain: string; type: string; doc_root: string; ssl_enabled: boolean; ssl_status: 'issued' | 'issuing' | 'expired' | 'failed' | 'none'; created_at: string; }
export interface Database { id: number; db_name: string; db_user: string; host: string; size_mb: number; }
export interface DBUser { id: number; username: string; }
export interface EmailAccount { id: number; email: string; forward_to: string; quota_mb: number; status: string; }
export interface CMSInstall { id: number; domain: string; cms_type: string; version: string; install_url: string; admin_url: string; status: string; }
export interface SSLCert { id: number; domain: string; issuer: string; expires_at: string; auto_renew: number; status: string; created_at: string; }
export interface Ticket { id: number; subject: string; status: string; created_at: string; }
export interface Backup {
  id: number;
  domain: string;
  type: 'full' | 'files' | 'database';
  file_size: number;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'restoring';
  notes: string;
  progress: number;
  trigger: 'manual' | 'schedule';
  message: string;
  created_at: string;
}
export interface BackupSchedule {
  id: number;
  name: string;
  backup_type: 'full' | 'files' | 'database';
  frequency: 'daily' | 'weekly' | 'monthly';
  time: string;
  day_of_week: number;
  day_of_month: number;
  retention: number;
  enabled: boolean;
  next_run_at: string;
  last_run_at: string;
  last_status: string;
  created_at: string;
}
export interface BackupSummary {
  total_backups: number;
  total_size: number;
  last_backup_at: string;
  last_status: string;
  schedule_count: number;
  enabled_schedules: number;
}
export interface CronJob { id: number; command: string; schedule: string; enabled: number; }
export const getCronJobs = () => request('GET', '/child/cron');
export const createCronJob = (data: { command: string; schedule: string; description?: string }) =>
  request('POST', '/child/cron', data);
export const updateCronJob = (id: number, data: any) => request('PUT', `/child/cron/${id}`, data);
export const deleteCronJob = (id: number) => request('DELETE', `/child/cron/${id}`);
export const toggleCronJob = (id: number) => request('POST', `/child/cron/${id}/toggle`);
export const getCronPresets = () => request('GET', '/child/cron/presets');
export interface DNSRecord { id: number; domain: string; type: string; name: string; value: string; priority: number; ttl: number; }
export interface FTPAccount { id: number; username: string; domain: string; directory: string; quota_mb: number; status: string; created_at: string; }
export interface SSHKey { id: number; name: string; fingerprint: string; authorized: number; }
export interface DomainStats { total_visitors: number; total_bandwidth_bytes: number; top_pages: Array<{path: string, hits: number}>; recent_hits: Array<{timestamp: string, ip: string, path: string, status: number, bytes: number}>; }

const BASE = '/api/v1';

interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user?: { id: number; username: string; role: string; home_dir?: string };
}

interface ApiErrorBody {
  code?: string;
  message?: string;
  details?: string;
  fields?: Array<{field: string; reason: string}>;
  request_id?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: string;
  readonly fields?: Array<{field: string; reason: string}>;
  readonly requestId?: string;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message || body.code || `HTTP ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.code = body.code || 'UNKNOWN';
    this.details = body.details;
    this.fields = body.fields;
    this.requestId = body.request_id;
  }
}

export class NetworkError extends Error {
  readonly cause?: unknown;

  constructor(msg: string, cause?: unknown) {
    super(msg);
    this.name = 'NetworkError';
    this.cause = cause;
  }
}

export class SessionExpiredError extends Error {
  constructor() {
    super('Session expired');
    this.name = 'SessionExpiredError';
  }
}

let tokenPrefix = 'owp_admin_';
export function setTokenPrefix(prefix: string) { tokenPrefix = prefix; }
let refreshPath = '/auth/refresh';
export function setRefreshPath(path: string) { refreshPath = path; }

function tk(key: string): string { return tokenPrefix + key; }

export function setTokensFor(prefix: string, tokens: TokenPair) {
  localStorage.setItem(prefix + 'access_token', tokens.access_token);
  localStorage.setItem(prefix + 'refresh_token', tokens.refresh_token);
  if (tokens.user) {
    localStorage.setItem(prefix + 'user', JSON.stringify(tokens.user));
  }
}

export function getAccessToken(): string | null {
  return localStorage.getItem(tk('access_token'));
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(tk('refresh_token'));
}

function setTokens(tokens: TokenPair) {
  localStorage.setItem(tk('access_token'), tokens.access_token);
  localStorage.setItem(tk('refresh_token'), tokens.refresh_token);
  if (tokens.user) {
    localStorage.setItem(tk('user'), JSON.stringify(tokens.user));
  }
}

export function clearTokens() {
  localStorage.removeItem(tk('access_token'));
  localStorage.removeItem(tk('refresh_token'));
  localStorage.removeItem(tk('user'));
  // Notify feature-gate caches (FeatureGate.tsx) to drop per-account data so
  // the next login never inherits the previous user's gates.
  if (typeof window !== 'undefined') window.dispatchEvent(new Event('owp:logout'));
}

export function getUser() {
  try {
    const u = localStorage.getItem(tk('user'));
    return u ? JSON.parse(u) : null;
  } catch {
    return null;
  }
}

let refreshPromise: Promise<TokenPair | null> | null = null;

async function doRefresh(): Promise<TokenPair | null> {
  if (refreshPromise) return refreshPromise;
  refreshPromise = (async () => {
    const rt = getRefreshToken();
    if (!rt) return null;
    try {
      const res = await fetch(`${BASE}${refreshPath}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${rt}` },
        body: JSON.stringify({ refresh_token: rt }),
      });
      if (!res.ok) { clearTokens(); return null; }
      const tokens: TokenPair = await res.json();
      setTokens(tokens);
      return tokens;
    } catch {
      clearTokens();
      return null;
    } finally {
      refreshPromise = null;
    }
  })();
  return refreshPromise;
}

async function parseError(res: Response): Promise<ApiError> {
  try {
    const body = await res.json();
    if (body && body.error) {
      // Backend `jsonError` returns {error:"..."} (a string), while the
      // shared `WriteError` middleware returns {error:{code,message,...}}.
      // Handle both so the real message isn't discarded and the UI never
      // shows a generic "HTTP 500".
      if (typeof body.error === 'string') {
        return new ApiError(res.status, { code: `HTTP_${res.status}`, message: body.error });
      }
      return new ApiError(res.status, body.error);
    }
    if (body && body.code) {
      return new ApiError(res.status, body);
    }
    return new ApiError(res.status, { code: `HTTP_${res.status}`, message: body?.message || res.statusText });
  } catch {
    return new ApiError(res.status, { code: `HTTP_${res.status}`, message: res.statusText });
  }
}

async function request(method: string, path: string, body?: any): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getAccessToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;

  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    throw new NetworkError('Network request failed', e);
  }

  if (res.status === 401 && getRefreshToken()) {
    const tokens = await doRefresh();
    if (tokens) {
      headers['Authorization'] = `Bearer ${tokens.access_token}`;
      try {
        res = await fetch(`${BASE}${path}`, { method, headers, body: body ? JSON.stringify(body) : undefined });
      } catch (e) {
        throw new NetworkError('Network request failed on retry', e);
      }
      if (!res.ok) {
        throw await parseError(res);
      }
      return res.json();
    }
    throw new SessionExpiredError();
  }

  if (!res.ok) {
    throw await parseError(res);
  }

  return res.json();
}

export const login = (username: string, password: string) =>
  request('POST', '/auth/login', { username, password }).then((tokens: TokenPair) => {
    setTokens(tokens);
    return tokens;
  });

export const loginChild = (username: string, password: string) =>
  request('POST', '/child/auth/login', { username, password }).then((tokens: TokenPair) => {
    setTokens(tokens);
    return tokens;
  });

export const getMe = () => request('GET', '/auth/me');
export const updateMe = (data: { username?: string; password?: string }) =>
  request('PUT', '/auth/me', data);

export function setStoredUser(patch: Record<string, any>) {
  try {
    const u = getUser();
    if (u) localStorage.setItem(tk('user'), JSON.stringify({ ...u, ...patch }));
  } catch { /* non-fatal */ }
}

export const getPackages = () => request('GET', '/packages');
export const createPackage = (data: any) => request('POST', '/packages', data);
export const updatePackage = (id: number, data: any) => request('PUT', `/packages/${id}`, data);
export const deletePackage = (id: number) => request('DELETE', `/packages/${id}`);

export const getAccounts = (status?: string) =>
  request('GET', `/accounts${status ? `?status=${status}` : ''}`);
export const getAccount = (id: number) => request('GET', `/accounts/${id}`);
export const getAccountResources = (id: number) => request('GET', `/accounts/${id}/resources`);
export const getAccountActivity = (id: number) => request('GET', `/accounts/${id}/activity`);
export const createAccount = (data: any) => request('POST', '/accounts', data);
export const suspendAccount = (id: number, reason?: string) =>
  request('POST', `/accounts/${id}/suspend`, { reason });
export const unsuspendAccount = (id: number) =>
  request('POST', `/accounts/${id}/unsuspend`);
export const terminateAccount = (id: number) => request('DELETE', `/accounts/${id}`);
export const purgeAccount = (id: number) => request('DELETE', `/accounts/${id}/purge`);
export const getAccountUploadLimit = (id: number) => request('GET', `/accounts/${id}/upload-limit`);
export const setAccountUploadLimit = (id: number, limitMB: number) =>
  request('PUT', `/accounts/${id}/upload-limit`, { limit_mb: limitMB });
export const getAccountRamLimit = (id: number) => request('GET', `/accounts/${id}/ram-limit`);
export const setAccountRamLimit = (id: number, limitMB: number) =>
  request('PUT', `/accounts/${id}/ram-limit`, { limit_mb: limitMB });
export const getAccountResourceLimits = (id: number) => request('GET', `/accounts/${id}/resource-limits`);
export const setAccountResourceLimits = (id: number, data: any) =>
  request('PUT', `/accounts/${id}/resource-limits`, data);
export const changeAccountPackage = (id: number, packageId: number) =>
  request('PUT', `/accounts/${id}/package`, { package_id: packageId });
export const updateAccount = (id: number, data: any) => request('PUT', `/accounts/${id}`, data);
export const loginAsChild = (id: number) => request('POST', `/accounts/${id}/login-as`);
export const resetAccountPassword = (id: number, password: string) =>
  request('POST', `/accounts/${id}/reset-password`, { password });

export const getStatsOverview = () => request('GET', '/stats/overview');
export const getServerStatus = () => request('GET', '/server/status');

export const getAdminTokens = () => request('GET', '/api-tokens');
export const createAdminToken = (data: { name: string; expires_in_days?: number }) =>
  request('POST', '/api-tokens', data);
export const toggleAdminToken = (id: number, enabled: boolean) =>
  request('PUT', `/api-tokens/${id}`, { enabled });
export const deleteAdminToken = (id: number) => request('DELETE', `/api-tokens/${id}`);

export const getK8sStatus = () => request('GET', '/nodes/k8s/status');
export const getK8sJoinCommand = () => request('POST', '/nodes/k8s/join-command');
export const getK8sMetricsSummary = () => request('GET', '/nodes/k8s/metrics/summary');
export const drainK8sNode = (name: string) =>
  request('POST', `/nodes/k8s/nodes/${encodeURIComponent(name)}/drain`, {});
export const deleteK8sNode = (name: string) =>
  request('DELETE', `/nodes/k8s/nodes/${encodeURIComponent(name)}`);
export const installWorkerDeps = (name: string) =>
  request('POST', `/nodes/k8s/workers/${encodeURIComponent(name)}/install-deps`, {});
export const getWorkerInstallLog = (name: string, tail = 200) =>
  request('GET', `/nodes/k8s/workers/${encodeURIComponent(name)}/install-log?tail=${tail}`);
export const getWorkerHealth = (name: string) =>
  request('GET', `/nodes/k8s/workers/${encodeURIComponent(name)}/health`);
export const recheckWorkerHealth = (name: string) =>
  request('POST', `/nodes/k8s/workers/${encodeURIComponent(name)}/health/recheck`, {});
export const fixWorkerHealth = (name: string) =>
  request('POST', `/nodes/k8s/workers/${encodeURIComponent(name)}/health/fix`, {});

export const getSettings = () => request('GET', '/settings');
export const updateSettings = (updates: Record<string, string>) =>
  request('PUT', '/settings', updates);
export const getUploadLimit = () => request('GET', '/settings/upload-limit');

export const getFileList = (path: string = '/') =>
  request('GET', `/child/files/list?path=${encodeURIComponent(path)}`);
export const readFile = (path: string) =>
  request('GET', `/child/files/read?path=${encodeURIComponent(path)}`);
export const writeFile = (path: string, content: string) =>
  request('POST', '/child/files/write', { path, content });
export const mkdir = (path: string, name: string) =>
  request('POST', '/child/files/mkdir', { path, name });
export const deleteFile = (path: string) =>
  request('POST', '/child/files/delete', { path });
export const renameFile = (oldPath: string, newPath: string) =>
  request('POST', '/child/files/rename', { old_path: oldPath, new_path: newPath });
export const getDiskUsage = () => request('GET', '/child/files/disk-usage');

export const uploadFile = async (
  path: string,
  file: File,
  onProgress?: (pct: number, phase: 'uploading' | 'processing' | 'done') => void,
) => {
  const form = new FormData();
  form.append('file', file);
  form.append('path', path);
  const token = getAccessToken();
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${BASE}/child/files/upload`);
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`);

    let idleTimer: ReturnType<typeof setTimeout>;
    function resetIdle(ms: number) {
      clearTimeout(idleTimer);
      idleTimer = setTimeout(() => {
        xhr.abort();
        reject(new NetworkError(`Upload timed out — no activity for ${ms / 1000}s`));
      }, ms);
    }

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) {
        const pct = Math.round((e.loaded / e.total) * 100);
        onProgress?.(Math.min(pct, 100), 'uploading');
        resetIdle(30_000);
      }
    };

    xhr.upload.onload = () => {
      onProgress?.(100, 'processing');
      resetIdle(120_000);
    };

    xhr.onload = () => {
      clearTimeout(idleTimer);
      let parsed: any;
      try {
        parsed = JSON.parse(xhr.responseText || '{}');
      } catch {
        reject(new ApiError(0, { code: 'INVALID_RESPONSE', message: 'Invalid server response' }));
        return;
      }
      if (xhr.status === 200) {
        onProgress?.(100, 'done');
        resolve(parsed);
      } else {
        const errBody: ApiErrorBody =
          typeof parsed.error === 'string'
            ? { code: `HTTP_${xhr.status}`, message: parsed.error }
            : (parsed.error as ApiErrorBody);
        reject(new ApiError(xhr.status, errBody));
      }
    };

    xhr.onerror = () => { clearTimeout(idleTimer); reject(new NetworkError('Upload failed — network error')); };
    xhr.onabort = () => { clearTimeout(idleTimer); reject(new NetworkError('Upload cancelled')); };
    xhr.send(form);
  });
};

export const compressFiles = (path: string, files: string[], archiveName: string) =>
  request('POST', '/child/files/compress', { path, files, archive_name: archiveName });
export const extractFile = (archivePath: string, destination?: string) =>
  request('POST', '/child/files/extract', { path: archivePath, destination });
export const moveFiles = (paths: string[], destination: string, overwrite?: boolean) =>
  request('POST', '/child/files/move', { paths, destination, overwrite });
export const copyFiles = (paths: string[], destination: string, overwrite?: boolean) =>
  request('POST', '/child/files/copy', { paths, destination, overwrite });

export const getTrashList = () => request('GET', '/child/files/trash');
export const restoreFromTrash = (trashId: number) =>
  request('POST', `/child/files/trash/${trashId}/restore`);
export const deletePermanently = (trashId: number) =>
  request('DELETE', `/child/files/trash/${trashId}`);
export const emptyTrash = () => request('POST', '/child/files/trash/empty');

// Permission management
export const getFileStat = (path: string) =>
  request('GET', `/child/files/stat?path=${encodeURIComponent(path)}`);
export const chmodFile = (path: string, mode: string) =>
  request('POST', '/child/files/chmod', { path, mode });
export const chownFile = (path: string, owner?: string, group?: string) =>
  request('POST', '/child/files/owner', { path, owner, group });

// File search
export const searchFiles = (query: string, searchType?: string, recursive?: boolean) =>
  request('GET', `/child/files/search?query=${encodeURIComponent(query)}&type=${searchType || 'filename'}&recursive=${recursive !== false}`);


// Image manipulation
export const resizeImage = (path: string, width: number, height: number) =>
  request('POST', '/child/files/image/resize', { path, width, height });
export const rotateImage = (path: string, degrees: number) =>
  request('POST', '/child/files/image/rotate', { path, degrees });
export const cropImage = (path: string, x: number, y: number, width: number, height: number) =>
  request('POST', '/child/files/image/crop', { path, x, y, width, height });

// Batch download
export const downloadMultipleFiles = async (paths: string[]) => {
  const token = getAccessToken();
  const res = await fetch(`${BASE}/child/files/download-multiple`, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ paths }),
  });
  if (!res.ok) throw new Error('Download failed');
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'download.zip';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  setTimeout(() => URL.revokeObjectURL(url), 1000);
};

export const getChildAccount = () => request('GET', '/child/account');
export const getChildDockerStats = () => request('GET', '/child/account/docker-stats');

export const getDatabases = () => request('GET', '/child/databases');
export const createDatabase = (db_name: string) =>
  request('POST', '/child/databases', { db_name });
export const deleteDatabase = (id: number) =>
  request('DELETE', `/child/databases/${id}`);
export const getPhpMyAdminLink = (dbId: number) =>
  request('GET', `/child/databases/phpmyadmin?db_id=${dbId}`);

export const getDbUsers = () => request('GET', '/child/databases/users');
export const createDbUser = (username: string, password: string) =>
  request('POST', '/child/databases/users', { username, password });
export const changeDbUserPassword = (id: number, password: string) =>
  request('PUT', `/child/databases/users/${id}/password`, { password });
export const assignDbUser = (userId: number, dbId: number, privileges?: string) =>
  request('POST', `/child/databases/users/${userId}/assign`, { db_id: dbId, privileges });
export const unassignDbUser = (userId: number, dbId: number) =>
  request('POST', `/child/databases/users/${userId}/unassign`, { db_id: dbId });
export const deleteDbUser = (id: number) =>
  request('DELETE', `/child/databases/users/${id}`);

export const getRemoteAccess = () => request('GET', '/child/databases/remote');
export const updateRemoteAccess = (dbId: number, enabled: boolean) =>
  request('PUT', `/child/databases/remote/${dbId}`, { enabled });

export const getDomains = () => request('GET', '/child/domains');
export const createDomain = (domain: string, type: string) =>
  request('POST', '/child/domains', { domain, type });
export const deleteDomain = (id: number) =>
  request('DELETE', `/child/domains/${id}`);

export const getTickets = (params?: any) => request('GET', '/child/tickets' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const createTicket = (data: any) => request('POST', '/child/tickets', data);
export const getTicketMessages = (id: number, params?: any) => request('GET', `/child/tickets/${id}` + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const replyTicket = (id: number, message: string) => request('POST', `/child/tickets/${id}/reply`, { message });
export const updateTicketStatus = (id: number, status: string) => request('PUT', `/child/tickets/${id}/status`, { status });
export const deleteTicket = (id: number) => request('DELETE', `/child/tickets/${id}`);

export const getAdminTickets = (params?: any) => request('GET', '/tickets' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const getAdminTicketMessages = (id: number, params?: any) => request('GET', `/tickets/${id}` + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const replyAdminTicket = (id: number, message: string) => request('POST', `/tickets/${id}/reply`, { message });
export const updateAdminTicketStatus = (id: number, status: string) => request('PUT', `/tickets/${id}/status`, { status });
export const deleteAdminTicket = (id: number) => request('DELETE', `/tickets/${id}`);

export const getBandwidth = (params?: any) => request('GET', '/child/bandwidth' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const getAdminBandwidthSummary = () => request('GET', '/bandwidth/summary');
export const getAdminBandwidthAccounts = () => request('GET', '/bandwidth/accounts');

export const getPhpVersions = () => request('GET', '/php-versions');
export const updatePhpVersion = (id: number, data: any) => request('PUT', `/php-versions/${id}`, data);
export const deletePhpVersion = (id: number) => request('DELETE', `/php-versions/${id}`);
export const downloadPhpVersion = (id: number) => request('POST', `/php-versions/${id}/download`);
export const cancelDownloadPhpVersion = (id: number) => request('POST', `/php-versions/${id}/cancel`);
export const getDownloadStatuses = () => request('GET', `/php-versions/downloads/status`);
export const activatePhpVersion = (id: number) => request('POST', `/php-versions/${id}/activate`);
export const deactivatePhpVersion = (id: number) => request('POST', `/php-versions/${id}/deactivate`);
export const updatePhpVersionPackages = (id: number) => request('POST', `/php-versions/${id}/update`);
export const uninstallPhpVersion = (id: number) => request('POST', `/php-versions/${id}/uninstall`);

export const getAvailablePhpVersions = () => request('GET', '/child/php-versions');
export const getCurrentPhpVersion = () => request('GET', '/child/php-versions/current');
export const selectPhpVersion = (phpVersionId: number) => request('PUT', '/child/php-versions/select', { php_version_id: phpVersionId });

export const getCMSInstalls = () => request('GET', '/child/cms');
export const getCMSInstall = (id: number) => request('GET', `/child/cms/${id}`);
export const checkSSLDomain = (domain: string) => request('GET', `/child/cms/ssl-check/${domain}`);
export const getCMSVersions = () => request('GET', '/child/cms/versions');
export const installCMS = (data: any) => request('POST', '/child/cms/install', data);
export const deleteCMSInstall = (id: number) => request('DELETE', `/child/cms/${id}`);

export const getSSLCerts = () => request('GET', '/child/ssl');
export const issueSSLCert = (data: any) => request('POST', '/child/ssl/issue', data);
export const setDomainForceHTTPS = (id: number, force: boolean) =>
  request('PUT', `/child/domains/${id}/https`, { force });
export const deleteSSLCert = (id: number) => request('DELETE', `/child/ssl/${id}`);
export const installCustomSSLCert = (data: any) => request('POST', '/child/ssl/custom', data);

export const getEmails = () => request('GET', '/child/emails');
export const createEmail = (data: any) => request('POST', '/child/emails', data);
export const updateEmail = (id: number, data: any) => request('PUT', `/child/emails/${id}`, data);
export const deleteEmail = (id: number) => request('DELETE', `/child/emails/${id}`);
export const getEmailCount = () => request('GET', '/child/emails/count');

export const getInbox = (id: number) => request('GET', `/child/emails/${id}/inbox`);
export const readMessage = (id: number, mid: number) => request('GET', `/child/emails/${id}/messages/${mid}`);
export const sendEmail = (id: number, data: any) => request('POST', `/child/emails/${id}/send`, data);
export const deleteMessage = (id: number, mid: number) => request('DELETE', `/child/emails/${id}/messages/${mid}`);

export const getFTPAccounts = () => request('GET', '/child/ftp');
export const createFTPAccount = (data: any) => request('POST', '/child/ftp', data);
export const updateFTPAccount = (id: number, data: any) => request('PUT', `/child/ftp/${id}`, data);
export const deleteFTPAccount = (id: number) => request('DELETE', `/child/ftp/${id}`);

export const getRecentErrors = () => request('GET', '/child/errors/recent');
export const getCustomErrorPages = () => request('GET', '/child/errors/custom');
export const getCustomErrorContent = (id: number) => request('GET', `/child/errors/custom/${id}`);
export const saveCustomErrorPage = (data: any) => request('PUT', '/child/errors/custom', data);
export const deleteCustomErrorPage = (id: number) => request('DELETE', `/child/errors/custom/${id}`);

export const getErrorPagesByDomain = (domainId: number) =>
  request('GET', `/child/errors/custom/by-domain/${domainId}`);
export const getErrorPageContent = (id: number) =>
  request('GET', `/child/errors/custom/${id}`);
export const saveErrorPage = (data: any) =>
  request('PUT', '/child/errors/custom', data);
export const deleteErrorPage = (id: number) =>
  request('DELETE', `/child/errors/custom/${id}`);
export const toggleErrorPage = (id: number, enabled: boolean) =>
  request('POST', `/child/errors/custom/${id}/toggle`, { enabled });
export const resetErrorPage = (id: number) =>
  request('POST', `/child/errors/custom/${id}/reset`);
export const testErrorPage = (id: number) =>
  request('POST', `/child/errors/custom/${id}/test`);
export const getErrorPageStats = (domainId: number) =>
  request('GET', `/child/errors/custom/stats?domain_id=${domainId}`);
export const exportErrorPages = (domainId: number) =>
  request('GET', `/child/errors/custom/export?domain_id=${domainId}`);
export const importErrorPages = (data: any) =>
  request('POST', '/child/errors/custom/import', data);

export const getSubmissions = (params?: any) => request('GET', '/submissions' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const deleteSubmission = (id: number) => request('DELETE', `/submissions/${id}`);

export const changePassword = (data: any) => request('PUT', '/child/auth/change-password', data);

export const getChildNotifications = () => request('GET', '/child/notifications');
export const getUnreadNotificationCount = () => request('GET', '/child/notifications/unread-count');
export const markNotificationRead = (id: number) => request('POST', `/child/notifications/${id}/read`);
export const markAllNotificationsRead = () => request('POST', '/child/notifications/read-all');

export const getAdminNotifications = () => request('GET', '/notifications');
export const createAdminNotification = (data: { account_id?: number; title: string; message: string }) => request('POST', '/notifications', data);
export const deleteAdminNotification = (id: number) => request('DELETE', `/notifications/${id}`);

// ---------- Backups ----------

export const getBackups = () => request('GET', '/child/backups');
export const getBackupSummary = () => request('GET', '/child/backups/summary');
export const createBackup = (type: 'full' | 'files' | 'database', notes: string) =>
  request('POST', '/child/backups', { type, notes });
export const getBackupStatus = (id: number) =>
  request('GET', `/child/backups/${id}/status`);
export const deleteBackup = (id: number) => request('DELETE', `/child/backups/${id}`);
export const restoreBackup = (id: number, restore_files: boolean, restore_database: boolean) =>
  request('POST', `/child/backups/${id}/restore`, { restore_files, restore_database });

export const downloadBackup = async (id: number, fallbackName: string) => {
  const token = getAccessToken();
  const res = await fetch(`${BASE}/child/backups/${id}/download`, {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${token}` },
  });
  if (!res.ok) throw new Error('Download failed');
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  const cd = res.headers.get('Content-Disposition') || '';
  const m = cd.match(/filename="?([^";]+)"?/);
  a.download = m ? m[1] : fallbackName;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  setTimeout(() => URL.revokeObjectURL(url), 1000);
};

export const getBackupSchedules = () => request('GET', '/child/backups/schedules');
export const createBackupSchedule = (data: Partial<BackupSchedule>) =>
  request('POST', '/child/backups/schedules', data);
export const updateBackupSchedule = (id: number, data: Partial<BackupSchedule>) =>
  request('PUT', `/child/backups/schedules/${id}`, data);
export const deleteBackupSchedule = (id: number) =>
  request('DELETE', `/child/backups/schedules/${id}`);

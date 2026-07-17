export interface Account { id: number; username: string; domain: string; email: string; package_name: string; status: string; home_dir: string; disk_used_mb: number; bandwidth_used_mb: number; created_at: string; }
export interface Package { id: number; name: string; disk_mb: number; bandwidth_mb: number; max_db: number; max_email: number; max_ftp: number; max_domains: number; max_subdomains: number; ssh_access: number; backup_enabled: number; is_default: number; }
export interface Domain { id: number; domain: string; type: string; doc_root: string; ssl_enabled: boolean; ssl_status: 'issued' | 'issuing' | 'expired' | 'failed' | 'none'; created_at: string; }
export interface Database { id: number; db_name: string; db_user: string; host: string; size_mb: number; }
export interface DBUser { id: number; username: string; }
export interface EmailAccount { id: number; email: string; forward_to: string; quota_mb: number; status: string; }
export interface CMSInstall { id: number; domain: string; cms_type: string; version: string; install_url: string; admin_url: string; status: string; }
export interface SSLCert { id: number; domain: string; issuer: string; expires_at: string; auto_renew: number; status: string; created_at: string; }
export interface Ticket { id: number; subject: string; status: string; created_at: string; }
export interface Backup { id: number; domain: string; type: string; file_size: number; status: string; created_at: string; }
export interface CronJob { id: number; command: string; schedule: string; enabled: number; }
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

// Allow each panel build to use different localStorage keys and API paths
let tokenPrefix = 'owp_admin_';
export function setTokenPrefix(prefix: string) { tokenPrefix = prefix; }
let refreshPath = '/auth/refresh';
export function setRefreshPath(path: string) { refreshPath = path; }

function tk(key: string): string { return tokenPrefix + key; }

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
}

export function getUser() {
  try {
    const u = localStorage.getItem(tk('user'));
    return u ? JSON.parse(u) : null;
  } catch (e: any) {
    console.error('Get user:', e);
    return null;
  }
}

async function request(method: string, path: string, body?: any): Promise<any> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getAccessToken();
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401 && getRefreshToken()) {
    // Try refresh
    const refreshRes = await fetch(`${BASE}${refreshPath}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getRefreshToken()}` },
      body: JSON.stringify({ refresh_token: getRefreshToken() }),
    });
    if (refreshRes.ok) {
      const tokens: TokenPair = await refreshRes.json();
      setTokens(tokens);
      headers['Authorization'] = `Bearer ${tokens.access_token}`;
      const retryRes = await fetch(`${BASE}${path}`, { method, headers, body: body ? JSON.stringify(body) : undefined });
      if (!retryRes.ok) {
        const err = await retryRes.json().catch(() => ({ error: retryRes.statusText }));
        throw err;
      }
      return retryRes.json();
    }
    clearTokens();
    throw { error: 'Session expired' };
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw err;
  }

  return res.json();
}

// CAPTCHA
export interface CaptchaChallenge {
  type: string;
  prompt: string;
}

export interface CaptchaResult {
  session_id: string;
  challenge: CaptchaChallenge;
}

export const getCaptcha = (isChild: boolean): Promise<CaptchaResult> =>
  request('GET', isChild ? '/child/auth/captcha' : '/auth/captcha');

// Auth
export const login = (username: string, password: string, captcha_session_id?: string, captcha_answer?: string) =>
  request('POST', '/auth/login', { username, password, captcha_session_id, captcha_answer }).then((tokens: TokenPair) => {
    setTokens(tokens);
    return tokens;
  });

export const loginChild = (username: string, password: string, captcha_session_id?: string, captcha_answer?: string) =>
  request('POST', '/child/auth/login', { username, password, captcha_session_id, captcha_answer }).then((tokens: TokenPair) => {
    setTokens(tokens);
    return tokens;
  });

export const getMe = () => request('GET', '/auth/me');

// Packages
export const getPackages = () => request('GET', '/packages');
export const createPackage = (data: any) => request('POST', '/packages', data);
export const updatePackage = (id: number, data: any) => request('PUT', `/packages/${id}`, data);
export const deletePackage = (id: number) => request('DELETE', `/packages/${id}`);

// Accounts
export const getAccounts = (status?: string) =>
  request('GET', `/accounts${status ? `?status=${status}` : ''}`);
export const getAccount = (id: number) => request('GET', `/accounts/${id}`);
export const createAccount = (data: any) => request('POST', '/accounts', data);
export const suspendAccount = (id: number, reason?: string) =>
  request('POST', `/accounts/${id}/suspend`, { reason });
export const unsuspendAccount = (id: number) =>
  request('POST', `/accounts/${id}/unsuspend`);
export const terminateAccount = (id: number) => request('DELETE', `/accounts/${id}`);
export const getAccountUploadLimit = (id: number) => request('GET', `/accounts/${id}/upload-limit`);
export const setAccountUploadLimit = (id: number, limitMB: number) =>
  request('PUT', `/accounts/${id}/upload-limit`, { limit_mb: limitMB });
export const getAccountRamLimit = (id: number) => request('GET', `/accounts/${id}/ram-limit`);
export const setAccountRamLimit = (id: number, limitMB: number) =>
  request('PUT', `/accounts/${id}/ram-limit`, { limit_mb: limitMB });

// Stats
export const getStatsOverview = () => request('GET', '/stats/overview');
export const getServerStatus = () => request('GET', '/server/status');

// Settings
export const getSettings = () => request('GET', '/settings');
export const updateSettings = (updates: Record<string, string>) =>
  request('PUT', '/settings', updates);
export const getUploadLimit = () => request('GET', '/settings/upload-limit');

// Child: Files
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

    // Idle timeout: if no upload progress for 30s, abort.
    // After upload completes, give the server 2 min to process.
    let idleTimer: ReturnType<typeof setTimeout>;
    function resetIdle(ms: number) {
      clearTimeout(idleTimer);
      idleTimer = setTimeout(() => {
        xhr.abort();
        reject({ error: `Upload timed out — no activity for ${ms / 1000}s` });
      }, ms);
    }

    // Upload phase — real bytes-sent progress, no artificial capping.
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) {
        const pct = Math.round((e.loaded / e.total) * 100);
        onProgress?.(Math.min(pct, 100), 'uploading');
        resetIdle(30_000);
      }
    };

    // Upload finished — wait for the server to write & respond.
    xhr.upload.onload = () => {
      onProgress?.(100, 'processing');
      resetIdle(120_000); // 2 min for server-side write
    };

    xhr.onload = () => {
      clearTimeout(idleTimer);
      let parsed: any;
      try {
        parsed = JSON.parse(xhr.responseText || '{}');
      } catch {
        parsed = { error: 'Invalid server response' };
      }
      if (xhr.status === 200) {
        onProgress?.(100, 'done');
        resolve(parsed);
      } else {
        reject(parsed);
      }
    };

    xhr.onerror = () => { clearTimeout(idleTimer); reject({ error: 'Upload failed — network error' }); };
    xhr.onabort = () => { clearTimeout(idleTimer); reject({ error: 'Upload cancelled' }); };
    xhr.send(form);
  });
};

export const compressFiles = (path: string, files: string[], archiveName: string) =>
  request('POST', '/child/files/compress', { path, files, archive_name: archiveName });
export const extractFile = (archivePath: string) =>
  request('POST', '/child/files/extract', { path: archivePath });

// Child: Trash
export const getTrashList = () => request('GET', '/child/files/trash');
export const restoreFromTrash = (trashId: number) =>
  request('POST', `/child/files/trash/${trashId}/restore`);
export const deletePermanently = (trashId: number) =>
  request('DELETE', `/child/files/trash/${trashId}`);
export const emptyTrash = () => request('POST', '/child/files/trash/empty');

// Child: Account
export const getChildAccount = () => request('GET', '/child/account');

// Child: DBs
export const getDatabases = () => request('GET', '/child/databases');
export const createDatabase = (db_name: string) =>
  request('POST', '/child/databases', { db_name });
export const deleteDatabase = (id: number) =>
  request('DELETE', `/child/databases/${id}`);

export const getPhpMyAdminLink = (dbId: number) =>
  request('GET', `/child/databases/phpmyadmin?db_id=${dbId}`);

// Child: DB Users
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

// Child: Remote DB Access
export const getRemoteAccess = () => request('GET', '/child/databases/remote');
export const updateRemoteAccess = (dbId: number, remoteAccess: string) =>
  request('PUT', `/child/databases/remote/${dbId}`, { remote_access: remoteAccess });

// Child: Domains
export const getDomains = () => request('GET', '/child/domains');
export const createDomain = (domain: string, type: string) =>
  request('POST', '/child/domains', { domain, type });
export const deleteDomain = (id: number) =>
  request('DELETE', `/child/domains/${id}`);

// Tickets
export const getTickets = (params?: any) => request('GET', '/child/tickets' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const createTicket = (data: any) => request('POST', '/child/tickets', data);
export const getTicketMessages = (id: number, params?: any) => request('GET', `/child/tickets/${id}` + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const replyTicket = (id: number, message: string) => request('POST', `/child/tickets/${id}/reply`, { message });
export const updateTicketStatus = (id: number, status: string) => request('PUT', `/child/tickets/${id}/status`, { status });
export const deleteTicket = (id: number) => request('DELETE', `/child/tickets/${id}`);

// Admin Tickets
export const getAdminTickets = (params?: any) => request('GET', '/tickets' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const getAdminTicketMessages = (id: number, params?: any) => request('GET', `/tickets/${id}` + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const replyAdminTicket = (id: number, message: string) => request('POST', `/tickets/${id}/reply`, { message });
export const updateAdminTicketStatus = (id: number, status: string) => request('PUT', `/tickets/${id}/status`, { status });
export const deleteAdminTicket = (id: number) => request('DELETE', `/tickets/${id}`);

// Bandwidth
export const getBandwidth = (params?: any) => request('GET', '/child/bandwidth' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const getAdminBandwidthSummary = () => request('GET', '/bandwidth/summary');
export const getAdminBandwidthAccounts = () => request('GET', '/bandwidth/accounts');

// PHP Versions (Admin)
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

// PHP Versions (Child)
export const getAvailablePhpVersions = () => request('GET', '/child/php-versions');
export const getCurrentPhpVersion = () => request('GET', '/child/php-versions/current');
export const selectPhpVersion = (phpVersionId: number) => request('PUT', '/child/php-versions/select', { php_version_id: phpVersionId });

// CMS
export const getCMSInstalls = () => request('GET', '/child/cms');
export const getCMSInstall = (id: number) => request('GET', `/child/cms/${id}`);
export const checkSSLDomain = (domain: string) => request('GET', `/child/cms/ssl-check/${domain}`);
export const getCMSVersions = () => request('GET', '/child/cms/versions');
export const installCMS = (data: any) => request('POST', '/child/cms/install', data);
export const deleteCMSInstall = (id: number) => request('DELETE', `/child/cms/${id}`);

// SSL
export const getSSLCerts = () => request('GET', '/child/ssl');
export const issueSSLCert = (data: any) => request('POST', '/child/ssl/issue', data);
export const deleteSSLCert = (id: number) => request('DELETE', `/child/ssl/${id}`);
export const installCustomSSLCert = (data: any) => request('POST', '/child/ssl/custom', data);

// Emails
export const getEmails = () => request('GET', '/child/emails');
export const createEmail = (data: any) => request('POST', '/child/emails', data);
export const updateEmail = (id: number, data: any) => request('PUT', `/child/emails/${id}`, data);
export const deleteEmail = (id: number) => request('DELETE', `/child/emails/${id}`);
export const getEmailCount = () => request('GET', '/child/emails/count');

// Webmail
export const getInbox = (id: number) => request('GET', `/child/emails/${id}/inbox`);
export const readMessage = (id: number, mid: number) => request('GET', `/child/emails/${id}/messages/${mid}`);
export const sendEmail = (id: number, data: any) => request('POST', `/child/emails/${id}/send`, data);
export const deleteMessage = (id: number, mid: number) => request('DELETE', `/child/emails/${id}/messages/${mid}`);

// FTP
export const getFTPAccounts = () => request('GET', '/child/ftp');
export const createFTPAccount = (data: any) => request('POST', '/child/ftp', data);
export const updateFTPAccount = (id: number, data: any) => request('PUT', `/child/ftp/${id}`, data);
export const deleteFTPAccount = (id: number) => request('DELETE', `/child/ftp/${id}`);

// Error Pages (legacy)
export const getRecentErrors = () => request('GET', '/child/errors/recent');
export const getCustomErrorPages = () => request('GET', '/child/errors/custom');
export const getCustomErrorContent = (id: number) => request('GET', `/child/errors/custom/${id}`);
export const saveCustomErrorPage = (data: any) => request('PUT', '/child/errors/custom', data);
export const deleteCustomErrorPage = (id: number) => request('DELETE', `/child/errors/custom/${id}`);

// Error Code Handling (new)
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

// Submissions
export const getSubmissions = (params?: any) => request('GET', '/submissions' + (params ? '?' + new URLSearchParams(params).toString() : ''));
export const deleteSubmission = (id: number) => request('DELETE', `/submissions/${id}`);

// Change password
export const changePassword = (data: any) => request('PUT', '/child/auth/change-password', data);

// Notifications
export const getChildNotifications = () => request('GET', '/child/notifications');
export const getUnreadNotificationCount = () => request('GET', '/child/notifications/unread-count');
export const markNotificationRead = (id: number) => request('POST', `/child/notifications/${id}/read`);
export const markAllNotificationsRead = () => request('POST', '/child/notifications/read-all');

// Admin notifications
export const getAdminNotifications = () => request('GET', '/notifications');
export const createAdminNotification = (data: { account_id?: number; title: string; message: string }) => request('POST', '/notifications', data);
export const deleteAdminNotification = (id: number) => request('DELETE', `/notifications/${id}`);

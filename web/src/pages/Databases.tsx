import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import {
  getDatabases, createDatabase, deleteDatabase, getPhpMyAdminLink,
  getDbUsers, createDbUser, changeDbUserPassword, assignDbUser, unassignDbUser, deleteDbUser,
  getRemoteAccess, updateRemoteAccess,
} from '../lib/api';
import {
  Database, Plus, Trash2, ExternalLink, Loader2, Server, Key, X, AlertTriangle,
  Users, Shield, Eye, EyeOff, Globe, Pencil,
} from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';

/* ── shared helpers ── */
function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

function ErrorBanner({ msg, onDismiss }: { msg: string; onDismiss: () => void }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -10 }}
      animate={{ opacity: 1, y: 0 }}
      className="flex items-center gap-2 p-3 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-600 dark:text-red-300"
    >
      <AlertTriangle className="h-4 w-4 shrink-0" /> {msg}
      <button onClick={onDismiss} className="ml-auto text-red-400 hover:text-red-600"><X className="h-4 w-4" /></button>
    </motion.div>
  );
}

type Tab = 'databases' | 'users' | 'remote';

const tabs: { key: Tab; label: string; icon: typeof Database }[] = [
  { key: 'databases', label: 'Databases', icon: Database },
  { key: 'users', label: 'Database Users', icon: Users },
  { key: 'remote', label: 'Remote Access', icon: Globe },
];

/* ── Databases page ── */
export function Databases() {
  const [tab, setTab] = useState<Tab>('databases');
  const [error, setError] = useState('');

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="space-y-5"
    >
      <div>
        <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Databases</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Manage databases, users, and remote access</p>
      </div>

      {error && <ErrorBanner msg={error} onDismiss={() => setError('')} />}

      {/* Tab bar */}
      <div className="flex gap-1 bg-gray-100 dark:bg-gray-800 rounded-lg p-1 w-fit">
        {tabs.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => { setTab(key); setError(''); }}
            className={`flex items-center gap-1.5 px-4 py-2 rounded-md text-sm font-medium transition-colors ${
              tab === key ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm' : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'
            }`}
          >
            <Icon className="h-4 w-4" /> {label}
          </button>
        ))}
      </div>

      {tab === 'databases' && <DatabasesTab onError={setError} />}
      {tab === 'users' && <UsersTab onError={setError} />}
      {tab === 'remote' && <RemoteTab onError={setError} />}
    </motion.div>
  );
}

/* ── Databases Tab ── */
function DatabasesTab({ onError }: { onError: (e: string) => void }) {
  const [dbs, setDbs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const load = useCallback(() => {
    setLoading(true);
    getDatabases().then((d) => setDbs(d || [])).catch(() => {}).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const [showCreate, setShowCreate] = useState(false);
  const [dbName, setDbName] = useState('');
  const [creating, setCreating] = useState(false);
  const [pmaLoading, setPmaLoading] = useState<number | null>(null);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    try {
      await createDatabase(dbName);
      setShowCreate(false); setDbName(''); onError(''); load();
    } catch (err: any) { onError(err?.error || 'Failed'); }
    finally { setCreating(false); }
  };

  const handleDelete = async (id: number, name: string) => {
    if (!confirm(`Delete "${name}"?`)) return;
    await deleteDatabase(id); load();
  };

  const handlePhpMyAdmin = async (dbId: number) => {
    setPmaLoading(dbId);
    try { const d = await getPhpMyAdminLink(dbId); window.open(`/pma/${d.token}/`, '_blank', 'noopener,noreferrer'); }
    catch (err: any) { onError(err?.error || 'Failed'); }
    finally { setPmaLoading(null); }
  };

  return (
    <>
      <div className="flex items-center justify-between">
        <p className="text-sm text-gray-500 dark:text-gray-400">{loading ? '...' : `${dbs.length} databases`}</p>
        <Button onClick={() => setShowCreate(true)} size="sm">
          <Plus className="h-4 w-4" /> Create Database
        </Button>
      </div>

      <div className="space-y-3">
        {loading && dbs.length === 0 && [1,2].map(i => (
          <Card key={i}>
            <div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 mb-3 animate-pulse" />
            <div className="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2 animate-pulse" />
          </Card>
        ))}
        {dbs.map((db, i) => (
          <motion.div
            key={db.id}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.05 }}
          >
            <Card>
              <div className="flex items-start justify-between">
                <div className="flex items-start gap-3">
                  <div className="p-2 bg-blue-50 dark:bg-blue-900/30 rounded-lg"><Database className="h-5 w-5 text-blue-600 dark:text-blue-400" /></div>
                  <div>
                    <h3 className="font-medium text-gray-900 dark:text-gray-100">{db.db_name}</h3>
                    <div className="mt-1.5 space-y-0.5 text-xs text-gray-500 dark:text-gray-400">
                      <div className="flex items-center gap-1.5"><Server className="h-3 w-3" /> {db.host || 'localhost'}</div>
                      <div className="flex items-center gap-1.5"><Key className="h-3 w-3" /> {db.db_user}</div>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Button variant="secondary" size="sm" onClick={() => handlePhpMyAdmin(db.id)} disabled={pmaLoading === db.id} loading={pmaLoading === db.id}>
                    <ExternalLink className="h-3.5 w-3.5" /> phpMyAdmin
                  </Button>
                  <button onClick={() => handleDelete(db.id, db.db_name)} className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </Card>
          </motion.div>
        ))}
        {!loading && dbs.length === 0 && (
          <EmptyState
            icon={<Database size={28} className="text-gray-400" />}
            title="No databases yet"
            message="Create your first database"
            actionLabel="Create Database"
            onAction={() => setShowCreate(true)}
          />
        )}
      </div>

      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowCreate(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5"><h3 className="font-semibold text-gray-900 dark:text-gray-100">Create Database</h3>
              <button onClick={() => setShowCreate(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button></div>
            <form onSubmit={handleCreate} className="space-y-3">
              <div><label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Database Name</label>
                <input type="text" autoFocus value={dbName} onChange={e => setDbName(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="my_database" pattern="[a-zA-Z0-9_]+" required /></div>
              <Button type="submit" disabled={creating || !dbName} className="w-full" loading={creating}>
                Create Database
              </Button>
            </form>
          </div>
        </div>
      )}
    </>
  );
}

/* ── Users Tab ── */
function UsersTab({ onError }: { onError: (e: string) => void }) {
  const [users, setUsers] = useState<any[]>([]);
  const [dbs, setDbs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    Promise.all([getDbUsers(), getDatabases()]).then(([u, d]) => {
      setUsers(u || []); setDbs(d || []);
    }).catch(() => {}).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const [showCreate, setShowCreate] = useState(false);
  const [newUser, setNewUser] = useState('');
  const [newPass, setNewPass] = useState('');
  const [creating, setCreating] = useState(false);
  const [showPw, setShowPw] = useState<number | null>(null);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault(); setCreating(true);
    try { await createDbUser(newUser, newPass); setShowCreate(false); setNewUser(''); setNewPass(''); onError(''); load(); }
    catch (err: any) { onError(err?.error || 'Failed'); }
    finally { setCreating(false); }
  };

  const handlePassword = async (id: number) => {
    const pw = prompt('New password (min 8 chars):');
    if (!pw || pw.length < 8) return;
    await changeDbUserPassword(id, pw);
    onError('');
  };

  const [assignModal, setAssignModal] = useState<number | null>(null);
  const [assignDbId, setAssignDbId] = useState(0);

  const handleAssign = async () => {
    if (!assignDbId || assignModal === null) return;
    await assignDbUser(assignModal, assignDbId);
    setAssignModal(null); load();
  };

  const handleUnassign = async (uid: number, dbId: number) => {
    await unassignDbUser(uid, dbId); load();
  };

  const handleDelete = async (id: number, name: string) => {
    if (!confirm(`Delete user "${name}"?`)) return;
    await deleteDbUser(id); load();
  };

  const getAssignedDbs = (u: any) => u.assigned_dbs ? u.assigned_dbs.split(',').map(Number) : [];

  return (
    <>
      <div className="flex items-center justify-between">
        <p className="text-sm text-gray-500 dark:text-gray-400">{loading ? '...' : `${users.length} users`}</p>
        <Button onClick={() => setShowCreate(true)} size="sm">
          <Plus className="h-4 w-4" /> Create User
        </Button>
      </div>

      <div className="space-y-3">
        {loading && users.length === 0 && [1,2].map(i => (
          <Card key={i}>
            <div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 mb-3 animate-pulse" /><div className="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2 animate-pulse" />
          </Card>
        ))}
        {users.map((u, i) => {
          const assigned = getAssignedDbs(u);
          return (
            <motion.div
              key={u.id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.05 }}
            >
              <Card>
                <div className="flex items-start justify-between">
                  <div>
                    <div className="flex items-center gap-2">
                      <div className="p-1.5 bg-purple-50 dark:bg-purple-900/30 rounded-lg"><Users className="h-4 w-4 text-purple-600 dark:text-purple-400" /></div>
                      <h3 className="font-medium text-gray-900 dark:text-gray-100">{u.username}</h3>
                    </div>
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      {assigned.length > 0 ? assigned.map((dbId: number) => {
                        const db = dbs.find((d: any) => d.id === dbId);
                        return db ? (
                          <span key={dbId} className="inline-flex items-center gap-1 px-2 py-0.5 bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded-full text-xs">
                            {db.db_name}
                            <button onClick={() => handleUnassign(u.id, dbId)} className="text-blue-400 hover:text-red-500"><X className="h-3 w-3" /></button>
                          </span>
                        ) : null;
                      }) : <span className="text-xs text-gray-400 dark:text-gray-500">No databases assigned</span>}
                    </div>
                  </div>
                  <div className="flex items-center gap-1">
                    <button onClick={() => setAssignModal(u.id)} className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 rounded-lg hover:bg-blue-50 dark:hover:bg-blue-900/30" title="Assign to database">
                      <Plus className="h-4 w-4" /></button>
                    <button onClick={() => handlePassword(u.id)} className="p-1.5 text-gray-400 hover:text-amber-600 dark:hover:text-amber-400 rounded-lg hover:bg-amber-50 dark:hover:bg-amber-900/30" title="Change password">
                      <Pencil className="h-4 w-4" /></button>
                    <button onClick={() => handleDelete(u.id, u.username)} className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 rounded-lg hover:bg-red-50 dark:hover:bg-red-900/30" title="Delete">
                      <Trash2 className="h-4 w-4" /></button>
                  </div>
                </div>
              </Card>
            </motion.div>
          );
        })}
        {!loading && users.length === 0 && (
          <EmptyState
            icon={<Users size={28} className="text-gray-400" />}
            title="No database users"
            message="Create your first user"
            actionLabel="Create User"
            onAction={() => setShowCreate(true)}
          />
        )}
      </div>

      {/* Create user modal */}
      {showCreate && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowCreate(false)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5"><h3 className="font-semibold text-gray-900 dark:text-gray-100">Create Database User</h3>
              <button onClick={() => setShowCreate(false)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button></div>
            <form onSubmit={handleCreate} className="space-y-3">
              <div><label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username</label>
                <input type="text" autoFocus value={newUser} onChange={e => setNewUser(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="db_user" pattern="[a-zA-Z0-9_]+" required /></div>
              <div><label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Password</label>
                <div className="relative">
                  <input type={showPw ? 'text' : 'password'} value={newPass} onChange={e => setNewPass(e.target.value)}
                    className="w-full px-3 py-2 pr-10 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                    placeholder="min 8 characters" required minLength={8} />
                  <button type="button" onClick={() => setShowPw(showPw ? null : 1)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400">
                    {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</button>
                </div>
              </div>
              <Button type="submit" disabled={creating || !newUser || !newPass} className="w-full" loading={creating}>
                Create User
              </Button>
            </form>
          </div>
        </div>
      )}

      {/* Assign to DB modal */}
      {assignModal !== null && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setAssignModal(null)}>
          <div className="bg-white dark:bg-gray-800 rounded-xl w-full max-w-sm mx-4 p-6 shadow-xl" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between mb-5"><h3 className="font-semibold text-gray-900 dark:text-gray-100">Assign to Database</h3>
              <button onClick={() => setAssignModal(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-5 w-5" /></button></div>
            <div className="space-y-3">
              <select value={assignDbId} onChange={e => setAssignDbId(+e.target.value)} className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded-lg text-sm dark:text-gray-100">
                <option value={0}>Select database...</option>
                {dbs.map((d: any) => (
                  <option key={d.id} value={d.id} disabled={getAssignedDbs(users.find((u: any) => u.id === assignModal) || {}).includes(d.id)}>
                    {d.db_name}
                  </option>
                ))}
              </select>
              <Button onClick={handleAssign} disabled={!assignDbId} className="w-full">
                Assign User
              </Button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

/* ── Remote Access Tab ── */
function RemoteTab({ onError }: { onError: (e: string) => void }) {
  const [remotes, setRemotes] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    getRemoteAccess().then((d) => setRemotes(d || [])).catch(() => {}).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const [editId, setEditId] = useState<number | null>(null);
  const [editVal, setEditVal] = useState('');
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (editId === null) return;
    setSaving(true);
    try { await updateRemoteAccess(editId, editVal); setEditId(null); load(); }
    catch (err: any) { onError(err?.error || 'Failed'); }
    finally { setSaving(false); }
  };

  const accessHelp: Record<string, string> = {
    'localhost': 'Local only (default)',
    '%': 'Any remote host',
    '192.168.%': 'Local network',
  };

  return (
    <>
      <p className="text-sm text-gray-500 dark:text-gray-400">{loading ? '...' : `${remotes.length} databases`} &middot; Configure which IPs can connect remotely</p>

      <div className="space-y-3">
        {loading && remotes.length === 0 && [1,2].map(i => (
          <Card key={i}>
            <div className="h-4 bg-gray-200 dark:bg-gray-700 rounded w-1/3 mb-3 animate-pulse" /><div className="h-3 bg-gray-100 dark:bg-gray-700 rounded w-1/2 animate-pulse" />
          </Card>
        ))}
        {remotes.map((r, i) => (
          <motion.div
            key={r.id}
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.05 }}
          >
            <Card>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="p-2 bg-indigo-50 dark:bg-indigo-900/30 rounded-lg"><Shield className="h-5 w-5 text-indigo-600 dark:text-indigo-400" /></div>
                  <div>
                    <h3 className="font-medium text-gray-900 dark:text-gray-100">{r.db_name}</h3>
                    {editId === r.id ? (
                      <div className="flex items-center gap-2 mt-1">
                        <input type="text" value={editVal} onChange={e => setEditVal(e.target.value)}
                          className="px-2 py-1 border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 rounded text-sm dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 w-40"
                          placeholder="% or IP" />
                        <Button size="sm" onClick={handleSave} loading={saving}>Save</Button>
                        <button onClick={() => setEditId(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-4 w-4" /></button>
                      </div>
                    ) : (
                      <div className="flex items-center gap-2 mt-1">
                        <code className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/30 px-2 py-0.5 rounded">{r.remote_access}</code>
                        <span className="text-xs text-gray-400 dark:text-gray-500">{accessHelp[r.remote_access] || 'Custom'}</span>
                        <button onClick={() => { setEditId(r.id); setEditVal(r.remote_access); }}
                          className="p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"><Pencil className="h-3.5 w-3.5" /></button>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </Card>
          </motion.div>
        ))}
        {!loading && remotes.length === 0 && (
          <EmptyState
            icon={<Globe size={28} className="text-gray-400" />}
            title="No databases to configure"
          />
        )}
      </div>

      <div className="bg-gray-50 dark:bg-gray-800/50 rounded-lg p-3 text-xs text-gray-500 dark:text-gray-400">
        <p className="font-medium mb-1">Remote access hosts:</p>
        <ul className="space-y-0.5 ml-4 list-disc">
          <li><code>localhost</code> — local connections only</li>
          <li><code>%</code> — allow any remote IP</li>
          <li><code>192.168.1.%</code> — allow a specific subnet</li>
          <li><code>10.0.0.1</code> — allow a single IP</li>
        </ul>
      </div>
    </>
  );
}

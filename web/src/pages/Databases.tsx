import { useEffect, useState, useCallback } from 'react';
import { motion } from 'framer-motion';
import {
  getDatabases, createDatabase, deleteDatabase, getPhpMyAdminLink,
  getDbUsers, createDbUser, changeDbUserPassword, assignDbUser, unassignDbUser, deleteDbUser,
  getRemoteAccess, updateRemoteAccess,
} from '../lib/api';
import {
  Database, Plus, Trash2, ExternalLink, Loader2, Server, Key, X,
  Users, Shield, Eye, EyeOff, Globe, Pencil, AlertTriangle,
} from 'lucide-react';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Modal from '../components/ui/Modal';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

function ErrorBanner({ msg, onDismiss }: { msg: string; onDismiss: () => void }) {
  return (
    <motion.div
      initial={{ opacity: 0, y: -8 }}
      animate={{ opacity: 1, y: 0 }}
      className="flex items-center gap-2 p-3 bg-red-50 border border-red-200 rounded-md text-sm text-red-700"
    >
      <AlertTriangle className="h-4 w-4 shrink-0" />
      <span className="flex-1">{msg}</span>
      <button onClick={onDismiss} className="text-red-400 hover:text-red-600 p-0.5"><X className="h-4 w-4" /></button>
    </motion.div>
  );
}

type Tab = 'databases' | 'users' | 'remote';

const tabs: { key: Tab; label: string; icon: typeof Database }[] = [
  { key: 'databases', label: 'Databases', icon: Database },
  { key: 'users', label: 'Users', icon: Users },
  { key: 'remote', label: 'Remote Access', icon: Globe },
];

export function Databases() {
  const [tab, setTab] = useState<Tab>('databases');
  const [error, setError] = useState('');

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="max-w-4xl mx-auto space-y-5"
    >
      <div>
        <h1 className="text-lg font-semibold text-gray-900">Databases</h1>
        <p className="text-sm text-gray-500 mt-0.5">Manage databases, users, and remote access</p>
      </div>

      {error && <ErrorBanner msg={error} onDismiss={() => setError('')} />}

      {/* Tabs */}
      <div className="flex gap-1 border-b border-gray-200">
        {tabs.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => { setTab(key); setError(''); }}
            className={`flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors ${
              tab === key
                ? 'border-purple-600 text-purple-700'
                : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
            }`}
          >
            <Icon className="h-4 w-4" strokeWidth={1.5} /> {label}
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
    getDatabases().then((d) => setDbs(d || [])).catch((e: any) => console.error('Load databases:', e)).finally(() => setLoading(false));
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
    try { await deleteDatabase(id); load(); } catch (err: any) { onError(err?.error || 'Delete failed'); }
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
        <p className="text-sm text-gray-500">{loading ? '...' : `${dbs.length} databases`}</p>
        <Button onClick={() => setShowCreate(true)} size="sm">
          <Plus className="h-4 w-4" /> Create Database
        </Button>
      </div>

      <div className="space-y-2">
        {loading && dbs.length === 0 && [1, 2].map(i => (
          <Card key={i}>
            <div className="h-4 bg-gray-100 rounded w-1/3 mb-3 animate-pulse" />
            <div className="h-3 bg-gray-50 rounded w-1/2 animate-pulse" />
          </Card>
        ))}
        {dbs.map((db, i) => (
          <motion.div key={db.id} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.03 }}>
            <Card hover>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3 min-w-0 flex-1">
                  <div className="p-2 rounded-md bg-purple-50 shrink-0">
                    <Database className="h-4 w-4 text-purple-600" strokeWidth={1.5} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <h3 className="text-sm font-medium text-gray-900">{db.db_name}</h3>
                    <div className="flex items-center gap-3 mt-0.5 text-xs text-gray-400">
                      <span className="flex items-center gap-1"><Server className="h-3 w-3" /> {db.host || 'localhost'}</span>
                      <span className="flex items-center gap-1"><Key className="h-3 w-3" /> {db.db_user}</span>
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <Button variant="secondary" size="sm" onClick={() => handlePhpMyAdmin(db.id)} disabled={pmaLoading === db.id} loading={pmaLoading === db.id}>
                    <ExternalLink className="h-3.5 w-3.5" /> phpMyAdmin
                  </Button>
                  <button onClick={() => handleDelete(db.id, db.db_name)} className="p-1.5 text-gray-400 hover:text-red-600 rounded-md hover:bg-red-50 transition-colors">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </Card>
          </motion.div>
        ))}
        {!loading && dbs.length === 0 && (
          <EmptyState
            icon={<Database className="h-5 w-5 text-gray-400" />}
            title="No databases yet"
            message="Create your first database"
            actionLabel="Create Database"
            onAction={() => setShowCreate(true)}
          />
        )}
      </div>

      <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create Database" size="sm">
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Database Name</label>
            <input type="text" autoFocus value={dbName} onChange={e => setDbName(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all"
              placeholder="my_database" pattern="[a-zA-Z0-9_]+" required />
          </div>
          <Button type="submit" disabled={creating || !dbName} className="w-full" loading={creating}>
            Create Database
          </Button>
        </form>
      </Modal>
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
    }).catch((e: any) => console.error('Load users:', e)).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const [showCreate, setShowCreate] = useState(false);
  const [newUser, setNewUser] = useState('');
  const [newPass, setNewPass] = useState('');
  const [creating, setCreating] = useState(false);
  const [showPw, setShowPw] = useState(false);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault(); setCreating(true);
    try { await createDbUser(newUser, newPass); setShowCreate(false); setNewUser(''); setNewPass(''); onError(''); load(); }
    catch (err: any) { onError(err?.error || 'Failed'); }
    finally { setCreating(false); }
  };

  const handlePassword = async (id: number) => {
    const pw = prompt('New password (min 8 chars):');
    if (!pw || pw.length < 8) return;
    try { await changeDbUserPassword(id, pw); onError(''); } catch (err: any) { onError(err?.error || 'Password change failed'); }
  };

  const [assignModal, setAssignModal] = useState<number | null>(null);
  const [assignDbId, setAssignDbId] = useState(0);

  const handleAssign = async () => {
    if (!assignDbId || assignModal === null) return;
    try { await assignDbUser(assignModal, assignDbId); setAssignModal(null); load(); } catch (err: any) { onError(err?.error || 'Assign failed'); }
  };

  const handleUnassign = async (uid: number, dbId: number) => {
    try { await unassignDbUser(uid, dbId); load(); } catch (err: any) { onError(err?.error || 'Unassign failed'); }
  };

  const handleDelete = async (id: number, name: string) => {
    if (!confirm(`Delete user "${name}"?`)) return;
    try { await deleteDbUser(id); load(); } catch (err: any) { onError(err?.error || 'Delete failed'); }
  };

  const getAssignedDbs = (u: any) => u.assigned_dbs ? u.assigned_dbs.split(',').map(Number) : [];

  return (
    <>
      <div className="flex items-center justify-between">
        <p className="text-sm text-gray-500">{loading ? '...' : `${users.length} users`}</p>
        <Button onClick={() => setShowCreate(true)} size="sm">
          <Plus className="h-4 w-4" /> Create User
        </Button>
      </div>

      <div className="space-y-2">
        {loading && users.length === 0 && [1, 2].map(i => (
          <Card key={i}>
            <div className="h-4 bg-gray-100 rounded w-1/3 mb-3 animate-pulse" /><div className="h-3 bg-gray-50 rounded w-1/2 animate-pulse" />
          </Card>
        ))}
        {users.map((u, i) => {
          const assigned = getAssignedDbs(u);
          return (
            <motion.div key={u.id} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.03 }}>
              <Card hover>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 min-w-0 flex-1">
                    <div className="p-2 rounded-md bg-blue-50 shrink-0">
                      <Users className="h-4 w-4 text-blue-600" strokeWidth={1.5} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <h3 className="text-sm font-medium text-gray-900">{u.username}</h3>
                      <div className="flex flex-wrap gap-1.5 mt-1">
                        {assigned.length > 0 ? assigned.map((dbId: number) => {
                          const db = dbs.find((d: any) => d.id === dbId);
                          return db ? (
                            <span key={dbId} className="inline-flex items-center gap-1 px-2 py-0.5 bg-blue-50 text-blue-700 rounded-md text-xs">
                              {db.db_name}
                              <button onClick={() => handleUnassign(u.id, dbId)} className="text-blue-400 hover:text-red-500"><X className="h-3 w-3" /></button>
                            </span>
                          ) : null;
                        }) : <Badge variant="neutral">No databases assigned</Badge>}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1 shrink-0">
                    <button onClick={() => setAssignModal(u.id)} className="p-1.5 text-gray-400 hover:text-purple-600 rounded-md hover:bg-purple-50 transition-colors" title="Assign">
                      <Plus className="h-4 w-4" /></button>
                    <button onClick={() => handlePassword(u.id)} className="p-1.5 text-gray-400 hover:text-amber-600 rounded-md hover:bg-amber-50 transition-colors" title="Change password">
                      <Pencil className="h-4 w-4" /></button>
                    <button onClick={() => handleDelete(u.id, u.username)} className="p-1.5 text-gray-400 hover:text-red-600 rounded-md hover:bg-red-50 transition-colors" title="Delete">
                      <Trash2 className="h-4 w-4" /></button>
                  </div>
                </div>
              </Card>
            </motion.div>
          );
        })}
        {!loading && users.length === 0 && (
          <EmptyState
            icon={<Users className="h-5 w-5 text-gray-400" />}
            title="No database users"
            message="Create your first user"
            actionLabel="Create User"
            onAction={() => setShowCreate(true)}
          />
        )}
      </div>

      <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create Database User" size="sm">
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Username</label>
            <input type="text" autoFocus value={newUser} onChange={e => setNewUser(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all"
              placeholder="db_user" pattern="[a-zA-Z0-9_]+" required />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Password</label>
            <div className="relative">
              <input type={showPw ? 'text' : 'password'} value={newPass} onChange={e => setNewPass(e.target.value)}
                className="w-full px-3 py-2 pr-10 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all"
                placeholder="min 8 characters" required minLength={8} />
              <button type="button" onClick={() => setShowPw(!showPw)}
                className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600">
                {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}</button>
            </div>
          </div>
          <Button type="submit" disabled={creating || !newUser || !newPass} className="w-full" loading={creating}>
            Create User
          </Button>
        </form>
      </Modal>

      <Modal isOpen={assignModal !== null} onClose={() => setAssignModal(null)} title="Assign to Database" size="sm">
        <div className="space-y-4">
          <select value={assignDbId} onChange={e => setAssignDbId(+e.target.value)}
            className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 focus:border-transparent transition-all">
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
      </Modal>
    </>
  );
}

/* ── Remote Access Tab ── */
function RemoteTab({ onError }: { onError: (e: string) => void }) {
  const [remotes, setRemotes] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    getRemoteAccess().then((d) => setRemotes(d || [])).catch((e: any) => console.error('Load remote:', e)).finally(() => setLoading(false));
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
      <p className="text-sm text-gray-500">{loading ? '...' : `${remotes.length} databases`} &middot; Configure remote IP access</p>

      <div className="space-y-2">
        {loading && remotes.length === 0 && [1, 2].map(i => (
          <Card key={i}>
            <div className="h-4 bg-gray-100 rounded w-1/3 mb-3 animate-pulse" /><div className="h-3 bg-gray-50 rounded w-1/2 animate-pulse" />
          </Card>
        ))}
        {remotes.map((r, i) => (
          <motion.div key={r.id} initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: i * 0.03 }}>
            <Card hover>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3 min-w-0 flex-1">
                  <div className="p-2 rounded-md bg-purple-50 shrink-0">
                    <Shield className="h-4 w-4 text-purple-600" strokeWidth={1.5} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <h3 className="text-sm font-medium text-gray-900">{r.db_name}</h3>
                    {editId === r.id ? (
                      <div className="flex items-center gap-2 mt-1">
                        <input type="text" value={editVal} onChange={e => setEditVal(e.target.value)}
                          className="px-2.5 py-1.5 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-purple-500 w-40 transition-all"
                          placeholder="% or IP" />
                        <Button size="sm" onClick={handleSave} loading={saving}>Save</Button>
                        <button onClick={() => setEditId(null)} className="p-1 text-gray-400 hover:text-gray-600"><X className="h-4 w-4" /></button>
                      </div>
                    ) : (
                      <div className="flex items-center gap-2 mt-1">
                        <code className="text-xs text-purple-700 bg-purple-50 px-2 py-0.5 rounded font-mono">{r.remote_access}</code>
                        <span className="text-xs text-gray-400">{accessHelp[r.remote_access] || 'Custom'}</span>
                        <button onClick={() => { setEditId(r.id); setEditVal(r.remote_access); }}
                          className="p-0.5 text-gray-400 hover:text-purple-600"><Pencil className="h-3 w-3" /></button>
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
            icon={<Globe className="h-5 w-5 text-gray-400" />}
            title="No databases to configure"
          />
        )}
      </div>

      <div className="bg-gray-50 border border-gray-200 rounded-md p-3 text-xs text-gray-500">
        <p className="font-medium text-gray-700 mb-1">Remote access hosts</p>
        <ul className="space-y-0.5 ml-4 list-disc">
          <li><code className="text-purple-600">localhost</code> — local only</li>
          <li><code className="text-purple-600">%</code> — any remote IP</li>
          <li><code className="text-purple-600">192.168.1.%</code> — subnet</li>
          <li><code className="text-purple-600">10.0.0.1</code> — single IP</li>
        </ul>
      </div>
    </>
  );
}

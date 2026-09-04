import { useEffect, useState, useCallback } from 'react';
import {
  getDatabases, createDatabase, deleteDatabase, getPhpMyAdminLink,
  getDbUsers, createDbUser, changeDbUserPassword, assignDbUser, unassignDbUser, deleteDbUser,
  getRemoteAccess, updateRemoteAccess,
} from '../lib/api';
import {
  Database, Plus, Trash2, ExternalLink, Loader2, Server, Key, X,
  Users, Shield, Eye, EyeOff, Globe, Pencil, AlertTriangle, Info,
} from 'lucide-react';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import EmptyState from '../components/ui/EmptyState';
import Modal from '../components/ui/Modal';

function Spinner() { return <Loader2 className="h-4 w-4 animate-spin" />; }

function ErrorBanner({ msg, onDismiss }: { msg: string; onDismiss: () => void }) {
  return (
    <div className="flex items-center gap-2 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
      <AlertTriangle className="h-4 w-4 shrink-0" />
      <span className="flex-1">{msg}</span>
      <button onClick={onDismiss} className="text-red-400 hover:text-red-600 p-0.5" aria-label="Dismiss">
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}

const iconBtn = 'p-1.5 text-gray-400 hover:text-blue-600 hover:bg-blue-50 rounded-md transition-colors';
const dangerBtn = 'p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded-md transition-colors';

type Tab = 'databases' | 'users' | 'remote';

const tabs: { key: Tab; label: string; icon: typeof Database }[] = [
  { key: 'databases', label: 'Databases', icon: Database },
  { key: 'users', label: 'Users', icon: Users },
  { key: 'remote', label: 'Remote Access', icon: Globe },
];

function SegmentedTabs({ value, onChange }: { value: Tab; onChange: (t: Tab) => void }) {
  return (
    <div className="inline-flex items-center gap-1 p-1 bg-gray-100 rounded-lg">
      {tabs.map(({ key, label, icon: Icon }) => (
        <button
          key={key}
          onClick={() => onChange(key)}
          className={`flex items-center gap-1.5 px-3.5 py-1.5 text-sm font-medium rounded-md transition-all ${
            value === key
              ? 'bg-white text-blue-700 shadow-sm'
              : 'text-gray-500 hover:text-gray-700'
          }`}
        >
          <Icon className="h-4 w-4" strokeWidth={1.5} /> {label}
        </button>
      ))}
    </div>
  );
}

function ToggleSwitch({ checked, onChange, disabled }: { checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex items-center h-6 w-11 rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 ${
        checked ? 'bg-blue-600' : 'bg-gray-200'
      } ${disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}`}
    >
      <span
        className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform ${
          checked ? 'translate-x-[22px]' : 'translate-x-0.5'
        }`}
      />
    </button>
  );
}

export function Databases() {
  const [tab, setTab] = useState<Tab>('databases');
  const [error, setError] = useState('');

  return (
    <div className="max-w-5xl mx-auto space-y-5">
      <div>
        <h1 className="text-lg font-semibold text-gray-900">Databases</h1>
        <p className="text-sm text-gray-500 mt-0.5">Manage databases, users, and remote access</p>
      </div>

      {error && <ErrorBanner msg={error} onDismiss={() => setError('')} />}

      <SegmentedTabs value={tab} onChange={(t) => { setTab(t); setError(''); }} />

      {tab === 'databases' && <DatabasesTab onError={setError} />}
      {tab === 'users' && <UsersTab onError={setError} />}
      {tab === 'remote' && <RemoteTab onError={setError} />}
    </div>
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
    try {
      const d = await getPhpMyAdminLink(dbId);
      const link = d.url || `/pma/${d.token}/`;
      window.open(link, '_blank', 'noopener,noreferrer');
    }
    catch (err: any) { onError(err?.error || 'Failed'); }
    finally { setPmaLoading(null); }
  };

  return (
    <>
      <div className="flex items-center justify-between mb-3">
        <p className="text-sm text-gray-500">{loading ? 'Loading…' : `${dbs.length} database${dbs.length === 1 ? '' : 's'}`}</p>
        <Button onClick={() => setShowCreate(true)} size="sm">
          <Plus className="h-4 w-4" /> Create Database
        </Button>
      </div>

      {loading && dbs.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          {[1, 2, 3].map(i => <div key={i} className="h-14 border-b border-gray-100 animate-pulse" />)}
        </div>
      ) : dbs.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg">
          <EmptyState
            icon={<Database className="h-5 w-5 text-gray-400" />}
            title="No databases yet"
            message="Create your first database to get started"
            actionLabel="Create Database"
            onAction={() => setShowCreate(true)}
          />
        </div>
      ) : (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-500 border-b border-gray-200 bg-gray-50">
                <th className="px-4 py-3 font-medium">Database</th>
                <th className="px-4 py-3 font-medium">Host</th>
                <th className="px-4 py-3 font-medium">User</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {dbs.map(db => (
                <tr key={db.id} className="hover:bg-gray-50 transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2.5">
                      <div className="w-8 h-8 rounded-md bg-blue-50 flex items-center justify-center shrink-0">
                        <Database className="h-4 w-4 text-blue-600" strokeWidth={1.5} />
                      </div>
                      <span className="font-medium text-gray-900">{db.db_name}</span>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-gray-500">{db.host || 'localhost'}</td>
                  <td className="px-4 py-3 text-gray-500">{db.db_user}</td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <button onClick={() => handlePhpMyAdmin(db.id)} disabled={pmaLoading === db.id} className={iconBtn} title="Open phpMyAdmin">
                        {pmaLoading === db.id ? <Spinner /> : <ExternalLink className="h-4 w-4" />}
                      </button>
                      <button onClick={() => handleDelete(db.id, db.db_name)} className={dangerBtn} title="Delete database">
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create Database" size="sm">
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Database Name</label>
            <input type="text" autoFocus value={dbName} onChange={e => setDbName(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all"
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
      <div className="flex items-center justify-between mb-3">
        <p className="text-sm text-gray-500">{loading ? 'Loading…' : `${users.length} user${users.length === 1 ? '' : 's'}`}</p>
        <Button onClick={() => setShowCreate(true)} size="sm">
          <Plus className="h-4 w-4" /> Create User
        </Button>
      </div>

      {loading && users.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          {[1, 2, 3].map(i => <div key={i} className="h-14 border-b border-gray-100 animate-pulse" />)}
        </div>
      ) : users.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg">
          <EmptyState
            icon={<Users className="h-5 w-5 text-gray-400" />}
            title="No database users"
            message="Create a user to connect to your databases"
            actionLabel="Create User"
            onAction={() => setShowCreate(true)}
          />
        </div>
      ) : (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-500 border-b border-gray-200 bg-gray-50">
                <th className="px-4 py-3 font-medium">Username</th>
                <th className="px-4 py-3 font-medium">Assigned Databases</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {users.map(u => {
                const assigned = getAssignedDbs(u);
                return (
                  <tr key={u.id} className="hover:bg-gray-50 transition-colors">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5">
                        <div className="w-8 h-8 rounded-md bg-blue-50 flex items-center justify-center shrink-0">
                          <Users className="h-4 w-4 text-blue-600" strokeWidth={1.5} />
                        </div>
                        <span className="font-medium text-gray-900">{u.username}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      {assigned.length > 0 ? (
                        <div className="flex flex-wrap gap-1.5">
                          {assigned.map((dbId: number) => {
                            const db = dbs.find((d: any) => d.id === dbId);
                            return db ? (
                              <span key={dbId} className="inline-flex items-center gap-1 px-2 py-0.5 bg-blue-50 text-blue-700 rounded-md text-xs">
                                {db.db_name}
                                <button onClick={() => handleUnassign(u.id, dbId)} className="text-blue-400 hover:text-red-500" title="Remove access">
                                  <X className="h-3 w-3" />
                                </button>
                              </span>
                            ) : null;
                          })}
                        </div>
                      ) : (
                        <span className="text-xs text-gray-400">None assigned</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center justify-end gap-1">
                        <button onClick={() => { setAssignModal(u.id); setAssignDbId(0); }} className={iconBtn} title="Assign to database">
                          <Plus className="h-4 w-4" />
                        </button>
                        <button onClick={() => handlePassword(u.id)} className={iconBtn} title="Change password">
                          <Pencil className="h-4 w-4" />
                        </button>
                        <button onClick={() => handleDelete(u.id, u.username)} className={dangerBtn} title="Delete user">
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <Modal isOpen={showCreate} onClose={() => setShowCreate(false)} title="Create Database User" size="sm">
        <form onSubmit={handleCreate} className="space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Username</label>
            <input type="text" autoFocus value={newUser} onChange={e => setNewUser(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all"
              placeholder="db_user" pattern="[a-zA-Z0-9_]+" required />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-500 mb-1.5">Password</label>
            <div className="relative">
              <input type={showPw ? 'text' : 'password'} value={newPass} onChange={e => setNewPass(e.target.value)}
                className="w-full px-3 py-2 pr-10 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all"
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
            className="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all">
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
  const [editId, setEditId] = useState<number | null>(null);
  const [editEnabled, setEditEnabled] = useState(false);
  const [savingId, setSavingId] = useState<number | null>(null);

  const load = useCallback(() => {
    setLoading(true);
    getRemoteAccess().then((d) => setRemotes(d || [])).catch((e: any) => console.error('Load remote:', e)).finally(() => setLoading(false));
  }, []);
  useEffect(() => { load(); }, [load]);

  const isEnabled = (r: any) => r.remote_access === '%';

  const openEdit = (r: any) => {
    setEditId(r.id);
    setEditEnabled(isEnabled(r));
  };

  const handleSave = async (id: number, enabled: boolean) => {
    setSavingId(id);
    try {
      await updateRemoteAccess(id, enabled);
      setEditId(null);
      onError('');
      load();
    } catch (err: any) {
      onError(err?.error || 'Failed to update remote access');
    } finally {
      setSavingId(null);
    }
  };

  return (
    <>
      <p className="text-sm text-gray-500 mb-3">{loading ? 'Loading…' : `${remotes.length} database${remotes.length === 1 ? '' : 's'}`}</p>

      {loading && remotes.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          {[1, 2, 3].map(i => <div key={i} className="h-14 border-b border-gray-100 animate-pulse" />)}
        </div>
      ) : remotes.length === 0 ? (
        <div className="bg-white border border-gray-200 rounded-lg">
          <EmptyState
            icon={<Globe className="h-5 w-5 text-gray-400" />}
            title="No databases to configure"
            message="Remote access settings appear here once you have databases"
          />
        </div>
      ) : (
        <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-500 border-b border-gray-200 bg-gray-50">
                <th className="px-4 py-3 font-medium">Database</th>
                <th className="px-4 py-3 font-medium">Remote Access</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {remotes.map(r => {
                const editing = editId === r.id;
                const enabled = isEnabled(r);
                return (
                  <tr key={r.id} className="hover:bg-gray-50 transition-colors">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2.5">
                        <div className="w-8 h-8 rounded-md bg-blue-50 flex items-center justify-center shrink-0">
                          <Shield className="h-4 w-4 text-blue-600" strokeWidth={1.5} />
                        </div>
                        <span className="font-medium text-gray-900">{r.db_name}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      {editing ? (
                        <div className="flex items-center gap-3">
                          <ToggleSwitch checked={editEnabled} onChange={setEditEnabled} />
                          <span className={`text-xs font-medium ${editEnabled ? 'text-blue-600' : 'text-gray-500'}`}>
                            {editEnabled ? 'Enabled' : 'Disabled'}
                          </span>
                        </div>
                      ) : (
                        <Badge variant={enabled ? 'info' : 'neutral'} dot>
                          {enabled ? 'Remote enabled' : 'Local only'}
                        </Badge>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center justify-end gap-1.5">
                        {editing ? (
                          <>
                            <Button size="sm" onClick={() => handleSave(r.id, editEnabled)} loading={savingId === r.id}>
                              Save
                            </Button>
                            <button onClick={() => setEditId(null)} className={iconBtn} title="Cancel">
                              <X className="h-4 w-4" />
                            </button>
                          </>
                        ) : (
                          <button onClick={() => openEdit(r)} className={iconBtn} title="Edit remote access">
                            <Pencil className="h-4 w-4" />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4 text-xs text-blue-800 flex items-start gap-2.5">
        <Info className="h-4 w-4 text-blue-600 shrink-0 mt-0.5" />
        <div>
          <p className="font-medium text-blue-900 mb-1">What does this do?</p>
          <p className="text-blue-700 leading-relaxed">
            Enable remote access to connect to this database from outside the server (e.g. desktop tools like MySQL Workbench).
            Disabled means only this server can connect. Local connections via phpMyAdmin always work.
          </p>
        </div>
      </div>
    </>
  );
}

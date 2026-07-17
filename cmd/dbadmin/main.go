package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	_ "github.com/mattn/go-sqlite3"
)

var (
	dbDir     string
	systemDB  *sql.DB
	childDBs  = make(map[string]*sql.DB)
	jwtManager *auth.JWTManager
)

func main() {
	dbDir = os.Getenv("OWP_DBADMIN_DIR")
	if dbDir == "" {
		dbDir = "."
	}

	// Initialize JWT manager from env
	jwtSecret := os.Getenv("OWP_JWT_SECRET")
	if jwtSecret == "" {
		log.Fatalf("OWP_JWT_SECRET environment variable is not set. This is critical for security.")
	}
	jwtManager = auth.NewJWTManager(jwtSecret, 900, 604800) // 15 min access, 7 day refresh

	// Connect to the system DB to look up child databases
	sysPath := os.Getenv("OWP_DB_PATH")
	if sysPath == "" {
		sysPath = "./openwebpanel.db"
	}
	var err error
	systemDB, err = sql.Open("sqlite3", sysPath)
	if err != nil {
		log.Fatalf("Failed to open system DB: %v", err)
	}

	port := os.Getenv("OWP_DBADMIN_PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRequest)

	log.Printf("DB Admin Tool running on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func authenticateRequest(w http.ResponseWriter, r *http.Request) bool {
	// First try JWT Bearer token (set by parentd proxy)
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			_, err := jwtManager.ValidateToken(parts[1])
			if err == nil {
				return true
			}
		}
	}

	// Fallback to Basic Auth with admin credential check
	user, pass, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="DB Admin"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	// Validate credentials against admins table
	var storedHash string
	err := systemDB.QueryRow("SELECT password_hash FROM admins WHERE username = ?", user).Scan(&storedHash)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	if !auth.CheckPassword(storedHash, pass) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	return true
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	if !authenticateRequest(w, r) {
		return
	}

	// Index page or login redirect
	if path == "" || path == "index.php" || path == "index.html" {
		serveIndex(w, r)
		return
	}

	// API: list databases
	if path == "api/databases" {
		handleListDatabases(w)
		return
	}

	// API: list tables in a database
	if strings.HasPrefix(path, "api/tables/") {
		dbName := strings.TrimPrefix(path, "api/tables/")
		handleListTables(w, dbName)
		return
	}

	// API: run query
	if path == "api/query" && r.Method == "POST" {
		handleQuery(w, r)
		return
	}

	// Static files: serve the single-page app
	serveIndex(w, r)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>DB Admin — OpenWebPanel</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f0f4f8; color: #1a1a2e; }
.header { background: #1a1a2e; color: #fff; padding: 16px 24px; display: flex; align-items: center; justify-content: space-between; }
.header h1 { font-size: 18px; font-weight: 600; }
.layout { display: flex; height: calc(100vh - 56px); }
.sidebar { width: 260px; background: #fff; border-right: 1px solid #e2e8f0; padding: 16px; overflow-y: auto; }
.sidebar h3 { font-size: 11px; text-transform: uppercase; color: #94a3b8; letter-spacing: 0.05em; margin-bottom: 8px; }
.db-list { list-style: none; }
.db-list li { padding: 8px 12px; cursor: pointer; border-radius: 6px; margin-bottom: 2px; font-size: 14px; color: #334155; }
.db-list li:hover { background: #f1f5f9; color: #1a1a2e; }
.db-list li.active { background: #e0e7ff; color: #4338ca; font-weight: 500; }
.main { flex: 1; padding: 24px; overflow-y: auto; }
.toolbar { display: flex; gap: 8px; margin-bottom: 16px; flex-wrap: wrap; }
.toolbar button { padding: 8px 16px; background: #fff; border: 1px solid #e2e8f0; border-radius: 6px; cursor: pointer; font-size: 13px; }
.toolbar button:hover { background: #f8fafc; border-color: #cbd5e1; }
.toolbar button.primary { background: #4338ca; color: #fff; border-color: #4338ca; }
.toolbar button.primary:hover { background: #3730a3; }
.sql-editor { width: 100%; min-height: 120px; padding: 12px; border: 1px solid #e2e8f0; border-radius: 8px; font-family: 'SF Mono', 'Fira Code', monospace; font-size: 13px; resize: vertical; margin-bottom: 12px; }
.sql-editor:focus { outline: none; border-color: #4338ca; box-shadow: 0 0 0 3px rgba(67, 56, 202, 0.1); }
.results { background: #fff; border: 1px solid #e2e8f0; border-radius: 8px; overflow: hidden; }
.results table { width: 100%; border-collapse: collapse; font-size: 13px; }
.results th { background: #f8fafc; padding: 8px 12px; text-align: left; font-weight: 600; color: #475569; border-bottom: 1px solid #e2e8f0; }
.results td { padding: 8px 12px; border-bottom: 1px solid #f1f5f9; color: #334155; max-width: 300px; overflow: hidden; text-overflow: ellipsis; }
.results tr:hover td { background: #f8fafc; }
.info { font-size: 13px; color: #64748b; margin-bottom: 12px; }
.error { color: #dc2626; background: #fef2f2; padding: 8px 12px; border-radius: 6px; margin-bottom: 12px; font-size: 13px; }
.success { color: #16a34a; background: #f0fdf4; padding: 8px 12px; border-radius: 6px; margin-bottom: 12px; font-size: 13px; }
.loading { opacity: 0.5; pointer-events: none; }
.empty { text-align: center; padding: 40px; color: #94a3b8; }
</style>
</head>
<body>
<div class="header"><h1>Database Admin</h1><span style="font-size:12px;opacity:0.7">OpenWebPanel</span></div>
<div class="layout">
  <div class="sidebar">
    <h3>Databases</h3>
    <ul class="db-list" id="dbList"><li class="empty" style="font-size:12px">Loading...</li></ul>
  </div>
  <div class="main">
    <div class="toolbar">
      <button class="primary" onclick="runQuery()">Run Query (Ctrl+Enter)</button>
      <button onclick="document.getElementById('sqlInput').value='SELECT name, type FROM sqlite_master WHERE type IN (\'table\',\'view\') ORDER BY name'">Show Tables</button>
      <button onclick="document.getElementById('sqlInput').value='PRAGMA table_info(table_name)'">Table Schema</button>
      <button onclick="document.getElementById('sqlInput').value=''">Clear</button>
    </div>
    <div id="messages"></div>
    <textarea id="sqlInput" class="sql-editor" placeholder="Enter SQL query..." spellcheck="false">SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name</textarea>
    <div class="info" id="dbInfo">Select a database from the sidebar</div>
    <div class="results" id="results"><div class="empty">Run a query to see results</div></div>
  </div>
</div>
<script>
let currentDB = '';

document.getElementById('sqlInput').addEventListener('keydown', function(e) {
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) runQuery();
});

async function loadDatabases() {
  const res = await fetch('api/databases');
  const dbs = await res.json();
  const list = document.getElementById('dbList');
  list.innerHTML = '';
  for (const db of dbs) {
    const li = document.createElement('li');
    li.textContent = db.name + ' (' + db.type + ')';
    li.onclick = () => selectDB(db.name, li);
    if (db.name === currentDB) li.classList.add('active');
    list.appendChild(li);
  }
  if (dbs.length > 0 && !currentDB) selectDB(dbs[0].name, list.children[0]);
}

async function selectDB(name, el) {
  currentDB = name;
  document.querySelectorAll('.db-list li').forEach(l => l.classList.remove('active'));
  if (el) el.classList.add('active');
  document.getElementById('dbInfo').textContent = 'Database: ' + name;
  const res = await fetch('api/tables/' + encodeURIComponent(name));
  const tables = await res.json();
  if (tables.length > 0) {
    document.getElementById('sqlInput').value = 'SELECT * FROM ' + tables[0];
    document.getElementById('results').innerHTML = '<div class="empty">Run query to see data</div>';
  }
}

async function runQuery() {
  const sql = document.getElementById('sqlInput').value.trim();
  if (!sql || !currentDB) return;
  setLoading(true);
  clearMessages();
  try {
    const res = await fetch('api/query', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({db: currentDB, sql: sql})
    });
    const data = await res.json();
    if (data.error) { showError(data.error); return; }
    if (data.affected) { showSuccess(data.affected + ' rows affected'); return; }
    if (data.columns && data.rows) {
      let html = '<table><thead><tr>';
      for (const col of data.columns) html += '<th>' + col + '</th>';
      html += '</tr></thead><tbody>';
      for (const row of data.rows) {
        html += '<tr>';
        for (const cell of row) html += '<td>' + (cell === null ? '<span style="color:#94a3b8">NULL</span>' : escapeHtml(String(cell))) + '</td>';
        html += '</tr>';
      }
      html += '</tbody></table>';
      if (data.rows.length === 0) html = '<div class="empty">No results</div>';
      else html += '<div class="info" style="padding:8px 12px;border-top:1px solid #e2e8f0">' + data.rows.length + ' row(s)</div>';
      document.getElementById('results').innerHTML = html;
    }
  } catch (e) { showError('Query failed: ' + e.message); }
  finally { setLoading(false); }
}

function setLoading(v) { document.querySelector('.toolbar').classList.toggle('loading', v); }
function clearMessages() { document.getElementById('messages').innerHTML = ''; }
function showError(msg) { document.getElementById('messages').innerHTML = '<div class="error">' + escapeHtml(msg) + '</div>'; }
function showSuccess(msg) { document.getElementById('messages').innerHTML = '<div class="success">' + escapeHtml(msg) + '</div>'; }
function escapeHtml(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;'); }

loadDatabases();
</script>
</body>
</html>`)
}

func handleListDatabases(w http.ResponseWriter) {
	type dbInfo struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Path string `json:"path"`
	}
	dbs := []dbInfo{
		{Name: "system", Type: "main", Path: "openwebpanel.db"},
	}

	rows, err := systemDB.Query("SELECT db_name FROM child_databases")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var dbName string
			rows.Scan(&dbName)
			dbPath := filepath.Join(dbDir, dbName+".db")
			if _, err := os.Stat(dbPath); err == nil {
				dbs = append(dbs, dbInfo{Name: dbName, Type: "child", Path: dbPath})
			} else {
				dbs = append(dbs, dbInfo{Name: dbName, Type: "child (no file)", Path: ""})
			}
		}
	}

	json.NewEncoder(w).Encode(dbs)
}

func handleListTables(w http.ResponseWriter, dbName string) {
	db := getDB(dbName)
	if db == nil {
		json.NewEncoder(w).Encode([]string{})
		return
	}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type IN ('table','view') ORDER BY name")
	if err != nil {
		json.NewEncoder(w).Encode([]string{})
		return
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	json.NewEncoder(w).Encode(tables)
}

var allowedSQLPrefixes = []string{"SELECT ", "PRAGMA ", "EXPLAIN "}

func isAllowedQuery(sql string) bool {
	upper := strings.TrimSpace(strings.ToUpper(sql))
	for _, prefix := range allowedSQLPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DB  string `json:"db"`
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
		return
	}

	db := getDB(req.DB)
	if db == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "database not found"})
		return
	}

	// Restrict to read-only queries only
	// Critical security fix: Disallow direct arbitrary SQL execution.
	// This endpoint requires a complete refactor to use parameterized queries
	// for specific, safe operations only. Allowing raw SQL, even with prefix checks,
	// is highly dangerous.
	json.NewEncoder(w).Encode(map[string]string{"error": "Direct SQL query execution is disabled for security. Refactor needed."})
	return
}

func getDB(name string) *sql.DB {
	if name == "system" {
		return systemDB
	}
	if db, ok := childDBs[name]; ok {
		return db
	}

	dbPath := filepath.Join(dbDir, name+".db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil
	}
	childDBs[name] = db
	return db
}

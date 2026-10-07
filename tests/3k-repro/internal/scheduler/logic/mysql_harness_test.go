package logic

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sxwl/3k/internal/scheduler/config"
	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/internal/scheduler/svc"
	"sxwl/3k/internal/scheduler/types"

	_ "github.com/go-sql-driver/mysql"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

// mysql harness talks to a real MariaDB/MySQL loaded from
// deployment/sxcloud/database/init.sql. REPRO_MYSQL_DSN defaults to
// repro:repro@tcp(127.0.0.1:3306)/.

type harness struct {
	t    *testing.T
	db   *sql.DB
	name string
	svc  *svc.ServiceContext
}

var (
	schemaOnce       sync.Once
	schemaErr        error
	savedStrict      int
	savedFormat      string
	globalsSaved     bool
	isolationVarName string
)

func TestMain(m *testing.M) {
	code := m.Run()
	dropSchemaTemplate()
	os.Exit(code)
}

func adminDSN() (user, pass, host, port string) {
	raw := os.Getenv("REPRO_MYSQL_DSN")
	if raw == "" {
		raw = "repro:repro@tcp(127.0.0.1:3306)/"
	}
	// user:pass@tcp(host:port)/
	at := strings.Index(raw, "@tcp(")
	if at < 0 {
		return "repro", "repro", "127.0.0.1", "3306"
	}
	userpass := raw[:at]
	colon := strings.Index(userpass, ":")
	user, pass = userpass[:colon], userpass[colon+1:]
	rest := raw[at+5:]
	end := strings.Index(rest, ")")
	hp := rest[:end]
	hcolon := strings.LastIndex(hp, ":")
	return user, pass, hp[:hcolon], hp[hcolon+1:]
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := findModuleRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func findModuleRoot(dir string) (string, error) {
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.Contains(string(b), "module sxwl/3k") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("sxwl/3k go.mod not found from %s", dir)
		}
		dir = parent
	}
}

func ensureSchema(t *testing.T) {
	t.Helper()
	root := moduleRoot(t)
	schemaOnce.Do(func() {
		schemaErr = loadTemplate(root)
	})
	if schemaErr != nil {
		t.Fatalf("load schema: %v", schemaErr)
	}
}

func loadTemplate(root string) error {
	user, pass, host, port := adminDSN()
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/?parseTime=true&multiStatements=true", user, pass, host, port))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("mysql ping: %w (start mysqld and create the repro user)", err)
	}
	if err := db.QueryRow("SELECT @@GLOBAL.innodb_strict_mode, @@GLOBAL.innodb_default_row_format").Scan(&savedStrict, &savedFormat); err != nil {
		return err
	}
	globalsSaved = true
	defer restoreGlobals(db)

	if _, err := db.Exec("DROP DATABASE IF EXISTS repro_schema"); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE DATABASE repro_schema CHARACTER SET utf8mb4"); err != nil {
		return err
	}
	initSQL := filepath.Join(root, "deployment", "sxcloud", "database", "init.sql")
	cnf, err := os.CreateTemp("", "repro-my-*.cnf")
	if err != nil {
		return err
	}
	defer os.Remove(cnf.Name())
	if _, err := fmt.Fprintf(cnf, "[client]\nuser=%s\npassword=%s\nhost=%s\nport=%s\nprotocol=tcp\n", user, pass, host, port); err != nil {
		return err
	}
	cnf.Close()
	cmd := exec.Command("mysql", "--defaults-extra-file="+cnf.Name(), "repro_schema")
	sqlText, err := os.ReadFile(initSQL)
	if err != nil {
		return err
	}
	// The dump asks for ROW_FORMAT=COMPACT. MariaDB then stores varchar prefixes
	// inline and rejects sys_user (ERROR 1118). DYNAMIC keeps the same columns.
	sqlText = bytes.ReplaceAll(sqlText, []byte("ROW_FORMAT=COMPACT"), []byte("ROW_FORMAT=DYNAMIC"))
	if _, err := db.Exec("SET GLOBAL innodb_default_row_format='dynamic'"); err != nil {
		return err
	}
	if _, err := db.Exec("SET GLOBAL innodb_strict_mode=OFF"); err != nil {
		return err
	}
	cmd.Stdin = bytes.NewReader(sqlText)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mysql < init.sql: %w\n%s", err, out)
	}
	extra := []string{
		`CREATE TABLE IF NOT EXISTS sys_app (
			id bigint NOT NULL AUTO_INCREMENT,
			app_id varchar(255) NOT NULL DEFAULT '',
			app_name varchar(255) NOT NULL DEFAULT '',
			user_id varchar(255) NOT NULL DEFAULT '',
			` + "`desc`" + ` text,
			crd mediumtext,
			status bigint NOT NULL DEFAULT 0,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id)
		)`,
		`CREATE TABLE IF NOT EXISTS sys_app_job (
			id bigint NOT NULL AUTO_INCREMENT,
			job_name varchar(255) NOT NULL DEFAULT '',
			user_id varchar(255) NOT NULL DEFAULT '',
			app_id varchar(255) NOT NULL DEFAULT '',
			app_name varchar(255) NOT NULL DEFAULT '',
			instance_name varchar(255) NOT NULL DEFAULT '',
			cpod_id varchar(255) NOT NULL DEFAULT '',
			status bigint NOT NULL DEFAULT 0,
			billing_status bigint NOT NULL DEFAULT 1,
			url varchar(512) NOT NULL DEFAULT '',
			meta text,
			start_time datetime NULL,
			end_time datetime NULL,
			created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (id)
		)`,
	}
	schemaDB, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/repro_schema?parseTime=true&multiStatements=true", user, pass, host, port))
	if err != nil {
		return err
	}
	defer schemaDB.Close()
	for _, q := range extra {
		if _, err := schemaDB.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	if err := addColumnIfMissing(schemaDB, "sys_inference", "model_meta", "text NULL"); err != nil {
		return err
	}
	if err := addColumnIfMissing(schemaDB, "sys_cpod_node", "cpod_name", "varchar(255) NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return nil
}

// isolationVariable is transaction_isolation on MySQL 8 and tx_isolation on
// MariaDB 10.11. The name is discovered from the server and then set on
// every test connection.
func isolationVariable(db *sql.DB) (string, error) {
	if isolationVarName != "" {
		return isolationVarName, nil
	}
	var last error
	for _, name := range []string{"transaction_isolation", "tx_isolation"} {
		var current string
		err := db.QueryRow("SELECT @@" + name).Scan(&current)
		if err == nil {
			isolationVarName = name
			return name, nil
		}
		last = err
	}
	return "", fmt.Errorf("isolation variable: %w", last)
}

func addColumnIfMissing(db *sql.DB, table, column, def string) error {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'repro_schema' AND table_name = ? AND column_name = ?`, table, column).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	q := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", table, column, def)
	if _, err := db.Exec(q); err != nil {
		return fmt.Errorf("%s: %w", q, err)
	}
	return nil
}

func restoreGlobals(db *sql.DB) {
	if !globalsSaved {
		return
	}
	_, _ = db.Exec(fmt.Sprintf("SET GLOBAL innodb_strict_mode=%d", savedStrict))
	format := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			return r
		}
		return -1
	}, savedFormat)
	if format != "" {
		_, _ = db.Exec("SET GLOBAL innodb_default_row_format='" + format + "'")
	}
}

func dropSchemaTemplate() {
	user, pass, host, port := adminDSN()
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/?parseTime=true", user, pass, host, port))
	if err != nil {
		return
	}
	defer db.Close()
	if !globalsSaved {
		_ = db.QueryRow("SELECT @@GLOBAL.innodb_strict_mode, @@GLOBAL.innodb_default_row_format").Scan(&savedStrict, &savedFormat)
	}
	restoreGlobals(db)
	_, _ = db.Exec("DROP DATABASE IF EXISTS repro_schema")
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ensureSchema(t)
	user, pass, host, port := adminDSN()
	base := "r_" + strings.ReplaceAll(t.Name(), "/", "_")
	base = strings.ReplaceAll(base, "-", "_")
	suffix := fmt.Sprintf("_%d", time.Now().UnixNano()%1_000_000_000)
	if len(base)+len(suffix) > 64 {
		base = base[:64-len(suffix)]
	}
	name := base + suffix
	admin, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/?parseTime=true&multiStatements=true", user, pass, host, port))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + name); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	rows, err := admin.Query("SELECT table_name FROM information_schema.tables WHERE table_schema = 'repro_schema'")
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	if _, err := admin.Exec("USE " + name); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		q := fmt.Sprintf("CREATE TABLE `%s`.`%s` LIKE `repro_schema`.`%s`", name, table, table)
		if _, err := admin.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	isoVar, err := isolationVariable(admin)
	if err != nil {
		t.Fatal(err)
	}
	admin.Close()

	// Session isolation is set explicitly. go-zero runs these statements
	// autocommit, so REPEATABLE-READ is not a snapshot around CpodJob.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4&loc=Local&%s='REPEATABLE-READ'", user, pass, host, port, name, isoVar)
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(20)
	var iso string
	if err := sqlDB.QueryRow("SELECT @@" + isoVar).Scan(&iso); err != nil {
		t.Fatal(err)
	}
	if iso != "REPEATABLE-READ" {
		t.Fatalf("session isolation (%s) = %q, want REPEATABLE-READ", isoVar, iso)
	}
	conn := sqlx.NewMysql(dsn)
	h := &harness{
		t:    t,
		db:   sqlDB,
		name: name,
		svc: &svc.ServiceContext{
			Config: config.Config{
				BannedCpod: map[string]string{},
			},
			DB:               conn,
			UserJobModel:     model.NewSysUserJobModel(conn),
			InferenceModel:   model.NewSysInferenceModel(conn),
			JupyterlabModel:  model.NewSysJupyterlabModel(conn),
			AppModel:         model.NewSysAppModel(conn),
			AppJobModel:      model.NewSysAppJobModel(conn),
			CpodNodeModel:    model.NewSysCpodNodeModel(conn),
			CpodCacheModel:   model.NewSysCpodCacheModel(conn),
			QuotaModel:       model.NewSysQuotaModel(conn),
			UserBalanceModel: model.NewUserBalanceModel(conn),
			UserBillingModel: model.NewUserBillingModel(conn),
			PriceModel:       model.NewSysPriceModel(conn),
		},
	}
	t.Cleanup(func() {
		sqlDB.Close()
		admin, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s:%s)/?parseTime=true", user, pass, host, port))
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
	})
	return h
}

func (h *harness) exec(q string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Exec(q, args...); err != nil {
		h.t.Fatalf("exec %s: %v", q, err)
	}
}

func (h *harness) insertNode(cpod, nodeName, prod string, gpu, cpu int, memBytes int64) int64 {
	h.t.Helper()
	res, err := h.db.Exec(`INSERT INTO sys_cpod_node
		(cpod_id, cpod_version, user_id, node_name, gpu_vendor, gpu_prod, gpu_mem, gpu_total, gpu_allocatable, cpu_total, cpu_allocatable, mem_total, mem_allocatable)
		VALUES (?, 'v1', 'owner', ?, 'nvidia', ?, 0, ?, ?, ?, ?, ?, ?)`,
		cpod, nodeName, prod, gpu, gpu, cpu, cpu, memBytes, memBytes)
	if err != nil {
		h.t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) insertTrain(jobName, user, cpod, gpuType string, gpu int, obtain, work int) {
	h.t.Helper()
	var cpodVal any
	if cpod == "" {
		cpodVal = nil
	} else {
		cpodVal = cpod
	}
	var gpuTypeVal any
	if gpuType == "" {
		gpuTypeVal = ""
	} else {
		gpuTypeVal = gpuType
	}
	h.exec(`INSERT INTO sys_user_job
		(user_id, new_user_id, cpod_id, work_status, obtain_status, billing_status, job_name, gpu_number, gpu_type, json_all, deleted)
		VALUES (1, ?, ?, ?, ?, 1, ?, ?, ?, ?, 0)`,
		user, cpodVal, work, obtain, jobName, gpu, gpuTypeVal, fmt.Sprintf(`{"jobName":"%s"}`, jobName))
}

func (h *harness) staleNode(id int64) {
	h.t.Helper()
	h.exec(`UPDATE sys_cpod_node SET updated_at = DATE_SUB(NOW(), INTERVAL 31 MINUTE) WHERE id = ?`, id)
}

func (h *harness) pull(cpod string) *types.CpodJobResp {
	h.t.Helper()
	resp, err := NewCpodJobLogic(context.Background(), h.svc).CpodJob(&types.CpodJobReq{CpodId: cpod})
	if err != nil {
		h.t.Fatalf("CpodJob %s: %v", cpod, err)
	}
	return resp
}

func trainNames(resp *types.CpodJobResp) []string {
	var out []string
	if resp == nil {
		return out
	}
	for _, m := range resp.JobList {
		if v, ok := m["jobName"].(string); ok {
			out = append(out, v)
		}
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func (h *harness) trainRow(jobName string) (cpod string, obtain, deleted, work int) {
	h.t.Helper()
	var cpodNS sql.NullString
	err := h.db.QueryRow(`SELECT cpod_id, obtain_status, deleted, work_status FROM sys_user_job WHERE job_name = ?`, jobName).
		Scan(&cpodNS, &obtain, &deleted, &work)
	if err != nil {
		h.t.Fatal(err)
	}
	return cpodNS.String, obtain, deleted, work
}

func (h *harness) nodeAlloc(id int64) (gpu, cpu int) {
	h.t.Helper()
	err := h.db.QueryRow(`SELECT gpu_allocatable, cpu_allocatable FROM sys_cpod_node WHERE id = ?`, id).Scan(&gpu, &cpu)
	if err != nil {
		h.t.Fatal(err)
	}
	return gpu, cpu
}

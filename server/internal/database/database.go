package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite" // Pure Go SQLite driver (uses modernc.org/sqlite)
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/model"
)

// DB wraps the GORM DB connection with additional context.
// For SQLite, separate read and write pools are used to avoid contention:
// the write pool has a single connection (SQLite only supports one writer),
// while the read pool has multiple connections for concurrent reads via WAL mode.
type DB struct {
	*gorm.DB // write pool (also used for Migrate/Seed)
	ReadDB   *gorm.DB
	Driver   string
}

// New creates a new database connection based on configuration.
// For SQLite, it creates separate read and write connection pools.
func New(cfg *config.Config) (*DB, error) {
	// Configure logger to only log slow queries (>1 second)
	slowLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second, // Log queries slower than 1 second
			LogLevel:                  logger.Warn, // Only log warnings and errors
			IgnoreRecordNotFoundError: true,        // Don't log "record not found" as error
			Colorful:                  true,
		},
	)

	driver := cfg.DatabaseDriver
	dsn := cfg.CleanDSN()

	switch driver {
	case "postgres":
		gormCfg := &gorm.Config{Logger: slowLogger}
		db, err := gorm.Open(postgres.Open(dsn), gormCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
		}
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(5)
		return &DB{DB: db, Driver: driver}, nil

	case "sqlite":
		return newSQLite(dsn, slowLogger)

	default:
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}
}

// newSQLite creates a DB with separate read and write connection pools.
// The write pool has a single connection (SQLite only supports one writer)
// with _txlock=immediate to acquire the write lock at BEGIN, preventing
// the classic deadlock where two deferred transactions both try to upgrade.
// The read pool has multiple connections for concurrent reads in WAL mode,
// opened with mode=ro and query_only(1) as defense-in-depth against
// accidental writes.
//
// File-based DSNs use the file: URI format so SQLite interprets URI
// parameters like mode=rwc (write pool) and mode=ro (read pool).
func newSQLite(dsn string, dbLogger logger.Interface) (*DB, error) {
	// Normalize: strip file: prefix so we can work with the raw path,
	// then re-add it for file-based databases before opening.
	rawPath := strings.TrimPrefix(dsn, "file:")

	isMemory := rawPath == ":memory:" || strings.HasPrefix(rawPath, ":memory:")

	// Ensure parent directory exists for file-based databases
	if !isMemory {
		dir := filepath.Dir(rawPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
		}
	}

	// Base DSN: file: URI for file-based, plain for :memory:
	baseDSN := rawPath
	if !isMemory {
		baseDSN = "file:" + rawPath
	}

	// Pragmas applied to every connection in both pools via the DSN.
	// Setting them per-DSN ensures every connection opened by the pool
	// gets the same configuration, unlike db.Exec which only affects
	// a single connection.
	basePragmas := []string{
		"_pragma=journal_mode(WAL)",   // concurrent readers + single writer
		"_pragma=busy_timeout(5000)",  // wait up to 5s instead of SQLITE_BUSY
		"_pragma=foreign_keys(1)",     // enforce FK constraints
		"_pragma=synchronous(NORMAL)", // safe with WAL, much faster than FULL
	}

	appendParams := func(base string, params []string) string {
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		return base + sep + strings.Join(params, "&")
	}

	// --- Write pool: single connection, read-write-create, immediate tx lock ---
	writeParams := append(basePragmas, "_txlock=immediate")
	if !isMemory {
		writeParams = append(writeParams, "mode=rwc")
	}
	writeDSN := appendParams(baseDSN, writeParams)
	writeDB, err := gorm.Open(sqlite.Open(writeDSN), &gorm.Config{Logger: dbLogger})
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite write pool: %w", err)
	}
	writeSQLDB, err := writeDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get write pool sql.DB: %w", err)
	}
	writeSQLDB.SetMaxOpenConns(1)
	writeSQLDB.SetMaxIdleConns(1)

	// --- Read pool: multiple connections, read-only ---
	// For in-memory databases, a second Open creates a separate database,
	// so skip the read pool and reuse the write pool.
	if isMemory {
		return &DB{DB: writeDB, Driver: "sqlite"}, nil
	}

	// mode=ro: SQLite opens the connection read-only at the VFS level.
	// query_only(1): additional PRAGMA-level guard that errors on writes.
	readParams := append(basePragmas, "mode=ro", "_pragma=query_only(1)")
	readDSN := appendParams(baseDSN, readParams)
	readDB, err := gorm.Open(sqlite.Open(readDSN), &gorm.Config{Logger: dbLogger})
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite read pool: %w", err)
	}
	readSQLDB, err := readDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get read pool sql.DB: %w", err)
	}
	readSQLDB.SetMaxOpenConns(25)
	readSQLDB.SetMaxIdleConns(4)

	return &DB{DB: writeDB, ReadDB: readDB, Driver: "sqlite"}, nil
}

// migrateModels returns the model list to use for AutoMigrate.
//
// The deprecated agents table is intentionally skipped on SQLite. Older SQLite
// databases may still have foreign keys referencing agents, and asking GORM's
// SQLite migrator to reconcile the Agent schema can trigger a table rebuild
// that attempts to drop `agents`, causing restart-time migration failures.
func (db *DB) migrateModels() []interface{} {
	models := model.AllModels()
	if !db.IsSQLite() {
		return models
	}

	filtered := make([]interface{}, 0, len(models))
	for _, m := range models {
		if _, ok := m.(*model.Agent); ok {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// Migrate runs database migrations using GORM's AutoMigrate
func (db *DB) Migrate() error {
	log.Println("Running GORM AutoMigrate...")

	// First run AutoMigrate to add new columns/tables.
	// On SQLite, skip the deprecated Agent model entirely to avoid unsafe table
	// rebuilds on legacy databases during startup.
	if err := db.AutoMigrate(db.migrateModels()...); err != nil {
		return err
	}

	// Drop obsolete columns that are no longer in the model
	// Note: AutoMigrate only adds columns, it never removes them.
	//
	// SQLite implements DROP COLUMN by rebuilding tables. That rebuild can fail on
	// older Discobot schemas where foreign keys still reference legacy tables or
	// columns (for example sessions.agent_id -> agents.id). Since those obsolete
	// columns are harmless and compatibility is more important than cleanup during
	// startup, skip destructive column cleanup entirely on SQLite.
	if db.IsSQLite() {
		log.Println("Skipping obsolete column cleanup on SQLite for migration compatibility")
		return nil
	}

	migrator := db.Migrator()

	// Drop obsolete Agent columns (removed when simplifying agent configuration)
	obsoleteAgentCols := []string{"name", "description", "system_prompt"}
	var agentColsToDrop []string
	for _, col := range obsoleteAgentCols {
		if migrator.HasColumn(&model.Agent{}, col) {
			agentColsToDrop = append(agentColsToDrop, col)
		}
	}
	for _, col := range agentColsToDrop {
		log.Printf("Dropping obsolete Agent.%s column...\n", col)
		if err := migrator.DropColumn(&model.Agent{}, col); err != nil {
			return fmt.Errorf("failed to drop Agent.%s: %w", col, err)
		}
	}

	// Drop obsolete Workspace columns (commit status moved to session-only tracking)
	obsoleteWorkspaceCols := []string{"commit_status", "commit_error"}
	var workspaceColsToDrop []string
	for _, col := range obsoleteWorkspaceCols {
		if migrator.HasColumn(&model.Workspace{}, col) {
			workspaceColsToDrop = append(workspaceColsToDrop, col)
		}
	}
	for _, col := range workspaceColsToDrop {
		log.Printf("Dropping obsolete Workspace.%s column...\n", col)
		if err := migrator.DropColumn(&model.Workspace{}, col); err != nil {
			return fmt.Errorf("failed to drop Workspace.%s: %w", col, err)
		}
	}

	return nil
}

// Seed creates the anonymous user and default project for no-auth mode.
// This is idempotent - it will not create duplicates if called multiple times.
func (db *DB) Seed() error {
	log.Println("Seeding database with anonymous user and default project...")

	// Create anonymous user if not exists
	anonUser := model.NewAnonymousUser()
	result := db.DB.Where("id = ?", model.AnonymousUserID).FirstOrCreate(anonUser)
	if result.Error != nil {
		return fmt.Errorf("failed to create anonymous user: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		log.Println("Created anonymous user")
	}

	// Create default project if not exists
	defaultProject := model.NewDefaultProject()
	result = db.DB.Where("id = ?", model.DefaultProjectID).FirstOrCreate(defaultProject)
	if result.Error != nil {
		return fmt.Errorf("failed to create default project: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		log.Println("Created default project")
	}

	// Create project membership for anonymous user if not exists
	membership := &model.ProjectMember{
		ProjectID: model.DefaultProjectID,
		UserID:    model.AnonymousUserID,
		Role:      "owner",
	}
	result = db.DB.Where("project_id = ? AND user_id = ?", model.DefaultProjectID, model.AnonymousUserID).FirstOrCreate(membership)
	if result.Error != nil {
		return fmt.Errorf("failed to create project membership: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		log.Println("Added anonymous user to default project")
	}

	log.Println("Database seeding completed")
	return nil
}

// IsPostgres returns true if using PostgreSQL
func (db *DB) IsPostgres() bool {
	return db.Driver == "postgres"
}

// IsSQLite returns true if using SQLite
func (db *DB) IsSQLite() bool {
	return db.Driver == "sqlite"
}

// Close closes both the write and read database connections.
func (db *DB) Close() error {
	writeSQLDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	if err := writeSQLDB.Close(); err != nil {
		return err
	}
	if db.ReadDB != nil {
		readSQLDB, err := db.ReadDB.DB()
		if err != nil {
			return err
		}
		return readSQLDB.Close()
	}
	return nil
}

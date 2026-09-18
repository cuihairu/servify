// dbrecovery 是 P2-3"数据恢复、备份与迁移演练"的运维入口，覆盖 sqlite
// 单机形态的两类资产：
//
//	db-backup     数据库全量逻辑备份（JSONL + manifest，含自增位置）
//	db-restore    从备份恢复数据库（逐文件校验 sha256 后回写）
//	files-backup  上传资产目录打 tar.gz 归档（逐文件 sha256 清单）
//	files-restore 从归档恢复上传资产（字节级校验）
//
// PostgreSQL 生产形态不经过本工具：一致性备份走 pg_dump/pg_restore
// SOP（docs/backup-and-recovery.md）；迁移回滚同样以恢复备份为唯一口径。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"servify/apps/server/internal/platform/recovery"

	appbootstrap "servify/apps/server/internal/app/bootstrap"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// getenvDefault 返回环境变量值，为空时回退默认值。
func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

const usage = `commands:
  db-backup     -out DIR
  db-restore    -backup DIR
  files-backup  -dir UPLOADS -archive FILE [-manifest FILE]
  files-restore -archive FILE -manifest FILE -dest DIR
common flags:   -config FILE -db-driver (sqlite|postgres) -dsn DSN`

// options 是一次调用的全部参数；按 command 取用对应子集。
type options struct {
	command    string
	configFile string
	dbDriver   string
	dsn        string
	outDir     string
	backupDir  string
	dir        string
	archive    string
	manifest   string
	dest       string
}

// parseArgs 解析 "command + flags" 形态的参数；用法问题返回 error，
// 由 main 统一 fatal，保持错误出口单一。
func parseArgs(args []string) (*options, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing command\n%s", usage)
	}
	opts := &options{command: args[0]}
	fs := flag.NewFlagSet("dbrecovery "+opts.command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.configFile, "config", "", "path to config file (default: ./config.yml)")
	fs.StringVar(&opts.dbDriver, "db-driver", getenvDefault("DB_DRIVER", "sqlite"), "database driver (dbrecovery supports sqlite only)")
	fs.StringVar(&opts.dsn, "dsn", os.Getenv("DB_DSN"), "sqlite file path or DSN")
	fs.StringVar(&opts.outDir, "out", "", "backup output directory (db-backup)")
	fs.StringVar(&opts.backupDir, "backup", "", "backup directory to restore from (db-restore)")
	fs.StringVar(&opts.dir, "dir", "", "uploads directory to archive (files-backup)")
	fs.StringVar(&opts.archive, "archive", "", "tar.gz archive path (files-backup / files-restore)")
	fs.StringVar(&opts.manifest, "manifest", "", "files manifest path (default: <archive>.manifest.json)")
	fs.StringVar(&opts.dest, "dest", "", "restore destination directory (files-restore)")
	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}
	return opts, nil
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		log.Fatalf("dbrecovery: %v", err)
	}
	if err := opts.run(); err != nil {
		log.Fatalf("dbrecovery %s failed: %v", opts.command, err)
	}
	log.Printf("dbrecovery %s completed", opts.command)
}

// run 分发子命令；数据库子命令在建立连接前拒绝 pg 方言，避免把
// sqlite 逻辑备份误用在生产形态上。
func (o *options) run() error {
	switch o.command {
	case "db-backup":
		return o.runDB(func(db *gorm.DB) error {
			manifest, err := recovery.BackupDatabase(db, o.outDir)
			if err != nil {
				return err
			}
			log.Printf("db-backup: tables=%d total_rows=%d manifest=%s",
				len(manifest.Tables), manifest.TotalRows, filepath.Join(o.outDir, "manifest.json"))
			return nil
		})
	case "db-restore":
		return o.runDB(func(db *gorm.DB) error {
			manifest, err := recovery.RestoreDatabase(db, o.backupDir)
			if err != nil {
				return err
			}
			log.Printf("db-restore: tables=%d total_rows=%d", len(manifest.Tables), manifest.TotalRows)
			return nil
		})
	case "files-backup":
		return o.runFilesBackup()
	case "files-restore":
		return o.runFilesRestore()
	default:
		return fmt.Errorf("unknown command %q\n%s", o.command, usage)
	}
}

func (o *options) runDB(action func(*gorm.DB) error) error {
	if o.dbDriver != "sqlite" {
		return fmt.Errorf("dbrecovery supports the sqlite deployment form only; for postgres use the pg_dump runbook in docs/backup-and-recovery.md")
	}
	cfg, err := appbootstrap.LoadConfig(o.configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	db, err := appbootstrap.OpenDatabase(cfg, appbootstrap.DatabaseOptions{
		Driver:   o.dbDriver,
		DSN:      o.dsn,
		LogLevel: logger.Silent,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	// 连接归还尽力而为：动作本身失败才是命令的失败口径。
	defer func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	}()
	return action(db)
}

func (o *options) runFilesBackup() error {
	if o.dir == "" || o.archive == "" {
		return fmt.Errorf("files-backup requires -dir and -archive")
	}
	manifest, err := recovery.BackupFiles(o.dir, o.archive)
	if err != nil {
		return err
	}
	manifestPath := o.manifest
	if manifestPath == "" {
		manifestPath = o.archive + ".manifest.json"
	}
	if err := writeFilesManifest(manifestPath, manifest); err != nil {
		return err
	}
	log.Printf("files-backup: files=%d archive=%s manifest=%s", len(manifest.Files), o.archive, manifestPath)
	return nil
}

func (o *options) runFilesRestore() error {
	if o.archive == "" || o.manifest == "" || o.dest == "" {
		return fmt.Errorf("files-restore requires -archive, -manifest and -dest")
	}
	manifest, err := readFilesManifest(o.manifest)
	if err != nil {
		return err
	}
	if _, err := recovery.RestoreFiles(o.archive, manifest, o.dest); err != nil {
		return err
	}
	log.Printf("files-restore: files=%d dest=%s", len(manifest.Files), o.dest)
	return nil
}

// jsonMarshalIndent 是清单序列化的测试 seam：FilesManifest 全由
// JSON 安全类型构成，编码错误在真实数据上不可达，仅测试注入。
var jsonMarshalIndent = json.MarshalIndent

func writeFilesManifest(path string, manifest recovery.FilesManifest) error {
	raw, err := jsonMarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func readFilesManifest(path string) (recovery.FilesManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return recovery.FilesManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest recovery.FilesManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return recovery.FilesManifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return manifest, nil
}

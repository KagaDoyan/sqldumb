package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
}

type BackupService struct {
	dbConfig  DBConfig
	backupDir string
}

func NewBackupService(config DBConfig, backupDir string) *BackupService {
	return &BackupService{
		dbConfig:  config,
		backupDir: backupDir,
	}
}

func (s *BackupService) CreateBackup() (string, error) {
	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(s.backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s_%s.sql", s.dbConfig.Name, timestamp)
	backupPath := filepath.Join(s.backupDir, filename)

	// Construct mysqldump command with explicit network host (for Docker)
	cmd := exec.Command("mysqldump",
		"-h", s.dbConfig.Host,
		"-P", fmt.Sprintf("%d", s.dbConfig.Port),
		"-u", s.dbConfig.User,
		fmt.Sprintf("-p%s", s.dbConfig.Password),
		"--single-transaction",
		"--routines",
		"--triggers",
		"--databases", s.dbConfig.Name,
		"--protocol=TCP", // Force TCP protocol
	)

	// Open output file
	outFile, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer outFile.Close()

	// Set output to our file
	cmd.Stdout = outFile
	cmd.Stderr = os.Stderr

	// Execute the command
	if err := cmd.Run(); err != nil {
		os.Remove(backupPath) // Clean up failed backup file
		return "", fmt.Errorf("backup failed: %w", err)
	}

	return backupPath, nil
}

func (s *BackupService) CleanOldBackups(retentionDays int) error {
	// Get all files in backup directory
	files, err := os.ReadDir(s.backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".sql" {
			filePath := filepath.Join(s.backupDir, file.Name())
			info, err := file.Info()
			if err != nil {
				continue
			}

			if info.ModTime().Before(cutoffTime) {
				if err := os.Remove(filePath); err != nil {
					fmt.Printf("Failed to remove old backup %s: %v\n", filePath, err)
				}
			}
		}
	}

	return nil
}

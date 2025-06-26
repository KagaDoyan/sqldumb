package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v2"
	"github.com/spf13/viper"

	"dbbak/backup"
)

type DBStatus struct {
	Connected bool   `json:"connected"`
	Time      string `json:"time"`
	Error     string `json:"error,omitempty"`
}

type Config struct {
	Database struct {
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		User     string `mapstructure:"user"`
		Password string `mapstructure:"password"`
		Name     string `mapstructure:"name"`
	} `mapstructure:"database"`
	Server struct {
		Port int `mapstructure:"port"`
	} `mapstructure:"server"`
}

type ScheduleOption struct {
	Name string `json:"name"`
	Cron string `json:"cron"`
}

type BackupConfig struct {
	Schedules       string           `json:"schedules"`
	ScheduleOptions []ScheduleOption `json:"schedule_option"`
	ScheduleEnabled bool             `json:"schedule_enabled"`
	BackupPath      string           `json:"backup_path"`
	LastBackup      *time.Time       `json:"last_backup"`
	NextBackup      *time.Time       `json:"next_backup"`
}

var (
	backupScheduler *backup.Scheduler
	backupService   *backup.BackupService
)

func loadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &config, nil
}

func loadBackupConfig() (*BackupConfig, error) {
	data, err := ioutil.ReadFile("backup-config.json")
	if err != nil {
		return nil, fmt.Errorf("error reading backup config: %w", err)
	}

	var config BackupConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error unmarshaling backup config: %w", err)
	}

	return &config, nil
}

func saveBackupConfig(config *BackupConfig) error {
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return fmt.Errorf("error marshaling backup config: %w", err)
	}

	if err := ioutil.WriteFile("backup-config.json", data, 0644); err != nil {
		return fmt.Errorf("error writing backup config: %w", err)
	}

	return nil
}

func updateScheduler(config *BackupConfig) error {
	if config.ScheduleEnabled {
		// Find the selected schedule option
		var cronExpr string
		for _, opt := range config.ScheduleOptions {
			if opt.Name == config.Schedules {
				cronExpr = opt.Cron
				break
			}
		}
		if cronExpr == "" {
			return fmt.Errorf("invalid schedule option")
		}

		// Start the scheduler with the selected cron expression
		if err := backupScheduler.Start(cronExpr); err != nil {
			return err
		}
	} else {
		backupScheduler.Stop()
	}

	return nil
}

func main() {
	// Load configuration
	config, err := loadConfig()
	if err != nil {
		panic(err)
	}

	app := fiber.New()

	// Create MySQL DSN from configuration
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		config.Database.User,
		config.Database.Password,
		config.Database.Host,
		config.Database.Port,
		config.Database.Name,
	)

	// Initialize database connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Initialize backup service and scheduler
	dbConfig := backup.DBConfig{
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		User:     config.Database.User,
		Password: config.Database.Password,
		Name:     config.Database.Name,
	}

	backupConfig, err := loadBackupConfig()
	if err != nil {
		panic(err)
	}

	backupService = backup.NewBackupService(dbConfig, backupConfig.BackupPath)
	backupScheduler = backup.NewScheduler(backupService)

	// Initialize scheduler if enabled
	if backupConfig.ScheduleEnabled {
		if err := updateScheduler(backupConfig); err != nil {
			fmt.Printf("Failed to start scheduler: %v\n", err)
		}
	}

	// Serve static files from the public directory
	app.Static("/", "./public")

	// Create API group
	api := app.Group("/api")

	// Database status endpoint
	api.Get("/db-status", func(c *fiber.Ctx) error {
		status := DBStatus{
			Time: time.Now().Format(time.RFC3339),
		}

		// Check connection
		err := db.Ping()
		if err != nil {
			status.Connected = false
			status.Error = err.Error()
		} else {
			status.Connected = true
		}

		return c.JSON(status)
	})

	// Get backup configuration and status
	api.Get("/backup/config", func(c *fiber.Ctx) error {
		config, err := loadBackupConfig()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": "Failed to load backup configuration",
			})
		}

		// Update status from scheduler
		if backupScheduler != nil {
			running, lastRun, nextRun, _ := backupScheduler.Status()
			config.ScheduleEnabled = running
			if !lastRun.IsZero() {
				config.LastBackup = &lastRun
			}
			if !nextRun.IsZero() {
				config.NextBackup = &nextRun
			}
		}

		return c.JSON(config)
	})

	// Update backup configuration
	api.Put("/backup/config", func(c *fiber.Ctx) error {
		var updateData struct {
			Schedule string `json:"schedule"`
			Enabled  bool   `json:"enabled"`
		}

		if err := c.BodyParser(&updateData); err != nil {
			return c.Status(400).JSON(fiber.Map{
				"error": "Invalid request body",
			})
		}

		config, err := loadBackupConfig()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": "Failed to load backup configuration",
			})
		}

		// Update the configuration
		config.Schedules = updateData.Schedule
		config.ScheduleEnabled = updateData.Enabled

		// Update scheduler
		if err := updateScheduler(config); err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": fmt.Sprintf("Failed to update scheduler: %v", err),
			})
		}

		if err := saveBackupConfig(config); err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": "Failed to save backup configuration",
			})
		}

		return c.JSON(config)
	})

	// Manual backup endpoint
	api.Post("/backup/manual", func(c *fiber.Ctx) error {
		backupPath, err := backupService.CreateBackup()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"error": fmt.Sprintf("Backup failed: %v", err),
			})
		}

		// Update last backup time in config
		config, err := loadBackupConfig()
		if err == nil {
			now := time.Now()
			config.LastBackup = &now
			saveBackupConfig(config)
		}

		return c.JSON(fiber.Map{
			"message":  "Backup created successfully",
			"path":     backupPath,
			"filename": filepath.Base(backupPath),
		})
	})

	// Download backup endpoint
	api.Get("/backup/download/:filename", func(c *fiber.Ctx) error {
		filename := c.Params("filename")
		if filename == "" {
			return c.Status(400).JSON(fiber.Map{
				"error": "Filename is required",
			})
		}

		pwd, _ := os.Getwd()
		filePath := filepath.Join(pwd+"/backups", filename)

		// Verify the file exists and is within the backup directory
		if !strings.HasPrefix(filePath, pwd+"/backups") {
			return c.Status(403).JSON(fiber.Map{
				"error": "Invalid file path",
			})
		}
		return c.Download(filePath)
	})

	// Start server with configured port
	app.Listen(fmt.Sprintf(":%d", config.Server.Port))
}

package backup

import (
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron     *cron.Cron
	service  *BackupService
	mu       sync.Mutex
	jobID    cron.EntryID
	running  bool
	lastRun  time.Time
	nextRun  time.Time
	cronExpr string
}

func NewScheduler(service *BackupService) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		service: service,
	}
}

func (s *Scheduler) Start(cronExpression string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.Stop()
	}

	job := func() {
		backupPath, err := s.service.CreateBackup()
		s.lastRun = time.Now()

		if err != nil {
			fmt.Printf("Scheduled backup failed: %v\n", err)
			return
		}
		fmt.Printf("Scheduled backup created: %s\n", backupPath)
	}

	id, err := s.cron.AddFunc(cronExpression, job)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	s.jobID = id
	s.cronExpr = cronExpression
	s.running = true
	s.cron.Start()

	// Calculate next run time
	schedule, err := cron.ParseStandard(cronExpression)
	if err == nil {
		s.nextRun = schedule.Next(time.Now())
	}

	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.cron.Stop()
		s.cron.Remove(s.jobID)
		s.running = false
		s.nextRun = time.Time{}
	}
}

func (s *Scheduler) Status() (bool, time.Time, time.Time, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.lastRun, s.nextRun, s.cronExpr
}

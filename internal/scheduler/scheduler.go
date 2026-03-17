package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
)

const (
	// schedulerBatchSize is the maximum number of notifications fetched per
	// tick for both scheduled sends and retry re-enqueues.
	schedulerBatchSize = 100
)

// Scheduler periodically scans the database for notifications that are ready
// to be sent (scheduled notifications whose time has arrived) and failed
// notifications that are ready for retry, then enqueues them for worker
// processing.
type Scheduler struct {
	repo     repository.NotificationRepository
	producer queue.Producer
	cfg      config.WorkerConfig
	wg       sync.WaitGroup
}

// NewScheduler creates a Scheduler with the required dependencies.
func NewScheduler(
	repo repository.NotificationRepository,
	producer queue.Producer,
	cfg config.WorkerConfig,
) *Scheduler {
	return &Scheduler{
		repo:     repo,
		producer: producer,
		cfg:      cfg,
	}
}

// Start launches two background goroutines: one for scheduled notification
// dispatch and one for retry re-enqueueing. Both run on the configured
// SchedulerInterval. Start returns immediately; cancel the context to
// initiate graceful shutdown, then call Stop to wait for completion.
func (s *Scheduler) Start(ctx context.Context) {
	s.wg.Add(2)
	go s.runScheduledTicker(ctx)
	go s.runRetryTicker(ctx)
	log.Printf("scheduler started: interval=%s, batch_size=%d", s.cfg.SchedulerInterval, schedulerBatchSize)
}

// Stop blocks until both scheduler goroutines have exited after context
// cancellation.
func (s *Scheduler) Stop() {
	s.wg.Wait()
	log.Println("scheduler stopped")
}

// runScheduledTicker periodically fetches scheduled notifications whose
// scheduled_at time has passed and enqueues them for processing.
func (s *Scheduler) runScheduledTicker(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.cfg.SchedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("scheduler: scheduled ticker shutting down")
			return
		case <-ticker.C:
			if err := s.processScheduledNotifications(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("scheduler: error processing scheduled notifications: %v", err)
			}
		}
	}
}

// runRetryTicker periodically fetches failed notifications whose retry time
// has arrived and re-enqueues them for another delivery attempt.
func (s *Scheduler) runRetryTicker(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.cfg.SchedulerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("scheduler: retry ticker shutting down")
			return
		case <-ticker.C:
			if err := s.processRetryNotifications(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("scheduler: error processing retry notifications: %v", err)
			}
		}
	}
}

// processScheduledNotifications fetches up to schedulerBatchSize scheduled
// notifications that are ready to send and enqueues each one. The database
// query uses FOR UPDATE SKIP LOCKED to allow concurrent schedulers.
func (s *Scheduler) processScheduledNotifications(ctx context.Context) error {
	notifications, err := s.repo.FetchScheduledReady(ctx, schedulerBatchSize)
	if err != nil {
		return fmt.Errorf("fetching scheduled-ready notifications: %w", err)
	}

	if len(notifications) == 0 {
		return nil
	}

	log.Printf("scheduler: enqueuing %d scheduled notifications", len(notifications))

	for _, n := range notifications {
		if err := s.enqueueNotification(ctx, n); err != nil {
			log.Printf("scheduler: failed to enqueue scheduled notification %s: %v", n.ID, err)
			continue
		}
	}

	return nil
}

// processRetryNotifications fetches up to schedulerBatchSize failed
// notifications whose next_retry_at has arrived and that still have retry
// budget remaining, then re-enqueues them.
func (s *Scheduler) processRetryNotifications(ctx context.Context) error {
	notifications, err := s.repo.FetchRetryReady(ctx, schedulerBatchSize)
	if err != nil {
		return fmt.Errorf("fetching retry-ready notifications: %w", err)
	}

	if len(notifications) == 0 {
		return nil
	}

	log.Printf("scheduler: re-enqueuing %d notifications for retry", len(notifications))

	for _, n := range notifications {
		if err := s.enqueueNotification(ctx, n); err != nil {
			log.Printf("scheduler: failed to re-enqueue notification %s for retry: %v", n.ID, err)
			continue
		}
	}

	return nil
}

// enqueueNotification transitions a notification to queued status and
// publishes it to the processing queue.
func (s *Scheduler) enqueueNotification(ctx context.Context, n *entity.Notification) error {
	if err := s.repo.UpdateStatus(ctx, n.ID, entity.StatusQueued, nil, nil); err != nil {
		return fmt.Errorf("updating notification %s to queued: %w", n.ID, err)
	}

	if err := s.producer.Enqueue(ctx, n); err != nil {
		return fmt.Errorf("enqueuing notification %s: %w", n.ID, err)
	}

	return nil
}

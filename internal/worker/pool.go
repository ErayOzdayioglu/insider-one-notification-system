package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/queue"
)

// WorkerPool manages a set of concurrent worker goroutines that consume
// notifications from all channel/priority queue combinations and process
// them through the Processor.
type WorkerPool struct {
	consumer  queue.Consumer
	processor *Processor
	cfg       config.WorkerConfig
	wg        sync.WaitGroup
}

// NewWorkerPool creates a pool that will spawn workers for every
// channel x priority combination.
func NewWorkerPool(
	consumer queue.Consumer,
	processor *Processor,
	cfg config.WorkerConfig,
) *WorkerPool {
	return &WorkerPool{
		consumer:  consumer,
		processor: processor,
		cfg:       cfg,
	}
}

// Start launches worker goroutines for each channel/priority combination.
// Each combination gets cfg.ConcurrencyPerChannel workers, resulting in
// 3 channels * 3 priorities * concurrency = total goroutines.
// Start returns immediately; call Stop or cancel the context to shut down.
func (wp *WorkerPool) Start(ctx context.Context) {
	channels := entity.AllChannels()
	priorities := entity.AllPriorities()

	for _, ch := range channels {
		for _, pri := range priorities {
			for i := 0; i < wp.cfg.ConcurrencyPerChannel; i++ {
				wp.wg.Add(1)
				go wp.runWorker(ctx, ch, pri, i)
			}
		}
	}

	log.Printf("worker pool started: %d channels x %d priorities x %d concurrency = %d workers",
		len(channels), len(priorities), wp.cfg.ConcurrencyPerChannel,
		len(channels)*len(priorities)*wp.cfg.ConcurrencyPerChannel,
	)
}

// Stop blocks until all worker goroutines have finished draining their
// in-flight work after the context has been cancelled.
func (wp *WorkerPool) Stop() {
	wp.wg.Wait()
	log.Println("worker pool stopped: all workers drained")
}

// runWorker is the main loop for a single worker goroutine. It continuously
// dequeues messages from its assigned channel/priority stream and processes
// them until the context is cancelled.
func (wp *WorkerPool) runWorker(ctx context.Context, ch entity.Channel, pri entity.Priority, workerID int) {
	defer wp.wg.Done()

	workerName := fmt.Sprintf("worker-%s-%s-%d", ch, pri, workerID)
	log.Printf("%s: started", workerName)

	for {
		select {
		case <-ctx.Done():
			log.Printf("%s: shutting down", workerName)
			return
		default:
		}

		messages, err := wp.consumer.Dequeue(ctx, ch, pri, 1)
		if err != nil {
			// Context cancellation during dequeue is expected during shutdown.
			if ctx.Err() != nil {
				log.Printf("%s: shutting down", workerName)
				return
			}
			log.Printf("%s: dequeue error: %v", workerName, err)
			wp.backoff(ctx)
			continue
		}

		if len(messages) == 0 {
			// Queue is empty; wait before polling again to avoid busy-spin.
			wp.backoff(ctx)
			continue
		}

		for _, msg := range messages {
			if err := wp.processor.Process(ctx, msg); err != nil {
				log.Printf("%s: process error for notification %s: %v",
					workerName, msg.Notification.ID, err)
			}
		}
	}
}

// backoff sleeps for the configured poll interval or returns early if the
// context is cancelled.
func (wp *WorkerPool) backoff(ctx context.Context) {
	timer := time.NewTimer(wp.cfg.PollInterval)
	select {
	case <-ctx.Done():
		timer.Stop()
	case <-timer.C:
	}
}

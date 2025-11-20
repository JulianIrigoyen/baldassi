package worker

import (
	"context"
	"sync"
)

// Job represents a unit of work to be executed by the pool
type Job interface {
	Execute(ctx context.Context) error
}

// Result wraps the outcome of a job execution
type Result struct {
	ID    int
	Data  interface{}
	Error error
}

// Pool manages a pool of workers for concurrent job execution
type Pool struct {
	workers    int
	jobQueue   chan Job
	resultChan chan Result
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewPool creates a new worker pool with the specified number of workers
func NewPool(numWorkers int) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		workers:    numWorkers,
		jobQueue:   make(chan Job, numWorkers*2), // Buffer size = 2x workers
		resultChan: make(chan Result, numWorkers*2),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start initializes and starts all workers in the pool
func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// worker is the main loop for each worker goroutine
func (p *Pool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case job, ok := <-p.jobQueue:
			if !ok {
				// Channel closed, worker should exit
				return
			}

			// Execute the job
			err := job.Execute(p.ctx)

			// Send result (job should handle its own result reporting)
			_ = err // Jobs handle their own error reporting

		case <-p.ctx.Done():
			// Context cancelled, worker should exit
			return
		}
	}
}

// Submit adds a new job to the work queue
func (p *Pool) Submit(job Job) {
	select {
	case p.jobQueue <- job:
		// Job submitted successfully
	case <-p.ctx.Done():
		// Pool is shutting down, don't accept new jobs
		return
	}
}

// SubmitBatch submits multiple jobs at once
func (p *Pool) SubmitBatch(jobs []Job) {
	for _, job := range jobs {
		p.Submit(job)
	}
}

// Results returns the result channel for consumers to read from
func (p *Pool) Results() <-chan Result {
	return p.resultChan
}

// Stop gracefully shuts down the worker pool
func (p *Pool) Stop() {
	// Stop accepting new jobs
	close(p.jobQueue)

	// Wait for all workers to finish current jobs
	p.wg.Wait()

	// Cancel context and close result channel
	p.cancel()
	close(p.resultChan)
}

// StopImmediate immediately stops all workers without waiting
func (p *Pool) StopImmediate() {
	// Cancel context to signal all workers to stop
	p.cancel()

	// Close channels
	close(p.jobQueue)
	close(p.resultChan)
}

// WorkerCount returns the number of workers in the pool
func (p *Pool) WorkerCount() int {
	return p.workers
}

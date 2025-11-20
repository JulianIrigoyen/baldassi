package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJob is a simple job implementation for testing
type TestJob struct {
	ID       int
	Duration time.Duration
	counter  *int32
	wg       *sync.WaitGroup
}

func (t *TestJob) Execute(ctx context.Context) error {
	atomic.AddInt32(t.counter, 1)
	time.Sleep(t.Duration)
	if t.wg != nil {
		t.wg.Done()
	}
	return nil
}

func TestPoolCreation(t *testing.T) {
	pool := NewPool(3)
	assert.NotNil(t, pool)
	assert.Equal(t, 3, pool.WorkerCount())
}

func TestPoolStartStop(t *testing.T) {
	pool := NewPool(2)

	// Start the pool
	pool.Start()

	// Submit a simple job
	var counter int32
	job := &TestJob{
		ID:       1,
		Duration: 10 * time.Millisecond,
		counter:  &counter,
	}

	pool.Submit(job)

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	// Stop the pool
	pool.Stop()

	// Verify job was executed
	assert.Equal(t, int32(1), atomic.LoadInt32(&counter))
}

func TestPoolConcurrentJobs(t *testing.T) {
	pool := NewPool(3)
	pool.Start()
	defer pool.Stop()

	var counter int32
	var wg sync.WaitGroup

	// Submit 10 jobs
	numJobs := 10
	wg.Add(numJobs)

	startTime := time.Now()

	for i := 0; i < numJobs; i++ {
		job := &TestJob{
			ID:       i,
			Duration: 100 * time.Millisecond,
			counter:  &counter,
			wg:       &wg,
		}
		pool.Submit(job)
	}

	// Wait for all jobs to complete
	wg.Wait()

	duration := time.Since(startTime)

	// With 3 workers and 10 jobs of 100ms each:
	// Sequential would take 1000ms
	// With 3 workers should take ~400ms (10 jobs / 3 workers = 3.33 rounds)
	assert.Less(t, duration.Milliseconds(), int64(500), "Jobs should complete faster with parallelism")
	assert.Equal(t, int32(numJobs), atomic.LoadInt32(&counter), "All jobs should have executed")
}

func TestPoolBatchSubmit(t *testing.T) {
	pool := NewPool(2)
	pool.Start()
	defer pool.Stop()

	var counter int32
	var wg sync.WaitGroup

	// Create batch of jobs
	jobs := make([]Job, 5)
	wg.Add(5)

	for i := 0; i < 5; i++ {
		jobs[i] = &TestJob{
			ID:       i,
			Duration: 10 * time.Millisecond,
			counter:  &counter,
			wg:       &wg,
		}
	}

	// Submit batch
	pool.SubmitBatch(jobs)

	// Wait for completion
	wg.Wait()

	assert.Equal(t, int32(5), atomic.LoadInt32(&counter), "All batch jobs should have executed")
}

func TestPoolStopImmediate(t *testing.T) {
	pool := NewPool(2)
	pool.Start()

	var counter int32

	// Submit long-running jobs
	for i := 0; i < 10; i++ {
		job := &TestJob{
			ID:       i,
			Duration: 1 * time.Second,
			counter:  &counter,
		}
		pool.Submit(job)
	}

	// Give workers time to start processing
	time.Sleep(50 * time.Millisecond)

	// Stop immediately
	pool.StopImmediate()

	// Counter should be less than 10 since we stopped immediately
	finalCount := atomic.LoadInt32(&counter)
	assert.Less(t, finalCount, int32(10), "Not all jobs should have completed with immediate stop")
}

func TestPoolPerformance(t *testing.T) {
	// Compare sequential vs parallel execution
	numJobs := 20
	jobDuration := 50 * time.Millisecond

	// Sequential execution
	sequentialStart := time.Now()
	var counter int32
	for i := 0; i < numJobs; i++ {
		job := &TestJob{
			ID:       i,
			Duration: jobDuration,
			counter:  &counter,
		}
		job.Execute(context.Background())
	}
	sequentialDuration := time.Since(sequentialStart)

	// Parallel execution with 4 workers
	pool := NewPool(4)
	pool.Start()
	defer pool.Stop()

	var wg sync.WaitGroup
	wg.Add(numJobs)
	counter = 0

	parallelStart := time.Now()
	for i := 0; i < numJobs; i++ {
		job := &TestJob{
			ID:       i,
			Duration: jobDuration,
			counter:  &counter,
			wg:       &wg,
		}
		pool.Submit(job)
	}
	wg.Wait()
	parallelDuration := time.Since(parallelStart)

	// Parallel should be significantly faster
	speedup := float64(sequentialDuration) / float64(parallelDuration)
	assert.Greater(t, speedup, 2.0, "Parallel execution should be at least 2x faster")

	t.Logf("Sequential: %v, Parallel: %v, Speedup: %.2fx",
		sequentialDuration, parallelDuration, speedup)
}

// BenchmarkPoolSubmit benchmarks job submission
func BenchmarkPoolSubmit(b *testing.B) {
	pool := NewPool(4)
	pool.Start()
	defer pool.Stop()

	var counter int32

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job := &TestJob{
			ID:       i,
			Duration: 0,
			counter:  &counter,
		}
		pool.Submit(job)
	}
}

// BenchmarkPoolThroughput benchmarks overall throughput
func BenchmarkPoolThroughput(b *testing.B) {
	pool := NewPool(4)
	pool.Start()
	defer pool.Stop()

	var counter int32
	var wg sync.WaitGroup
	wg.Add(b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job := &TestJob{
			ID:       i,
			Duration: 1 * time.Microsecond,
			counter:  &counter,
			wg:       &wg,
		}
		pool.Submit(job)
	}
	wg.Wait()

	require.Equal(b, int32(b.N), atomic.LoadInt32(&counter))
}

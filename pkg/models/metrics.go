package models

import (
	"sync/atomic"
)

// NewMetrics creates a new Metrics instance
func NewMetrics() *Metrics {
	return &Metrics{}
}

// IncrementConnections increments the active connections counter
func (m *Metrics) IncrementConnections() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.ActiveConnections++
}

// DecrementConnections decrements the active connections counter
func (m *Metrics) DecrementConnections() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.ActiveConnections--
}

// AddBytes adds to the total bytes transferred
func (m *Metrics) AddBytes(n int64) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.BytesTransferred += n
}

// IncrementRequests increments the request counter
func (m *Metrics) IncrementRequests() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.RequestCount++
}

// IncrementErrors increments the error counter
func (m *Metrics) IncrementErrors() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Errors++
}

// RecordPrefetchHit records a successful prefetch hit
func (m *Metrics) RecordPrefetchHit() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.PrefetchHits++
}

// RecordPrefetchMiss records a prefetch miss
func (m *Metrics) RecordPrefetchMiss() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.PrefetchMisses++
}

// GetStats returns the current metrics
func (m *Metrics) GetStats() map[string]int64 {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	return map[string]int64{
		"activeConnections": m.ActiveConnections,
		"bytesTransferred":  m.BytesTransferred,
		"requestCount":      m.RequestCount,
		"errors":            m.Errors,
		"prefetchHits":      m.PrefetchHits,
		"prefetchMisses":    m.PrefetchMisses,
	}
}

type StreamingMetrics struct {
	BytesServed        int64
	ChunksServed       int64
	AveragePrefetchGap float64
	BufferUnderruns    int64
}

func (m *StreamingMetrics) RecordChunk(size int64, gap float64) {
	atomic.AddInt64(&m.BytesServed, size)
	atomic.AddInt64(&m.ChunksServed, 1)
	m.AveragePrefetchGap = (m.AveragePrefetchGap*float64(m.ChunksServed-1) + gap) / float64(m.ChunksServed)
}

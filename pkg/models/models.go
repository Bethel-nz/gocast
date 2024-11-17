package models

import (
	"crypto/sha256"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// VideoBuffer represents a prefetched segment of video
type VideoBuffer struct {
	Data        []byte
	Start       int64
	End         int64
	LastAccess  time.Time
	Mu          sync.RWMutex
	Prefetching bool
}

// Metrics tracks server statistics
type Metrics struct {
	ActiveConnections int64
	BytesTransferred  int64
	RequestCount      int64
	Errors            int64
	PrefetchHits      int64
	PrefetchMisses    int64
	Mu                sync.RWMutex
}

// Connection represents a client connection with rate limiting
type Connection struct {
	Conn       net.Conn
	Limiter    *rate.Limiter
	CreatedAt  time.Time
	LastActive time.Time
	Speed      float64
	SpeedMu    sync.RWMutex
}

// VideoFile represents a video file in the system
type VideoFile struct {
	VideoID      string
	Name         string
	DisplayName  string
	Title        string
	Size         int64
	LastModified time.Time
}

// VideoStore manages video mappings and lookups
type VideoStore struct {
	videos map[string]VideoFile
	mu     sync.RWMutex
}

func NewVideoStore() *VideoStore {
	return &VideoStore{
		videos: make(map[string]VideoFile),
	}
}

// GenerateID creates a unique ID for a video file
func (vs *VideoStore) GenerateID(name string) string {
	hash := sha256.Sum256([]byte(name))
	return fmt.Sprintf("%x", hash)[:8] // Use first 8 characters of hash
}

// CleanDisplayName removes unnecessary characters and file extension
func (vs *VideoStore) CleanDisplayName(filename string) string {
	// Remove extension
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	name = strings.NewReplacer(
		".", " ",
		"_", " ",
		"-", " ",
		"[", " ",
		"]", " ",
		"(", " ",
		")", " ",
	).Replace(name)

	name = strings.Join(strings.Fields(name), " ")

	return name
}

func (vs *VideoStore) AddVideo(file VideoFile) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.videos[file.VideoID] = file
}

// GetVideo retrieves a video by ID
func (vs *VideoStore) GetVideo(id string) (VideoFile, bool) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	video, exists := vs.videos[id]
	return video, exists
}

// GetAllVideos returns all videos
func (vs *VideoStore) GetAllVideos() []VideoFile {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	videos := make([]VideoFile, 0, len(vs.videos))
	for _, v := range vs.videos {
		videos = append(videos, v)
	}
	return videos
}

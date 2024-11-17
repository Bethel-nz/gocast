package server

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/notkyloren/gocast/pkg/models"
	"github.com/notkyloren/gocast/pkg/templates"
	"golang.org/x/time/rate"
)

// VideoServer represents the main server structure
type VideoServer struct {
	Listener    net.Listener
	Wg          sync.WaitGroup
	Ctx         context.Context
	Cancel      context.CancelFunc
	Metrics     *models.Metrics
	Connections sync.Map
	Buffers     map[string]*models.VideoBuffer
	BuffersMu   sync.RWMutex
	ConnLimit   chan struct{}
	Template    *template.Template
	Config      *Config
	VideoStore  *models.VideoStore
}

// Config holds server configuration
type Config struct {
	VideoDir          string
	Port              string
	ChunkSize         int64
	PrefetchSize      int64
	PrefetchThreshold float64
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	MaxConns          int
	CleanupInterval   time.Duration
	ThumbnailDir      string
	ThumbnailQuality  int
	ThumbnailWidth    int
}

// DefaultConfig returns default server configuration
func DefaultConfig() *Config {
	return &Config{
		VideoDir:          "./videos",
		Port:              "0.0.0.0:4221",
		ChunkSize:         1024 * 64,
		PrefetchSize:      10 * 1024 * 1024,
		PrefetchThreshold: 0.7,
		ReadTimeout:       time.Second * 30,
		WriteTimeout:      time.Second * 30,
		MaxConns:          100,
		CleanupInterval:   time.Minute * 5,
		ThumbnailDir:      "./thumbnails",
		ThumbnailQuality:  75,
		ThumbnailWidth:    480,
	}
}

func New(config *Config) *VideoServer {
	ctx, cancel := context.WithCancel(context.Background())

	// Create required directories
	if err := ensureDirectories(config); err != nil {
		log.Printf("Error creating directories: %v", err)
	}

	tmpl := template.Must(template.New("videoList").Funcs(template.FuncMap{
		"BytesToHuman": func(b int64) string {
			const unit = 1024
			if b < unit {
				return fmt.Sprintf("%d B", b)
			}
			div, exp := int64(unit), 0
			for n := b / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
		},
		"FormatTime": func(t time.Time) string {
			return t.Format("Jan 02, 2006 15:04:05")
		},
	}).ParseFS(templates.GetTemplatesFS(), "templates/*.html"))

	return &VideoServer{
		Ctx:        ctx,
		Cancel:     cancel,
		Metrics:    &models.Metrics{},
		Buffers:    make(map[string]*models.VideoBuffer),
		ConnLimit:  make(chan struct{}, config.MaxConns),
		Template:   tmpl,
		Config:     config,
		VideoStore: models.NewVideoStore(),
	}
}

func (s *VideoServer) Start() error {
	var err error
	s.Listener, err = net.Listen("tcp", s.Config.Port)
	if err != nil {
		return fmt.Errorf("failed to start server: %v", err)
	}

	log.Printf("Server running on %s\n", s.Config.Port)

	go s.cleanBuffers()
	go s.acceptConnections()

	return nil
}

func (s *VideoServer) Stop() {
	s.Cancel()
	if s.Listener != nil {
		s.Listener.Close()
	}
	s.Wg.Wait()
}

func (s *VideoServer) acceptConnections() {
	defer s.Wg.Done()

	for {
		select {
		case <-s.Ctx.Done():
			return
		case s.ConnLimit <- struct{}{}:
			conn, err := s.Listener.Accept()
			if err != nil {
				if !strings.Contains(err.Error(), "use of closed network connection") {
					log.Printf("Error accepting connection: %v", err)
				}
				<-s.ConnLimit
				continue
			}

			s.Wg.Add(1)
			go func() {
				defer func() {
					conn.Close()
					<-s.ConnLimit
					s.Wg.Done()
				}()

				connection := &models.Connection{
					Conn:      conn,
					Limiter:   rate.NewLimiter(rate.Limit(1024*1024), 1024*1024),
					CreatedAt: time.Now(),
				}
				s.handleConnection(connection)
			}()
		}
	}
}

// ensureDirectories creates necessary directories if they don't exist
func ensureDirectories(config *Config) error {
	dirs := []string{
		config.VideoDir,
		config.ThumbnailDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	return nil
}

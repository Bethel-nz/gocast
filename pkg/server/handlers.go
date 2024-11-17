package server

import (
	"bufio"
	"fmt"
	"log"
	"mime"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/notkyloren/gocast/pkg/models"
	"golang.org/x/time/rate"
)

var supportedFormats = map[string]bool{
	".mp4":  true,
	".webm": true,
	".mov":  true,
	".mkv":  true,
	".avi":  true,
	".flv":  true,
	".wmv":  true,
	".m4v":  true,
	".3gp":  true,
	".ts":   true, // MPEG transport stream
	".mts":  true, // AVCHD
	".m2ts": true, // Blu-ray BDAV
}

// Add this struct for the watch page template data
type WatchTemplateData struct {
	Title        string
	VideoID      string
	Size         int64
	LastModified time.Time
}

func (s *VideoServer) handleConnection(conn *models.Connection) {
	reader := bufio.NewReader(conn.Conn)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Failed to read request: %v", err)
		s.writeError(conn.Conn, 400, "Bad Request")
		s.Metrics.IncrementErrors()
		return
	}

	s.Metrics.IncrementRequests()
	conn.LastActive = time.Now()

	// Parse HTTP headers
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil || line == "\r\n" || line == "\n" {
			break
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	// Parse request line
	parts := strings.Split(strings.TrimSpace(requestLine), " ")
	if len(parts) != 3 {
		s.writeError(conn.Conn, 400, "Bad Request")
		s.Metrics.IncrementErrors()
		return
	}

	method, path, _ := parts[0], parts[1], parts[2]

	switch {
	case method == "GET" && path == "/":
		s.serveVideoList(conn.Conn)
	case method == "GET" && strings.HasPrefix(path, "/videos/"):
		videoID := filepath.Base(path)
		if video, exists := s.VideoStore.GetVideo(videoID); exists {
			videoFile := filepath.Join(s.Config.VideoDir, video.Name)
			s.serveVideo(conn, videoFile, headers)
		} else {
			s.writeError(conn.Conn, 404, "Video Not Found")
			s.Metrics.IncrementErrors()
		}
	case method == "GET" && strings.HasPrefix(path, "/watch/"):
		videoID := filepath.Base(path)
		s.serveWatchPage(conn.Conn, videoID)
	case method == "GET" && strings.HasPrefix(path, "/thumbnails/"):
		s.handleThumbnail(conn.Conn, path)
	default:
		s.writeError(conn.Conn, 404, "Not Found")
		s.Metrics.IncrementErrors()
	}
}

func (s *VideoServer) serveVideo(conn *models.Connection, path string, headers map[string]string) {
	file, err := os.Open(path)
	if err != nil {
		s.writeError(conn.Conn, 404, "Video Not Found")
		s.Metrics.IncrementErrors()
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		s.writeError(conn.Conn, 500, "Internal Server Error")
		s.Metrics.IncrementErrors()
		return
	}

	rangeHeader := headers["Range"]
	start, end := int64(0), fileInfo.Size()-1

	if rangeHeader != "" {
		if strings.HasPrefix(rangeHeader, "bytes=") {
			rangeHeader = strings.TrimPrefix(rangeHeader, "bytes=")
			parts := strings.Split(rangeHeader, "-")
			if len(parts) == 2 {
				start, _ = strconv.ParseInt(parts[0], 10, 64)
				if parts[1] != "" {
					end, _ = strconv.ParseInt(parts[1], 10, 64)
				}
			}
		}
	}

	// Write headers
	contentLength := end - start + 1
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if rangeHeader != "" {
		conn.Conn.Write([]byte("HTTP/1.1 206 Partial Content\r\n"))
		conn.Conn.Write([]byte(fmt.Sprintf("Content-Range: bytes %d-%d/%d\r\n", start, end, fileInfo.Size())))
	} else {
		conn.Conn.Write([]byte("HTTP/1.1 200 OK\r\n"))
	}

	conn.Conn.Write([]byte(fmt.Sprintf("Content-Length: %d\r\n", contentLength)))
	conn.Conn.Write([]byte(fmt.Sprintf("Content-Type: %s\r\n", contentType)))
	conn.Conn.Write([]byte("Accept-Ranges: bytes\r\n"))
	conn.Conn.Write([]byte("\r\n"))

	// Seek to start position
	file.Seek(start, 0)

	// Start streaming
	s.streamVideo(conn, file, start, end)
}

func (s *VideoServer) serveVideoList(conn net.Conn) {
	videos, err := s.scanVideos()
	if err != nil {
		s.writeError(conn, 500, "Internal Server Error")
		s.Metrics.IncrementErrors()
		return
	}

	conn.Write([]byte("HTTP/1.1 200 OK\r\n"))
	conn.Write([]byte("Content-Type: text/html\r\n"))
	conn.Write([]byte("\r\n"))

	err = s.Template.ExecuteTemplate(conn, "video_list.html", struct {
		Videos []models.VideoFile
	}{
		Videos: videos,
	})
	if err != nil {
		log.Printf("Error executing template: %v", err)
		s.Metrics.IncrementErrors()
	}
}

func (s *VideoServer) writeError(conn net.Conn, status int, message string) {
	conn.Write([]byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, message)))
	conn.Write([]byte("Content-Type: text/plain\r\n\r\n"))
	conn.Write([]byte(message))
}

func (s *VideoServer) scanVideos() ([]models.VideoFile, error) {
	err := filepath.Walk(s.Config.VideoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if supportedFormats[ext] {
				name := filepath.Base(path)
				displayName := s.VideoStore.CleanDisplayName(name)
				video := models.VideoFile{
					Name:         name,
					DisplayName:  displayName,
					Title:        displayName,
					Size:         info.Size(),
					LastModified: info.ModTime(),
				}
				video.VideoID = s.VideoStore.GenerateID(name)

				// Generate thumbnail for the video
				thumbnailPath := filepath.Join(s.Config.ThumbnailDir, video.VideoID+".jpg")
				if _, err := os.Stat(thumbnailPath); os.IsNotExist(err) {
					if err := s.generateVideoThumbnail(path, thumbnailPath); err != nil {
						log.Printf("Error generating thumbnail for %s: %v", name, err)
					}
				}

				s.VideoStore.AddVideo(video)
			}
		}
		return nil
	})
	return s.VideoStore.GetAllVideos(), err
}

func (s *VideoServer) generateVideoThumbnail(videoPath, thumbnailPath string) error {
	cmd := exec.Command("ffmpeg",
		"-i", videoPath, // Input file
		"-ss", "00:00:01", // Seek to 1 second
		"-vframes", "1", // Extract 1 frame
		"-vf", fmt.Sprintf("scale=%d:-1", s.Config.ThumbnailWidth), // Scale width, maintain aspect ratio
		"-q:v", strconv.Itoa(s.Config.ThumbnailQuality), // JPEG quality (1-31, lower is better)
		"-y",          // Overwrite output file
		thumbnailPath, // Output file
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg error: %v, output: %s", err, string(output))
	}

	return nil
}

func (s *VideoServer) serveWatchPage(conn net.Conn, videoID string) {
	video, exists := s.VideoStore.GetVideo(videoID)
	if !exists {
		s.writeError(conn, 404, "Video Not Found")
		s.Metrics.IncrementErrors()
		return
	}

	data := WatchTemplateData{
		Title:        video.DisplayName,
		VideoID:      video.VideoID,
		Size:         video.Size,
		LastModified: video.LastModified,
	}

	conn.Write([]byte("HTTP/1.1 200 OK\r\n"))
	conn.Write([]byte("Content-Type: text/html\r\n"))
	conn.Write([]byte("\r\n"))

	err := s.Template.ExecuteTemplate(conn, "watch.html", data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		s.Metrics.IncrementErrors()
	}
}

func (s *VideoServer) handleThumbnail(conn net.Conn, path string) {
	videoID := filepath.Base(path)

	// Path to thumbnail cache
	thumbnailPath := filepath.Join(s.Config.ThumbnailDir, videoID+".jpg")
	videoPath := filepath.Join(s.Config.VideoDir, videoID)

	// Check if thumbnail already exists
	if _, err := os.Stat(thumbnailPath); os.IsNotExist(err) {
		// Generate thumbnail using ffmpeg
		err = generateThumbnail(videoPath, thumbnailPath)
		if err != nil {
			s.writeError(conn, 500, "Internal Server Error")
			s.Metrics.IncrementErrors()
			return
		}
	}

	thumbnailFile, err := os.Open(thumbnailPath)
	if err != nil {
		s.writeError(conn, 500, "Internal Server Error")
		s.Metrics.IncrementErrors()
		return
	}
	defer thumbnailFile.Close()

	thumbnailFileInfo, err := thumbnailFile.Stat()
	if err != nil {
		s.writeError(conn, 500, "Internal Server Error")
		s.Metrics.IncrementErrors()
		return
	}

	// Write headers
	conn.Write([]byte("HTTP/1.1 200 OK\r\n"))
	conn.Write([]byte("Content-Type: image/jpeg\r\n"))
	conn.Write([]byte(fmt.Sprintf("Content-Length: %d\r\n", thumbnailFileInfo.Size())))
	conn.Write([]byte("\r\n"))

	// Create a models.Connection with default rate limiter for thumbnail
	thumbnailConn := &models.Connection{
		Conn:       conn,
		Limiter:    rate.NewLimiter(rate.Limit(1024*1024), 1024*1024), // 1MB/s limit for thumbnails
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}

	// Start streaming
	s.streamVideo(thumbnailConn, thumbnailFile, 0, thumbnailFileInfo.Size()-1)
}

func generateThumbnail(videoPath, thumbnailPath string) error {
	// Use ffmpeg to generate thumbnail from the middle of the video
	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-ss", "00:00:01", // Take frame from 1 second in
		"-vframes", "1",
		"-vf", "scale=480:-1", // Scale width to 480px, maintain aspect ratio
		"-f", "image2",
		thumbnailPath,
	)

	return cmd.Run()
}

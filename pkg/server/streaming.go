package server

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"ren.local/gocast/pkg/models"
)

func (s *VideoServer) streamVideo(conn *models.Connection, file *os.File, start, end int64) {
	buffer := make([]byte, s.Config.ChunkSize)
	bytesRemaining := end - start + 1
	currentPos := start

	if tcpConn, ok := conn.Conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	for bytesRemaining > 0 {
		select {
		case <-s.Ctx.Done():
			return
		default:
			s.BuffersMu.RLock()
			buf, exists := s.Buffers[conn.Conn.RemoteAddr().String()]
			s.BuffersMu.RUnlock()

			var bytesWritten int
			var err error

			if exists && currentPos >= buf.Start && currentPos < buf.End {
				offset := currentPos - buf.Start
				toWrite := min(buf.End-currentPos, bytesRemaining)

				if offset < 0 || offset+toWrite > int64(len(buf.Data)) {
					exists = false
				} else {
					conn.Conn.SetWriteDeadline(time.Now().Add(s.Config.WriteTimeout))
					bytesWritten, err = conn.Conn.Write(buf.Data[offset : offset+toWrite])
					if err == nil {
						s.Metrics.RecordPrefetchHit()
						currentPos += int64(bytesWritten)
					}
				}
			}

			if !exists || err != nil {
				s.Metrics.RecordPrefetchMiss()

				n := min(int64(len(buffer)), bytesRemaining)
				bytesRead, err := file.Read(buffer[:n])
				if err != nil {
					if err != io.EOF {
						if !isConnectionClosed(err) {
							log.Printf("Error reading file: %v", err)
						}
					}
					return
				}

				err = conn.Limiter.WaitN(s.Ctx, bytesRead)
				if err != nil {
					if err != context.Canceled {
						log.Printf("Rate limiting error: %v", err)
					}
					return
				}

				conn.Conn.SetWriteDeadline(time.Now().Add(s.Config.WriteTimeout))
				bytesWritten, err = conn.Conn.Write(buffer[:bytesRead])
				if err == nil {
					currentPos += int64(bytesWritten)
				}
			}

			if err != nil {
				if !isConnectionClosed(err) {
					log.Printf("Error writing to connection: %v", err)
				}
				return
			}

			bytesRemaining -= int64(bytesWritten)
			s.Metrics.AddBytes(int64(bytesWritten))
			conn.LastActive = time.Now()

			if !exists || (currentPos >= buf.End && !buf.Prefetching) {
				go s.prefetchNextSegment(conn, file, currentPos)
			}
		}
	}
}

// Helper function to check if an error is due to connection closure
func isConnectionClosed(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection was forcibly closed") ||
		strings.Contains(errStr, "connection was aborted") ||
		strings.Contains(errStr, "use of closed network connection")
}

func (s *VideoServer) prefetchNextSegment(conn *models.Connection, originalFile *os.File, start int64) {
	fileName := originalFile.Name()
	newFile, err := os.Open(fileName)
	if err != nil {
		if !isConnectionClosed(err) {
			log.Printf("Error opening file for prefetch: %v", err)
		}
		return
	}
	defer newFile.Close()

	s.BuffersMu.Lock()
	buf, exists := s.Buffers[conn.Conn.RemoteAddr().String()]
	if exists && buf.Prefetching {
		s.BuffersMu.Unlock()
		return
	}

	if !exists {
		buf = &models.VideoBuffer{
			Data:        make([]byte, s.Config.PrefetchSize),
			Start:       start,
			LastAccess:  time.Now(),
			Prefetching: true,
		}
		s.Buffers[conn.Conn.RemoteAddr().String()] = buf
	} else {
		buf.Prefetching = true
		buf.Start = start
	}
	s.BuffersMu.Unlock()

	_, err = newFile.Seek(start, 0)
	if err != nil {
		if !isConnectionClosed(err) {
			log.Printf("Error seeking in file for prefetch: %v", err)
		}
		s.BuffersMu.Lock()
		buf.Prefetching = false
		s.BuffersMu.Unlock()
		return
	}

	n, err := io.ReadFull(newFile, buf.Data)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		if !isConnectionClosed(err) {
			log.Printf("Error prefetching: %v", err)
		}
		s.BuffersMu.Lock()
		buf.Prefetching = false
		s.BuffersMu.Unlock()
		return
	}

	s.BuffersMu.Lock()
	buf.End = start + int64(n)
	buf.LastAccess = time.Now()
	buf.Prefetching = false
	s.BuffersMu.Unlock()
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (s *VideoServer) cleanBuffers() {
	for addr, buf := range s.Buffers {
		if time.Since(buf.LastAccess) > s.Config.CleanupInterval {
			delete(s.Buffers, addr)
		}
	}
}

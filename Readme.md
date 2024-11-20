# GoCast

GoCast is a lightweight, tcp based video streaming server written in Go that allows you to easily host and stream your video collection through a web browser.

## Features

- Supports multiple video formats (MP4, WebM, MOV, MKV, AVI, etc.)
- Efficient local video streaming with adaptive buffering
- Auto-generated video thumbnails
- Rate limiting to prevent server overload
- Auto-resume playback position

## Quick Start

1. **Clone the Repo**:
    ```bash
    gh repo clone Bethel-nz/gocast
    ```

2. **Install Dependencies**
   - FFmpeg (for thumbnail generation)

3. **Build and Run**
   ```bash
   go build -o gocast ./cmd/gocast
   ./gocast
   ```

4. **Add Videos**
   - Place your video files in the `./videos` directory
   - The server will automatically scan and index them

5. **Access the Interface**
   - Open your browser and navigate to `http://localhost:4221`
   - Your video library will be displayed with thumbnails

## Configuration

Default configuration values can be found in `server/server.go`. Key settings include:

- Video directory: `./videos`
- Port: `4221`
- Thumbnail quality: `75`
- Max concurrent connections: `100`
- Buffer size: `64KB`
- Prefetch size: `10MB`

package media

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MaxVideo caps uploads: a few minutes of phone video. Videos are stored as
// uploaded; re-encoding would take an old laptop longer than it is worth.
const MaxVideo = 300 << 20

// SniffVideo recognises a video from its first bytes and returns the file
// extension to store it under, or "" if it is not a supported video.
func SniffVideo(head []byte) string {
	switch {
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		// ISO base media: MP4 and phone MOV share this header. QuickTime
		// ("qt  ") plays in Android's player too when the codec is H.264/HEVC.
		return "mp4"
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "webm"
	}
	return ""
}

// FFmpeg finds ffmpeg, or returns "" when it is not installed; posters are
// then skipped and videos still play.
func FFmpeg(configured string) string {
	if configured == "" {
		configured = "ffmpeg"
	}
	p, err := exec.LookPath(configured)
	if err != nil {
		return ""
	}
	return p
}

// Poster saves one frame, a second in (past fade-ins), scaled to 960 px wide,
// as a JPEG.
func Poster(ctx context.Context, ffmpeg, video, out string) error {
	if ffmpeg == "" {
		return fmt.Errorf("ffmpeg not installed")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for _, seek := range []string{"1", "0"} { // very short clips have no second 1
		cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
			"-ss", seek, "-i", video, "-frames:v", "1", "-vf", "scale='min(960,iw)':-2", "-q:v", "4", out)
		msg, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if seek == "0" {
			return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(msg)))
		}
	}
	return nil
}

package api

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMeasureMusicLoudness(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	levels := make([]float64, 0, 2)
	for _, volume := range []string{"1", "0.1"} {
		path := filepath.Join(t.TempDir(), "tone.wav")
		output, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=1000:duration=2", "-af", "volume="+volume, "-y", path).CombinedOutput()
		if err != nil {
			t.Fatalf("create test audio: %v: %s", err, output)
		}
		loudness, peak, err := measureMusicLoudness(ctx, ffmpeg, path)
		if err != nil || loudness == nil || peak == nil {
			t.Fatalf("measure %s: loudness=%v peak=%v err=%v", volume, loudness, peak, err)
		}
		levels = append(levels, *loudness)
	}
	if difference := levels[0] - levels[1]; difference < 18 || difference > 22 {
		t.Fatalf("expected roughly 20 LU difference, got %.2f LU", difference)
	}
}

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/store"
)

// Loudness analysis decodes one file at a time and stores measurements only.
// The original files and the audio streaming path are never changed.
func runMusicLoudnessWorker(ctx context.Context, db *store.Store) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		ffmpeg, err := exec.LookPath("ffmpeg")
		if err == nil {
			if err := analyzePendingMusic(ctx, db, ffmpeg); err != nil && ctx.Err() == nil {
				log.Printf("HomeMusic loudness: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func analyzePendingMusic(ctx context.Context, db *store.Store, ffmpeg string) error {
	for ctx.Err() == nil {
		library, err := db.MusicLibrary(ctx)
		if err != nil {
			return err
		}
		if library.Root == "" || !musicRootAllowed(library.Root) {
			return nil
		}
		track, err := db.MusicNextUnmeasuredTrack(ctx, library.Root)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		info, err := os.Lstat(track.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != track.Size || info.ModTime().UnixNano() != track.ModifiedAt {
			wakeMusicScanner()
			return nil
		}
		loudness, peak, analysisErr := measureMusicLoudness(ctx, ffmpeg, track.Path)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if analysisErr != nil {
			log.Printf("HomeMusic loudness %s: %v", track.ID, analysisErr)
		}
		info, err = os.Lstat(track.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != track.Size || info.ModTime().UnixNano() != track.ModifiedAt {
			wakeMusicScanner()
			return nil
		}
		if err := db.MusicSetLoudness(ctx, track, loudness, peak); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return ctx.Err()
}

func measureMusicLoudness(ctx context.Context, ffmpeg, path string) (*float64, *float64, error) {
	measureContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(measureContext, ffmpeg, "-hide_banner", "-nostats", "-loglevel", "info", "-threads", "1", "-filter_threads", "1", "-i", path, "-map", "0:a:0", "-vn", "-af", "loudnorm=I=-16:TP=-1.5:LRA=11:print_format=json", "-f", "null", "-").CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("ffmpeg analysis failed: %w", err)
	}
	start := strings.LastIndex(string(output), "{")
	if start < 0 {
		return nil, nil, errors.New("loudness report is missing")
	}
	end := strings.IndexByte(string(output)[start:], '}')
	if end < 0 {
		return nil, nil, errors.New("loudness report is incomplete")
	}
	var report struct {
		InputI  string `json:"input_i"`
		InputTP string `json:"input_tp"`
	}
	if err := json.Unmarshal(output[start:start+end+1], &report); err != nil {
		return nil, nil, err
	}
	loudness, err := strconv.ParseFloat(report.InputI, 64)
	if err != nil || math.IsNaN(loudness) || math.IsInf(loudness, 0) || loudness < -70 || loudness > 0 {
		return nil, nil, errors.New("loudness measurement is invalid")
	}
	peak, err := strconv.ParseFloat(report.InputTP, 64)
	if err != nil || math.IsNaN(peak) || math.IsInf(peak, 0) || peak < -100 || peak > 20 {
		return nil, nil, errors.New("peak measurement is invalid")
	}
	return &loudness, &peak, nil
}

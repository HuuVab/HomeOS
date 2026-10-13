package api

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type musicTrack struct {
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Duration    float64 `json:"duration"`
	Size        int64   `json:"size"`
	HasCover    bool    `json:"hasCover"`
	CoverStream int     `json:"-"`
}

type musicMetadataEntry struct {
	Size       int64
	ModifiedAt time.Time
	Track      musicTrack
}

var musicMetadataCache = struct {
	sync.Mutex
	entries map[string]musicMetadataEntry
}{entries: make(map[string]musicMetadataEntry)}

func metadataTag(tags map[string]string, key string) string {
	for name, value := range tags {
		if strings.EqualFold(name, key) && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readMusicMetadata(ctx context.Context, probe, path, name string, size int64, modifiedAt time.Time) musicTrack {
	track := musicTrack{Name: name, Title: strings.TrimSuffix(name, filepath.Ext(name)), Size: size}
	if probe == "" {
		return track
	}
	musicMetadataCache.Lock()
	if cached, ok := musicMetadataCache.entries[path]; ok && cached.Size == size && cached.ModifiedAt.Equal(modifiedAt) {
		musicMetadataCache.Unlock()
		return cached.Track
	}
	musicMetadataCache.Unlock()
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeContext, probe, "-v", "error", "-show_entries", "format=duration:format_tags=title,artist,album:stream=index,codec_type:stream_disposition=attached_pic", "-of", "json", "-i", path).Output()
	if err == nil {
		var result struct {
			Streams []struct {
				Index       int    `json:"index"`
				CodecType   string `json:"codec_type"`
				Disposition struct {
					AttachedPic int `json:"attached_pic"`
				} `json:"disposition"`
			} `json:"streams"`
			Format struct {
				Duration string            `json:"duration"`
				Tags     map[string]string `json:"tags"`
			} `json:"format"`
		}
		if json.Unmarshal(output, &result) == nil {
			if value := metadataTag(result.Format.Tags, "title"); value != "" {
				track.Title = value
			}
			track.Artist = metadataTag(result.Format.Tags, "artist")
			track.Album = metadataTag(result.Format.Tags, "album")
			if seconds, parseErr := strconv.ParseFloat(result.Format.Duration, 64); parseErr == nil && seconds > 0 {
				track.Duration = seconds
			}
			for _, stream := range result.Streams {
				if stream.CodecType == "video" && stream.Disposition.AttachedPic == 1 {
					track.HasCover = true
					track.CoverStream = stream.Index
					break
				}
			}
		}
	}
	musicMetadataCache.Lock()
	if len(musicMetadataCache.entries) > 4096 {
		clear(musicMetadataCache.entries)
	}
	musicMetadataCache.entries[path] = musicMetadataEntry{Size: size, ModifiedAt: modifiedAt, Track: track}
	musicMetadataCache.Unlock()
	return track
}

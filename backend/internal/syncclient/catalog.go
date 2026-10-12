package syncclient

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// remoteCatalog uses the change feed after the initial full listing. The
// catalog and cursor are committed together so an interrupted run replays
// changes instead of silently skipping them.
func remoteCatalog(ctx context.Context, state *State, remote *Remote) (map[string]RemoteEntry, int64, error) {
	ready, err := state.Meta("catalog_ready")
	if err != nil {
		return nil, 0, err
	}
	var entries map[string]RemoteEntry
	var cursor int64
	if ready == "1" {
		entries, err = state.RemoteEntries()
		if err != nil {
			return nil, 0, err
		}
		value, err := state.Meta("cursor")
		if err != nil {
			return nil, 0, err
		}
		cursor, err = strconv.ParseInt(value, 10, 64)
		if err != nil || cursor < 0 {
			return nil, 0, errors.New("invalid sync change cursor")
		}
		// A zero cursor cannot establish that this server has an active
		// change feed (including older/test servers). Keep listing until
		// the first durable event is observed.
		if cursor == 0 {
			entries, err = remote.ListAll(ctx)
			if err != nil {
				return nil, 0, err
			}
		}
	} else {
		entries, err = remote.ListAll(ctx)
		if err != nil {
			return nil, 0, err
		}
	}
	changed := make(map[string]bool)
	for page := 0; page < 1000; page++ {
		events, next, err := remote.Changes(ctx, cursor)
		if err != nil {
			return nil, 0, err
		}
		if len(events) == 0 {
			break
		}
		if next <= cursor {
			return nil, 0, errors.New("change feed did not advance")
		}
		for _, event := range events {
			if !validPath(event.Path) {
				return nil, 0, fmt.Errorf("unsafe change path %q", event.Path)
			}
			changed[path.Dir(event.Path)] = true
		}
		cursor = next
		if len(events) < 100 {
			break
		}
		if page == 999 {
			return nil, 0, errors.New("too many unprocessed remote changes")
		}
	}
	folders := make([]string, 0, len(changed))
	for folder := range changed {
		folders = append(folders, folder)
	}
	sort.Slice(folders, func(i, j int) bool { return pathDepth(folders[i]) < pathDepth(folders[j]) })
	for _, folder := range folders {
		if folder != "." {
			parent, exists := entries[folder]
			if !exists || parent.Kind != "folder" {
				continue
			}
		} else {
			folder = ""
		}
		children, err := remote.ListFolder(ctx, folder)
		if err != nil {
			return nil, 0, err
		}
		fresh := make(map[string]RemoteEntry, len(children))
		for _, entry := range children {
			fresh[entry.Path] = entry
		}
		for name := range entries {
			if path.Dir(name) == folder || folder == "" && !strings.Contains(name, "/") {
				if _, exists := fresh[name]; !exists {
					delete(entries, name)
					for child := range entries {
						if strings.HasPrefix(child, name+"/") {
							delete(entries, child)
						}
					}
				}
			}
		}
		for name, entry := range fresh {
			_, known := entries[name]
			entries[name] = entry
			if !known && entry.Kind == "folder" {
				nested, err := remote.ListTree(ctx, name)
				if err != nil {
					return nil, 0, err
				}
				for child, item := range nested {
					entries[child] = item
				}
			}
		}
	}
	if len(entries) > 10000 {
		return nil, 0, errors.New("sync prototype is limited to 10,000 entries")
	}
	if err := state.SaveRemoteEntries(entries, cursor); err != nil {
		return nil, 0, err
	}
	if err := state.SetMeta("catalog_ready", "1"); err != nil {
		return nil, 0, err
	}
	return entries, cursor, nil
}

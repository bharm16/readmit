package desktop

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Dropping files on the window (#552) is choosing them another way. The
// shell enables the host's file drop, and the window receives the dropped
// paths from its runtime (the "wails:file-drop" event, OnFileDrop); this
// sorts them into the files, folders and ZIP archives an import names, so
// they reach ProbeImport exactly as the picker's choices do. It reads each
// path's own type and nothing inside it.

// MaxDroppedSources bounds one drop.
const MaxDroppedSources = 256

// DroppedRefusal names one dropped path an import cannot take, by its base
// name, and why.
type DroppedRefusal struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// DroppedSourcesResult is one drop sorted as an import names its inputs:
// Files, Folders and Archives in the order they were dropped, and each path
// it cannot take in Refused. Empty when nothing was dropped.
type DroppedSourcesResult struct {
	State    State            `json:"state"`
	Reason   string           `json:"reason,omitzero"`
	Files    []string         `json:"files"`
	Folders  []string         `json:"folders"`
	Archives []string         `json:"archives"`
	Refused  []DroppedRefusal `json:"refused"`
}

func (r *DroppedSourcesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ClassifyDroppedSources sorts the paths a drop delivered: a folder is a
// folder, a regular file named .zip an archive and any other regular file a
// file. A symbolic link, a device, a pipe or a path that is not there is
// refused by name, as the picker would never have offered it. It reads, opens
// and writes nothing.
func (a *App) ClassifyDroppedSources(paths []string) DroppedSourcesResult {
	return runRead(a, false, func(context.Context) DroppedSourcesResult {
		result := DroppedSourcesResult{Files: []string{}, Folders: []string{}, Archives: []string{}, Refused: []DroppedRefusal{}}
		if len(paths) == 0 {
			result.State = Empty
			return result
		}
		if len(paths) > MaxDroppedSources {
			result.refuse(Failed, "drop at most 256 files and folders at once")
			return result
		}
		for _, path := range paths {
			name := filepath.Base(path)
			if !filepath.IsAbs(path) {
				result.Refused = append(result.Refused, DroppedRefusal{Name: name, Reason: "the drop did not name where it is"})
				continue
			}
			info, err := os.Lstat(path)
			switch {
			case err != nil:
				result.Refused = append(result.Refused, DroppedRefusal{Name: name, Reason: "it cannot be read"})
			case info.Mode()&fs.ModeSymlink != 0:
				result.Refused = append(result.Refused, DroppedRefusal{Name: name, Reason: "it is a link; drop what it points to"})
			case info.IsDir():
				result.Folders = append(result.Folders, path)
			case !info.Mode().IsRegular():
				result.Refused = append(result.Refused, DroppedRefusal{Name: name, Reason: "it is not a file or a folder"})
			case strings.EqualFold(filepath.Ext(path), ".zip"):
				result.Archives = append(result.Archives, path)
			default:
				result.Files = append(result.Files, path)
			}
		}
		result.State = Completed
		return result
	})
}

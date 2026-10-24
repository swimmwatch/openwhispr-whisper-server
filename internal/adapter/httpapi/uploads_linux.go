package httpapi

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/sys/unix"
)

const uploadMarker = "whisper-api-upload-v1\n"

var uploadSession = regexp.MustCompile(`^run-[A-Z2-7]{26}$`)

func openUploads(path string) (root *os.Root, lock *os.File, err error) {
	if err = os.MkdirAll(path, 0700); err != nil {
		return
	}

	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, os.ErrInvalid
	}

	root, err = os.OpenRoot(path)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			if lock != nil {
				_ = lock.Close()
			}
			_ = root.Close()
		}
	}()

	lock, err = root.OpenFile(".owner.lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return
	}
	info, err = lock.Stat()
	if err != nil {
		return
	}
	if !info.Mode().IsRegular() {
		err = os.ErrInvalid
		return
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return
	}

	dir, err := root.Open(".")
	if err != nil {
		return
	}
	defer dir.Close()

	for {
		var entries []os.DirEntry
		entries, err = dir.ReadDir(128)
		for _, entry := range entries {
			if !entry.IsDir() || !uploadSession.MatchString(entry.Name()) {
				continue
			}
			f, openErr := root.OpenFile(filepath.Join(entry.Name(), ".owner"), os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if openErr != nil {
				continue
			}

			info, statErr := f.Stat()
			var marker []byte
			if statErr == nil && info.Mode().IsRegular() {
				marker, _ = io.ReadAll(io.LimitReader(f, int64(len(uploadMarker)+1)))
			}
			_ = f.Close()
			if string(marker) == uploadMarker {
				if removeErr := root.RemoveAll(entry.Name()); removeErr != nil {
					err = removeErr
					return
				}
			}
		}
		if errors.Is(err, io.EOF) {
			err = nil
			return
		}
		if err != nil {
			return
		}
	}
}

func (h *Handler) uploadDirectory() (string, error) {
	name := "run-" + rand.Text()
	if err := h.root.Mkdir(name, 0700); err != nil {
		return "", err
	}
	if err := h.root.WriteFile(filepath.Join(name, ".owner"), []byte(uploadMarker), 0600); err != nil {
		_ = h.root.RemoveAll(name)
		return "", err
	}
	return name, nil
}

package backend

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/sys/unix"
)

const workMarker = "whisper-backend-work-v1\n"

var sessionPattern = regexp.MustCompile(`^run-[A-Z2-7]{26}$`)

type workArea struct {
	root       *os.Root
	lock       *os.File
	name, path string
}

func openWork(path string) (area *workArea, err error) {
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errNative
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}

	area = &workArea{root: root}
	defer func() {
		if err != nil {
			_ = area.close()
		}
	}()

	area.lock, err = root.OpenFile(".owner.lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return area, err
	}
	info, err = area.lock.Stat()
	if err != nil {
		return area, err
	}
	if !info.Mode().IsRegular() {
		return area, errNative
	}

	if err = unix.Flock(int(area.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return area, err
	}

	if err = cleanAbandonedWork(root); err != nil {
		return area, err
	}

	area.name = "run-" + rand.Text()
	if err = root.Mkdir(area.name, 0700); err != nil {
		area.name = ""
		return area, err
	}
	if err = root.WriteFile(filepath.Join(area.name, ".owner"), []byte(workMarker), 0600); err != nil {
		return area, err
	}
	if err = root.Mkdir(filepath.Join(area.name, "public"), 0700); err != nil {
		return area, err
	}

	area.path = filepath.Join(path, area.name)
	return area, nil
}

func (w *workArea) close() error {
	var err error
	if w.name != "" {
		err = w.root.RemoveAll(w.name)
	}
	if w.lock != nil {
		err = errors.Join(err, w.lock.Close())
	}
	return errors.Join(err, w.root.Close())
}

func cleanAbandonedWork(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()

	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if !sessionPattern.MatchString(entry.Name()) || !entry.IsDir() {
				continue
			}

			file, openErr := root.OpenFile(
				filepath.Join(entry.Name(), ".owner"),
				os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK,
				0,
			)
			if openErr != nil {
				continue
			}

			info, statErr := file.Stat()
			var marker []byte
			if statErr == nil && info.Mode().IsRegular() {
				marker, _ = io.ReadAll(io.LimitReader(file, int64(len(workMarker)+1)))
			}
			_ = file.Close()

			if string(marker) == workMarker {
				if err := root.RemoveAll(entry.Name()); err != nil {
					return err
				}
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

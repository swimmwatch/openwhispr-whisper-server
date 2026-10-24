package audio

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/application/transcription"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/domain"
	"golang.org/x/sys/unix"
)

const (
	waveHeaderBytes   = 44
	riffSizeOverhead  = waveHeaderBytes - 8
	pcmSampleRate     = 16000
	pcmChannels       = 1
	pcmBitsPerSample  = 16
	pcmBlockBytes     = pcmChannels * pcmBitsPerSample / 8
	pcmBytesPerSecond = pcmSampleRate * pcmBlockBytes
)

type Options struct {
	Command          []string
	AudioBytes       int64
	DecodedSeconds   int
	OperationTimeout time.Duration
	CleanupTimeout   time.Duration
	Capacity         int
}

type Decoder struct {
	opts             Options
	retained, active chan struct{}
}

func New(opts Options) (*Decoder, error) {
	if len(opts.Command) == 0 ||
		opts.Command[0] == "" ||
		opts.AudioBytes <= 0 ||
		opts.DecodedSeconds <= 0 ||
		opts.OperationTimeout <= 0 ||
		opts.CleanupTimeout <= 0 ||
		opts.Capacity < 1 {
		return nil, &domain.Failure{Kind: domain.InvalidInput}
	}

	opts.Command = slices.Clone(opts.Command)
	return &Decoder{opts: opts, retained: make(chan struct{}, opts.Capacity), active: make(chan struct{}, 1)}, nil
}

func (d *Decoder) Normalize(parent context.Context, input transcription.Input) (result transcription.Normalized, err error) {
	ctx, cancel := context.WithTimeout(parent, d.opts.OperationTimeout)
	defer cancel()

	select {
	case d.retained <- struct{}{}:
	case <-ctx.Done():
		return result, audioFailure(domain.DeadlineExceeded, ctx.Err())
	}

	owned := false
	defer func() {
		if !owned {
			<-d.retained
		}
	}()

	select {
	case d.active <- struct{}{}:
	case <-ctx.Done():
		return result, audioFailure(domain.DeadlineExceeded, ctx.Err())
	}
	defer func() {
		<-d.active
	}()

	if err = ctx.Err(); err != nil {
		return result, audioFailure(domain.DeadlineExceeded, err)
	}
	if !filepath.IsAbs(input.UploadPath) || input.Size <= 0 || input.Size > d.opts.AudioBytes {
		return result, audioFailure(domain.InvalidInput, nil)
	}

	source, err := os.OpenFile(input.UploadPath, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return result, audioFailure(domain.UnsupportedAudio, err)
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != input.Size {
		return result, audioFailure(domain.InvalidInput, err)
	}

	root, err := os.OpenRoot(filepath.Dir(input.UploadPath))
	if err != nil {
		return result, audioFailure(domain.Internal, err)
	}

	name := "normalized-" + rand.Text() + ".wav"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		_ = root.Close()
		return result, audioFailure(domain.Internal, err)
	}

	output := &normalizedAudio{
		File: file,
		root: root,
		name: name,
		release: func() {
			<-d.retained
		},
	}
	defer func() {
		if !owned {
			err = errors.Join(err, output.discard())
		}
	}()

	if _, err = file.Write(make([]byte, waveHeaderBytes)); err != nil {
		return result, audioFailure(domain.Internal, err)
	}

	pcm := &pcmOutput{file: file, limit: decodedDataLimit(d.opts.DecodedSeconds), cancel: cancel}
	cmd := d.command(ctx, filepath.Dir(input.UploadPath), source, pcm)
	if err = cmd.Start(); err != nil {
		return result, audioFailure(domain.BackendUnavailable, err)
	}

	waitErr := cmd.Wait()
	cleanupErr := reapGroup(cmd.Process.Pid, d.opts.CleanupTimeout)
	written, over, writeErr := pcm.snapshot()
	if cleanupErr != nil {
		return result, audioFailure(domain.BackendUnavailable, cleanupErr)
	}
	if over {
		return result, audioFailure(domain.AudioTooLong, waitErr)
	}
	if ctx.Err() != nil {
		return result, audioFailure(domain.DeadlineExceeded, ctx.Err())
	}
	if writeErr != nil {
		return result, audioFailure(domain.Internal, writeErr)
	}
	if waitErr != nil || written == 0 || written%pcmBlockBytes != 0 {
		return result, audioFailure(domain.UnsupportedAudio, waitErr)
	}

	header := waveHeader(uint32(written))
	if _, err = file.WriteAt(header, 0); err != nil {
		return result, audioFailure(domain.Internal, err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return result, audioFailure(domain.Internal, err)
	}

	owned = true
	return transcription.Normalized{
		Audio:     output,
		Size:      waveHeaderBytes + written,
		Duration:  time.Duration(written/pcmBlockBytes) * time.Second / pcmSampleRate,
		RequestID: input.RequestID,
		Fields:    input.Fields,
	}, nil
}

func decodedDataLimit(seconds int) int64 {
	maxData := int64(math.MaxUint32) - riffSizeOverhead
	if int64(seconds) <= maxData/pcmBytesPerSecond {
		maxData = int64(seconds) * pcmBytesPerSecond
	}
	maxData -= maxData % pcmBlockBytes
	return maxData
}

func (d *Decoder) command(ctx context.Context, directory string, source io.Reader, pcm *pcmOutput) *exec.Cmd {
	args := append(slices.Clone(d.opts.Command[1:]), decoderArgs()...)
	cmd := exec.CommandContext(ctx, d.opts.Command[0], args...)
	cmd.Dir = directory
	cmd.Env = slices.DeleteFunc(os.Environ(), func(value string) bool {
		return strings.HasPrefix(value, "FFREPORT=")
	})
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}

	cmd.WaitDelay = max(time.Millisecond, min(100*time.Millisecond, d.opts.CleanupTimeout/4))
	cmd.Stdin = io.LimitReader(source, d.opts.AudioBytes)
	cmd.Stdout = pcm
	cmd.Stderr = io.Discard
	return cmd
}

func decoderArgs() []string {
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-threads", "1",
		"-protocol_whitelist", "pipe,cache",
		"-format_whitelist", "wav,mp3,mov,ogg,flac,matroska,webm,mpeg",
		"-read_ahead_limit", "-1",
		"-i", "cache:pipe:0",
		"-map", "0:a:0",
		"-vn",
		"-sn",
		"-dn",
		"-ac", strconv.Itoa(pcmChannels),
		"-ar", strconv.Itoa(pcmSampleRate),
		"-c:a", "pcm_s16le",
		"-f", "s16le",
		"pipe:1",
	}
}

func audioFailure(kind domain.FailureKind, cause error) error {
	return &domain.Failure{Kind: kind, Cause: cause}
}

func waveHeader(size uint32) []byte {
	h := make([]byte, waveHeaderBytes)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], size+riffSizeOverhead)
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
	binary.LittleEndian.PutUint16(h[22:], pcmChannels)
	binary.LittleEndian.PutUint32(h[24:], pcmSampleRate)
	binary.LittleEndian.PutUint32(h[28:], pcmBytesPerSecond)
	binary.LittleEndian.PutUint16(h[32:], pcmBlockBytes)
	binary.LittleEndian.PutUint16(h[34:], pcmBitsPerSample)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], size)
	return h
}

func reapGroup(pid int, budget time.Duration) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	deadline := time.Now().Add(budget)
	for {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		time.Sleep(time.Millisecond)
	}
}

type pcmOutput struct {
	mu             sync.Mutex
	file           *os.File
	written, limit int64
	over           bool
	err            error
	cancel         context.CancelFunc
}

func (p *pcmOutput) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if int64(len(data)) > p.limit-p.written {
		data = data[:p.limit-p.written]
		p.over = true
	}

	n, err := p.file.Write(data)
	p.written += int64(n)
	p.err = errors.Join(p.err, err)
	if p.over {
		p.cancel()
		return n, errors.New("decoded duration limit")
	}
	return n, err
}

func (p *pcmOutput) snapshot() (int64, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written, p.over, p.err
}

type normalizedAudio struct {
	*os.File
	root    *os.Root
	name    string
	release func()
	once    sync.Once
	err     error
}

func (a *normalizedAudio) discard() error {
	return errors.Join(a.File.Close(), a.root.Remove(a.name), a.root.Close())
}

func (a *normalizedAudio) Close() error {
	a.once.Do(func() {
		if err := a.discard(); err != nil {
			a.err = audioFailure(domain.Internal, err)
		}
		a.release()
	})
	return a.err
}

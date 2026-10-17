package devices

import (
	"board"
	"board/device"
	"board/filesystemprobe"
	"board/volumearchive"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const StorageUpload = board.StorageUpload

type StorageOptions struct {
	ID, Name, RootID, Path              string
	AllowRestore                        bool
	MaxTransferBytes, MaxExtractedBytes int64
}

func Storage(o StorageOptions) (*device.Provider, error) {
	if o.ID == "" || o.RootID == "" || o.Path == "" {
		return nil, errors.New("storage requires provider ID, resource ID, and approved path")
	}
	canonical, err := filepath.EvalSymlinks(o.Path)
	if err != nil {
		return nil, err
	}
	o.Path, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	identity, err := os.Stat(o.Path)
	if err != nil || !identity.IsDir() {
		return nil, errors.New("storage root is unavailable")
	}
	if o.MaxTransferBytes == 0 {
		o.MaxTransferBytes = 128 << 20
	}
	if o.MaxExtractedBytes == 0 {
		o.MaxExtractedBytes = 1 << 30
	}
	if o.MaxTransferBytes <= 0 || o.MaxTransferBytes > 1<<30 || o.MaxExtractedBytes <= 0 || o.MaxExtractedBytes > 16<<30 {
		return nil, errors.New("invalid storage limits")
	}
	check := func() error {
		now, err := os.Stat(o.Path)
		if err != nil || !os.SameFile(identity, now) {
			return board.ErrDriveUnavailable
		}
		return nil
	}
	caps := []board.CapabilityDescriptor{device.Descriptor(board.StorageInspect, device.EmptySchema, true), device.Descriptor(board.StorageExport, device.EmptySchema, true)}
	if o.AllowRestore {
		caps = append(caps, device.Descriptor(StorageUpload, device.EmptySchema, true), device.Descriptor(board.StorageRestore, device.EmptySchema, true))
	}
	return &device.Provider{ProviderID: o.ID, ResourceIDs: []string{o.RootID}, Device: board.Device{ID: "volume", Kind: "storage", Name: o.Name, Capabilities: caps}, Bind: func(context.Context, board.Grant) (device.Binding, error) {
		if err := check(); err != nil {
			return device.Binding{}, err
		}
		root, err := os.OpenRoot(o.Path)
		if err != nil {
			return device.Binding{}, err
		}
		now, err := root.Stat(".")
		if err != nil || !os.SameFile(identity, now) {
			root.Close()
			return device.Binding{}, board.ErrDriveUnavailable
		}
		tmp, err := os.MkdirTemp("", "board-transfer-")
		if err != nil {
			root.Close()
			return device.Binding{}, err
		}
		var mu sync.Mutex
		var upload *uploadFile
		ops := map[board.Capability]device.Operation{}
		ops[board.StorageInspect] = device.Operation{Invoke: func(_ context.Context, a board.Action) (board.Result, error) {
			if err := device.Empty(a.Input); err != nil {
				return board.Result{}, err
			}
			if err := check(); err != nil {
				return board.Result{}, err
			}
			v, ok := filesystemprobe.VolumeCapacityBytes(o.Path)
			return device.Result(map[string]any{"available": ok, "capacityBytes": v.CapacityBytes, "availableBytes": v.AvailableBytes})
		}}
		ops[board.StorageExport] = device.Operation{Stream: func(ctx context.Context, a board.Action) (board.Content, error) {
			if err := device.Empty(a.Input); err != nil {
				return nil, err
			}
			if err := check(); err != nil {
				return nil, err
			}
			mu.Lock()
			defer mu.Unlock()
			f, err := os.CreateTemp(tmp, "export-")
			if err != nil {
				return nil, err
			}
			limited := &limitedWriter{writer: f, remaining: o.MaxTransferBytes}
			err = volumearchive.ExportRoot(ctx, limited, root)
			err = errors.Join(err, f.Close())
			if err != nil {
				_ = os.Remove(f.Name())
				return nil, err
			}
			content, err := openContent(f.Name(), o.MaxTransferBytes)
			if err != nil {
				os.Remove(f.Name())
				return nil, err
			}
			return &temporaryContent{Content: content, path: f.Name()}, nil
		}}
		ops[StorageUpload] = device.Operation{Channel: func(_ context.Context, a board.Action, l board.ChannelLimits) (io.ReadWriteCloser, error) {
			if err := device.Empty(a.Input); err != nil {
				return nil, err
			}
			if l.ReadBytes != 0 || l.WriteBytes > o.MaxTransferBytes {
				return nil, board.ErrNotGranted
			}
			mu.Lock()
			defer mu.Unlock()
			if upload != nil {
				return nil, errors.New("archive already uploaded; open a new session for another transfer")
			}
			f, err := os.Create(filepath.Join(tmp, "upload.tar.gz"))
			if err != nil {
				return nil, err
			}
			upload = &uploadFile{file: f, remaining: o.MaxTransferBytes}
			return upload, nil
		}}
		ops[board.StorageRestore] = device.Operation{Invoke: func(ctx context.Context, a board.Action) (board.Result, error) {
			if err := device.Empty(a.Input); err != nil {
				return board.Result{}, err
			}
			if err := check(); err != nil {
				return board.Result{}, err
			}
			mu.Lock()
			defer mu.Unlock()
			if upload == nil || !upload.complete() {
				return board.Result{}, errors.New("close the upload channel before restoring")
			}
			if err := volumearchive.RestoreRootWithLimits(ctx, filepath.Join(tmp, "upload.tar.gz"), root, volumearchive.Limits{Bytes: o.MaxExtractedBytes, Entries: 100000}); err != nil {
				return board.Result{}, err
			}
			return device.Result(map[string]bool{"restored": true})
		}}
		return device.Binding{Operations: ops, Close: func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			if upload != nil {
				_ = upload.Close()
			}
			return errors.Join(root.Close(), os.RemoveAll(tmp))
		}}, nil
	}}, nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, board.ErrLimit
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

type uploadFile struct {
	mu        sync.Mutex
	file      *os.File
	remaining int64
	closed    bool
	err       error
}

func (u *uploadFile) Read([]byte) (int, error) { return 0, board.ErrNotGranted }
func (u *uploadFile) Write(p []byte) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return 0, board.ErrClosed
	}
	if int64(len(p)) > u.remaining {
		u.err = board.ErrLimit
		return 0, u.err
	}
	n, err := u.file.Write(p)
	u.remaining -= int64(n)
	if err != nil {
		u.err = err
	}
	return n, err
}
func (u *uploadFile) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.closed {
		u.closed = true
		u.err = errors.Join(u.err, u.file.Close())
	}
	return u.err
}
func (u *uploadFile) complete() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed && u.err == nil
}

type temporaryContent struct {
	board.Content
	path string
	once sync.Once
	err  error
}

func (c *temporaryContent) Close() error {
	c.once.Do(func() { c.err = errors.Join(c.Content.Close(), os.Remove(c.path)) })
	return c.err
}

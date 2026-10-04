package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"blockade"
	"console/bindings"
)

// The CLI host, rather than Console, chooses an existing artifact directory.
// OpenRoot confines mutations to that directory, including symlink traversal.
func consoleAudioConsumer(root *os.Root, directory, format string, ceiling int64) bindings.SpeechConsumer {
	return func(ctx context.Context, stream blockade.SpeechStream) (json.RawMessage, error) {
		extensions := map[string]string{"mp3": "mp3", "opus": "ogg", "aac": "aac", "flac": "flac", "wav": "wav", "pcm": "pcm"}
		extension, ok := extensions[format]
		if !ok {
			return nil, errors.New("unsupported speech artifact format")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		name := hex.EncodeToString(id[:]) + "." + extension
		temporary := "." + name + ".partial"
		file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		defer root.Remove(temporary)
		count, copyErr := io.Copy(file, io.LimitReader(stream.Audio, ceiling+1))
		closeErr := file.Close()
		if count > ceiling {
			return nil, errors.Join(errors.New("speech artifact exceeds byte ceiling"), copyErr, closeErr)
		}
		if err := errors.Join(copyErr, closeErr, ctx.Err()); err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, errors.New("speech artifact is empty")
		}
		if err := root.Link(temporary, name); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"path": filepath.Join(directory, name), "mediaType": stream.MediaType, "bytes": count})
	}
}

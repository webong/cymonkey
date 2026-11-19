package vncgateway

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayRequiresBearerToken(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/vnc", nil)
	response := httptest.NewRecorder()
	(Server{Token: "secret", Upstream: "127.0.0.1:5999"}).Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestGatewayExposesEachDisplayTransport(t *testing.T) {
	t.Parallel()

	server := (Server{Token: "secret", Upstream: "127.0.0.1:5999"}).Handler()
	for _, path := range []string{"/v1/vnc", "/v1/rdp", "/v1/spice"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}

func TestReadMaskedClientFrame(t *testing.T) {
	t.Parallel()

	payload := []byte("RFB 003.008\n")
	mask := []byte{1, 2, 3, 4}
	frame := []byte{0x82, 0x80 | byte(len(payload))}
	frame = append(frame, mask...)
	for index, value := range payload {
		frame = append(frame, value^mask[index%4])
	}
	opcode, got, err := readClientFrame(bufio.NewReader(bytes.NewReader(frame)), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0x2 || !bytes.Equal(got, payload) {
		t.Fatalf("frame = opcode %d payload %q", opcode, got)
	}
}

func TestWriteServerFrame(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte{7}, 300)
	var output bytes.Buffer
	if err := writeServerFrame(&output, 0x2, payload); err != nil {
		t.Fatal(err)
	}
	raw := output.Bytes()
	if raw[0] != 0x82 || raw[1] != 126 || binary.BigEndian.Uint16(raw[2:4]) != 300 ||
		!bytes.Equal(raw[4:], payload) {
		t.Fatalf("invalid server frame")
	}
}

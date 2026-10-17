package main

import (
	"sync"

	"board/display/sdk"
	"jangolova/displaykit"
)

// material is the engine's view of one endpoint's prepared connection
// material. The host replaces it when it renews credentials, and the engine
// acknowledges each revision it has applied.
type material struct {
	mu       sync.Mutex
	current  displaykit.Connection
	updates  chan uint64
	acked    uint64
	revision uint64
}

func newMaterial(connection displaykit.Connection) *material {
	return &material{current: connection, revision: connection.Revision, updates: make(chan uint64, 1)}
}

func (m *material) Snapshot() sdk.EndpointConnectionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	headers := make(map[string]string, len(m.current.Headers))
	for name, value := range m.current.Headers {
		headers[name] = value
	}
	return sdk.EndpointConnectionSnapshot{
		Headers: headers, Revision: m.current.Revision,
		CredentialRevision: m.current.CredentialRevision, TLSRevision: m.current.TLSRevision,
	}
}

func (m *material) Updates() <-chan uint64 { return m.updates }

func (m *material) Acknowledge(revision uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if revision > m.acked {
		m.acked = revision
	}
}

// update installs a newer revision and wakes whoever waits on Updates. An older
// revision is ignored, so a late message cannot roll credentials back.
func (m *material) update(connection displaykit.Connection) {
	m.mu.Lock()
	if connection.Revision <= m.revision {
		m.mu.Unlock()
		return
	}
	m.current, m.revision = connection, connection.Revision
	m.mu.Unlock()
	select {
	case m.updates <- connection.Revision:
	default:
	}
}

# Session Persistence

Both the SIP handler (`pkg/sip`) and the higher-level session manager (`pkg/session`) operate on the same session data model. This document explains how to wire them together.

## Storage Interfaces

- `pkg/sip.SessionStore` – Minimal interface used by the SIP handler for persisting `CallData` snapshots.
- `pkg/session.SessionStore` – Richer interface used by the session manager (supports heartbeats, indexing, backups).

`pkg/sip/session_store_adapter.go` bridges these two interfaces so you can initialise a single backing store and share it.

## In-Memory Mode

If you do not provide a store, the SIP handler falls back to an in-memory map that is cleared on shutdown:

```go
handlerConfig := &sip.Config{
    // SessionStore left nil – in-memory persistence
}
handler, _ := sip.NewHandler(logger, handlerConfig, nil)
```

This mode is useful for local testing but does not survive process restarts.

## Redis Example

```go
redisStore, err := session.NewRedisSessionStore(redisConfig, logger)
if err != nil {
    panic(err)
}

handlerConfig := &sip.Config{
    SessionStore:  redisStore,
    SessionNodeID: "recorder-1",
}
handler, _ := sip.NewHandler(logger, handlerConfig, sttManager)

managerConfig := &session.ManagerConfig{
    NodeID:            "recorder-1",
    HeartbeatInterval: 30 * time.Second,
    CleanupInterval:   5 * time.Minute,
    SessionTimeout:    time.Hour,
}
manager := session.NewSessionManager(redisStore, managerConfig, logger)
```

Both components receive the **same** `redisStore`, ensuring consistency between SIP handling and higher-level orchestration.

## Call Snapshot Format

The adapter serialises:

- Recording session metadata (`RecordingSession` from `pkg/siprec`)
- SIP dialog info (Call-ID, tags, URIs, route set)
- Last activity timestamp

Custom metadata can be stored in the session manager by extending the adapter or by populating the `Metadata` map before calling `Save`.

# SIPREC Server E2E Tests

This directory contains End-to-End (E2E) tests for the SIPREC server, verifying the integration of SIP signaling, RTP media processing, Recording storage, and STT (Speech-to-Text) providers.

## Running the Tests

To run the full E2E suite with race detection enabled:

```bash
go test -v -race -tags=e2e ./test/e2e/...
```

## Test Files & Coverage

### 1. `siprec_simulation_test.go`
**Goal**: Verify the complete lifecycle of a SIPREC session without external dependencies.
- **Scope**: simulates SIP INVITE, RTP packets, and BYE.
- **Verifies**:
  - SIPREC metadata parsing.
  - Media pipeline initialization.
  - Mock STT provider integration (deadlock verified fixed).
  - Graceful session termination.

### 2. `siprec_recording_test.go`
**Goal**: Verify that audio is correctly written to disk.
- **Scope**: Sends simulated RTP packets and checks the filesystem.
- **Verifies**:
  - `RTPForwarder` writes valid `.wav` files.
  - File size > WAV header (44 bytes).
  - **Fixed Bug**: Data race on `LastRTPTime` during packet processing.

### 3. `siprec_redundancy_test.go`
**Goal**: Verify failover and high-availability features.
- **Scope**: Simulates stream failures and redundant session handling.
- **Verifies**:
  - Stream continuity after failover.
  - **Fixed Bug**: Data race in audio stream collection map.

### 4. `siprec_e2e_test.go`
*Legacy/Placeholder*. Contains notes on API migration. See the above files for active tests.

## Troubleshooting

- **Timeouts**: If `TestSimulatedSiprecFullFlow` times out, ensure the simulated UDP connection is closed *before* waiting for the STT provider.
- **Race Conditions**: Always run with `-race`. We have fixed known races in `pkg/media/rtp.go` (packet timestamps) and `pkg/stt/mock.go` (channel synchronization).

# Oracle Linux 10 deployment notes

This guide adapts the useful parts of the [hopefulGiupplo/siprec OL10 guide](https://github.com/hopefulGiupplo/siprec/blob/ed6f1b3ff8467a608cdff9f99c3194d771f72ed4/OL10_README.md). It uses the upstream repository, a pinned revision, a dedicated service account, and explicit operator-managed firewall rules. These steps have not been validated on an OL10 host in this change.

## Build prerequisites

Use an Oracle Linux 10 host with an administrator account and approved, signed repositories. Basic build tools used by the fork are:

```bash
sudo dnf install git gcc gcc-c++ make pkgconf-pkg-config ca-certificates
```

Install the Go toolchain required by `go.mod` (currently Go 1.26 or newer), following the [Go installation instructions](https://go.dev/doc/install). Verify the download checksum before installation. Do not automatically replace an existing system Go installation.

The native CGO build also needs Opus development headers and the bcg729 library and headers. Check availability in your approved OL10 repositories before building. The fork imports bcg729 from an OL9 repository; this guide deliberately does not do that. Cross-major-version binary compatibility has not been established here. If the libraries are unavailable, use the repository's Docker build or arrange an independently tested native dependency build.

## Pin and build the source

Set `SIPREC_REF` to a reviewed release tag or full commit SHA before running these commands. Use a fresh checkout so local dependency or configuration changes are not overwritten.

```bash
: "${SIPREC_REF:?Set SIPREC_REF to the release tag or commit you intend to deploy}"
git clone https://github.com/loreste/siprec.git siprec-build
cd siprec-build
git checkout --detach "$SIPREC_REF"
git rev-parse HEAD
go version
pkg-config --modversion opus
make build
```

The native binary is `build/siprec`. Record the commit and dependency versions in your deployment inventory. For a container build with an already configured Docker installation:

```bash
docker build -t siprec-local:reviewed .
```

See [installation](installation.md) for container runtime configuration. The metadata compatibility feature described below is newer than v1.3.4; choose a revision containing it if you need that feature.

## Install a native service

Create the service account once and install files outside a developer's home directory:

```bash
sudo useradd --system --home-dir /var/lib/siprec --shell /sbin/nologin siprec
sudo install -d -m 0755 /usr/local/libexec
sudo install -m 0755 build/siprec /usr/local/libexec/siprec
sudo install -d -m 0750 -o root -g siprec /etc/siprec
sudo install -d -m 0750 -o siprec -g siprec /var/lib/siprec /var/log/siprec
sudo install -m 0640 -o root -g siprec config.example.yaml /etc/siprec/config.yaml
sudo restorecon -v /usr/local/libexec/siprec
```

Review `/etc/siprec/config.yaml` before starting the service. Set the recording directory to `/var/lib/siprec/recordings`, match SIP/RTP ports to your SBC, configure NAT and TLS as appropriate, and enable only the STT providers you use. Keep credential-bearing files readable only by root and the service account. Back up existing configuration before an upgrade; do not overwrite it with the example.

Create `/etc/systemd/system/siprec.service`:

```ini
[Unit]
Description=IZI SIPREC recording server
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=siprec
Group=siprec
WorkingDirectory=/var/lib/siprec
Environment=CONFIG_FILE=/etc/siprec/config.yaml
ExecStart=/usr/local/libexec/siprec
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/siprec /var/log/siprec

[Install]
WantedBy=multi-user.target
```

Any local Whisper executable, model, or credential file must be readable from locations allowed by this unit. Configure its cache under `/var/lib/siprec` when it needs writable storage. Keep SELinux enforcing; inspect relevant AVC denials and apply narrowly scoped policy or file-context changes rather than disabling enforcement.

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now siprec
sudo systemctl status siprec --no-pager
sudo journalctl -u siprec -n 100 --no-pager
curl --fail http://127.0.0.1:8080/health/live
```

Adjust the health URL if you changed the HTTP port.

## Firewall and validation

Allow the configured SIP and RTP ports only from the SRC/SBC networks that need them. Allow the HTTP API and metrics port only from approved management networks. Record the exact firewalld zone and rules you add; do not remove or replace unrelated rules merely because they mention the same port.

Validate an actual SIPREC call before production use: successful INVITE/200/ACK, negotiated RTP ports, a usable recording, BYE cleanup, and any configured transcription. Verify behavior with SELinux enforcing and the firewall enabled.

For SRCs with incomplete metadata, see [metadata compatibility settings](configuration.md#incomplete-siprec-metadata). Both compatibility options default to false. Only enable them after establishing which metadata your SRC omits and which trusted SBC inserts fallback participant headers.

## Upgrade and removal

Keep the previous binary and configuration backup for rollback. Stop the service before replacing the binary, then restore its SELinux label and restart it. Recheck health and a recording after each upgrade.

For removal, stop and disable the service, back up configuration and recordings, and remove only files and firewall rules recorded as belonging to this installation. Leave recordings and shared build/runtime dependencies intact unless an operator explicitly decides to remove them. The fork's broad uninstall script is not included.

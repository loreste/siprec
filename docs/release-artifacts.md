# Release artifacts and codec support

## Choose an artifact

| Artifact | Opus / G.729 decoding | Requirements |
| --- | --- | --- |
| Standalone Linux, macOS, and Windows archives | Not available | Portable builds made with `CGO_ENABLED=0` |
| Published Linux Docker image | Available | Container runtime; native libraries are included |
| Native build from source | Available when built with CGO and codec libraries | Go, C/C++ toolchain, libopus and bcg729 development libraries |

The standalone archives retain other supported codecs; they are not substitutes for a native build when your SBC negotiates Opus or G.729. See [installation](https://github.com/loreste/siprec/blob/main/docs/installation.md) for native and container setup. No existing immutable release assets are changed by the revised workflow.

Builds containing the updated version command report JSON without starting services or loading configuration:

```bash
./siprec --version
```

The output includes `version`, `commit`, `build_time`, `go_version`, `os`, `arch`, and `native_codecs`. The last field is true for CGO-enabled builds. Local builds without injected metadata use `unknown` for commit and build time. Existing v1.3.4 downloads predate this command.

## How release publication works

1. Push a new version tag matching the `VERSION` file and a dated entry in `CHANGELOG.md`.
2. Test the source and build all five portable archives. Verify the Linux executable's injected metadata.
3. Package the Helm chart and create SHA-256 checksums covering the archives, chart, and this build note.
4. Upload every asset to a **draft** release. Missing uploads fail the job.
5. Build and push the Docker images, then verify version metadata and native-codec support in the published AMD64 image.
6. Download the draft assets and verify the exact asset set and checksums. Only then publish the release.

A failed build or upload leaves the release unpublished so it can be retried. Docker tags may already exist if a later step fails. Once published, GitHub's immutable release protection prevents changes to its tag and assets: fixes require a new version. Do not force-move an existing published tag.

The release workflow uses the curated changelog section; it does not overwrite the repository changelog with a generated commit list. The Helm chart is uploaded before publication, and upload failures are no longer ignored.

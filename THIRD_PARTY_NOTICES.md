# Third-party notices

Project-owned code and chart are licensed under [Apache-2.0](LICENSE),
Copyright 2026 Dmitry Vasiliev. This does not relicense dependencies, model
weights, container base layers or NVIDIA components.

No backend, decoder, model or container image has been redistributed by this
project yet. This initial inventory distinguishes verified source licensing
from pending distribution obligations. P05 and C02 must resolve the exact
versions, build flags, files, notices, source obligations and redistributable
terms against the actual images and their software bill of materials before
release. A planned component is not evidence of a completed license audit.

| Component | Verified basis | Distribution status |
| --- | --- | --- |
| OpenWhispr/whisper.cpp, including bundled ggml | [Pinned source LICENSE](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/LICENSE): MIT; Copyright (c) 2023-2026 The ggml authors | Backend packaging preserves the source LICENSE and bundled component notices under `/usr/local/share/whisper/licenses/whisper`. No native source changes. C02 retains the full dependency audit. |
| FFmpeg decoder | [FFmpeg n9.0.2 license](https://github.com/FFmpeg/FFmpeg/blob/n9.0.2/LICENSE.md); LGPL 2.1 or later with GPL/nonfree components disabled | API packaging uses shared libraries and preserves license texts, exact source archive, configure flags and tool/package versions under `/usr/local/share/whisper`. Replacement libraries can be supplied in a derived image or mounted by its operator. Published-artifact/source availability is checked separately in C02/R01. |
| NVIDIA CUDA/runtime | [CUDA 12.8.1 agreement and supplement](https://docs.nvidia.com/cuda/archive/12.8.1/eula/index.html) | CUDA 12.8.1 builder/runtime images are pinned by digest. Backend packaging preserves the version-specific EULA and inherited container license. CUDA is not Apache-2.0; C02/R01 must verify actual redistributables before publication. |
| Container base/system packages | Pinned Ubuntu 24.04 and NVIDIA CUDA 12.8.1 Ubuntu 24.04 bases; see `containers/build-inputs.json` | Runtime images retain installed package copyright/license material and record `runtime-packages.txt`. Full SBOM, vulnerability and redistribution checks remain C02/R01. No artifact has been published. |
| Go dependencies | F01 resolved the actual Go dependency graph; source license inventory below | Both image definitions package module license/notice files, the Go license and actual executable module/compiler metadata under `/usr/local/share/whisper`. |
| Model weights | No models included or distributed | Selected model source/license and distribution policy pending; do not infer weight licensing from whisper.cpp. |

## Go source dependency inventory

F01 inspected the license files in the authenticated module downloads for the
20 external modules used by the production import graph. Versions are fixed by
`go.mod` and `go.sum`; this table does not replace the full license texts and
copyright notices required when distributing the resulting binaries/images.
Test-only and build-tool dependencies require the later distribution audit if
they are included in a shipped artifact.

| Module | Version | Source license |
| --- | --- | --- |
| github.com/labstack/echo/v5 | v5.4.0 | MIT |
| github.com/go-playground/validator/v10 | v10.30.5 | MIT |
| github.com/go-viper/mapstructure/v2 | v2.4.0 | MIT |
| github.com/knadh/koanf/v2 | v2.3.8 | MIT |
| github.com/knadh/koanf/maps | v0.1.2 | MIT |
| github.com/knadh/koanf/providers/basicflag | v1.1.1 | MIT |
| github.com/knadh/koanf/providers/confmap | v1.0.1 | MIT |
| github.com/knadh/koanf/providers/env/v2 | v2.0.2 | MIT |
| github.com/knadh/koanf/providers/file | v1.2.1 | MIT |
| github.com/gabriel-vasile/mimetype | v1.4.15 | MIT |
| github.com/go-playground/locales | v0.14.1 | MIT |
| github.com/go-playground/universal-translator | v0.18.1 | MIT |
| github.com/leodido/go-urn | v1.5.0 | MIT |
| github.com/mitchellh/copystructure | v1.2.0 | MIT |
| github.com/mitchellh/reflectwalk | v1.0.2 | MIT |
| github.com/fsnotify/fsnotify | v1.9.0 | BSD-3-Clause |
| golang.org/x/crypto | v0.57.0 | BSD-3-Clause |
| golang.org/x/sys | v0.48.0 | BSD-3-Clause |
| golang.org/x/text | v0.42.0 | BSD-3-Clause |
| golang.org/x/time | v0.15.0 | BSD-3-Clause |

The [code of conduct](CODE_OF_CONDUCT.md) is adapted from Contributor Covenant
2.1 and retains its source attribution. It is a community document, not a
claim about runtime dependency licensing.

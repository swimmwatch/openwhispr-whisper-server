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
| OpenWhispr/whisper.cpp, including bundled ggml | [Pinned source LICENSE](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/LICENSE): MIT; Copyright (c) 2023-2026 The ggml authors | Backend not built or redistributed; preserve the copyright and full permission notice in distributed copies. Audit other bundled components in P05/C02. |
| FFmpeg decoder | [Official legal guidance](https://www.ffmpeg.org/legal.html): baseline LGPL 2.1 or later; enabled GPL components change obligations; nonfree configurations may prohibit redistribution | Exact version, flags, linkage, license files and corresponding-source obligations pending P05/C02. |
| NVIDIA CUDA/runtime | [Official CUDA EULA](https://docs.nvidia.com/cuda/eula/contents.html) | Exact selected version, applicable agreement/supplement and permitted redistributables pending P05/C02; not Apache-2.0. |
| Container base/system packages | No base images selected or distributed yet | Exact digests, package licenses and required notices/source offers pending P05/C02. |
| Go dependencies | Current go.mod contains no dependencies | Add actual licenses when dependencies are introduced. |
| Model weights | No models included or distributed | Selected model source/license and distribution policy pending; do not infer weight licensing from whisper.cpp. |

The [code of conduct](CODE_OF_CONDUCT.md) is adapted from Contributor Covenant
2.1 and retains its source attribution. It is a community document, not a
claim about runtime dependency licensing.

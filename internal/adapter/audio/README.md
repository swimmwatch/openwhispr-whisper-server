# Bounded audio normalization

`Decoder` implements the application-owned `Normalizer` port. Its input path is
an immutable, regular upload created by the HTTP adapter, never a client path.
One decoder process runs at a time. Retained-output credits remain reserved until
the returned WAV reader is closed after all consumers have stopped.

FFmpeg receives capped stdin as `cache:pipe:0`, with only pipe/cache protocols
and the supported audio-container demuxers allowed. PCM output is counted before
writing; exceeding the duration bound kills/reaps the process rather than
returning truncated audio. The output is canonical mono 16 kHz signed 16-bit WAV.
Diagnostics are discarded and inherited `FFREPORT` is disabled.

The [cache protocol](https://ffmpeg.org/ffmpeg-all.html#cache) permits seeking for
MP4 with its index at the end. Its temporary disk cache belongs on the isolated,
size-limited `/tmp` volume provided by packaging. The integration test observes
the real cache descriptor, its size and removal after cancellation; checking
directory entries alone would miss an unlinked open cache.

Tests generate synthetic fixtures at runtime: a 440 Hz sine wave at 24 kHz,
encoded using PCM, MP3, AAC, MPEG audio, Vorbis, Opus and FLAC; black 16x16 video
without audio; and invalid HLS/DASH playlists. No recorded/private audio is
shipped. MPGA uses an explicit MP2 muxer; MP4 keeps `moov` after `mdat`. Local
HTTP and inotify traps observe attempted external reads.

P03 integration evidence used installed FFmpeg 6.1.1-3ubuntu5 and Go 1.27.3.
This verifies the exercised CLI contract with that compatible developer binary.
The release source baseline remains FFmpeg n9.0.2; its packaging and GPU/runtime
qualification belong to later packets. Container volume confinement is not
established by these host integration tests.

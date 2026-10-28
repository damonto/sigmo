# Opus reference packets

`opus-reference.json` contains synthetic audio encoded and decoded by unmodified
libopus 1.6.1, independently of Pion. Each case has six consecutive packets and
the reference mono 16 kHz output's sample count and RMS. Normal tests only read
the fixture and do not load libopus or require a C toolchain.

The input combines 440 Hz (left/mono) or 880 Hz (right) at amplitude 7000 with
5 kHz at amplitude 2500. Both sine phases continue across packets. Encoding uses
32 kbit/s and `OPUS_APPLICATION_AUDIO`. Cases cover 16 kHz mono SILK at 20/60 ms,
48 kHz mono hybrid at 20 ms, and 48 kHz CELT at 10 ms and stereo at 20 ms.
The reference encoder's internal force-mode control selects SILK/hybrid/CELT
(1000/1001/1002); this control is used only to produce fixtures. SILK uses
wideband and the other modes use fullband. The decoder uses 16 kHz mono output.

The signals are generated locally and contain no speech or carrier data.

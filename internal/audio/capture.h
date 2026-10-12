#ifndef DICTATOR_CAPTURE_H
#define DICTATOR_CAPTURE_H

#include <stdint.h>

typedef struct dk_rec dk_rec;

// Starts recording 16 kHz mono from the default input into path: AAC in an
// .m4a container when aac != 0 (falls back to WAV if the encoder is
// unavailable; *is_aac reports which). handle is passed back to goAudioLevel.
// Returns NULL and sets *err (OSStatus) on failure.
dk_rec *dk_rec_start(const char *path, int aac, uintptr_t handle, int *err, int *is_aac);

// Asks the recorder to continue in a new file at path from the next buffer on.
// The finished file is reported with goSegmentClosed(handle). Any thread.
void dk_rec_cut(dk_rec *r, const char *path);

// Stops, flushes and frees the recorder. Returns the seconds of audio captured.
// Every earlier segment has been reported when it returns. *cut_pending is 1
// if a requested cut never took effect (its path was not created).
double dk_rec_stop(dk_rec *r, int *cut_pending);

// Decodes any audio file to 16 kHz mono 16-bit WAV (used if a model rejects m4a).
int dk_to_wav(const char *in_path, const char *out_path);

// Microphone permission: 0 not determined, 1 restricted, 2 denied, 3 authorized.
int dk_mic_status(void);
// Asks for permission (shows the system prompt once); blocks until answered.
int dk_mic_request(void);

#endif

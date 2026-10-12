// Microphone capture with AudioQueue + ExtAudioFile.
//
// The queue exists only while recording, so the mic (and its orange menu-bar
// dot) is off the rest of the time. Audio goes straight from the callback into
// ExtAudioFileWriteAsync, which encodes AAC on its own thread; the callback
// only computes an RMS level per 100 ms buffer for silence detection.
//
// Live mode cuts the recording into phrases: dk_rec_cut hands the callback a
// new path, the callback switches files between two buffers (so no audio is
// lost) and the finished file is closed on a serial queue, which then reports
// it with goSegmentClosed.

#import <AVFoundation/AVFoundation.h>
#include <AudioToolbox/AudioToolbox.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

#include "capture.h"
#include "_cgo_export.h"

#define DK_RATE 16000.0
#define DK_BUFFERS 3
#define DK_BUFFER_FRAMES 1600 // 100 ms

struct dk_rec {
	AudioQueueRef queue;
	ExtAudioFileRef file;
	uintptr_t handle;
	volatile int running;
	uint64_t frames;
	int aac;
	char *next_path;          // set by dk_rec_cut, taken by the callback (atomic)
	void *closer;             // dispatch queue (retained) that closes finished segments, in order
};

static AudioStreamBasicDescription dk_pcm(void) {
	AudioStreamBasicDescription f = {0};
	f.mSampleRate = DK_RATE;
	f.mFormatID = kAudioFormatLinearPCM;
	f.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked;
	f.mChannelsPerFrame = 1;
	f.mBitsPerChannel = 16;
	f.mBytesPerFrame = 2;
	f.mFramesPerPacket = 1;
	f.mBytesPerPacket = 2;
	return f;
}

static OSStatus dk_open_file(dk_rec *r, CFURLRef url, int aac);

// Called on the queue's thread between two buffers: start writing to the path
// dk_rec_cut asked for, and close the previous file off this thread.
static void dk_take_cut(dk_rec *r) {
	char *path = __atomic_exchange_n(&r->next_path, NULL, __ATOMIC_ACQ_REL);
	if (!path) return;
	ExtAudioFileRef old = r->file;
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path, (CFIndex)strlen(path), false);
	r->file = NULL;
	OSStatus st = dk_open_file(r, url, r->aac);
	CFRelease(url);
	if (st != noErr) {
		// keep recording into the old file; the phrase just gets longer
		if (r->file) ExtAudioFileDispose(r->file);
		r->file = old;
		free(path);
		return;
	}
	free(path);
	uintptr_t h = r->handle;
	dispatch_async((__bridge dispatch_queue_t)r->closer, ^{
		ExtAudioFileDispose(old); // flushes the encoder and finalises the file
		goSegmentClosed(h);
	});
}

static void dk_input(void *ud, AudioQueueRef q, AudioQueueBufferRef buf,
                     const AudioTimeStamp *ts, UInt32 npk,
                     const AudioStreamPacketDescription *desc) {
	dk_rec *r = (dk_rec *)ud;
	if (npk > 0 && r->running) dk_take_cut(r);
	if (npk > 0 && r->file) {
		AudioBufferList abl;
		abl.mNumberBuffers = 1;
		abl.mBuffers[0].mNumberChannels = 1;
		abl.mBuffers[0].mDataByteSize = buf->mAudioDataByteSize;
		abl.mBuffers[0].mData = buf->mAudioData;
		ExtAudioFileWriteAsync(r->file, npk, &abl);

		const int16_t *s = (const int16_t *)buf->mAudioData;
		double sum = 0;
		for (UInt32 i = 0; i < npk; i++) {
			double v = s[i] / 32768.0;
			sum += v * v;
		}
		r->frames += npk;
		goAudioLevel(r->handle, sqrt(sum / npk), (int)(npk * 1000 / (UInt32)DK_RATE));
	}
	if (r->running) AudioQueueEnqueueBuffer(q, buf, 0, NULL);
}

static OSStatus dk_open_file(dk_rec *r, CFURLRef url, int aac) {
	AudioStreamBasicDescription pcm = dk_pcm();
	OSStatus st;
	if (aac) {
		AudioStreamBasicDescription a = {0};
		a.mSampleRate = DK_RATE;
		a.mFormatID = kAudioFormatMPEG4AAC;
		a.mChannelsPerFrame = 1;
		st = ExtAudioFileCreateWithURL(url, kAudioFileM4AType, &a, NULL,
		                               kAudioFileFlags_EraseFile, &r->file);
	} else {
		st = ExtAudioFileCreateWithURL(url, kAudioFileWAVEType, &pcm, NULL,
		                               kAudioFileFlags_EraseFile, &r->file);
	}
	if (st != noErr) return st;
	st = ExtAudioFileSetProperty(r->file, kExtAudioFileProperty_ClientDataFormat,
	                             sizeof(pcm), &pcm);
	if (st != noErr) return st;
	if (aac) {
		// 32 kbit/s is plenty for speech recognition (~4 KB per second of audio).
		AudioConverterRef conv = NULL;
		UInt32 sz = sizeof(conv);
		if (ExtAudioFileGetProperty(r->file, kExtAudioFileProperty_AudioConverter, &sz, &conv) == noErr && conv) {
			UInt32 rate = 32000;
			if (AudioConverterSetProperty(conv, kAudioConverterEncodeBitRate, sizeof(rate), &rate) == noErr) {
				CFArrayRef cfg = NULL;
				ExtAudioFileSetProperty(r->file, kExtAudioFileProperty_ConverterConfig, sizeof(cfg), &cfg);
			}
		}
	}
	// prime the async writer outside the realtime callback
	return ExtAudioFileWriteAsync(r->file, 0, NULL);
}

dk_rec *dk_rec_start(const char *path, int aac, uintptr_t handle, int *err, int *is_aac) {
	dk_rec *r = calloc(1, sizeof(dk_rec));
	r->handle = handle;
	r->closer = (void *)CFBridgingRetain(dispatch_queue_create("dictator.segments", DISPATCH_QUEUE_SERIAL));
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)path,
	                                                       (CFIndex)strlen(path), false);
	OSStatus st = dk_open_file(r, url, aac);
	*is_aac = aac;
	if (st != noErr && aac) { // no AAC encoder? plain WAV still works everywhere
		if (r->file) { ExtAudioFileDispose(r->file); r->file = NULL; }
		st = dk_open_file(r, url, 0);
		*is_aac = 0;
	}
	r->aac = *is_aac;
	CFRelease(url);
	if (st != noErr) goto fail;

	AudioStreamBasicDescription pcm = dk_pcm();
	// NULL run loop: callbacks arrive on the queue's own thread, not ours.
	st = AudioQueueNewInput(&pcm, dk_input, r, NULL, NULL, 0, &r->queue);
	if (st != noErr) goto fail;
	for (int i = 0; i < DK_BUFFERS; i++) {
		AudioQueueBufferRef b;
		st = AudioQueueAllocateBuffer(r->queue, DK_BUFFER_FRAMES * 2, &b);
		if (st != noErr) goto fail;
		AudioQueueEnqueueBuffer(r->queue, b, 0, NULL);
	}
	r->running = 1;
	st = AudioQueueStart(r->queue, NULL);
	if (st != noErr) goto fail;
	return r;

fail:
	*err = (int)st;
	r->running = 0;
	if (r->queue) AudioQueueDispose(r->queue, true);
	if (r->file) ExtAudioFileDispose(r->file);
	CFBridgingRelease(r->closer);
	free(r);
	return NULL;
}

void dk_rec_cut(dk_rec *r, const char *path) {
	char *old = __atomic_exchange_n(&r->next_path, strdup(path), __ATOMIC_ACQ_REL);
	free(old); // an earlier cut that never happened (can't occur between buffers in practice)
}

double dk_rec_stop(dk_rec *r, int *cut_pending) {
	if (!r) return 0;
	r->running = 0;
	AudioQueueStop(r->queue, true);   // synchronous: delivers the last buffers first
	AudioQueueDispose(r->queue, true);
	ExtAudioFileDispose(r->file);     // flushes the encoder and finalises the file
	dispatch_sync((__bridge dispatch_queue_t)r->closer, ^{}); // earlier segments are closed and reported
	CFBridgingRelease(r->closer);
	char *pending = __atomic_exchange_n(&r->next_path, NULL, __ATOMIC_ACQ_REL);
	*cut_pending = pending != NULL;
	free(pending);
	double secs = (double)r->frames / DK_RATE;
	free(r);
	return secs;
}

int dk_to_wav(const char *in_path, const char *out_path) {
	CFURLRef in = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)in_path, (CFIndex)strlen(in_path), false);
	CFURLRef out = CFURLCreateFromFileSystemRepresentation(NULL, (const UInt8 *)out_path, (CFIndex)strlen(out_path), false);
	ExtAudioFileRef src = NULL, dst = NULL;
	AudioStreamBasicDescription pcm = dk_pcm();
	OSStatus st = ExtAudioFileOpenURL(in, &src);
	if (st == noErr) st = ExtAudioFileSetProperty(src, kExtAudioFileProperty_ClientDataFormat, sizeof(pcm), &pcm);
	if (st == noErr) st = ExtAudioFileCreateWithURL(out, kAudioFileWAVEType, &pcm, NULL, kAudioFileFlags_EraseFile, &dst);
	if (st == noErr) st = ExtAudioFileSetProperty(dst, kExtAudioFileProperty_ClientDataFormat, sizeof(pcm), &pcm);
	int16_t buf[4096];
	while (st == noErr) {
		AudioBufferList abl;
		abl.mNumberBuffers = 1;
		abl.mBuffers[0].mNumberChannels = 1;
		abl.mBuffers[0].mDataByteSize = sizeof(buf);
		abl.mBuffers[0].mData = buf;
		UInt32 frames = sizeof(buf) / 2;
		st = ExtAudioFileRead(src, &frames, &abl);
		if (st != noErr || frames == 0) break;
		st = ExtAudioFileWrite(dst, frames, &abl);
	}
	if (src) ExtAudioFileDispose(src);
	if (dst) ExtAudioFileDispose(dst);
	CFRelease(in);
	CFRelease(out);
	return (int)st;
}

int dk_mic_status(void) {
	switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio]) {
	case AVAuthorizationStatusNotDetermined: return 0;
	case AVAuthorizationStatusRestricted: return 1;
	case AVAuthorizationStatusDenied: return 2;
	default: return 3;
	}
}

int dk_mic_request(void) {
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	__block int granted = 0;
	[AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio
	                         completionHandler:^(BOOL ok) {
		granted = ok ? 1 : 0;
		dispatch_semaphore_signal(sem);
	}];
	dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
	return granted;
}

// Package audio records the microphone to a compressed file and reports
// levels for silence detection. See capture.m for the native side.
package audio

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AudioToolbox -framework AVFoundation -framework CoreFoundation -framework Foundation
#include <stdlib.h>
#include "capture.h"
*/
import "C"

import (
	"fmt"
	"path/filepath"
	"runtime/cgo"
	"strings"
	"sync"
	"unsafe"
)

// Segment is one finished piece of a recording (live mode cuts at pauses;
// otherwise the whole recording is a single segment).
type Segment struct {
	Index    int
	Path     string
	SpeechMs int  // how much of it was above the speech threshold
	Final    bool // the last segment of the recording
}

// Recorder is one recording. Create with Start, finish with Stop.
type Recorder struct {
	c         *C.dk_rec
	h         cgo.Handle
	onLevel   func(rms float64, ms int)
	onSegment func(Segment)
	Path      string // the first segment's file
	AAC       bool   // false if it fell back to WAV

	mu   sync.Mutex
	open []Segment // the segment being written, plus any cut waiting to be closed
	next int
}

// Start begins recording into path (an .m4a; WAV is used if AAC is
// unavailable, reported by Recorder.AAC). onLevel is called about every 100 ms
// on the audio thread; it must be quick and must not block. onSegment (may be
// nil) receives each segment closed by Cut, on a background thread.
func Start(path string, onLevel func(rms float64, ms int), onSegment func(Segment)) (*Recorder, error) {
	r := &Recorder{onLevel: onLevel, onSegment: onSegment, Path: path, open: []Segment{{Path: path}}, next: 1}
	r.h = cgo.NewHandle(r)
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	var st, isAAC C.int
	r.mu.Lock()
	r.c = C.dk_rec_start(cp, 1, C.uintptr_t(r.h), &st, &isAAC)
	r.mu.Unlock()
	if r.c == nil {
		r.h.Delete()
		return nil, fmt.Errorf("could not start the microphone (OSStatus %d)", int(st))
	}
	r.AAC = isAAC != 0
	return r, nil
}

// Cut closes the current segment (crediting it with speechMs of speech) and
// continues into a new file from the next buffer on. Safe from the level callback.
func (r *Recorder) Cut(speechMs int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.c == nil {
		return
	}
	r.open[len(r.open)-1].SpeechMs = speechMs
	ext := filepath.Ext(r.Path)
	seg := Segment{Index: r.next, Path: fmt.Sprintf("%s-%d%s", strings.TrimSuffix(r.Path, ext), r.next, ext)}
	r.next++
	r.open = append(r.open, seg)
	cp := C.CString(seg.Path)
	defer C.free(unsafe.Pointer(cp))
	C.dk_rec_cut(r.c, cp)
}

// Stop ends the recording, finalises the file and returns its total duration
// and the last segment. Every earlier segment has been passed to onSegment by
// then. The caller adds the speech of the phrase still open (only readable once
// the level callback has stopped, i.e. after Stop) to the last segment's SpeechMs.
func (r *Recorder) Stop() (float64, Segment) {
	r.mu.Lock()
	c := r.c
	r.c = nil // no more cuts
	r.mu.Unlock()
	if c == nil {
		return 0, Segment{}
	}
	var pending C.int
	secs := float64(C.dk_rec_stop(c, &pending))
	r.h.Delete()
	r.mu.Lock()
	defer r.mu.Unlock()
	last := r.open[len(r.open)-1]
	if pending != 0 && len(r.open) > 1 {
		// the last cut never happened: its audio is still in the previous file,
		// which was already credited with the speech before the cut
		last = r.open[len(r.open)-2]
	} else {
		last.SpeechMs = 0
	}
	last.Final = true
	return secs, last
}

//export goSegmentClosed
func goSegmentClosed(h C.uintptr_t) {
	r, ok := cgo.Handle(h).Value().(*Recorder)
	if !ok {
		return
	}
	r.mu.Lock()
	seg := r.open[0]
	r.open = r.open[1:]
	r.mu.Unlock()
	if r.onSegment != nil {
		r.onSegment(seg)
	}
}

//export goAudioLevel
func goAudioLevel(h C.uintptr_t, rms C.double, ms C.int) {
	if r, ok := cgo.Handle(h).Value().(*Recorder); ok && r.onLevel != nil {
		r.onLevel(float64(rms), int(ms))
	}
}

// ToWAV converts any audio file to 16 kHz mono WAV.
func ToWAV(in, out string) error {
	ci, co := C.CString(in), C.CString(out)
	defer C.free(unsafe.Pointer(ci))
	defer C.free(unsafe.Pointer(co))
	if st := C.dk_to_wav(ci, co); st != 0 {
		return fmt.Errorf("convert to WAV failed (OSStatus %d)", int(st))
	}
	return nil
}

// Permission states.
const (
	PermUnknown    = 0
	PermRestricted = 1
	PermDenied     = 2
	PermGranted    = 3
)

func MicStatus() int { return int(C.dk_mic_status()) }

// RequestMic shows the system prompt (first time only) and blocks for the answer.
// Never call it on the main thread.
func RequestMic() bool { return C.dk_mic_request() != 0 }

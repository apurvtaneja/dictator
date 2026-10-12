// Package sysproxy reads the macOS system HTTPS proxy, so requests follow the
// same route as the rest of the machine (corporate proxies). Environment
// variables (HTTPS_PROXY, NO_PROXY) take precedence.
package sysproxy

/*
#cgo LDFLAGS: -framework CFNetwork -framework CoreFoundation
#include <CFNetwork/CFNetwork.h>
#include <CoreFoundation/CoreFoundation.h>

static int sp_int(CFDictionaryRef d, CFStringRef key) {
	CFNumberRef n = CFDictionaryGetValue(d, key);
	int v = 0;
	if (n && CFGetTypeID(n) == CFNumberGetTypeID()) CFNumberGetValue(n, kCFNumberIntType, &v);
	return v;
}

// Fills host (cap bytes) and port; returns 1 if an HTTPS proxy is enabled.
// *pac is set when a PAC file / auto-discovery is configured.
static int sp_https(char *host, int cap, int *port, int *pac) {
	CFDictionaryRef d = CFNetworkCopySystemProxySettings();
	if (!d) return 0;
	*pac = sp_int(d, kCFNetworkProxiesProxyAutoConfigEnable) ||
	       sp_int(d, CFSTR("ProxyAutoDiscoveryEnable"));
	int on = sp_int(d, CFSTR("HTTPSEnable"));
	if (on) {
		CFStringRef h = CFDictionaryGetValue(d, CFSTR("HTTPSProxy"));
		*port = sp_int(d, CFSTR("HTTPSPort"));
		if (!h || !CFStringGetCString(h, host, cap, kCFStringEncodingUTF8)) on = 0;
	}
	CFRelease(d);
	return on;
}
*/
import "C"

import (
	"net/http"
	"net/url"
	"strconv"
	"unsafe"
)

// Info describes the system configuration (for --check).
type Info struct {
	HTTPS string // "host:port" or ""
	PAC   bool   // a PAC file / WPAD is configured (not evaluated)
}

func Read() Info {
	buf := make([]byte, 256)
	var port, pac C.int
	on := C.sp_https((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)), &port, &pac)
	inf := Info{PAC: pac != 0}
	if on != 0 {
		host := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
		if port == 0 {
			port = 443
		}
		inf.HTTPS = host + ":" + strconv.Itoa(int(port))
	}
	return inf
}

// ProxyFunc is an http.Transport.Proxy: environment first, then the system
// HTTPS proxy (read once; proxy changes apply on the next app start).
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	sys := Read()
	var sysURL *url.URL
	if sys.HTTPS != "" {
		sysURL = &url.URL{Scheme: "http", Host: sys.HTTPS}
	}
	return func(r *http.Request) (*url.URL, error) {
		if u, err := http.ProxyFromEnvironment(r); u != nil || err != nil {
			return u, err
		}
		return sysURL, nil
	}
}

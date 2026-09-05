package registry

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// probeWorkers limits how many tags are resolved at once. The probes are
// latency-bound, so this sits well above the core count.
const probeWorkers = 24

// TagForDigest probes candidate tags of image until one resolves to digest,
// returning "" when none matches. Probes run concurrently and stop early.
func TagForDigest(f Fetcher, image string, candidates []string, digest string) string {
	var (
		wg sync.WaitGroup
		// The first worker to match wins: CompareAndSwap from nil is what makes
		// "first" well defined without a lock around every read of it.
		match atomic.Pointer[string]
		queue = make(chan string)
	)

	for range min(probeWorkers, len(candidates)) {
		wg.Go(func() {
			for tag := range queue {
				if match.Load() != nil {
					continue
				}

				candidateDigest, err := f.Digest(image + ":" + tag)
				if err != nil {
					slog.Debug("Failed probing tag", "image", image, "tag", tag, "error", err)
					continue
				}
				if candidateDigest != digest {
					continue
				}

				match.CompareAndSwap(nil, &tag)
			}
		})
	}

	for _, tag := range candidates {
		if match.Load() != nil {
			break
		}
		queue <- tag
	}
	close(queue)
	wg.Wait()

	if m := match.Load(); m != nil {
		return *m
	}
	return ""
}
